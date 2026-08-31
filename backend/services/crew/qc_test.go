package crew

import (
	"strings"
	"testing"
)

func TestDetectDuplicateDerivativeRefs(t *testing.T) {
	issues := detectRefAndCostumeIssues([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】韩铮坐着。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "赛后赤膊", DisplayName: "韩铮（赛后赤膊）", ParentName: "韩铮", ParentID: 1, IsDerivative: true, ResourceID: 1316},
			{Kind: "character", Name: "赤膊状态", DisplayName: "韩铮（赤膊状态）", ParentName: "韩铮", ParentID: 1, IsDerivative: true, ResourceID: 1326},
		},
	}})
	if !hasQCCode(issues, "R1") || !hasQCMessage(issues, "重复绑定") {
		t.Fatalf("expected duplicate derivative R1, got %#v", issues)
	}
}

func TestDropLLMGhostIssuesKeepsDeterministicOnly(t *testing.T) {
	det := []QCIssue{{Code: "R1", ShotID: 1, ShotIndex: 1, Message: "还缺场景"}}
	got := dropLLMGhostIssues([]QCIssue{
		{Code: "R4", ShotID: 2, ShotIndex: 2, Message: "穿衣镜还要再绑赤膊图做过渡"},
		{Code: "R1", ShotID: 1, ShotIndex: 1, Message: "还缺场景"},
		{Code: "R2", ShotID: 3, ShotIndex: 3, Message: "台词被拆开"},
		{Code: "R9", ShotID: 4, ShotIndex: 4, Message: "文案有动刀，平台会拒审"},
	}, det)
	if hasQCMessage(got, "赤膊图做过渡") {
		t.Fatalf("fixed costume R4 should not come back from LLM, got %#v", got)
	}
	if !hasQCMessage(got, "还缺场景") || !hasQCMessage(got, "台词被拆开") {
		t.Fatalf("real leftover + dialogue should stay, got %#v", got)
	}
	if hasQCCode(got, "R9") {
		t.Fatalf("platform compatibility must not re-enter creative QC, got %#v", got)
	}
}

func TestDropLLMGhostSpeakerWhenDeterministicClear(t *testing.T) {
	got := dropLLMGhostIssues([]QCIssue{
		{Code: "R2", ShotID: 2, ShotIndex: 2, Message: "台词未标明说话人"},
		{Code: "R2", ShotID: 10, ShotIndex: 10, Message: "这段约 3 秒，台词 18 字，按 4 字/秒会说不完"},
		{Code: "R2", ShotID: 3, ShotIndex: 3, Message: "台词顺序与剧本不一致"},
	}, nil)
	if hasQCMessage(got, "未标明说话人") || hasQCMessage(got, "会说不完") {
		t.Fatalf("fixed speaker/overlong should not come back from LLM, got %#v", got)
	}
	if !hasQCMessage(got, "台词顺序") {
		t.Fatalf("faithfulness R2 should stay, got %#v", got)
	}
}

func TestDetectParentAndChildRefsTogether(t *testing.T) {
	issues := detectRefAndCostumeIssues([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】韩铮坐着。\n【3-7秒】韩铮看奖牌。\n【7-10秒】韩铮接衬衫。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "韩铮", DisplayName: "韩铮"},
			{Kind: "character", Name: "赤膊战损", DisplayName: "韩铮（赤膊战损）", ParentName: "韩铮", IsDerivative: true, ResourceID: 11},
		},
	}})
	if !hasQCCode(issues, "R1") {
		t.Fatalf("expected R1 parent+child, got %#v", issues)
	}
}

func TestDetectShirtlessThenPuttingOnInSameShot(t *testing.T) {
	issues := detectRefAndCostumeIssues([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】手伸进冰水。\n【3-7秒】赤膊的韩铮坐在木凳上。\n【7-10秒】韩铮伸手接衬衫。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "赤膊战损", DisplayName: "韩铮（赤膊战损）", ParentName: "韩铮", IsDerivative: true},
		},
	}})
	if !hasQCCode(issues, "R3") {
		t.Fatalf("expected R3 shirtless+putting on, got %#v", issues)
	}
}

func TestDetectDressingShotBoundToShirtlessRef(t *testing.T) {
	issues := detectRefAndCostumeIssues([]ShotContext{{
		ID:     2,
		Index:  2,
		Script: "【0-3秒】韩铮扣衬衫扣子。\n【3-7秒】远处拳手问话。\n【7-10秒】韩铮起身离开。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "赤膊战损", DisplayName: "韩铮（赤膊战损）", ParentName: "韩铮", IsDerivative: true},
		},
	}})
	if !hasQCCode(issues, "R4") {
		t.Fatalf("expected R4 dressing vs shirtless ref, got %#v", issues)
	}
}

func TestDetectCostumeReversalAcrossShots(t *testing.T) {
	issues := detectRefAndCostumeIssues([]ShotContext{
		{
			ID:     1,
			Index:  1,
			Script: "【0-3秒】韩铮坐着。\n【7-10秒】韩铮扣扣子。",
			Refs:   []ShotRefInfo{{Kind: "character", Name: "韩铮"}},
		},
		{
			ID:     2,
			Index:  2,
			Script: "【0-3秒】赤膊的韩铮按住胸口。\n【7-10秒】韩铮起身。",
			Refs:   []ShotRefInfo{{Kind: "character", Name: "韩铮"}},
		},
	})
	if !hasQCCode(issues, "R3") {
		t.Fatalf("expected R3 reversal, got %#v", issues)
	}
}

func TestDetectMissingSceneAndCharacterRefs(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1},
		{Name: "更衣室", Type: "scene", ResourceID: 2},
		{Name: "冰水桶", Type: "prop", ResourceID: 3},
	}
	issues := detectDeterministicQC([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：特写，韩铮把手伸进冰水桶。\n【3-7秒】镜头：中景。\n【7-10秒】镜头：韩铮抬头。",
		Refs:   []ShotRefInfo{{Kind: "character", Name: "韩铮"}},
	}}, assets, "")
	if !hasQCCode(issues, "R1") {
		t.Fatalf("expected missing scene R1, got %#v", issues)
	}
	if !hasQCMessage(issues, "场景") {
		t.Fatalf("expected scene missing, got %#v", issues)
	}
	if !hasQCMessage(issues, "冰水桶") {
		t.Fatalf("expected prop missing, got %#v", issues)
	}
}

func TestStandaloneMentionIgnoresNameInsideScene(t *testing.T) {
	if standaloneMention("韩铮意识空间里很安静。", "韩铮", []string{"韩铮", "韩铮意识空间"}) {
		t.Fatal("character name inside scene title should not count")
	}
	if !standaloneMention("韩铮坐在更衣室。", "韩铮", []string{"韩铮", "韩铮意识空间"}) {
		t.Fatal("standalone character name should count")
	}
}

func TestDetectBGMAndAppearanceAndLongDialogue(t *testing.T) {
	issues := detectAudioBeatAndLookIssues([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：近景，身着蓝袍的韩铮转身；音效：低沉鼓点、冰水声；「今晚的对手已经把场子买通了你听见没有还要不要打」",
	}})
	if hasQCCode(issues, "R5") {
		t.Fatalf("BGM should be allowed, got %#v", issues)
	}
	if !hasQCCode(issues, "R6") {
		t.Fatalf("expected appearance R6, got %#v", issues)
	}
	if !hasQCCode(issues, "R2") {
		t.Fatalf("expected long dialogue R2, got %#v", issues)
	}
}

func TestDetectUserLockerRoomDialogueOverload(t *testing.T) {
	issues := detectAudioBeatAndLookIssues([]ShotContext{{
		ID:    1,
		Index: 1,
		Script: "【0-3秒】中景，阿彪随手将一件白衬衫扔向赤膊的韩铮，韩铮伸手接住；音效：低沉鼓点配乐床，衬衫划过空气的轻响。「铮哥，今晚庆功局订好了，对手那边放话——你断了人家的财路，小心点。」\n" +
			"【3-7秒】近景，韩铮拿着衬衫嘴角上扬；音效：低沉鼓点持续。「断财路？那叫市场调控。谁不服，排队，我给你再调控一次。」\n" +
			"【7-10秒】中景，韩铮展开衬衫准备穿上；音效：低沉鼓点持续。「 」",
	}})
	if !hasQCMessage(issues, "会说不完") {
		t.Fatalf("expected overlong dialogue, got %#v", issues)
	}
	if !hasQCMessage(issues, "空") {
		t.Fatalf("expected empty quote, got %#v", issues)
	}
	high := false
	for _, issue := range issues {
		if issue.Code == "R2" && issue.Severity == "high" {
			high = true
		}
	}
	if !high {
		t.Fatalf("overlong dialogue should be high, got %#v", issues)
	}
}

func TestDetectDrumBeatContinuityGap(t *testing.T) {
	issues := detectBGMContinuity([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：近景；音效：低鼓点、冰水声",
	}, {
		ID:     2,
		Index:  2,
		Script: "【0-3秒】镜头：中景；音效：冰水声",
	}})
	if !hasQCCode(issues, "R5") {
		t.Fatalf("expected missing BGM continuity R5, got %#v", issues)
	}
	if !hasQCMessage(issues, "断了") {
		t.Fatalf("expected continuity break, got %#v", issues)
	}
}

func TestDrumEnergyChangeIsCompatible(t *testing.T) {
	issues := detectBGMContinuity([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：近景；音效：低鼓点、冰水声",
	}, {
		ID:     2,
		Index:  2,
		Script: "【0-3秒】镜头：中景；音效：紧张鼓点、脚步声",
	}})
	if hasQCCode(issues, "R5") {
		t.Fatalf("drum energy change should be ok, got %#v", issues)
	}
}

func TestScoreQCIssues(t *testing.T) {
	if scoreQCIssues(nil) != "A" {
		t.Fatal("empty should be A")
	}
	if scoreQCIssues([]QCIssue{{Severity: "high"}}) != "C" {
		t.Fatal("one high should be C")
	}
	if scoreQCIssues([]QCIssue{{Severity: "high"}, {Severity: "high"}, {Severity: "high"}}) != "D" {
		t.Fatal("three high should be D")
	}
}

func hasQCCode(issues []QCIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func hasQCMessage(issues []QCIssue, part string) bool {
	for _, issue := range issues {
		if strings.Contains(issue.Message, part) {
			return true
		}
	}
	return false
}

func TestDetectAccessoryMismatchParentVsChild(t *testing.T) {
	issues := detectAccessoryContinuity([]ShotContext{
		{ID: 1, Index: 1, Refs: []ShotRefInfo{{Kind: "character", Name: "韩铮", ResourceID: 1}}},
		{ID: 2, Index: 2, Refs: []ShotRefInfo{{Kind: "character", Name: "赤膊战损", ParentName: "韩铮", ResourceID: 11, IsDerivative: true}}},
	}, []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1, Prompt: "青年拳手，胸口挂金牌，蓝绶带项链"},
		{Name: "赤膊战损", Type: "character", ResourceID: 11, ParentID: 1, ParentName: "韩铮", IsDerivative: true, Prompt: "赤膊，无配饰，锁骨擦伤"},
	})
	if !hasQCCode(issues, "R4") || !hasQCMessage(issues, "奖牌") {
		t.Fatalf("parent vs shirtless accessory mismatch, got %#v", issues)
	}
}

func TestDetectAccessoryIgnoresUnusedOrUnspecifiedVariant(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1, Prompt: "青年拳手，胸口挂金牌，蓝绶带项链"},
		{Name: "赤膊赛后", Type: "character", ResourceID: 11, ParentID: 1, ParentName: "韩铮", IsDerivative: true, Prompt: "赤膊，锁骨擦伤"},
	}
	shots := []ShotContext{
		{ID: 1, Index: 1, Refs: []ShotRefInfo{{Kind: "character", Name: "韩铮", ResourceID: 1}}},
		{ID: 2, Index: 2, Refs: []ShotRefInfo{{Kind: "character", Name: "赤膊赛后", ParentName: "韩铮", ResourceID: 11, IsDerivative: true}}},
	}
	if issues := detectAccessoryContinuity(shots, assets); hasQCCode(issues, "R4") {
		t.Fatalf("an omitted accessory is unknown, not an explicit mismatch: %#v", issues)
	}
	if issues := detectAccessoryContinuity(shots[:1], assets); hasQCCode(issues, "R4") {
		t.Fatalf("an unused derivative cannot cause on-screen continuity errors: %#v", issues)
	}
}

func TestDetectAccessoryMismatchAcrossShots(t *testing.T) {
	issues := detectAccessoryContinuity([]ShotContext{
		{ID: 1, Index: 1, Script: "韩铮浸冰水。", Refs: []ShotRefInfo{
			{Kind: "character", Name: "赤膊战损", ParentName: "韩铮", ResourceID: 11, IsDerivative: true, Prompt: "赤膊，无配饰"},
		}},
		{ID: 4, Index: 4, Script: "韩铮扣衬衫。", Refs: []ShotRefInfo{
			{Kind: "character", Name: "韩铮", ResourceID: 1, Prompt: "日常衬衫，脖子金牌项链"},
		}},
	}, nil)
	if !hasQCCode(issues, "R4") || !hasQCMessage(issues, "配饰不一致") {
		t.Fatalf("bound refs accessory mismatch, got %#v", issues)
	}
}

func TestDetectAmbiguousInteractionTargets(t *testing.T) {
	issues := detectAmbiguousInteractionTargets([]ShotContext{{
		ID: 8, Index: 5,
		Script: "【0-3秒】镜头：洛清霜(中中)目光锁定听者以确认其态度；洛清霜说：「你认识对方吗？」",
	}})
	if !hasQCCode(issues, "R3") || !hasQCMessage(issues, "模糊指代") {
		t.Fatalf("ambiguous visual target should be R3: %#v", issues)
	}

	clean := detectAmbiguousInteractionTargets([]ShotContext{{
		ID: 9, Index: 6,
		Script: "【0-3秒】镜头：洛清霜(中中)目光锁定韩铮以确认态度；洛清霜说：「你认识对方吗？」",
	}})
	if len(clean) != 0 {
		t.Fatalf("dialogue may contain 对方 while visual target is named: %#v", clean)
	}
}

func TestDetectMedalBakedIntoCharacter(t *testing.T) {
	issues := detectAccessoryContinuity([]ShotContext{
		{ID: 2, Index: 2, Script: "【3-7秒】韩铮看奖牌，奖牌冷光扫过脸。", Refs: []ShotRefInfo{
			{Kind: "character", Name: "韩铮", ResourceID: 1, Prompt: "青年拳手，胸口挂金牌"},
			{Kind: "prop", Name: "奖杯", ResourceID: 9},
		}},
	}, []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1, Prompt: "青年拳手，胸口挂金牌"},
		{Name: "奖杯", Type: "prop", ResourceID: 9, Description: "冠军奖杯"},
	})
	if !hasQCCode(issues, "R7") {
		t.Fatalf("medal baked into character should be R7, got %#v", issues)
	}
}

func TestLookAtMedalDoesNotCountAsWearing(t *testing.T) {
	set := neckPropSet("韩铮看向奖牌，奖牌冷光扫过脸，没有项链")
	if set["奖牌"] {
		t.Fatalf("looking at a medal should not count as wearing one, got %#v", set)
	}
}

func TestMergeQCAfterFixDropsNovelIssues(t *testing.T) {
	previous := []QCIssue{
		{Code: "R2", ShotID: 468, ShotIndex: 1, Severity: "high", Message: "台词被拆开"},
		{Code: "R1", ShotID: 470, ShotIndex: 3, Severity: "medium", Message: "拳手甲未绑定"},
	}
	fresh := QCReport{
		Issues: []QCIssue{
			{Code: "R2", ShotID: 468, ShotIndex: 1, Severity: "high", Message: "后半句派给了韩铮"},
			{Code: "R5", ShotID: 480, ShotIndex: 13, Severity: "medium", Message: "新编的配乐问题"},
			{Code: "R3", ShotID: 469, ShotIndex: 2, Severity: "medium", Message: "新的穿衣问题"},
		},
	}
	got := MergeQCAfterFix(fresh, previous, nil)
	if len(got.Issues) != 1 || !hasQCMessage(got.Issues, "后半句") {
		t.Fatalf("select-all recheck should only keep original locations, got %#v", got.Issues)
	}
}

func TestMergeQCAfterFixKeepsUnselectedLeftover(t *testing.T) {
	previous := []QCIssue{
		{Code: "R2", ShotID: 468, Message: "台词被拆开"},
		{Code: "R1", ShotID: 470, Message: "拳手甲未绑定"},
	}
	fresh := QCReport{Issues: []QCIssue{
		{Code: "R2", ShotID: 468, Message: "台词仍被拆开"},
		{Code: "R1", ShotID: 470, Message: "拳手甲未绑定"},
		{Code: "R5", ShotID: 480, Message: "新编配乐"},
	}}
	leftover := LeftoverQCIssues(previous, []QCIssue{{Code: "R2", ShotID: 468, Message: "台词被拆开"}})
	got := MergeQCAfterFix(fresh, previous, leftover)
	if !hasQCMessage(got.Issues, "台词仍被拆开") || !hasQCMessage(got.Issues, "拳手甲") {
		t.Fatalf("got %#v", got.Issues)
	}
	if hasQCMessage(got.Issues, "新编配乐") {
		t.Fatalf("should drop novel issue, got %#v", got.Issues)
	}
}

func TestMergeQCAfterFixDropsFixedSpeakerLeftover(t *testing.T) {
	previous := []QCIssue{
		{Code: "R2", ShotID: 2, ShotIndex: 2, Message: "台词未标明说话人"},
		{Code: "R2", ShotID: 5, ShotIndex: 5, Message: "仍未标明说话人，也不是「阿彪说：」格式"},
	}
	leftover := []QCIssue{{Code: "R2", ShotID: 5, ShotIndex: 5, Message: "仍未标明说话人，也不是「阿彪说：」格式"}}
	got := MergeQCAfterFix(QCReport{
		Summary: "复检发现 9 项既有问题中仍有 8 项未修复",
		Issues:  []QCIssue{},
	}, previous, leftover)
	if len(got.Issues) != 0 {
		t.Fatalf("fixed speaker leftover should not return, got %#v", got.Issues)
	}
	if !strings.Contains(got.Summary, "复检通过") {
		t.Fatalf("summary should ignore LLM unfixed claim, got %q", got.Summary)
	}
}

func TestAttributedSayQuoteNotFlagged(t *testing.T) {
	issues := detectAudioBeatAndLookIssues([]ShotContext{{
		ID:     2,
		Index:  2,
		Script: "【0-3秒】镜头：中景，阿彪把衬衫扔向韩铮；音效：低鼓点；阿彪说：「铮哥，今晚庆功局订好了。」",
	}})
	if hasQCMessage(issues, "未标明说话人") {
		t.Fatalf("attributed quote should pass, got %#v", issues)
	}
}

func TestNameInActionStillNeedsSayPrefix(t *testing.T) {
	issues := detectAudioBeatAndLookIssues([]ShotContext{{
		ID:     2,
		Index:  2,
		Script: "【0-3秒】镜头：中景，阿彪把衬衫扔向韩铮；音效：低鼓点。「铮哥，今晚庆功局订好了。」",
	}})
	if !hasQCMessage(issues, "未标明说话人") {
		t.Fatalf("name in action is not a speaker label, got %#v", issues)
	}
}

func TestMergedQuotesInOneBeatFlagged(t *testing.T) {
	issues := detectAudioBeatAndLookIssues([]ShotContext{{
		ID:     6,
		Index:  6,
		Script: "【0-3秒】镜头：对峙；拳手甲说：「下一轮我请他吃小炒。」韩铮说：「行，我奉陪。」",
	}})
	if !hasQCMessage(issues, "挤在同一拍") {
		t.Fatalf("two quotes in one beat should flag, got %#v", issues)
	}
	if hasQCMessage(issues, "未标明说话人") {
		t.Fatalf("attributed merged quotes should not also flag speaker, got %#v", issues)
	}
}

func TestDetectDurationOverflow(t *testing.T) {
	issues := detectDurationOverflow([]ShotContext{{
		ID: 1, Index: 1, Duration: 10,
		Script: "【0-3秒】镜头：A。\n【10-13秒】镜头：阿彪说：「我去跟那边传话——」",
	}})
	if !hasQCCode(issues, "R8") || !hasQCMessage(issues, "13 秒") {
		t.Fatalf("expected R8 overflow, got %#v", issues)
	}
}

func TestWithinShotDuplicateDialogueFlaggedAndFixed(t *testing.T) {
	line := "姚爷都不认？昨天还在灶下给"
	shots := []ShotContext{{
		ID: 3, Index: 3, Duration: 10,
		Script: "【0-3秒】镜头：反应；顾满仓说：「" + line + "」\n" +
			"【3-6秒】镜头：近景；顾满仓说：「" + line + "」\n" +
			"【6-9秒】镜头：走过去。\n【9-13秒】镜头：闻。",
	}}
	issues := detectDuplicateDialogue(shots)
	if !hasQCMessage(issues, "同一镜内重复") {
		t.Fatalf("expected within-shot duplicate, got %#v", issues)
	}
	fixed := ApplyQCFixes(shots, nil, append(issues, detectDurationOverflow(shots)...))
	if strings.Count(fixed[0].Script, line) != 1 {
		t.Fatalf("should keep one copy of the line, got %s", fixed[0].Script)
	}
	if ScriptEndSeconds(fixed[0].Script) > 10 {
		t.Fatalf("packed fix should clamp duration, got %s", fixed[0].Script)
	}
}

func TestLeftoverQCIssuesDropsSelected(t *testing.T) {
	got := LeftoverQCIssues([]QCIssue{
		{Code: "R2", ShotID: 468, Message: "台词被拆开"},
		{Code: "R1", ShotID: 470, Message: "拳手甲"},
	}, []QCIssue{
		{Code: "R2", ShotID: 468, Message: "台词被拆开"},
	})
	if len(got) != 1 || got[0].Code != "R1" {
		t.Fatalf("got %#v", got)
	}
}

func TestDetectDuplicateDialogueAcrossShots(t *testing.T) {
	line := "拳手甲说：「哥，心脏那老毛病又疼？」"
	issues := detectDuplicateDialogue([]ShotContext{
		{ID: 1, Index: 1, Script: "【6-10秒】镜头：中景，拳手甲远远开口；" + line},
		{ID: 2, Index: 2, Script: "【0-3秒】镜头：中景；" + line + "\n【3-6秒】镜头：韩铮说：「疼才证明我还活着。走，庆功。」"},
	})
	if !hasQCCode(issues, "R2") || !hasQCMessage(issues, "重复同一句") {
		t.Fatalf("expected duplicate dialogue R2, got %#v", issues)
	}
}

func TestDetectDuplicateDialogueFragmentAcrossShots(t *testing.T) {
	full := "先去洗菜。今日小郡王生辰备料，错一份，咱们全膳房喝西北风。记住——少惹姚三刀。"
	frag := "料，错一份，咱们全膳房喝西北风。记住——"
	issues := detectDuplicateDialogue([]ShotContext{
		{ID: 1, Index: 1, Script: "【0-3秒】裴长河说：「" + full + "」"},
		{ID: 2, Index: 2, Script: "【0-3秒】裴长河说：「" + frag + "」"},
	})
	if !hasQCMessage(issues, "重复同一句") {
		t.Fatalf("fragment of an earlier line should count as duplicate, got %#v", issues)
	}
	fixed := DedupeDialogueAcrossShots([]ShotContext{
		{ID: 1, Index: 1, Script: "【0-3秒】裴长河说：「" + full + "」"},
		{ID: 2, Index: 2, Script: "【0-3秒】镜头：反应；裴长河说：「" + frag + "」\n【3-7秒】镜头：走开。"},
	})
	if strings.Contains(fixed[1].Script, "错一份") {
		t.Fatalf("dedupe should strip fragment from later shot, got %s", fixed[1].Script)
	}
	if !strings.Contains(fixed[0].Script, "少惹姚三刀") {
		t.Fatalf("first shot should keep full line, got %s", fixed[0].Script)
	}
}

func TestDedupeWithinShotShortTailReminder(t *testing.T) {
	full := "先去洗菜。今日小都王生辰备料，错一份，咱们全膳房喝西北风。记住—少惹姚三刀。"
	frag := "记住—少惹姚三刀。"
	if !quotesSubstantivelyDuplicate(full, frag) {
		t.Fatal("7-han tail reminder must count as duplicate of the full line")
	}
	script := "【0-3秒】镜头：近景；韩铮说：「师傅！我这不是……菜没切完嘛。命硬，阎王不收。」\n" +
		"【3-7秒】镜头：中景；裴长河说：「" + full + "」\n" +
		"【7-10秒】镜头：特写；裴长河说：「" + frag + "」"
	got := FinalizeShotScript(script, 10)
	if strings.Count(got, "少惹姚三刀") != 1 {
		t.Fatalf("expected one reminder line after finalize, got:\n%s", got)
	}
	if !strings.Contains(got, "先去洗菜") {
		t.Fatalf("should keep the full first delivery, got:\n%s", got)
	}
}

func TestDedupeWithinShotExactReminderTwice(t *testing.T) {
	// User screenshot: same 「少惹姚三刀。」 in 【3-6秒】 and 【6-10秒】.
	line := "少惹姚三刀。"
	script := "【0-3秒】镜头：反应\n【3-6秒】镜头：反应；裴长河说：「" + line + "」\n【6-10秒】镜头：裴长河出门后的反应镜头；音效：低沉鼓点；裴长河说：「" + line + "」"
	got := FinalizeShotScript(script, 10)
	if strings.Count(got, "少惹姚三刀") != 1 {
		t.Fatalf("exact within-shot duplicate must collapse to one, got:\n%s", got)
	}
	if !strings.Contains(got, "反应") {
		t.Fatalf("reaction beats should remain, got:\n%s", got)
	}
}

func TestDetectSceneRefMismatch(t *testing.T) {
	issues := detectSceneRefMismatch([]ShotContext{
		{
			ID: 1, Index: 1,
			Script: "【6-10秒】镜头：中景，韩铮站在更衣室门口，语气带笑。",
			Refs:   []ShotRefInfo{{Kind: "scene", Name: "私人会所包厢"}},
		},
		{
			ID: 2, Index: 2,
			Script: "【0-3秒】镜头：近景，韩铮扣衬衫扣子。",
			Refs:   []ShotRefInfo{{Kind: "scene", Name: "北城铁腕地下拳馆更衣室"}},
		},
	}, nil)
	if !hasQCCode(issues, "R1") || !hasQCMessage(issues, "更衣室") || !hasQCMessage(issues, "会所") {
		t.Fatalf("expected scene mismatch R1, got %#v", issues)
	}
}

func TestDetectSceneRefMismatchSkipsWhenBoundMatches(t *testing.T) {
	issues := detectSceneRefMismatch([]ShotContext{{
		ID: 1, Index: 1,
		Script: "【0-3秒】镜头：近景，韩铮扣衬衫，更衣室灯管嗡响。",
		Refs:   []ShotRefInfo{{Kind: "scene", Name: "北城铁腕地下拳馆更衣室"}},
	}, {
		ID: 2, Index: 2,
		Script: "【0-3秒】镜头：会所包厢里小鹿起身。",
		Refs:   []ShotRefInfo{{Kind: "scene", Name: "私人会所包厢"}},
	}}, nil)
	if hasQCMessage(issues, "参考图绑的是") {
		t.Fatalf("matching scene should not flag, got %#v", issues)
	}
}

func TestDetectCrowdCharacterRefs(t *testing.T) {
	tooManyNames := detectCrowdCharacterRefs([]ShotContext{{
		ID: 1, Index: 1,
		Script: "【0-3秒】镜头：韩铮(左前)、小鹿(中前)、小南(右前)、阿彪(左中)、小嘉(中中)、林悦(右中)同时回头。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "韩铮", ResourceID: 1},
			{Kind: "character", Name: "小鹿", ResourceID: 2},
			{Kind: "character", Name: "小南", ResourceID: 3},
			{Kind: "character", Name: "阿彪", ResourceID: 4},
			{Kind: "character", Name: "小嘉", ResourceID: 5},
			{Kind: "character", Name: "林悦", ResourceID: 6},
		},
	}})
	if !hasQCMessage(tooManyNames, "点名了") || !hasQCMessage(tooManyNames, "超过 5 人") {
		t.Fatalf("expected overpacked-script R1, got %#v", tooManyNames)
	}

	manyRefsOK := detectCrowdCharacterRefs([]ShotContext{{
		ID: 2, Index: 2,
		Script: "【0-3秒】镜头：韩铮(左前)3/4正面朝右。小鹿说：「走。」",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "韩铮", ResourceID: 1},
			{Kind: "character", Name: "小鹿", ResourceID: 2},
			{Kind: "character", Name: "小南", ResourceID: 3},
			{Kind: "character", Name: "阿彪", ResourceID: 4},
			{Kind: "character", Name: "小嘉", ResourceID: 5},
			{Kind: "character", Name: "林悦", ResourceID: 6},
		},
	}})
	if len(manyRefsOK) != 0 {
		t.Fatalf("hanging many named faces is allowed when script names ≤5, got %#v", manyRefsOK)
	}
}

func TestDetectMissingCharacterSkippedWhenCrowdCapped(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1},
		{Name: "阿彪", Type: "character", ResourceID: 2},
		{Name: "小鹿", Type: "character", ResourceID: 3},
		{Name: "杀手甲", Type: "character", ResourceID: 8},
	}
	issues := detectMissingAssetRefs([]ShotContext{{
		ID: 1, Index: 1,
		Script: "【0-3秒】韩铮、阿彪、小鹿在场。杀手甲站在门口。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "韩铮", ResourceID: 1},
			{Kind: "character", Name: "阿彪", ResourceID: 2},
			{Kind: "character", Name: "小鹿", ResourceID: 3},
		},
	}}, assets)
	if hasQCMessage(issues, "杀手甲") {
		t.Fatalf("crowd extra should not require a face sheet, got %#v", issues)
	}
}

func TestDetectMissingNamedCharacterEvenWithManyRefs(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1},
		{Name: "阿彪", Type: "character", ResourceID: 2},
		{Name: "小鹿", Type: "character", ResourceID: 3},
		{Name: "小南", Type: "character", ResourceID: 4},
		{Name: "小嘉", Type: "character", ResourceID: 5},
		{Name: "林悦", Type: "character", ResourceID: 6},
	}
	issues := detectMissingAssetRefs([]ShotContext{{
		ID: 1, Index: 1,
		Script: "【0-3秒】韩铮(左前)、阿彪(右前)、小鹿(中前)、小南(左中)、小嘉(中中)。林悦说：「走。」",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "韩铮", ResourceID: 1},
			{Kind: "character", Name: "阿彪", ResourceID: 2},
			{Kind: "character", Name: "小鹿", ResourceID: 3},
			{Kind: "character", Name: "小南", ResourceID: 4},
			{Kind: "character", Name: "小嘉", ResourceID: 5},
		},
	}}, assets)
	if !hasQCMessage(issues, "林悦") {
		t.Fatalf("named person must still be flagged as missing even with 5 faces already hung, got %#v", issues)
	}
}

func TestDetectMissingSpatialBlocking(t *testing.T) {
	missing := detectMissingSpatialBlocking([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】韩铮看向阿彪。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "韩铮", ResourceID: 1},
			{Kind: "character", Name: "阿彪", ResourceID: 2},
		},
	}})
	if !hasQCMessage(missing, "九格站位") {
		t.Fatalf("expected spatial R3, got %#v", missing)
	}

	ok := detectMissingSpatialBlocking([]ShotContext{{
		ID:     2,
		Index:  2,
		Script: "【0-3秒】镜头：中景，韩铮(左前)3/4正面朝右，阿彪(右中)3/4正面朝左。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "韩铮", ResourceID: 1},
			{Kind: "character", Name: "阿彪", ResourceID: 2},
		},
	}})
	if hasQCMessage(ok, "九格站位") {
		t.Fatalf("grid script should pass, got %#v", ok)
	}

	solo := detectMissingSpatialBlocking([]ShotContext{{
		ID:     3,
		Index:  3,
		Script: "【0-3秒】韩铮抬头。",
		Refs:   []ShotRefInfo{{Kind: "character", Name: "韩铮", ResourceID: 1}},
	}})
	if !hasQCMessage(solo, "九格站位") {
		t.Fatalf("solo character shot should require spatial blocking, got %#v", solo)
	}
}

func TestDetectNarrativeGoalLeak(t *testing.T) {
	issues := detectNarrativeGoalLeak([]ShotContext{{
		ID: 7, Index: 2,
		Script: "【6-10秒】镜头：韩铮挑眉，意图点破打脸爽感逗趣师傅；音效：喜庆鼓点",
	}})
	if !hasQCCode(issues, "R3") || !hasQCMessage(issues, "创作分析标签") {
		t.Fatalf("expected narrative-goal leak R3, got %#v", issues)
	}
	clean := detectNarrativeGoalLeak([]ShotContext{{
		ID: 8, Index: 3,
		Script: "【7-10秒】镜头：韩铮(左中)挑眉，把菜碟推到裴长河面前，等他当众表态；音效：喜庆鼓点",
	}})
	if len(clean) != 0 {
		t.Fatalf("concrete goal-driven action should pass, got %#v", clean)
	}
}

func TestDetectDurationGap(t *testing.T) {
	shots := []ShotContext{{
		ID: 21, Index: 4, Duration: 10,
		Script: "【0-3秒】镜头：打手甲喝令；打手甲说：「按住他！」\n【7-10秒】镜头：韩铮侧身准备动作",
	}}
	issues := detectDurationOverflow(shots)
	if !hasQCCode(issues, "R8") || !hasQCMessage(issues, "3-7 秒") || !hasQCMessage(issues, "4 秒") {
		t.Fatalf("expected timeline-gap R8, got %#v", issues)
	}
	fixed := ApplyQCFixes(shots, nil, issues)
	if len(fixed) != 1 || scriptHasOverlappingBeats(fixed[0].Script) {
		t.Fatalf("gap fix should keep one continuous shot, got %#v", fixed)
	}
	if _, _, gap := scriptTimelineGap(fixed[0].Script); gap {
		t.Fatalf("gap should be closed after fix, got %s", fixed[0].Script)
	}
}

func TestDetectMisattributedSpeakers(t *testing.T) {
	script := `韩铮（自语）: "……舌头？你也穿越了？"
姚三刀（画外）: "不可能！他怎么还活着？！顾满仓——再找人！"
`
	issues := detectMisattributedSpeakers([]ShotContext{{
		ID:     9,
		Index:  9,
		Script: "【0-3秒】镜头：侧面中景；姚三刀说：「……舌头？你也穿越了？」\n【3-7秒】镜头：余韵\n【7-10秒】镜头：停",
	}}, script)
	if !hasQCMessage(issues, "改派说话人") || !hasQCMessage(issues, "韩铮") {
		t.Fatalf("expected speaker misattribution, got %#v", issues)
	}

	ok := detectMisattributedSpeakers([]ShotContext{{
		ID:     10,
		Script: "【0-3秒】镜头：近景；韩铮说：「……舌头？你也穿越了？」",
	}}, script)
	if len(ok) != 0 {
		t.Fatalf("correct speaker should pass, got %#v", ok)
	}
}

func TestDetectScriptFormatLiteralNewline(t *testing.T) {
	issues := detectScriptFormatIssues([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：中景；韩铮说：「先活下去。」\\n【3-7秒】镜头：反应；音效：低沉鼓点",
	}})
	if !hasQCMessage(issues, "字面量") {
		t.Fatalf("expected literal \\n R2, got %#v", issues)
	}
}

func TestDetectScriptFormatMetaSpeaker(t *testing.T) {
	issues := detectScriptFormatIssues([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：中景；第3集说：「韩家的孩子」",
	}})
	if !hasQCMessage(issues, "第N集说") {
		t.Fatalf("expected meta speaker R2, got %#v", issues)
	}
}

func TestDetectScriptFormatFlagsHanWordSplit(t *testing.T) {
	issues := detectScriptFormatIssues([]ShotContext{{
		ID:    1,
		Index: 1,
		Script: "【0-3秒】镜头：姚三刀走来；姚三刀说：「裴师傅，生辰宴主菜交给我靠」\n" +
			"【3-7秒】镜头：姚三刀扫过案板；姚三刀说：「谱人。韩小灶——去择菜叶，别脏了贵人的眼。」",
	}})
	if !hasQCMessage(issues, "词中间被切开") {
		t.Fatalf("expected 靠/谱人 split R2, got %#v", issues)
	}
}

func TestDetectScriptFormatAllowsPunctuationSplit(t *testing.T) {
	issues := detectScriptFormatIssues([]ShotContext{{
		ID:    1,
		Index: 1,
		Script: "【0-3秒】镜头：姚三刀走来；姚三刀说：「裴师傅，」\n" +
			"【3-7秒】镜头：姚三刀扫过案板；姚三刀说：「生辰宴主菜交给我靠谱人。」",
	}})
	if hasQCMessage(issues, "词中间被切开") {
		t.Fatalf("punctuation boundary should be valid, got %#v", issues)
	}
}

func TestDetectMissingScriptDialoguePeiLine(t *testing.T) {
	script := `**裴长河**（盯他两秒，叹）："先去洗菜。今日小郡王生辰备料，错一份，咱们全膳房喝西北风。记住——少惹姚三刀。"`
	issues := detectMissingScriptDialogue([]ShotContext{{
		ID:     6,
		Index:  6,
		Script: "【0-3秒】镜头：中景；裴长河说：「记住——少惹姚三刀。」",
	}}, script)
	if !hasQCMessage(issues, "未进分镜") || !hasQCMessage(issues, "先去洗菜") {
		t.Fatalf("expected missing Pei line R2, got %#v", issues)
	}
}

func TestDetectMissingScriptDialogueDoesNotAcceptLongPrefix(t *testing.T) {
	script := `**韩铮**（低声）："韩家的孩子……先活下去。福公你话说一半就跑，差评。"`
	issues := detectMissingScriptDialogue([]ShotContext{{
		ID: 2, Index: 2,
		Script: "【0-3秒】镜头：近景；韩铮说：「韩家的孩子……先活下去。」",
	}}, script)
	if !hasQCMessage(issues, "未进分镜") {
		t.Fatalf("a long prefix must not count as the complete line: %#v", issues)
	}
}

func TestFaithfulModernDialogueIsNotMetaIssue(t *testing.T) {
	source := `韩铮："行。主线任务更新：忍膳房双鬼。"`
	shots := []ShotContext{{ID: 1, Script: "【0-5秒】镜头：近景；韩铮说：「行。主线任务更新：忍膳房双鬼。」"}}
	issues := detectScriptFormatIssuesAgainst(shots, source)
	if hasQCMessage(issues, "第四面墙") {
		t.Fatalf("faithful source dialogue must not be treated as model meta junk: %#v", issues)
	}
}

func TestCharacterMentionOnlyInsideDialogueDoesNotRequireRef(t *testing.T) {
	shots := []ShotContext{{ID: 1, Script: "【0-5秒】镜头：裴长河近景；裴长河说：「记住——少惹姚三刀。」", Refs: []ShotRefInfo{{Kind: "character", Name: "裴长河", ResourceID: 1}, {Kind: "scene", Name: "侧库", ResourceID: 3}}}}
	assets := []AssetItem{{Name: "裴长河", Type: "character", ResourceID: 1}, {Name: "姚三刀", Type: "character", ResourceID: 2}, {Name: "侧库", Type: "scene", ResourceID: 3}}
	issues := detectMissingAssetRefs(shots, assets)
	if hasQCMessage(issues, "姚三刀") {
		t.Fatalf("an off-screen name spoken in dialogue is not a visual ref: %#v", issues)
	}
}

func TestDedupeKeepsFormatAndMultipleMissingDialogueIssues(t *testing.T) {
	issues := dedupeQCIssues([]QCIssue{
		{Severity: "high", Code: "R2", ShotID: 2, Message: "分镜文案含字面量 \\n，时序没有正确分行"},
		{Severity: "high", Code: "R2", ShotID: 2, Message: "剧本 韩铮 的台词未进分镜：「第一句关键信息」"},
		{Severity: "high", Code: "R2", ShotID: 2, Message: "剧本 韩铮 的台词未进分镜：「第二句关键信息」"},
	})
	if len(issues) != 3 {
		t.Fatalf("distinct R2 failures must not hide each other: %#v", issues)
	}
}

func TestScriptBeatsExpandsLiteralNewline(t *testing.T) {
	beats := scriptBeats("【0-3秒】镜头：A\\n【3-7秒】镜头：B\\n【7-10秒】镜头：C")
	if len(beats) != 3 {
		t.Fatalf("expected 3 beats after \\n expand, got %d: %#v", len(beats), beats)
	}
}

func TestDetectScriptFormatShortDialogueLongBeat(t *testing.T) {
	issues := detectScriptFormatIssues([]ShotContext{{
		ID:     7,
		Index:  7,
		Script: "【0-3秒】镜头：近景；韩铮说：「小郡王生辰……现代菜能上桌吗？」\n【3-10秒】镜头：中景；韩铮说：「先活过今天。」",
	}})
	if !hasQCMessage(issues, "短台词") {
		t.Fatalf("expected short dialogue long beat R2, got %#v", issues)
	}
}

func TestDetectOffscreenSpokenSpeakers(t *testing.T) {
	issues := detectOffscreenSpokenSpeakers([]ShotContext{{
		ID:    12,
		Index: 12,
		Script: "【0-3秒】镜头：特写，金万两(中心)朝右盯楚鸿霄；音效：低鼓点；金万两说：「你看着我。」\n" +
			"【3-7秒】镜头：中近景，楚鸿霄看着金万两；音效：低鼓点；谢无尘说：「金掌柜……何妨宽限五十年。」\n" +
			"【7-10秒】镜头：近景，谢无尘(右中)开口说话；音效：低鼓点；谢无尘说：「欠债好商量。」",
	}})
	if !hasQCMessage(issues, "说话人未进画面") || !hasQCMessage(issues, "谢无尘") {
		t.Fatalf("expected offscreen speaker R2 for 谢无尘, got %#v", issues)
	}
	if hasQCMessage(issues, "金万两") {
		t.Fatalf("onscreen speaker should not be flagged: %#v", issues)
	}
}

func TestDetectOffscreenSpokenSpeakersSkipsInnerMonologue(t *testing.T) {
	issues := detectOffscreenSpokenSpeakers([]ShotContext{{
		ID:     3,
		Index:  3,
		Script: "【0-10秒】镜头：全景，韩铮独自站在门口；音效：风声；裴长河内心独白：「先活过今天。」",
	}})
	if hasQCMessage(issues, "说话人未进画面") {
		t.Fatalf("inner monologue must not require on-screen speaker: %#v", issues)
	}
}
