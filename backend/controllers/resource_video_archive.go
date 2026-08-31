package controllers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"

	"gorm.io/gorm"
)

func videoResourceName(db *gorm.DB, shotID uint, shotIndex int) string {
	var count int64
	db.Model(&models.Resource{}).Where("shot_id = ? AND type = ?", shotID, "video").Count(&count)
	base := fmt.Sprintf("分镜%02d", shotIndex+1)
	if count == 0 {
		return base
	}
	return fmt.Sprintf("%s · 版本%d", base, count+1)
}

func isResourceLibraryVideoPath(p string) bool {
	return strings.Contains(p, string(filepath.Separator)+"resources"+string(filepath.Separator))
}

func shotVideoAlreadyArchived(db *gorm.DB, storage *services.Storage, projectID, shotID uint, size int64) bool {
	if size <= 0 {
		return false
	}
	var resources []models.Resource
	db.Where("project_id = ? AND shot_id = ? AND type = ?", projectID, shotID, "video").
		Order("id desc").Find(&resources)
	for _, r := range resources {
		if !isResourceLibraryVideoPath(r.VideoPath) {
			continue
		}
		sz, err := storage.FileSize(r.VideoPath)
		if err == nil && sz == size {
			return true
		}
	}
	return false
}

// archiveShotVideoBeforeReplace copies the current shot video into the resource library
// before it is overwritten. Callers must invoke this BEFORE replacing the shot object.
// Prefer COS server-side Copy; never download the full video into the API process.
func archiveShotVideoBeforeReplace(db *gorm.DB, storage *services.Storage, projectID uint, shot models.Shot) (models.Resource, bool, error) {
	if shot.ActiveVideoResourceID != nil && *shot.ActiveVideoResourceID != 0 {
		var existing models.Resource
		if err := db.First(&existing, *shot.ActiveVideoResourceID).Error; err == nil &&
			existing.Type == "video" && isResourceLibraryVideoPath(existing.VideoPath) {
			return models.Resource{}, false, nil
		}
	}

	path, ext, ok := storage.FindShotVideo(projectID, shot.ID)
	if !ok {
		return models.Resource{}, false, nil
	}
	size, err := storage.FileSize(path)
	if err != nil || size <= 0 {
		return models.Resource{}, false, nil
	}
	if shotVideoAlreadyArchived(db, storage, projectID, shot.ID, size) {
		return models.Resource{}, false, nil
	}

	source := "upload"
	if shot.VideoTaskID != "" {
		source = "ai"
	}
	if storage.COSEnabled() {
		resource, err := createVideoResourceFromCOS(db, storage, projectID, shot, source, ext, nil, path)
		if err != nil {
			return models.Resource{}, false, err
		}
		return resource, true, nil
	}

	data, readExt, err := storage.ReadShotVideo(projectID, shot.ID)
	if err != nil {
		return models.Resource{}, false, nil
	}
	resource, err := createVideoResource(db, storage, projectID, shot, data, source, readExt, nil)
	if err != nil {
		return models.Resource{}, false, err
	}
	return resource, true, nil
}

func ensureResourceVideoCopy(db *gorm.DB, storage *services.Storage, r *models.Resource) {
	if r.Type != "video" || r.VideoPath == "" {
		return
	}
	if isResourceLibraryVideoPath(r.VideoPath) {
		return
	}
	if !strings.Contains(r.VideoPath, string(filepath.Separator)+"videos"+string(filepath.Separator)) {
		return
	}
	ext := "mp4"
	if i := strings.LastIndex(r.VideoPath, "."); i >= 0 {
		ext = r.VideoPath[i+1:]
	}
	dst := storage.ResourceVideoPath(r.ProjectID, r.ID, ext)
	if storage.COSEnabled() {
		if err := storage.COS.Copy(storage.ObjectKey(r.VideoPath), storage.ObjectKey(dst)); err == nil {
			r.VideoPath = dst
			_ = os.Remove(dst)
			_ = db.Save(r).Error
			return
		}
	}
	data, err := storage.ReadFile(r.VideoPath)
	if err != nil {
		return
	}
	path, err := storage.SaveResourceVideoBytes(r.ProjectID, r.ID, data, ext)
	if err != nil {
		return
	}
	r.VideoPath = path
	_ = db.Save(r).Error
}
