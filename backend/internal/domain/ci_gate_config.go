package domain

import "fmt"

// CIGateMode selects whether expensive PR CI waits for AO review.
type CIGateMode string

const (
	// CIGateOff leaves PR draft state alone. It is the default.
	CIGateOff CIGateMode = "off"
	// CIGateReview opens PRs as drafts until AO review approves the head.
	CIGateReview CIGateMode = "review"
)

// CIGateWithoutAutoReview says what a session without auto-review does when
// the gate mode is review.
type CIGateWithoutAutoReview string

const (
	// CIGateWithoutReady leaves the gate inactive: a normal PR, as today.
	CIGateWithoutReady CIGateWithoutAutoReview = "ready"
	// CIGateWithoutManual keeps the PR draft until a manual approval or
	// `ao pr ready`.
	CIGateWithoutManual CIGateWithoutAutoReview = "manual"
)

// CIGateConfig is the project-level CI gate setting. The zero value is
// {mode: off, withoutAutoReview: ready}.
type CIGateConfig struct {
	Mode              CIGateMode              `json:"mode,omitempty" enum:"off,review"`
	WithoutAutoReview CIGateWithoutAutoReview `json:"withoutAutoReview,omitempty" enum:"ready,manual"`
}

// Validate rejects values outside the two vocabularies; empty means default.
func (c CIGateConfig) Validate() error {
	switch c.Mode {
	case "", CIGateOff, CIGateReview:
	default:
		return fmt.Errorf("ciGate.mode: %q is not one of \"off\", \"review\"", c.Mode)
	}
	switch c.WithoutAutoReview {
	case "", CIGateWithoutReady, CIGateWithoutManual:
	default:
		return fmt.Errorf("ciGate.withoutAutoReview: %q is not one of \"ready\", \"manual\"", c.WithoutAutoReview)
	}
	return nil
}

// Active resolves whether the gate applies to a session spawned with the
// given auto-review value.
func (c CIGateConfig) Active(autoReview bool) bool {
	if c.Mode != CIGateReview {
		return false
	}
	return autoReview || c.WithoutAutoReview == CIGateWithoutManual
}
