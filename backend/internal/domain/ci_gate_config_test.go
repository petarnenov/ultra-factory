package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCIGateConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     CIGateConfig
		wantErr string
	}{
		{"missing block", CIGateConfig{}, ""},
		{"off and ready", CIGateConfig{Mode: CIGateOff, WithoutAutoReview: CIGateWithoutReady}, ""},
		{"review and manual", CIGateConfig{Mode: CIGateReview, WithoutAutoReview: CIGateWithoutManual}, ""},
		{"unknown mode", CIGateConfig{Mode: "always"}, `ciGate.mode: "always"`},
		{"unknown withoutAutoReview", CIGateConfig{WithoutAutoReview: "skip"}, `ciGate.withoutAutoReview: "skip"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ProjectConfig{CIGate: tc.cfg}.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestCIGateConfigDefaultsWhenBlockMissing(t *testing.T) {
	var cfg ProjectConfig
	if err := json.Unmarshal([]byte(`{"autoReview":true}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.CIGate.Active(true) || cfg.CIGate.Active(false) {
		t.Fatalf("a config without ciGate must behave as {off, ready}: %+v", cfg.CIGate)
	}
}

// All combinations of mode x autoReview x withoutAutoReview. An empty
// withoutAutoReview is the default "ready".
func TestCIGateConfigActive(t *testing.T) {
	cases := []struct {
		mode       CIGateMode
		autoReview bool
		without    CIGateWithoutAutoReview
		want       bool
	}{
		{CIGateOff, true, CIGateWithoutReady, false},
		{CIGateOff, true, CIGateWithoutManual, false},
		{CIGateOff, false, CIGateWithoutReady, false},
		{CIGateOff, false, CIGateWithoutManual, false},
		{CIGateReview, true, CIGateWithoutReady, true},
		{CIGateReview, true, CIGateWithoutManual, true},
		{CIGateReview, false, CIGateWithoutReady, false},
		{CIGateReview, false, CIGateWithoutManual, true},
		{CIGateReview, false, "", false},
		{"", true, "", false},
	}
	for _, tc := range cases {
		name := string(tc.mode) + "/" + string(tc.without)
		if tc.autoReview {
			name += "/autoReview"
		}
		t.Run(name, func(t *testing.T) {
			got := CIGateConfig{Mode: tc.mode, WithoutAutoReview: tc.without}.Active(tc.autoReview)
			if got != tc.want {
				t.Fatalf("Active(%v) = %v, want %v", tc.autoReview, got, tc.want)
			}
		})
	}
}
