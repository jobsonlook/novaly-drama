package services

import (
	"strings"
	"testing"

	"novaly/backend/models"
)

func TestVideoPromptKeepsInscriptionSeparateFromSpeech(t *testing.T) {
	for _, inscription := range []string{
		"剑身金纹由上至下聚成恭迎师叔四个古字",
		"剑身金纹聚成「恭迎师叔」四个古字",
		"剑身显示：「恭迎师叔」",
	} {
		t.Run(inscription, func(t *testing.T) {
			got := BuildVideoPrompt(VideoInput{
				Script:   "【0-3秒】镜头：特写固定，" + inscription + "；音效：剑鸣\n【3-10秒】镜头：近景，谢无尘说：「认错人了？」",
				Duration: 10,
				Refs:     []VideoRef{{Kind: "scene", Resource: models.Resource{ID: 1, Type: "scene", Name: "祖师殿", ImagePath: "scene.png"}}},
			})
			if !strings.Contains(got, "恭迎师叔") || strings.Contains(got, "说 {恭迎师叔}") {
				t.Fatalf("inscription must remain visual, not spoken: %s", got)
			}
			if !strings.Contains(got, "说 {认错人了？}") {
				t.Fatalf("actual speech must remain spoken: %s", got)
			}
			for _, forbidden := range []string{"纯净无字", "禁止在任何表面生成文字", "禁止任何可读文字"} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("conflicting blanket ban %q: %s", forbidden, got)
				}
			}
			if !strings.Contains(got, "默认不生成任何文字") || !strings.Contains(got, "禁止字幕") {
				t.Fatalf("default text and subtitle restrictions must remain: %s", got)
			}
		})
	}
}

func TestVideoInscriptionDoesNotCreateSpeech(t *testing.T) {
	got := BuildVideoPrompt(VideoInput{Script: "【0-10秒】镜头：剑身聚成「恭迎师叔」四个古字", Duration: 10})
	if hasSeedanceSpeech(got) || !strings.Contains(got, VideoNoSpeechConstraint) {
		t.Fatalf("inscription-only shot must remain silent: %s", got)
	}
}
