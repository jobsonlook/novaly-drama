package controllers

import (
	"testing"

	"novaly/backend/models"
)

func TestExtractStoryboardTTSProject(t *testing.T) {
	source := models.Project{
		ID:    7,
		Title: "麦城",
		Episodes: []models.Episode{{
			Number: 1,
			Shots: []models.Shot{
				{
					ID:        11,
					SortOrder: 1,
					Script:    "【0-3秒】镜头：关羽握刀喘息；关羽（虚弱喘息）：“援军……为何还不到？”\n【3-6秒】旁白：大雪封住了最后的退路。",
				},
				{
					ID:        12,
					SortOrder: 2,
					Script:    "【0-5秒】张辽：放下兵器！；音效：战马嘶鸣",
				},
			},
		}},
	}

	project := extractStoryboardTTSProject(source)
	if project.SourceProjectID != 7 {
		t.Fatalf("source project id = %d", project.SourceProjectID)
	}
	if len(project.Lines) != 3 {
		t.Fatalf("lines = %d, want 3: %#v", len(project.Lines), project.Lines)
	}
	if got := project.Lines[0]; got.Speaker != "关羽" || got.Time != "0-3秒" || got.SpeechRate >= 0 || got.Pitch >= 0 {
		t.Fatalf("unexpected first line: %#v", got)
	}
	if got := project.Lines[1]; got.Speaker != "旁白" || got.Type != "旁白" {
		t.Fatalf("unexpected narration: %#v", got)
	}
	if got := project.Lines[2]; got.GlobalShot != 2 || got.LoudnessRate <= 0 {
		t.Fatalf("unexpected second-shot line: %#v", got)
	}
	if len(project.Characters) != 3 {
		t.Fatalf("characters = %d, want 3", len(project.Characters))
	}
}

func TestMergeExtractedTTSProjectPreservesVoiceAndMatchingAudio(t *testing.T) {
	current := &ttsProject{
		Characters: []ttsCharacter{{ID: "new", Name: "关羽", DefaultSpeed: 1}},
		Lines:      []ttsLine{{ID: "shot_1_line_01", Text: "且慢"}},
	}
	old := &ttsProject{
		Characters: []ttsCharacter{{ID: "old", Name: "关羽", VoiceType: "voice-a", DefaultSpeed: 1.2}},
		Lines: []ttsLine{{
			ID: "shot_1_line_01", Text: "且慢", VoiceType: "voice-b",
			Filename: "old.mp3", AudioURL: "/audio/old.mp3", AudioReady: true,
		}},
	}

	mergeExtractedTTSProject(current, old)
	if current.Characters[0].VoiceType != "voice-a" || current.Characters[0].DefaultSpeed != 1.2 {
		t.Fatalf("character settings not preserved: %#v", current.Characters[0])
	}
	if !current.Lines[0].AudioReady || current.Lines[0].VoiceType != "voice-b" {
		t.Fatalf("line settings not preserved: %#v", current.Lines[0])
	}
}

func TestExtractStoryboardScriptFormat(t *testing.T) {
	source := models.Project{
		ID:    8,
		Title: "灵兽",
		Episodes: []models.Episode{{
			Number: 1,
			Shots: []models.Shot{{
				ID:        21,
				SortOrder: 1,
				Script: `【视频提示词】
  ◆ 0-3秒 | 运镜：（远景/开场）
      镜头：近景，小七双眼血红
      台词：（清晰可辨，决绝）：“死，也要拉你们垫背！”
      内心戏：（无）
  ◆ 3-6秒 | 运镜：（特写）
      镜头：系统界面亮起
      台词：（系统电子音）：宿主绑定成功。
角色：小七（狠辣果决）、杀手（惊恐）。`,
			}},
		}},
	}

	project := extractStoryboardTTSProject(source)
	if len(project.Lines) != 2 {
		t.Fatalf("lines = %d, want 2: %#v", len(project.Lines), project.Lines)
	}
	if got := project.Lines[0]; got.Speaker != "小七" || got.Time != "0-3秒" || got.Text != "死，也要拉你们垫背！" {
		t.Fatalf("unexpected dialogue: %#v", got)
	}
	if got := project.Lines[1]; got.Speaker != "系统" || got.Time != "3-6秒" {
		t.Fatalf("unexpected system line: %#v", got)
	}
}
