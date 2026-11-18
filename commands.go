package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

const setupHelp = `Cách dùng:
  glops setup [--endpoint URL] [--username USER] [--token TOKEN]
              [--token-stdin] [--insecure | --secure]

Không truyền flag thì lệnh hỏi từng mục trên terminal.
Token cần scope api. --token nằm trong lịch sử shell; --token-stdin đọc từ stdin.

  --endpoint      URL GitLab, kể cả endpoint tự host
  --username      Username GitLab
  --token         Personal access token
  --token-stdin   Đọc token từ stdin
  --insecure      Bỏ qua kiểm tra chứng chỉ TLS
  --secure        Bật lại kiểm tra chứng chỉ TLS
`

const mergeHelp = `Cách dùng:
  glops merge <link-merge-request> [--remove-source-branch]

Ví dụ:
  glops merge https://gitlab.company.com/team/app/-/merge_requests/42

Merge ngay nếu merge request đang merge được và tài khoản có quyền.
Nếu pipeline còn chạy và project bắt pipeline phải pass, lệnh hẹn merge khi pipeline thành công.
`

const tagHelp = `Cách dùng:
  glops tag [--name <tag>] [--ref <nhánh-hoặc-tag>] [--project <project>] [--yes]
  glops tag <tag> <nhánh-hoặc-tag> [--project <project>] [--yes]

Project nhận dạng group/app, URL project, hoặc remote Git trong thư mục hiện tại
nếu host trùng endpoint đã lưu.

Ví dụ:
  glops tag --name v1.2.3 --ref main --yes
  glops tag v1.2.3 release/1.2 --project team/app --yes
`

const retryHelp = `Cách dùng:
  glops retry <link-pipeline>

Ví dụ:
  glops retry https://gitlab.company.com/team/app/-/pipelines/900

GitLab chỉ chạy lại các job thất bại hoặc đã hủy trong pipeline đó.
`

func (a *App) cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	endpointFlag := fs.String("endpoint", "", "URL GitLab")
	usernameFlag := fs.String("username", "", "Username GitLab")
	tokenFlag := fs.String("token", "", "Personal access token")
	tokenStdin := fs.Bool("token-stdin", false, "Đọc token từ stdin")
	insecure := fs.Bool("insecure", false, "Bỏ qua kiểm tra chứng chỉ TLS")
	secure := fs.Bool("secure", false, "Bật lại kiểm tra chứng chỉ TLS")
	fs.Usage = func() { fmt.Fprint(a.Err, setupHelp) }
	if err := flagParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fmt.Fprint(a.Err, setupHelp)
		return code(2, errors.New("setup không nhận thêm tham số"))
	}
	if *insecure && *secure {
		return errors.New("chỉ chọn một trong --insecure và --secure")
	}
	if *tokenStdin && *tokenFlag != "" {
		return errors.New("chỉ dùng một trong --token và --token-stdin")
	}

	existing, err := a.loadOptional()
	if err != nil {
		fmt.Fprintf(a.Err, "Cấu hình cũ không dùng được (%s). Sẽ ghi đè khi setup thành công.\n", err)
		existing = nil
	}

	ask := a.interactive() && !*tokenStdin
	endpoint := strings.TrimSpace(*endpointFlag)
	if endpoint == "" && ask {
		def := "https://gitlab.com"
		if existing != nil && existing.Endpoint != "" {
			def = existing.Endpoint
		}
		endpoint, err = a.prompt("Endpoint GitLab", def)
		if err != nil {
			return err
		}
	}
	if endpoint == "" && existing != nil {
		endpoint = existing.Endpoint
	}
	endpoint, err = normalizeEndpoint(endpoint)
	if err != nil {
		return err
	}

	username := strings.TrimSpace(*usernameFlag)
	if username == "" && ask {
		def := ""
		if existing != nil {
			def = existing.Username
		}
		username, err = a.prompt("Username", def)
		if err != nil {
			return err
		}
	}
	if username == "" && existing != nil {
		username = existing.Username
	}

	token := *tokenFlag
	if *tokenStdin {
		body, err := io.ReadAll(a.reader())
		if err != nil {
			return err
		}
		token = string(body)
	}
	token = cleanToken(token)
	if token == "" && ask {
		fmt.Fprintln(a.Err, "Personal access token cần scope api.")
		hint := "Token: "
		if existing != nil && existing.Token != "" {
			hint = "Token (Enter để giữ token cũ): "
		}
		token, err = a.readSecret(hint)
		if err != nil {
			return err
		}
	}
	if token == "" && existing != nil {
		token = existing.Token
	}
	if token == "" {
		return errors.New("thiếu token. Truyền --token, --token-stdin, hoặc nhập khi chạy setup")
	}
	if strings.ContainsAny(token, " \t\r\n") {
		return errors.New("token không được chứa khoảng trắng")
	}

	skipVerify := false
	if existing != nil {
		skipVerify = existing.InsecureSkipVerify
	}
	if *insecure {
		skipVerify = true
	}
	if *secure {
		skipVerify = false
	}

	if strings.HasPrefix(endpoint, "http://") {
		fmt.Fprintln(a.Err, "Cảnh báo: endpoint dùng HTTP, token sẽ được gửi không mã hóa.")
	}

	user, err := NewClient(endpoint, token, skipVerify).CurrentUser(context.Background())
	if err != nil {
		return friendly(err, "kiểm tra token")
	}
	if username != "" && !strings.EqualFold(username, user.Username) {
		return fmt.Errorf("token thuộc về %q, không phải %q", user.Username, username)
	}
	if username != "" && username != user.Username {
		fmt.Fprintf(a.Err, "Username trên GitLab là %s.\n", user.Username)
	}

	cfg := Config{
		Endpoint:           endpoint,
		Username:           user.Username,
		Token:              token,
		InsecureSkipVerify: skipVerify,
	}
	path, err := a.saveConfig(cfg)
	if err != nil {
		return err
	}
	tlsMode := "bật"
	if skipVerify {
		tlsMode = "tắt"
	}
	fmt.Fprintf(a.Out, "Đã lưu cấu hình cho %s tại %s\nKiểm tra TLS: %s. Chỉ user hiện tại đọc được file này.\n", user.Username, path, tlsMode)
	return nil
}

func (a *App) cmdConfig(args []string) error {
	if len(args) != 0 {
		return code(2, errors.New("cách dùng: glops config"))
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	path, _ := a.configFile()
	tlsMode := "bật"
	if cfg.InsecureSkipVerify {
		tlsMode = "tắt"
	}
	fmt.Fprintf(a.Out, "endpoint: %s\nusername: %s\ntoken: %s\ntls_verify: %s\nfile: %s\n", cfg.Endpoint, cfg.Username, maskToken(cfg.Token), tlsMode, path)
	return nil
}

func (a *App) cmdWhoami(args []string) error {
	if len(args) != 0 {
		return code(2, errors.New("whoami không nhận thêm tham số"))
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	user, err := a.client(cfg).CurrentUser(context.Background())
	if err != nil {
		return friendly(err, "kiểm tra token")
	}
	path, _ := a.configFile()
	fmt.Fprintf(a.Out, "Endpoint: %s\nUsername: %s\nTên: %s\nUser ID: %d\nToken: %s\nFile: %s\n", cfg.Endpoint, user.Username, user.Name, user.ID, maskToken(cfg.Token), path)
	if cfg.Username != "" && !strings.EqualFold(cfg.Username, user.Username) {
		fmt.Fprintf(a.Err, "Cảnh báo: cấu hình ghi username %s nhưng token thuộc về %s.\n", cfg.Username, user.Username)
	}
	return nil
}

func (a *App) cmdMerge(args []string) error {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	removeSource := fs.Bool("remove-source-branch", false, "Xóa nhánh nguồn sau khi merge")
	fs.Usage = func() { fmt.Fprint(a.Err, mergeHelp) }
	if err := flagParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fmt.Fprint(a.Err, mergeHelp)
		return code(2, errors.New("cần đúng một link merge request"))
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	link, err := parseLink(cfg.Endpoint, fs.Arg(0))
	if err != nil {
		return err
	}
	if link.Kind != linkMergeRequest {
		return wrongLink(link.Kind)
	}

	client := a.client(cfg)
	ctx := context.Background()
	mr, err := client.GetMergeRequest(ctx, link.Project, link.Number)
	if err != nil {
		return friendly(err, "đọc merge request")
	}
	decision := planMerge(mr)
	for i := 0; decision.Action == actWait && i < 4; i++ {
		a.sleep()
		mr, err = client.GetMergeRequest(ctx, link.Project, link.Number)
		if err != nil {
			return friendly(err, "đọc merge request")
		}
		decision = planMerge(mr)
	}
	if decision.Action == actWait {
		decision = mergeDecision{Action: actNow}
	}

	web := mergeRequestURL(cfg.Endpoint, link.Project, firstIID(mr, link.Number), mr.WebURL)
	switch decision.Action {
	case actAlready:
		fmt.Fprintf(a.Out, "Merge request !%d đã được merge.\n%s\n", firstIID(mr, link.Number), web)
		return nil
	case actDenyPerm:
		return code(3, errors.New(decision.Message))
	case actDeny:
		return errors.New(decision.Message)
	case actNow, actPipeline:
		updated, err := client.Merge(ctx, link.Project, link.Number, MergeOptions{
			SHA:                  mr.SHA,
			RemoveSourceBranch:   *removeSource,
			WhenPipelineSucceeds: decision.Action == actPipeline,
		})
		if err != nil {
			return friendly(err, "merge")
		}
		title := firstNonEmpty(updated.Title, mr.Title)
		source := firstNonEmpty(updated.SourceBranch, mr.SourceBranch)
		target := firstNonEmpty(updated.TargetBranch, mr.TargetBranch)
		iid := firstIID(updated, firstIID(mr, link.Number))
		web = mergeRequestURL(cfg.Endpoint, link.Project, iid, firstNonEmpty(updated.WebURL, mr.WebURL))
		if decision.Action == actPipeline {
			fmt.Fprintf(a.Out, "Đã hẹn merge !%d — %s\n%s → %s khi pipeline thành công.\n%s\n", iid, title, source, target, web)
			if mr.HeadPipeline != nil && mr.HeadPipeline.WebURL != "" {
				fmt.Fprintf(a.Out, "Pipeline: %s\n", mr.HeadPipeline.WebURL)
			}
			return nil
		}
		fmt.Fprintf(a.Out, "Đã merge !%d — %s\n%s → %s\n%s\n", iid, title, source, target, web)
		return nil
	default:
		return errors.New("không xử lý được trạng thái merge")
	}
}

func (a *App) cmdTag(args []string) error {
	fs := flag.NewFlagSet("tag", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	projectFlag := fs.String("project", "", "Project dạng group/app hoặc URL")
	nameFlag := fs.String("name", "", "Tên tag")
	refFlag := fs.String("ref", "", "Nhánh hoặc tag nguồn")
	yes := fs.Bool("yes", false, "Không hỏi lại trước khi tạo tag")
	yesShort := fs.Bool("y", false, "Không hỏi lại trước khi tạo tag")
	fs.Usage = func() { fmt.Fprint(a.Err, tagHelp) }
	if err := flagParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 2 {
		fmt.Fprint(a.Err, tagHelp)
		return code(2, errors.New("tag chỉ nhận tối đa hai tham số: <tên-tag> <nhánh-hoặc-tag>"))
	}
	name := *nameFlag
	ref := *refFlag
	if name == "" && fs.NArg() >= 1 {
		name = fs.Arg(0)
	}
	if ref == "" && fs.NArg() >= 2 {
		ref = fs.Arg(1)
	}
	if name != "" {
		if err := validateRefName(name, "tên tag"); err != nil {
			return err
		}
	}
	if ref != "" {
		if err := validateRefName(ref, "nhánh hoặc tag nguồn"); err != nil {
			return err
		}
	}

	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	project := strings.TrimSpace(*projectFlag)
	if project == "" {
		project = a.gitProject(cfg.Endpoint)
	}
	if !a.interactive() {
		if project == "" {
			fmt.Fprint(a.Err, tagHelp)
			return code(2, errors.New("thiếu project. Truyền --project hoặc chạy trong repo Git có remote GitLab"))
		}
		if name == "" {
			fmt.Fprint(a.Err, tagHelp)
			return code(2, errors.New("thiếu tên tag. Truyền --name"))
		}
		if ref == "" {
			fmt.Fprint(a.Err, tagHelp)
			return code(2, errors.New("thiếu nhánh hoặc tag nguồn. Truyền --ref"))
		}
		if !*yes && !*yesShort {
			return code(2, errors.New("thêm --yes để tạo tag mà không hỏi lại"))
		}
	}

	if project == "" {
		project, err = a.prompt("Project", "")
		if err != nil {
			return err
		}
	}
	project, err = parseProject(cfg.Endpoint, project)
	if err != nil {
		return err
	}

	client := a.client(cfg)
	ctx := context.Background()
	proj, err := client.GetProject(ctx, project)
	if err != nil {
		return friendly(err, "đọc project")
	}
	if proj.PathWithNamespace != "" {
		project = proj.PathWithNamespace
	}
	if name == "" {
		name, err = a.prompt("Tên tag", "")
		if err != nil {
			return err
		}
		if err := validateRefName(name, "tên tag"); err != nil {
			return err
		}
	}
	if ref == "" {
		ref, err = a.prompt("Nhánh hoặc tag nguồn", proj.DefaultBranch)
		if err != nil {
			return err
		}
		if err := validateRefName(ref, "nhánh hoặc tag nguồn"); err != nil {
			return err
		}
	}
	if !*yes && !*yesShort {
		ok, err := a.confirm(fmt.Sprintf("Tạo tag %s từ %s trên %s", name, ref, project))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(a.Out, "Đã hủy.")
			return nil
		}
	}

	tag, err := client.CreateTag(ctx, project, name, ref)
	if err != nil {
		return friendly(err, "tạo tag")
	}
	if tag.Name != "" {
		name = tag.Name
	}
	short := tag.Commit.ShortID
	if short == "" && len(tag.Commit.ID) >= 8 {
		short = tag.Commit.ID[:8]
	}
	if short == "" {
		short = tag.Target
	}
	fmt.Fprintf(a.Out, "Đã tạo tag %s từ %s (%s).\n", name, ref, short)

	pipe, found, waitErr := a.waitForTagPipeline(ctx, client, project, name, tag.Commit.ID)
	if found {
		web := pipe.WebURL
		if web == "" {
			web = pipelineURL(cfg.Endpoint, project, pipe.ID)
		}
		fmt.Fprintf(a.Out, "Pipeline #%d %s\n%s\n", pipe.ID, statusVI(pipe.Status), web)
		return nil
	}
	if waitErr != nil {
		fmt.Fprintf(a.Err, "Đã tạo tag, nhưng không kiểm tra được pipeline: %s\n", friendly(waitErr, "đọc pipeline"))
		return nil
	}
	fmt.Fprintln(a.Out, "Chưa thấy pipeline cho tag này. Pipeline chỉ chạy nếu .gitlab-ci.yml khớp với tag.")
	return nil
}

func (a *App) waitForTagPipeline(ctx context.Context, client *Client, project, ref, sha string) (Pipeline, bool, error) {
	const attempts = 8
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			a.sleep()
		}
		pipes, err := client.PipelinesByRef(ctx, project, ref)
		if err != nil {
			lastErr = err
			continue
		}
		for _, pipe := range pipes {
			if sha == "" || strings.EqualFold(pipe.SHA, sha) {
				return pipe, true, nil
			}
		}
	}
	return Pipeline{}, false, lastErr
}

func (a *App) cmdRetry(args []string) error {
	fs := flag.NewFlagSet("retry", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	fs.Usage = func() { fmt.Fprint(a.Err, retryHelp) }
	if err := flagParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fmt.Fprint(a.Err, retryHelp)
		return code(2, errors.New("cần đúng một link pipeline"))
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	link, err := parseLink(cfg.Endpoint, fs.Arg(0))
	if err != nil {
		return err
	}
	if link.Kind != linkPipeline {
		return wrongLink(link.Kind)
	}
	updated, err := a.client(cfg).RetryPipeline(context.Background(), link.Project, link.Number)
	if err != nil {
		return friendly(err, "chạy lại pipeline")
	}
	id := updated.ID
	if id == 0 {
		id = link.Number
	}
	web := updated.WebURL
	if web == "" {
		web = pipelineURL(cfg.Endpoint, link.Project, id)
	}
	fmt.Fprintf(a.Out, "Đã chạy lại pipeline #%d.\nTrạng thái: %s\n%s\n", id, statusVI(updated.Status), web)
	return nil
}

func (a *App) client(cfg Config) *Client {
	return NewClient(cfg.Endpoint, cfg.Token, cfg.InsecureSkipVerify)
}

func wrongLink(kind linkKind) error {
	return code(2, fmt.Errorf("link này là %s. Dùng `glops %s`", kindLabel(kind), kindCommand(kind)))
}

func kindLabel(kind linkKind) string {
	switch kind {
	case linkMergeRequest:
		return "merge request"
	case linkPipeline:
		return "pipeline"
	case linkProject:
		return "project"
	default:
		return "không rõ"
	}
}

func kindCommand(kind linkKind) string {
	switch kind {
	case linkMergeRequest:
		return "merge <link>"
	case linkPipeline:
		return "retry <link>"
	default:
		return "tag --project <url>"
	}
}

func mergeRequestURL(endpoint, project string, iid int, from string) string {
	if from != "" {
		return from
	}
	return strings.TrimRight(endpoint, "/") + "/" + strings.Trim(project, "/") + "/-/merge_requests/" + fmt.Sprint(iid)
}

func pipelineURL(endpoint, project string, id int) string {
	return strings.TrimRight(endpoint, "/") + "/" + strings.Trim(project, "/") + "/-/pipelines/" + fmt.Sprint(id)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstIID(mr MergeRequest, fallback int) int {
	if mr.IID > 0 {
		return mr.IID
	}
	return fallback
}

func validateRefName(name, what string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s không được để trống", what)
	}
	if len(name) > 255 {
		return fmt.Errorf("%s quá dài", what)
	}
	if name == "@" || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") {
		return fmt.Errorf("%s không hợp lệ: %s", what, name)
	}
	if strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.Contains(name, "//") {
		return fmt.Errorf("%s không hợp lệ: %s", what, name)
	}
	if strings.ContainsAny(name, " ~^:?*[\\") {
		return fmt.Errorf("%s không hợp lệ: %s", what, name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s không hợp lệ: %s", what, name)
		}
	}
	return nil
}
