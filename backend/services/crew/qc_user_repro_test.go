package crew

import (
	"strings"
	"testing"
)

func TestQCUserBatchDoesNotGutShot6(t *testing.T) {
	shot6Script := "【0-4秒】镜头：中景固定，裴长河盯着韩铮看两秒，叹气摇头；音效：低沉鼓点；韩铮说：「命硬，阎王不收。」\n" +
		"【4-10秒】镜头：近景固定，韩铮垂眸把焦黑腰牌塞进衣缝；音效：衣料摩擦"
	shots := make([]ShotContext, 22)
	for i := range shots {
		shots[i] = ShotContext{
			ID: uint(i + 1), Index: i + 1, Duration: 10,
			Script: "【0-3秒】镜头：过渡；音效：紧张鼓点",
			Refs:   []ShotRefInfo{{Kind: "scene", Name: "御膳房侧库房", ResourceID: 10}},
		}
	}
	shots[5] = ShotContext{
		ID: 6, Index: 6, Duration: 10, Script: shot6Script,
		Label: "韩铮暗下决心藏",
		Refs: []ShotRefInfo{
			{Kind: "scene", Name: "御膳房侧库房", ResourceID: 10},
			{Kind: "character", Name: "韩铮", ResourceID: 1},
			{Kind: "character", Name: "裴长河", ResourceID: 2},
		},
	}
	shots[13] = ShotContext{
		ID: 14, Index: 14, Duration: 10,
		Script: "【0-3秒】镜头：近景；裴长河说：「先去洗菜。今日小郡王生辰备料，错一份，」\n【3-7秒】镜头：中景。\n【7-10秒】镜头：反应。",
	}
	issues := []QCIssue{
		{Code: "R1", ShotID: 1, ShotIndex: 1, Message: "文案出现道具「焦黑腰牌」，但本镜 refs 未绑定"},
		{Code: "R1", ShotID: 6, ShotIndex: 6, Message: "文案出现「姚三刀」，但本镜 refs 未绑定该角色"},
		{Code: "R1", ShotID: 7, ShotIndex: 7, Message: "文案出现道具「焦黑腰牌」，但本镜 refs 未绑定"},
		{Code: "R1", ShotID: 14, ShotIndex: 14, Message: "文案出现「裴长河」，但本镜 refs 未绑定该角色"},
		{Code: "R1", ShotID: 15, ShotIndex: 15, Message: "文案出现「姚三刀」，但本镜 refs 未绑定该角色"},
		{Code: "R1", ShotID: 16, ShotIndex: 16, Message: "本镜没有绑定场景参考图"},
		{Code: "R1", ShotID: 17, ShotIndex: 17, Message: "本镜没有绑定场景参考图"},
		{Code: "R1", ShotID: 18, ShotIndex: 18, Message: "本镜没有绑定场景参考图"},
		{Code: "R2", ShotID: 14, ShotIndex: 14, Message: "这段约 3 秒，台词 17 字，按 4 字/秒会说不完（上限 12 字）"},
		{Code: "R5", ShotID: 11, ShotIndex: 11, Message: "上一镜配乐是「紧张鼓点」，这一镜音效里断了"},
		{Code: "R5", ShotID: 12, ShotIndex: 12, Message: "上一镜配乐是「紧张鼓点」，这一镜音效里断了"},
		{Code: "R5", ShotID: 13, ShotIndex: 13, Message: "上一镜配乐是「紧张鼓点」，这一镜音效里断了"},
		{Code: "R5", ShotID: 14, ShotIndex: 14, Message: "上一镜配乐是「紧张鼓点」，这一镜音效里断了"},
		{Code: "R5", ShotID: 15, ShotIndex: 15, Message: "上一镜配乐是「紧张鼓点」，这一镜音效里断了"},
		{Code: "R5", ShotID: 16, ShotIndex: 16, Message: "上一镜配乐是「紧张鼓点」，这一镜音效里断了"},
		{Code: "R5", ShotID: 17, ShotIndex: 17, Message: "上一镜配乐是「紧张鼓点」，这一镜音效里断了"},
	}
	assets := []AssetItem{
		{Name: "焦黑腰牌", Type: "prop", ResourceID: 50},
		{Name: "姚三刀", Type: "character", ResourceID: 99},
		{Name: "裴长河", Type: "character", ResourceID: 2},
		{Name: "韩铮", Type: "character", ResourceID: 1},
		{Name: "御膳房侧库房", Type: "scene", ResourceID: 10},
	}
	epScript := "裴长河：先去洗菜。今日小郡王生辰备料，错一份，咱们全膳房喝西北风。记住——少惹姚三刀。\n韩铮：命硬，阎王不收。"
	patched := ApplyQCFixes(shots, assets, issues)
	patched = ApplyQCFixes(patched, assets, issues)
	patched = PrepareShotsAfterFix(patched, assets, epScript, nil, issues)
	var got ShotContext
	for _, s := range patched {
		if s.Index == 6 {
			got = s
			break
		}
	}
	t.Logf("shot6 after fix:\n%s", got.Script)
	if !strings.Contains(got.Script, "腰牌") && !strings.Contains(got.Script, "垂眸") && !strings.Contains(got.Script, "近景") {
		t.Fatalf("shot 6 second beat gutted:\n%s", got.Script)
	}
	if strings.Contains(got.Script, "镜头：；") || strings.Contains(got.Script, "镜头：；") {
		t.Fatalf("empty lens field:\n%s", got.Script)
	}
}

func TestPrependBGMDoesNotGutSecondBeat(t *testing.T) {
	script := "【0-4秒】镜头：中景；音效：低沉鼓点；韩铮说：「命硬，阎王不收。」\n【4-10秒】镜头：近景固定，韩铮垂眸把焦黑腰牌塞进衣缝；音效：衣料摩擦"
	got := prependBGM(script, "紧张鼓点")
	if !strings.Contains(got, "腰牌") || !strings.Contains(got, "近景") {
		t.Fatalf("prependBGM gutted second beat: %s", got)
	}
}

func TestEnsureLensLinesDoesNotGutBeat(t *testing.T) {
	script := "【0-4秒】镜头：中景；音效：低沉鼓点\n【4-10秒】近景固定，韩铮垂眸把焦黑腰牌塞进衣缝；音效：衣料摩擦"
	got := ensureLensLines(script)
	if !strings.Contains(got, "腰牌") {
		t.Fatalf("ensureLensLines gutted: %s", got)
	}
}

func TestDetectLoopAloneDoesNotGutShot6(t *testing.T) {
	shot6Script := "【0-4秒】镜头：中景固定，裴长河盯着韩铮看两秒，叹气摇头；音效：低沉鼓点；韩铮说：「命硬，阎王不收。」\n【4-10秒】镜头：近景固定，韩铮垂眸把焦黑腰牌塞进衣缝；音效：衣料摩擦"
	shots := []ShotContext{{ID: 6, Index: 6, Duration: 10, Script: shot6Script}}
	for pass := 0; pass < 4; pass++ {
		issues := detectDeterministicQC(shots, nil, "")
		if len(issues) == 0 {
			break
		}
		next := ApplyQCFixes(shots, nil, issues)
		if !shotContextsChanged(shots, next) {
			break
		}
		shots = next
	}
	if !strings.Contains(shots[0].Script, "腰牌") {
		t.Fatalf("detect loop gutted shot6: %s", shots[0].Script)
	}
}
