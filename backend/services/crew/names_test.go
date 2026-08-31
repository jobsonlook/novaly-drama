package crew

import (
	"strings"
	"testing"
)

func TestAssetDisplayNameAndRoster(t *testing.T) {
	assets := []AssetItem{
		{Name: "韩铮", Type: "character", ResourceID: 1},
		{Name: "赤膊战损", Type: "character", ParentID: 1, ParentName: "韩铮", IsDerivative: true, ResourceID: 2},
		{Name: "更衣室", Type: "scene", ResourceID: 3},
	}
	if AssetDisplayName(assets[1]) != "韩铮 · 赤膊战损" {
		t.Fatalf("display=%q", AssetDisplayName(assets[1]))
	}
	if !AssetNameMatches(assets[1], "赤膊战损") || !AssetNameMatches(assets[1], "韩铮 · 赤膊战损") {
		t.Fatal("derivative should match state name and full name")
	}
	if AssetNameMatches(assets[1], "韩铮") {
		t.Fatal("derivative must not steal the parent name")
	}
	roster := formatAssetRoster(assets)
	for _, part := range []string{"character | 韩铮 · 赤膊战损", "不要同时再写「韩铮」", "script 里人名仍用「韩铮」"} {
		if !strings.Contains(roster, part) {
			t.Fatalf("roster missing %q:\n%s", part, roster)
		}
	}
}
