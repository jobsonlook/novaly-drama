package services

import (
	"strings"
	"testing"

	"novaly/backend/models"
)

func TestRecurringCharacterNamesRequiresTwoMentions(t *testing.T) {
	script := "【0-3秒】太监甲(左后)、太监乙(右后)垂手。\n【3-7秒】太监甲(中后)上前；太监乙说：「是。」"
	got := RecurringCharacterNames(script, 2)
	want := map[string]bool{"太监甲": true, "太监乙": true}
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	for _, n := range got {
		if !want[n] {
			t.Fatalf("unexpected %q", n)
		}
	}
}

func TestMentionedCharacterNamesFromGrid(t *testing.T) {
	script := "【0-3秒】镜头：中景固定，小鹿(左前)媚笑，小南(右前)抿嘴笑，韩铮(中中)吃着串。\n【3-6秒】阿彪(右后)举杯；阿彪说：「敬铁腕！」"
	got := MentionedCharacterNames(script)
	want := map[string]bool{"小鹿": true, "小南": true, "韩铮": true, "阿彪": true}
	if len(got) != 4 {
		t.Fatalf("got %v", got)
	}
	for _, n := range got {
		if !want[n] {
			t.Fatalf("unexpected %q in %v", n, got)
		}
	}
}

func TestEnsureMentionedCharacterPicksAddsFrontRow(t *testing.T) {
	script := "【0-3秒】小鹿(左前)媚笑，小南(右前)抿嘴笑，韩铮(中中)吃串。\n【3-6秒】阿彪(右后)举杯。"
	candidates := []RefMatchCandidate{
		{ID: 1, Type: "scene", Name: "私人会所包厢"},
		{ID: 2, Type: "character", Name: "韩铮"},
		{ID: 3, Type: "character", Name: "阿彪"},
		{ID: 4, Type: "character", Name: "小鹿"},
		{ID: 5, Type: "character", Name: "小南"},
	}
	picks := []RefMatchPick{
		{ID: 1, Label: "私人会所包厢"},
		{ID: 2, Label: "韩铮"},
		{ID: 3, Label: "阿彪"},
	}
	got := EnsureMentionedCharacterPicks(picks, candidates, script)
	have := map[string]bool{}
	for _, p := range got {
		have[p.Label] = true
	}
	for _, n := range []string{"韩铮", "阿彪", "小鹿", "小南"} {
		if !have[n] {
			t.Fatalf("missing %s in %#v", n, got)
		}
	}
}

func TestEnsureMentionedCharacterPicksDoesNotCapAtFive(t *testing.T) {
	script := "【0-3秒】韩铮(左前)、小鹿(中前)、小南(右前)、阿彪(左中)、小嘉(中中)、林悦(右中)。"
	candidates := []RefMatchCandidate{
		{ID: 1, Type: "character", Name: "韩铮"},
		{ID: 2, Type: "character", Name: "小鹿"},
		{ID: 3, Type: "character", Name: "小南"},
		{ID: 4, Type: "character", Name: "阿彪"},
		{ID: 5, Type: "character", Name: "小嘉"},
		{ID: 6, Type: "character", Name: "林悦"},
	}
	got := EnsureMentionedCharacterPicks(nil, candidates, script)
	if len(got) != 6 {
		t.Fatalf("hanging refs must keep every named person, got %#v", got)
	}
}

func TestKeepCharacterFocusOpeningTwoShot(t *testing.T) {
	script := "【0-3秒】镜头：中景，韩铮(左中)3/4正面朝右，阿彪(右中)3/4正面朝左。\n【3-6秒】包厢灯色暧昧。\n【6-10秒】韩铮说：「走错了。」小嘉(左前)3/4正面朝右，小南(右前)3/4正面朝左；小嘉说：「请进。」小南说：「坐。」"
	names := []string{"韩铮", "小嘉", "小南", "阿彪"}
	flags := make([]CharacterFocus, len(names))
	for i, n := range names {
		flags[i] = AnalyzeCharacterFocus(n, script)
	}
	picked := KeepCharacterFocus(flags, 3)
	keep := map[string]bool{}
	for i, n := range names {
		if picked[i] {
			keep[n] = true
		}
	}
	if !keep["韩铮"] || !keep["阿彪"] {
		t.Fatalf("opening two-shot must keep 韩铮 and 阿彪, got %v flags %+v", keep, flags)
	}
	if keep["小嘉"] && keep["小南"] {
		t.Fatalf("should drop one later speaker for the 3-face cap, got %v", keep)
	}
}

func TestKeepCharacterFocusKeepsFiveNamed(t *testing.T) {
	script := "【0-3秒】韩铮(左中)，阿彪(右中)，小嘉(左前)，小南(右前)，小雁(中后)。"
	names := []string{"韩铮", "阿彪", "小嘉", "小南", "小雁"}
	flags := make([]CharacterFocus, len(names))
	for i, n := range names {
		flags[i] = AnalyzeCharacterFocus(n, script)
	}
	picked := KeepCharacterFocus(flags, MaxNamedCharacterRefs)
	for i, n := range names {
		if !picked[i] {
			t.Fatalf("named person %s should keep a sheet under the 5-face cap", n)
		}
	}
}

func TestCapNamedCharacterVideoRefsKeepsOpeningGrid(t *testing.T) {
	script := "【0-3秒】镜头：中景，韩铮(左中)3/4正面朝右，阿彪(右中)3/4正面朝左。\n【6-10秒】小嘉说：「请进。」小南说：「坐。」"
	refs := []VideoRef{
		{Kind: "character", Label: "韩铮", Resource: models.Resource{ID: 1, Type: "character", Name: "韩铮", ImagePath: "han.jpg"}},
		{Kind: "character", Label: "小嘉", Resource: models.Resource{ID: 2, Type: "character", Name: "小嘉", ImagePath: "jia.jpg"}},
		{Kind: "character", Label: "小南", Resource: models.Resource{ID: 3, Type: "character", Name: "小南", ImagePath: "nan.jpg"}},
		{Kind: "character", Label: "阿彪", Resource: models.Resource{ID: 4, Type: "character", Name: "阿彪", ImagePath: "biao.jpg"}},
	}
	got := capNamedCharacterVideoRefs(refs, script)
	names := map[string]bool{}
	for _, r := range got {
		names[r.Label] = true
	}
	if !names["韩铮"] || !names["阿彪"] {
		t.Fatalf("opening grid two-shot must keep 韩铮 and 阿彪, got %#v", names)
	}
	if !names["小嘉"] || !names["小南"] {
		t.Fatalf("all named people should keep a sheet, got %#v", names)
	}
}

func TestScriptHasSpatialSlot(t *testing.T) {
	if !ScriptHasSpatialSlot("中景固定，韩铮(左前)3/4正面朝右对峙，阿彪(右中)3/4正面朝左") {
		t.Fatal("nine-grid slot should match")
	}
	if !ScriptHasSpatialSlot("韩铮站在画面左侧，阿彪在画面右侧") {
		t.Fatal("画面左/右 cue should match")
	}
	if ScriptHasSpatialSlot("【0-3秒】韩铮看向阿彪。") {
		t.Fatal("plain two-shot without slots should not match")
	}
}

func TestBuildVideoPromptSpatialGrid(t *testing.T) {
	duo := BuildVideoPrompt(VideoInput{
		Script: "【0-3秒】镜头：中景固定，韩铮(左前)3/4正面朝右对峙，阿彪(右中)3/4正面朝左。",
		Ratio:  "16:9",
		Refs: []VideoRef{
			{Kind: "character", Label: "韩铮", Resource: models.Resource{ID: 1, Type: "character", Name: "韩铮", ImagePath: "han.jpg"}},
			{Kind: "character", Label: "阿彪", Resource: models.Resource{ID: 2, Type: "character", Name: "阿彪", ImagePath: "biao.jpg"}},
		},
	})
	for _, part := range []string{"站位网格", "3×3", "按文案格子"} {
		if !strings.Contains(duo, part) {
			t.Fatalf("duo shot missing %q:\n%s", part, duo)
		}
	}

	solo := BuildVideoPrompt(VideoInput{
		Script: "【0-3秒】韩铮抬头。",
		Ratio:  "16:9",
		Refs: []VideoRef{
			{Kind: "character", Label: "韩铮", Resource: models.Resource{ID: 1, Type: "character", Name: "韩铮", ImagePath: "han.jpg"}},
		},
	})
	if strings.Contains(solo, "站位网格") {
		t.Fatalf("solo shot should not hang spatial grid:\n%s", solo)
	}

	crowd := BuildVideoPrompt(VideoInput{
		Script: "【0-3秒】小鹿小南尖叫。",
		Ratio:  "16:9",
		Refs: []VideoRef{
			{Kind: "character", Label: "韩铮", Resource: models.Resource{ID: 1, Type: "character", Name: "韩铮", ImagePath: "han.jpg"}},
			{Kind: "scene", Label: "站位图", Resource: models.Resource{ID: 9, Type: "scene", Name: "包厢 · 站位图", ImagePath: "pos.jpg", GenType: "positioning"}},
		},
	})
	if !strings.Contains(crowd, "站位网格") {
		t.Fatalf("positioning shot should hang spatial grid even with one named face:\n%s", crowd)
	}
	if !strings.Contains(crowd, "按站位参考图") && !strings.Contains(crowd, "以站位图为准") {
		t.Fatalf("positioning shot should prefer the map over script grid:\n%s", crowd)
	}
}
