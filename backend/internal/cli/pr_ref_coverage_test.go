package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCLI_PRRefCoverageIsNumericPRRef(t *testing.T) {
	cases := []struct {
		ref  string
		want bool
	}{
		{"42", true},
		{"#42", true},
		{"  #7  ", true},
		{"0", false},
		{"-1", false},
		{"abc", false},
		{"1.0", false},
	}
	for _, tc := range cases {
		if got := isNumericPRRef(tc.ref); got != tc.want {
			t.Fatalf("isNumericPRRef(%q) = %v, want %v", tc.ref, got, tc.want)
		}
	}
}

func TestCLI_PRRefCoverageResolveEmpty(t *testing.T) {
	c := &commandContext{}
	if _, err := c.resolvePRRef(context.Background(), "   ", projectDetails{}); err == nil || !strings.Contains(err.Error(), "PR reference must be a PR/MR URL or a number") {
		t.Fatalf("empty ref err = %v", err)
	}
}

func TestCLI_PRRefCoverageResolveNumeric(t *testing.T) {
	ctx := context.Background()

	t.Run("repo from project", func(t *testing.T) {
		c := &commandContext{}
		got, err := c.resolvePRRef(ctx, "42", projectDetails{Repo: "https://github.com/owner/repo"})
		if err != nil || got != "https://github.com/owner/repo/pull/42" {
			t.Fatalf("got %q err %v", got, err)
		}
	})

	t.Run("canonical repo wins", func(t *testing.T) {
		c := &commandContext{}
		project := projectDetails{Repo: "https://github.com/owner/repo", Config: &projectConfig{CanonicalRepoURL: "https://gitlab.com/group/sub/repo.git"}}
		got, err := c.resolvePRRef(ctx, "#7", project)
		if err != nil || got != "https://gitlab.com/group/sub/repo/-/merge_requests/7" {
			t.Fatalf("got %q err %v", got, err)
		}
	})

	t.Run("remote without repo", func(t *testing.T) {
		c := &commandContext{hostSelector: "build"}
		_, err := c.resolvePRRef(ctx, "7", projectDetails{})
		if err == nil || !strings.Contains(err.Error(), "remote project has no recorded repository") || ExitCode(err) != 2 {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("gh lookup success", func(t *testing.T) {
		c := &commandContext{deps: Deps{CommandOutputInDir: func(context.Context, string, string, ...string) ([]byte, error) {
			return []byte("https://github.com/owner/repo\n"), nil
		}}}
		got, err := c.resolvePRRef(ctx, "3", projectDetails{Path: "/repo"})
		if err != nil || got != "https://github.com/owner/repo/pull/3" {
			t.Fatalf("got %q err %v", got, err)
		}
	})

	t.Run("gh lookup failure", func(t *testing.T) {
		c := &commandContext{deps: Deps{CommandOutputInDir: func(context.Context, string, string, ...string) ([]byte, error) {
			return nil, errors.New("gh missing")
		}}}
		_, err := c.resolvePRRef(ctx, "3", projectDetails{Path: "/repo"})
		if err == nil || !strings.Contains(err.Error(), "gh not available") || ExitCode(err) != 2 {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("gh lookup empty", func(t *testing.T) {
		c := &commandContext{deps: Deps{CommandOutputInDir: func(context.Context, string, string, ...string) ([]byte, error) {
			return []byte("   "), nil
		}}}
		_, err := c.resolvePRRef(ctx, "3", projectDetails{Path: "/repo"})
		if err == nil || !strings.Contains(err.Error(), "gh not available") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("invalid repo URL", func(t *testing.T) {
		c := &commandContext{}
		_, err := c.resolvePRRef(ctx, "3", projectDetails{Repo: "not a url"})
		if err == nil || !strings.Contains(err.Error(), "PR reference must be a PR/MR URL or a number") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("ssh repo origin", func(t *testing.T) {
		c := &commandContext{}
		got, err := c.resolvePRRef(ctx, "9", projectDetails{Repo: "git@gitlab.com:group/sub/repo.git"})
		if err != nil || got != "https://gitlab.com/group/sub/repo/-/merge_requests/9" {
			t.Fatalf("got %q err %v", got, err)
		}
	})
}

func TestCLI_PRRefCoverageResolveURL(t *testing.T) {
	c := &commandContext{}
	cases := []struct {
		ref  string
		want string
		bad  bool
	}{
		{"https://github.com/owner/repo/pull/42", "https://github.com/owner/repo/pull/42", false},
		{"https://gitlab.com/group/sub/repo/-/merge_requests/5", "https://gitlab.com/group/sub/repo/-/merge_requests/5", false},
		{"http://github.com/owner/repo/pull/1", "", true},
		{"https://github.com/owner/repo/pull/abc", "", true},
		{"https://github.com/owner/repo/pull/0", "", true},
		{"not a url", "", true},
	}
	for _, tc := range cases {
		got, err := c.resolvePRRef(context.Background(), tc.ref, projectDetails{})
		if tc.bad {
			if err == nil || !strings.Contains(err.Error(), "PR reference must be a PR/MR URL or a number") {
				t.Fatalf("ref %q: err = %v, want usage error", tc.ref, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("ref %q: got %q err %v, want %q", tc.ref, got, err, tc.want)
		}
	}
}

func TestCLI_PRRefCoverageParsePRURL(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"user info", "https://user:pass@github.com/owner/repo/pull/1", "not https"},
		{"empty host", "https:///owner/repo/pull/1", "not https"},
		{"http scheme", "http://github.com/owner/repo/pull/1", "not https"},
		{"gitlab bad number", "https://gitlab.com/owner/repo/-/merge_requests/abc", "bad number"},
		{"gitlab path too short", "https://gitlab.com/-/merge_requests/1", "not a PR/MR URL"},
		{"not pr url", "https://gitlab.com/owner/repo/issues/1", "not a PR/MR URL"},
		{"github extra parts", "https://github.com/owner/repo/pull/1/extra", "not a PR/MR URL"},
		{"github wrong marker", "https://github.com/owner/repo/merge_requests/1", "not a PR/MR URL"},
		{"valid github", "https://github.com/owner/repo/pull/1", ""},
	}
	for _, tc := range cases {
		_, _, _, _, err := cliParsePRURL(tc.raw)
		if tc.wantErr == "" {
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("%s: err = %v, want %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestCLI_PRRefCoverageRepoFromURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		bad  bool
	}{
		{"empty", "", true},
		{"ssh no colon", "git@gitlab.comgroup/repo", true},
		{"ssh too few", "git@gitlab.com:repo", true},
		{"ssh empty segment", "git@gitlab.com:owner//repo", true},
		{"https too few", "https://github.com/repo", true},
		{"https empty segment", "https://github.com/owner//repo", true},
	}
	for _, tc := range cases {
		_, _, _, err := cliRepoFromURL(tc.raw)
		if tc.bad && err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
		if !tc.bad && err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
	}
}

func TestCLI_PRRefCoverageCLIPRURLFromParts(t *testing.T) {
	if got := cliPRURLFromParts("github.com", "owner", "repo", 5); got != "https://github.com/owner/repo/pull/5" {
		t.Fatalf("github URL = %q", got)
	}
	if got := cliPRURLFromParts("gitlab.com", "group/sub", "repo", 5); got != "https://gitlab.com/group/sub/repo/-/merge_requests/5" {
		t.Fatalf("gitlab URL = %q", got)
	}
}

func TestCLI_PRRefCoverageGitHubHost(t *testing.T) {
	if !isCLIGitHubHost("GitHub.com:443") {
		t.Fatal("host with port not recognized")
	}
	if isCLIGitHubHost("gitlab.com") {
		t.Fatal("gitlab recognized as github")
	}
	if !isCLIGitHubHost("foo.GITHUB.com") {
		t.Fatal("subdomain case not recognized")
	}
}
