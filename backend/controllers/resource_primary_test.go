package controllers

import (
	"strings"
	"testing"

	"novaly/backend/models"
)

func TestAppendPromptRevisionKeepsBase(t *testing.T) {
	base := "已提供 2 张参考图，生成定妆照。\n角色要求：青年拳手"
	got := appendPromptRevision(base, "去掉脖子伤痕")
	if !strings.HasPrefix(got, promptRevisionMarker) {
		t.Fatalf("revision should come first, got %q", got)
	}
	if !strings.Contains(got, promptOriginalMarker) || !strings.Contains(got, base) {
		t.Fatalf("original prompt should remain, got %q", got)
	}
	if !strings.Contains(got, "去掉脖子伤痕") {
		t.Fatalf("missing revision text, got %q", got)
	}
	idxRev := strings.Index(got, "去掉脖子伤痕")
	idxBase := strings.Index(got, base)
	if idxRev < 0 || idxBase < 0 || idxRev > idxBase {
		t.Fatalf("revision should precede original, got %q", got)
	}
	if appendPromptRevision(base, "") != base {
		t.Fatal("empty revision should keep the original prompt")
	}
}

func TestUnwrapStoredGenPromptStripsStackedRevisions(t *testing.T) {
	messy := promptRevisionMarker + `
把图2人物脖子上的奖牌换成图1那枚圆形金属奖牌，面部特写和正面/侧面/背面全身都要戴上，面容服装其余不变。

` + promptOriginalMarker + `
已提供 2 张参考图，请融合角色外貌与服装特征，生成专业影视角色设定参考图。
角色要求：浅灰色衬衫。
不要logo

` + promptRevisionMarkerLegacy + `
把图2人物脖子上的奖牌换一下，换成图1`
	got := unwrapStoredGenPrompt(messy)
	if strings.Contains(got, "本次修改") || strings.Contains(got, "原定妆照要求") {
		t.Fatalf("markers should be stripped, got %q", got)
	}
	if !strings.Contains(got, "已提供 2 张参考图") || !strings.Contains(got, "浅灰色衬衫") {
		t.Fatalf("original sheet prompt missing, got %q", got)
	}
	if strings.Contains(got, "把图2人物脖子上的奖牌换一下") {
		t.Fatalf("legacy revision should be stripped, got %q", got)
	}
}

func TestApplyPreservePromptForGenUsesRevisionOnlyButKeepsSavedPromptForPersistence(t *testing.T) {
	input := imageGenJobInput{
		Description:    "旧定妆照长提示词",
		Revision:       "把图2脖子上的奖牌换成图1",
		PreservePrompt: true,
		RawPrompt:      true,
		SavedPrompt:    "旧定妆照长提示词",
	}
	applyPreservePromptForGen(&input)
	if input.Description != "把图2脖子上的奖牌换成图1" {
		t.Fatalf("image edit should send only the one-time revision, got %q", input.Description)
	}
	if input.RawPrompt {
		t.Fatal("revision should use the image-edit prompt wrapper, not resend the saved full prompt raw")
	}
	input.Revision = ""
	input.Description = "should be replaced"
	input.RawPrompt = false
	applyPreservePromptForGen(&input)
	if input.Description != "旧定妆照长提示词" || !input.RawPrompt {
		t.Fatalf("empty revision should re-roll saved prompt raw, got %q raw=%v", input.Description, input.RawPrompt)
	}
}

func TestCandidateOnlyNeverResolvesCanonicalFill(t *testing.T) {
	rc := &ResourceController{}
	if _, ok := rc.resolveFillResource(1, "character", "韩铮", candidatePersistMeta{
		FillResourceID: 99,
		CandidateOnly:  true,
	}); ok {
		t.Fatal("candidate-only regeneration must not resolve or overwrite the canonical resource")
	}
}

func TestParseCandidateBase(t *testing.T) {
	cases := []struct {
		name string
		base string
		ok   bool
	}{
		{"韩铮 · 候选1", "韩铮", true},
		{"韩铮·候选1", "韩铮", true},
		{"韩铮", "", false},
		{"韩铮 · 候选2", "韩铮", true},
	}
	for _, tc := range cases {
		base, ok := parseCandidateBase(tc.name)
		if ok != tc.ok || base != tc.base {
			t.Fatalf("%q: got (%q, %v), want (%q, %v)", tc.name, base, ok, tc.base, tc.ok)
		}
	}
}

func TestResourceLibraryGroupKeySameForExtractAndCandidate(t *testing.T) {
	extract := models.Resource{Type: "character", Name: "韩铮"}
	candidate := models.Resource{Type: "character", Name: "韩铮 · 候选1"}
	if resourceLibraryGroupKey(extract) == "" || resourceLibraryGroupKey(extract) != resourceLibraryGroupKey(candidate) {
		t.Fatalf("extract %q vs candidate %q", resourceLibraryGroupKey(extract), resourceLibraryGroupKey(candidate))
	}
	parent := uint(9)
	derived := models.Resource{Type: "character", Name: "拳台装", ParentID: &parent}
	if resourceLibraryGroupKey(extract) == resourceLibraryGroupKey(derived) {
		t.Fatal("derivative should not share the base group")
	}
}

func TestPickCanonicalPrefersExtractName(t *testing.T) {
	extract := models.Resource{ID: 1, Type: "character", Name: "韩铮"}
	candidate := models.Resource{ID: 2, Type: "character", Name: "韩铮 · 候选1"}
	got := pickCanonicalInGroup(candidate, []models.Resource{extract, candidate})
	if got.ID != extract.ID {
		t.Fatalf("canonical id=%d, want extract %d", got.ID, extract.ID)
	}
}
