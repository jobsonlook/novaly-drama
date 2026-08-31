package crew

import (
	"strings"
	"testing"
)

func TestSplitScriptOverflowMovesPastMax(t *testing.T) {
	script := "【0-3秒】镜头：中景，阿彪把衬衫扔向韩铮；阿彪说：「铮哥，今晚庆功局订好了。」\n" +
		"【3-7秒】镜头：近景，韩铮嘴角上扬；韩铮说：「断财路？那叫市场调控。谁不服，排队。」\n" +
		"【7-10秒】镜头：中景，韩铮展开衬衫；韩铮说：「我给你再调控一次。」\n" +
		"【10-13秒】镜头：阿彪反应；阿彪说：「我去跟那边传话——」"
	keep, overflow := SplitScriptOverflow(script, 10)
	if strings.Contains(keep, "10-13") || strings.Contains(keep, "传话") {
		t.Fatalf("keep should stop at 10s, got %s", keep)
	}
	if !strings.Contains(keep, "再调控一次") {
		t.Fatalf("keep should retain 7-10s beat, got %s", keep)
	}
	if !strings.Contains(overflow, "阿彪说：「我去跟那边传话——」") {
		t.Fatalf("overflow should move last beat, got %s", overflow)
	}
	if !strings.Contains(overflow, "【0-3秒】") || strings.Contains(overflow, "【10-13秒】") {
		t.Fatalf("overflow should remap to 0, got %s", overflow)
	}
}

func TestSplitScriptOverflowMovesStraddlingBeat(t *testing.T) {
	script := "【0-3秒】镜头：A。\n【3-6秒】镜头：B。\n【6-9秒】镜头：C。\n【9-13秒】镜头：韩铮说：「闻见了。」"
	keep, overflow := SplitScriptOverflow(script, 10)
	if strings.Contains(keep, "9-13") || strings.Contains(keep, "闻见了") {
		t.Fatalf("straddling 【9-13秒】 must leave this shot, got keep=%s", keep)
	}
	if ScriptEndSeconds(keep) > 10 {
		t.Fatalf("keep end %d > 10: %s", ScriptEndSeconds(keep), keep)
	}
	if !strings.Contains(overflow, "闻见了") || !strings.Contains(overflow, "【0-") {
		t.Fatalf("overflow should hold remapped beat, got %s", overflow)
	}
}

func TestSplitScriptOverflowMovesOrphanDialogue(t *testing.T) {
	script := "【0-3秒】镜头：A。\n【3-7秒】镜头：B。\n【7-10秒】镜头：C。\n韩铮说：「韩家的孩子……先活下去。差评。」"
	keep, overflow := SplitScriptOverflow(script, 10)
	if strings.Contains(keep, "差评") || strings.Contains(keep, "韩家的孩子") {
		t.Fatalf("orphan dialogue after 10s must move, got keep=%s", keep)
	}
	if !strings.Contains(overflow, "差评") {
		t.Fatalf("overflow should hold orphan line, got %s", overflow)
	}
}

func TestPackShotContextsMovesOverflowToNext(t *testing.T) {
	shots := PackShotContexts([]ShotContext{
		{ID: 1, Index: 1, Duration: 10, Script: "【0-3秒】镜头：A。\n【3-7秒】镜头：B。\n【7-10秒】镜头：C。\n【10-13秒】镜头：阿彪反应；阿彪说：「我去跟那边传话——」"},
		{ID: 2, Index: 2, Duration: 10, Script: "【0-3秒】镜头：更衣室门口。\n【3-7秒】镜头：走廊。\n【7-10秒】镜头：停下。"},
	})
	if len(shots) < 2 {
		t.Fatalf("expected at least 2 shots, got %d", len(shots))
	}
	if strings.Contains(shots[0].Script, "传话") || ScriptEndSeconds(shots[0].Script) > 10 {
		t.Fatalf("shot 1 should stay within 10s, got %s", shots[0].Script)
	}
	if !strings.Contains(shots[1].Script, "传话") {
		t.Fatalf("overflow should land on next shot, got %s", shots[1].Script)
	}
	if issues := detectDurationOverflow(shots); len(issues) != 0 {
		t.Fatalf("packed shots should stay within 10s, got %#v", issues)
	}
	if len(shots) != 3 {
		t.Fatalf("expected cascade into 3 shots, got %d: %#v", len(shots), scriptsOf(shots))
	}
	if !strings.Contains(shots[2].Script, "停下") {
		t.Fatalf("original next-shot tail should cascade, got %s", shots[2].Script)
	}
}

func TestPackShotContextsCreatesContinuation(t *testing.T) {
	shots := PackShotContexts([]ShotContext{{
		ID: 1, Index: 1, Duration: 10, Label: "更衣室",
		Script: "【0-3秒】镜头：A。\n【7-10秒】镜头：C。\n【10-13秒】镜头：阿彪说：「我去跟那边传话——」",
	}})
	if len(shots) != 2 {
		t.Fatalf("expected continuation shot, got %d: %#v", len(shots), scriptsOf(shots))
	}
	if issues := detectDurationOverflow(shots); len(issues) != 0 {
		t.Fatalf("packed shots should stay within 10s, got %#v", issues)
	}
	if shots[1].ID != 0 || !strings.Contains(shots[1].Script, "传话") {
		t.Fatalf("new shot should hold overflow, got %#v", shots[1])
	}
	if !strings.Contains(shots[1].Label, "续") {
		t.Fatalf("continuation label, got %s", shots[1].Label)
	}
}

func TestPackDuplicateSevenToTenMovesToNext(t *testing.T) {
	shots := PackShotContexts([]ShotContext{
		{ID: 1, Index: 1, Duration: 10, Script: "【0-3秒】镜头：阿彪说：「铮哥，今晚庆功局订好了。」\n" +
			"【3-7秒】镜头：韩铮说：「断财路？那叫市场调控。」\n" +
			"【7-10秒】镜头：韩铮说：「我给你再调控一次。」\n" +
			"【7-10秒】镜头：反应；韩铮说：「你断了人家的财路，小心点。」"},
		{ID: 2, Index: 2, Duration: 10, Script: "【0-3秒】镜头：更衣室门口。"},
	})
	if strings.Count(shots[0].Script, "【7-10秒】") != 1 {
		t.Fatalf("shot 1 should keep one 7-10 beat, got %s", shots[0].Script)
	}
	if strings.Contains(shots[0].Script, "财路，小心") {
		t.Fatalf("overlapping beat should leave this shot, got %s", shots[0].Script)
	}
	if !strings.Contains(shots[1].Script, "财路，小心") {
		t.Fatalf("overlapping beat should move to next shot, got %s", shots[1].Script)
	}
}

func TestPackDropsDuplicateOverflowQuote(t *testing.T) {
	line := "拳手甲说：「哥，心脏那老毛病又疼？」"
	shots := PackShotContexts([]ShotContext{
		{ID: 1, Index: 1, Duration: 10, Script: "【0-3秒】镜头：扣扣子。\n【3-6秒】镜头：按左胸。\n【6-10秒】镜头：中景；" + line + "\n【6-10秒】镜头：中景；" + line},
		{ID: 2, Index: 2, Duration: 10, Script: "【0-3秒】镜头：韩铮说：「疼才证明我还活着。走，庆功。」"},
	})
	if strings.Count(shots[0].Script+shots[1].Script, "心脏那老毛病") != 1 {
		t.Fatalf("duplicate quote should not be copied onto next shot, got %#v", scriptsOf(shots))
	}
	if !strings.Contains(shots[1].Script, "疼才证明我还活着") {
		t.Fatalf("next shot original line should stay, got %s", shots[1].Script)
	}
}

func TestDetectOverlappingBeats(t *testing.T) {
	issues := detectDurationOverflow([]ShotContext{{
		ID: 1, Index: 1, Duration: 10,
		Script: "【7-10秒】镜头：A。\n【7-10秒】镜头：B。",
	}})
	if !hasQCMessage(issues, "时序重叠") {
		t.Fatalf("expected overlapping R8, got %#v", issues)
	}
}

func TestSplitEmbeddedBeatLines(t *testing.T) {
	merged := "【0-3秒】镜头：过肩近景，裴长河(右后)看向韩铮；音效：低沉鼓点；韩铮说：【3-7秒】镜头：中景固定，裴长河盯着韩铮；音效：低沉鼓点；【7-10秒】镜头：近景，裴长河开口；裴长河说：「师傅！我这不是……菜没切完嘛。」"
	got := splitEmbeddedBeatLines(merged)
	if strings.Contains(got, "说：【3-7秒】") {
		t.Fatalf("embedded headers must split to new lines, got:\n%s", got)
	}
	if strings.Count(got, "\n") < 2 {
		t.Fatalf("expected multiple lines, got:\n%s", got)
	}
}

func TestFinalizeFixesMergedBeatsAndNo310Overflow(t *testing.T) {
	merged := "【0-3秒】镜头：中景固定，内景御膳房侧库房；音效：脚步声；裴长河说：「小灶？人呢——」【3-7秒】镜头：摇镜对准韩铮；音效：低沉鼓点；【7-10秒】镜头：近景，韩铮调整表情；音效：低沉鼓点"
	got := FinalizeShotScript(merged, 10)
	if strings.Contains(got, "说：【3-7秒】") {
		t.Fatalf("dangling speaker/embedded beat remains:\n%s", got)
	}
	if strings.Contains(got, "【3-10秒】") || strings.Contains(got, "【10-") {
		t.Fatalf("must not append overlapping 3-10 overflow beat, got:\n%s", got)
	}
	if !strings.Contains(got, "小灶") {
		t.Fatalf("first line dialogue lost:\n%s", got)
	}
	if ScriptEndSeconds(got) != 10 {
		t.Fatalf("expected end=10, got %d:\n%s", ScriptEndSeconds(got), got)
	}
}

func TestCollapseSFXOnlyBeatIntoPrevious(t *testing.T) {
	script := "【0-3秒】镜头：中景，姚三刀与顾满仓对视后齐声大笑，顾满仓(左前)3/4正面朝右轻蔑；音效：紧张鼓点、笑声；顾满仓说：「你傻啊？」\n" +
		"【3-7秒】镜头：近景固定，姚三刀歪头语气刻薄；音效：紧张鼓点；姚三刀说：「……姚爷你都不认？」\n" +
		"【7-10秒】音效：紧张鼓点"
	got := FinalizeShotScript(script, 10)
	if strings.Contains(got, "【7-10秒】") && strings.Count(got, "紧张鼓点") > 0 {
		// Either merged away the third row or third row has 镜头
		if strings.Contains(got, "【7-10秒】音效") && !strings.Contains(got, "【7-10秒】镜头") {
			t.Fatalf("must not keep sfx-only third beat, got:\n%s", got)
		}
	}
	if !strings.Contains(got, "姚爷你都不认") {
		t.Fatalf("dialogue must stay, got:\n%s", got)
	}
	if ScriptEndSeconds(got) != 10 {
		t.Fatalf("expected end=10, got %d:\n%s", ScriptEndSeconds(got), got)
	}
}

func TestCollapsePlotlessReactionBeat(t *testing.T) {
	script := "【0-3秒】镜头：过肩特写，姚三刀眯眼；音效：紧张鼓点；姚三刀说：「真是……难怪我派去的人没回来！」\n" +
		"【3-7秒】镜头：正反打，韩铮(右中)3/4正面朝左定住；音效：紧张鼓点；韩铮说：「漏网了！」\n" +
		"【7-10秒】镜头：反应；音效：紧张鼓点"
	got := FinalizeShotScript(script, 10)
	if strings.Contains(got, "【7-10秒】镜头：反应") {
		t.Fatalf("empty reaction beat should merge, got:\n%s", got)
	}
	if !strings.Contains(got, "漏网了") {
		t.Fatalf("second beat dialogue lost:\n%s", got)
	}
}

func TestKeepSubstantiveSilentThirdBeat(t *testing.T) {
	script := "【0-3秒】镜头：中景，顾满仓踢向韩铮胫骨；音效：紧张鼓点；韩铮说：「你二人是谁？」\n" +
		"【3-7秒】镜头：特写，韩铮晃而未倒；音效：紧张鼓点\n" +
		"【7-10秒】镜头：近景，韩铮低头看滚落的绿豆糕再抬眼发愣；音效：紧张鼓点"
	got := FinalizeShotScript(script, 10)
	if !strings.Contains(got, "绿豆糕") {
		t.Fatalf("substantive third beat must stay, got:\n%s", got)
	}
}

func TestFinalizeShotScriptClipsThirteenAndFillsNine(t *testing.T) {
	over := "【0-3秒】镜头：A。\n【3-7秒】镜头：B。\n【7-10秒】镜头：C。\n【10-13秒】镜头：D；韩铮说：「谁？」"
	got := FinalizeShotScript(over, 10)
	if strings.Contains(got, "10-13") || strings.Contains(got, "谁？") {
		t.Fatalf("13s beat must be clipped, got %s", got)
	}
	if ScriptEndSeconds(got) != 10 {
		t.Fatalf("expected end=10, got %d: %s", ScriptEndSeconds(got), got)
	}

	under := "【0-3秒】镜头：反应。\n【3-6秒】镜头：定镜。\n【6-9秒】镜头：余韵。"
	got = FinalizeShotScript(under, 10)
	if ScriptEndSeconds(got) != 10 {
		t.Fatalf("9s ending must stretch to 10, got %d: %s", ScriptEndSeconds(got), got)
	}
	if !strings.Contains(got, "【6-10秒】") {
		t.Fatalf("last beat should become 【6-10秒】, got %s", got)
	}
}

func TestParseStoryboardPacksOverflow(t *testing.T) {
	got, err := parseStoryboardResult(`{"shots":[{"label":"更衣室","duration":10,"script":"【0-3秒】镜头：A。\n【10-13秒】镜头：B。"}]}`, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Shots) != 2 {
		t.Fatalf("expected split into 2 shots, got %#v", got.Shots)
	}
	if strings.Contains(got.Shots[0].Script, "10-13") {
		t.Fatalf("first shot still over 10s: %s", got.Shots[0].Script)
	}
}

func TestPolishShotForQCDropsOverflowAndFlattens(t *testing.T) {
	script := "【0-3秒】镜头：中景，阿彪把衬衫扔向韩铮；音效：低鼓点；阿彪说：「铮哥，今晚庆功局订好了。」\n" +
		"【3-7秒】镜头：近景，韩铮嘴角上扬；音效：低鼓点；韩铮说：「断财路？那叫市场调控。」\n" +
		"【7-10秒】镜头：中景，韩铮展开衬衫；音效：低鼓点；韩铮说：「我给你再调控一次。」\n" +
		"【10-13秒】镜头：阿彪反应；音效：低鼓点；阿彪说：「我去跟那边传话。」"
	polished := PolishShotForQC(ShotContext{ID: 1, Duration: 10, Script: script}, nil)
	if len(polished) < 1 {
		t.Fatal("expected packed shots")
	}
	if strings.Contains(polished[0].Script, "10-13") || ScriptEndSeconds(polished[0].Script) > 10 {
		t.Fatalf("current shot should stay within 10s, got %s", polished[0].Script)
	}
	flat := FlattenPackedScript(polished)
	if !strings.Contains(flat, "【10-13秒】") || !strings.Contains(flat, "传话") {
		t.Fatalf("flatten should restore overflow after 10s for episode packing, got %s", flat)
	}
}

func TestPolishShotForQCKeepsCompliantScript(t *testing.T) {
	script := "【0-3秒】镜头：中景，阿彪把衬衫扔向韩铮；音效：低鼓点、衣料破空；阿彪说：「铮哥，今晚庆功局订好了。」\n" +
		"【3-7秒】镜头：近景，韩铮捏着衬衫嘴角上扬；音效：低鼓点、更衣室外闷响；韩铮说：「断财路？那叫市场调控。」\n" +
		"【7-10秒】镜头：中景，韩铮展开衬衫准备穿上；音效：低鼓点、衣料摩擦；韩铮说：「我给你再调控一次。」"
	polished := PolishShotForQC(ShotContext{ID: 1, Duration: 10, Script: script}, nil)
	if len(polished) != 1 {
		t.Fatalf("compliant script should stay one shot, got %d: %#v", len(polished), scriptsOf(polished))
	}
	if ScriptEndSeconds(polished[0].Script) > 10 {
		t.Fatalf("should stay within 10s, got %s", polished[0].Script)
	}
	if !strings.Contains(polished[0].Script, "再调控一次") {
		t.Fatalf("should keep original dialogue, got %s", polished[0].Script)
	}
}

func scriptsOf(shots []ShotContext) []string {
	out := make([]string, len(shots))
	for i, shot := range shots {
		out[i] = shot.Script
	}
	return out
}

func TestFinalizeShotScriptRetimesTripleZeroTen(t *testing.T) {
	script := "【0-10秒】镜头：中景手持微晃，韩铮喘气；音效：呼吸声\n【0-10秒】镜头：推到掌心特写；音效：衣料声\n【0-10秒】镜头：固定特写摩挲；音效：嘈杂声"
	got := FinalizeShotScript(script, 10)
	if strings.Count(got, "【0-10秒】") != 0 {
		t.Fatalf("should not keep full-span repeats, got:\n%s", got)
	}
	if !strings.Contains(got, "【0-3秒】") || !strings.Contains(got, "【3-7秒】") || !strings.Contains(got, "【7-10秒】") {
		t.Fatalf("expected standard thirds, got:\n%s", got)
	}
	if !strings.Contains(got, "喘气") || !strings.Contains(got, "掌心") || !strings.Contains(got, "摩挲") {
		t.Fatalf("bodies must be preserved, got:\n%s", got)
	}
}

func TestFinalizeShotScriptRetimesOverlapAndStripsMetaJunk(t *testing.T) {
	script := "【0-3秒】镜头：近景固定，韩铮侧光；韩铮说：「韩家的孩子……先活下去。」\n" +
		"【3-7秒】镜头：特写慢推，韩铮抹额上灰；音效：布料摩擦、低沉鼓点\n" +
		"【7-10秒】镜头：缓拉中景，韩铮嗅到异味；音效：低沉鼓点\n" +
		"【3-10秒】镜头：反应；韩铮说：「福公公，话说一半就走了，差评。」"
	got := FinalizeShotScript(script, 10)
	if strings.Contains(got, "差评") || strings.Contains(got, "福公公") {
		t.Fatalf("meta junk dialogue must be stripped, got:\n%s", got)
	}
	if strings.Contains(got, "【3-10秒】") {
		t.Fatalf("overlapping 【3-10秒】 must not remain, got:\n%s", got)
	}
	if !strings.Contains(got, "【0-3秒】") || !strings.Contains(got, "【3-7秒】") || !strings.Contains(got, "【7-10秒】") {
		t.Fatalf("expected sequential thirds, got:\n%s", got)
	}
	if ScriptEndSeconds(got) != 10 {
		t.Fatalf("expected end=10, got %d:\n%s", ScriptEndSeconds(got), got)
	}
	if scriptHasOverlappingBeats(got) {
		t.Fatalf("final script must not overlap, got:\n%s", got)
	}
}

func TestNormalizeShotTimelineKeepsOverflowForPack(t *testing.T) {
	script := "【0-3秒】镜头：A\n【3-7秒】镜头：B\n【7-10秒】镜头：C\n【3-10秒】镜头：D；阿彪说：「我去跟那边传话——」"
	got := NormalizeShotTimeline(script, 10)
	if strings.Contains(got, "【3-10秒】") {
		t.Fatalf("overlap should be remapped off the 10s window, got:\n%s", got)
	}
	if !strings.Contains(got, "传话") {
		t.Fatalf("overflow body must stay for pack, got:\n%s", got)
	}
	if ScriptEndSeconds(got) <= 10 {
		t.Fatalf("normalize must leave overflow past 10s, got end=%d:\n%s", ScriptEndSeconds(got), got)
	}
}

func TestPackThenFinalizeDoesNotKeepTripleZeroTenInOneShot(t *testing.T) {
	in := []StoryboardShot{{
		Label: "侧库", Duration: 10,
		Script: "【0-10秒】镜头：A喘气\n【0-10秒】镜头：B特写\n【0-10秒】镜头：C摩挲",
	}}
	packed := PackStoryboardShots(in)
	for i := range packed {
		packed[i].Script = FinalizeShotScript(packed[i].Script, 10)
	}
	if len(packed) != 1 {
		t.Fatalf("expected 1 shot (retime before pack), got %d", len(packed))
	}
	s := packed[0].Script
	if strings.Contains(s, "【0-10秒】") {
		t.Fatalf("should not keep 【0-10秒】, got:\n%s", s)
	}
	if !strings.Contains(s, "【0-3秒】") || !strings.Contains(s, "【3-7秒】") || !strings.Contains(s, "【7-10秒】") {
		t.Fatalf("expected standard thirds, got:\n%s", s)
	}
	if !(strings.Contains(s, "A喘气") && strings.Contains(s, "B特写") && strings.Contains(s, "C摩挲")) {
		t.Fatalf("lost beat bodies: %s", s)
	}
}

func TestRetimeBeforePackKeepsOneShot(t *testing.T) {
	script := "【0-10秒】镜头：A喘气\n【0-10秒】镜头：B特写\n【0-10秒】镜头：C摩挲"
	fixed := retimeOverlappingOrFullSpanBeats(script, 10)
	got := PackStoryboardShots([]StoryboardShot{{Label: "侧库", Duration: 10, Script: fixed}})
	if len(got) != 1 {
		t.Fatalf("expected 1 shot after retime+pack, got %d: %#v", len(got), got)
	}
	s := FinalizeShotScript(got[0].Script, 10)
	if !strings.Contains(s, "【0-3秒】") || !strings.Contains(s, "【3-7秒】") || !strings.Contains(s, "【7-10秒】") {
		t.Fatalf("expected standard thirds, got:\n%s", s)
	}
}

func TestNormalizeStoryboardShotsDropsDuplicateReminder(t *testing.T) {
	line := "少惹姚三刀。"
	in := []StoryboardShot{{
		Label: "侧库", Duration: 10,
		Script: "【0-3秒】镜头：反应\n【3-7秒】镜头：中景；裴长河说：「" + line + "」\n【7-10秒】镜头：出门；裴长河说：「" + line + "」",
	}}
	got := NormalizeStoryboardShots(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 shot, got %d", len(got))
	}
	if strings.Count(got[0].Script, "少惹姚三刀") != 1 {
		t.Fatalf("normalize must drop duplicate reminder, got:\n%s", got[0].Script)
	}
}

func TestRetimeBeatsByContentShortSecondLine(t *testing.T) {
	script := "【0-3秒】镜头：全景固定，御膳房外花园夹道，韩铮端盘走来；音效：紧张鼓点；顾满仓说：「那小子不是韩小灶吗？」\n" +
		"【3-10秒】镜头：中景推镜，姚三刀与顾满仓拦路；音效：紧张鼓点延续；顾满仓说：「怎么还喘气？」"
	got := FinalizeShotScript(script, 10)
	if strings.Contains(got, "【3-10秒】") {
		t.Fatalf("short second line should not keep 7s beat, got:\n%s", got)
	}
	beats := scriptBeats(got)
	if len(beats) < 2 {
		t.Fatalf("expected 2 beats, got:\n%s", got)
	}
	_, end2, ok := beatRange(beats[1])
	if !ok {
		t.Fatal("missing second beat range")
	}
	_, start2, _ := beatRange(beats[1])
	if end2-start2 > 5 {
		t.Fatalf("second beat too long for short line (%d-%d):\n%s", start2, end2, got)
	}
	if start2 < 5 {
		t.Fatalf("first beat should expand for longer setup, got %d-%d:\n%s", start2, end2, got)
	}
}

func TestRetimeBeatsByContentHanZhengShortTail(t *testing.T) {
	script := "【0-3秒】镜头：近景固定，韩铮掏出焦黑腰牌塞进衣缝；音效：低沉鼓点；韩铮说：「小郡王生辰……现代菜能上桌吗？」\n" +
		"【3-10秒】镜头：中景，韩铮抬头看向膳房方向；音效：低沉鼓点延续；韩铮说：「先活过今天。」"
	got := FinalizeShotScript(script, 10)
	beats := scriptBeats(got)
	if len(beats) < 2 {
		t.Fatalf("expected 2 beats, got:\n%s", got)
	}
	_, start2, ok := beatRange(beats[1])
	_, end2, ok2 := beatRange(beats[1])
	if !ok || !ok2 {
		t.Fatal("missing second beat range")
	}
	if end2-start2 > 5 {
		t.Fatalf("tail line should not sit on 7s beat, got %d-%d:\n%s", start2, end2, got)
	}
}

func TestPolishSavedShotScriptExpandsLiteralNewline(t *testing.T) {
	raw := "【0-3秒】镜头：A\\n【3-7秒】镜头：B"
	got := PolishSavedShotScript(raw, 10)
	if strings.Contains(got, `\n`) {
		t.Fatalf("literal \\n should expand, got %q", got)
	}
	if len(scriptBeats(got)) != 2 {
		t.Fatalf("expected 2 beats, got %q", got)
	}
}

func TestPrepareShotsForQCKeepsCompletedLockedDialogue(t *testing.T) {
	source := "韩铮：\"韩家的孩子……先活下去。福公你话说一半就跑，差评。\"\n" +
		"韩铮：\"行。主线任务更新：查韩家旧案。\""
	shots := []ShotContext{
		{ID: 1, Duration: 10, Script: "【0-5秒】镜头：韩铮近景；韩铮说：「韩家的孩子……先活下去。」\\n【5-10秒】镜头：韩铮继续；韩铮说：「福公你话说一半就跑，差评。」"},
		{ID: 2, Duration: 10, Script: "【0-5秒】镜头：韩铮苦笑；韩铮说：「行。主线任务更新：查韩家旧案。」\n【5-10秒】镜头：停顿；音效：配乐床"},
	}
	if !shotContextsCoverAllDialogue(shots, source, nil) {
		t.Fatalf("test fixture should have complete locked dialogue: %#v", shots)
	}
	got := PrepareShotsForQC(shots, nil, source)
	if len(got) != 2 {
		t.Fatalf("completed schedule should not append legacy dialogue shots: %#v", got)
	}
	joined := got[0].Script + "\n" + got[1].Script
	for _, want := range []string{"差评。", "主线任务更新", "查韩家旧案"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("locked dialogue %q was removed:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, `\n`) {
		t.Fatalf("literal newline survived preparation: %q", joined)
	}
}
