package crew

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStoryboardPipelinePreservesMixedNewlineLongDialogue(t *testing.T) {
	source := `**韩铮**（低声）："韩家的孩子……先活下去。福公你话说一半就跑，差评。"
**韩铮**（皱眉）："这酱不对。像……陈年的坏。"`
	model := StoryboardResult{Shots: []StoryboardShot{
		{
			Label: "韩铮躲进侧库", Duration: 10, SceneName: "侧库", CharacterNames: []string{"韩铮"},
			Script: "【0-3秒】镜头：韩铮喘气；音效：鼓点\\n\n【3-7秒】镜头：腰牌特写；音效：衣料声\\n\n【7-10秒】镜头：韩铮垂眸；韩铮说：「韩家的孩子……先活下去。」",
		},
		{
			Label: "韩铮察觉酱味", Duration: 10, SceneName: "侧库", CharacterNames: []string{"韩铮"},
			Script: "【0-3秒】镜头：韩铮擦脸\\n【3-7秒】镜头：酱罐特写\\n【7-10秒】镜头：韩铮皱眉；韩铮说：「这酱不对。像……陈年的坏。」",
		},
	}}
	raw, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parseStoryboardResult(string(raw), source)
	if err != nil {
		t.Fatal(err)
	}
	contexts := make([]ShotContext, len(result.Shots))
	for i, shot := range result.Shots {
		contexts[i] = ShotContext{ID: uint(i + 1), Index: i + 1, Label: shot.Label, Duration: shot.Duration, Script: shot.Script}
	}
	contexts = PrepareShotsForQC(contexts, nil, source)
	if !shotContextsCoverAllDialogue(contexts, source, nil) {
		t.Fatalf("full pipeline lost locked dialogue: %#v", contexts)
	}
	joined := ""
	for _, shot := range contexts {
		joined += shot.Script + "\n"
		if strings.Contains(shot.Script, `\n`) {
			t.Fatalf("literal newline survived: %q", shot.Script)
		}
		if strings.Contains(shot.Label, "对白") && len(quotesInScript(shot.Script)) == 0 {
			t.Fatalf("empty dialogue shot survived: %#v", shot)
		}
	}
	for _, want := range []string{"福公你话说一半就跑", "差评。", "这酱不对"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q after full pipeline:\n%s", want, joined)
		}
	}
}

func TestStoryboardPipelineDoesNotSplitYaosReliableLineAtVerbObject(t *testing.T) {
	source := `**姚三刀**（路过，阴阳）："裴师傅，生辰宴主菜交给我靠谱人。韩小灶——去择菜叶，别脏了贵人的眼。"`
	first, tail := splitQuoteForBeat("裴师傅，生辰宴主菜交给我靠谱人。韩小灶——去择菜叶，别脏了贵人的眼。", 3)
	if first != "裴师傅，" || !strings.HasPrefix(tail, "生辰宴") {
		t.Fatalf("3-second split must use the first punctuation, got %q / %q", first, tail)
	}
	model := StoryboardResult{Shots: []StoryboardShot{{
		Label: "姚三刀发话", Duration: 10, SceneName: "御膳房大灶间", CharacterNames: []string{"姚三刀", "裴长河"},
		Script: "【0-3秒】镜头：姚三刀开口；音效：低沉鼓点\n" +
			"【3-7秒】镜头：裴长河反应；音效：锅铲轻碰\n" +
			"【7-10秒】镜头：姚三刀冷笑；音效：灶火噼啪",
	}}}
	raw, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parseStoryboardResult(string(raw), source)
	if err != nil {
		t.Fatal(err)
	}
	var scripts []string
	for _, shot := range result.Shots {
		scripts = append(scripts, shot.Script)
	}
	joined := strings.Join(scripts, "\n")
	for _, bad := range []string{"交给」\n", "「我靠谱人", "我靠」\n", "「谱人"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("dialogue split at unsafe boundary %q:\n%s", bad, joined)
		}
	}
	if !storyboardsCoverAllDialogue(result.Shots, source, nil) {
		t.Fatalf("dialogue must remain complete:\n%s", joined)
	}
}
