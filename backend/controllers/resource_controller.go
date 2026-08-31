package controllers

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"novaly/backend/models"
	"novaly/backend/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ResourceController struct {
	DB            *gorm.DB
	Ark           *services.ArkService
	Storage       *services.Storage
	TOS           *services.TOSStorage
	PublicBaseURL string
	// PixRefRelay is the Tokyo relay origin (e.g. http://43.133.196.27:9080).
	// PixAPI fetches refs via this host (fast), which then pulls COS/app (not PixAPI→COS direct).
	PixRefRelay string
}

func (rc *ResourceController) List(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	typ := strings.TrimSpace(c.Query("type"))
	search := strings.TrimSpace(c.Query("q"))
	page := parsePositiveInt(c.Query("page"), 1)
	pageSize := parsePositiveInt(c.Query("pageSize"), 12)
	if pageSize > 48 {
		pageSize = 48
	}
	enrich := c.Query("enrich") != "0"
	hideSceneGridCells := c.Query("hideSceneGridCells") == "1"
	parentID := parseID(c.Query("parentId"))
	hideDerivatives := parentID == 0 && c.Query("includeDerivatives") != "1"

	counts := gin.H{
		"all":       countLibraryResources(rc.DB, projectID, "", "", hideSceneGridCells, 0, true),
		"character": countLibraryResources(rc.DB, projectID, "character", "", hideSceneGridCells, 0, true),
		"scene":     countLibraryResources(rc.DB, projectID, "scene", "", hideSceneGridCells, 0, true),
		"prop":      countLibraryResources(rc.DB, projectID, "prop", "", hideSceneGridCells, 0, true),
		"other":     countLibraryResources(rc.DB, projectID, "other", "", hideSceneGridCells, 0, true),
		"video":     countLibraryResources(rc.DB, projectID, "video", "", hideSceneGridCells, 0, true),
	}

	q := libraryResourcesQuery(rc.DB, projectID, typ, search, hideSceneGridCells, parentID, hideDerivatives)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		fail(c, 500, "读取资源失败")
		return
	}

	var items []models.Resource
	if err := q.Order("created_at desc, id desc").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&items).Error; err != nil {
		fail(c, 500, "读取资源失败")
		return
	}
	for i := range items {
		if enrich {
			ensureResourceVideoCopy(rc.DB, rc.Storage, &items[i])
			fillResourceFields(&items[i], rc.DB, rc.Storage)
		} else {
			fillResourceURLs(&items[i], rc.Storage)
		}
	}
	if parentID == 0 {
		fillDeriveCounts(rc.DB, items)
	}
	c.JSON(200, gin.H{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
		"counts":   counts,
	})
}

func libraryResourcesQuery(db *gorm.DB, projectID uint, typ, search string, hideSceneGridCells bool, parentID uint, hideDerivatives bool) *gorm.DB {
	q := db.Model(&models.Resource{}).
		Where("project_id = ?", projectID).
		Where("(name NOT LIKE ? OR is_group_primary = ?)", "% · 候选%", true)
	if hideSceneGridCells {
		q = q.Where("(gen_type IS NULL OR gen_type NOT IN ?)", []string{
			"scene_grid_cell",
			"motion_grid_cell",
			"scene_panorama_view",
		})
	}
	if parentID > 0 {
		q = q.Where("parent_id = ?", parentID)
	} else if hideDerivatives {
		q = q.Where("parent_id IS NULL")
	}
	if parentID == 0 && typ != "" && typ != "all" {
		q = q.Where("type = ?", typ)
	}
	if search != "" {
		like := "%" + search + "%"
		q = q.Where("(name LIKE ? OR description LIKE ? OR COALESCE(remark, '') LIKE ?)", like, like, like)
	}
	return q
}

func countLibraryResources(db *gorm.DB, projectID uint, typ, search string, hideSceneGridCells bool, parentID uint, hideDerivatives bool) int64 {
	var n int64
	_ = libraryResourcesQuery(db, projectID, typ, search, hideSceneGridCells, parentID, hideDerivatives).Count(&n).Error
	return n
}

func fillDeriveCounts(db *gorm.DB, items []models.Resource) {
	ids := make([]uint, 0, len(items))
	for _, r := range items {
		if r.ParentID == nil || *r.ParentID == 0 {
			ids = append(ids, r.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	type row struct {
		ParentID uint `gorm:"column:parent_id"`
		N        int  `gorm:"column:n"`
	}
	var rows []row
	_ = db.Model(&models.Resource{}).
		Select("parent_id, count(*) as n").
		Where("parent_id IN ?", ids).
		Group("parent_id").
		Scan(&rows).Error
	byID := map[uint]int{}
	for _, r := range rows {
		byID[r.ParentID] = r.N
	}
	for i := range items {
		items[i].DeriveCount = byID[items[i].ID]
	}
}

func parsePositiveInt(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func (rc *ResourceController) GenerateCharacter(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	var input imageGenJobInput
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	rc.startImageGenerationJob(c, projectID, "character", input)
}

func (rc *ResourceController) GenerateScene(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	var input imageGenJobInput
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	rc.startImageGenerationJob(c, projectID, "scene", input)
}

func (rc *ResourceController) GenerateProp(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	var input imageGenJobInput
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	rc.startImageGenerationJob(c, projectID, "prop", input)
}

func (rc *ResourceController) Create(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	var input struct {
		Type        string `json:"type"`
		Name        string `json:"name"`
		Description string `json:"description"`
		ImageData   string `json:"imageData"`
		ImageURL    string `json:"imageUrl"`
		ParentID    uint   `json:"parentId"`
	}
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	if input.Type != "character" && input.Type != "scene" && input.Type != "prop" && input.Type != "other" {
		fail(c, 400, "资源类型必须是 character、scene、prop 或 other")
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "参考图"
	}
	if strings.TrimSpace(input.ImageData) == "" && strings.TrimSpace(input.ImageURL) == "" {
		fail(c, 400, "请上传资源图片或选择 AI 生成的候选图")
		return
	}
	parentID := rc.resolveCreateParentID(projectID, input.ParentID)
	resource := models.Resource{
		ProjectID:   projectID,
		Type:        input.Type,
		Source:      "upload",
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		ParentID:    parentID,
	}
	created := false
	if existing, ok := rc.findCanonicalResource(projectID, input.Type, name, parentID); ok {
		resource = existing
		if strings.TrimSpace(input.Description) != "" {
			resource.Description = strings.TrimSpace(input.Description)
		}
	} else {
		if err := rc.DB.Create(&resource).Error; err != nil {
			fail(c, 500, "创建资源失败")
			return
		}
		created = true
	}
	if strings.TrimSpace(input.ImageURL) != "" {
		resource.Source = "ai"
	}
	var path string
	var err error
	if strings.TrimSpace(input.ImageData) != "" {
		path, err = rc.Storage.SaveResourceImage(projectID, resource.ID, input.ImageData)
	} else {
		data, dlErr := rc.Ark.DownloadImage(strings.TrimSpace(input.ImageURL))
		if dlErr != nil {
			if created {
				rc.DB.Delete(&resource)
			}
			fail(c, 502, "下载图片失败："+dlErr.Error())
			return
		}
		path, err = rc.Storage.SaveResourceImageBytes(projectID, resource.ID, data)
	}
	if err != nil {
		if created {
			rc.DB.Delete(&resource)
		}
		fail(c, 400, err.Error())
		return
	}
	resource.ImagePath = path
	resource.IsGroupPrimary = true
	rc.DB.Save(&resource)
	if kept, actErr := rc.activateCandidatePrimary(resource); actErr == nil {
		resource = kept
	}
	fillResourceURLs(&resource, rc.Storage)
	c.JSON(201, resource)
}

func (rc *ResourceController) Stylize(c *gin.Context) {
	resource, ok := rc.find(c)
	if !ok {
		return
	}
	if resource.Type != "character" && resource.Type != "scene" && resource.Type != "other" && resource.Type != "prop" {
		fail(c, 400, "仅角色、场景、道具或其他资源可生成非真人图")
		return
	}
	var input struct {
		Prompt     string `json:"prompt"`
		ModelID    uint   `json:"modelId"`
		Resolution string `json:"resolution"`
		Quality    string `json:"quality"`
	}
	_ = c.ShouldBindJSON(&input)
	if err := rc.stylizeImageResource(&resource, strings.TrimSpace(input.Prompt), input.ModelID, firstNonEmpty(input.Resolution, input.Quality)); err != nil {
		fail(c, 502, "非真人图生成失败："+err.Error())
		return
	}
	fillResourceFields(&resource, rc.DB, rc.Storage)
	c.JSON(200, resource)
}

func defaultStylizePrompt(resourceType string) string {
	if resourceType == "scene" {
		return services.SceneStylizePrompt
	}
	if resourceType == "other" {
		return services.OtherStylizePrompt
	}
	if resourceType == "prop" {
		return services.OtherStylizePrompt
	}
	return services.CharacterStylizePrompt
}

func (rc *ResourceController) stylizeImageResource(resource *models.Resource, customPrompt string, modelID uint, resolution string) error {
	if resource.ImagePath == "" {
		return nil
	}
	totalStart := time.Now()
	var model models.AIModel
	if modelID != 0 {
		if err := rc.DB.Where("id = ? AND capability = ? AND enabled = ?", modelID, "image", true).First(&model).Error; err != nil {
			return fmt.Errorf("所选图像模型不可用")
		}
	} else if err := rc.DB.Where("capability = ? AND enabled = ? AND is_default = ?", "image", true, true).First(&model).Error; err != nil {
		return err
	}
	var provider models.AIProvider
	if err := rc.DB.First(&provider, model.ProviderID).Error; err != nil {
		return err
	}
	resolveStart := time.Now()
	sourceURL, err := rc.resolveReferenceImage(provider, resource.ProjectID, "", resource.ID, "original")
	if err != nil {
		return err
	}
	resolveMs := time.Since(resolveStart).Milliseconds()
	prompt := customPrompt
	if prompt == "" {
		prompt = defaultStylizePrompt(resource.Type)
	}
	genStart := time.Now()
	remoteURL, err := rc.Ark.StylizeImageWithSpec(provider, model, sourceURL, prompt, resolution)
	if err != nil {
		return err
	}
	genMs := time.Since(genStart).Milliseconds()
	dlStart := time.Now()
	data, err := rc.Ark.DownloadImagePreferPix(remoteURL, services.IsPixAPI(provider) || services.IsXais(provider))
	if err != nil {
		return err
	}
	dlMs := time.Since(dlStart).Milliseconds()
	saveStart := time.Now()
	path, err := rc.Storage.SaveStylizedImageBytes(resource.ProjectID, resource.ID, data)
	if err != nil {
		return err
	}
	saveMs := time.Since(saveStart).Milliseconds()
	resource.StylizedImagePath = path
	if err := rc.DB.Save(resource).Error; err != nil {
		return err
	}
	log.Printf(
		"stylize timing resource=%d provider=%s model=%s resolve=%dms generate=%dms download=%dms save=%dms total=%dms bytes=%d source=%s",
		resource.ID, provider.Slug, model.ModelID, resolveMs, genMs, dlMs, saveMs,
		time.Since(totalStart).Milliseconds(), len(data), shortenURLForLog(sourceURL),
	)
	return nil
}

func shortenURLForLog(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "data:") {
		return fmt.Sprintf("data:(%d chars)", len(u))
	}
	if len(u) > 96 {
		return u[:96] + "…"
	}
	return u
}

func (rc *ResourceController) Update(c *gin.Context) {
	resource, ok := rc.findIncludingTrash(c)
	if !ok {
		return
	}
	var input struct {
		Name                 string  `json:"name"`
		Description          string  `json:"description"`
		Remark               string  `json:"remark"`
		VoicePrompt          *string `json:"voicePrompt"`
		GenPrompt            *string `json:"genPrompt"`
		SceneGridShapeLegend *string `json:"sceneGridShapeLegend"`
		ImageData            string  `json:"imageData"`
	}
	if c.ShouldBindJSON(&input) != nil || strings.TrimSpace(input.Name) == "" {
		fail(c, 400, "请求格式错误")
		return
	}
	resource.Name = strings.TrimSpace(input.Name)
	resource.Description = strings.TrimSpace(input.Description)
	resource.Remark = strings.TrimSpace(input.Remark)
	if input.VoicePrompt != nil {
		resource.VoicePrompt = strings.TrimSpace(*input.VoicePrompt)
	}
	if input.GenPrompt != nil {
		resource.GenPrompt = strings.TrimSpace(*input.GenPrompt)
	}
	if input.SceneGridShapeLegend != nil {
		resource.SceneGridShapeLegend = strings.TrimSpace(*input.SceneGridShapeLegend)
	}
	if strings.TrimSpace(input.ImageData) != "" {
		path, err := rc.Storage.SaveResourceImage(resource.ProjectID, resource.ID, input.ImageData)
		if err != nil {
			fail(c, 400, err.Error())
			return
		}
		resource.ImagePath = path
	}
	if err := rc.DB.Save(&resource).Error; err != nil {
		fail(c, 500, "保存资源失败")
		return
	}
	fillResourceURLs(&resource, rc.Storage)
	c.JSON(200, resource)
}

func (rc *ResourceController) Delete(c *gin.Context) {
	resource, ok := rc.find(c)
	if !ok {
		return
	}
	// Soft-delete only — keep files so trash / restore still works.
	if err := rc.DB.Transaction(func(tx *gorm.DB) error {
		if resource.IsGroupPrimary {
			if err := tx.Model(&resource).Update("is_group_primary", false).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&resource).Error
	}); err != nil {
		fail(c, 503, "删除资源失败")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (rc *ResourceController) find(c *gin.Context) (models.Resource, bool) {
	var resource models.Resource
	if err := rc.DB.First(&resource, c.Param("id")).Error; err != nil {
		fail(c, 404, "资源不存在")
		return resource, false
	}
	return resource, true
}

func (rc *ResourceController) persistCandidateImages(projectID uint, resType, name, description string, meta candidatePersistMeta, urls []string) ([]models.Resource, error) {
	return rc.persistCandidateImagesWithProgress(projectID, resType, name, description, urls, meta, nil)
}

func (rc *ResourceController) resolveReferenceImages(provider models.AIProvider, projectID uint, imageDataList []string, resourceRefs []imageGenResourceRef, resourceIDs []uint, legacyImageData string, legacyResourceID uint) ([]string, error) {
	refs := make([]string, 0, len(imageDataList)+len(resourceRefs)+len(resourceIDs)+2)
	for _, data := range imageDataList {
		if strings.TrimSpace(data) == "" {
			continue
		}
		img, err := rc.resolveReferenceImage(provider, projectID, strings.TrimSpace(data), 0, "")
		if err != nil {
			return nil, err
		}
		if img != "" {
			refs = append(refs, img)
		}
	}
	for _, ref := range resourceRefs {
		if ref.ID == 0 {
			continue
		}
		img, err := rc.resolveReferenceImage(provider, projectID, "", ref.ID, ref.Variant)
		if err != nil {
			return nil, err
		}
		if img != "" {
			refs = append(refs, img)
		}
	}
	for _, id := range resourceIDs {
		img, err := rc.resolveReferenceImage(provider, projectID, "", id, "")
		if err != nil {
			return nil, err
		}
		if img != "" {
			refs = append(refs, img)
		}
	}
	if len(refs) == 0 {
		img, err := rc.resolveReferenceImage(provider, projectID, legacyImageData, legacyResourceID, "")
		if err != nil {
			return nil, err
		}
		if img != "" {
			refs = append(refs, img)
		}
	}
	return refs, nil
}

func resourceRefImagePath(resource models.Resource, variant string) string {
	variant = strings.TrimSpace(variant)
	if variant == "original" {
		if resource.ImagePath != "" {
			return resource.ImagePath
		}
		return resource.StylizedImagePath
	}
	if variant == "stylized" || variant == "" {
		if resource.StylizedImagePath != "" {
			return resource.StylizedImagePath
		}
		return resource.ImagePath
	}
	if resource.ImagePath != "" {
		return resource.ImagePath
	}
	return resource.StylizedImagePath
}

func (rc *ResourceController) resolveReferenceImage(provider models.AIProvider, projectID uint, imageData string, resourceID uint, variant string) (string, error) {
	if strings.TrimSpace(imageData) != "" {
		data := strings.TrimSpace(imageData)
		if strings.HasPrefix(data, "http://") || strings.HasPrefix(data, "https://") {
			return data, nil
		}
		if services.IsPixAPI(provider) {
			return rc.publishPixAPIReference(projectID, data, "", 0)
		}
		// Xais accepts inline base64 — skip public URL publish.
		if services.IsXais(provider) {
			return data, nil
		}
		// Prefer a public URL so Ark pulls the image instead of receiving multi‑MB base64
		// (large payloads are a common cause of Client.Timeout awaiting headers).
		if url, err := rc.publishArkReference(projectID, data); err == nil && url != "" {
			return url, nil
		}
		return data, nil
	}
	if resourceID == 0 {
		return "", nil
	}
	var resource models.Resource
	// Unscoped: soft-deleted candidates (未选用的「候选N」) often remain as genRefs / drafts with
	// valid image files; excluding them caused false "参考资源不存在" on regenerate.
	if err := rc.DB.Unscoped().Where("id = ? AND project_id = ?", resourceID, projectID).First(&resource).Error; err != nil {
		return "", fmt.Errorf("参考资源不存在（#%d），可能已彻底删除，请重新选择参考图", resourceID)
	}
	if resource.Type == "video" {
		return "", fmt.Errorf("视频资源不能作为参考图")
	}
	path := resourceRefImagePath(resource, variant)
	if path == "" {
		return "", fmt.Errorf("所选资源没有可用图片（#%d）", resourceID)
	}
	if services.IsPixAPI(provider) {
		return rc.publishPixAPIReference(projectID, "", path, resource.ID)
	}
	if services.IsXais(provider) {
		return rc.Storage.ImageDataURL(path)
	}
	if url := rc.arkPublicRefURL(path); url != "" {
		return url, nil
	}
	return rc.Storage.ImageDataURL(path)
}

// arkPublicRefURL returns an http(s) URL Ark can fetch, or empty if unavailable.
func (rc *ResourceController) arkPublicRefURL(filePath string) string {
	if filePath == "" || rc.Storage == nil {
		return ""
	}
	if rc.Storage.COSEnabled() {
		return rc.Storage.COS.PublicURL(rc.Storage.ObjectKey(filePath))
	}
	if rc.PublicBaseURL != "" {
		key := rc.Storage.ObjectKey(filePath)
		if key == "" {
			return ""
		}
		return services.AbsolutePublicURL(rc.PublicBaseURL, "/api/uploads/"+key)
	}
	return ""
}

// publishArkReference uploads paste/upload base64 refs to COS (or local public path) and returns a URL.
func (rc *ResourceController) publishArkReference(projectID uint, imageData string) (string, error) {
	if rc.Storage == nil {
		return "", fmt.Errorf("storage unavailable")
	}
	if !rc.Storage.COSEnabled() && rc.PublicBaseURL == "" {
		return "", fmt.Errorf("no public URL backend")
	}
	rel, err := rc.Storage.SaveTempReferenceImage(projectID, imageData)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "http://") || strings.HasPrefix(rel, "https://") {
		return rel, nil
	}
	if rc.PublicBaseURL != "" {
		return services.AbsolutePublicURL(rc.PublicBaseURL, rel), nil
	}
	return "", fmt.Errorf("no public URL for temp ref")
}

func (rc *ResourceController) publishPixAPIReference(projectID uint, imageData, filePath string, resourceID uint) (string, error) {
	relayOK := strings.TrimSpace(rc.PixRefRelay) != ""
	if err := services.PixAPIReferenceURLError(rc.PublicBaseURL, rc.TOS, rc.Storage != nil && rc.Storage.COSEnabled(), relayOK); err != nil {
		return "", err
	}

	// Resolve an origin URL first (COS / app / TOS), then rewrite through Tokyo so PixAPI
	// pulls from the overseas hop (fast) instead of Shanghai COS / Aliyun directly (slow).
	var origin string
	var err error
	if filePath != "" {
		origin, err = rc.originPublicImageURL(projectID, resourceID, filePath)
	} else if strings.TrimSpace(imageData) != "" {
		origin, err = rc.originPublicImageDataURL(projectID, imageData)
	}
	if err != nil {
		return "", err
	}
	if origin == "" {
		return "", fmt.Errorf("PixAPI 参考图发布失败：未配置可用的参考图地址")
	}
	wrapped := services.WrapPixAPIRefURL(origin, rc.PixRefRelay, rc.Storage)
	if wrapped != origin {
		log.Printf("pixapi ref via tokyo relay: %s -> %s", services.TruncateURLForLog(origin), services.TruncateURLForLog(wrapped))
	}
	return wrapped, nil
}

func (rc *ResourceController) originPublicImageURL(projectID, resourceID uint, filePath string) (string, error) {
	if rc.Storage != nil && rc.Storage.COSEnabled() {
		if key := rc.Storage.ObjectKey(filePath); key != "" {
			return rc.Storage.COS.PublicURL(key), nil
		}
	}
	if services.CanUseLocalPixAPIRef(rc.PublicBaseURL) {
		if key := rc.Storage.ObjectKey(filePath); key != "" {
			return services.AbsolutePublicURL(rc.PublicBaseURL, "/api/uploads/"+key), nil
		}
		rel := rc.Storage.ResourcePublicURL(projectID, resourceID, filePath)
		if strings.HasPrefix(rel, "http://") || strings.HasPrefix(rel, "https://") {
			return rel, nil
		}
		return services.AbsolutePublicURL(rc.PublicBaseURL, rel), nil
	}
	if rc.TOS != nil && rc.TOS.Enabled() {
		raw, ext, err := rc.Storage.ImageBytes(filePath)
		if err != nil {
			return "", err
		}
		return rc.TOS.UploadPixAPIRef(projectID, raw, ext)
	}
	return "", fmt.Errorf("无法发布参考图：请配置 COS、PUBLIC_BASE_URL 或 TOS")
}

func (rc *ResourceController) originPublicImageDataURL(projectID uint, imageData string) (string, error) {
	if rc.Storage != nil && rc.Storage.COSEnabled() {
		rel, err := rc.Storage.SaveTempReferenceImage(projectID, imageData)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(rel, "http://") || strings.HasPrefix(rel, "https://") {
			return rel, nil
		}
		key := strings.TrimPrefix(rel, "/api/uploads/")
		if key != "" && key != rel {
			return rc.Storage.COS.PublicURL(key), nil
		}
	}
	if services.CanUseLocalPixAPIRef(rc.PublicBaseURL) {
		rel, err := rc.Storage.SaveTempReferenceImage(projectID, imageData)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(rel, "http://") || strings.HasPrefix(rel, "https://") {
			return rel, nil
		}
		return services.AbsolutePublicURL(rc.PublicBaseURL, rel), nil
	}
	if rc.TOS != nil && rc.TOS.Enabled() {
		raw, ext, err := services.DecodeImageData(imageData)
		if err != nil {
			return "", err
		}
		return rc.TOS.UploadPixAPIRef(projectID, raw, ext)
	}
	return "", fmt.Errorf("无法发布参考图：请配置 COS、PUBLIC_BASE_URL 或 TOS")
}
