package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSendsTokenAndEncodedProject(t *testing.T) {
	var gotToken, gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		gotURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":9,"path_with_namespace":"group/sub/app","default_branch":"main"}`)
	}))
	defer srv.Close()

	project, err := NewClient(srv.URL, "secret-token", false).GetProject(context.Background(), "group/sub/app")
	if err != nil {
		t.Fatal(err)
	}
	if gotToken != "secret-token" {
		t.Fatalf("token header %q", gotToken)
	}
	if !strings.Contains(gotURI, "/projects/group%2Fsub%2Fapp") {
		t.Fatalf("uri %s", gotURI)
	}
	if project.PathWithNamespace != "group/sub/app" || project.DefaultBranch != "main" {
		t.Fatalf("%+v", project)
	}
}

func TestClientBlocksCrossHostRedirect(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("token reached the other host")
		io.WriteString(w, `{"id":1,"username":"x"}`)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/api/v4/user", http.StatusFound)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "secret-token", false).CurrentUser(context.Background())
	if err == nil || !strings.Contains(err.Error(), "host khác") {
		t.Fatal(err)
	}
}

func TestAPIErrorRedactsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"message":{"ref":["not found secret-token-value"]}}`)
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, "secret-token-value", false).CurrentUser(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret-token-value") {
		t.Fatal(err)
	}
	if !strings.Contains(err.Error(), "ref:") || !strings.Contains(err.Error(), "***") {
		t.Fatal(err)
	}
}

func TestFlattenAPIMessage(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"message":"nope"}`, "nope"},
		{`{"message":["a","b"]}`, "a; b"},
		{`{"error":"invalid_token"}`, "invalid_token"},
		{`{"message":{"base":["Reference not found"]}}`, "base: [Reference not found]"},
	}
	for _, tc := range cases {
		got := flattenAPIMessage([]byte(tc.in))
		if got != tc.want {
			t.Errorf("flatten %s: got %q want %q", tc.in, got, tc.want)
		}
	}
}
