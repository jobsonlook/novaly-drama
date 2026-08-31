package controllers

import (
	"fmt"
	"regexp"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var candidateNameRE = regexp.MustCompile(`^(.+?)\s*·\s*候选(\d+)$`)

func parseCandidateBase(name string) (string, bool) {
	m := candidateNameRE.FindStringSubmatch(strings.TrimSpace(name))
	if len(m) < 2 {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

func resourceBaseName(name string) string {
	name = strings.TrimSpace(name)
	if base, ok := parseCandidateBase(name); ok {
		return base
	}
	return name
}

func resourceParentKey(r models.Resource) uint {
	if r.ParentID == nil {
		return 0
	}
	return *r.ParentID
}

func resourceLibraryGroupKey(r models.Resource) string {
	base := resourceBaseName(r.Name)
	if base == "" {
		return ""
	}
	if r.Type != "character" && r.Type != "scene" && r.Type != "prop" {
		if _, ok := parseCandidateBase(r.Name); !ok {
			return ""
		}
	}
	return fmt.Sprintf("%s:%d:%s", r.Type, resourceParentKey(r), strings.ToLower(base))
}

func libraryGenFamily(r models.Resource) string {
	switch strings.TrimSpace(r.GenType) {
	case "positioning", "positioning_skeleton", "scene_grid", "motion_grid", "motion_grid_cell", "scene_grid_cell", "scene_reverse", "scene_reverse_skeleton", "scene_panorama":
		return r.GenType
	default:
		return r.Type
	}
}

func sameLibraryGroup(a, b models.Resource) bool {
	if resourceLibraryGroupKey(a) == "" || resourceLibraryGroupKey(a) != resourceLibraryGroupKey(b) {
		return false
	}
	return libraryGenFamily(a) == libraryGenFamily(b)
}

func (rc *ResourceController) UsePrimary(c *gin.Context) {
	resource, ok := rc.findIncludingTrash(c)
	if !ok {
		return
	}
	// Trashed rows that are not part of a candidate/extract group: undelete back into the library.
	if resource.DeletedAt.Valid && resourceLibraryGroupKey(resource) == "" {
		kept, err := rc.restoreTrashedResource(resource)
		if err != nil {
			fail(c, 400, err.Error())
			return
		}
		c.JSON(200, gin.H{"resource": kept, "updated": []models.Resource{kept}})
		return
	}
	kept, err := rc.activateCandidatePrimary(resource)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	c.JSON(200, gin.H{"resource": kept, "updated": []models.Resource{kept}})
}

func (rc *ResourceController) restoreTrashedResource(resource models.Resource) (models.Resource, error) {
	if !resource.DeletedAt.Valid {
		return resource, fmt.Errorf("资源不在回收站中")
	}
	if err := rc.DB.Unscoped().Model(&resource).Updates(map[string]any{
		"deleted_at": nil,
	}).Error; err != nil {
		return resource, fmt.Errorf("恢复资源失败")
	}
	if err := rc.DB.Unscoped().First(&resource, resource.ID).Error; err != nil {
		return resource, fmt.Errorf("恢复后读取资源失败")
	}
	if err := repairVideoResourceFile(rc.DB, rc.Storage, &resource); err != nil {
		return resource, err
	}
	// Point the shot at this restored version when nothing else is marked active.
	if resource.Type == "video" && resource.ShotID != nil {
		var shot models.Shot
		if err := rc.DB.First(&shot, *resource.ShotID).Error; err == nil {
			if shot.ActiveVideoResourceID == nil || *shot.ActiveVideoResourceID == 0 {
				id := resource.ID
				_ = rc.DB.Model(&shot).Update("active_video_resource_id", id).Error
			}
		}
	}
	fillResourceURLs(&resource, rc.Storage)
	return resource, nil
}

// repairVideoResourceFile re-copies from the shot video when a trash entry has no usable file
// (e.g. generate registered the row then failed mid-upload and soft-deleted it).
func repairVideoResourceFile(db *gorm.DB, storage *services.Storage, resource *models.Resource) error {
	if resource.Type != "video" || resource.ShotID == nil {
		return nil
	}
	if resource.VideoPath != "" {
		if _, err := storage.ReadFile(resource.VideoPath); err == nil {
			return nil
		}
	}
	data, ext, err := storage.ReadShotVideo(resource.ProjectID, *resource.ShotID)
	if err != nil || len(data) == 0 {
		return nil // restore anyway; preview may stay empty
	}
	path, err := storage.SaveResourceVideoBytes(resource.ProjectID, resource.ID, data, ext)
	if err != nil {
		return fmt.Errorf("恢复视频文件失败：%w", err)
	}
	resource.VideoPath = path
	if err := db.Model(resource).Update("video_path", path).Error; err != nil {
		return fmt.Errorf("恢复视频路径失败：%w", err)
	}
	return nil
}

func (rc *ResourceController) findCanonicalResource(projectID uint, resType, name string, parentID *uint) (models.Resource, bool) {
	base := resourceBaseName(name)
	if base == "" || projectID == 0 {
		return models.Resource{}, false
	}
	q := rc.DB.Where("project_id = ? AND type = ?", projectID, resType)
	if parentID != nil && *parentID > 0 {
		q = q.Where("parent_id = ?", *parentID)
	} else {
		q = q.Where("parent_id IS NULL")
	}
	var items []models.Resource
	if err := q.Order("id asc").Find(&items).Error; err != nil {
		return models.Resource{}, false
	}
	for _, r := range items {
		if _, isCand := parseCandidateBase(r.Name); isCand {
			continue
		}
		switch strings.TrimSpace(r.GenType) {
		case "positioning", "positioning_skeleton", "scene_grid", "motion_grid", "motion_grid_cell", "scene_grid_cell", "scene_reverse", "scene_reverse_skeleton", "scene_panorama":
			continue
		}
		if strings.EqualFold(strings.TrimSpace(r.Name), base) {
			return r, true
		}
	}
	return models.Resource{}, false
}

func (rc *ResourceController) validCreateParent(projectID, parentID uint) bool {
	if parentID == 0 || projectID == 0 {
		return false
	}
	var parent models.Resource
	if err := rc.DB.Select("id, project_id, parent_id, type").First(&parent, parentID).Error; err != nil {
		return false
	}
	if parent.ProjectID != projectID || parentIDOf(parent) > 0 {
		return false
	}
	switch parent.Type {
	case "character", "scene":
		return true
	default:
		return false
	}
}

func (rc *ResourceController) resolveCreateParentID(projectID, parentID uint) *uint {
	if !rc.validCreateParent(projectID, parentID) {
		return nil
	}
	id := parentID
	return &id
}

func (rc *ResourceController) resolveFillResource(projectID uint, resType, name string, meta candidatePersistMeta) (models.Resource, bool) {
	if meta.CandidateOnly {
		return models.Resource{}, false
	}
	if meta.FillResourceID > 0 {
		var target models.Resource
		if err := rc.DB.First(&target, "id = ? AND project_id = ?", meta.FillResourceID, projectID).Error; err == nil {
			if _, isCand := parseCandidateBase(target.Name); isCand {
				if canonical, ok := rc.findCanonicalResource(projectID, target.Type, target.Name, target.ParentID); ok {
					return canonical, true
				}
			}
			return target, true
		}
	}
	gen := strings.TrimSpace(meta.GenType)
	if gen == "positioning" || gen == "positioning_skeleton" || gen == "scene_grid" || gen == "motion_grid" || gen == "motion_grid_cell" || gen == "scene_grid_cell" || gen == "scene_reverse" || gen == "scene_reverse_skeleton" || gen == "scene_panorama" {
		return models.Resource{}, false
	}
	if resType != "character" && resType != "scene" && resType != "prop" {
		return models.Resource{}, false
	}
	return rc.findCanonicalResource(projectID, resType, name, meta.ParentID)
}

// rehostMergedImages copies image bytes from a candidate row onto the canonical
// resource's own storage keys (projects/{pid}/resources/{canonicalId}.jpg).
// Without this, image_path may point at {candidateId}.jpg while PublicURL uses
// {canonicalId}.jpg — the DB looks updated but COS returns 404.
func (rc *ResourceController) rehostMergedImages(canonical, src models.Resource) (models.Resource, error) {
	out := src
	if canonical.ID == 0 || src.ID == 0 || canonical.ID == src.ID {
		return out, nil
	}
	if strings.TrimSpace(src.ImagePath) != "" {
		data, err := rc.Storage.ReadFile(src.ImagePath)
		if err != nil {
			return out, err
		}
		path, err := rc.Storage.SaveResourceImageBytes(canonical.ProjectID, canonical.ID, data)
		if err != nil {
			return out, err
		}
		out.ImagePath = path
	}
	if strings.TrimSpace(src.StylizedImagePath) != "" {
		data, err := rc.Storage.ReadFile(src.StylizedImagePath)
		if err != nil {
			return out, err
		}
		path, err := rc.Storage.SaveStylizedImageBytes(canonical.ProjectID, canonical.ID, data)
		if err != nil {
			return out, err
		}
		out.StylizedImagePath = path
	}
	return out, nil
}

func mergeGeneratedImageUpdates(dst, src models.Resource) map[string]any {
	updates := map[string]any{}
	if strings.TrimSpace(src.ImagePath) != "" {
		updates["image_path"] = src.ImagePath
	}
	if strings.TrimSpace(src.StylizedImagePath) != "" {
		updates["stylized_image_path"] = src.StylizedImagePath
	}
	if strings.TrimSpace(src.GenPrompt) != "" && strings.TrimSpace(dst.GenPrompt) == "" {
		updates["gen_prompt"] = src.GenPrompt
	}
	if strings.TrimSpace(src.GenType) != "" {
		updates["gen_type"] = src.GenType
	}
	if strings.TrimSpace(src.GenRefsJSON) != "" {
		updates["gen_refs_json"] = src.GenRefsJSON
	}
	if strings.TrimSpace(src.Source) != "" {
		updates["source"] = src.Source
	}
	return updates
}

func pickCanonicalInGroup(keep models.Resource, group []models.Resource) models.Resource {
	var liveBase, anyBase *models.Resource
	for i := range group {
		item := &group[i]
		if _, isCand := parseCandidateBase(item.Name); isCand {
			continue
		}
		if anyBase == nil {
			anyBase = item
		}
		if !item.DeletedAt.Valid && liveBase == nil {
			liveBase = item
		}
	}
	if liveBase != nil {
		return *liveBase
	}
	if anyBase != nil {
		return *anyBase
	}
	return keep
}

func (rc *ResourceController) activateCandidatePrimary(keep models.Resource) (models.Resource, error) {
	if keep.Type != "character" && keep.Type != "scene" && keep.Type != "prop" {
		if _, isCand := parseCandidateBase(keep.Name); !isCand {
			return keep, nil
		}
	}
	groupKey := resourceLibraryGroupKey(keep)
	if groupKey == "" {
		return keep, nil
	}
	var siblings []models.Resource
	if err := rc.DB.Unscoped().Where("project_id = ? AND type = ?", keep.ProjectID, keep.Type).Find(&siblings).Error; err != nil {
		return keep, err
	}
	group := make([]models.Resource, 0, len(siblings))
	for i := range siblings {
		if !sameLibraryGroup(keep, siblings[i]) {
			continue
		}
		group = append(group, siblings[i])
	}
	if len(group) == 0 {
		return keep, fmt.Errorf("候选资源不存在")
	}
	canonical := pickCanonicalInGroup(keep, group)
	if keep.ID != canonical.ID {
		merged, err := rc.rehostMergedImages(canonical, keep)
		if err != nil {
			return keep, fmt.Errorf("合并候选图失败：%w", err)
		}
		updates := mergeGeneratedImageUpdates(canonical, merged)
		updates["is_group_primary"] = true
		updates["deleted_at"] = nil
		if err := rc.DB.Unscoped().Model(&canonical).Updates(updates).Error; err != nil {
			return keep, err
		}
	} else {
		updates := map[string]any{
			"is_group_primary": true,
			"deleted_at":       nil,
		}
		if base, ok := parseCandidateBase(canonical.Name); ok {
			updates["name"] = base
		}
		if err := rc.DB.Unscoped().Model(&canonical).Updates(updates).Error; err != nil {
			return keep, err
		}
	}
	if err := rc.DB.Unscoped().First(&canonical, canonical.ID).Error; err != nil {
		return keep, err
	}
	fillResourceURLs(&canonical, rc.Storage)

	for i := range group {
		item := group[i]
		if item.ID == canonical.ID {
			continue
		}
		if item.DeletedAt.Valid {
			if item.IsGroupPrimary {
				if err := rc.DB.Unscoped().Model(&item).Update("is_group_primary", false).Error; err != nil {
					return keep, err
				}
			}
			continue
		}
		if err := rc.DB.Model(&item).Update("is_group_primary", false).Error; err != nil {
			return keep, err
		}
		if err := rc.DB.Delete(&item).Error; err != nil {
			return keep, err
		}
	}
	return canonical, nil
}
