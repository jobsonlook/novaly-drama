package database

import (
	"testing"

	"novaly/backend/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestEnsureVolcengineVideoModelsBackfillsMiniWithoutStealingDefault(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:volcengine-video-seed?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIProvider{}, &models.AIModel{}); err != nil {
		t.Fatal(err)
	}
	provider := models.AIProvider{Name: "火山引擎 Ark", Slug: "volcengine-ark", Enabled: true}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	fast := models.AIModel{
		ProviderID: provider.ID, Name: "Seedance 2.0 Fast", ModelID: "doubao-seedance-2-0-fast-260128",
		Capability: "video", Enabled: true, IsDefault: true,
	}
	if err := db.Create(&fast).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureVolcengineVideoModels(db, provider.ID); err != nil {
		t.Fatal(err)
	}
	var mini models.AIModel
	if err := db.Where("provider_id = ? AND model_id = ?", provider.ID, "doubao-seedance-2-0-mini-260615").First(&mini).Error; err != nil {
		t.Fatal(err)
	}
	if !mini.Enabled || mini.Capability != "video" || mini.IsDefault {
		t.Fatalf("unexpected mini model: %#v", mini)
	}
	if err := db.First(&fast, fast.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !fast.IsDefault {
		t.Fatal("backfill must preserve the existing default video model")
	}
}
