package crew

import (
	"testing"

	"novaly/backend/models"
)

func TestInferChatPlanQCAndFix(t *testing.T) {
	if InferChatPlan("质检本集", 4).Action != "qc" {
		t.Fatal("质检 should be qc")
	}
	if InferChatPlan("按上次建议修改", 4).Action != "fix" {
		t.Fatal("按建议 should be fix")
	}
}

func TestPlanChatKeepsExplicitSplitWithoutLLM(t *testing.T) {
	got := PlanChat(nil, models.AIProvider{}, models.AIModel{}, "开始拆镜", "剧本", nil, nil, nil, nil)
	if got.Action != "split" || !got.Replace {
		t.Fatalf("empty shots should split, got %#v", got)
	}
}

func TestInferChatPlanSplitNeedsReplaceWhenShotsExist(t *testing.T) {
	got := InferChatPlan("开始拆镜", 3)
	if got.Action != "reply" {
		t.Fatalf("existing shots without replace should reply, got %#v", got)
	}
	got = InferChatPlan("重新拆镜替换", 3)
	if got.Action != "split" || !got.Replace {
		t.Fatalf("explicit replace should split, got %#v", got)
	}
	got = InferChatPlan("开始拆镜", 0)
	if got.Action != "split" {
		t.Fatalf("empty episode should split, got %#v", got)
	}
}

func TestResolvePlanIssuesUsesLastQC(t *testing.T) {
	shots := []ShotContext{{ID: 10, Index: 1}, {ID: 11, Index: 2}}
	last := []QCIssue{
		{Code: "R2", ShotID: 10, ShotIndex: 1, Message: "台词被拆开"},
		{Code: "R1", ShotID: 11, ShotIndex: 2, Message: "未绑定"},
	}
	got := ResolvePlanIssues(ChatPlan{Action: "fix"}, shots, last)
	if len(got) != 2 {
		t.Fatalf("fix without ids should take last report, got %#v", got)
	}
	got = ResolvePlanIssues(ChatPlan{Action: "fix", ShotIndexes: []int{1}}, shots, last)
	if len(got) != 1 || got[0].ShotID != 10 {
		t.Fatalf("shot index 1 should keep R2, got %#v", got)
	}
}

func TestFormatQCReportEmpty(t *testing.T) {
	got := FormatQCReport(QCReport{Score: "A", Summary: "通过"})
	if got == "" {
		t.Fatal("empty")
	}
}

func TestDecodeEncodeChatRoundTrip(t *testing.T) {
	msgs := []ChatMessage{NewChatMessage("user", "", "质检本集", "")}
	raw := EncodeChat(msgs)
	got := DecodeChat(raw)
	if len(got) != 1 || got[0].Content != "质检本集" {
		t.Fatalf("got %#v", got)
	}
}
