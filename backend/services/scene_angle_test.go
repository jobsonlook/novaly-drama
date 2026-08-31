package services

import (
	"reflect"
	"testing"
)

func TestInferWantedSceneAnglesFromBeats(t *testing.T) {
	script := "【0-3秒】镜头：中景固定，韩铮对峙。\n【3-7秒】镜头：特写，小鹿眼神。\n【7-10秒】镜头：俯视全景，包厢全貌。"
	got := InferWantedSceneAngles(script)
	want := []string{"正面近景", "侧面全景", "俯视近景"}
	// 特写 adds 正面近景 (already), 俯视 adds 俯视近景; 中景 adds 正面近景+侧面全景
	if !containsAll(got, []string{"正面近景", "侧面全景"}) {
		t.Fatalf("mid+close should include 正面近景/侧面全景, got %#v", got)
	}
	if !containsAll(got, []string{"俯视近景"}) && !containsAll(got, []string{"斜向高位总览"}) {
		t.Fatalf("俯视 beat should pick a high angle, got %#v (sample want %#v)", got, want)
	}
	if len(got) > 3 {
		t.Fatalf("cap at 3, got %#v", got)
	}
}

func TestInferWantedSceneAnglesExplicitMarker(t *testing.T) {
	got := InferWantedSceneAngles("【0-3秒】镜头：推进；机位：侧面近景")
	if !reflect.DeepEqual(got, []string{"侧面近景"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestPickSceneGridCellsForScript(t *testing.T) {
	cells := []SceneGridCellCandidate{
		{ID: 1, Name: "私人会所包厢·正面全景", GridCell: 1},
		{ID: 2, Name: "私人会所包厢·正面近景", GridCell: 2},
		{ID: 3, Name: "私人会所包厢·侧面全景", GridCell: 3},
		{ID: 8, Name: "私人会所包厢·俯视近景", GridCell: 8},
	}
	got := PickSceneGridCellsForScript(cells, "【0-3秒】镜头：中景。\n【7-10秒】镜头：俯视包厢。", 3)
	ids := map[uint]bool{}
	for _, p := range got {
		ids[p.ID] = true
	}
	if !ids[2] && !ids[3] {
		t.Fatalf("中景 should pick near/side, got %#v", got)
	}
	if !ids[8] {
		t.Fatalf("俯视 should pick cell 8, got %#v", got)
	}
}

func TestPickSkeletonSceneRefPrefersMatchingGridCell(t *testing.T) {
	cands := []SkeletonSceneCandidate{
		{ID: 10, Type: "scene", GenType: "scene", Name: "私人会所包厢空镜"},
		{ID: 2, Type: "scene", GenType: "scene_grid_cell", Name: "私人会所包厢·正面近景", GridCell: 2},
		{ID: 8, Type: "scene", GenType: "scene_grid_cell", Name: "私人会所包厢·俯视近景", GridCell: 8},
		{ID: 99, Type: "scene", GenType: "scene_grid", Name: "私人会所包厢 · 9宫格"},
	}
	got := PickSkeletonSceneRef(cands, "【0-3秒】镜头：俯视包厢。机位：俯视近景")
	if got == nil || got.ID != 8 {
		t.Fatalf("should pick matching 9-grid cell, got %#v", got)
	}
}

func TestPickSkeletonSceneRefFallsBackToEmptyPlate(t *testing.T) {
	cands := []SkeletonSceneCandidate{
		{ID: 10, Type: "scene", GenType: "scene", Name: "私人会所包厢空镜"},
		{ID: 99, Type: "scene", GenType: "scene_grid", Name: "私人会所包厢 · 9宫格"},
	}
	got := PickSkeletonSceneRef(cands, "【0-3秒】镜头：中景。")
	if got == nil || got.ID != 10 {
		t.Fatalf("without cells should use empty plate, got %#v", got)
	}
}

func TestPickSkeletonSceneRefSkipsFullGridCollage(t *testing.T) {
	cands := []SkeletonSceneCandidate{
		{ID: 99, Type: "scene", GenType: "scene_grid", Name: "私人会所包厢 · 9宫格"},
	}
	if got := PickSkeletonSceneRef(cands, "【0-3秒】镜头：中景。"); got != nil {
		t.Fatalf("full 9-grid collage must not be a skeleton spatial ref, got %#v", got)
	}
}

func containsAll(have, need []string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, n := range need {
		if !set[n] {
			return false
		}
	}
	return true
}
