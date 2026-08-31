package services

import (
	"fmt"
	"strings"
	"testing"
)

func TestSanitizePlatformViolenceRewritesKnifeThreat(t *testing.T) {
	in := "【6-10秒】镜头：中景，韩铮站在更衣室门口，语气带笑；韩铮说：「谁敢在我冠军夜动刀——他有种。」"
	got := SanitizePlatformViolence(in)
	if HasPlatformViolence(got) {
		t.Fatalf("sanitized script still flagged: %s", got)
	}
	if !strings.Contains(got, "搅局") || !strings.Contains(got, "他有种") {
		t.Fatalf("should keep swagger and drop 动刀, got %s", got)
	}
	if strings.Contains(got, "动刀") {
		t.Fatalf("动刀 should be gone, got %s", got)
	}
}

func TestSanitizePlatformViolenceRewritesAssassinName(t *testing.T) {
	in := "主体定义：将图4定义为群演外观参考（杀手甲）；持刀威胁。"
	got := SanitizePlatformViolence(in)
	if strings.Contains(got, "杀手") || strings.Contains(got, "持刀") {
		t.Fatalf("still flagged: %s", got)
	}
	if !strings.Contains(got, "来人甲") || !strings.Contains(got, "出言威胁") {
		t.Fatalf("should keep identity slot, got %s", got)
	}
}

func TestHumanizeVideoErrorExplainsSafetyReject(t *testing.T) {
	err := humanizeVideoError(fmt.Errorf("视频生成失败：生成内容中疑似包含侵权 / 违规内容，无法返回该内容"))
	if err == nil || !strings.Contains(err.Error(), "平台安全审核拦截") {
		t.Fatalf("got %v", err)
	}
}

func TestSanitizePlatformViolenceKeepsCleanScript(t *testing.T) {
	in := "【0-3秒】镜头：近景；韩铮说：「疼才证明我还活着。走，庆功。」"
	if HasPlatformViolence(in) {
		t.Fatalf("clean script should not flag")
	}
	if got := SanitizePlatformViolence(in); got != in {
		t.Fatalf("clean script should stay, got %s", got)
	}
}

func TestSanitizePlatformViolencePreservesDialogue(t *testing.T) {
	in := "镜头：杀手逼近；沈惜月说：「你到底是厨子，还是杀手？」\n韩铮说 {杀手不会答你。}"
	got := SanitizePlatformViolencePreserveDialogue(in)
	if !strings.Contains(got, "镜头：来人逼近") {
		t.Fatalf("visual direction should be sanitized, got %s", got)
	}
	if !strings.Contains(got, "「你到底是厨子，还是杀手？」") || !strings.Contains(got, "{杀手不会答你。}") {
		t.Fatalf("spoken dialogue must stay verbatim, got %s", got)
	}
}

func TestBuildVideoPromptDoesNotRewriteSpokenKiller(t *testing.T) {
	got := BuildVideoPrompt(VideoInput{
		Script:   "【7-10秒】镜头：沈惜月盯住门外杀手；沈惜月说：「你到底是厨子，还是杀手？」",
		Duration: 10,
		Ratio:    "9:16",
	})
	if !strings.Contains(got, "你到底是厨子，还是杀手？") {
		t.Fatalf("video prompt changed locked dialogue: %s", got)
	}
	if strings.Contains(got, "门外杀手") || !strings.Contains(got, "门外来人") {
		t.Fatalf("non-dialogue visual direction should still be sanitized: %s", got)
	}
}
