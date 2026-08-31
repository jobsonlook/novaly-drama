package services

import (
	"strings"
	"testing"
)

func TestBuildPerformanceInstructionBoostsDrama(t *testing.T) {
	got := buildPerformanceInstruction(
		"清晰可辨，决绝；语气坚定，关键词适度重读；仅调整语气，不改变原本声线质感",
		5,
	)
	for _, want := range []string{"情绪强度5/5", "最后一搏", "关键词明显重读"} {
		if !strings.Contains(got, want) {
			t.Fatalf("instruction missing %q: %s", want, got)
		}
	}
	for _, weak := range []string{"关键词适度重读", "仅调整语气"} {
		if strings.Contains(got, weak) {
			t.Fatalf("instruction still contains weak phrase %q: %s", weak, got)
		}
	}
}

func TestBuildPerformanceInstructionDefaultsToStrongEmotion(t *testing.T) {
	got := buildPerformanceInstruction("惊恐颤抖", 0)
	if !strings.Contains(got, "情绪强度4/5") || !strings.Contains(got, "致命危险") {
		t.Fatalf("unexpected instruction: %s", got)
	}
}
