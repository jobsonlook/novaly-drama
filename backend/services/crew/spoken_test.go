package crew

import (
	"strings"
	"testing"
)

func TestExtractSpokenLinesUserScriptQuotes(t *testing.T) {
	script := `场次三
场景：内景 · 柴房 · 日
出场人物：韩铮，福公公
△ (中景) 福公公单膝跪地，姿态恭敬。
福公公（低）: "福安，参见……小灶公子。"
△ (特写) 他一把拉开柴门。
韩铮（后退撞上柴堆）: "别。2024了，角色扮演请加钱。我是韩铮，北城——等等。"
`
	got := ExtractSpokenLines(script)
	if len(got) != 2 {
		t.Fatalf("want 2 lines, got %#v", got)
	}
	if got[0].Speaker != "福公公" || got[0].Text != "福安，参见……小灶公子。" {
		t.Fatalf("line0: %#v", got[0])
	}
	if got[1].Speaker != "韩铮" || got[1].Text != "别。2024了，角色扮演请加钱。我是韩铮，北城——等等。" {
		t.Fatalf("line1: %#v", got[1])
	}
}

func TestExtractSpokenLinesMarkdownEpisodeAndInlineDialogue(t *testing.T) {
	script := `# 第3集：韩家的孩子
△ 顾满仓起哄：「杂役也配碰师傅的方子？」
**韩铮**（低声）："韩家的孩子……先活下去。福公你话说一半就跑，差评。"
△ 腰牌上的「韩」字发烫。
`
	got := ExtractSpokenLines(script)
	if len(got) != 2 {
		t.Fatalf("want inline and markdown dialogue only, got %#v", got)
	}
	if got[0].Speaker != "顾满仓" || got[0].Text != "杂役也配碰师傅的方子？" {
		t.Fatalf("inline dialogue: %#v", got[0])
	}
	if got[1].Speaker != "韩铮" || !strings.Contains(got[1].Text, "福公你话说一半") {
		t.Fatalf("markdown dialogue: %#v", got[1])
	}
}

func TestExtractInlineDialogueUsesImmediateSpeakerBeforeVerb(t *testing.T) {
	script := `△ 灶下。原身被按在水缸边，姚三刀把脏抹布塞进他嘴里，顾满仓起哄：「杂役也配碰师傅的方子？」`
	got := ExtractSpokenLines(script)
	if len(got) != 1 || got[0].Speaker != "顾满仓" {
		t.Fatalf("inline speaker must be the name immediately before 起哄: %#v", got)
	}
}

func TestRestoreShotContextsKeepsAppendedDialogueShots(t *testing.T) {
	shots := []ShotContext{{ID: 7, Label: "已有", Duration: 10, Script: "【0-3秒】镜头：近景；韩铮说：「第一句。」"}}
	got := RestoreShotContextDialogue(shots, "韩铮：\"第一句。\"\n福公公：\"身世关键句。\"\n")
	if len(got) != 2 {
		t.Fatalf("appended dialogue context was dropped: %#v", got)
	}
	if got[1].ID != 0 || !strings.Contains(got[1].Script, "身世关键句") {
		t.Fatalf("bad appended context: %#v", got[1])
	}
}

func TestScheduleStoryboardDialogueOwnsOrderSpeakerAndOverflow(t *testing.T) {
	script := `**韩铮**（低声）："韩家的孩子……先活下去。福公你话说一半就跑，差评。"
**裴长河**："小灶？人呢，说你惹了祸——你还敢回膳房？"`
	shots := []StoryboardShot{
		{Label: "韩铮藏身", Duration: 10, SceneName: "侧库", Script: "【0-3秒】镜头：腰牌特写；顾满仓说：「那小子不是韩小灶吗？」\n【3-10秒】镜头：韩铮低头；韩铮说：「韩家的孩子……先活下去。」"},
		{Label: "裴长河进门", Duration: 10, SceneName: "侧库", Script: "【0-10秒】镜头：裴长河探头；裴长河说：「小灶？人呢，」"},
	}
	got := ScheduleStoryboardDialogue(shots, script, nil)
	if len(got) > 3 {
		t.Fatalf("dialogue should reuse visual beats before adding continuations, got %d shots", len(got))
	}
	var quotes []string
	for _, shot := range got {
		quotes = append(quotes, quotesInScript(shot.Script)...)
		if strings.Contains(shot.Script, "顾满仓说") {
			t.Fatalf("model-invented/misplaced dialogue survived: %s", shot.Script)
		}
		for _, q := range quotesInScript(shot.Script) {
			if speechRunes(q) > 18 {
				t.Fatalf("dialogue chunk exceeds deterministic budget: %q", q)
			}
		}
	}
	joined := normalizeQuoteKey(strings.Join(quotes, ""))
	want := normalizeQuoteKey("韩家的孩子……先活下去。福公你话说一半就跑，差评。小灶？人呢，说你惹了祸——你还敢回膳房？")
	if joined != want {
		t.Fatalf("dialogue must be complete and ordered\n got: %s\nwant: %s", joined, want)
	}
	if !strings.Contains(strings.Join(storyboardScripts(got), "\n"), "裴长河说") {
		t.Fatal("speaker attribution was not rebuilt")
	}
}

func TestPackStoryboardShotsPreservesShortContinuationTail(t *testing.T) {
	shots := []StoryboardShot{{Duration: 10, Script: "【0-5秒】镜头：近景；韩铮说：「韩家的孩子……先活下去。福公你话说一半就跑，」\n【5-10秒】镜头：换景别；韩铮说：「差评。」"}}
	got := PackStoryboardShotsPreservingDialogue(shots)
	if len(got) != 1 || !strings.Contains(got[0].Script, "差评。") {
		t.Fatalf("meaningful short tail was removed: %#v", got)
	}
}

func storyboardScripts(shots []StoryboardShot) []string {
	out := make([]string, len(shots))
	for i := range shots {
		out[i] = shots[i].Script
	}
	return out
}

func TestRestoreStoryboardDialogueKeepsOriginalWords(t *testing.T) {
	script := `韩铮（冷）: "别。2024了，角色扮演请加钱。"
福公公（低）: "福安，参见小灶公子。"
`
	shots := []StoryboardShot{{
		Label:  "柴房",
		Script: "【0-3秒】镜头：中景；音效：鼓点；韩铮说：「别过来，这是角色扮演。」\n【3-7秒】镜头：福公公跪地；音效：鼓点；福公公说：「给您请安。」\n【7-10秒】镜头：余韵；音效：鼓点",
	}}
	got := RestoreStoryboardDialogue(shots, script)
	if len(got) != 1 {
		t.Fatalf("want 1 shot, got %d", len(got))
	}
	if !strings.Contains(got[0].Script, "别。2024了，角色扮演请加钱。") {
		t.Fatalf("missing han zheng line: %s", got[0].Script)
	}
	if !strings.Contains(got[0].Script, "福安，参见小灶公子。") {
		t.Fatalf("missing fu line: %s", got[0].Script)
	}
	if strings.Contains(got[0].Script, "别过来") {
		t.Fatal("paraphrase should be replaced")
	}
}

func TestRestoreStoryboardDialogueFixesWrongSpeaker(t *testing.T) {
	script := `韩铮（自语）: "……舌头？你也穿越了？"
姚三刀（画外）: "不可能！他怎么还活着？！顾满仓——再找人！"
`
	shots := []StoryboardShot{{
		Label:  "夹道",
		Script: "【0-3秒】镜头：中景；姚三刀说：「……舌头？你也穿越了？」\n【3-7秒】镜头：余韵；音效：鼓点\n【7-10秒】镜头：停；音效：鼓点",
	}}
	got := RestoreStoryboardDialogue(shots, script)
	if !strings.Contains(got[0].Script, "韩铮说：「……舌头？你也穿越了？」") {
		t.Fatalf("speaker should be restored to 韩铮:\n%s", got[0].Script)
	}
	if strings.Contains(got[0].Script, "姚三刀说：「……舌头") {
		t.Fatalf("wrong speaker should be gone:\n%s", got[0].Script)
	}
}

func TestRestoreAppendsDroppedLines(t *testing.T) {
	script := `韩铮: "第一句。"
福公公: "第二句。"
`
	shots := []StoryboardShot{{
		Label:  "一",
		Script: "【0-3秒】镜头：近景；韩铮说：「第一句改了。」\n【3-7秒】镜头：停；音效：风\n【7-10秒】镜头：停；音效：风",
	}}
	got := RestoreStoryboardDialogue(shots, script)
	if len(got) != 2 {
		t.Fatalf("want appended shot, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[1].Script, "第二句。") {
		t.Fatalf("missing dropped line: %s", got[1].Script)
	}
}

func TestRestoreSkipsExpandingShortTailIntoLongLine(t *testing.T) {
	full := "先去洗菜。今日小都王生辰备料，错一份，咱们全膳房喝西北风。记住—少惹姚三刀。"
	script := "裴长河: \"" + full + "\"\n"
	shots := []StoryboardShot{{
		Label:  "侧库",
		Script: "【0-3秒】镜头：特写；裴长河说：「少惹姚三刀。」\n【3-7秒】镜头：反应；音效：鼓点\n【7-10秒】镜头：余韵；音效：鼓点",
	}}
	got := RestoreStoryboardDialogue(shots, script)
	if strings.Contains(got[0].Script, "先去洗菜") {
		t.Fatalf("short reminder tail must not expand into full lecture inside 0-3s beat:\n%s", got[0].Script)
	}
	if !strings.Contains(got[0].Script, "少惹姚三刀") {
		t.Fatalf("model tail should stay:\n%s", got[0].Script)
	}
	for i := 1; i < len(got); i++ {
		if strings.Contains(got[i].Script, "少惹姚三刀") && !strings.Contains(got[i].Script, "先去洗菜") {
			t.Fatalf("leftover must not re-append reminder-only shot:\n%s", got[i].Script)
		}
	}
}
