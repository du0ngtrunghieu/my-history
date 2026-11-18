package main

import (
	"strings"
	"testing"
)

func TestPlanMerge(t *testing.T) {
	mergeable := func() MergeRequest {
		return MergeRequest{
			IID:                 42,
			State:               "opened",
			MergeStatus:         "can_be_merged",
			DetailedMergeStatus: "mergeable",
			User:                &mergeUser{CanMerge: true},
		}
	}
	cases := []struct {
		name   string
		mr     MergeRequest
		action string
		msg    string
	}{
		{name: "mergeable", mr: mergeable(), action: actNow},
		{name: "already", mr: func() MergeRequest { m := mergeable(); m.State = "merged"; return m }(), action: actAlready},
		{name: "closed", mr: func() MergeRequest { m := mergeable(); m.State = "closed"; return m }(), action: actDeny, msg: "closed"},
		{name: "draft", mr: func() MergeRequest { m := mergeable(); m.Draft = true; return m }(), action: actDeny, msg: "draft"},
		{name: "no permission", mr: func() MergeRequest { m := mergeable(); m.User = &mergeUser{CanMerge: false}; return m }(), action: actDenyPerm, msg: "không có quyền"},
		{name: "unknown user still tries", mr: func() MergeRequest { m := mergeable(); m.User = nil; return m }(), action: actNow},
		{name: "conflict", mr: func() MergeRequest { m := mergeable(); m.HasConflicts = true; return m }(), action: actDeny, msg: "conflict"},
		{name: "approval", mr: func() MergeRequest { m := mergeable(); m.DetailedMergeStatus = "not_approved"; return m }(), action: actDeny, msg: "approval"},
		{name: "checking", mr: func() MergeRequest { m := mergeable(); m.DetailedMergeStatus = "checking"; return m }(), action: actWait},
		{name: "pipeline running", mr: func() MergeRequest {
			m := mergeable()
			m.DetailedMergeStatus = "ci_still_running"
			m.HeadPipeline = &Pipeline{Status: "running", WebURL: "https://gitlab.example.com/p/1"}
			return m
		}(), action: actPipeline},
		{name: "pipeline failed", mr: func() MergeRequest {
			m := mergeable()
			m.DetailedMergeStatus = "ci_must_pass"
			m.HeadPipeline = &Pipeline{Status: "failed", WebURL: "https://gitlab.example.com/p/2"}
			return m
		}(), action: actDeny, msg: "thất bại"},
		{name: "pipeline success falls through to merge", mr: func() MergeRequest {
			m := mergeable()
			m.DetailedMergeStatus = "ci_must_pass"
			m.HeadPipeline = &Pipeline{Status: "success"}
			return m
		}(), action: actNow},
		{name: "missing pipeline", mr: func() MergeRequest {
			m := mergeable()
			m.DetailedMergeStatus = "ci_must_pass"
			return m
		}(), action: actDeny, msg: "chưa thành công"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := planMerge(tc.mr)
			if got.Action != tc.action {
				t.Fatalf("action %s message %s", got.Action, got.Message)
			}
			if tc.msg != "" && !strings.Contains(got.Message, tc.msg) {
				t.Fatalf("message %q, want substring %q", got.Message, tc.msg)
			}
		})
	}
}
