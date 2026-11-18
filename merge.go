package main

import "fmt"

const (
	actAlready  = "already"
	actNow      = "now"
	actPipeline = "pipeline"
	actWait     = "wait"
	actDeny     = "deny"
	actDenyPerm = "deny_perm"
)

type mergeDecision struct {
	Action  string
	Message string
}

func planMerge(mr MergeRequest) mergeDecision {
	iid := mr.IID
	if mr.State == "merged" {
		return mergeDecision{Action: actAlready}
	}
	if mr.State == "locked" {
		return mergeDecision{Action: actDeny, Message: fmt.Sprintf("Merge request !%d đang được merge.", iid)}
	}
	if mr.State != "opened" {
		state := mr.State
		if state == "" {
			state = "không rõ"
		}
		return mergeDecision{Action: actDeny, Message: fmt.Sprintf("Merge request !%d đang ở trạng thái %s.", iid, state)}
	}
	if mr.Draft || mr.WorkInProgress || mr.DetailedMergeStatus == "draft_status" {
		return mergeDecision{Action: actDeny, Message: fmt.Sprintf("Merge request !%d đang là draft.", iid)}
	}
	if mr.User != nil && !mr.User.CanMerge {
		return mergeDecision{Action: actDenyPerm, Message: fmt.Sprintf("Tài khoản không có quyền merge !%d.", iid)}
	}
	if mr.HasConflicts || mr.DetailedMergeStatus == "conflict" {
		return mergeDecision{Action: actDeny, Message: fmt.Sprintf("Merge request !%d đang có conflict.", iid)}
	}
	switch mr.DetailedMergeStatus {
	case "checking", "unchecked", "approvals_syncing", "preparing":
		return mergeDecision{Action: actWait}
	case "ci_still_running", "ci_must_pass":
		return pipelineDecision(mr)
	case "", "mergeable":
	default:
		if format, ok := denyReasons[mr.DetailedMergeStatus]; ok {
			return mergeDecision{Action: actDeny, Message: fmt.Sprintf(format, iid)}
		}
		return mergeDecision{Action: actDeny, Message: fmt.Sprintf("Chưa thể merge !%d (trạng thái: %s).", iid, mr.DetailedMergeStatus)}
	}
	switch mr.MergeStatus {
	case "checking", "unchecked":
		return mergeDecision{Action: actWait}
	case "cannot_be_merged":
		return mergeDecision{Action: actDeny, Message: fmt.Sprintf("Merge request !%d chưa thể merge.", iid)}
	default:
		return mergeDecision{Action: actNow}
	}
}

func pipelineDecision(mr MergeRequest) mergeDecision {
	status := ""
	web := ""
	if mr.HeadPipeline != nil {
		status = mr.HeadPipeline.Status
		web = mr.HeadPipeline.WebURL
	}
	withURL := func(msg string) string {
		if web == "" {
			return msg
		}
		return msg + "\n" + web
	}
	switch status {
	case "success":
		return mergeDecision{Action: actNow}
	case "failed":
		return mergeDecision{Action: actDeny, Message: withURL(fmt.Sprintf("Pipeline của !%d thất bại, chưa thể merge.", mr.IID))}
	case "canceled", "canceling":
		return mergeDecision{Action: actDeny, Message: withURL(fmt.Sprintf("Pipeline của !%d đã hủy, chưa thể merge.", mr.IID))}
	case "manual", "skipped":
		return mergeDecision{Action: actDeny, Message: withURL(fmt.Sprintf("Pipeline của !%d không chạy tự động (%s), chưa thể merge.", mr.IID, statusVI(status)))}
	case "created", "waiting_for_resource", "preparing", "pending", "running", "scheduled":
		return mergeDecision{Action: actPipeline}
	case "":
		if mr.DetailedMergeStatus == "ci_still_running" {
			return mergeDecision{Action: actPipeline}
		}
		return mergeDecision{Action: actDeny, Message: withURL(fmt.Sprintf("Pipeline của !%d chưa thành công, chưa thể merge.", mr.IID))}
	default:
		if mr.DetailedMergeStatus == "ci_still_running" {
			return mergeDecision{Action: actPipeline}
		}
		return mergeDecision{Action: actDeny, Message: withURL(fmt.Sprintf("Pipeline của !%d đang %s, chưa thể merge.", mr.IID, statusVI(status)))}
	}
}

var denyReasons = map[string]string{
	"discussions_not_resolved":   "Còn discussion chưa được resolve trên !%d.",
	"not_approved":               "Chưa đủ approval để merge !%d.",
	"requested_changes":          "Còn yêu cầu thay đổi chưa xử lý trên !%d.",
	"merge_request_blocked":      "Merge request !%d đang bị chặn.",
	"blocked_status":             "Merge request !%d đang bị chặn.",
	"status_checks_must_pass":    "Status check bên ngoài của !%d chưa pass.",
	"external_status_checks":     "Status check bên ngoài của !%d chưa pass.",
	"jira_association_missing":   "Merge request !%d thiếu liên kết Jira bắt buộc.",
	"need_rebase":                "Merge request !%d cần rebase trước khi merge.",
	"commits_status":             "Commit của !%d chưa đủ điều kiện merge.",
	"security_policy_violations": "Merge request !%d vi phạm security policy.",
	"locked_paths":               "Merge request !%d có file đang bị khóa.",
	"locked_lfs_files":           "Merge request !%d có file LFS đang bị khóa.",
	"not_open":                   "Merge request !%d không còn mở.",
}

func statusVI(status string) string {
	switch status {
	case "created":
		return "vừa tạo"
	case "waiting_for_resource":
		return "chờ tài nguyên"
	case "preparing":
		return "đang chuẩn bị"
	case "pending":
		return "đang chờ"
	case "running":
		return "đang chạy"
	case "success":
		return "thành công"
	case "failed":
		return "thất bại"
	case "canceled", "canceling":
		return "đã hủy"
	case "skipped":
		return "bỏ qua"
	case "manual":
		return "chạy tay"
	case "scheduled":
		return "đã lên lịch"
	case "":
		return "không rõ"
	default:
		return status
	}
}
