package services

import (
	"strings"
	"testing"
)

func TestApplyDramaSkillGuidanceRoutesAndDeduplicatesStages(t *testing.T) {
	got := ApplyDramaSkillGuidance("原提示词", "storyboard", "storyboard", "video-prompts", "unknown")
	for _, want := range []string{"原提示词", "内置短剧技能 · 分镜", "内置短剧技能 · 视频提示词"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if strings.Count(got, "内置短剧技能 · 分镜") != 1 {
		t.Fatalf("stage guidance should be appended once: %s", got)
	}
}
