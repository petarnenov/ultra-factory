package cli

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseAgentSelection(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want agentSelection
	}{
		{"inherit", agentSelection{Mode: "inherit"}},
		{"fixed:a", agentSelection{Mode: "fixed", ProfileID: "a"}},
		{"random", agentSelection{Mode: "random"}},
		{"random:a, b", agentSelection{Mode: "random", ProfileIDs: []string{"a", "b"}}},
	} {
		got, err := parseAgentSelection("--x", tc.in)
		if err != nil || !reflect.DeepEqual(*got, tc.want) {
			t.Errorf("%q: got %+v, %v", tc.in, got, err)
		}
	}
	for _, bad := range []string{"", "fixed", "fixed:", "fixed:a,b", "inherit:a", "random:a,", "shuffle"} {
		_, err := parseAgentSelection("--x", bad)
		var usage usageError
		if !errors.As(err, &usage) {
			t.Errorf("%q: want a usage error, got %v", bad, err)
		}
	}
}

func TestParseAgentProfile(t *testing.T) {
	got, err := parseAgentProfile("id=a,harness=claude-code,model=sonnet,effort=medium,weight=3,label=Fast")
	want := agentProfile{ID: "a", Label: "Fast", Harness: "claude-code", Model: "sonnet", Effort: "medium", Weight: 3}
	if err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, bad := range []string{"id=a,harness=codex,model=m", "id=a,harness=codex,model=m,effort=e,weight=0", "id=a,harness=codex,model=m,effort=e,x=1", "junk"} {
		if _, err := parseAgentProfile(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}
