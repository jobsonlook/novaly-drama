package services

import (
	"strings"
	"testing"
)

func TestCleanOptimizedShotScriptRemovesAnglesArtifacts(t *testing.T) {
	cases := []string{
		`{"script":"【0-3秒】镜头：推进\n【3-6秒】镜头：特写","angles":[{"id":1}]}`,
		`{'script':'【0-3秒】镜头：推进
【3-6秒】镜头：特写','angles':[{'id':1,'label':'正面近景'}]}`,
		`【0-3秒】镜头：推进
【3-6秒】镜头：特写','angles':[{'id':1}]}`,
	}
	for _, input := range cases {
		got := CleanOptimizedShotScript(input)
		if strings.Contains(got, "angles") || strings.Contains(got, `"id"`) || strings.Contains(got, "'id'") {
			t.Fatalf("JSON artifact remains in %q", got)
		}
		if !strings.Contains(got, "【0-3秒】") || !strings.Contains(got, "【3-6秒】") {
			t.Fatalf("script content lost: %q", got)
		}
	}
}

func TestCleanOptimizeShotRepetitionRemovesSharedStyleSuffix(t *testing.T) {
	style := "虚幻引擎UE5建模风格，高精度建模渲染，电影质感，35mm电影胶片颗粒，4K超高细节纹理，纯真人影视实拍效果"
	input := "【0-3秒】镜头：侧面近景缓推，人物起身，" + style + "；音效：碎石声\n" +
		"【3-6秒】镜头：正面特写，人物抬头，" + style + "；音效：风声"
	got := cleanOptimizeShotRepetition(input, style)
	if strings.Contains(got, "UE5") || strings.Contains(got, "35mm") || strings.Contains(got, "4K") {
		t.Fatalf("global style remains in timing beats: %q", got)
	}
	if !strings.Contains(got, "人物起身") || !strings.Contains(got, "人物抬头") {
		t.Fatalf("unique beat content lost: %q", got)
	}
}

func TestCleanOptimizeShotRepetitionRemovesSingleBeatStyleSubset(t *testing.T) {
	style := "不需要字幕，虚幻引擎 UE5 建模风格，高精度建模渲染，电影质感，光影写实，色彩自然。超真实古风仙侠电影质感，35mm 电影胶片颗粒，4K 超高细节纹理，纯真人影视实拍效果"
	input := "【0-3秒】镜头：复眼微距特写，小七触角震颤；音效：轻嗡\n" +
		"【8-10秒】镜头：复眼超特写定镜，小七悬停不退，虚幻引擎 UE5 建模风格，高精度建模渲染，电影质感，光影写实，色彩自然，超真实古风仙侠电影质感，35mm 电影胶片颗粒，4K 超高细节纹理，纯真人影视实拍效果；音效：振翅低鸣"
	got := cleanOptimizeShotRepetition(input, style)
	for _, unwanted := range []string{"UE5", "电影质感", "35mm", "4K", "真人影视实拍"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("style clause %q remains: %q", unwanted, got)
		}
	}
	if !strings.Contains(got, "小七悬停不退") || !strings.Contains(got, "振翅低鸣") {
		t.Fatalf("unique beat content lost: %q", got)
	}
}
