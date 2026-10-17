package domain

import "testing"

func TestActivityStateUnknownDoesNotImplyUserPause(t *testing.T) {
	for _, state := range []ActivityState{"", "future_state"} {
		t.Run(string(state), func(t *testing.T) {
			if state.IsSticky() {
				t.Errorf("unknown activity %q must not become sticky", state)
			}
			if state.NeedsInput() {
				t.Errorf("unknown activity %q must not imply the user can unblock it", state)
			}
		})
	}
}
