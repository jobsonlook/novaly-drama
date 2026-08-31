package services

import (
	"strings"
	"testing"
)

func TestBuildScenePanoramaPromptHasEquirectRules(t *testing.T) {
	got := BuildScenePanoramaPrompt("大朗皇宫柴房", "木结构室内，柴堆与木桌")
	for _, part := range []string{"2:1", "等距柱状", "360", "大朗皇宫柴房", "空镜无人", "九宫格", "格1", "格5", "格7"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in:\n%s", part, got)
		}
	}
}

func TestNormalizeImageAspectScenePanorama(t *testing.T) {
	if got := NormalizeImageAspect("16:9", "scene_panorama"); got != "2:1" {
		t.Fatalf("scene_panorama must force 2:1, got %q", got)
	}
	if got := NormalizeImageAspect("2:1", "scene"); got != "2:1" {
		t.Fatalf("explicit 2:1 should pass, got %q", got)
	}
}

func TestResolveArkImageSize2to1(t *testing.T) {
	// Seedream rejects sizes under 3686400 pixels; 2560x1280 fails that check.
	if got := resolveArkImageSize("2k", "2:1"); got != "2880x1440" {
		t.Fatalf("2k 2:1 = %q", got)
	}
	if got := resolveArkImageSize("4k", "2:1"); got != "3840x1920" {
		t.Fatalf("4k 2:1 = %q", got)
	}
	if got := resolveArkImageSize("1k", "2:1"); got != "2880x1440" {
		t.Fatalf("1k 2:1 should also clear Seedream's pixel floor, got %q", got)
	}
}
