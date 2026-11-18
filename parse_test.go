package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseLink(t *testing.T) {
	const ep = "https://gitlab.example.com"
	cases := []struct {
		name string
		ep   string
		in   string
		kind linkKind
		proj string
		n    int
		err  string
	}{
		{name: "mr", in: ep + "/team/app/-/merge_requests/42", kind: linkMergeRequest, proj: "team/app", n: 42},
		{name: "mr nested diffs", in: ep + "/a/b/c/-/merge_requests/3/diffs?x=1", kind: linkMergeRequest, proj: "a/b/c", n: 3},
		{name: "mr legacy", in: ep + "/team/app/merge_requests/8", kind: linkMergeRequest, proj: "team/app", n: 8},
		{name: "pipeline", in: ep + "/team/app/-/pipelines/900", kind: linkPipeline, proj: "team/app", n: 900},
		{name: "pipeline builds", in: ep + "/team/app/-/pipelines/900/builds", kind: linkPipeline, proj: "team/app", n: 900},
		{name: "pipeline legacy slash", in: ep + "/team/app/pipelines/7/", kind: linkPipeline, proj: "team/app", n: 7},
		{name: "project", in: ep + "/team/app", kind: linkProject, proj: "team/app"},
		{name: "project git suffix", in: ep + "/team/app.git", kind: linkProject, proj: "team/app"},
		{name: "relative", in: "team/app/-/merge_requests/5", kind: linkMergeRequest, proj: "team/app", n: 5},
		{name: "wrapped", in: "<" + ep + "/team/app/-/merge_requests/5>", kind: linkMergeRequest, proj: "team/app", n: 5},
		{name: "host case", in: "https://GitLab.Example.com/team/app/-/merge_requests/2", kind: linkMergeRequest, proj: "team/app", n: 2},
		{name: "explicit 443", in: "https://gitlab.example.com:443/team/app/-/merge_requests/2", kind: linkMergeRequest, proj: "team/app", n: 2},
		{name: "encoded space", in: ep + "/team/my%20app/-/merge_requests/4", kind: linkMergeRequest, proj: "team/my app", n: 4},
		{name: "subpath", ep: "https://git.example.com/gitlab", in: "https://git.example.com/gitlab/team/app/-/pipelines/3", kind: linkPipeline, proj: "team/app", n: 3},
		{name: "other host", in: "https://gitlab.com/team/app/-/merge_requests/1", err: "gitlab.com"},
		{name: "other port", in: "https://gitlab.example.com:8443/team/app/-/merge_requests/1", err: "8443"},
		{name: "job", in: ep + "/team/app/-/jobs/4", err: "job"},
		{name: "empty", in: "   ", err: "trống"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := tc.ep
			if endpoint == "" {
				endpoint = ep
			}
			got, err := parseLink(endpoint, tc.in)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("error %v, want substring %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != tc.kind || got.Project != tc.proj || got.Number != tc.n {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestParseRemote(t *testing.T) {
	cases := []struct {
		remote   string
		endpoint string
		project  string
		ok       bool
	}{
		{"git@gitlab.example.com:team/sub/app.git", "https://gitlab.example.com", "team/sub/app", true},
		{"https://gitlab.example.com/team/app.git", "https://gitlab.example.com", "team/app", true},
		{"https://oauth2:token@gitlab.example.com/team/app.git", "https://gitlab.example.com", "team/app", true},
		{"ssh://git@gitlab.example.com:2222/team/app.git", "https://gitlab.example.com", "team/app", true},
		{"git@gitlab.example.com:team/app.git", "https://gitlab.example.com:8443", "team/app", true},
		{"https://git.example.com/gitlab/team/app.git", "https://git.example.com/gitlab", "team/app", true},
		{"https://gitlab.example.com:8443/team/app.git", "https://gitlab.example.com:8443", "team/app", true},
		{"git@other.example.com:team/app.git", "https://gitlab.example.com", "", false},
		{"https://gitlab.example.com:8443/team/app.git", "https://gitlab.example.com", "", false},
		{"http://gitlab.example.com/team/app.git", "https://gitlab.example.com", "", false},
	}
	for _, tc := range cases {
		got, ok := parseRemote(tc.remote, tc.endpoint)
		if ok != tc.ok || got != tc.project {
			t.Errorf("remote %s endpoint %s: got %q %v, want %q %v", tc.remote, tc.endpoint, got, ok, tc.project, tc.ok)
		}
	}
}

func TestParseProject(t *testing.T) {
	got, err := parseProject("https://gitlab.example.com", "team/app")
	if err != nil || got != "team/app" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = parseProject("https://gitlab.example.com", "git@gitlab.example.com:team/app.git")
	if err != nil || got != "team/app" {
		t.Fatalf("remote got %q %v", got, err)
	}
	if _, err := parseProject("https://gitlab.example.com", "git@other.example.com:team/app.git"); err == nil {
		t.Fatal("expected host mismatch")
	}
}

func TestValidateRefName(t *testing.T) {
	if err := validateRefName("v1.2.3", "tên tag"); err != nil {
		t.Fatal(err)
	}
	if err := validateRefName("feature/login", "nhánh hoặc tag nguồn"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", " ", "v1..2", "rel*", "tag.lock", "/v1", "v1/", "a b", "a@{1}"} {
		if err := validateRefName(bad, "tên tag"); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestDetectGitProject(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=glops",
			"GIT_AUTHOR_EMAIL=glops@example.com",
			"GIT_COMMITTER_NAME=glops",
			"GIT_COMMITTER_EMAIL=glops@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), out)
		}
	}
	git("init")
	git("remote", "add", "other", "git@gitlab.example.com:other/app.git")
	git("remote", "add", "origin", "git@gitlab.example.com:team/app.git")
	got, err := detectGitProjectIn("https://gitlab.example.com", dir)
	if err != nil || got != "team/app" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := detectGitProjectIn("https://example.com", dir); err == nil {
		t.Fatal("expected no matching remote")
	}
	if _, err := detectGitProjectIn("https://gitlab.example.com", t.TempDir()); err == nil {
		t.Fatal("expected error outside a repository")
	}
}
