package services

import "testing"

func TestSceneGridCellNameOmitsCandidateTag(t *testing.T) {
	got := SceneGridCellName("私人会所包厢 · 9宫格 · 候选1", 7)
	if got != "私人会所包厢·俯视全景" {
		t.Fatalf("got %q", got)
	}
	got = SceneGridCellName("私人会所包厢 · 9宫格", 1)
	if got != "私人会所包厢·正面全景" {
		t.Fatalf("plain grid got %q", got)
	}
}

func TestSceneGridMatchesPlate(t *testing.T) {
	cases := []struct {
		grid, plate string
		want        bool
	}{
		{"私人会所包厢 · 9宫格", "私人会所包厢", true},
		{"私人会所包厢 · 9宫格", "包厢站位图", true},
		{"私人会所包厢 · 9宫格", "包厢站位图-反打", true},
		{"更衣室 · 9宫格", "包厢站位图", false},
		{"私人会所包厢 · 9宫格", "拳台", false},
	}
	for _, tc := range cases {
		if got := SceneGridMatchesPlate(tc.grid, tc.plate); got != tc.want {
			t.Fatalf("SceneGridMatchesPlate(%q, %q)=%v want %v", tc.grid, tc.plate, got, tc.want)
		}
	}
}
