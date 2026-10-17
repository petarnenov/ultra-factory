package domain

import "testing"

func TestParseRepositoryIdentityNormalizedTransportAndNamespace(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want RepositoryIdentity
	}{
		{"git@GitHub.com:Owner/Repo.git", RepositoryIdentity{"github", "github.com", "Owner", "Repo"}},
		{"https://GITLAB.example:65535/group/subgroup/repo.git/", RepositoryIdentity{"gitlab", "gitlab.example:65535", "group/subgroup", "repo"}},
		{"ssh://git@gitlab.example:1/team/repo", RepositoryIdentity{"gitlab", "gitlab.example:1", "team", "repo"}},
		{"http://[::1]:8080/team/repo", RepositoryIdentity{"gitlab", "[::1]:8080", "team", "repo"}},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := ParseRepositoryIdentity(tc.raw)
			if err != nil || got != tc.want {
				t.Fatalf("parse = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestParseRepositoryIdentityRejectsMalformedRemotes(t *testing.T) {
	for _, raw := range []string{
		"", " https://github.com/team/repo", "https://github.com/team/repo ",
		"git@github.com/team/repo", "https://github.com/%zz/repo", "file:///team/repo", "https:///team/repo",
		"https://github.com:/team/repo", "https://github.com:0/team/repo", "https://github.com:65536/team/repo", "https://github.com:abc/team/repo",
		"https://github.com/team", "https://github.com/team/.git", "https://gitlab.com/team//repo", "https://gitlab.com/team/./repo", "https://gitlab.com/team/../repo",
		"https://github.com/team/repo?", "https://github.com/team/repo#section", "https://github.com/team/repo?key=value",
		"https://gitlab.com/team/re%2Fpo", "https://gitlab.com/team/re\\po", "https://gitlab.com/team/re po",
		"https://github.com/team/repo/pull/1", "https://gitlab.com/team/repo/-/merge_requests/1",
	} {
		t.Run(raw, func(t *testing.T) {
			got, err := ParseRepositoryIdentity(raw)
			if err == nil || got != (RepositoryIdentity{}) {
				t.Fatalf("invalid remote returned %+v, %v", got, err)
			}
		})
	}
}

func TestRepositoryProviderHostBoundaries(t *testing.T) {
	for _, tc := range []struct{ host, want string }{
		{"GITHUB.COM:443", "github"}, {"api.github.com", "github"}, {"www.github.com", "github"}, {"enterprise.github.com", "github"}, {"tenant.ghe.io", "github"},
		{"github.com.attacker.example", "gitlab"}, {"notgithub.com", "gitlab"}, {"ghe.io", "gitlab"}, {"gitlab.example:8443", "gitlab"}, {"[::1]:8443", "gitlab"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			if got := RepositoryProvider(tc.host); got != tc.want {
				t.Fatalf("provider = %q, want %q", got, tc.want)
			}
		})
	}
}
