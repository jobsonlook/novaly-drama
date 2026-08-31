package crew

import (
	"strings"
	"testing"

	"novaly/backend/models"
)

func TestDirectorCraftRulesUsesTenSecondsNotFifteen(t *testing.T) {
	if !strings.Contains(DirectorCraftRules, "10 秒") {
		t.Fatal("director craft must lock 10s clips")
	}
	if strings.Contains(DirectorCraftRules, "锁定 15") || strings.Contains(DirectorCraftRules, "总时长严格锁定 15") {
		t.Fatal("director craft must not keep the Xiaohongshu 15s lock")
	}
	if !strings.Contains(DirectorCraftRules, "剧本") {
		t.Fatal("must say this is 剧本转分镜")
	}
	got := storyboardPrompt("韩铮说：「走。」", "", models.Project{}, nil, 4, 3, 5, 0, StoryboardPaceFine, nil)
	if !strings.Contains(got, "不是 15 秒") {
		t.Fatal("storyboard prompt should warn against 15s templates")
	}
	if !strings.Contains(got, "人名(格子)朝向") && !strings.Contains(got, "人名(左前") {
		t.Fatal("storyboard prompt must keep blocking rules")
	}
	if !strings.Contains(got, "导演级") {
		t.Fatal("storyboard prompt should include director craft block")
	}
}
