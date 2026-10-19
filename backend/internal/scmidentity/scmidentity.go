// Package scmidentity resolves a project's SCM account to a machine-local
// token and injects it into that session without durable state.
package scmidentity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TokenStore reads machine-local SCM tokens by account login.
type TokenStore interface {
	GitHubToken(ctx context.Context, login string) (string, error)
}

// Resolver maps a project's configured GitHub account to its machine-local
// token.
type Resolver struct {
	tokens   TokenStore
	hostname func() (string, error)
}

// New returns a resolver. A nil hostname falls back to os.Hostname.
func New(tokens TokenStore, hostname func() (string, error)) *Resolver {
	if hostname == nil {
		hostname = os.Hostname
	}
	return &Resolver{tokens: tokens, hostname: hostname}
}

// Resolved is a project's resolved SCM identity. Env holds only the variables
// a session needs; it never becomes durable session metadata.
type Resolved struct {
	Account string
	Env     map[string]string
}

// UnavailableError reports that a project is bound to an account whose token
// is missing on this machine. It names the fix without carrying the secret.
type UnavailableError struct {
	ProjectID string
	Account   string
	Machine   string
	Cause     error
}

// FixCommand is the command an operator runs on the affected machine.
func (e *UnavailableError) FixCommand() string {
	if e == nil || strings.TrimSpace(e.Account) == "" {
		return "ao secret set github <login>"
	}
	return "ao secret set github " + e.Account
}

// Error renders the structured, actionable message shown to the operator.
func (e *UnavailableError) Error() string {
	if e == nil {
		return "scm identity unavailable"
	}
	machine := e.Machine
	if machine == "" {
		machine = "this machine"
	}
	return fmt.Sprintf(
		"project %q is bound to GitHub account %q, but no token is stored on machine %q; run %q on that machine",
		e.ProjectID, e.Account, machine, e.FixCommand(),
	)
}

// Unwrap exposes the underlying store error for errors.Is checks.
func (e *UnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Resolve returns the environment for a project's bound GitHub account. A
// project with no binding resolves empty, preserving today's fallback.
func (r *Resolver) Resolve(ctx context.Context, project domain.ProjectRecord) (Resolved, error) {
	account := project.Config.GitHubAccount()
	if account == "" {
		return Resolved{}, nil
	}
	if r == nil || r.tokens == nil {
		return Resolved{}, &UnavailableError{
			ProjectID: project.ID,
			Account:   account,
			Machine:   r.machine(),
			Cause:     errors.New("secret store is not configured"),
		}
	}
	token, err := r.tokens.GitHubToken(ctx, account)
	if err != nil {
		return Resolved{}, &UnavailableError{
			ProjectID: project.ID,
			Account:   account,
			Machine:   r.machine(),
			Cause:     err,
		}
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Resolved{}, &UnavailableError{
			ProjectID: project.ID,
			Account:   account,
			Machine:   r.machine(),
			Cause:     errors.New("stored token is empty"),
		}
	}
	return Resolved{
		Account: account,
		// GH_TOKEN wins over gh's stored active account, so both `gh api user`
		// and the gh credential helper use the project's identity.
		Env: map[string]string{"GH_TOKEN": token},
	}, nil
}

// TokenForAccount resolves one account for daemon-side SCM calls. It
// implements the GitHub adapter's AccountTokenSource.
func (r *Resolver) TokenForAccount(ctx context.Context, account string) (string, error) {
	account = strings.TrimSpace(account)
	if account == "" {
		return "", errors.New("scm identity: account is required")
	}
	if r == nil || r.tokens == nil {
		return "", errors.New("scm identity: secret store is not configured")
	}
	token, err := r.tokens.GitHubToken(ctx, account)
	if err != nil {
		return "", fmt.Errorf("GitHub account %q has no usable token on machine %q; run %q on that machine: %w",
			account, r.machine(), fixCommand(account), err)
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("GitHub account %q has an empty token on machine %q; run %q on that machine",
			account, r.machine(), fixCommand(account))
	}
	return token, nil
}

func fixCommand(account string) string {
	if strings.TrimSpace(account) == "" {
		return "ao secret set github <login>"
	}
	return "ao secret set github " + strings.TrimSpace(account)
}

func (r *Resolver) machine() string {
	if r == nil || r.hostname == nil {
		return ""
	}
	name, err := r.hostname()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}
