package crew

import (
	"strings"
	"testing"
)

func TestBGMFillsSubstantiveBeatsOnly(t *testing.T) {
	script := "【0-4秒】镜头：中景；音效：低沉鼓点\n【4-10秒】镜头：近景藏腰牌；音效：衣料摩擦"
	got := prependBGMToAllBeats(script, "紧张鼓点")
	if !strings.Contains(got, "衣料摩擦") || !strings.Contains(got, "紧张鼓点") {
		t.Fatalf("substantive beats should get BGM bed: %s", got)
	}
	second := strings.Split(got, "\n")[1]
	if !strings.Contains(second, "紧张鼓点") {
		t.Fatalf("second beat missing bed: %s", second)
	}
}

func TestBGMSkipsPlotlessBeatLine(t *testing.T) {
	script := "【0-3秒】镜头：中景，韩铮开口；音效：低沉鼓点\n【3-7秒】镜头：近景继续；音效：衣料摩擦\n【7-10秒】音效：紧张鼓点"
	got := prependBGMToAllBeats(script, "紧张鼓点")
	if strings.Contains(got, "【7-10秒】音效：紧张鼓点、紧张鼓点") {
		t.Fatalf("should not double-fill plotless beat: %s", got)
	}
}

func TestFixKeepsDrumBGMAndFillsGap(t *testing.T) {
	shots := ApplyQCFixes([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：近景；音效：低鼓点、冰水声；",
	}, {
		ID:     2,
		Index:  2,
		Script: "【0-3秒】镜头：中景；音效：脚步声",
	}}, nil, []QCIssue{{
		Code:       "R5",
		ShotID:     2,
		ShotIndex:  2,
		Message:    "上一镜配乐是「低鼓点」，这一镜音效里断了",
		Suggestion: "本镜音效续上「低鼓点」",
	}})
	if !containsAll(shots[0].Script, "低鼓点", "冰水声") {
		t.Fatalf("should keep shot1 BGM, got %s", shots[0].Script)
	}
	if !containsAll(shots[1].Script, "低鼓点", "脚步声") {
		t.Fatalf("should fill shot2 BGM bed, got %s", shots[1].Script)
	}
}

func TestFixDoesNotAddDuplicateAliasProps(t *testing.T) {
	assets := []AssetItem{
		{Name: "沾血拳击绷带", Type: "prop", ResourceID: 3},
		{Name: "沾血绷带", Type: "prop", ResourceID: 30},
		{Name: "地下拳赛冠军奖牌", Type: "prop", ResourceID: 4},
		{Name: "冠军奖牌", Type: "prop", ResourceID: 40},
		{Name: "韩铮（赤膊赛后）", Type: "character", ResourceID: 8, ParentName: "韩铮", ParentID: 1, IsDerivative: true},
		{Name: "赤膊赛后态", Type: "character", ResourceID: 80, ParentName: "韩铮", ParentID: 1, IsDerivative: true},
	}
	shots := ApplyQCFixes([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：沾血绷带入冰水。韩铮坐着，看向冠军奖牌。",
		Refs: []ShotRefInfo{
			{Kind: "prop", Name: "沾血拳击绷带", ResourceID: 3},
			{Kind: "prop", Name: "地下拳赛冠军奖牌", ResourceID: 4},
			{Kind: "character", Name: "韩铮（赤膊赛后）", DisplayName: "韩铮（赤膊赛后）", ParentName: "韩铮", ResourceID: 8, IsDerivative: true},
		},
	}}, assets, []QCIssue{
		{Code: "R1", ShotID: 1, ShotIndex: 1, Message: `文案出现道具「沾血绷带」，但本镜 refs 未绑定`},
		{Code: "R1", ShotID: 1, ShotIndex: 1, Message: `文案出现道具「冠军奖牌」，但本镜 refs 未绑定`},
		{Code: "R1", ShotID: 1, ShotIndex: 1, Message: `文案出现「赤膊赛后态」，但本镜 refs 未绑定`},
	})
	got := map[uint]bool{}
	for _, r := range shots[0].Refs {
		got[r.ResourceID] = true
	}
	if got[30] || got[40] || got[80] {
		t.Fatalf("should not bind alias resources, got %#v", shots[0].Refs)
	}
	if !got[3] || !got[4] || !got[8] {
		t.Fatalf("should keep original refs, got %#v", shots[0].Refs)
	}
}

func TestFixBindsScriptMentionedGridCharacters(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1},
		{Name: "小鹿", Type: "character", ResourceID: 2},
		{Name: "小南", Type: "character", ResourceID: 3},
		{Name: "杀手甲", Type: "character", ResourceID: 8},
	}
	shots := ApplyQCFixes([]ShotContext{{
		ID:     2,
		Index:  2,
		Script: "【0-3秒】镜头：中景，韩铮(左前)吃串，小鹿(右前)媚笑。",
		Refs:   nil,
	}}, assets, []QCIssue{
		{Code: "R1", ShotID: 2, ShotIndex: 2, ResourceID: 1, Message: `文案出现「韩铮」，但本镜 refs 未绑定该角色`},
		{Code: "R1", ShotID: 2, ShotIndex: 2, ResourceID: 2, Message: `文案出现「小鹿」，但本镜 refs 未绑定该角色`},
	})
	got := map[uint]bool{}
	for _, r := range shots[0].Refs {
		got[r.ResourceID] = true
	}
	if !got[1] || !got[2] {
		t.Fatalf("grid-named people must bind, got %#v", shots[0].Refs)
	}
	if got[8] {
		t.Fatalf("crowd extra should not auto-bind from grid extract, got %#v", shots[0].Refs)
	}
}

func TestFixBindsMissingPropsAndCharacters(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1},
		{Name: "杀手甲", Type: "character", ResourceID: 8},
		{Name: "冰水桶", Type: "prop", ResourceID: 3},
		{Name: "地下拳王奖杯", Type: "prop", ResourceID: 4},
		{Name: "更衣室", Type: "scene", ResourceID: 2},
	}
	shots := ApplyQCFixes([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：韩铮看着冰水桶和地下拳王奖杯。杀手甲站在门口。",
		Refs:   []ShotRefInfo{{Kind: "character", Name: "韩铮", ResourceID: 1}},
	}}, assets, []QCIssue{
		{Code: "R1", ShotID: 1, ShotIndex: 1, ResourceID: 3, Message: `文案出现道具「冰水桶」，但本镜 refs 未绑定`},
		{Code: "R1", ShotID: 1, ShotIndex: 1, ResourceID: 4, Message: `文案出现道具「地下拳王奖杯」，但本镜 refs 未绑定`},
		{Code: "R1", ShotID: 1, ShotIndex: 1, ResourceID: 8, Message: `文案出现「杀手甲」，但本镜 refs 未绑定该角色`},
		{Code: "R1", ShotID: 1, ShotIndex: 1, Message: "本镜没有绑定场景参考图"},
	})
	got := map[uint]string{}
	for _, r := range shots[0].Refs {
		got[r.ResourceID] = r.Kind
	}
	for _, id := range []uint{1, 2, 3, 4, 8} {
		if got[id] == "" {
			t.Fatalf("missing ref %d in %#v", id, shots[0].Refs)
		}
	}
}

func TestFixSwapsShirtlessRefOnDressingShot(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1},
		{Name: "赤膊战损", Type: "character", ResourceID: 11, ParentID: 1, ParentName: "韩铮", IsDerivative: true},
	}
	shots := ApplyQCFixes([]ShotContext{{
		ID:     2,
		Index:  2,
		Script: "【0-3秒】镜头：韩铮扣衬衫扣子。",
		Refs: []ShotRefInfo{{
			Kind: "character", Name: "赤膊战损", DisplayName: "韩铮（赤膊战损）",
			ParentName: "韩铮", ParentID: 1, IsDerivative: true, ResourceID: 11,
		}},
	}}, assets, []QCIssue{{
		Code: "R4", ShotID: 2, ShotIndex: 2,
		Message: "文案在穿衣/扣衬衫，参考图却仍是赤膊衍生",
	}})
	if len(shots[0].Refs) != 1 || shots[0].Refs[0].ResourceID != 1 {
		t.Fatalf("expected parent 韩铮, got %#v", shots[0].Refs)
	}
}

func TestFixSkipsAccessoryR4CostumeSwap(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1},
		{Name: "赤膊战损", Type: "character", ResourceID: 11, ParentID: 1, ParentName: "韩铮", IsDerivative: true},
	}
	shots := ApplyQCFixes([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：韩铮浸冰水。",
		Refs: []ShotRefInfo{{
			Kind: "character", Name: "赤膊战损", ParentName: "韩铮", ParentID: 1, IsDerivative: true, ResourceID: 11,
		}},
	}}, assets, []QCIssue{{
		Code: "R4", ShotID: 1, ShotIndex: 1,
		Message:    "同一角色「韩铮」各镜参考图配饰不一致（有的有奖牌，有的是无奖牌/项链）",
		Suggestion: "统一角色设定：奖牌不要画进人物图。",
	}})
	if len(shots[0].Refs) != 1 || shots[0].Refs[0].ResourceID != 11 {
		t.Fatalf("accessory R4 must not swap costume refs, got %#v", shots[0].Refs)
	}
}

func TestFixDropsDuplicateDerivativeRefs(t *testing.T) {
	shots := ApplyQCFixes([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：韩铮坐着浸冰水。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "赛后赤膊", DisplayName: "韩铮（赛后赤膊）", ParentName: "韩铮", ParentID: 1, IsDerivative: true, ResourceID: 1316},
			{Kind: "character", Name: "赤膊状态", DisplayName: "韩铮（赤膊状态）", ParentName: "韩铮", ParentID: 1, IsDerivative: true, ResourceID: 1326},
		},
	}}, nil, []QCIssue{{
		Code: "R1", ShotID: 1, ShotIndex: 1,
		Message:    "同一父角色在本镜重复绑定了两张衍生图（1316、1326）",
		Suggestion: "删除 1316，保留 1326。",
	}})
	if len(shots[0].Refs) != 1 || shots[0].Refs[0].ResourceID != 1326 {
		t.Fatalf("expected only 1326, got %#v", shots[0].Refs)
	}
}

func TestFixOverlapR1WithoutStockPhrase(t *testing.T) {
	shots := ApplyQCFixes([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：韩铮坐着浸冰水。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "韩铮", ResourceID: 1},
			{Kind: "character", Name: "赤膊战损", ParentName: "韩铮", ParentID: 1, IsDerivative: true, ResourceID: 11},
		},
	}}, nil, []QCIssue{{
		Code: "R1", ShotID: 1, ShotIndex: 1,
		Message: "同一角色两个资源 1 和 11 同镜，禁止日常图和衍生图同时出现",
	}})
	if len(shots[0].Refs) != 1 || shots[0].Refs[0].ResourceID != 11 {
		t.Fatalf("shirtless script should keep child, got %#v", shots[0].Refs)
	}
}

func TestFixAccessoryR4BindsMedalProp(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1},
		{Name: "地下拳赛冠军奖牌", Type: "prop", ResourceID: 20, Description: "银质奖牌"},
	}
	shots := ApplyQCFixes([]ShotContext{{
		ID:     4,
		Index:  4,
		Script: "【0-3秒】镜头：韩铮看奖牌。",
		Refs:   []ShotRefInfo{{Kind: "character", Name: "韩铮", ResourceID: 1}},
	}}, assets, []QCIssue{{
		Code: "R4", ShotID: 4, ShotIndex: 4,
		Message:    "同一角色各镜参考图配饰不一致（有的有奖牌）",
		Suggestion: "奖牌不要画进人物图，改绑道具。",
	}})
	got := map[uint]string{}
	for _, r := range shots[0].Refs {
		got[r.ResourceID] = r.Kind
	}
	if got[1] != "character" || got[20] != "prop" {
		t.Fatalf("expected 韩铮 + 奖牌, got %#v", shots[0].Refs)
	}
}

func TestFixDropsParentWhenChildPresent(t *testing.T) {
	shots := ApplyQCFixes([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：韩铮坐着浸冰水。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "韩铮", ResourceID: 1},
			{Kind: "character", Name: "赤膊战损", DisplayName: "韩铮（赤膊战损）", ParentName: "韩铮", ParentID: 1, IsDerivative: true, ResourceID: 11},
		},
	}}, nil, []QCIssue{{
		Code: "R1", ShotID: 1, ShotIndex: 1,
		Message: "同一角色同时绑了日常图和换装图（韩铮（赤膊战损））",
	}})
	if len(shots[0].Refs) != 1 || shots[0].Refs[0].ResourceID != 11 {
		t.Fatalf("expected only child ref, got %#v", shots[0].Refs)
	}
}

func TestFixStripsShirtlessWhenPuttingOn(t *testing.T) {
	shots := ApplyQCFixes([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【0-3秒】镜头：赤膊的韩铮坐着。\n【7-10秒】镜头：韩铮伸手接衬衫。",
	}}, nil, []QCIssue{{
		Code: "R3", ShotID: 1, ShotIndex: 1,
		Message:    "同一镜先写赤膊/没穿上衣，后又接衬衫、扣扣子",
		Suggestion: "不要再写「赤膊」",
	}})
	if containsAny(shots[0].Script, "赤膊") {
		t.Fatalf("should strip 赤膊, got %s", shots[0].Script)
	}
	if !containsAll(shots[0].Script, "接衬衫") {
		t.Fatalf("should keep action, got %s", shots[0].Script)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func TestFixBindsBoxerFromUnquotedR1(t *testing.T) {
	assets := []AssetItem{{Name: "拳手甲", Type: "character", ResourceID: 9}}
	shots := ApplyQCFixes([]ShotContext{{
		ID:     470,
		Index:  5,
		Script: "【0-3秒】镜头：拳手甲站在门口问话。",
		Refs:   []ShotRefInfo{{Kind: "character", Name: "韩铮", ResourceID: 1}},
	}}, assets, []QCIssue{{
		Code: "R1", ShotID: 470, ShotIndex: 5,
		Message: "角色拳手甲出现在分镜中，但资产名单未绑定，refs 也没有拳手甲",
	}})
	got := map[uint]bool{}
	for _, r := range shots[0].Refs {
		got[r.ResourceID] = true
	}
	if !got[9] {
		t.Fatalf("expected 拳手甲 ref 9, got %#v", shots[0].Refs)
	}
}

func TestFixPullsMisattributedDialogue(t *testing.T) {
	shots := ApplyQCFixes([]ShotContext{{
		ID:     468,
		Index:  3,
		Script: "【0-3秒】镜头：阿彪冷笑；「告诉你老板……」",
	}, {
		ID:     469,
		Index:  4,
		Script: "【0-3秒】镜头：韩铮扣扣子；「下一轮我请他吃小炒。」",
	}}, nil, []QCIssue{{
		Code: "R2", ShotID: 468, ShotIndex: 3,
		Message: "阿彪的完整台词被拆成两段，后半句被派给韩铮",
	}})
	if !strings.Contains(shots[0].Script, "小炒") {
		t.Fatalf("should pull continuation into shot 468, got %s", shots[0].Script)
	}
	if strings.Contains(shots[1].Script, "小炒") {
		t.Fatalf("should remove misattributed quote from 469, got %s", shots[1].Script)
	}
}

func TestFixAddsMissingSpeakerEvenIfNameInAction(t *testing.T) {
	shots := ApplyQCFixes([]ShotContext{{
		ID:    2,
		Index: 2,
		Script: "【0-3秒】镜头：中景，阿彪把衬衫扔向韩铮；音效：低鼓点。「铮哥，今晚庆功局订好了。」\n" +
			"【3-7秒】镜头：近景，韩铮嘴角上扬；「断财路？那叫市场调控。」",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "阿彪", ResourceID: 2},
			{Kind: "character", Name: "韩铮", ResourceID: 1},
		},
	}}, nil, []QCIssue{{
		Code: "R2", ShotID: 2, ShotIndex: 2,
		Message:    "台词未标明说话人",
		Suggestion: "写成 阿彪说：「……」和 韩铮说：「……」",
	}})
	if !strings.Contains(shots[0].Script, "阿彪说：「") {
		t.Fatalf("missing 阿彪说：\n%s", shots[0].Script)
	}
	if !strings.Contains(shots[0].Script, "韩铮说：「") {
		t.Fatalf("missing 韩铮说：\n%s", shots[0].Script)
	}
	if strings.Count(shots[0].Script, "阿彪说：") > 1 && strings.Contains(shots[0].Script, "阿彪说：阿彪说：") {
		t.Fatalf("duplicated speaker prefix:\n%s", shots[0].Script)
	}
}

func TestDialoguePipelineScopedToR2ShotOnly(t *testing.T) {
	shots := []ShotContext{
		{ID: 6, Index: 6, Script: "【0-4秒】镜头：中景；韩铮说：「命硬。」\n【4-10秒】镜头：近景，韩铮藏腰牌；音效：衣料摩擦"},
		{ID: 14, Index: 14, Script: "【0-3秒】裴长河说：「先去洗菜。今日小郡王生辰备料，错一份，」"},
	}
	issues := []QCIssue{
		{Code: "R1", ShotID: 6, ShotIndex: 6, Message: "姚三刀 refs"},
		{Code: "R2", ShotID: 14, ShotIndex: 14, Message: "这段约 3 秒，台词 17 字，按 4 字/秒会说不完（上限 12 字）"},
	}
	ids := dialoguePipelineShotIDs(shots, issues)
	if ids[6] {
		t.Fatal("R1-only shot 6 must not enter dialogue pipeline")
	}
	if !ids[14] {
		t.Fatal("R2 shot 14 must enter dialogue pipeline")
	}
}

func TestRewriteAllowedSkipsR1OnlyBatch(t *testing.T) {
	shots := []ShotContext{{ID: 6, Index: 6}, {ID: 14, Index: 14}}
	issues := []QCIssue{
		{Code: "R1", ShotID: 6, ShotIndex: 6, Message: "姚三刀 refs"},
		{Code: "R2", ShotID: 14, ShotIndex: 14, Message: "这段约 3 秒，台词 17 字，按 4 字/秒会说不完（上限 12 字）"},
	}
	editorial := editorialQCIssues(issues)
	if len(editorial) != 0 {
		t.Fatalf("overlong R2 is mechanical, expected no editorial issues, got %#v", editorial)
	}
	if QCIssuesNeedRewrite(issues) {
		t.Fatal("mechanical batch must not trigger AI rewrite")
	}
	allowed := rewriteAllowedIDs(shots, editorial)
	if len(allowed) != 0 {
		t.Fatalf("no editorial issues => empty rewrite set, got %#v", allowed)
	}
}

func TestQCIssuesNeedRewriteSkipsMechanicalR3Lens(t *testing.T) {
	issues := []QCIssue{
		{Code: "R1", Message: "refs未绑定焦黑腰牌"},
		{Code: "R3", Message: "时序行缺少「镜头：景别+运镜+动作」"},
		{Code: "R5", Message: "本集音效没有配乐床"},
	}
	if QCIssuesNeedRewrite(issues) {
		t.Fatal("ref/lens/BGM batch should not trigger AI rewrite")
	}
	if IssuesNeedDialoguePipeline(issues) {
		t.Fatal("ref/lens/BGM batch should not run restore+split pipeline")
	}
}

func TestQCIssuesNeedRewriteIncludesAmbiguousR3Target(t *testing.T) {
	issues := []QCIssue{{
		Code:       "R3",
		ShotID:     8,
		ShotIndex:  8,
		Message:    "动作使用了“听者/对方/对面的人”等模糊指代，视频模型无法确定互动对象",
		Suggestion: "把模糊指代改成当前镜中具体角色姓名",
	}}
	if !QCIssuesNeedRewrite(issues) {
		t.Fatal("ambiguous R3 target needs contextual editorial rewrite")
	}
	if got := editorialQCIssues(issues); len(got) != 1 {
		t.Fatalf("expected ambiguous R3 in editorial batch, got %#v", got)
	}
}

func TestMissingDialogueScopeIncludesFollowingShot(t *testing.T) {
	shots := []ShotContext{{ID: 1, Index: 1}, {ID: 2, Index: 2}}
	ids := dialoguePipelineShotIDs(shots, []QCIssue{{
		Code: "R2", ShotID: 1, ShotIndex: 1,
		Message: "剧本 陆铁算盘 的台词未进分镜：「掌门，养一个五岁孩子，每月至少要——」",
	}})
	if !ids[1] || !ids[2] {
		t.Fatalf("missing dialogue restore should include reported and following shot, got %#v", ids)
	}
}

func TestPrepareAfterFixRestoresMissingDialogueDespiteLockedSchedule(t *testing.T) {
	line := "掌门，养一个五岁孩子，每月至少要三十两银子，还要添置四季衣裳和每日饭食。"
	shots := []ShotContext{
		{ID: 1, Index: 1, Duration: 10, Label: "对白一", Script: "【0-10秒】镜头：陆铁算盘拨动算盘；音效：配乐床、环境声"},
		{ID: 2, Index: 2, Duration: 10, Label: "对白二", Script: "【0-10秒】镜头：金万两抬眼；音效：配乐床、环境声"},
	}
	got := PrepareShotsAfterFix(shots, nil, "陆铁算盘："+line, nil, []QCIssue{{
		Code: "R2", ShotID: 1, ShotIndex: 1,
		Message: "剧本 陆铁算盘 的台词未进分镜：「掌门，养一个五岁孩子，每月至少要三十两银子，…」",
	}})
	joined := got[0].Script + "\n" + got[1].Script
	allQuotes := ""
	for _, shot := range got {
		for _, quote := range quotesInScript(shot.Script) {
			allQuotes += dialogueCoverageKey(quote)
		}
	}
	if !strings.Contains(allQuotes, dialogueCoverageKey(line)) {
		t.Fatalf("missing source dialogue was not restored through locked schedule:\n%s", joined)
	}
	for _, issue := range detectMissingScriptDialogue(got, "陆铁算盘："+line) {
		if issue.Code == "R2" {
			t.Fatalf("restored source line must pass missing-dialogue recheck: %#v\n%s", issue, joined)
		}
	}
}

func TestApplyQCFixesConcretizesAmbiguousTargetWithoutAI(t *testing.T) {
	shots := ApplyQCFixes([]ShotContext{{
		ID: 8, Index: 8,
		Script: "【0-3秒】镜头：近景，洛清霜(左前)盯住对方的眼睛；洛清霜说：「你认得对方吗？」",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "洛清霜"},
			{Kind: "character", Name: "韩铮"},
		},
	}}, nil, []QCIssue{{
		Code: "R3", ShotID: 8, ShotIndex: 8,
		Message: "动作使用了“听者/对方/对面的人”等模糊指代，视频模型无法确定互动对象",
	}})
	if strings.Contains(stripQuotedDialogue(shots[0].Script), "对方") {
		t.Fatalf("action ambiguity survived deterministic fallback: %s", shots[0].Script)
	}
	if !strings.Contains(shots[0].Script, "盯住韩铮的眼睛") {
		t.Fatalf("expected concrete target name, got: %s", shots[0].Script)
	}
	if !strings.Contains(shots[0].Script, "你认得对方吗") {
		t.Fatalf("spoken dialogue must remain unchanged: %s", shots[0].Script)
	}
}

func TestQCIssuesNeedRewriteKeepsEditorialR2(t *testing.T) {
	issues := []QCIssue{{
		Code: "R2", Message: "阿彪的完整台词被拆成两段，后半句被派给韩铮",
	}}
	if !QCIssuesNeedRewrite(issues) {
		t.Fatal("editorial R2 should still trigger rewrite")
	}
	if !IssuesNeedDialoguePipeline(issues) {
		t.Fatal("R2 should run dialogue pipeline")
	}
}

func TestSplitQuoteForBeatAtClauseNotMidWord(t *testing.T) {
	full := "先去洗菜。今日小郡王生辰备料，错一份，咱们全膳房喝西北风。记住——少惹姚三刀。"
	keep, rest := splitQuoteForBeat(full, 3)
	if strings.HasSuffix(keep, "备") && strings.HasPrefix(strings.TrimLeft(rest, "…—–-"), "料") {
		t.Fatalf("must not split 备/料 mid-word, got keep=%q rest=%q", keep, rest)
	}
	if keep == "" && rest != full {
		t.Fatalf("when 3s cannot fit, defer whole quote, got keep=%q rest=%q", keep, rest)
	}
	keep4, rest4 := splitQuoteForBeat(full, 7)
	if keep4 != "" && rest4 != "" && quoteSplitMidWord(keep4, rest4) {
		t.Fatalf("7s split mid-word: keep=%q rest=%q", keep4, rest4)
	}
}

func TestSplitQuoteForBeatNeverCutsReliableWord(t *testing.T) {
	if !quoteSplitMidWord("裴师傅，生辰宴主菜交给我靠", "谱人。韩小灶——去择菜叶") {
		t.Fatal("靠/谱人 must be recognized as a bad split")
	}
	keep, rest := splitQuoteForBeat("裴师傅生辰宴主菜交给我靠谱人韩小灶去择菜叶", 3)
	if strings.HasSuffix(keep, "靠") && strings.HasPrefix(rest, "谱人") {
		t.Fatalf("must not split 靠/谱人, got keep=%q rest=%q", keep, rest)
	}
}

func TestSplitOverlongMovesWholeQuoteNotMidWord(t *testing.T) {
	full := "先去洗菜。今日小郡王生辰备料，错一份，咱们全膳房喝西北风。记住——少惹姚三刀。"
	shot := ShotContext{
		Duration: 10,
		Script: "【0-3秒】镜头：中近景，裴长河叮嘱；音效：低沉鼓点；裴长河说：「" + full + "」\n" +
			"【3-7秒】镜头：裴长河走向门口；音效：低沉鼓点\n" +
			"【7-10秒】镜头：裴长河出门；音效：低沉鼓点",
	}
	splitOverlongDialogue(&shot, nil)
	if strings.Contains(shot.Script, "「料") || strings.Contains(shot.Script, "备」\n") {
		t.Fatalf("mid-word split:\n%s", shot.Script)
	}
	if !strings.Contains(shot.Script, "生辰备料") {
		t.Fatalf("备料 must stay together:\n%s", shot.Script)
	}
}

func TestSplitOverlongPlacesOverflowInEarlierBeat(t *testing.T) {
	full := "先去洗菜。今日小郡王生辰备料，错一份，咱们全膳房喝西北风。记住——少惹姚三刀。"
	shot := ShotContext{
		Duration: 10,
		Script: "【0-3秒】镜头：反应\n" +
			"【3-6秒】镜头：中近景固定，裴长河(右中)3/4正面朝左，语气严肃准备叮嘱；音效：低沉鼓点\n" +
			"【6-10秒】镜头：维持机位不变；音效：低沉鼓点；裴长河说：「" + full + "」",
	}
	splitOverlongDialogue(&shot, nil)
	joined := strings.Join(quotesInScript(shot.Script), "")
	if !strings.Contains(joined, "少惹姚三刀") || !strings.Contains(joined, "先去洗菜") {
		t.Fatalf("full dialogue must remain across beats, got:\n%s", shot.Script)
	}
	if strings.Count(joined, "少惹姚三刀") != 1 {
		t.Fatalf("tail should appear once, got:\n%s", shot.Script)
	}
	// First beat with dialogue should start the line, not the tail.
	firstQ := firstQuote(shot.Script)
	if !strings.HasPrefix(strings.TrimSpace(firstQ), "先去洗菜") {
		t.Fatalf("dialogue should play forward in time, first quote=%q in\n%s", firstQ, shot.Script)
	}
}

func TestRestoreShotDialoguesScopedNoPanicWhenLeftover(t *testing.T) {
	script := `**裴长河**："先去洗菜。今日小郡王生辰备料，错一份，咱们全膳房喝西北风。记住——少惹姚三刀。"
**韩铮**："小郡王生辰……现代菜能上桌吗？先活过今天。"
**顾满仓**："那小子不是韩小灶吗？怎么还喘气？"`
	shots := make([]ShotContext, 6)
	for i := range shots {
		shots[i] = ShotContext{ID: uint(i + 1), Index: i + 1, Duration: 10, Script: "【0-3秒】镜头：反应"}
	}
	only := map[uint]bool{1: true, 2: true, 3: true, 4: true, 5: true, 6: true}
	got := RestoreShotDialoguesScoped(shots, script, nil, only)
	if len(got) != 6 {
		t.Fatalf("scoped restore must keep shot count, got %d", len(got))
	}
}

func TestPrepareAfterFixSkipsDialoguePipelineForRefsOnly(t *testing.T) {
	full := "先去洗菜。今日小郡王生辰备料，错一份，咱们全膳房喝西北风。记住——少惹姚三刀。"
	orig := "【0-3秒】镜头：近景固定，裴长河(右中)3/4正面朝左；音效：低沉鼓点\n" +
		"【3-7秒】镜头：中近景；裴长河说：「" + full + "」\n" +
		"【7-10秒】镜头：裴长河转身；音效：低沉鼓点"
	got := PrepareShotsAfterFix([]ShotContext{{
		ID: 1, Index: 1, Duration: 10, Script: orig,
	}}, nil, "裴长河："+full, nil, []QCIssue{
		{Code: "R1", Message: "本镜 refs 未绑定裴长河"},
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 shot, got %d", len(got))
	}
	if !strings.Contains(got[0].Script, "少惹姚三刀") {
		t.Fatalf("refs-only fix must not truncate dialogue, got:\n%s", got[0].Script)
	}
	if strings.Count(got[0].Script, full) != 1 {
		t.Fatalf("full line should stay intact, got:\n%s", got[0].Script)
	}
}

func TestFixSplitsTwoQuotesInOneBeat(t *testing.T) {
	shots := ApplyQCFixes([]ShotContext{{
		ID:     6,
		Index:  6,
		Script: "【0-3秒】镜头：对峙；「下一轮我请他吃小炒。」「行，我奉陪。」\n【3-7秒】镜头：中景反应。",
		Refs: []ShotRefInfo{
			{Kind: "character", Name: "小南", ResourceID: 3},
			{Kind: "character", Name: "韩铮", ResourceID: 1},
		},
	}}, nil, []QCIssue{{
		Code: "R2", ShotID: 6, ShotIndex: 6,
		Message:    "两句台词挤在同一拍且未标明说话人",
		Suggestion: "拆成两拍，写成 小南说：「……」和 韩铮说：「……」",
	}})
	beats := scriptBeats(shots[0].Script)
	if len(beats) < 2 {
		t.Fatalf("expected two beats, got %s", shots[0].Script)
	}
	if strings.Count(beats[0], "「") != 1 {
		t.Fatalf("first beat should keep one quote: %s", beats[0])
	}
	if !strings.Contains(beats[1], "奉陪") {
		t.Fatalf("second quote should move to next beat: %s", shots[0].Script)
	}
}

func TestFixSplitsOverlongBeatDialogue(t *testing.T) {
	long := "告诉你老板下一轮我请他吃小炒再加一打啤酒"
	shots := ApplyQCFixes([]ShotContext{{
		ID:     480,
		Index:  15,
		Script: "【0-3秒】镜头：阿彪说话；「" + long + "」\n【3-7秒】镜头：中景反应。",
	}}, nil, []QCIssue{{
		Code: "R2", ShotID: 480, ShotIndex: 15,
		Message: "这段约 3 秒，台词 23 字，按 4 字/秒会说不完",
	}})
	first := firstScriptBeat(shots[0].Script)
	q := firstQuote(first)
	if speechRunes(q) > maxSpeechRunes(3) {
		t.Fatalf("first beat still too long: %q in %s", q, shots[0].Script)
	}
	if !strings.Contains(shots[0].Script, "小炒") && !strings.Contains(shots[0].Script, "啤酒") {
		t.Fatalf("overflow should stay in shot, got %s", shots[0].Script)
	}
}

func TestPrepareShotsForQCRestoresThenSplitsLongDialogue(t *testing.T) {
	original := "告诉你老板下一轮我请他吃小炒再加一打啤酒"
	shots := PrepareShotsForQC([]ShotContext{{
		ID: 1, Index: 1, Duration: 10,
		Script: "【0-3秒】镜头：近景；阿彪说：「告诉你老板下一轮我请他吃小炒」\n【3-7秒】镜头：反应；音效：低鼓点",
	}}, nil, "阿彪：\""+original+"\"")
	if len(shots) == 0 {
		t.Fatal("expected prepared shots")
	}
	for _, beat := range scriptBeats(shots[0].Script) {
		secs := beatSeconds(beat)
		for _, q := range quotesInScript(beat) {
			if secs > 0 && speechRunes(q) > maxSpeechRunes(secs) {
				t.Fatalf("dialogue was restored after splitting and no longer fits: %s", shots[0].Script)
			}
		}
	}
	joined := ""
	for _, shot := range shots {
		for _, q := range quotesInScript(shot.Script) {
			joined += q
		}
	}
	if quoteKey(joined) != quoteKey(original) {
		t.Fatalf("original dialogue must be preserved, got %q want %q", joined, original)
	}
}

func TestRewriteAllowedIncludesNextShotForSplitDialogue(t *testing.T) {
	shots := []ShotContext{{ID: 468, Index: 3}, {ID: 469, Index: 4}}
	got := rewriteAllowedIDs(shots, []QCIssue{{
		Code: "R2", ShotID: 468, ShotIndex: 3,
		Message: "阿彪的台词被拆成两段，后半句派给韩铮",
	}})
	if !got[468] || !got[469] {
		t.Fatalf("R2 split should allow current and next, got %#v", got)
	}
}

func TestRewritePatchesIgnoreUntargetedShots(t *testing.T) {
	shots := []ShotContext{
		{ID: 468, Index: 1, Script: "【0-3秒】阿才：「下一轮我请他吃小炒。」"},
		{ID: 469, Index: 2, Script: "【0-3秒】韩铮扣扣子。"},
	}
	got := applyRewritePatches(shots, []rewriteShotPatch{
		{ID: 468, Script: "【0-3秒】阿才：「下一轮——我请他吃小炒。」"},
		{ID: 469, Script: "【0-3秒】韩铮：「下一轮我请他吃小炒。」"},
	}, map[uint]bool{468: true})
	if !strings.Contains(got[0].Script, "小炒") {
		t.Fatalf("should patch targeted shot, got %s", got[0].Script)
	}
	if strings.Contains(got[1].Script, "小炒") {
		t.Fatalf("must not rewrite neighbor speaker, got %s", got[1].Script)
	}
}

func TestFixDropsDuplicateDialogueOnLaterShot(t *testing.T) {
	line := "哥，心脏那老毛病又疼？"
	shots := ApplyQCFixes([]ShotContext{
		{ID: 1, Index: 1, Script: "【6-10秒】镜头：中景；拳手甲说：「" + line + "」"},
		{ID: 2, Index: 2, Script: "【0-3秒】镜头：中景；拳手甲说：「" + line + "」\n【3-6秒】镜头：近景；韩铮说：「疼才证明我还活着。走，庆功。」"},
	}, nil, []QCIssue{{
		Code: "R2", ShotID: 2, ShotIndex: 2,
		Message: "与分镜1重复同一句台词：「" + line + "」",
	}})
	if strings.Count(shots[0].Script+shots[1].Script, line) != 1 {
		t.Fatalf("later duplicate should be removed, got %#v", []string{shots[0].Script, shots[1].Script})
	}
	if !strings.Contains(shots[1].Script, "疼才证明我还活着") {
		t.Fatalf("should keep original next-shot line, got %s", shots[1].Script)
	}
}

func TestFixSwapsMismatchedSceneRef(t *testing.T) {
	assets := []AssetItem{
		{Name: "私人会所包厢", Type: "scene", ResourceID: 11},
		{Name: "北城铁腕地下拳馆更衣室", Type: "scene", ResourceID: 12},
	}
	shots := ApplyQCFixes([]ShotContext{{
		ID:     1,
		Index:  1,
		Script: "【6-10秒】镜头：中景，韩铮站在更衣室门口。",
		Refs:   []ShotRefInfo{{Kind: "scene", Name: "私人会所包厢", ResourceID: 11}},
	}}, assets, []QCIssue{{
		Code: "R1", ShotID: 1, ShotIndex: 1,
		Message: "文案地点是「北城铁腕地下拳馆更衣室」，但参考图绑的是「私人会所包厢」",
	}})
	if len(shots[0].Refs) == 0 || shots[0].Refs[0].ResourceID != 12 {
		t.Fatalf("should bind locker scene, got %#v", shots[0].Refs)
	}
}

func TestFixUpdatesStaleLabelAndNote(t *testing.T) {
	assets := []AssetItem{
		{Name: "私人会所包厢", Type: "scene", ResourceID: 11},
		{Name: "北城铁腕地下拳馆更衣室", Type: "scene", ResourceID: 12},
		{Name: "韩铮", Type: "character", ResourceID: 1},
		{Name: "小鹿", Type: "character", ResourceID: 2},
		{Name: "阿彪", Type: "character", ResourceID: 3},
	}
	shots := ApplyQCFixes([]ShotContext{{
		ID:     1,
		Index:  1,
		Label:  "会所包厢小鹿喂",
		Note:   "会所包厢小鹿喂",
		Script: "【6-10秒】镜头：中景，韩铮站在更衣室门口。",
		Refs: []ShotRefInfo{
			{Kind: "scene", Name: "私人会所包厢", ResourceID: 11},
			{Kind: "character", Name: "小鹿", DisplayName: "小鹿", ResourceID: 2},
			{Kind: "character", Name: "韩铮", DisplayName: "韩铮", ResourceID: 1},
		},
	}, {
		ID:     2,
		Index:  2,
		Label:  "更衣室韩铮",
		Script: "【0-3秒】镜头：韩铮扣扣子。",
		Refs: []ShotRefInfo{
			{Kind: "scene", Name: "北城铁腕地下拳馆更衣室", ResourceID: 12},
			{Kind: "character", Name: "韩铮", DisplayName: "韩铮", ResourceID: 1},
			{Kind: "character", Name: "阿彪", DisplayName: "阿彪", ResourceID: 3},
		},
	}}, assets, []QCIssue{{
		Code: "R1", ShotID: 1, ShotIndex: 1,
		Message: "文案地点是「北城铁腕地下拳馆更衣室」，但参考图绑的是「私人会所包厢」",
	}})
	if !strings.Contains(shots[0].Label, "更衣室") || strings.Contains(shots[0].Label, "会所") {
		t.Fatalf("label should follow new scene, got %s", shots[0].Label)
	}
	if shots[0].Note != shots[0].Label {
		t.Fatalf("short note should match new label, note=%s label=%s", shots[0].Note, shots[0].Label)
	}
	if shots[1].Label != "更衣室韩铮" {
		t.Fatalf("untouched shot label should stay, got %s", shots[1].Label)
	}
}

func TestFixSanitizesPlatformViolence(t *testing.T) {
	shots := ApplyQCFixes([]ShotContext{{
		ID:     4,
		Index:  4,
		Script: "【6-10秒】镜头：中景，韩铮站在更衣室门口；韩铮说：「谁敢在我冠军夜动刀——他有种。」",
	}}, nil, []QCIssue{{
		Code: "R9", ShotID: 4, ShotIndex: 4,
		Message: "文案有持刀/动刀/刺杀或人身威胁，视频平台会拒审",
	}})
	if strings.Contains(shots[0].Script, "动刀") {
		t.Fatalf("R9 should drop 动刀, got %s", shots[0].Script)
	}
	if !strings.Contains(shots[0].Script, "搅局") || !strings.Contains(shots[0].Script, "他有种") {
		t.Fatalf("should keep swagger, got %s", shots[0].Script)
	}
}

func TestEnsureOnscreenSpokenSpeakers(t *testing.T) {
	shot := ShotContext{
		ID:    9,
		Index: 9,
		Script: "【3-7秒】镜头：中近景，楚鸿霄看着金万两；音效：低鼓点；谢无尘说：「金掌柜……何妨宽限五十年。」",
	}
	ensureOnscreenSpokenSpeakers(&shot)
	if !strings.Contains(shot.Script, "谢无尘说话") {
		t.Fatalf("expected speaker injected into lens, got %s", shot.Script)
	}
	if !strings.Contains(shot.Script, "谢无尘说：「") {
		t.Fatalf("dialogue attribution must remain, got %s", shot.Script)
	}
}

func TestApplyDialogueFixOffscreenSpeaker(t *testing.T) {
	shots := ApplyQCFixes([]ShotContext{{
		ID:     9,
		Index:  9,
		Script: "【3-7秒】镜头：中近景，楚鸿霄看着金万两；音效：低鼓点；谢无尘说：「金掌柜……何妨宽限五十年。」",
	}}, nil, []QCIssue{{
		Code: "R2", ShotID: 9, ShotIndex: 9,
		Message: "说话人未进画面：台词是谢无尘说，但镜头未写谢无尘，视频模型容易对错口型",
		Suggestion: "本拍镜头主体改成谢无尘说话",
	}})
	if !strings.Contains(shots[0].Script, "谢无尘说话") {
		t.Fatalf("R2 onscreen fix should inject speaker, got %s", shots[0].Script)
	}
}
