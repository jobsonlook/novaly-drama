package crew

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractJSONObjectFromFence(t *testing.T) {
	raw := "```json\n{\"script\":\"hello\"}\n```"
	got, err := extractJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"script":"hello"}` {
		t.Fatalf("got %q", got)
	}
}

func TestExtractJSONObjectRepairsLLMJunk(t *testing.T) {
	raw := "```json\n{\"score\":\"D\",\"summary\":\"需修正\",\"issues\":[{\"severity\":\"high\",\"code\":\"R2\",\"shotId\":483,\"shotIndex\":2,\"message\":\"阿彬先说完整句 \"就能回皮\" 但顺序颠倒\",\"suggestion\":\"按剧本顺序拆拍\",}\n"
	got, err := extractJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	var report QCReport
	if err := json.Unmarshal([]byte(got), &report); err != nil {
		t.Fatalf("unmarshal %v\n%s", err, got)
	}
	if report.Score != "D" || len(report.Issues) != 1 {
		t.Fatalf("report=%+v json=%s", report, got)
	}
	if !strings.Contains(report.Issues[0].Message, "就能回皮") {
		t.Fatalf("message=%q", report.Issues[0].Message)
	}
}

func TestExtractJSONObjectTruncatedCloses(t *testing.T) {
	raw := `{"score":"C","summary":"复检","issues":[{"severity":"high","code":"R1","shotId":483,"shotIndex":1,"message":"日常图和换装图同镜"`
	got, err := extractJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	var report QCReport
	if err := json.Unmarshal([]byte(got), &report); err != nil {
		t.Fatalf("unmarshal %v\n%s", err, got)
	}
	if report.Score != "C" || len(report.Issues) != 1 || report.Issues[0].Code != "R1" {
		t.Fatalf("report=%+v json=%s", report, got)
	}
}

func TestExtractJSONObjectTruncatedTrailingBackslash(t *testing.T) {
	raw := `{"shots":[{"label":"韩铮躲入侧库","duration":10,"script":"【0-3秒】镜头：中景；音效：低沉鼓点、衣料摩擦；` + `\`
	got, err := extractJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	var result StoryboardResult
	if err := json.Unmarshal([]byte(got), &result); err != nil {
		t.Fatalf("unmarshal %v\n%s", err, got)
	}
	if len(result.Shots) != 1 {
		t.Fatalf("expected 1 salvaged shot, got %d", len(result.Shots))
	}
}

func TestExtractJSONObjectPrefersShotsObject(t *testing.T) {
	raw := `{"note":"ignore"}{"shots":[{"label":"x","duration":10,"script":"a"}]}`
	got, err := extractJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	var result StoryboardResult
	if err := json.Unmarshal([]byte(got), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Shots) != 1 || result.Shots[0].Label != "x" {
		t.Fatalf("got wrong object: %s", got)
	}
}

func TestParsePromptOutputStripsFenceAndJSON(t *testing.T) {
	got := parsePromptOutput("```\n提示词：女性角色四视图设定图，赛璐璐上色\n```")
	if !strings.Contains(got, "四视图") {
		t.Fatalf("got %q", got)
	}
	got = parsePromptOutput(`<think>x</think>{"prompt":"角色四视图设定图，全身立像"}`)
	if got != "角色四视图设定图，全身立像" {
		t.Fatalf("got %q", got)
	}
}

func TestArtSystemPromptCoversManuals(t *testing.T) {
	for _, id := range []string{"2d-90s-anime", "2d-guofeng", "2d-flat", "3d-cute", "3d-guofeng", "3d-clay", "real-ancient", "real-urban"} {
		style := visualStyles[id]
		if style.ID == "" {
			t.Fatalf("missing style %s", id)
		}
		sys := artSystemPrompt("character", style, false)
		if !strings.Contains(sys, "四视图") || !strings.Contains(sys, "仅输出提示词正文") || !strings.Contains(sys, "奖牌") {
			t.Fatalf("%s character handbook incomplete", id)
		}
		der := artSystemPrompt("character", style, true)
		if !strings.Contains(der, "面容") || !strings.Contains(der, "L1") || !strings.Contains(der, "与父图一致") {
			t.Fatalf("%s character derivative handbook incomplete", id)
		}
		if artSystemPrompt("scene", style, false) == "" || artSystemPrompt("prop", style, false) == "" {
			t.Fatalf("%s scene/prop empty", id)
		}
	}
}

func TestMergeAssetsDedupes(t *testing.T) {
	out := mergeAssets(
		[]AssetItem{{Name: "林晓", Type: "character"}, {Name: "林晓", Type: "character"}},
		[]AssetItem{{Name: "客厅", Type: "scene"}},
		nil,
	)
	if len(out) != 2 {
		t.Fatalf("len=%d", len(out))
	}
}
