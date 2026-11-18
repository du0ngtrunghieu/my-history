package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Client struct {
	Endpoint string
	Token    string
	HTTP     *http.Client
}

type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

type Project struct {
	ID                int    `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	DefaultBranch     string `json:"default_branch"`
	WebURL            string `json:"web_url"`
}

type Pipeline struct {
	ID     int    `json:"id"`
	IID    int    `json:"iid"`
	Status string `json:"status"`
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
	WebURL string `json:"web_url"`
}

type MergeRequest struct {
	IID                       int        `json:"iid"`
	Title                     string     `json:"title"`
	State                     string     `json:"state"`
	Draft                     bool       `json:"draft"`
	WorkInProgress            bool       `json:"work_in_progress"`
	MergeStatus               string     `json:"merge_status"`
	DetailedMergeStatus       string     `json:"detailed_merge_status"`
	HasConflicts              bool       `json:"has_conflicts"`
	SHA                       string     `json:"sha"`
	SourceBranch              string     `json:"source_branch"`
	TargetBranch              string     `json:"target_branch"`
	WebURL                    string     `json:"web_url"`
	MergeWhenPipelineSucceeds bool       `json:"merge_when_pipeline_succeeds"`
	HeadPipeline              *Pipeline  `json:"head_pipeline"`
	User                      *mergeUser `json:"user"`
}

type mergeUser struct {
	CanMerge bool `json:"can_merge"`
}

type Tag struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Commit struct {
		ID      string `json:"id"`
		ShortID string `json:"short_id"`
		Title   string `json:"title"`
	} `json:"commit"`
}

type MergeOptions struct {
	SHA                  string
	RemoveSourceBranch   bool
	WhenPipelineSucceeds bool
}

type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("GitLab trả về HTTP %d", e.Status)
	}
	return fmt.Sprintf("GitLab trả về HTTP %d: %s", e.Status, e.Message)
}

func NewClient(endpoint, token string, insecure bool) *Client {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	transport := base.Clone()
	if insecure {
		// The user explicitly opted in for a private or self-signed GitLab certificate.
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		Endpoint: strings.TrimRight(endpoint, "/"),
		Token:    token,
		HTTP: &http.Client{
			Timeout:   45 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) == 0 {
					return nil
				}
				if !sameURLHost(req.URL, via[0].URL) {
					return fmt.Errorf("từ chối chuyển hướng sang host khác (%s)", req.URL.Host)
				}
				if len(via) >= 5 {
					return errors.New("quá nhiều chuyển hướng")
				}
				return nil
			},
		},
	}
}

func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	var user User
	err := c.get(ctx, "/user", &user)
	return user, err
}

func (c *Client) GetProject(ctx context.Context, project string) (Project, error) {
	var out Project
	err := c.get(ctx, projectPrefix(project), &out)
	return out, err
}

func (c *Client) GetMergeRequest(ctx context.Context, project string, iid int) (MergeRequest, error) {
	var out MergeRequest
	err := c.get(ctx, fmt.Sprintf("%s/merge_requests/%d", projectPrefix(project), iid), &out)
	return out, err
}

func (c *Client) Merge(ctx context.Context, project string, iid int, opt MergeOptions) (MergeRequest, error) {
	body := map[string]any{
		"should_remove_source_branch":  opt.RemoveSourceBranch,
		"merge_when_pipeline_succeeds": opt.WhenPipelineSucceeds,
	}
	if opt.SHA != "" {
		body["sha"] = opt.SHA
	}
	var out MergeRequest
	err := c.post(ctx, fmt.Sprintf("%s/merge_requests/%d/merge", projectPrefix(project), iid), body, &out)
	return out, err
}

func (c *Client) CreateTag(ctx context.Context, project, name, ref string) (Tag, error) {
	body := map[string]string{
		"tag_name": name,
		"ref":      ref,
	}
	var out Tag
	err := c.post(ctx, projectPrefix(project)+"/repository/tags", body, &out)
	return out, err
}

func (c *Client) PipelinesByRef(ctx context.Context, project, ref string) ([]Pipeline, error) {
	q := url.Values{}
	q.Set("ref", ref)
	q.Set("order_by", "id")
	q.Set("sort", "desc")
	q.Set("per_page", "20")
	var out []Pipeline
	err := c.get(ctx, projectPrefix(project)+"/pipelines?"+q.Encode(), &out)
	return out, err
}

func (c *Client) RetryPipeline(ctx context.Context, project string, id int) (Pipeline, error) {
	var out Pipeline
	err := c.post(ctx, fmt.Sprintf("%s/pipelines/%d/retry", projectPrefix(project), id), nil, &out)
	return out, err
}

func projectPrefix(project string) string {
	return "/projects/" + url.PathEscape(strings.Trim(project, "/"))
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, cancel, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer cancel()
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, cancel, err := c.newRequest(ctx, http.MethodPost, path, reader)
	if err != nil {
		return err
	}
	defer cancel()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req, out)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, context.CancelFunc, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	req, err := http.NewRequestWithContext(ctx, method, c.Endpoint+"/api/v4"+path, body)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "glops/"+version)
	return req, cancel, nil
}

func (c *Client) do(req *http.Request, out any) error {
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("không gọi được GitLab: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return c.apiError(res)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("GitLab trả về dữ liệu không đọc được: %w", err)
	}
	return nil
}

func (c *Client) apiError(res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	msg := flattenAPIMessage(body)
	msg = strings.ReplaceAll(msg, c.Token, "***")
	return &APIError{Status: res.StatusCode, Message: strings.TrimSpace(msg)}
}

func flattenAPIMessage(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	var raw struct {
		Message json.RawMessage `json:"message"`
		Error   string          `json:"error"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return string(body)
	}
	if msg := flattenRaw(raw.Message); msg != "" {
		return msg
	}
	if raw.Error != "" {
		return raw.Error
	}
	return ""
}

func flattenRaw(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var list []any
	if json.Unmarshal(raw, &list) == nil {
		parts := make([]string, 0, len(list))
		for _, item := range list {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, "; ")
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+": "+fmt.Sprint(obj[k]))
		}
		return strings.Join(parts, "; ")
	}
	return string(raw)
}

func friendly(err error, action string) error {
	if err == nil {
		return nil
	}
	var api *APIError
	if !errors.As(err, &api) {
		return err
	}
	switch api.Status {
	case http.StatusUnauthorized:
		return code(3, errors.New("token không hợp lệ hoặc đã hết hạn. Chạy `glops setup` lại"))
	case http.StatusForbidden:
		msg := "không có quyền " + action
		if strings.TrimSpace(api.Message) != "" {
			msg += ": " + api.Message
		}
		return code(3, errors.New(msg))
	case http.StatusNotFound:
		return fmt.Errorf("không tìm thấy (%s)", action)
	default:
		if api.Message != "" {
			return fmt.Errorf("%s: HTTP %d: %s", action, api.Status, api.Message)
		}
		return fmt.Errorf("%s: HTTP %d", action, api.Status)
	}
}
