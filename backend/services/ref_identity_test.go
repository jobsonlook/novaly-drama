package services

import (
	"strings"
	"testing"

	"novaly/backend/models"
)

func TestResourceQueryMatchesDerivativeAliases(t *testing.T) {
	cases := []struct {
		name, parent, query string
		want                bool
	}{
		{"赤膊战损", "韩铮", "赤膊战损", true},
		{"赤膊战损", "韩铮", "韩铮 · 赤膊战损", true},
		{"赤膊战损", "韩铮", "韩铮（赤膊战损）", true},
		{"赤膊战损", "韩铮", "韩铮", false},
		{"韩铮", "", "韩铮", true},
		{"韩铮", "", "赤膊战损", false},
		{"阿彪", "", "韩铮", false},
	}
	for _, tc := range cases {
		if got := ResourceQueryMatches(tc.name, tc.parent, tc.query); got != tc.want {
			t.Fatalf("ResourceQueryMatches(%q,%q,%q)=%v, want %v", tc.name, tc.parent, tc.query, got, tc.want)
		}
	}
}

func TestResourceIdentityLabel(t *testing.T) {
	parent := uint(1)
	got := ResourceIdentityLabel(models.Resource{Name: "赤膊战损", ParentID: &parent, ParentName: "韩铮"})
	if got != "韩铮（赤膊战损）" {
		t.Fatalf("got %q", got)
	}
	if ResourceIdentityLabel(models.Resource{Name: "韩铮"}) != "韩铮" {
		t.Fatal("base character should stay bare name")
	}
}

func TestNormalizeVideoRefsDropsParentAndRewritesLabel(t *testing.T) {
	parentID := uint(10)
	childID := uint(11)
	parent := models.Resource{ID: parentID, Type: "character", Name: "韩铮", StylizedImagePath: "s1.png"}
	child := models.Resource{
		ID: childID, Type: "character", Name: "赤膊战损", ParentID: &parentID, ParentName: "韩铮",
		ImagePath: "c-orig.png", StylizedImagePath: "c-sty.png",
	}
	out := NormalizeVideoRefs([]VideoRef{
		{Resource: parent, Kind: "character", Variant: "original", Label: "韩铮"},
		{Resource: child, Kind: "character", Variant: "original", Label: "赤膊战损"},
		{Resource: models.Resource{ID: 20, Type: "scene", Name: "更衣室", ImagePath: "room.png"}, Kind: "scene", Variant: "original", Label: "更衣室"},
	})
	if len(out) != 2 {
		t.Fatalf("len=%d, want 2 (parent dropped)", len(out))
	}
	if out[0].Resource.ID != childID {
		t.Fatalf("first remaining id=%d, want child", out[0].Resource.ID)
	}
	if out[0].Label != "韩铮（赤膊战损）" {
		t.Fatalf("child label=%q", out[0].Label)
	}
	if out[0].Variant != "stylized" {
		t.Fatalf("child variant=%q, want stylized", out[0].Variant)
	}
}

func TestBuildVideoPromptToonflowIdentity(t *testing.T) {
	parentID := uint(10)
	child := models.Resource{
		ID: 11, Type: "character", Name: "赤膊战损", ParentID: &parentID, ParentName: "韩铮",
		ImagePath: "c.png", StylizedImagePath: "c-s.png",
	}
	got := BuildVideoPrompt(VideoInput{
		Script: "【0-3秒】韩铮把手伸进冰水桶。",
		Refs: []VideoRef{
			{Resource: models.Resource{ID: parentID, Type: "character", Name: "韩铮", StylizedImagePath: "p.png"}, Kind: "character", Label: "韩铮"},
			{Resource: child, Kind: "character", Label: "赤膊战损"},
			{Resource: models.Resource{ID: 20, Type: "scene", Name: "更衣室", ImagePath: "room.png"}, Kind: "scene", Label: "更衣室"},
		},
		Ratio: "16:9",
	})
	for _, part := range []string{
		"主体定义：",
		"韩铮（赤膊战损）",
		"<主体1>",
		"<场景1>",
		"把手伸进冰水桶",
		"画面质感：",
		"人物面部稳定不变形",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("prompt missing %q:\n%s", part, got)
		}
	}
	if strings.Contains(got, "图2为赤膊战损") {
		t.Fatalf("old nameless derivative legend:\n%s", got)
	}
	if strings.Contains(got, "参考图：图") {
		t.Fatalf("old legend format still present:\n%s", got)
	}
	if !strings.Contains(got, "配饰锁定") || !strings.Contains(got, "参考图没有奖牌") {
		t.Fatalf("missing accessory lock:\n%s", got)
	}
}
