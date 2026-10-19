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

// TokenStore reads machine-local SCM tokens by host and account login.
type TokenStore interface {
	GitHubToken(ctx context.Context, host, login string) (string, error)
}

// Resolver maps a project's configured GitHub account to its machine-local
// token for the project's repository host.
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
	Host    string
	Account string
	Env     map[string]string
}

// UnavailableError reports that a project is bound to an account whose token
// is missing on this machine. It names the fix without carrying the secret.
type UnavailableError struct {
	ProjectID string
	Host      string
	Account   string
	Machine   string
	Cause     error
}

// FixCommand is the command an operator runs on the affected machine.
func (e *UnavailableError) FixCommand() string {
	if e == nil || strings.TrimSpace(e.Account) == "" {
		return "ao secret set github <login>"
	}
	command := "ao secret set github " + e.Account
	host, err := domain.NormalizeGitHubHost(e.Host)
	if err == nil && host != "github.com" {
		command += " --github-host " + host
	}
	return command
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
	host, err := domain.NormalizeGitHubHost(e.Host)
	if err != nil {
		host = e.Host
	}
	return fmt.Sprintf(
		"project %q is bound to GitHub account %q on host %q, but no token is stored on machine %q; run %q on that machine",
		e.ProjectID, e.Account, host, machine, e.FixCommand(),
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
	host, err := GitHubHost(project)
	if err != nil {
		return Resolved{}, fmt.Errorf("project %q: %w", project.ID, err)
	}
	if r == nil || r.tokens == nil {
		return Resolved{}, &UnavailableError{
			ProjectID: project.ID,
			Host:      host,
			Account:   account,
			Machine:   r.machine(),
			Cause:     errors.New("secret store is not configured"),
		}
	}
	token, err := r.tokens.GitHubToken(ctx, host, account)
	if err != nil {
		return Resolved{}, &UnavailableError{
			ProjectID: project.ID,
			Host:      host,
			Account:   account,
			Machine:   r.machine(),
			Cause:     err,
		}
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Resolved{}, &UnavailableError{
			ProjectID: project.ID,
			Host:      host,
			Account:   account,
			Machine:   r.machine(),
			Cause:     errors.New("stored token is empty"),
		}
	}
	return Resolved{Host: host, Account: account, Env: EnvForHost(host, token)}, nil
}

// GitHubHost derives the project's GitHub host from its repository origin,
// falling back to canonicalRepoURL and then github.com.
func GitHubHost(project domain.ProjectRecord) (string, error) {
	for _, raw := range []string{project.RepoOriginURL, project.Config.CanonicalRepoURL} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		identity, err := domain.ParseRepositoryIdentity(raw)
		if err != nil {
			continue
		}
		if identity.Provider != "github" {
			return "", fmt.Errorf("scm.github.account requires a GitHub repository origin, found %s", identity.Provider)
		}
		return identity.Host, nil
	}
	return "github.com", nil
}

// EnvForHost uses GH_TOKEN for github.com and GH_HOST plus enterprise token
// variables elsewhere, neutralizing default-host variables in that case.
func EnvForHost(host, token string) map[string]string {
	host, err := domain.NormalizeGitHubHost(host)
	if err != nil || host == "github.com" {
		return map[string]string{
			"GH_TOKEN":                token,
			"GH_HOST":                 "",
			"GH_ENTERPRISE_TOKEN":     "",
			"GITHUB_ENTERPRISE_TOKEN": "",
			"GITHUB_TOKEN":            "",
		}
	}
	return map[string]string{
		"GH_HOST":                 host,
		"GH_ENTERPRISE_TOKEN":     token,
		"GITHUB_ENTERPRISE_TOKEN": token,
	}
}

// TokenForAccount resolves one host/account for daemon-side SCM calls. It
// implements the GitHub adapter's AccountTokenSource.
func (r *Resolver) TokenForAccount(ctx context.Context, host, account string) (string, error) {
	account = strings.TrimSpace(account)
	if account == "" {
		return "", errors.New("scm identity: account is required")
	}
	host, err := domain.NormalizeGitHubHost(host)
	if err != nil {
		return "", fmt.Errorf("scm identity: invalid GitHub host %q: %w", host, err)
	}
	if r == nil || r.tokens == nil {
		return "", errors.New("scm identity: secret store is not configured")
	}
	token, err := r.tokens.GitHubToken(ctx, host, account)
	if err != nil {
		return "", fmt.Errorf("GitHub account %q on host %q has no usable token on machine %q; run %q on that machine: %w",
			account, host, r.machine(), fixCommand(host, account), err)
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("GitHub account %q on host %q has an empty token on machine %q; run %q on that machine",
			account, host, r.machine(), fixCommand(host, account))
	}
	return token, nil
}

func fixCommand(host, account string) string {
	if strings.TrimSpace(account) == "" {
		return "ao secret set github <login>"
	}
	command := "ao secret set github " + strings.TrimSpace(account)
	normalized, err := domain.NormalizeGitHubHost(host)
	if err == nil && normalized != "github.com" {
		command += " --github-host " + normalized
	}
	return command
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
