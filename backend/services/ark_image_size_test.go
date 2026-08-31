package services

import (
	"strings"
	"testing"
)

func TestNormalizeImageAspectCharacterIgnoresProjectRatio(t *testing.T) {
	if got := NormalizeImageAspect("9:16", "character"); got != "16:9" {
		t.Fatalf("character sheet should stay landscape, got %q", got)
	}
	if got := NormalizeImageAspect("9:16", "prop"); got != "16:9" {
		t.Fatalf("prop sheet should stay landscape, got %q", got)
	}
	if got := NormalizeImageAspect("9:16", "scene"); got != "9:16" {
		t.Fatalf("scene with explicit 9:16 aspect should stay 9:16, got %q", got)
	}
	if got := NormalizeImageAspect("16:9", "scene"); got != "16:9" {
		t.Fatalf("scene image job should stay 16:9 when requested, got %q", got)
	}
	if got := NormalizeImageAspect("9:16", "scene_grid"); got != "16:9" {
		t.Fatalf("scene 9-grid collage must stay landscape, got %q", got)
	}
	if got := NormalizeImageAspect("9:16", "motion_grid"); got != "16:9" {
		t.Fatalf("motion 9-grid collage must stay landscape, got %q", got)
	}
	if got := NormalizeImageAspect("9:16", "scene_reverse_skeleton"); got != "16:9" {
		t.Fatalf("reverse line drawing must stay landscape, got %q", got)
	}
	if got := NormalizeImageAspect("16:9", "scene_reverse"); got != "16:9" {
		t.Fatalf("reverse photoreal plate should stay 16:9 when requested, got %q", got)
	}
	if got := NormalizeImageAspect("16:9", "scene_panorama"); got != "2:1" {
		t.Fatalf("scene panorama must force 2:1, got %q", got)
	}
}

func TestIsSceneFloorPlanJob(t *testing.T) {
	if !IsSceneFloorPlanJob("监控门外 · 二维建筑平面布局图", "") {
		t.Fatal("name should match")
	}
	if !IsSceneFloorPlanJob("", "生成一张纯正交俯视二维建筑平面布局图") {
		t.Fatal("prompt should match")
	}
	if IsSceneFloorPlanJob("监控门外", "写实空镜，夜景石门") {
		t.Fatal("normal scene should not match")
	}
	got := withSceneFloorPlanConstraint("白底黑线")
	if !strings.Contains(got, "【最高优先级·CAD平面图】") || !strings.Contains(got, "白底黑线") {
		t.Fatalf("wrap failed: %s", got)
	}
}

func TestResolveArkImageSizeUsesPixels(t *testing.T) {
	if got := resolveArkImageSize("2k", "16:9"); got != "2560x1440" {
		t.Fatalf("2k 16:9 = %q", got)
	}
	if got := resolveArkImageSize("2k", "9:16"); got != "1440x2560" {
		t.Fatalf("2k 9:16 = %q", got)
	}
	if got := resolveArkImageSize("2k", "1:1"); got != "2048x2048" {
		t.Fatalf("2k 1:1 = %q", got)
	}
	if got := resolveArkImageSize("4k", "16:9"); got != "3840x2160" {
		t.Fatalf("4k 16:9 = %q", got)
	}
}
