package services

import (
	"strings"
	"testing"
)

func TestBuildCharacterPromptHonors3DProjectStyle(t *testing.T) {
	prompt := buildCharacterPrompt(
		"小禾",
		"国风3D动画角色。\n画面质感：超写实真人摄影棚拍摄，8K超高清，不是CG，不是3D渲染",
		"国风3D，高精度建模与PBR材质",
	)
	for _, want := range []string{"画面质感（最高优先级）", "国风3D", "PBR材质", "禁止真人照片"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "超写实真人摄影棚拍摄") || strings.Contains(prompt, "不是3D渲染") {
		t.Fatalf("3D prompt still contains conflicting photoreal directive: %s", prompt)
	}
}

func TestBuildCharacterPromptKeepsPhotorealDefault(t *testing.T) {
	prompt := buildCharacterPrompt("角色", "古装成年男性", "")
	if !strings.Contains(prompt, "超写实真人摄影棚拍摄") {
		t.Fatalf("photoreal project should keep live-action rendering direction: %s", prompt)
	}
}
