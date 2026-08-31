package crew

import (
	"testing"

	"novaly/backend/models"
)

func TestEnsureRecurringCharactersAddsMissing(t *testing.T) {
	script := "【0-3秒】韩铮(左前)与福公公(右前)对峙。\n【3-7秒】太监甲(左后)、太监乙(右后)垂手。\n【7-10秒】太监甲(中后)上前；太监乙说：「是。」"
	result := DirectorResult{
		Characters: []AssetItem{{Name: "韩铮", Type: "character"}, {Name: "福公公", Type: "character"}},
	}
	EnsureRecurringCharacters(&result, script)
	have := map[string]bool{}
	for _, c := range result.Characters {
		have[c.Name] = true
	}
	for _, want := range []string{"韩铮", "福公公", "太监甲", "太监乙"} {
		if !have[want] {
			t.Fatalf("missing %s in %#v", want, result.Characters)
		}
	}
}

func TestEnsureRecurringCharactersInAssetsSkipsOnce(t *testing.T) {
	script := "【0-3秒】路人甲(左后)经过。"
	assets := []AssetItem{{Name: "韩铮", Type: "character"}}
	got, added := EnsureRecurringCharactersInAssets(assets, script)
	if len(added) != 0 {
		t.Fatalf("single mention should not add asset, added=%v", added)
	}
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
}

func TestApplyRecurringCharacterDetails(t *testing.T) {
	assets := []AssetItem{{Name: "太监甲", Type: "character"}}
	details := map[string]AssetItem{
		"太监甲": {Description: "中年太监，瘦削", VoicePrompt: "中年男性，声线尖细"},
	}
	ApplyRecurringCharacterDetails(assets, details)
	if assets[0].Description == "" || assets[0].VoicePrompt == "" {
		t.Fatalf("details not applied: %#v", assets[0])
	}
	_ = models.Project{}
}
