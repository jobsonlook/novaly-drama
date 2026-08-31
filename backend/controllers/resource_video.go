package controllers

import (
	"fmt"
	"log"
	"os"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"
	"novaly/backend/services/crew"

	"gorm.io/gorm"
)

type VideoGenMeta struct {
	Script       string
	VisualStyle  string
	ProjectStyle string
	ModelName    string
	ModelID      string
	ProviderName string
}

func buildVideoGenMeta(db *gorm.DB, shot models.Shot, projectID uint, source string) VideoGenMeta {
	meta := VideoGenMeta{
		Script:       shot.Script,
		VisualStyle:  shot.VisualStyle,
		ProjectStyle: "",
		ModelName:    "",
		ModelID:      "",
		ProviderName: "",
	}
	var project models.Project
	if err := db.First(&project, projectID).Error; err == nil {
		meta.ProjectStyle = crew.VideoLookPack(project, "")
		if meta.ProjectStyle == "" {
			meta.ProjectStyle = project.Style
		}
	}
	if source == "upload" {
		meta.ModelName = "本地上传"
		return meta
	}
	var model models.AIModel
	if shot.VideoModelID != nil {
		_ = db.First(&model, *shot.VideoModelID).Error
	} else {
		_ = db.Where("capability = ? AND enabled = ? AND is_default = ?", "video", true, true).First(&model).Error
	}
	if model.ID != 0 {
		meta.ModelName = model.Name
		meta.ModelID = model.ModelID
		var provider models.AIProvider
		if db.First(&provider, model.ProviderID).Error == nil {
			meta.ProviderName = provider.Name
		}
	}
	return meta
}

func createVideoResource(db *gorm.DB, storage *services.Storage, projectID uint, shot models.Shot, videoData []byte, source, ext string, meta *VideoGenMeta) (models.Resource, error) {
	return createVideoResourceFrom(db, storage, projectID, shot, videoData, source, ext, meta, "")
}

func createVideoResourceFrom(db *gorm.DB, storage *services.Storage, projectID uint, shot models.Shot, videoData []byte, source, ext string, meta *VideoGenMeta, copyFromLocal string) (models.Resource, error) {
	if meta == nil {
		m := buildVideoGenMeta(db, shot, projectID, source)
		meta = &m
	}
	var episode models.Episode
	if err := db.First(&episode, shot.EpisodeID).Error; err != nil {
		return models.Resource{}, err
	}
	var shots []models.Shot
	db.Where("episode_id = ?", episode.ID).Order("sort_order asc, id asc").Find(&shots)
	shotIndex := 0
	for i, s := range shots {
		if s.ID == shot.ID {
			shotIndex = i
			break
		}
	}
	desc := strings.TrimSpace(meta.Script)
	if len(desc) > 200 {
		desc = desc[:200] + "…"
	}
	duration := shot.Duration
	if duration <= 0 {
		duration = 10
	}
	resolution := shot.Resolution
	if resolution == "" {
		resolution = "720p"
	}
	shotID := shot.ID
	if source == "" {
		source = "ai"
	}
	if ext == "" {
		ext = "mp4"
	}
	resource := models.Resource{
		ProjectID:       projectID,
		Type:            "video",
		Source:          source,
		Name:            videoResourceName(db, shotID, shotIndex),
		Description:     desc,
		ShotID:          &shotID,
		Duration:        duration,
		Resolution:      resolution,
		GenScript:       meta.Script,
		GenVisualStyle:  meta.VisualStyle,
		GenProjectStyle: meta.ProjectStyle,
		GenModelName:    meta.ModelName,
		GenModelID:      meta.ModelID,
		GenProviderName: meta.ProviderName,
	}
	if err := db.Create(&resource).Error; err != nil {
		return models.Resource{}, err
	}
	var path string
	var err error
	if copyFromLocal != "" {
		path, err = storage.SaveResourceVideoCopyFrom(projectID, resource.ID, videoData, ext, copyFromLocal)
	} else {
		path, err = storage.SaveResourceVideoBytes(projectID, resource.ID, videoData, ext)
	}
	if err != nil {
		// Incomplete rows must not linger in trash.
		db.Unscoped().Delete(&resource)
		return models.Resource{}, err
	}
	resource.VideoPath = path
	if err = db.Save(&resource).Error; err != nil {
		return models.Resource{}, err
	}
	fillResourceURLs(&resource, storage)
	return resource, nil
}

func abortIncompleteResource(db *gorm.DB, resource *models.Resource) {
	if resource == nil || resource.ID == 0 {
		return
	}
	_ = db.Unscoped().Delete(resource).Error
}

// createVideoResourceFromCOS creates a video resource by copying an existing COS object (no re-download).
func createVideoResourceFromCOS(db *gorm.DB, storage *services.Storage, projectID uint, shot models.Shot, source, ext string, meta *VideoGenMeta, copyFromLocal string) (models.Resource, error) {
	if meta == nil {
		m := buildVideoGenMeta(db, shot, projectID, source)
		meta = &m
	}
	var episode models.Episode
	if err := db.First(&episode, shot.EpisodeID).Error; err != nil {
		return models.Resource{}, err
	}
	var shots []models.Shot
	db.Where("episode_id = ?", episode.ID).Order("sort_order asc, id asc").Find(&shots)
	shotIndex := 0
	for i, s := range shots {
		if s.ID == shot.ID {
			shotIndex = i
			break
		}
	}
	desc := strings.TrimSpace(meta.Script)
	if len(desc) > 200 {
		desc = desc[:200] + "…"
	}
	duration := shot.Duration
	if duration <= 0 {
		duration = 10
	}
	resolution := shot.Resolution
	if resolution == "" {
		resolution = "720p"
	}
	shotID := shot.ID
	if source == "" {
		source = "ai"
	}
	if ext == "" {
		ext = "mp4"
	}
	resource := models.Resource{
		ProjectID:       projectID,
		Type:            "video",
		Source:          source,
		Name:            videoResourceName(db, shotID, shotIndex),
		Description:     desc,
		ShotID:          &shotID,
		Duration:        duration,
		Resolution:      resolution,
		GenScript:       meta.Script,
		GenVisualStyle:  meta.VisualStyle,
		GenProjectStyle: meta.ProjectStyle,
		GenModelName:    meta.ModelName,
		GenModelID:      meta.ModelID,
		GenProviderName: meta.ProviderName,
	}
	if err := db.Create(&resource).Error; err != nil {
		return models.Resource{}, err
	}
	dst := storage.ResourceVideoPath(projectID, resource.ID, ext)
	copied := false
	if storage.COSEnabled() && copyFromLocal != "" {
		if err := storage.COS.Copy(storage.ObjectKey(copyFromLocal), storage.ObjectKey(dst)); err != nil {
			log.Printf("createVideoResourceFromCOS: copy failed (%v), will re-upload", err)
		} else {
			copied = true
			// Bytes stay on COS; do not keep a server-local copy.
			_ = os.Remove(dst)
		}
	}
	if !copied {
		// Fall back to reading the shot/local file so a COS Copy blip doesn't lose the version.
		data, readExt, err := storage.ReadShotVideo(projectID, shot.ID)
		if err != nil && copyFromLocal != "" {
			data, err = storage.ReadFile(copyFromLocal)
			if ext == "" {
				readExt = "mp4"
			} else {
				readExt = ext
			}
		}
		if err != nil || len(data) == 0 {
			abortIncompleteResource(db, &resource)
			if err != nil {
				return models.Resource{}, fmt.Errorf("登记视频资源失败：%w", err)
			}
			return models.Resource{}, fmt.Errorf("登记视频资源失败：视频文件为空")
		}
		if readExt != "" {
			ext = readExt
		}
		path, saveErr := storage.SaveResourceVideoBytes(projectID, resource.ID, data, ext)
		if saveErr != nil {
			abortIncompleteResource(db, &resource)
			return models.Resource{}, saveErr
		}
		dst = path
	}
	resource.VideoPath = dst
	if err := db.Save(&resource).Error; err != nil {
		return models.Resource{}, err
	}
	fillResourceURLs(&resource, storage)
	return resource, nil
}
