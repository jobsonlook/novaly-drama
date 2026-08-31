package models

import "time"

type AIProvider struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `json:"name"`
	Slug      string    `gorm:"uniqueIndex" json:"slug"`
	BaseURL   string    `json:"baseUrl"`
	APIKey    string    `json:"-"`
	SortOrder int       `json:"sortOrder"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Models    []AIModel `gorm:"foreignKey:ProviderID" json:"models"`
}
type AIModel struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ProviderID uint      `gorm:"index" json:"providerId"`
	Name       string    `json:"name"`
	ModelID    string    `json:"modelId"`
	Capability string    `json:"capability"` // text, image, video
	Enabled    bool      `json:"enabled"`
	IsDefault  bool      `json:"isDefault"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
type ProviderDTO struct {
	ID           uint      `json:"id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	BaseURL      string    `json:"baseUrl"`
	APIKeyMasked string    `json:"apiKeyMasked"`
	HasAPIKey    bool      `json:"hasApiKey"`
	SortOrder    int       `json:"sortOrder"`
	Enabled      bool      `json:"enabled"`
	Models       []AIModel `json:"models"`
}
