package crew

import "testing"

func TestSelectCharacterAssetForShotPrefersAncientDerivative(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1, Description: "现代地下拳手，深色运动服"},
		{Name: "古装", Type: "character", ResourceID: 2, ParentID: 1, ParentName: "韩铮", IsDerivative: true, Description: "古代宫廷官服，束发长袍"},
		{Name: "赤膊赛后", Type: "character", ResourceID: 3, ParentID: 1, ParentName: "韩铮", IsDerivative: true, Description: "现代拳赛后赤膊"},
	}
	got, ok := SelectCharacterAssetForShot(assets, "韩铮", StoryboardShot{SceneName: "御膳房侧库房", Script: "韩铮把腰牌塞进古代厨役服的衣缝。"})
	if !ok || got.ResourceID != 2 {
		t.Fatalf("expected ancient derivative 2, got %#v, ok=%v", got, ok)
	}
}

func TestSelectCharacterAssetForShotKeepsModernBaseLook(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1, Description: "现代地下拳手，深色运动服"},
		{Name: "古装", Type: "character", ResourceID: 2, ParentID: 1, ParentName: "韩铮", IsDerivative: true, Description: "古代宫廷官服"},
	}
	got, ok := SelectCharacterAssetForShot(assets, "韩铮", StoryboardShot{SceneName: "现代地下拳场", Script: "韩铮穿着运动服走下擂台。"})
	if !ok || got.ResourceID != 1 {
		t.Fatalf("expected modern base 1, got %#v, ok=%v", got, ok)
	}
}

func TestSelectCharacterAssetForShotHonorsExplicitState(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1, Description: "现代地下拳手"},
		{Name: "古装", Type: "character", ResourceID: 2, ParentID: 1, ParentName: "韩铮", IsDerivative: true, Description: "古代宫廷官服"},
		{Name: "赤膊战损", Type: "character", ResourceID: 3, ParentID: 1, ParentName: "韩铮", IsDerivative: true},
	}
	got, ok := SelectCharacterAssetForShot(assets, "韩铮", StoryboardShot{SceneName: "现代拳馆", Script: "韩铮赤膊站在擂台中央，肩头带着战损擦伤。"})
	if !ok || got.ResourceID != 3 {
		t.Fatalf("expected explicit state derivative 3, got %#v, ok=%v", got, ok)
	}
}
