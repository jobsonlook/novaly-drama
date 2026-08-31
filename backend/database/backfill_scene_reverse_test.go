package database

import (
	"fmt"
	"testing"

	"novaly/backend/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSceneReversePlateName(t *testing.T) {
	got := sceneReversePlateName("包厢站位图-反打 · 反打骨架")
	if got != "包厢站位图-反打" {
		t.Fatalf("got %q", got)
	}
	got = sceneReversePlateName("包厢站位图-反打 · 反打 · 候选1")
	if got != "包厢站位图-反打" {
		t.Fatalf("candidate plate got %q", got)
	}
}

func TestBackfillSceneReverseParents(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:reverse-parent?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Resource{}); err != nil {
		t.Fatal(err)
	}
	original := models.Resource{ProjectID: 1, Type: "scene", Name: "包厢站位图-反打"}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	skeleton := models.Resource{
		ProjectID:   1,
		Type:        "other",
		Name:        "包厢站位图-反打 · 反打骨架",
		GenType:     "scene_reverse_skeleton",
		GenRefsJSON: fmt.Sprintf(`[{"id":%d,"kind":"scene"}]`, original.ID),
	}
	reverse := models.Resource{
		ProjectID: 1,
		Type:      "scene",
		Name:      "包厢站位图-反打 · 反打",
		GenType:   "scene_reverse",
	}
	if err := db.Create(&skeleton).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&reverse).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillSceneReverseParents(db); err != nil {
		t.Fatal(err)
	}
	var gotSk, gotRv models.Resource
	if err := db.First(&gotSk, skeleton.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&gotRv, reverse.ID).Error; err != nil {
		t.Fatal(err)
	}
	if resourceParentKey(gotSk) != original.ID {
		t.Fatalf("skeleton parent=%d want %d", resourceParentKey(gotSk), original.ID)
	}
	if resourceParentKey(gotRv) != original.ID {
		t.Fatalf("reverse parent=%d want %d", resourceParentKey(gotRv), original.ID)
	}
}

func TestBackfillSceneGridParents(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:grid-parent?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Resource{}); err != nil {
		t.Fatal(err)
	}
	original := models.Resource{ProjectID: 1, Type: "scene", Name: "私人会所包厢"}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	grid := models.Resource{
		ProjectID: 1,
		Type:      "scene",
		Name:      "私人会所包厢 · 9宫格",
		GenType:   "scene_grid",
	}
	if err := db.Create(&grid).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillSceneGridParents(db); err != nil {
		t.Fatal(err)
	}
	var got models.Resource
	if err := db.First(&got, grid.ID).Error; err != nil {
		t.Fatal(err)
	}
	if resourceParentKey(got) != original.ID {
		t.Fatalf("grid parent=%d want %d", resourceParentKey(got), original.ID)
	}
}

func TestBackfillResourceCandidatesKeepsGridCells(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:grid-cand?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Resource{}); err != nil {
		t.Fatal(err)
	}
	a := models.Resource{
		ProjectID: 1, Type: "scene", Name: "私人会所包厢·俯视全景·候选1",
		GenType: "scene_grid_cell", GridID: 11, GridCell: 7,
	}
	b := models.Resource{
		ProjectID: 1, Type: "scene", Name: "私人会所包厢·俯视全景·候选2",
		GenType: "scene_grid_cell", GridID: 12, GridCell: 7,
	}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillResourceCandidates(db); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&models.Resource{}).Where("gen_type = ?", "scene_grid_cell").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("kept %d grid cells, want 2", n)
	}
}

func TestBackfillMergeExtractKeepsGridCells(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:grid-merge?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Resource{}); err != nil {
		t.Fatal(err)
	}
	canonical := models.Resource{ProjectID: 1, Type: "scene", Name: "私人会所包厢·俯视全景"}
	cell := models.Resource{
		ProjectID: 1, Type: "scene", Name: "私人会所包厢·俯视全景·候选1",
		GenType: "scene_grid_cell", GridID: 21, GridCell: 7,
	}
	if err := db.Create(&canonical).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&cell).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillMergeExtractCandidates(db); err != nil {
		t.Fatal(err)
	}
	var got models.Resource
	if err := db.First(&got, cell.ID).Error; err != nil {
		t.Fatalf("grid cell deleted by merge backfill: %v", err)
	}
}

func TestBackfillSceneGridCellNamesStripsCandidate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:grid-rename?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Resource{}); err != nil {
		t.Fatal(err)
	}
	grid := models.Resource{ProjectID: 1, Type: "scene", Name: "私人会所包厢 · 9宫格 · 候选1", GenType: "scene_grid"}
	if err := db.Create(&grid).Error; err != nil {
		t.Fatal(err)
	}
	cell := models.Resource{
		ProjectID: 1, Type: "scene", Name: "私人会所包厢·俯视全景·候选1",
		GenType: "scene_grid_cell", GridID: grid.ID, GridCell: 7,
	}
	if err := db.Create(&cell).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillSceneGridCellNames(db); err != nil {
		t.Fatal(err)
	}
	var got models.Resource
	if err := db.First(&got, cell.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Name != "私人会所包厢·俯视全景" {
		t.Fatalf("renamed to %q", got.Name)
	}
}
