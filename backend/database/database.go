package database

import (
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"novaly/backend/config"
	"novaly/backend/models"
)

func Open(path string) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err = db.AutoMigrate(&models.Project{}, &models.Episode{}, &models.Shot{}, &models.Resource{}, &models.AIProvider{}, &models.AIModel{}, &models.ImageGenerationJob{}, &models.CrewJob{}, &models.EditProject{}); err != nil {
		return nil, err
	}
	// In-flight image jobs cannot survive process restart; mark them failed so the UI can recover.
	_ = db.Model(&models.ImageGenerationJob{}).
		Where("status IN ?", []string{"pending", "running"}).
		Updates(map[string]any{
			"status":        "failed",
			"message":       "生成任务已中断（服务重启），请重新生成",
			"error_message": "生成任务已中断（服务重启），请重新生成",
		}).Error
	_ = db.Model(&models.CrewJob{}).
		Where("status = ?", "running").
		Updates(map[string]any{
			"status":        "failed",
			"error_message": "剧组任务已中断（服务重启），请重试当前阶段",
		}).Error
	if err = BackfillSceneGridCellNames(db); err != nil {
		return nil, err
	}
	if err = BackfillResourceCandidates(db); err != nil {
		return nil, err
	}
	if err = BackfillMergeExtractCandidates(db); err != nil {
		return nil, err
	}
	if err = BackfillVideoGenMeta(db); err != nil {
		return nil, err
	}
	if err = BackfillSceneGridParents(db); err != nil {
		return nil, err
	}
	if err = BackfillSceneReverseParents(db); err != nil {
		return nil, err
	}
	if err = BackfillScenePanoramaParents(db); err != nil {
		return nil, err
	}
	if err = BackfillScenePanoramaViewParents(db); err != nil {
		return nil, err
	}
	return db, nil
}
func SeedArk(db *gorm.DB, cfg config.Config) error {
	if err := seedDoubaoWebAPI(db, cfg); err != nil {
		return err
	}
	if err := seedVolcengineArk(db, cfg); err != nil {
		return err
	}
	if err := ensureDefaultVideoModel(db); err != nil {
		return err
	}
	if err := ensurePixAPI(db, cfg); err != nil {
		return err
	}
	if err := ensureXais(db, cfg); err != nil {
		return err
	}
	return seedDeepSeek(db, cfg)
}

func ensureXais(db *gorm.DB, cfg config.Config) error {
	baseURL := first(cfg.XaisBaseURL, "https://sg2.dchai.cn")
	var provider models.AIProvider
	if err := db.Where("slug = ?", "xais").First(&provider).Error; err != nil {
		provider = models.AIProvider{
			Name: "Xais", Slug: "xais", BaseURL: baseURL,
			APIKey: cfg.XaisAPIKey, SortOrder: 3, Enabled: true,
		}
		if err = db.Create(&provider).Error; err != nil {
			return err
		}
	} else {
		updates := map[string]any{"base_url": baseURL}
		if cfg.XaisAPIKey != "" {
			updates["api_key"] = cfg.XaisAPIKey
		}
		if err := db.Model(&provider).Updates(updates).Error; err != nil {
			return err
		}
		_ = db.First(&provider, provider.ID).Error
	}

	type item struct{ Name, ModelID string }
	// Family model IDs — actual API id is resolved with resolution (1K/2K/4K) at generate time.
	// GPT Image → Xais Img2_1K / Xais Img2_2K / Xais Img2_4K（见 Xais 价目表）
	// GPT Image HQ → 固定 Xais Img2_4K_H（高画质 4K）
	modelsToSeed := []item{
		{Name: "GPT Image", ModelID: "Image2"},
		{Name: "GPT Image HQ", ModelID: "Xais Img2_4K_H"},
		{Name: "Nano Banana 2", ModelID: "Nano_Banana_2"},
		{Name: "Nano Banana Pro", ModelID: "Nano_Banana_Pro"},
		{Name: "Nano Lite", ModelID: "Xais_Nano_Lite_1K"},
	}
	for i, m := range modelsToSeed {
		var existing models.AIModel
		if err := db.Where("provider_id = ? AND model_id = ?", provider.ID, m.ModelID).First(&existing).Error; err == nil {
			// Refresh display name if we renamed.
			if existing.Name != m.Name {
				_ = db.Model(&existing).Update("name", m.Name).Error
			}
			continue
		}
		row := models.AIModel{
			ProviderID: provider.ID,
			Name:       m.Name,
			ModelID:    m.ModelID,
			Capability: "image",
			Enabled:    true,
			IsDefault:  i == 0 && !hasEnabledDefaultModel(db, "image"),
		}
		if err := db.Create(&row).Error; err != nil {
			return err
		}
	}
	// Remove legacy per-resolution rows (resolution is chosen in the studio UI now).
	legacyIDs := []string{
		"Nano_Banana_2_2K_0", "Nano_Banana_2_4K_0",
		"Nano_Banana_Pro_2K_0", "Nano_Banana_Pro_4K_0",
		"Image2_2K", "Image2_4K", "Xais Img2_1K", "Xais Img2_2K", "Xais Img2_4K",
	}
	_ = db.Where("provider_id = ? AND model_id IN ?", provider.ID, legacyIDs).Delete(&models.AIModel{}).Error

	// GPT Image was seeded earlier but often left disabled; make the family row available once.
	_ = db.Model(&models.AIModel{}).
		Where("provider_id = ? AND model_id = ?", provider.ID, "Image2").
		Updates(map[string]any{"name": "GPT Image", "enabled": true}).Error

	if !hasEnabledDefaultModel(db, "image") {
		_ = clearDefaults(db, "image")
		var prefer models.AIModel
		if err := db.Where("provider_id = ? AND model_id = ? AND enabled = ?", provider.ID, "Image2", true).First(&prefer).Error; err == nil {
			return db.Model(&prefer).Update("is_default", true).Error
		}
		var anyImage models.AIModel
		if err := db.Where("capability = ? AND enabled = ?", "image", true).Order("id asc").First(&anyImage).Error; err == nil {
			return db.Model(&anyImage).Update("is_default", true).Error
		}
	}
	return nil
}

func seedVolcengineArk(db *gorm.DB, cfg config.Config) error {
	var provider models.AIProvider
	if err := db.Where("slug = ?", "volcengine-ark").First(&provider).Error; err != nil {
		provider = models.AIProvider{Name: "火山引擎 Ark", Slug: "volcengine-ark", BaseURL: cfg.ArkBaseURL, APIKey: cfg.ArkAPIKey, SortOrder: 1, Enabled: true}
		if err := db.Create(&provider).Error; err != nil {
			return err
		}
		items := []models.AIModel{
			{Name: "Doubao Seed 2.0 Pro", ModelID: first(cfg.ArkModel, "doubao-seed-2-0-pro-260215"), Capability: "text", Enabled: true, IsDefault: true},
			{Name: "Doubao Seed 2.0 Lite", ModelID: "doubao-seed-2-0-lite-260215", Capability: "text"},
			{Name: "Seedream 5.0 Lite", ModelID: "doubao-seedream-5-0-260128", Capability: "image", Enabled: true, IsDefault: !hasEnabledDefaultModel(db, "image")},
			{Name: "Seedream 4.5", ModelID: "doubao-seedream-4-5-251128", Capability: "image", Enabled: true},
			{Name: "Seedance 2.0 Fast", ModelID: "doubao-seedance-2-0-fast-260128", Capability: "video", Enabled: true},
			{Name: "Seedance 2.0 Mini", ModelID: "doubao-seedance-2-0-mini-260615", Capability: "video", Enabled: true},
		}
		for i := range items {
			items[i].ProviderID = provider.ID
		}
		return db.Create(&items).Error
	}
	updates := map[string]any{}
	if cfg.ArkBaseURL != "" {
		updates["base_url"] = cfg.ArkBaseURL
	}
	if cfg.ArkAPIKey != "" {
		updates["api_key"] = cfg.ArkAPIKey
	}
	if len(updates) > 0 {
		if err := db.Model(&provider).Updates(updates).Error; err != nil {
			return err
		}
	}
	if err := ensureVolcengineImageModels(db, provider.ID); err != nil {
		return err
	}
	return ensureVolcengineVideoModels(db, provider.ID)
}

// ensureVolcengineImageModels backfills Seedream models when the provider already existed.
// Does not re-enable or steal default from models the user already configured.
func ensureVolcengineImageModels(db *gorm.DB, providerID uint) error {
	items := []models.AIModel{
		{
			ProviderID: providerID, Name: "Seedream 5.0 Lite", ModelID: "doubao-seedream-5-0-260128",
			Capability: "image", Enabled: true, IsDefault: !hasEnabledDefaultModel(db, "image"),
		},
		{
			ProviderID: providerID, Name: "Seedream 4.5", ModelID: "doubao-seedream-4-5-251128",
			Capability: "image", Enabled: true, IsDefault: false,
		},
	}
	for _, imageModel := range items {
		var existing models.AIModel
		if err := db.Where("provider_id = ? AND model_id = ?", providerID, imageModel.ModelID).First(&existing).Error; err == nil {
			continue
		}
		if err := db.Create(&imageModel).Error; err != nil {
			return err
		}
	}
	return nil
}

// ensureVolcengineVideoModels backfills newly supported Ark video models for
// existing installations without changing the user's current default model.
func ensureVolcengineVideoModels(db *gorm.DB, providerID uint) error {
	model := models.AIModel{
		ProviderID: providerID,
		Name:       "Seedance 2.0 Mini",
		ModelID:    "doubao-seedance-2-0-mini-260615",
		Capability: "video",
		Enabled:    true,
		IsDefault:  false,
	}
	var existing models.AIModel
	if err := db.Where("provider_id = ? AND model_id = ?", providerID, model.ModelID).First(&existing).Error; err == nil {
		return nil
	}
	return db.Create(&model).Error
}

func ensurePixAPI(db *gorm.DB, cfg config.Config) error {
	baseURL := first(cfg.PixAPIBaseURL, "https://api.pixapi.ai/v1")
	var provider models.AIProvider
	if err := db.Where("slug = ?", "pixapi").First(&provider).Error; err != nil {
		provider = models.AIProvider{
			Name: "PixAPI", Slug: "pixapi", BaseURL: baseURL,
			APIKey: cfg.PixAPIAPIKey, SortOrder: 0, Enabled: true,
		}
		if err = db.Create(&provider).Error; err != nil {
			return err
		}
	} else {
		updates := map[string]any{"base_url": baseURL}
		if cfg.PixAPIAPIKey != "" {
			updates["api_key"] = cfg.PixAPIAPIKey
		}
		if err := db.Model(&provider).Updates(updates).Error; err != nil {
			return err
		}
		_ = db.First(&provider, provider.ID).Error
	}
	var imageModel models.AIModel
	if err := db.Where("provider_id = ? AND model_id = ?", provider.ID, "gpt-image-2").First(&imageModel).Error; err != nil {
		imageModel = models.AIModel{
			ProviderID: provider.ID, Name: "GPT Image 2", ModelID: "gpt-image-2",
			Capability: "image", Enabled: true,
			// Only become default when no enabled image default exists yet.
			IsDefault: !hasEnabledDefaultModel(db, "image"),
		}
		return db.Create(&imageModel).Error
	}
	// Respect user toggles: do not force-enable or overwrite is_default on restart.
	if !hasEnabledDefaultModel(db, "image") {
		// Clear stale defaults on disabled models, then promote this one only if still enabled
		// or no other enabled image model exists.
		_ = clearDefaults(db, "image")
		var enabled models.AIModel
		if err := db.Where("capability = ? AND enabled = ?", "image", true).Order("id asc").First(&enabled).Error; err == nil {
			return db.Model(&enabled).Update("is_default", true).Error
		}
	}
	return nil
}

func hasDefaultModel(db *gorm.DB, capability string) bool {
	var count int64
	db.Model(&models.AIModel{}).Where("capability = ? AND is_default = ?", capability, true).Count(&count)
	return count > 0
}

func hasEnabledDefaultModel(db *gorm.DB, capability string) bool {
	var count int64
	db.Model(&models.AIModel{}).Where("capability = ? AND enabled = ? AND is_default = ?", capability, true, true).Count(&count)
	return count > 0
}

func clearDefaults(db *gorm.DB, capability string) error {
	return db.Model(&models.AIModel{}).Where("capability = ?", capability).Update("is_default", false).Error
}

func seedDoubaoWebAPI(db *gorm.DB, cfg config.Config) error {
	baseURL := first(cfg.DoubaoWebBaseURL, "http://127.0.0.1:8080/api/v3")
	var provider models.AIProvider
	if err := db.Where("slug = ?", "doubao-web-api").First(&provider).Error; err != nil {
		provider = models.AIProvider{
			Name: "豆包 Web API", Slug: "doubao-web-api", BaseURL: baseURL,
			APIKey: cfg.DoubaoWebAPIKey, SortOrder: 2, Enabled: true,
		}
		if err = db.Create(&provider).Error; err != nil {
			return err
		}
		items := []models.AIModel{
			{Name: "Seedream Web", ModelID: "doubao-seedream-5-0", Capability: "image", Enabled: true, IsDefault: !hasEnabledDefaultModel(db, "image")},
			{Name: "Seedance 2.0 Mini", ModelID: "doubao-seedance-2-0-mini", Capability: "video", Enabled: true},
			{Name: "Seedance Web", ModelID: "doubao-seedance-2-0-fast", Capability: "video", Enabled: true, IsDefault: true},
			{Name: "Seedance 2.0", ModelID: "doubao-seedance-2-0", Capability: "video", Enabled: true},
		}
		for i := range items {
			items[i].ProviderID = provider.ID
		}
		return db.Create(&items).Error
	}
	updates := map[string]any{"base_url": baseURL}
	if cfg.DoubaoWebAPIKey != "" {
		updates["api_key"] = cfg.DoubaoWebAPIKey
	}
	if err := db.Model(&provider).Updates(updates).Error; err != nil {
		return err
	}
	var videoModel models.AIModel
	if err := db.Where("provider_id = ? AND model_id = ?", provider.ID, "doubao-seedance-2-0-fast").First(&videoModel).Error; err != nil {
		videoModel = models.AIModel{
			ProviderID: provider.ID, Name: "Seedance Web", ModelID: "doubao-seedance-2-0-fast",
			Capability: "video", Enabled: true, IsDefault: !hasEnabledDefaultModel(db, "video"),
		}
		return db.Create(&videoModel).Error
	}
	if videoModel.Name == "Seedance 2.0 Fast" {
		_ = db.Model(&videoModel).Update("name", "Seedance Web").Error
	}
	return nil
}

// ensureDefaultVideoModel only fills a video default when none is enabled+default.
// It must NOT override the user's choice on every process restart.
func ensureDefaultVideoModel(db *gorm.DB) error {
	if hasEnabledDefaultModel(db, "video") {
		return nil
	}
	_ = clearDefaults(db, "video")
	var provider models.AIProvider
	if err := db.Where("slug = ?", "doubao-web-api").First(&provider).Error; err != nil {
		// Fall back to any enabled video model.
		var any models.AIModel
		if err := db.Where("capability = ? AND enabled = ?", "video", true).Order("id asc").First(&any).Error; err != nil {
			return nil
		}
		return db.Model(&any).Update("is_default", true).Error
	}
	var videoModel models.AIModel
	if err := db.Where("provider_id = ? AND model_id = ? AND enabled = ?", provider.ID, "doubao-seedance-2-0-fast", true).First(&videoModel).Error; err != nil {
		var any models.AIModel
		if err := db.Where("capability = ? AND enabled = ?", "video", true).Order("id asc").First(&any).Error; err != nil {
			return nil
		}
		return db.Model(&any).Update("is_default", true).Error
	}
	return db.Model(&videoModel).Update("is_default", true).Error
}
func seedDeepSeek(db *gorm.DB, cfg config.Config) error {
	baseURL := first(cfg.DeepSeekBaseURL, "https://api.deepseek.com/v1")
	var provider models.AIProvider
	if err := db.Where("slug = ?", "deepseek").First(&provider).Error; err != nil {
		provider = models.AIProvider{
			Name: "DeepSeek", Slug: "deepseek", BaseURL: baseURL,
			APIKey: cfg.DeepSeekAPIKey, SortOrder: 0, Enabled: true,
		}
		if err = db.Create(&provider).Error; err != nil {
			return err
		}
	} else {
		updates := map[string]any{"base_url": baseURL, "enabled": true}
		if cfg.DeepSeekAPIKey != "" {
			updates["api_key"] = cfg.DeepSeekAPIKey
		}
		if err := db.Model(&provider).Updates(updates).Error; err != nil {
			return err
		}
		_ = db.First(&provider, provider.ID).Error
	}

	type item struct{ Name, ModelID string }
	modelsToSeed := []item{
		{Name: "DeepSeek V4 Pro", ModelID: "deepseek-v4-pro"},
		{Name: "DeepSeek V4 Flash", ModelID: "deepseek-v4-flash"},
	}
	proCreated := false
	var pro models.AIModel
	for _, m := range modelsToSeed {
		var existing models.AIModel
		if err := db.Where("provider_id = ? AND model_id = ?", provider.ID, m.ModelID).First(&existing).Error; err == nil {
			if existing.Name != m.Name {
				_ = db.Model(&existing).Update("name", m.Name).Error
			}
			if m.ModelID == "deepseek-v4-pro" {
				pro = existing
			}
			continue
		}
		row := models.AIModel{
			ProviderID: provider.ID,
			Name:       m.Name,
			ModelID:    m.ModelID,
			Capability: "text",
			Enabled:    true,
			IsDefault:  false,
		}
		if err := db.Create(&row).Error; err != nil {
			return err
		}
		if m.ModelID == "deepseek-v4-pro" {
			pro = row
			proCreated = true
		}
	}
	if pro.ID == 0 {
		return nil
	}
	// First insert of V4 Pro: switch default text so crew / 分镜文案走 DeepSeek.
	if proCreated {
		if err := clearDefaults(db, "text"); err != nil {
			return err
		}
		return db.Model(&pro).Updates(map[string]any{"enabled": true, "is_default": true}).Error
	}
	if !hasEnabledDefaultModel(db, "text") {
		_ = clearDefaults(db, "text")
		return db.Model(&pro).Updates(map[string]any{"enabled": true, "is_default": true}).Error
	}
	return nil
}

func first(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
