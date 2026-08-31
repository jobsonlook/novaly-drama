package services

import (
	"strings"
	"testing"
)

func TestWritePositioningContinuityContextIncludesBothSides(t *testing.T) {
	var b strings.Builder
	writePositioningContinuityContext(&b,
		[]string{"分镜03｜场景：御膳房\n韩铮(左中)坐着"},
		[]string{"分镜05｜场景：御膳房\n韩铮仍在左中"},
	)
	got := b.String()
	if !strings.Contains(got, "先筛选同场镜头") || !strings.Contains(got, "韩铮(左中)坐着") {
		t.Fatalf("missing prior same-scene context:\n%s", got)
	}
	if !strings.Contains(got, "仅用于校验连续性") || !strings.Contains(got, "韩铮仍在左中") {
		t.Fatalf("missing guarded following context:\n%s", got)
	}
}

func TestWithPositioningConstraintsAddsSeatedLock(t *testing.T) {
	got := withPositioningConstraints("包厢酒局，韩铮(中中)坐着3/4正面朝右，阿彪(右后)起身举杯。")
	if !strings.Contains(got, "【站位图固定要求】") {
		t.Fatalf("missing mosaic constraint:\n%s", got)
	}
	if !strings.Contains(got, "【姿态锁定】") || !strings.Contains(got, "坐姿") {
		t.Fatalf("seated prompt should lock sitting poses:\n%s", got)
	}
	if !strings.Contains(got, "姿势以提示词为准") {
		t.Fatalf("should tell model to ignore standing sheets:\n%s", got)
	}
}

func TestWithPositioningConstraintsSkipsSeatedLockWhenStandingOnly(t *testing.T) {
	got := withPositioningConstraints("门口对峙，韩铮(左前)站着3/4正面朝右，阿彪(右前)站着3/4正面朝左。")
	if strings.Contains(got, "【姿态锁定】") {
		t.Fatalf("standing-only prompt should not append seated lock:\n%s", got)
	}
}

func TestPositioningPoseCueFromTableScript(t *testing.T) {
	cue := positioningPoseCueFromScript("【0-3秒】镜头：包厢长桌，韩铮举杯；小鹿小南坐在两侧。")
	if !strings.Contains(cue, "坐") {
		t.Fatalf("sitting table script should force seated cue, got %q", cue)
	}
}

func TestEnforcePositioningPoseInjectsWhenModelForgotSit(t *testing.T) {
	script := "【0-3秒】镜头：包厢里众人坐着喝酒，阿彪起身举杯。"
	raw := "16:9，包厢长桌。韩铮(中中)3/4正面朝右，阿彪(右后)起身举杯。\n\n参考图：图1为包厢"
	got := enforcePositioningPoseInPrompt(raw, script)
	if !strings.Contains(got, "坐在桌边") {
		t.Fatalf("should inject seated lock when model omitted 坐着:\n%s", got)
	}
	if !strings.Contains(got, "参考图：图1为包厢") {
		t.Fatalf("legend must remain:\n%s", got)
	}
}

func TestBuildPositioningStickFigurePromptKeepsSitStand(t *testing.T) {
	src := "夜间包厢，韩铮(中中)坐着3/4正面朝右，阿彪(右后)起身举杯3/4正面朝左。\n\n参考图：图1为包厢，图2为韩铮"
	got := buildPositioningStickFigurePrompt(src, "16:9", false)
	if !strings.Contains(got, "纯白背景") || !strings.Contains(got, "火柴人") {
		t.Fatalf("should be a stick-figure prompt:\n%s", got)
	}
	if !strings.Contains(got, "韩铮(中中)坐着") || !strings.Contains(got, "阿彪(右后)起身") {
		t.Fatalf("should keep sit/stand layout:\n%s", got)
	}
	if strings.Contains(got, "参考图：图1为包厢") {
		t.Fatalf("stick-figure pass should drop character/scene legend:\n%s", got)
	}
	if strings.Contains(got, "【站位图固定要求】") {
		t.Fatalf("stick-figure pass should not use photoreal mosaic constraint:\n%s", got)
	}
}

func TestBuildPositioningStickFigurePromptWithSceneRef(t *testing.T) {
	src := "夜间包厢，韩铮(中中)坐着3/4正面朝右，阿彪(右后)起身举杯。\n\n参考图：图1为包厢·正面近景，图2为韩铮"
	got := buildPositioningStickFigurePrompt(src, "16:9", true)
	if !strings.Contains(got, "图1是场景空间底板") || !strings.Contains(got, "九宫格") {
		t.Fatalf("should trace scene layout from 图1:\n%s", got)
	}
	if strings.Contains(got, "纯白背景") {
		t.Fatalf("scene-ref skeleton should not force a blank backdrop:\n%s", got)
	}
	if strings.Contains(got, "参考图：图1为包厢") || strings.Contains(got, "图2为韩铮") {
		t.Fatalf("should drop photoreal legend so the model does not copy 定妆:\n%s", got)
	}
	if !strings.Contains(got, "韩铮(中中)坐着") {
		t.Fatalf("should keep blocking text:\n%s", got)
	}
}

func TestBuildPositioningSkeletonStripsCinematicDetailAndLocksCast(t *testing.T) {
	src := "16:9横构图，古代御膳房大灶间，灶火通红，真人古风写实影视质感，35mm胶片颗粒。" +
		"裴长河(中中)站着3/4正面朝右；韩铮(左中)站着3/4正面朝右，垂手握菜刀；" +
		"顾满仓(右前)站着3/4正面朝左；姚三刀(右中)站着斜身走动，3/4正面朝左朝向裴长河；不要骨架图的群演"
	got := buildPositioningStickFigurePrompt(src, "16:9", true)
	for _, unwanted := range []string{"真人古风写实", "35mm胶片颗粒", "灶火通红"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("skeleton prompt must strip cinematic detail %q:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "人物总数严格等于 4 人") || !strings.Contains(got, "裴长河、韩铮、顾满仓、姚三刀") {
		t.Fatalf("must lock exact named cast:\n%s", got)
	}
	if !strings.Contains(got, "姚三刀(右中)站着，轻微迈步，双脚贴近地面") {
		t.Fatalf("walking pose must become a grounded keyframe:\n%s", got)
	}
	if !strings.Contains(got, "绝不能站到、坐到或跨上灶台") {
		t.Fatalf("characters must stay on walkable floor:\n%s", got)
	}
}

func TestWithPositioningSkeletonGuideShiftsLegend(t *testing.T) {
	src := "夜间包厢，韩铮(中中)坐着。\n\n参考图：图1为包厢空镜，图2为韩铮，图10为杀手"
	got := withPositioningSkeletonGuide(src)
	if !strings.Contains(got, "【骨架优先") {
		t.Fatalf("missing skeleton constraint:\n%s", got)
	}
	if !strings.Contains(got, "参考图：图1为火柴人站位骨架，图2为包厢空镜，图3为韩铮，图11为杀手") {
		t.Fatalf("legend should insert skeleton as 图1 and shift the rest:\n%s", got)
	}
	again := withPositioningSkeletonGuide(got)
	if strings.Count(again, "图1为火柴人站位骨架") != 1 {
		t.Fatalf("re-applying guide should not double-shift:\n%s", again)
	}
}

func TestPrioritizePositioningRefsKeepsSkeletonAndSceneForCrowd(t *testing.T) {
	prompt := "韩铮(左中)站着；裴长河(中中)站着；顾满仓(右前)站着；姚三刀(右中)站着。\n\n" +
		"参考图：图1为火柴人骨架，图2为御膳房，图3为韩铮，图4为裴长河，图5为顾满仓，图6为姚三刀"
	gotPrompt, gotRefs := prioritizePositioningReferenceInputs(prompt, []string{"skeleton", "scene", "han", "pei", "gu", "yao"})
	if len(gotRefs) != 2 || gotRefs[0] != "skeleton" || gotRefs[1] != "scene" {
		t.Fatalf("crowd final pass must keep only skeleton+scene, got %#v", gotRefs)
	}
	if strings.Contains(gotPrompt, "图3为") || strings.Contains(gotPrompt, "图6为") {
		t.Fatalf("prompt must not reference dropped images:\n%s", gotPrompt)
	}
}

func TestPositioningLooksLikeSkeletonGuide(t *testing.T) {
	if !positioningLooksLikeSkeletonGuide("参考图：图1为火柴人站位骨架，图2为包厢") {
		t.Fatal("legend with 图1 skeleton should be detected")
	}
	if positioningLooksLikeSkeletonGuide("参考图：图1为包厢空镜，图2为韩铮") {
		t.Fatal("plain character/scene legend is not a skeleton guide")
	}
}

func TestShotTitledSkeletonLegendDoesNotShift(t *testing.T) {
	src := "现代都市私人会所包厢，韩铮站立。\n\n参考图：图1为韩铮嘲讽杀手走错门，火柴人骨架，图2为私人会所包厢，图3为阿彪"
	if !positioningLooksLikeSkeletonGuide(src) {
		t.Fatal("图1 labeled …火柴人骨架 must count as skeleton")
	}
	got := withPositioningSkeletonGuide(src)
	if !strings.Contains(got, "【骨架优先") {
		t.Fatalf("missing skeleton priority:\n%s", got)
	}
	if !strings.Contains(got, "图1为韩铮嘲讽杀手走错门，火柴人骨架，图2为私人会所包厢") {
		t.Fatalf("already-skeleton 图1 must not be shifted:\n%s", got)
	}
	if strings.Contains(got, "图2为韩铮嘲讽杀手走错门") {
		t.Fatalf("shifted legend would mis-bind scene/character refs:\n%s", got)
	}
	full := withPositioningConstraints(got)
	if strings.Contains(full, "姿势以提示词为准") {
		t.Fatalf("skeleton pass must not prefer text poses over 图1:\n%s", full)
	}
	if !strings.Contains(full, "人数必须与图1骨架一致") {
		t.Fatalf("cast should lock to skeleton:\n%s", full)
	}
}
