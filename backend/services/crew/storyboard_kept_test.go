package crew

import (
	"strings"
	"testing"
)

func TestFormatKeptShotsBlockUsesScriptNotOldTemplate(t *testing.T) {
	kept := []KeptShot{
		{
			Label:  "镜1",
			Script: "【0-3秒】镜头：中景\n角色说：「你好啊阿彪」\n【7-10秒】镜头：韩铮(左前)面向右侧收束",
		},
		{
			Label:  "镜2",
			Script: "【0-3秒】镜头：近景\n角色说：「我们现在就走吧」\n【7-10秒】镜头：阿彪(右中)转身离开",
		},
	}
	got := formatKeptShotsBlock(kept)
	if !strings.Contains(got, "按定稿剧本重拆") {
		t.Fatalf("expected script-first instruction, got:\n%s", got)
	}
	if !strings.Contains(got, "禁止把旧分镜文案当模板") {
		t.Fatalf("expected no-template rule, got:\n%s", got)
	}
	if !strings.Contains(got, "「你好啊阿彪」") || !strings.Contains(got, "「我们现在就走吧」") {
		t.Fatalf("expected covered quotes, got:\n%s", got)
	}
	// Full prior scripts must not be dumped as templates.
	if strings.Contains(got, "—— 已锁定") {
		t.Fatalf("should not dump full locked shot scripts as templates:\n%s", got)
	}
	if !strings.Contains(got, "仅供站位衔接") {
		t.Fatalf("expected last-beat continuity hint, got:\n%s", got)
	}
	if !strings.Contains(got, "阿彪(右中)转身离开") {
		t.Fatalf("expected last beat snippet from final shot, got:\n%s", got)
	}
}

func TestContinuationScriptResumesPartialDialogueTail(t *testing.T) {
	script := `**场景：** 内景 · 御膳房大灶间 · 日
**姚三刀**（阴阳）：「裴师傅，生辰宴主菜交给我靠谱人。韩小灶——去择菜叶，别脏了贵人的眼。」
△ 韩铮放下菜刀。
**裴长河**：「小灶，听安排。」`
	kept := []KeptShot{{
		Label:  "姚三刀发话",
		Script: "【0-3秒】姚三刀说：「裴师傅，生辰宴主菜交给我靠谱人。」",
	}}
	got := continuationScriptAfterKept(script, kept)
	if strings.Contains(got, "生辰宴主菜交给我靠谱人") {
		t.Fatalf("covered prefix must not be repeated:\n%s", got)
	}
	if !strings.Contains(got, "姚三刀") || !strings.Contains(got, "韩小灶——去择菜叶，别脏了贵人的眼") {
		t.Fatalf("partial source line must resume with its tail and speaker:\n%s", got)
	}
	if !strings.Contains(got, "裴长河") {
		t.Fatalf("following plot must remain:\n%s", got)
	}
}

func TestPartialCoveredDialogueIsNotTreatedAsWholeLine(t *testing.T) {
	line := "裴师傅，生辰宴主菜交给我靠谱人。韩小灶——去择菜叶，别脏了贵人的眼。"
	covered := []string{"【0-3秒】姚三刀说：「裴师傅，生辰宴主菜交给我靠谱人。」"}
	if dialogueCoveredByScripts(line, covered) {
		t.Fatal("a long prefix must not mark the complete screenplay line covered")
	}
}
