package crew

import "testing"

func TestInheritParentVoices(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", VoicePrompt: "30岁左右男性，声线低沉沙哑带磁性，不是少年音"},
		{Name: "韩铮 · 赤膊战损", Type: "character", ParentName: "韩铮", ParentID: 1, IsDerivative: true},
		{Name: "客厅", Type: "scene"},
	}
	inheritParentVoices(assets)
	if assets[1].VoicePrompt != assets[0].VoicePrompt {
		t.Fatalf("derivative should inherit parent voice, got %q", assets[1].VoicePrompt)
	}
	if assets[2].VoicePrompt != "" {
		t.Fatalf("scene should stay empty, got %q", assets[2].VoicePrompt)
	}
}
