package domain

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountIdentityMergePreservesPrimaryAndFillsMissing(t *testing.T) {
	fallback := AgentAccountIdentity{Email: "fallback@example.com", Name: "Fallback", Organization: "Team", Plan: "Pro", Provider: "provider", Method: AgentAccountMethodAPIKey, SourceKind: AgentCredentialSourceEnvironment, Source: "API_KEY"}
	if got := (AgentAccountIdentity{}).Merge(fallback); got != fallback {
		t.Fatalf("empty merge = %+v, want %+v", got, fallback)
	}
	primary := AgentAccountIdentity{Email: "primary@example.com", Name: "Primary", Organization: "Org", Plan: "Basic", Provider: "other", Method: AgentAccountMethodAccount, SourceKind: AgentCredentialSourceCommand, Source: "agent auth status"}
	if got := primary.Merge(fallback); got != primary {
		t.Fatalf("primary fields overwritten: %+v", got)
	}
	partial := AgentAccountIdentity{Email: primary.Email, SourceKind: AgentCredentialSourceKeychain}
	want := fallback
	want.Email, want.SourceKind, want.Source = primary.Email, AgentCredentialSourceKeychain, ""
	if got := partial.Merge(fallback); got != want {
		t.Fatalf("partial merge = %+v, want %+v", got, want)
	}
	if partial.Name != "" || fallback.Email != "fallback@example.com" {
		t.Fatal("merge mutated its inputs")
	}
}

func TestAccountIdentitySourcesAndMethodCopy(t *testing.T) {
	cases := []struct {
		name      string
		got, want AgentAccountIdentity
	}{
		{"environment", EnvironmentSource("PROVIDER_API_KEY"), AgentAccountIdentity{SourceKind: AgentCredentialSourceEnvironment, Source: "PROVIDER_API_KEY"}},
		{"command arguments", CommandSource(filepath.Join("private", "bin", "agent"), "auth", "status"), AgentAccountIdentity{SourceKind: AgentCredentialSourceCommand, Source: "agent auth status"}},
		{"command alone", CommandSource("agent"), AgentAccountIdentity{SourceKind: AgentCredentialSourceCommand, Source: "agent"}},
		{"keychain", KeychainSource(), AgentAccountIdentity{SourceKind: AgentCredentialSourceKeychain}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("source = %+v, want %+v", tc.got, tc.want)
			}
			want := tc.want
			want.Method = AgentAccountMethodToken
			if got := tc.got.WithMethod(AgentAccountMethodToken); got != want {
				t.Fatalf("WithMethod = %+v, want %+v", got, want)
			}
			if tc.got.Method != "" {
				t.Fatal("WithMethod mutated original")
			}
		})
	}
}

func TestAccountIdentitySanitizationBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"trim", " \tDeveloper\n", "Developer"},
		{"rune limit", strings.Repeat("界", 254), strings.Repeat("界", 254)},
		{"over rune limit", strings.Repeat("界", 255), ""},
		{"invalid UTF8", string([]byte{0xff}), ""},
		{"replacement rune", "bad\ufffdname", ""},
		{"embedded control", "dev\nname", ""},
		{"delete control", "dev\x7fname", ""},
		{"empty", " \t\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := AgentAccountIdentity{Name: tc.value, Organization: tc.value, Plan: tc.value, Provider: tc.value}
			got := input.Sanitized()
			if tc.want == "" {
				if got != nil {
					t.Fatalf("invalid fields survived: %+v", got)
				}
				return
			}
			want := AgentAccountIdentity{Name: tc.want, Organization: tc.want, Plan: tc.want, Provider: tc.want}
			if got == nil || *got != want {
				t.Fatalf("Sanitized = %+v, want %+v", got, want)
			}
			if input.Name != tc.value {
				t.Fatal("sanitization mutated original")
			}
		})
	}
	for _, method := range []AgentAccountMethod{AgentAccountMethodAccount, AgentAccountMethodAPIKey, AgentAccountMethodToken} {
		if got := (AgentAccountIdentity{Method: method}).Sanitized(); got == nil || *got != (AgentAccountIdentity{Method: method}) {
			t.Errorf("valid method %q lost: %+v", method, got)
		}
	}
	for _, kind := range []AgentCredentialSourceKind{AgentCredentialSourceEnvironment, AgentCredentialSourceFile, AgentCredentialSourceCommand} {
		got := (AgentAccountIdentity{SourceKind: kind, Source: " source "}).Sanitized()
		if got == nil || *got != (AgentAccountIdentity{SourceKind: kind, Source: "source"}) {
			t.Errorf("source %q = %+v", kind, got)
		}
		if got := (AgentAccountIdentity{SourceKind: kind, Source: "bad\x00source"}).Sanitized(); got != nil {
			t.Errorf("unsafe source survived: %+v", got)
		}
	}
	if got := (AgentAccountIdentity{SourceKind: "unsupported", Source: "source"}).Sanitized(); got != nil {
		t.Fatalf("unknown source survived: %+v", got)
	}
	if got := (AgentAccountIdentity{SourceKind: AgentCredentialSourceKeychain, Source: "private"}).Sanitized(); got == nil || *got != KeychainSource() {
		t.Fatalf("keychain must omit source details: %+v", got)
	}
}

func TestEffectiveAgentReadinessUnknownAndMissingInstallation(t *testing.T) {
	for _, auth := range []AgentAuthenticationState{AgentAuthenticationAuthorized, AgentAuthenticationUnauthorized, AgentAuthenticationConfigured, AgentAuthenticationNotApplicable, "", "future"} {
		for _, installation := range []AgentInstallationState{"", "future", AgentInstallationUnknown} {
			if got := EffectiveAgentReadiness(installation, auth); got != AgentReadinessUnknown {
				t.Errorf("(%q, %q) = %q, want unknown", installation, auth, got)
			}
		}
		if got := EffectiveAgentReadiness(AgentInstallationNotInstalled, auth); got != AgentReadinessNotReady {
			t.Errorf("missing installation with %q = %q", auth, got)
		}
	}
	for _, auth := range []AgentAuthenticationState{"", "future"} {
		if got := EffectiveAgentReadiness(AgentInstallationInstalled, auth); got != AgentReadinessUnknown {
			t.Errorf("installed with %q = %q", auth, got)
		}
	}
}
