package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeEndpoint(t *testing.T) {
	cases := map[string]string{
		"gitlab.com":                          "https://gitlab.com",
		"https://gitlab.com/":                 "https://gitlab.com",
		"https://GitLab.com/gitlab/":          "https://gitlab.com/gitlab",
		"http://localhost:8080":               "http://localhost:8080",
		"https://git.example.com:8443/gitlab": "https://git.example.com:8443/gitlab",
	}
	for in, want := range cases {
		got, err := normalizeEndpoint(in)
		if err != nil || got != want {
			t.Errorf("normalize %q: got %q err %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://gitlab.com", "https://user:pass@gitlab.com", "https://gitlab.com/a?b=1", "https://gitlab.com/a#x"} {
		if _, err := normalizeEndpoint(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestSaveConfigRoundTripAndMode(t *testing.T) {
	app := &App{ConfigDir: t.TempDir()}
	path, err := app.saveConfig(Config{
		Endpoint: "https://gitlab.example.com",
		Username: "hieu",
		Token:    "super-secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %o", info.Mode().Perm())
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", dir.Mode().Perm())
	}
	loaded, err := app.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Username != "hieu" || loaded.Token != "super-secret-token" || loaded.Endpoint != "https://gitlab.example.com" {
		t.Fatalf("%+v", loaded)
	}
}

func TestLoadMissingConfig(t *testing.T) {
	app := &App{ConfigDir: t.TempDir()}
	_, err := app.loadConfig()
	if !errors.Is(err, errNoConfig) {
		t.Fatal(err)
	}
}

func TestMaskToken(t *testing.T) {
	if got := maskToken("super-secret-token"); got != "****oken" {
		t.Fatal(got)
	}
	if got := maskToken("ab"); got != "****" {
		t.Fatal(got)
	}
	if got := maskToken(""); got != "" {
		t.Fatal(got)
	}
}
