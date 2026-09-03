package services

import (
	"strings"
	"testing"

	"novaly/backend/models"
)

func TestFormatCharacterVoices(t *testing.T) {
	got := formatCharacterVoices([]CharacterVoice{
		{Name: "韩铮", Prompt: "30岁左右男性，声线低沉沙哑带磁性，不是少年音"},
		{Name: "韩铮", Prompt: "重复应被忽略"},
		{Name: "杀手甲", Prompt: "40岁男性，中低音冷硬短促，不是浑厚男中音"},
		{Name: " ", Prompt: "无效"},
	})
	want := "【声音要求】\n- 韩铮：30岁左右男性，声线低沉沙哑带磁性，不是少年音\n- 杀手甲：40岁男性，中低音冷硬短促，不是浑厚男中音"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if formatCharacterVoices(nil) != "" {
		t.Fatal("empty voices should omit section")
	}
}

func TestBuildVideoPromptIncludesVoices(t *testing.T) {
	got := BuildVideoPrompt(VideoInput{
		Script: "【0-3秒】韩铮说：「先活下去。」",
		CharacterVoices: []CharacterVoice{
			{Name: "韩铮", Prompt: "30岁左右男性，声线低沉沙哑带磁性，不是少年音"},
		},
		Ratio: "16:9",
	})
	for _, part := range []string{"镜头1（0-3秒）", "{先活下去。}", "【声音要求】", "韩铮：30岁左右男性"} {
		if !strings.Contains(got, part) {
			t.Fatalf("prompt missing %q: %s", part, got)
		}
	}
	if strings.Count(got, VideoSpeechConstraint) != 2 {
		t.Fatalf("dialogue rule should be repeated at prompt tail, got %s", got)
	}
	wantSuffix := VideoSpeechConstraint + "\n" + VideoNoSubtitleConstraint + "\n" + NoLogoConstraint
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("dialogue prompt should end with speech then subtitle rules, got %s", got)
	}
}

func TestBuildVideoPromptWithoutDialogueBansInventedSpeech(t *testing.T) {
	got := BuildVideoPrompt(VideoInput{
		Script: "【0-5秒】镜头：过肩斜角，姚三刀下巴点向韩铮方向；音效：低沉鼓点\n【5-10秒】镜头：近景反打韩铮，韩铮垂头躬身；音效：低沉鼓点",
		CharacterVoices: []CharacterVoice{
			{Name: "姚三刀", Prompt: "中年男声，阴冷尖利"},
			{Name: "韩铮", Prompt: "青年男声，低沉克制"},
		},
		Duration: 10,
		Ratio:    "16:9",
	})
	if !strings.Contains(got, VideoNoSpeechConstraint) {
		t.Fatalf("silent shot must explicitly ban invented speech:\n%s", got)
	}
	if strings.Contains(got, "【声音要求】") || strings.Contains(got, "中年男声") || strings.Contains(got, "青年男声") {
		t.Fatalf("silent shot must not send voice descriptions:\n%s", got)
	}
	if strings.Count(got, VideoNoSpeechConstraint) < 2 {
		t.Fatalf("silent constraint should also be repeated at prompt tail:\n%s", got)
	}
}

func TestBuildVideoPromptToonflowLookPack(t *testing.T) {
	got := BuildVideoPrompt(VideoInput{
		Script:   "【0-3秒】韩铮抬头。",
		LookPack: "真人都市电影摄影，真人实拍质感。严禁二次元大眼、赛璐璐、3D卡通",
		Ratio:    "16:9",
	})
	for _, part := range []string{
		"画面质感：真人都市电影摄影",
		"严禁二次元大眼",
		"人物面部稳定不变形",
		"无穿模无卡顿",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("prompt missing %q:\n%s", part, got)
		}
	}
	if strings.Contains(got, "人物面部稳定不变形") && strings.Count(got, "人物面部稳定不变形") != 1 {
		t.Fatalf("quality pack duplicated:\n%s", got)
	}
}

func TestRewriteScriptForSeedanceSpeech(t *testing.T) {
	script := "【0-3秒】中景，阿彪随手将一件白衬衫扔向韩铮，韩铮伸手接住；音效：低沉鼓点配乐床，衬衫划过空气的轻响。「铮哥，今晚庆功局订好了，对手那边放话——你断了人家的财路，小心点。」\n" +
		"【3-7秒】近景，韩铮拿着衬衫嘴角上扬；音效：低沉鼓点持续。「断财路？那叫市场调控。」\n" +
		"【7-10秒】中景，韩铮展开衬衫准备穿上；音效：布料展开的悉索声。「 」"
	got := rewriteScriptForSeedance(script, []refImage{
		{Index: 1, Kind: "character", Label: "阿彪", Name: "阿彪"},
		{Index: 2, Kind: "character", Label: "韩铮（赤膊）", Name: "赤膊"},
	}, []CharacterVoice{
		{Name: "阿彪", Prompt: "青年男声，短促"},
		{Name: "韩铮", Prompt: "30岁男性，低沉沙哑"},
	})
	for _, part := range []string{
		"镜头1（0-3秒）",
		"<主体1> 说 {铮哥，今晚庆功局订好了，对手那边放话——你断了人家的财路，小心点。}",
		"音色：青年男声，短促",
		"<主体2> 说 {断财路？那叫市场调控。}",
		"<低沉鼓点配乐床>",
		"<衬衫划过空气的轻响>",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q:\n%s", part, got)
		}
	}
	if strings.Contains(got, "「") || strings.Contains(got, "」") {
		t.Fatalf("chinese quotes should be converted:\n%s", got)
	}
	if strings.Contains(got, "说 {}") || strings.Contains(got, "「 」") {
		t.Fatalf("empty quote should be dropped:\n%s", got)
	}
}

func TestClipScriptToDurationDropsOverflowBeats(t *testing.T) {
	script := "【0-3秒】镜头：A。\n【7-10秒】镜头：C。\n【10-13秒】镜头：阿彪说：「我去跟那边传话——」"
	got := clipScriptToDuration(script, 10)
	if strings.Contains(got, "传话") || strings.Contains(got, "10-13") {
		t.Fatalf("10s clip should drop overflow, got %s", got)
	}
	if !strings.Contains(got, "7-10") {
		t.Fatalf("should keep in-range beat, got %s", got)
	}
}

func TestClipScriptToDurationDropsStraddlingAndOrphan(t *testing.T) {
	script := "【0-3秒】镜头：A。\n【3-6秒】镜头：B。\n【6-9秒】镜头：C。\n【9-13秒】镜头：闻酱。\n韩铮说：「差评。」"
	got := clipScriptToDuration(script, 10)
	if strings.Contains(got, "9-13") || strings.Contains(got, "闻酱") || strings.Contains(got, "差评") {
		t.Fatalf("straddle+orphan must drop from 10s prompt, got %s", got)
	}
}

func TestClipScriptToDurationDropsOverlappingBeat(t *testing.T) {
	script := "【0-3秒】阿彪说：「订好了。」\n【3-7秒】韩铮说：「市场调控。」\n【7-10秒】韩铮说：「再调控一次。」\n【7-10秒】韩铮说：「你断了人家的赌路，小心点。」"
	got := clipScriptToDuration(script, 10)
	if strings.Contains(got, "小心点") {
		t.Fatalf("overlapping 7-10 beat should be clipped out of 10s prompt, got %s", got)
	}
	if strings.Count(got, "【7-10秒】") != 1 {
		t.Fatalf("prompt should keep a single 7-10 beat, got %s", got)
	}
}

func TestBuildVideoPromptBansSubtitlesAndOverlap(t *testing.T) {
	got := BuildVideoPrompt(VideoInput{
		Script:   "【0-3秒】阿彪说：「订好了。」\n【7-10秒】韩铮说：「再调控一次。」\n【7-10秒】韩铮说：「小心点。」",
		Duration: 10,
		Ratio:    "9:16",
	})
	if !strings.Contains(got, "禁止字幕") || !strings.Contains(got, "不要烧进画面") {
		t.Fatalf("prompt should ban subtitles, got %s", got)
	}
	if strings.Contains(got, "小心点") {
		t.Fatalf("overlapping beat should not reach video prompt, got %s", got)
	}
}

func TestBuildVideoPromptStripsSubtitleDirectivesAndEndsWithNoSubtitleRule(t *testing.T) {
	got := BuildVideoPrompt(VideoInput{
		Script:   "【字幕】三年后\n【0-4秒】韩铮说：「别动。」\n字幕：危险正在靠近",
		Duration: 10,
		Ratio:    "9:16",
	})
	if strings.Contains(got, "三年后") || strings.Contains(got, "危险正在靠近") {
		t.Fatalf("subtitle directives must not reach the video model, got %s", got)
	}
	if !strings.HasSuffix(got, VideoNoSubtitleConstraint+"\n"+NoLogoConstraint) {
		t.Fatalf("prompt should end with the no-subtitle rule before no-logo, got %s", got)
	}
}

func TestCapNamedCharacterVideoRefsKeepsSpeakers(t *testing.T) {
	script := "【0-3秒】小鹿小南尖叫。阿彪去拦。杀手乙把阿彪踢开。\n【6-10秒】韩铮说：「走错包厢了吧？」杀手甲上前。"
	refs := []VideoRef{
		{Kind: "character", Label: "杀手甲", Resource: models.Resource{ID: 1, Type: "character", Name: "杀手甲", ImagePath: "a.jpg"}},
		{Kind: "character", Label: "阿彪", Resource: models.Resource{ID: 2, Type: "character", Name: "阿彪", ImagePath: "b.jpg"}},
		{Kind: "character", Label: "小鹿", Resource: models.Resource{ID: 3, Type: "character", Name: "小鹿", ImagePath: "c.jpg"}},
		{Kind: "character", Label: "小南", Resource: models.Resource{ID: 4, Type: "character", Name: "小南", ImagePath: "d.jpg"}},
		{Kind: "character", Label: "杀手乙", Resource: models.Resource{ID: 5, Type: "character", Name: "杀手乙", ImagePath: "e.jpg"}},
		{Kind: "character", Label: "韩铮", Resource: models.Resource{ID: 6, Type: "character", Name: "韩铮", ImagePath: "f.jpg"}},
	}
	got := capNamedCharacterVideoRefs(refs, script)
	names := map[string]bool{}
	for _, r := range got {
		names[r.Label] = true
	}
	if !names["韩铮"] || !names["阿彪"] {
		t.Fatalf("should keep speaker and main action, got %#v", names)
	}
	if names["杀手甲"] || names["杀手乙"] {
		t.Fatalf("crowd extras should drop, got %#v", names)
	}
	if !names["韩铮"] || !names["小鹿"] || !names["小南"] {
		t.Fatalf("named people must all keep a sheet, got %#v", names)
	}
}

func TestBuildVideoPromptUsesPositioningForCrowd(t *testing.T) {
	got := BuildVideoPrompt(VideoInput{
		Script: "【0-3秒】小鹿小南尖叫。韩铮说：「走错包厢了吧？」",
		Ratio:  "16:9",
		Refs: []VideoRef{
			{Kind: "character", Label: "韩铮", Resource: models.Resource{ID: 1, Type: "character", Name: "韩铮", ImagePath: "han.jpg"}},
			{Kind: "character", Label: "小鹿", Resource: models.Resource{ID: 2, Type: "character", Name: "小鹿", ImagePath: "lu.jpg"}},
			{Kind: "character", Label: "小南", Resource: models.Resource{ID: 3, Type: "character", Name: "小南", ImagePath: "nan.jpg"}},
			{Kind: "character", Label: "杀手甲", Resource: models.Resource{ID: 4, Type: "character", Name: "杀手甲", ImagePath: "jia.jpg"}},
			{Kind: "scene", Label: "站位图", Resource: models.Resource{ID: 9, Type: "scene", Name: "包厢 · 站位图", ImagePath: "pos.jpg", GenType: "positioning"}},
		},
	})
	for _, part := range []string{
		"站位示意图",
		"站位图·群体",
		"站位网格",
		"按站位参考图",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q:\n%s", part, got)
		}
	}
	if strings.Contains(got, "优先按文案九格") || strings.Contains(got, "文案没写格子时才按站位图") {
		t.Fatalf("positioning should not defer to script grid:\n%s", got)
	}
	if !strings.Contains(got, "群演外观参考（来人甲") || strings.Contains(got, "<主体4>（杀手甲）") {
		t.Fatalf("killer extra should be sent as a non-subject appearance reference:\n%s", got)
	}
	if strings.Contains(got, "杀手") {
		t.Fatalf("platform-unsafe 杀手 should be rewritten before send:\n%s", got)
	}
	if strings.Contains(got, "人数锁定") && !strings.Contains(got, "站位图·群体") {
		t.Fatalf("positioning shot should use crowd-from-map, not empty-cast lock:\n%s", got)
	}
}

func TestBuildVideoPromptCrowdLockAndAntiTwin(t *testing.T) {
	got := BuildVideoPrompt(VideoInput{
		Script: "【0-3秒】韩铮看向阿彪。",
		Ratio:  "16:9",
		Refs: []VideoRef{
			{Kind: "character", Label: "韩铮", Resource: models.Resource{ID: 1, Type: "character", Name: "韩铮", ImagePath: "han.jpg"}},
			{Kind: "character", Label: "阿彪", Resource: models.Resource{ID: 2, Type: "character", Name: "阿彪", ImagePath: "biao.jpg"}},
			{Kind: "scene", Label: "更衣室", Resource: models.Resource{ID: 3, Type: "scene", Name: "更衣室", ImagePath: "scene.jpg"}},
		},
	})
	for _, part := range []string{
		"本镜出场人物共2人",
		"人数锁定",
		"双胞胎兜底",
		"多人正面动态",
		"空镜底板",
		"不要从图中抄人",
		"站位网格",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q:\n%s", part, got)
		}
	}
}

func TestBuildVideoPromptSingleCharacterNoAntiTwin(t *testing.T) {
	got := BuildVideoPrompt(VideoInput{
		Script: "【0-3秒】韩铮抬头。",
		Ratio:  "16:9",
		Refs: []VideoRef{
			{Kind: "character", Label: "韩铮", Resource: models.Resource{ID: 1, Type: "character", Name: "韩铮", ImagePath: "han.jpg"}},
		},
	})
	if !strings.Contains(got, "人数锁定") {
		t.Fatalf("single character still needs crowd lock:\n%s", got)
	}
	if strings.Contains(got, "双胞胎兜底") || strings.Contains(got, "多人正面动态") {
		t.Fatalf("solo shot should not hang multi-subject pack:\n%s", got)
	}
	if strings.Contains(got, "站位网格") {
		t.Fatalf("solo shot should not hang spatial grid:\n%s", got)
	}
}

func TestBuildVideoPromptParentChildCountsAsOnePerson(t *testing.T) {
	parent := uint(1)
	got := BuildVideoPrompt(VideoInput{
		Script: "【0-3秒】韩铮抬头。",
		Ratio:  "16:9",
		Refs: []VideoRef{
			{Kind: "character", Label: "韩铮", Resource: models.Resource{ID: 1, Type: "character", Name: "韩铮", ImagePath: "han.jpg"}},
			{Kind: "character", Label: "韩铮（赤膊）", Resource: models.Resource{ID: 2, Type: "character", Name: "赤膊", ImagePath: "han2.jpg", ParentID: &parent}},
		},
	})
	if !strings.Contains(got, "本镜出场人物共1人") {
		t.Fatalf("parent+child should count as one identity:\n%s", got)
	}
	if strings.Contains(got, "双胞胎兜底") {
		t.Fatalf("one identity should not hang anti-twin:\n%s", got)
	}
}

func TestWithSceneEmptyConstraint(t *testing.T) {
	got := withSceneEmptyConstraint("更衣室，木质长椅")
	if !strings.Contains(got, SceneEmptyConstraint) {
		t.Fatalf("should append empty-plate lock: %s", got)
	}
	if withSceneEmptyConstraint(got) != got {
		t.Fatal("should be idempotent")
	}
}

func TestBuildSceneReverseSkeletonPromptHasABCameras(t *testing.T) {
	got := BuildSceneReverseSkeletonPrompt("私人会所包厢", "长桌沙发")
	for _, need := range []string{"这张线稿就是反打", "平视", "过肩", "必须四角换位", "近左→远右", "远右→近左", "必须画人", "火柴人", "不要照片人物", "禁止俯视", "俯视格", "私人会所包厢"} {
		if !strings.Contains(got, need) {
			t.Fatalf("missing %q:\n%s", need, got)
		}
	}
	if strings.Contains(got, "画一条「关系轴线」") || strings.Contains(got, "机位A 正打/原镜头") {
		t.Fatalf("old top-down A/B diagram leaked into reverse-frame skeleton:\n%s", got)
	}
	if strings.Contains(got, "图1左边的人仍在左边") {
		t.Fatalf("reverse camera must not preserve the original screen side:\n%s", got)
	}
	if strings.Contains(got, "【空镜底板】") {
		t.Fatalf("skeleton should not use photoreal empty-plate block:\n%s", got)
	}
}

func TestBuildSceneReversePromptDropsResourceLegend(t *testing.T) {
	got := BuildSceneReversePrompt("包厢站位图", "参考图：图1为阿彪举杯敬韩铮 · 站位图 · 候选1（场景），图2为私人会所包厢·俯视全景·候选1（场景）。\n按图号引用上方参考图，不要弄混。\n生成图1的反打镜头")
	if strings.Contains(got, "阿彪举杯") || strings.Contains(got, "俯视全景") {
		t.Fatalf("【空间】 must not inherit the original 站位图 legend:\n%s", got)
	}
	if !strings.Contains(got, "【空间】包厢站位图") {
		t.Fatalf("should keep the scene name:\n%s", got)
	}
}

func TestOppositeSceneGridCell(t *testing.T) {
	if OppositeSceneGridCell(1) != 5 || OppositeSceneGridCell(5) != 1 {
		t.Fatal("正面/背面 should swap")
	}
	if OppositeSceneGridCell(2) != 6 || OppositeSceneGridCell(6) != 2 {
		t.Fatal("正面近景/背面近景 should swap")
	}
	if OppositeSceneGridCell(0) != 5 {
		t.Fatal("unknown cell should default to 背面全景")
	}
}

func TestBuildSceneReversePromptIsNotAFlip(t *testing.T) {
	got := BuildSceneReversePrompt("私人会所包厢", "长桌沙发")
	for _, need := range []string{"按图1每个火柴人旁的姓名", "骨架优先", "图2", "禁止换人", "俯视格", "反打一侧", "反打图固定要求", "姓名只认图1", "禁止同一姓名出现两次", "人物数=马赛克数=姓名数", "私人会所包厢"} {
		if !strings.Contains(got, need) {
			t.Fatalf("missing %q:\n%s", need, got)
		}
	}
	if strings.Contains(got, "空镜无人") {
		t.Fatalf("reverse photoreal should keep people from the reference:\n%s", got)
	}
	if strings.Contains(got, "俯视线稿") {
		t.Fatalf("photoreal should follow the reverse-frame line drawing, not a top-down plan:\n%s", got)
	}
}

func TestWithSceneReverseSkeletonGuideIdempotent(t *testing.T) {
	src := BuildSceneReversePrompt("包厢", "")
	got := withSceneReverseSkeletonGuide(src)
	if !strings.Contains(got, "【反打优先】") {
		t.Fatalf("missing reverse-priority:\n%s", got)
	}
	again := withSceneReverseSkeletonGuide(got)
	if strings.Count(again, "【反打优先】") != 1 {
		t.Fatalf("should not stack the guide:\n%s", again)
	}
}

func TestWithSceneReverseSkeletonLineArtIdempotent(t *testing.T) {
	src := BuildSceneReverseSkeletonPrompt("包厢", "")
	got := withSceneReverseSkeletonLineArt(src)
	if strings.Count(got, "【线稿硬锁") != 1 {
		t.Fatalf("line-art lock should appear once:\n%s", got)
	}
	again := withSceneReverseSkeletonLineArt(got)
	if strings.Count(again, "【线稿硬锁") != 1 {
		t.Fatalf("should not stack the line-art lock:\n%s", again)
	}
}

func TestBuildSceneGridPromptBansPeople(t *testing.T) {
	got := BuildSceneGridPrompt("更衣室", "", "")
	if !strings.Contains(got, "空镜") || !strings.Contains(got, "严禁出现人物") {
		t.Fatalf("scene grid must be empty plate:\n%s", got)
	}
	if !strings.Contains(got, "禁止白底黑线") {
		t.Fatalf("overhead cells must ban CAD floor plan paste:\n%s", got)
	}
}

func TestNeedsSceneGridPromptRefreshForOldOverheadWithoutCADBan(t *testing.T) {
	old := `输出一张 3×3 九宫格拼图
【空间主体】
库房
格7 第三行左 俯视全景：镜头在天花板往下。必须看见地板，桌子是俯视矩形，酒瓶瓶口朝镜头。禁止平视。
格8 第三行中 俯视近景`
	if !NeedsSceneGridPromptRefresh(old) {
		t.Fatal("old overhead wording without CAD ban should refresh")
	}
	fresh := BuildSceneGridPrompt("库房", "", "")
	if NeedsSceneGridPromptRefresh(fresh) {
		t.Fatalf("fresh template should not refresh:\n%s", fresh)
	}
}

func TestWithSceneGridFloorPlanRefConstraint(t *testing.T) {
	base := "九宫格空镜"
	if got := withSceneGridFloorPlanRefConstraint(base); got != base {
		t.Fatalf("should not append without floor-plan mention:\n%s", got)
	}
	withPlan := base + "\n【二维平面布局图约束】图2是平面图"
	got := withSceneGridFloorPlanRefConstraint(withPlan)
	if !strings.Contains(got, "【平面图仅锁方位·禁止成片】") {
		t.Fatalf("should append floor-plan lock:\n%s", got)
	}
	if again := withSceneGridFloorPlanRefConstraint(got); again != got {
		t.Fatal("should not stack constraint")
	}
}

func TestNormalizeSceneGridPromptRewritesLegacyArchitecture(t *testing.T) {
	legacy := `同一建筑连续摄影，输出为一张 3×3 九宫格拼图
【参考场景锁定模式】
完全相同的山体结构，ArchViz Presentation Board
【九宫格摄影机矩阵】
第三行中：俯视近景，展示屋顶结构
【建筑主体】
私人会所包厢：现代室内夜晚，长桌与卡座，酒水烧烤
【整体画面质感】
真人实拍现代都市，胶片颗粒`
	got := NormalizeSceneGridPrompt(legacy, "私人会所包厢", "")
	if !strings.Contains(got, "瓶口朝镜头") || !strings.Contains(got, "格5") {
		t.Fatalf("should rewrite to numbered camera matrix:\n%s", got)
	}
	if strings.Contains(got, "ArchViz") || strings.Contains(got, "屋顶结构") || strings.Contains(got, "【建筑主体】") {
		t.Fatalf("legacy architecture leftover:\n%s", got)
	}
	if !strings.Contains(got, "长桌与卡座") {
		t.Fatalf("should keep scene subject:\n%s", got)
	}
	if !strings.Contains(got, "胶片颗粒") {
		t.Fatalf("should keep style:\n%s", got)
	}
}

func TestNormalizeSceneGridPromptRefreshesWeakInteriorTemplate(t *testing.T) {
	weak := `同一空间连续摄影，输出为一张 3×3 九宫格拼图
【参考场景锁定模式】
九格必须是同一个空间
【九宫格摄影机矩阵】
第一行左：正面全景，人眼高度
第三行中：俯视近景
【空间主体】
私人会所包厢：长桌沙发
【整体画面质感】
胶片颗粒`
	got := NormalizeSceneGridPrompt(weak, "私人会所包厢", "")
	if !strings.Contains(got, "瓶口朝镜头") {
		t.Fatalf("weak interior template should refresh:\n%s", got)
	}
	if strings.Contains(got, "【九宫格摄影机矩阵】") {
		t.Fatalf("old matrix heading should be gone:\n%s", got)
	}
}

func TestNormalizeSceneGridPromptKeepsNewTemplate(t *testing.T) {
	src := BuildSceneGridPrompt("医殿治疗室", "青石床榻", "仙侠电影感")
	got := NormalizeSceneGridPrompt(src, "医殿治疗室", "")
	if got != src {
		t.Fatalf("new template should pass through")
	}
}

func TestBuildSceneGridPromptIsInteriorOrbit(t *testing.T) {
	got := BuildSceneGridPrompt("私人会所包厢", "包厢长桌与卡座", "")
	for _, need := range []string{
		"3×3 九宫格拼图",
		"格1",
		"格5",
		"格7",
		"正面全景",
		"背面全景",
		"俯视近景",
		"斜向高位总览",
		"瓶口朝镜头",
		"反打",
		"【空间主体】",
		"私人会所包厢",
	} {
		if !strings.Contains(got, need) {
			t.Fatalf("missing %q:\n%s", need, got)
		}
	}
	for _, ban := range []string{
		"同一建筑连续摄影",
		"屋顶结构",
		"ArchViz",
		"山体结构",
		"【建筑主体】",
		"【九宫格摄影机矩阵】",
	} {
		if strings.Contains(got, ban) {
			t.Fatalf("architectural leftover %q still in prompt:\n%s", ban, got)
		}
	}
}
