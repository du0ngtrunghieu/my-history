package main

import (
	"errors"
	"os/exec"
	"strings"
)

var errNoGitProject = errors.New("không thấy remote GitLab khớp endpoint")

func detectGitProject(endpoint string) (string, error) {
	return detectGitProjectIn(endpoint, "")
}

func detectGitProjectIn(endpoint, dir string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", err
	}
	if err := gitRun(dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return "", errNoGitProject
	}
	out, err := gitOutput(dir, "remote")
	if err != nil {
		return "", err
	}
	names := strings.Fields(out)
	ordered := make([]string, 0, len(names))
	rest := make([]string, 0, len(names))
	for _, name := range names {
		if name == "origin" {
			ordered = append(ordered, name)
			continue
		}
		rest = append(rest, name)
	}
	ordered = append(ordered, rest...)
	for _, name := range ordered {
		raw, err := gitOutput(dir, "remote", "get-url", name)
		if err != nil {
			continue
		}
		if project, ok := parseRemote(strings.TrimSpace(raw), endpoint); ok {
			project, err = normalizeProject(project)
			if err != nil {
				continue
			}
			return project, nil
		}
	}
	return "", errNoGitProject
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func gitRun(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.Run()
}

func (a *App) gitProject(endpoint string) string {
	fn := a.LookRemote
	if fn == nil {
		fn = detectGitProject
	}
	project, err := fn(endpoint)
	if err != nil {
		return ""
	}
	return project
}
