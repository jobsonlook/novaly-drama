package database

import (
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"novaly/backend/config"
	"novaly/backend/models"
)

func TestSeedArkCreatesOnlyDefaultProviders(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIProvider{}, &models.AIModel{}); err != nil {
		t.Fatal(err)
	}
	if err := SeedArk(db, config.Config{}); err != nil {
		t.Fatal(err)
	}
	var providers []models.AIProvider
	if err := db.Order("slug").Find(&providers).Error; err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(providers))
	for _, provider := range providers {
		got = append(got, provider.Slug)
	}
	want := []string{"deepseek", "doubao-web-api", "volcengine-ark"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default providers = %v, want %v", got, want)
	}
}
