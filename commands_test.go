package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func testApp(t *testing.T, in string) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	app := &App{
		In:        strings.NewReader(in),
		Out:       out,
		Err:       errOut,
		ConfigDir: t.TempDir(),
		Sleep:     func() {},
		termSet:   true,
		StdinTerm: false,
	}
	return app, out, errOut
}

func saveTestConfig(t *testing.T, app *App, endpoint, token string) {
	t.Helper()
	if _, err := app.saveConfig(Config{
		Endpoint: endpoint,
		Username: "hieu",
		Token:    token,
	}); err != nil {
		t.Fatal(err)
	}
}

type fakeGitLab struct {
	mu          sync.Mutex
	Token       string
	User        User
	ProjectPath string
	Project     Project
	MR          MergeRequest
	Tag         Tag
	Pipelines   []Pipeline
	Retry       Pipeline
	MergeStatus int
	MergeError  string
	TagStatus   int
	TagError    string
	RetryStatus int
	RetryError  string
	Calls       []string
	MergeBody   map[string]any
	TagBody     map[string]any
}

func (f *fakeGitLab) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("PRIVATE-TOKEN") != f.Token {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "401 Unauthorized"})
		return
	}
	f.mu.Lock()
	f.Calls = append(f.Calls, r.Method+" "+r.URL.RequestURI())
	f.mu.Unlock()

	path := r.URL.EscapedPath()
	if path == "/api/v4/user" {
		writeJSON(w, http.StatusOK, f.User)
		return
	}
	const marker = "/api/v4/projects/"
	if !strings.HasPrefix(path, marker) {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(path, marker)
	projEnc, tail, _ := strings.Cut(rest, "/")
	project, err := url.PathUnescape(projEnc)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if f.ProjectPath != "" && project != f.ProjectPath {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "404 Project Not Found"})
		return
	}

	switch {
	case r.Method == http.MethodGet && tail == "":
		writeJSON(w, http.StatusOK, f.Project)
	case r.Method == http.MethodGet && tail == "pipelines":
		writeJSON(w, http.StatusOK, f.Pipelines)
	case r.Method == http.MethodPost && tail == "repository/tags":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "bad json"})
			return
		}
		f.mu.Lock()
		f.TagBody = body
		f.mu.Unlock()
		if f.TagStatus != 0 {
			msg := f.TagError
			if msg == "" {
				msg = "tag failed"
			}
			writeJSON(w, f.TagStatus, map[string]string{"message": msg})
			return
		}
		writeJSON(w, http.StatusCreated, f.Tag)
	default:
		if id, extra, ok := splitAction(tail, "merge_requests/"); ok && id == f.MR.IID {
			if r.Method == http.MethodGet && extra == "" {
				writeJSON(w, http.StatusOK, f.MR)
				return
			}
			if r.Method == http.MethodPost && extra == "merge" {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"message": "bad json"})
					return
				}
				f.mu.Lock()
				f.MergeBody = body
				f.mu.Unlock()
				if f.MergeStatus != 0 {
					msg := f.MergeError
					if msg == "" {
						msg = "cannot merge"
					}
					writeJSON(w, f.MergeStatus, map[string]string{"message": msg})
					return
				}
				mr := f.MR
				if body["merge_when_pipeline_succeeds"] == true {
					mr.State = "opened"
					mr.MergeWhenPipelineSucceeds = true
				} else {
					mr.State = "merged"
				}
				writeJSON(w, http.StatusOK, mr)
				return
			}
		}
		if _, extra, ok := splitAction(tail, "pipelines/"); ok && r.Method == http.MethodPost && extra == "retry" {
			if f.RetryStatus != 0 {
				msg := f.RetryError
				if msg == "" {
					msg = "cannot retry"
				}
				writeJSON(w, f.RetryStatus, map[string]string{"message": msg})
				return
			}
			writeJSON(w, http.StatusCreated, f.Retry)
			return
		}
		http.NotFound(w, r)
	}
}

func (f *fakeGitLab) called(part string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.Calls {
		if strings.Contains(call, part) {
			return true
		}
	}
	return false
}

func splitAction(tail, prefix string) (int, string, bool) {
	if !strings.HasPrefix(tail, prefix) {
		return 0, "", false
	}
	rest := strings.TrimPrefix(tail, prefix)
	idPart, extra, _ := strings.Cut(rest, "/")
	id, err := strconv.Atoi(idPart)
	if err != nil || id <= 0 {
		return 0, "", false
	}
	return id, extra, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func startGitLab(t *testing.T, f *fakeGitLab) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv
}

func mergeableMR() MergeRequest {
	return MergeRequest{
		IID:                 42,
		Title:               "Thêm trang đăng nhập",
		State:               "opened",
		MergeStatus:         "can_be_merged",
		DetailedMergeStatus: "mergeable",
		SHA:                 "abc123def456",
		SourceBranch:        "feature/login",
		TargetBranch:        "main",
		WebURL:              "https://gitlab.example.com/team/app/-/merge_requests/42",
		User:                &mergeUser{CanMerge: true},
	}
}

func TestSetupWhoamiAndConfig(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{Token: token, User: User{ID: 7, Username: "hieu", Name: "Trung Hieu"}}
	srv := startGitLab(t, fake)
	app, out, errOut := testApp(t, "")

	if err := app.Run([]string{"setup", "--endpoint", srv.URL, "--username", "hieu", "--token", token, "--insecure"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	if !strings.Contains(out.String(), "Đã lưu cấu hình") || strings.Contains(out.String()+errOut.String(), token) {
		t.Fatalf("stdout %s\nstderr %s", out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "HTTP") {
		t.Fatalf("stderr %s", errOut.String())
	}
	cfg, err := app.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != token || cfg.Username != "hieu" || !cfg.InsecureSkipVerify || cfg.Endpoint != srv.URL {
		t.Fatalf("%+v", cfg)
	}

	out.Reset()
	errOut.Reset()
	if err := app.Run([]string{"setup", "--endpoint", srv.URL, "--secure"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	cfg, err = app.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InsecureSkipVerify || cfg.Token != token {
		t.Fatalf("%+v", cfg)
	}

	out.Reset()
	if err := app.Run([]string{"whoami"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Trung Hieu") || strings.Contains(out.String(), token) {
		t.Fatal(out.String())
	}
	out.Reset()
	if err := app.Run([]string{"config"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "****1234") || strings.Contains(out.String(), token) {
		t.Fatal(out.String())
	}
}

func TestSetupRejectsMismatchedUser(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{Token: token, User: User{ID: 7, Username: "hieu", Name: "Hieu"}}
	srv := startGitLab(t, fake)
	app, _, _ := testApp(t, "")
	err := app.Run([]string{"setup", "--endpoint", srv.URL, "--username", "other", "--token", token})
	if err == nil || !strings.Contains(err.Error(), "other") {
		t.Fatal(err)
	}
	if _, loadErr := app.loadConfig(); !errors.Is(loadErr, errNoConfig) {
		t.Fatal(loadErr)
	}
}

func TestSetupInteractive(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{Token: token, User: User{ID: 7, Username: "hieu", Name: "Hieu"}}
	srv := startGitLab(t, fake)
	app, out, errOut := testApp(t, srv.URL+"\nhieu\n"+token+"\n")
	app.StdinTerm = true
	if err := app.Run([]string{"setup"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	if !strings.Contains(out.String(), "Đã lưu cấu hình") || !strings.Contains(errOut.String(), "scope api") {
		t.Fatalf("stdout %s\nstderr %s", out.String(), errOut.String())
	}
}

func TestMergeSuccess(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{Token: token, ProjectPath: "team/app", MR: mergeableMR()}
	srv := startGitLab(t, fake)
	app, out, errOut := testApp(t, "")
	saveTestConfig(t, app, srv.URL, token)
	link := srv.URL + "/team/app/-/merge_requests/42"
	if err := app.Run([]string{"merge", link, "--remove-source-branch"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	if !strings.Contains(out.String(), "Đã merge !42") || !strings.Contains(out.String(), "feature/login → main") {
		t.Fatal(out.String())
	}
	fake.mu.Lock()
	body := fake.MergeBody
	fake.mu.Unlock()
	if body["sha"] != "abc123def456" || body["merge_when_pipeline_succeeds"] != false || body["should_remove_source_branch"] != true {
		t.Fatalf("%v", body)
	}
}

func TestMergeWhenPipelineRunning(t *testing.T) {
	const token = "token-abc-1234"
	mr := mergeableMR()
	mr.DetailedMergeStatus = "ci_still_running"
	mr.HeadPipeline = &Pipeline{ID: 7, Status: "running", WebURL: srvPipeline("https://gitlab.example.com")}
	fake := &fakeGitLab{Token: token, ProjectPath: "team/app", MR: mr}
	srv := startGitLab(t, fake)
	app, out, errOut := testApp(t, "")
	saveTestConfig(t, app, srv.URL, token)
	if err := app.Run([]string{"merge", srv.URL + "/team/app/-/merge_requests/42"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	if !strings.Contains(out.String(), "Đã hẹn merge") || !strings.Contains(out.String(), "Pipeline:") {
		t.Fatal(out.String())
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.MergeBody["merge_when_pipeline_succeeds"] != true {
		t.Fatalf("%v", fake.MergeBody)
	}
}

func srvPipeline(base string) string {
	return base + "/team/app/-/pipelines/7"
}

func TestMergeDeniesWithoutPermission(t *testing.T) {
	const token = "token-abc-1234"
	mr := mergeableMR()
	mr.User = &mergeUser{CanMerge: false}
	fake := &fakeGitLab{Token: token, ProjectPath: "team/app", MR: mr}
	srv := startGitLab(t, fake)
	app, _, _ := testApp(t, "")
	saveTestConfig(t, app, srv.URL, token)
	err := app.Run([]string{"merge", srv.URL + "/team/app/-/merge_requests/42"})
	var coded *codedError
	if !errors.As(err, &coded) || coded.code != 3 || !strings.Contains(err.Error(), "không có quyền") {
		t.Fatal(err)
	}
	if fake.called("POST") {
		t.Fatal("merge was called")
	}
}

func TestMergeAPIForbidden(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{Token: token, ProjectPath: "team/app", MR: mergeableMR(), MergeStatus: http.StatusForbidden, MergeError: "403 Forbidden"}
	srv := startGitLab(t, fake)
	app, _, _ := testApp(t, "")
	saveTestConfig(t, app, srv.URL, token)
	err := app.Run([]string{"merge", srv.URL + "/team/app/-/merge_requests/42"})
	var coded *codedError
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatal(err)
	}
}

func TestMergeAlreadyMerged(t *testing.T) {
	const token = "token-abc-1234"
	mr := mergeableMR()
	mr.State = "merged"
	fake := &fakeGitLab{Token: token, ProjectPath: "team/app", MR: mr}
	srv := startGitLab(t, fake)
	app, out, errOut := testApp(t, "")
	saveTestConfig(t, app, srv.URL, token)
	if err := app.Run([]string{"merge", srv.URL + "/team/app/-/merge_requests/42"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	if !strings.Contains(out.String(), "đã được merge") || fake.called("POST ") {
		t.Fatalf("out %s post %v", out.String(), fake.called("POST "))
	}
}

func TestMergeRejectsOtherHost(t *testing.T) {
	app, _, _ := testApp(t, "")
	saveTestConfig(t, app, "https://gitlab.example.com", "token-abc-1234")
	err := app.Run([]string{"merge", "https://gitlab.com/team/app/-/merge_requests/1"})
	if err == nil || !strings.Contains(err.Error(), "gitlab.com") {
		t.Fatal(err)
	}
}

func TestMissingConfig(t *testing.T) {
	app, _, _ := testApp(t, "")
	err := app.Run([]string{"merge", "https://gitlab.example.com/team/app/-/merge_requests/1"})
	if !errors.Is(err, errNoConfig) {
		t.Fatal(err)
	}
}

func TestRetryPipeline(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{
		Token:       token,
		ProjectPath: "team/app",
		Retry: Pipeline{
			ID:     901,
			Status: "pending",
			WebURL: "https://gitlab.example.com/team/app/-/pipelines/901",
		},
	}
	srv := startGitLab(t, fake)
	app, out, errOut := testApp(t, "")
	saveTestConfig(t, app, srv.URL, token)
	if err := app.Run([]string{"retry", srv.URL + "/team/app/-/pipelines/900"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	if !strings.Contains(out.String(), "Đã chạy lại pipeline #901") || !strings.Contains(out.String(), "đang chờ") {
		t.Fatal(out.String())
	}
	if !fake.called("/pipelines/900/retry") {
		t.Fatal("retry was not called")
	}
}

func TestRetryRejectsMergeLink(t *testing.T) {
	app, _, _ := testApp(t, "")
	saveTestConfig(t, app, "https://gitlab.example.com", "token-abc-1234")
	err := app.Run([]string{"retry", "https://gitlab.example.com/team/app/-/merge_requests/4"})
	if err == nil || !strings.Contains(err.Error(), "glops merge") {
		t.Fatal(err)
	}
}

func TestRetryForbidden(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{Token: token, ProjectPath: "team/app", RetryStatus: http.StatusForbidden, RetryError: "no failed jobs"}
	srv := startGitLab(t, fake)
	app, _, _ := testApp(t, "")
	saveTestConfig(t, app, srv.URL, token)
	err := app.Run([]string{"retry", srv.URL + "/team/app/-/pipelines/900"})
	var coded *codedError
	if !errors.As(err, &coded) || coded.code != 3 || !strings.Contains(err.Error(), "no failed jobs") {
		t.Fatal(err)
	}
}

func TestTagCreatesPipeline(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{
		Token:       token,
		ProjectPath: "team/app",
		Project:     Project{PathWithNamespace: "team/app", DefaultBranch: "main"},
		Tag: Tag{
			Name: "v1.2.3",
			Commit: struct {
				ID      string `json:"id"`
				ShortID string `json:"short_id"`
				Title   string `json:"title"`
			}{ID: "abcdef1234567890", ShortID: "abcdef12"},
		},
		Pipelines: []Pipeline{{
			ID:     88,
			Status: "running",
			SHA:    "abcdef1234567890",
			Ref:    "v1.2.3",
			WebURL: "https://gitlab.example.com/team/app/-/pipelines/88",
		}},
	}
	srv := startGitLab(t, fake)
	app, out, errOut := testApp(t, "")
	saveTestConfig(t, app, srv.URL, token)
	projectURL := srv.URL + "/team/app"
	if err := app.Run([]string{"tag", "--name", "v1.2.3", "--ref", "main", "--project", projectURL, "--yes"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	if !strings.Contains(out.String(), "Đã tạo tag v1.2.3 từ main (abcdef12).") || !strings.Contains(out.String(), "Pipeline #88 đang chạy") {
		t.Fatal(out.String())
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.TagBody["tag_name"] != "v1.2.3" || fake.TagBody["ref"] != "main" {
		t.Fatalf("%v", fake.TagBody)
	}
}

func TestTagWithoutPipeline(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{
		Token:       token,
		ProjectPath: "team/app",
		Project:     Project{PathWithNamespace: "team/app", DefaultBranch: "main"},
		Tag: Tag{Name: "v1.2.3", Commit: struct {
			ID      string `json:"id"`
			ShortID string `json:"short_id"`
			Title   string `json:"title"`
		}{ID: "abcdef1234567890", ShortID: "abcdef12"}},
	}
	srv := startGitLab(t, fake)
	app, out, errOut := testApp(t, "")
	saveTestConfig(t, app, srv.URL, token)
	if err := app.Run([]string{"tag", "v1.2.3", "main", "--project", "team/app", "--yes"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	if !strings.Contains(out.String(), "Chưa thấy pipeline") {
		t.Fatal(out.String())
	}
}

func TestTagRequiresYes(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{Token: token, ProjectPath: "team/app", Project: Project{PathWithNamespace: "team/app", DefaultBranch: "main"}}
	srv := startGitLab(t, fake)
	app, _, _ := testApp(t, "")
	saveTestConfig(t, app, srv.URL, token)
	err := app.Run([]string{"tag", "--name", "v1.2.3", "--ref", "main", "--project", "team/app"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatal(err)
	}
	if fake.called("repository/tags") {
		t.Fatal("tag was created")
	}
}

func TestTagForbidden(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{
		Token:       token,
		ProjectPath: "team/app",
		Project:     Project{PathWithNamespace: "team/app", DefaultBranch: "main"},
		TagStatus:   http.StatusForbidden,
		TagError:    "protected tag",
	}
	srv := startGitLab(t, fake)
	app, _, _ := testApp(t, "")
	saveTestConfig(t, app, srv.URL, token)
	err := app.Run([]string{"tag", "--name", "v1.2.3", "--ref", "main", "--project", "team/app", "-y"})
	var coded *codedError
	if !errors.As(err, &coded) || coded.code != 3 || !strings.Contains(err.Error(), "protected tag") {
		t.Fatal(err)
	}
}

func TestTagInteractiveCancel(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{
		Token:       token,
		ProjectPath: "team/app",
		Project:     Project{PathWithNamespace: "team/app", DefaultBranch: "main"},
	}
	srv := startGitLab(t, fake)
	app, out, errOut := testApp(t, "v1.2.3\n\nn\n")
	app.StdinTerm = true
	app.LookRemote = func(string) (string, error) { return "team/app", nil }
	saveTestConfig(t, app, srv.URL, token)
	if err := app.Run([]string{"tag"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	if !strings.Contains(out.String(), "Đã hủy.") || fake.called("repository/tags") {
		t.Fatalf("out %s stderr %s", out.String(), errOut.String())
	}
}

func TestTagInteractiveCreates(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{
		Token:       token,
		ProjectPath: "team/app",
		Project:     Project{PathWithNamespace: "team/app", DefaultBranch: "main"},
		Tag: Tag{Name: "v1.2.3", Commit: struct {
			ID      string `json:"id"`
			ShortID string `json:"short_id"`
			Title   string `json:"title"`
		}{ID: "abcdef1234567890", ShortID: "abcdef12"}},
		Pipelines: []Pipeline{{ID: 5, Status: "pending", SHA: "abcdef1234567890", WebURL: "https://gitlab.example.com/team/app/-/pipelines/5"}},
	}
	srv := startGitLab(t, fake)
	app, out, errOut := testApp(t, "team/app\nv1.2.3\n\ny\n")
	app.StdinTerm = true
	saveTestConfig(t, app, srv.URL, token)
	if err := app.Run([]string{"tag"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	if !strings.Contains(out.String(), "Đã tạo tag v1.2.3 từ main") || !strings.Contains(out.String(), "Pipeline #5") {
		t.Fatalf("stdout %s\nstderr %s", out.String(), errOut.String())
	}
}

func TestHelpVersionAndUnknown(t *testing.T) {
	app, out, _ := testApp(t, "")
	if err := app.Run(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "glops setup") {
		t.Fatal(out.String())
	}
	out.Reset()
	if err := app.Run([]string{"version"}); err != nil || !strings.Contains(out.String(), "glops") {
		t.Fatal(err, out.String())
	}
	err := app.Run([]string{"nope"})
	var coded *codedError
	if !errors.As(err, &coded) || coded.code != 2 {
		t.Fatal(err)
	}
}

func TestTokenStdin(t *testing.T) {
	const token = "token-abc-1234"
	fake := &fakeGitLab{Token: token, User: User{ID: 3, Username: "hieu", Name: "Hieu"}}
	srv := startGitLab(t, fake)
	app, out, errOut := testApp(t, token+"\n")
	if err := app.Run([]string{"setup", "--endpoint", srv.URL, "--username", "Hieu", "--token-stdin"}); err != nil {
		t.Fatal(err, errOut.String())
	}
	cfg, err := app.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Username != "hieu" || cfg.Token != token {
		t.Fatalf("%+v", cfg)
	}
	if !strings.Contains(errOut.String(), "Username trên GitLab là hieu") {
		t.Fatal(errOut.String(), out.String())
	}
}

func TestMergeLinkOfPipeline(t *testing.T) {
	app, _, _ := testApp(t, "")
	saveTestConfig(t, app, "https://gitlab.example.com", "token-abc-1234")
	err := app.Run([]string{"merge", "https://gitlab.example.com/team/app/-/pipelines/3"})
	if err == nil || !strings.Contains(err.Error(), "glops retry") {
		t.Fatal(err)
	}
}
