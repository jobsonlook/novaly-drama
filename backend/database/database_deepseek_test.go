package database

import (
	"testing"

	"novaly/backend/config"
	"novaly/backend/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSeedDeepSeekSetsDefaultTextOnce(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:deepseek-seed?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIProvider{}, &models.AIModel{}); err != nil {
		t.Fatal(err)
	}
	doubao := models.AIProvider{Name: "火山", Slug: "volcengine-ark", Enabled: true}
	if err := db.Create(&doubao).Error; err != nil {
		t.Fatal(err)
	}
	old := models.AIModel{ProviderID: doubao.ID, Name: "Doubao", ModelID: "doubao", Capability: "text", Enabled: true, IsDefault: true}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{DeepSeekAPIKey: "sk-test", DeepSeekBaseURL: "https://api.deepseek.com/v1"}
	if err := seedDeepSeek(db, cfg); err != nil {
		t.Fatal(err)
	}
	var pro models.AIModel
	if err := db.Where("model_id = ?", "deepseek-v4-pro").First(&pro).Error; err != nil {
		t.Fatal(err)
	}
	if !pro.IsDefault || !pro.Enabled {
		t.Fatalf("pro default=%v enabled=%v", pro.IsDefault, pro.Enabled)
	}
	if err := db.First(&old, old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if old.IsDefault {
		t.Fatal("old doubao should no longer be default")
	}

	if err := db.Model(&pro).Update("is_default", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&old).Update("is_default", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := seedDeepSeek(db, cfg); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&pro, pro.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pro.IsDefault {
		t.Fatal("second seed should not steal the text default")
	}
}
