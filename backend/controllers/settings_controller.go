package controllers

import (
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"novaly/backend/models"
	"novaly/backend/services"
)

type SettingsController struct {
	DB  *gorm.DB
	Ark *services.ArkService
}

// RevealAPIKey intentionally returns the local provider key only after an explicit user action.
// This application is single-user/local at the moment; add authentication before exposing it remotely.
func (sc *SettingsController) RevealAPIKey(c *gin.Context) {
	var provider models.AIProvider
	if err := sc.DB.First(&provider, c.Param("id")).Error; err != nil {
		fail(c, 404, "服务商不存在")
		return
	}
	c.JSON(200, gin.H{"apiKey": provider.APIKey})
}

func (sc *SettingsController) ListProviders(c *gin.Context) {
	var providers []models.AIProvider
	if err := sc.DB.Preload("Models", func(db *gorm.DB) *gorm.DB { return db.Order("capability, id") }).Order("sort_order,id").Find(&providers).Error; err != nil {
		fail(c, 500, "读取配置失败")
		return
	}
	result := make([]models.ProviderDTO, 0, len(providers))
	for _, p := range providers {
		result = append(result, providerDTO(p))
	}
	c.JSON(200, result)
}
func (sc *SettingsController) UpdateProvider(c *gin.Context) {
	var provider models.AIProvider
	if err := sc.DB.First(&provider, c.Param("id")).Error; err != nil {
		fail(c, 404, "服务商不存在")
		return
	}
	var input struct {
		Name    string  `json:"name"`
		BaseURL string  `json:"baseUrl"`
		APIKey  *string `json:"apiKey"`
		Enabled *bool   `json:"enabled"`
	}
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	if strings.TrimSpace(input.Name) != "" {
		provider.Name = strings.TrimSpace(input.Name)
	}
	if strings.TrimSpace(input.BaseURL) != "" {
		provider.BaseURL = strings.TrimSuffix(strings.TrimSpace(input.BaseURL), "/")
	}
	if input.APIKey != nil {
		provider.APIKey = strings.TrimSpace(*input.APIKey)
	}
	if input.Enabled != nil {
		provider.Enabled = *input.Enabled
	}
	if err := sc.DB.Save(&provider).Error; err != nil {
		fail(c, 500, "保存配置失败")
		return
	}
	sc.DB.Preload("Models").First(&provider, provider.ID)
	c.JSON(200, providerDTO(provider))
}
func (sc *SettingsController) AddModel(c *gin.Context) {
	var provider models.AIProvider
	if sc.DB.First(&provider, c.Param("id")).Error != nil {
		fail(c, 404, "服务商不存在")
		return
	}
	var input struct {
		Name       string `json:"name"`
		ModelID    string `json:"modelId"`
		Capability string `json:"capability"`
	}
	if c.ShouldBindJSON(&input) != nil || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.ModelID) == "" || !validCapability(input.Capability) {
		fail(c, 400, "请填写模型名称、模型 ID 与能力类型")
		return
	}
	model := models.AIModel{ProviderID: provider.ID, Name: strings.TrimSpace(input.Name), ModelID: strings.TrimSpace(input.ModelID), Capability: input.Capability}
	if err := sc.DB.Create(&model).Error; err != nil {
		fail(c, 500, "添加模型失败")
		return
	}
	c.JSON(201, model)
}
func (sc *SettingsController) UpdateModel(c *gin.Context) {
	var model models.AIModel
	if sc.DB.First(&model, c.Param("id")).Error != nil {
		fail(c, 404, "模型不存在")
		return
	}
	var input struct {
		Name      string `json:"name"`
		ModelID   string `json:"modelId"`
		Enabled   *bool  `json:"enabled"`
		IsDefault *bool  `json:"isDefault"`
	}
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	if strings.TrimSpace(input.Name) != "" {
		model.Name = strings.TrimSpace(input.Name)
	}
	if strings.TrimSpace(input.ModelID) != "" {
		model.ModelID = strings.TrimSpace(input.ModelID)
	}
	if input.Enabled != nil {
		model.Enabled = *input.Enabled
		if !model.Enabled {
			model.IsDefault = false
		}
	}
	if input.IsDefault != nil && *input.IsDefault {
		if err := sc.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.AIModel{}).Where("capability = ?", model.Capability).Update("is_default", false).Error; err != nil {
				return err
			}
			model.IsDefault = true
			model.Enabled = true
			return tx.Save(&model).Error
		}); err != nil {
			fail(c, 500, "设置默认模型失败")
			return
		}
		c.JSON(200, model)
		return
	}
	if input.IsDefault != nil {
		model.IsDefault = *input.IsDefault
	}
	if err := sc.DB.Save(&model).Error; err != nil {
		fail(c, 500, "保存模型失败")
		return
	}
	c.JSON(200, model)
}
func (sc *SettingsController) TestProvider(c *gin.Context) {
	var provider models.AIProvider
	if sc.DB.First(&provider, c.Param("id")).Error != nil {
		fail(c, 404, "服务商不存在")
		return
	}
	if services.IsDoubaoWebAPI(provider) {
		if err := sc.Ark.Test(provider, models.AIModel{}); err != nil {
			fail(c, 502, "连接失败："+err.Error())
			return
		}
		c.JSON(200, gin.H{"message": "连接成功"})
		return
	}
	var model models.AIModel
	if err := sc.DB.Where("provider_id = ? AND capability = ? AND enabled = ?", provider.ID, "text", true).First(&model).Error; err != nil {
		fail(c, 400, "请先启用一个文本模型")
		return
	}
	if err := sc.Ark.Test(provider, model); err != nil {
		fail(c, 502, "连接失败："+err.Error())
		return
	}
	c.JSON(200, gin.H{"message": "连接成功"})
}
func providerDTO(p models.AIProvider) models.ProviderDTO {
	return models.ProviderDTO{ID: p.ID, Name: p.Name, Slug: p.Slug, BaseURL: p.BaseURL, APIKeyMasked: maskKey(p.APIKey), HasAPIKey: p.APIKey != "", SortOrder: p.SortOrder, Enabled: p.Enabled, Models: p.Models}
}
func maskKey(key string) string {
	if len(key) < 9 {
		return ""
	}
	return key[:5] + strings.Repeat("•", max(8, len(key)-9)) + key[len(key)-4:]
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func validCapability(value string) bool {
	return value == "text" || value == "image" || value == "video"
}
