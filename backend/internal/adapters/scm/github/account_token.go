package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// AccountTokenSource resolves a project-bound GitHub login to a token on the
// machine running the daemon. Implementations must never log the token.
type AccountTokenSource interface {
	TokenForAccount(ctx context.Context, account string) (string, error)
}

type accountTokenContextKey struct{}

// WithAccountToken overrides the machine token for one provider call. The
// value never becomes durable state and lives only in this context.
func WithAccountToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, accountTokenContextKey{}, token)
}

func accountTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(accountTokenContextKey{}).(string)
	if !ok || strings.TrimSpace(token) == "" {
		return "", false
	}
	return token, true
}

// withAccountToken resolves account for one call. An account whose token is
// missing is an auth-class failure, not a silent fallback to another account.
func (p *Provider) withAccountToken(ctx context.Context, account string) (context.Context, error) {
	account = strings.TrimSpace(account)
	if account == "" || p.accounts == nil {
		return ctx, nil
	}
	token, err := p.accounts.TokenForAccount(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve GitHub account %q: %w", ErrAuthFailed, account, err)
	}
	return WithAccountToken(ctx, token), nil
}

// AuthenticatedIdentityForAccount returns the identity behind a project-bound
// login, caching one result per account for the provider's lifetime.
func (p *Provider) AuthenticatedIdentityForAccount(ctx context.Context, account string) (ports.SCMIdentity, error) {
	account = strings.TrimSpace(account)
	if account == "" {
		return p.AuthenticatedIdentity(ctx)
	}
	p.accountIdentityMu.Lock()
	defer p.accountIdentityMu.Unlock()
	if p.accountIdentities == nil {
		p.accountIdentities = map[string]ports.SCMIdentity{}
	}
	if identity, ok := p.accountIdentities[account]; ok {
		return identity, nil
	}
	scoped, err := p.withAccountToken(ctx, account)
	if err != nil {
		return ports.SCMIdentity{}, err
	}
	identity, err := p.authenticatedIdentity(scoped)
	if err != nil {
		return ports.SCMIdentity{}, err
	}
	p.accountIdentities[account] = identity
	return identity, nil
}
