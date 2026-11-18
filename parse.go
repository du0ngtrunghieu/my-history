package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type linkKind string

const (
	linkMergeRequest linkKind = "merge_request"
	linkPipeline     linkKind = "pipeline"
	linkProject      linkKind = "project"
)

type linkRef struct {
	Kind    linkKind
	Project string
	Number  int
}

func parseLink(endpoint, raw string) (linkRef, error) {
	raw = trimWrapping(raw)
	if raw == "" {
		return linkRef{}, errors.New("link trống")
	}
	ep, err := url.Parse(endpoint)
	if err != nil || ep.Host == "" {
		return linkRef{}, errors.New("endpoint trong cấu hình không hợp lệ")
	}
	if !strings.Contains(raw, "://") {
		raw = strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(raw, "/")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return linkRef{}, errors.New("link không hợp lệ")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return linkRef{}, errors.New("link phải bắt đầu bằng http hoặc https")
	}
	if !sameURLHost(ep, u) {
		return linkRef{}, fmt.Errorf("link là %s://%s nhưng cấu hình đang dùng %s", u.Scheme, u.Host, strings.TrimRight(endpoint, "/"))
	}
	path, err := stripEndpointPrefix(ep.Path, u.Path)
	if err != nil {
		return linkRef{}, err
	}
	ref, err := classifyPath(strings.Trim(path, "/"))
	if err != nil {
		return linkRef{}, err
	}
	ref.Project, err = normalizeProject(ref.Project)
	if err != nil {
		return linkRef{}, err
	}
	return ref, nil
}

func stripEndpointPrefix(endpointPath, linkPath string) (string, error) {
	prefix := strings.Trim(endpointPath, "/")
	path := strings.Trim(linkPath, "/")
	if prefix == "" {
		return path, nil
	}
	if path != prefix && !strings.HasPrefix(path, prefix+"/") {
		return "", errors.New("link không nằm trong đường dẫn của endpoint đã cấu hình")
	}
	return strings.Trim(strings.TrimPrefix(path, prefix), "/"), nil
}

func classifyPath(path string) (linkRef, error) {
	if path == "" {
		return linkRef{}, errors.New("link thiếu project")
	}
	segs := strings.Split(path, "/")
	for i := 0; i < len(segs); i++ {
		if segs[i] != "-" || i+1 >= len(segs) {
			continue
		}
		switch segs[i+1] {
		case "jobs":
			return linkRef{}, errors.New("đây là link job. Hãy gửi link pipeline (.../-/pipelines/<id>)")
		case "merge_requests", "pipelines":
			if i+2 >= len(segs) {
				return linkRef{}, errors.New("link thiếu số thứ tự")
			}
			return refFrom(segs[:i], segs[i+1], segs[i+2])
		}
	}
	for i := 0; i < len(segs); i++ {
		if segs[i] != "merge_requests" && segs[i] != "pipelines" {
			continue
		}
		if i > 0 && segs[i-1] == "-" {
			continue
		}
		if i+1 >= len(segs) {
			return linkRef{}, errors.New("link thiếu số thứ tự")
		}
		return refFrom(segs[:i], segs[i], segs[i+1])
	}
	return linkRef{Kind: linkProject, Project: path}, nil
}

func refFrom(projectSegs []string, kind, num string) (linkRef, error) {
	if len(projectSegs) == 0 {
		return linkRef{}, errors.New("link thiếu project")
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return linkRef{}, fmt.Errorf("số trong link không hợp lệ: %s", num)
	}
	k := linkMergeRequest
	if kind == "pipelines" {
		k = linkPipeline
	}
	return linkRef{Kind: k, Project: strings.Join(projectSegs, "/"), Number: n}, nil
}

func normalizeProject(p string) (string, error) {
	p = strings.Trim(strings.TrimSpace(p), "/")
	p = strings.TrimSuffix(p, ".git")
	if p == "" {
		return "", errors.New("thiếu project")
	}
	if strings.ContainsAny(p, "\\?#\t\r\n") {
		return "", fmt.Errorf("project không hợp lệ: %s", p)
	}
	for _, s := range strings.Split(p, "/") {
		if s == "" || s == "." || s == ".." || s == "-" {
			return "", fmt.Errorf("project không hợp lệ: %s", p)
		}
	}
	return p, nil
}

func parseProject(endpoint, raw string) (string, error) {
	raw = trimWrapping(raw)
	if raw == "" {
		return "", errors.New("thiếu project")
	}
	if strings.HasPrefix(raw, "git@") || strings.HasPrefix(raw, "ssh://") || strings.HasPrefix(raw, "git://") {
		project, ok := parseRemote(raw, endpoint)
		if !ok {
			return "", errors.New("URL project không khớp endpoint đã lưu")
		}
		return normalizeProject(project)
	}
	if strings.Contains(raw, "://") {
		link, err := parseLink(endpoint, raw)
		if err != nil {
			return "", err
		}
		return link.Project, nil
	}
	return normalizeProject(raw)
}

func parseRemote(remote, endpoint string) (string, bool) {
	remote = strings.TrimSpace(remote)
	ep, err := url.Parse(endpoint)
	if err != nil || ep.Host == "" || remote == "" {
		return "", false
	}
	kind, host, path, ok := splitRemote(remote)
	if !ok {
		return "", false
	}
	if !strings.EqualFold(hostname(host), ep.Hostname()) {
		return "", false
	}
	if kind == "http" {
		remoteURL := &url.URL{Host: host, Scheme: "https"}
		if strings.HasPrefix(remote, "http://") {
			remoteURL.Scheme = "http"
		}
		if portOf(remoteURL) != portOf(ep) {
			return "", false
		}
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	prefix := strings.Trim(ep.Path, "/")
	if prefix != "" {
		if path != prefix && !strings.HasPrefix(path, prefix+"/") {
			return "", false
		}
		path = strings.Trim(strings.TrimPrefix(path, prefix), "/")
	}
	if path == "" {
		return "", false
	}
	return path, true
}

func splitRemote(remote string) (kind, host, path string, ok bool) {
	if strings.HasPrefix(remote, "git@") && !strings.Contains(remote, "://") {
		rest := strings.TrimPrefix(remote, "git@")
		host, path, ok = strings.Cut(rest, ":")
		if !ok || host == "" || path == "" || strings.Contains(host, "/") {
			return "", "", "", false
		}
		return "ssh", host, path, true
	}
	u, err := url.Parse(remote)
	if err != nil || u.Host == "" {
		return "", "", "", false
	}
	kind = "http"
	switch u.Scheme {
	case "ssh", "git":
		kind = "ssh"
	case "http", "https":
		kind = "http"
	default:
		return "", "", "", false
	}
	return kind, u.Host, strings.TrimPrefix(u.Path, "/"), true
}

func hostname(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}
