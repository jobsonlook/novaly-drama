package controllers

import (
	"path/filepath"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DirectUploadController struct {
	DB      *gorm.DB
	Storage *services.Storage
}

func (dc *DirectUploadController) Status(c *gin.Context) {
	c.JSON(200, gin.H{
		"enabled":    dc.Storage.COSEnabled(),
		"accelerate": dc.Storage.COSEnabled() && dc.Storage.COS != nil && dc.Storage.COS.UsingAccelerate(),
	})
}

func (dc *DirectUploadController) InitMultipart(c *gin.Context) {
	if !dc.requireCOS(c) {
		return
	}
	var input struct {
		Key         string `json:"key"`
		ContentType string `json:"contentType"`
	}
	if c.ShouldBindJSON(&input) != nil || strings.TrimSpace(input.Key) == "" {
		fail(c, 400, "缺少 key")
		return
	}
	key := strings.TrimPrefix(input.Key, "/")
	if !strings.HasPrefix(key, "projects/") {
		fail(c, 400, "非法 key")
		return
	}
	uploadID, err := dc.Storage.COS.InitiateMultipart(key, input.ContentType)
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	c.JSON(200, gin.H{"uploadId": uploadID, "key": key})
}

func (dc *DirectUploadController) SignMultipartParts(c *gin.Context) {
	if !dc.requireCOS(c) {
		return
	}
	var input struct {
		Key         string `json:"key"`
		UploadID    string `json:"uploadId"`
		PartNumbers []int  `json:"partNumbers"`
	}
	if c.ShouldBindJSON(&input) != nil || input.Key == "" || input.UploadID == "" || len(input.PartNumbers) == 0 {
		fail(c, 400, "参数不完整")
		return
	}
	if len(input.PartNumbers) > 100 {
		fail(c, 400, "单次分片过多")
		return
	}
	key := strings.TrimPrefix(input.Key, "/")
	parts := make([]gin.H, 0, len(input.PartNumbers))
	for _, n := range input.PartNumbers {
		url, err := dc.Storage.COS.PresignPart(key, input.UploadID, n, 0)
		if err != nil {
			fail(c, 500, err.Error())
			return
		}
		parts = append(parts, gin.H{"partNumber": n, "uploadUrl": url})
	}
	c.JSON(200, gin.H{"parts": parts})
}

func (dc *DirectUploadController) CompleteMultipart(c *gin.Context) {
	if !dc.requireCOS(c) {
		return
	}
	var input struct {
		Key      string `json:"key"`
		UploadID string `json:"uploadId"`
		Parts    []struct {
			PartNumber int    `json:"partNumber"`
			ETag       string `json:"etag"`
		} `json:"parts"`
	}
	if c.ShouldBindJSON(&input) != nil || input.Key == "" || input.UploadID == "" || len(input.Parts) == 0 {
		fail(c, 400, "参数不完整")
		return
	}
	parts := make([]services.MultipartPart, 0, len(input.Parts))
	for _, p := range input.Parts {
		parts = append(parts, services.MultipartPart{PartNumber: p.PartNumber, ETag: p.ETag})
	}
	if err := dc.Storage.COS.CompleteMultipart(strings.TrimPrefix(input.Key, "/"), input.UploadID, parts); err != nil {
		fail(c, 500, err.Error())
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (dc *DirectUploadController) AbortMultipart(c *gin.Context) {
	if !dc.requireCOS(c) {
		return
	}
	var input struct {
		Key      string `json:"key"`
		UploadID string `json:"uploadId"`
	}
	_ = c.ShouldBindJSON(&input)
	dc.Storage.COS.AbortMultipart(strings.TrimPrefix(input.Key, "/"), input.UploadID)
	c.JSON(200, gin.H{"ok": true})
}

func (dc *DirectUploadController) requireCOS(c *gin.Context) bool {
	if !dc.Storage.COSEnabled() {
		fail(c, 503, "未配置腾讯云 COS，无法直传")
		return false
	}
	return true
}

func imageUploadExt(filename, contentType string) (string, bool) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if ext == "jpeg" {
		ext = "jpg"
	}
	switch ext {
	case "jpg", "png", "webp", "gif":
		return ext, true
	}
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "png"):
		return "png", true
	case strings.Contains(ct, "webp"):
		return "webp", true
	case strings.Contains(ct, "gif"):
		return "gif", true
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		return "jpg", true
	}
	return "", false
}

func normalizeContentType(ext, contentType string) string {
	ct := strings.TrimSpace(contentType)
	if ct != "" && ct != "application/octet-stream" {
		return ct
	}
	switch ext {
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "mp4":
		return "video/mp4"
	case "webm":
		return "video/webm"
	case "mov":
		return "video/quicktime"
	case "m4v":
		return "video/x-m4v"
	default:
		return "application/octet-stream"
	}
}

func (dc *DirectUploadController) presignLocal(c *gin.Context, localPath, contentType string) (gin.H, bool) {
	key := dc.Storage.ObjectKey(localPath)
	url, headers, err := dc.Storage.COS.PresignPut(key, contentType, 0)
	if err != nil {
		fail(c, 500, err.Error())
		return nil, false
	}
	return gin.H{
		"uploadUrl": url,
		"headers":   headers,
		"key":       key,
		"path":      localPath,
	}, true
}

// PresignResourceImage creates a resource row and returns a COS PUT URL for the image.
func (dc *DirectUploadController) PresignResourceImage(c *gin.Context) {
	if !dc.requireCOS(c) {
		return
	}
	projectID := parseID(c.Param("id"))
	var input struct {
		Type        string `json:"type"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Filename    string `json:"filename"`
		ContentType string `json:"contentType"`
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
	ext, ok := imageUploadExt(input.Filename, input.ContentType)
	if !ok {
		fail(c, 400, "不支持的图片格式（支持 jpg / png / webp / gif）")
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "参考图"
	}
	var parentID *uint
	if input.ParentID > 0 {
		var parent models.Resource
		if err := dc.DB.Select("id, project_id, parent_id, type").First(&parent, input.ParentID).Error; err == nil {
			if parent.ProjectID == projectID && parentIDOf(parent) == 0 && (parent.Type == "character" || parent.Type == "scene") {
				pid := input.ParentID
				parentID = &pid
			}
		}
	}
	resource := models.Resource{
		ProjectID:   projectID,
		Type:        input.Type,
		Source:      "upload",
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		ParentID:    parentID,
	}
	if err := dc.DB.Create(&resource).Error; err != nil {
		fail(c, 500, "创建资源失败")
		return
	}
	localPath := dc.Storage.ResourceImagePath(projectID, resource.ID, ext)
	ct := normalizeContentType(ext, input.ContentType)
	payload, ok := dc.presignLocal(c, localPath, ct)
	if !ok {
		dc.DB.Delete(&resource)
		return
	}
	payload["resourceId"] = resource.ID
	payload["ext"] = ext
	c.JSON(200, payload)
}

func (dc *DirectUploadController) ConfirmResourceImage(c *gin.Context) {
	if !dc.requireCOS(c) {
		return
	}
	var resource models.Resource
	if err := dc.DB.First(&resource, c.Param("id")).Error; err != nil {
		fail(c, 404, "资源不存在")
		return
	}
	var input struct {
		Ext string `json:"ext"`
		Key string `json:"key"`
	}
	_ = c.ShouldBindJSON(&input)
	ext := strings.TrimPrefix(strings.ToLower(input.Ext), ".")
	if ext == "" {
		ext = "jpg"
	}
	localPath := dc.Storage.ResourceImagePath(resource.ProjectID, resource.ID, ext)
	if input.Key != "" && dc.Storage.ObjectKey(localPath) != strings.TrimPrefix(input.Key, "/") {
		fail(c, 400, "上传 key 不匹配")
		return
	}
	if err := dc.Storage.BindCOSObject(localPath); err != nil {
		fail(c, 400, err.Error())
		return
	}
	resource.ImagePath = localPath
	resource.Source = "upload"
	if err := dc.DB.Save(&resource).Error; err != nil {
		fail(c, 500, "保存资源失败")
		return
	}
	fillResourceURLs(&resource, dc.Storage)
	c.JSON(200, resource)
}

// PresignResourceVideos creates video resource rows and returns COS PUT URLs.
func (dc *DirectUploadController) PresignResourceVideos(c *gin.Context) {
	if !dc.requireCOS(c) {
		return
	}
	projectID := parseID(c.Param("id"))
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Remark      string `json:"remark"`
		Files       []struct {
			Filename    string `json:"filename"`
			ContentType string `json:"contentType"`
		} `json:"files"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.Files) == 0 {
		fail(c, 400, "请选择要上传的视频")
		return
	}
	if len(input.Files) > 20 {
		fail(c, 400, "单次最多上传 20 个视频")
		return
	}
	baseName := strings.TrimSpace(input.Name)
	if baseName == "" {
		baseName = "上传视频"
	}
	description := strings.TrimSpace(input.Description)
	remark := strings.TrimSpace(input.Remark)
	if remark == "" {
		remark = description
	}
	items := make([]gin.H, 0, len(input.Files))
	created := make([]models.Resource, 0, len(input.Files))
	for i, f := range input.Files {
		ext, ok := videoUploadExt(f.Filename)
		if !ok {
			for _, r := range created {
				dc.DB.Delete(&r)
			}
			fail(c, 400, "不支持的视频格式（支持 mp4 / webm / mov / m4v）")
			return
		}
		resource := models.Resource{
			ProjectID:   projectID,
			Type:        "video",
			Source:      "upload",
			Name:        videoUploadName(baseName, f.Filename, i, len(input.Files)),
			Description: description,
			Remark:      remark,
		}
		if err := dc.DB.Create(&resource).Error; err != nil {
			for _, r := range created {
				dc.DB.Delete(&r)
			}
			fail(c, 500, "创建视频资源失败")
			return
		}
		created = append(created, resource)
		localPath := dc.Storage.ResourceVideoPath(projectID, resource.ID, ext)
		ct := normalizeContentType(ext, f.ContentType)
		payload, ok := dc.presignLocal(c, localPath, ct)
		if !ok {
			for _, r := range created {
				dc.DB.Delete(&r)
			}
			return
		}
		payload["resourceId"] = resource.ID
		payload["ext"] = ext
		payload["filename"] = f.Filename
		items = append(items, payload)
	}
	c.JSON(200, gin.H{"items": items})
}

func (dc *DirectUploadController) ConfirmResourceVideo(c *gin.Context) {
	if !dc.requireCOS(c) {
		return
	}
	var resource models.Resource
	if err := dc.DB.First(&resource, c.Param("id")).Error; err != nil {
		fail(c, 404, "资源不存在")
		return
	}
	var input struct {
		Ext string `json:"ext"`
		Key string `json:"key"`
	}
	_ = c.ShouldBindJSON(&input)
	ext := strings.TrimPrefix(strings.ToLower(input.Ext), ".")
	if ext == "" {
		ext = "mp4"
	}
	localPath := dc.Storage.ResourceVideoPath(resource.ProjectID, resource.ID, ext)
	if input.Key != "" && dc.Storage.ObjectKey(localPath) != strings.TrimPrefix(input.Key, "/") {
		fail(c, 400, "上传 key 不匹配")
		return
	}
	if err := dc.Storage.BindCOSObject(localPath); err != nil {
		fail(c, 400, err.Error())
		return
	}
	resource.VideoPath = localPath
	resource.Type = "video"
	resource.Source = "upload"
	if err := dc.DB.Save(&resource).Error; err != nil {
		fail(c, 500, "保存视频资源失败")
		return
	}
	fillResourceURLs(&resource, dc.Storage)
	c.JSON(200, resource)
}

func (dc *DirectUploadController) PresignShotVideo(c *gin.Context) {
	if !dc.requireCOS(c) {
		return
	}
	shot, ok := dc.findShot(c)
	if !ok {
		return
	}
	var episode models.Episode
	if err := dc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}
	var input struct {
		Filename    string `json:"filename"`
		ContentType string `json:"contentType"`
	}
	_ = c.ShouldBindJSON(&input)
	ext, okExt := videoUploadExt(input.Filename)
	if !okExt {
		ct := strings.ToLower(input.ContentType)
		switch {
		case strings.Contains(ct, "webm"):
			ext = "webm"
		case strings.Contains(ct, "quicktime"), strings.Contains(ct, "mov"):
			ext = "mov"
		case strings.Contains(ct, "m4v"):
			ext = "m4v"
		default:
			ext = "mp4"
		}
	}
	localPath := dc.Storage.ShotVideoPath(episode.ProjectID, shot.ID, ext)
	ct := normalizeContentType(ext, input.ContentType)
	payload, ok := dc.presignLocal(c, localPath, ct)
	if !ok {
		return
	}
	payload["ext"] = ext
	payload["shotId"] = shot.ID
	payload["projectId"] = episode.ProjectID
	c.JSON(200, payload)
}

func (dc *DirectUploadController) ConfirmShotVideo(c *gin.Context) {
	if !dc.requireCOS(c) {
		return
	}
	shot, ok := dc.findShot(c)
	if !ok {
		return
	}
	var episode models.Episode
	if err := dc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}
	var input struct {
		Ext string `json:"ext"`
		Key string `json:"key"`
	}
	_ = c.ShouldBindJSON(&input)
	ext := strings.TrimPrefix(strings.ToLower(input.Ext), ".")
	if ext == "" {
		ext = "mp4"
	}
	localPath := dc.Storage.ShotVideoPath(episode.ProjectID, shot.ID, ext)
	if input.Key != "" && dc.Storage.ObjectKey(localPath) != strings.TrimPrefix(input.Key, "/") {
		fail(c, 400, "上传 key 不匹配")
		return
	}
	if err := dc.Storage.BindCOSObject(localPath); err != nil {
		fail(c, 400, err.Error())
		return
	}
	// Direct upload already overwrote the shot COS key before confirm.
	// The previous version (if any) stays in the library via ActiveVideoResourceID —
	// do not archive from the shot path here (that would copy the new file and re-download).
	shot.VideoURL = dc.Storage.PublicURL("videos", episode.ProjectID, shot.ID, ext)
	shot.Status = "done"
	shot.ErrorMessage = ""
	shot.VideoTaskID = ""
	if err := dc.DB.Save(&shot).Error; err != nil {
		fail(c, 500, "保存分镜失败")
		return
	}
	videoResource, err := createVideoResourceFromCOS(dc.DB, dc.Storage, episode.ProjectID, shot, "upload", ext, nil, localPath)
	if err != nil {
		fail(c, 500, "保存视频资源失败："+err.Error())
		return
	}
	shot.ActiveVideoResourceID = &videoResource.ID
	dc.DB.Save(&shot)
	fillShotFields(&shot, dc.Storage)
	c.JSON(200, gin.H{"shot": shot, "videoResource": videoResource})
}

func (dc *DirectUploadController) findShot(c *gin.Context) (models.Shot, bool) {
	var shot models.Shot
	if err := dc.DB.First(&shot, c.Param("id")).Error; err != nil {
		fail(c, 404, "分镜不存在")
		return shot, false
	}
	return shot, true
}
