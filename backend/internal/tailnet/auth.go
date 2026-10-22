package tailnet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

// ErrNotTailnet means the source address is not a tailnet address, so
// tailscaled cannot vouch for it.
var ErrNotTailnet = errors.New("source is not a Tailscale address")

// ErrForbidden means tailscaled identified the caller and the policy rejects
// it: another Tailscale user, or a tagged device, that is not allowlisted.
var ErrForbidden = errors.New("tailscale identity is not allowed on this host")

// Policy is the optional allowlist beyond "same Tailscale user as this
// machine". Logins and tags compare case-insensitively.
type Policy struct {
	AllowUsers []string
	AllowTags  []string
}

// selfLoginTTL bounds how long the cached own-login survives. Login changes
// are rare; the cache keeps WhoIs from costing two CLI calls per request.
const selfLoginTTL = time.Minute

// Authorizer decides whether a tailnet caller may use the remote listener.
// It fails closed: no tailscaled answer means no access.
type Authorizer struct {
	run    Runner
	policy Policy
	now    func() time.Time

	mu        sync.Mutex
	selfLogin string
	selfAt    time.Time
}

// NewAuthorizer builds an Authorizer over run with the given allowlist.
func NewAuthorizer(run Runner, policy Policy) *Authorizer {
	return &Authorizer{run: run, policy: policy, now: time.Now}
}

// Authorize checks the caller at srcIP and returns its identity on success.
// A failure wraps ErrNotTailnet, ErrUnavailable, or ErrForbidden.
func (a *Authorizer) Authorize(ctx context.Context, srcIP string) (Identity, error) {
	if !remotehost.IsTailnetHost(srcIP) {
		return Identity{}, ErrNotTailnet
	}
	id, err := WhoIs(ctx, a.run, srcIP)
	if err != nil {
		return Identity{}, err
	}
	if len(id.Tags) > 0 {
		for _, tag := range id.Tags {
			if containsFold(a.policy.AllowTags, tag) {
				return id, nil
			}
		}
		return id, fmt.Errorf("%w: tagged device %s (%s)", ErrForbidden, id.NodeName, strings.Join(id.Tags, ","))
	}
	if id.LoginName == "" {
		return id, fmt.Errorf("%w: node %s has no login", ErrForbidden, id.NodeName)
	}
	if containsFold(a.policy.AllowUsers, id.LoginName) {
		return id, nil
	}
	self, err := a.self(ctx)
	if err != nil {
		return id, err
	}
	if strings.EqualFold(self, id.LoginName) {
		return id, nil
	}
	return id, fmt.Errorf("%w: %s belongs to %s", ErrForbidden, id.NodeName, id.LoginName)
}

// self returns this machine's Tailscale login, cached for selfLoginTTL.
func (a *Authorizer) self(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.selfLogin != "" && a.now().Sub(a.selfAt) < selfLoginTTL {
		return a.selfLogin, nil
	}
	st, err := ReadStatus(ctx, a.run)
	if err != nil {
		return "", err
	}
	if st.SelfLogin == "" {
		return "", fmt.Errorf("%w: this machine's Tailscale login is unknown", ErrUnavailable)
	}
	a.selfLogin, a.selfAt = st.SelfLogin, a.now()
	return a.selfLogin, nil
}

func containsFold(list []string, value string) bool {
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return true
		}
	}
	return false
}
