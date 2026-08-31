package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"novaly/backend/models"
	"novaly/backend/services"

	"github.com/gin-gonic/gin"
)

type imageGenResourceRef struct {
	ID      uint   `json:"id"`
	Variant string `json:"variant"`
}

type imageGenJobInput struct {
	Name             string                `json:"name"`
	Description      string                `json:"description"`
	Count            int                   `json:"count"`
	Quality          string                `json:"quality"`    // legacy: low | medium | high
	Resolution       string                `json:"resolution"` // preferred: 1k | 2k | 4k
	ModelID          uint                  `json:"modelId"`    // optional AIModel DB id; empty → default image model
	ImageDataList    []string              `json:"imageDataList"`
	ResourceRefs     []imageGenResourceRef `json:"resourceRefs"`
	ResourceIDs      []uint                `json:"resourceIds"`
	ImageData        string                `json:"imageData"`
	ResourceID       uint                  `json:"resourceId"`
	ShotID           uint                  `json:"shotId"`
	TargetResourceID uint                  `json:"targetResourceId"`
	ParentID         uint                  `json:"parentId"`
	Revision         string                `json:"revision"`
	PreservePrompt   bool                  `json:"preservePrompt"`
	RawPrompt        bool                  `json:"rawPrompt"`
	SavedPrompt      string                `json:"savedPrompt"`
	CandidateOnly    bool                  `json:"candidateOnly"` // preview only; do not replace canonical before confirmation
}

type imageGenJobResult struct {
	Images    []gin.H           `json:"images"`
	Resources []models.Resource `json:"resources"`
	Prompt    string            `json:"prompt"`
	ProjectID uint              `json:"projectId"`
}

func (rc *ResourceController) GetImageGenerationJob(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	jobID := parseID(c.Param("jobId"))
	rc.failStaleImageGenJobs(projectID)
	var job models.ImageGenerationJob
	if err := rc.DB.Where("id = ? AND project_id = ?", jobID, projectID).First(&job).Error; err != nil {
		fail(c, 404, "生成任务不存在")
		return
	}
	c.JSON(200, rc.imageGenJobResponse(&job))
}

// ListImageGenerationJobs returns all in-flight jobs plus recent finished jobs for the multi-job panel.
func (rc *ResourceController) ListImageGenerationJobs(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	rc.failStaleImageGenJobs(projectID)

	var active []models.ImageGenerationJob
	rc.DB.Where("project_id = ? AND status IN ?", projectID, []string{"pending", "running"}).
		Order("id desc").Find(&active)

	cutoff := time.Now().Add(-2 * time.Hour)
	var recent []models.ImageGenerationJob
	rc.DB.Where("project_id = ? AND dismissed = ? AND status IN ? AND updated_at >= ?",
		projectID, false, []string{"completed", "failed"}, cutoff).
		Order("id desc").Limit(30).Find(&recent)

	seen := map[uint]bool{}
	jobs := make([]models.ImageGenerationJob, 0, len(active)+len(recent))
	for _, j := range active {
		seen[j.ID] = true
		jobs = append(jobs, j)
	}
	for _, j := range recent {
		if seen[j.ID] {
			continue
		}
		jobs = append(jobs, j)
	}

	out := make([]gin.H, 0, len(jobs))
	for i := range jobs {
		out = append(out, rc.imageGenJobResponse(&jobs[i]))
	}
	c.JSON(200, out)
}

// DismissImageGenerationJob hides a finished job from the studio panel across reloads.
func (rc *ResourceController) DismissImageGenerationJob(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	jobID := parseID(c.Param("jobId"))
	var job models.ImageGenerationJob
	if err := rc.DB.Where("id = ? AND project_id = ?", jobID, projectID).First(&job).Error; err != nil {
		fail(c, 404, "生成任务不存在")
		return
	}
	if job.Status == "pending" || job.Status == "running" {
		fail(c, 400, "进行中的任务不能关闭")
		return
	}
	if err := rc.DB.Model(&job).Update("dismissed", true).Error; err != nil {
		fail(c, 500, "关闭生成任务失败")
		return
	}
	c.JSON(200, gin.H{"ok": true, "id": job.ID})
}

func (rc *ResourceController) failStaleImageGenJobs(projectID uint) {
	cutoff := time.Now().Add(-20 * time.Minute)
	msg := "生成任务已中断（超时或服务重启），请重新生成"
	rc.DB.Model(&models.ImageGenerationJob{}).
		Where("project_id = ? AND status IN ? AND updated_at < ?", projectID, []string{"pending", "running"}, cutoff).
		Updates(map[string]any{
			"status":        "failed",
			"message":       msg,
			"error_message": msg,
		})
}

func (rc *ResourceController) imageGenJobResponse(job *models.ImageGenerationJob) gin.H {
	resp := gin.H{
		"id":         job.ID,
		"projectId":  job.ProjectID,
		"type":       job.Type,
		"status":     job.Status,
		"progress":   job.Progress,
		"message":    job.Message,
		"totalCount": job.TotalCount,
		"doneCount":  job.DoneCount,
		"prompt":     job.Prompt,
		"createdAt":  job.CreatedAt,
		"updatedAt":  job.UpdatedAt,
	}
	if job.ErrorMessage != "" {
		resp["error"] = job.ErrorMessage
	}
	if job.InputJSON != "" {
		var input imageGenJobInput
		if err := json.Unmarshal([]byte(job.InputJSON), &input); err == nil {
			// Omit raw base64 uploads from API (too large); library refs are enriched for UI restore.
			resp["input"] = gin.H{
				"name":             input.Name,
				"description":      input.Description,
				"count":            input.Count,
				"quality":          input.Quality,
				"resolution":       input.Resolution,
				"modelId":          input.ModelID,
				"shotId":           input.ShotID,
				"targetResourceId": input.TargetResourceID,
				"resourceRefs":     rc.enrichImageGenResourceRefs(input.ResourceRefs),
				"uploadCount":      len(input.ImageDataList),
			}
		}
	}
	if job.Status == "completed" && job.ResultJSON != "" {
		var result imageGenJobResult
		if err := json.Unmarshal([]byte(job.ResultJSON), &result); err == nil {
			for i := range result.Resources {
				fillResourceFields(&result.Resources[i], rc.DB, rc.Storage)
			}
			resp["images"] = result.Images
			resp["resources"] = result.Resources
			resp["prompt"] = result.Prompt
			resp["projectId"] = result.ProjectID
			// Prefer materialized genRefs from saved resources (includes former uploads).
			if len(result.Resources) > 0 && len(result.Resources[0].GenRefs) > 0 {
				if input, ok := resp["input"].(gin.H); ok {
					input["resourceRefs"] = result.Resources[0].GenRefs
					resp["input"] = input
				}
			}
		}
	}
	return resp
}

func (rc *ResourceController) enrichImageGenResourceRefs(refs []imageGenResourceRef) []models.ResourceGenRef {
	out := make([]models.ResourceGenRef, 0, len(refs))
	for _, ref := range refs {
		if ref.ID == 0 {
			continue
		}
		var src models.Resource
		if err := rc.DB.Unscoped().First(&src, ref.ID).Error; err != nil {
			continue
		}
		fillResourceURLs(&src, rc.Storage)
		item := models.ResourceGenRef{
			ID:      src.ID,
			Variant: strings.TrimSpace(ref.Variant),
			Kind:    src.Type,
			Label:   src.Name,
		}
		if item.Variant == "stylized" && src.StylizedImageURL != "" {
			item.ImageURL = src.StylizedImageURL
		} else {
			item.ImageURL = src.ImageURL
		}
		out = append(out, item)
	}
	return out
}

func (rc *ResourceController) startImageGenerationJob(c *gin.Context, projectID uint, resType string, input imageGenJobInput) {
	job, status, errMsg := rc.enqueueImageGenerationJob(projectID, resType, input)
	if errMsg != "" {
		fail(c, status, errMsg)
		return
	}
	c.JSON(202, gin.H{"jobId": job.ID, "status": job.Status, "progress": job.Progress, "message": job.Message})
}

func (rc *ResourceController) enqueueImageGenerationJob(projectID uint, resType string, input imageGenJobInput) (models.ImageGenerationJob, int, string) {
	if strings.TrimSpace(input.Name) == "" {
		return models.ImageGenerationJob{}, 400, nameRequiredMessage(resType)
	}
	if !imageGenInputHasContent(input) {
		return models.ImageGenerationJob{}, 400, contentRequiredMessage(resType)
	}
	count := input.Count
	if count < 1 {
		if input.TargetResourceID > 0 || input.ResourceID > 0 {
			count = 1
		} else {
			count = 2
		}
	}
	if count > 6 {
		count = 6
	}
	var model models.AIModel
	if input.ModelID != 0 {
		if err := rc.DB.Where("id = ? AND capability = ? AND enabled = ?", input.ModelID, "image", true).First(&model).Error; err != nil {
			return models.ImageGenerationJob{}, 400, "所选图像模型不可用，请在设置中心检查"
		}
	} else if err := rc.DB.Where("capability = ? AND enabled = ? AND is_default = ?", "image", true, true).First(&model).Error; err != nil {
		return models.ImageGenerationJob{}, 503, "请先在设置中心启用一个默认图像模型"
	}
	var provider models.AIProvider
	if err := rc.DB.First(&provider, model.ProviderID).Error; err != nil {
		return models.ImageGenerationJob{}, 503, "模型服务商不存在"
	}
	if shouldInjectParentAsFirstRef(resType) {
		if resType == "scene_grid" && input.ParentID == 0 {
			input.ParentID = rc.resolveSceneReverseParentID(projectID, input)
		}
		rc.injectParentReference(projectID, &input)
	} else if resType == "scene_reverse" || resType == "scene_reverse_skeleton" {
		input.ParentID = rc.resolveSceneReverseParentID(projectID, input)
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return models.ImageGenerationJob{}, 500, "创建生成任务失败"
	}
	job := models.ImageGenerationJob{
		ProjectID:  projectID,
		Type:       resType,
		Status:     "pending",
		Progress:   0,
		Message:    imageGenJobStartMessage(resType),
		TotalCount: count,
		InputJSON:  string(inputJSON),
	}
	if err := rc.DB.Create(&job).Error; err != nil {
		return models.ImageGenerationJob{}, 500, "创建生成任务失败"
	}
	go rc.runImageGenerationJob(job.ID, projectID, resType, provider, model, input, count)
	return job, 0, ""
}

func shouldInjectParentAsFirstRef(resType string) bool {
	switch strings.TrimSpace(resType) {
	case "scene_reverse", "scene_reverse_skeleton":
		// 图1 必须是线稿或原镜头，不能把底模插到参考图第一张。
		return false
	default:
		return true
	}
}

func isSceneDerivedPlateGen(gen string) bool {
	switch strings.TrimSpace(gen) {
	case "scene_grid", "scene_grid_cell", "scene_reverse", "scene_reverse_skeleton", "scene_panorama", "positioning", "positioning_skeleton":
		return true
	default:
		return false
	}
}

func (rc *ResourceController) walkToLibraryRoot(projectID, id uint) models.Resource {
	var cur models.Resource
	seen := map[uint]bool{}
	next := id
	for next > 0 && !seen[next] {
		seen[next] = true
		if err := rc.DB.Select("id, project_id, parent_id, type, gen_type, name, grid_id").
			First(&cur, "id = ? AND project_id = ?", next, projectID).Error; err != nil {
			return models.Resource{}
		}
		if pid := parentIDOf(cur); pid > 0 {
			next = pid
			continue
		}
		if strings.TrimSpace(cur.GenType) == "scene_grid_cell" && cur.GridID > 0 {
			next = cur.GridID
			continue
		}
		return cur
	}
	return cur
}

func (rc *ResourceController) usableSceneReverseParent(projectID, id uint) uint {
	root := rc.walkToLibraryRoot(projectID, id)
	if root.ID == 0 || root.Type != "scene" || isSceneDerivedPlateGen(root.GenType) {
		return 0
	}
	return root.ID
}

func (rc *ResourceController) resolveSceneReverseParentID(projectID uint, input imageGenJobInput) uint {
	if id := rc.usableSceneReverseParent(projectID, input.ParentID); id > 0 {
		return id
	}
	if id := rc.usableSceneReverseParent(projectID, input.ResourceID); id > 0 {
		return id
	}
	for _, ref := range input.ResourceRefs {
		if id := rc.usableSceneReverseParent(projectID, ref.ID); id > 0 {
			return id
		}
	}
	for _, rid := range input.ResourceIDs {
		if id := rc.usableSceneReverseParent(projectID, rid); id > 0 {
			return id
		}
	}
	return 0
}

func isSceneOverheadResource(r models.Resource) bool {
	switch strings.TrimSpace(r.GenType) {
	case "scene_grid":
		return true
	case "scene_grid_cell":
		return r.GridCell >= 7 && r.GridCell <= 9
	default:
		return false
	}
}

func (rc *ResourceController) resourceRefsIncludeOverhead(projectID uint, refs []imageGenResourceRef) bool {
	for _, ref := range refs {
		if ref.ID == 0 {
			continue
		}
		var r models.Resource
		if err := rc.DB.Select("id, gen_type, grid_cell").First(&r, "id = ? AND project_id = ?", ref.ID, projectID).Error; err != nil {
			continue
		}
		if isSceneOverheadResource(r) {
			return true
		}
	}
	return false
}

func (rc *ResourceController) relatedSceneIDsForOverhead(projectID, parentID uint) []uint {
	if parentID == 0 {
		return nil
	}
	seen := map[uint]bool{parentID: true}
	ids := []uint{parentID}
	add := func(id uint) {
		if id == 0 || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	var src models.Resource
	if err := rc.DB.Select("id, name, gen_refs_json").First(&src, "id = ? AND project_id = ?", parentID, projectID).Error; err != nil {
		return ids
	}
	if strings.TrimSpace(src.GenRefsJSON) != "" {
		var refs []models.ResourceGenRef
		if json.Unmarshal([]byte(src.GenRefsJSON), &refs) == nil {
			for _, ref := range refs {
				add(rc.usableSceneReverseParent(projectID, ref.ID))
			}
		}
	}
	return ids
}

func (rc *ResourceController) findSceneGridIDsForParent(projectID, parentID uint) []uint {
	if parentID == 0 {
		return nil
	}
	var grids []models.Resource
	if err := rc.DB.Select("id, parent_id, name, gen_refs_json, image_path").
		Where("project_id = ? AND gen_type = ?", projectID, "scene_grid").
		Find(&grids).Error; err != nil {
		return nil
	}
	related := rc.relatedSceneIDsForOverhead(projectID, parentID)
	relatedSet := map[uint]bool{}
	var plates []string
	for _, id := range related {
		relatedSet[id] = true
		var r models.Resource
		if err := rc.DB.Select("name").First(&r, "id = ? AND project_id = ?", id, projectID).Error; err == nil {
			if n := strings.TrimSpace(r.Name); n != "" {
				plates = append(plates, n)
			}
		}
	}
	var parent models.Resource
	_ = rc.DB.Select("name").First(&parent, "id = ? AND project_id = ?", parentID, projectID)
	if n := strings.TrimSpace(parent.Name); n != "" {
		plates = append(plates, n)
	}
	seen := map[uint]bool{}
	ids := make([]uint, 0, len(grids))
	match := func(g models.Resource) bool {
		if relatedSet[parentIDOf(g)] {
			return true
		}
		for _, plate := range plates {
			if services.SceneGridMatchesPlate(g.Name, plate) {
				return true
			}
		}
		if strings.TrimSpace(g.GenRefsJSON) != "" {
			var refs []models.ResourceGenRef
			if json.Unmarshal([]byte(g.GenRefsJSON), &refs) == nil {
				for _, ref := range refs {
					if relatedSet[rc.usableSceneReverseParent(projectID, ref.ID)] {
						return true
					}
				}
			}
		}
		return false
	}
	for _, g := range grids {
		if !match(g) || seen[g.ID] {
			continue
		}
		seen[g.ID] = true
		ids = append(ids, g.ID)
	}
	if len(ids) > 0 {
		return ids
	}
	var withImage []uint
	for _, g := range grids {
		if strings.TrimSpace(g.ImagePath) == "" || seen[g.ID] {
			continue
		}
		withImage = append(withImage, g.ID)
	}
	if len(withImage) == 1 {
		return withImage
	}
	return nil
}

func (rc *ResourceController) findSceneOverheadResourceID(projectID, parentID uint) uint {
	gids := rc.findSceneGridIDsForParent(projectID, parentID)
	if len(gids) == 0 {
		return 0
	}
	for _, cell := range []int{7, 9} {
		var hit models.Resource
		if err := rc.DB.Select("id").
			Where("project_id = ? AND gen_type = ? AND grid_id IN ? AND grid_cell = ? AND image_path <> ''", projectID, "scene_grid_cell", gids, cell).
			Order("id desc").
			First(&hit).Error; err == nil && hit.ID > 0 {
			return hit.ID
		}
	}
	var grid models.Resource
	if err := rc.DB.Select("id").
		Where("id IN ? AND image_path <> ''", gids).
		Order("id desc").
		First(&grid).Error; err == nil {
		return grid.ID
	}
	return 0
}

// appendSceneOverheadRef adds the scene's 9-grid overhead cell (or the composite
// 9-grid) so reverse generation can lock table/door/sofa layout without copying
// the original camera.
func (rc *ResourceController) appendSceneOverheadRef(projectID uint, input *imageGenJobInput) {
	if rc.resourceRefsIncludeOverhead(projectID, input.ResourceRefs) {
		return
	}
	parentID := input.ParentID
	if parentID == 0 {
		parentID = rc.resolveSceneReverseParentID(projectID, *input)
	}
	id := rc.findSceneOverheadResourceID(projectID, parentID)
	if id == 0 {
		return
	}
	for _, ref := range input.ResourceRefs {
		if ref.ID == id {
			return
		}
	}
	input.ResourceRefs = append(input.ResourceRefs, imageGenResourceRef{ID: id, Variant: "original"})
}

// appendScenePanoramaRefs adds reverse plate, CAD floor plan, and key 9-grid cells
// when the client did not already send them. Parent/master is injected separately.
func (rc *ResourceController) appendScenePanoramaRefs(projectID uint, input *imageGenJobInput) {
	parentID := input.ParentID
	if parentID == 0 {
		parentID = rc.resolveSceneReverseParentID(projectID, *input)
	}
	if parentID == 0 {
		return
	}
	have := map[uint]bool{}
	for _, ref := range input.ResourceRefs {
		if ref.ID > 0 {
			have[ref.ID] = true
		}
	}
	add := func(id uint) {
		if id == 0 || have[id] || id == parentID {
			return
		}
		have[id] = true
		input.ResourceRefs = append(input.ResourceRefs, imageGenResourceRef{ID: id, Variant: "original"})
	}

	var reverse models.Resource
	if err := rc.DB.Select("id").
		Where("project_id = ? AND parent_id = ? AND gen_type = ? AND image_path <> ''", projectID, parentID, "scene_reverse").
		Order("id desc").First(&reverse).Error; err == nil {
		add(reverse.ID)
	}

	var cad models.Resource
	if err := rc.DB.Select("id").
		Where("project_id = ? AND parent_id = ? AND type = ? AND image_path <> '' AND (name LIKE ? OR name LIKE ?)",
			projectID, parentID, "scene", "%二维建筑平面布局图%", "%俯视布局线稿%").
		Order("id desc").First(&cad).Error; err == nil {
		add(cad.ID)
	}

	gids := rc.findSceneGridIDsForParent(projectID, parentID)
	if len(gids) == 0 {
		return
	}
	// Prefer the spatial spine of the camera matrix: front / back / overhead / side.
	for _, cell := range []int{1, 5, 7, 3} {
		var hit models.Resource
		if err := rc.DB.Select("id").
			Where("project_id = ? AND gen_type = ? AND grid_id IN ? AND grid_cell = ? AND image_path <> ''",
				projectID, "scene_grid_cell", gids, cell).
			Order("id desc").First(&hit).Error; err == nil {
			add(hit.ID)
		}
	}
}

func (rc *ResourceController) inferOriginalGridCell(projectID uint, input imageGenJobInput) int {
	ids := make([]uint, 0, 2+len(input.ResourceRefs))
	if input.ResourceID > 0 {
		ids = append(ids, input.ResourceID)
	}
	if input.ParentID > 0 {
		ids = append(ids, input.ParentID)
	}
	for _, ref := range input.ResourceRefs {
		if ref.ID > 0 {
			ids = append(ids, ref.ID)
		}
	}
	for _, id := range ids {
		var r models.Resource
		if err := rc.DB.Select("gen_type, grid_cell, name").First(&r, "id = ? AND project_id = ?", id, projectID).Error; err != nil {
			continue
		}
		g := strings.TrimSpace(r.GenType)
		if g == "scene_grid" || g == "scene_grid_cell" || g == "scene_reverse" || g == "scene_reverse_skeleton" {
			if g == "scene_grid_cell" && r.GridCell >= 1 && r.GridCell <= 6 {
				return r.GridCell
			}
			continue
		}
		if strings.Contains(r.Name, "背面") {
			return 5
		}
		if strings.Contains(r.Name, "侧面") {
			return 3
		}
		return 1
	}
	return 1
}

func (rc *ResourceController) findSceneOppositeResourceID(projectID, parentID uint, originalCell int) uint {
	gids := rc.findSceneGridIDsForParent(projectID, parentID)
	if len(gids) == 0 {
		return 0
	}
	want := services.OppositeSceneGridCell(originalCell)
	for _, cell := range []int{want} {
		if cell == 7 || cell == 8 || cell == 9 {
			continue
		}
		var hit models.Resource
		if err := rc.DB.Select("id").
			Where("project_id = ? AND gen_type = ? AND grid_id IN ? AND grid_cell = ? AND image_path <> ''", projectID, "scene_grid_cell", gids, cell).
			Order("id desc").
			First(&hit).Error; err == nil && hit.ID > 0 {
			return hit.ID
		}
	}
	return 0
}

func (rc *ResourceController) appendSceneOppositeRef(projectID uint, input *imageGenJobInput) {
	parentID := input.ParentID
	if parentID == 0 {
		parentID = rc.resolveSceneReverseParentID(projectID, *input)
	}
	origCell := rc.inferOriginalGridCell(projectID, *input)
	id := rc.findSceneOppositeResourceID(projectID, parentID, origCell)
	if id == 0 {
		return
	}
	for _, ref := range input.ResourceRefs {
		if ref.ID == id {
			return
		}
	}
	input.ResourceRefs = append(input.ResourceRefs, imageGenResourceRef{ID: id, Variant: "original"})
}

func (rc *ResourceController) injectParentReference(projectID uint, input *imageGenJobInput) uint {
	pid := input.ParentID
	if pid == 0 {
		fillID := input.TargetResourceID
		if fillID == 0 {
			fillID = input.ResourceID
		}
		if fillID > 0 {
			var r models.Resource
			if err := rc.DB.Select("parent_id").First(&r, "id = ? AND project_id = ?", fillID, projectID).Error; err == nil {
				pid = parentIDOf(r)
			}
		}
	}
	if pid == 0 {
		return 0
	}
	if !rc.validCreateParent(projectID, pid) {
		return 0
	}
	input.ParentID = pid
	for _, ref := range input.ResourceRefs {
		if ref.ID == pid {
			return pid
		}
	}
	input.ResourceRefs = append([]imageGenResourceRef{{ID: pid, Variant: "original"}}, input.ResourceRefs...)
	return pid
}

func (rc *ResourceController) waitForResourceImage(id uint, timeout time.Duration, onWait func(string)) bool {
	return rc.waitForParentIdentity(id, timeout, onWait) != ""
}

// waitForParentIdentity waits until the parent 定妆照 exists. Image gen prefers 真人底模.
func (rc *ResourceController) waitForParentIdentity(id uint, timeout time.Duration, onWait func(string)) string {
	return rc.waitForResourceVariant(id, timeout, false, onWait)
}

func (rc *ResourceController) waitForResourceVariant(id uint, timeout time.Duration, preferStylized bool, onWait func(string)) string {
	if id == 0 {
		return ""
	}
	deadline := time.Now().Add(timeout)
	var originalAt time.Time
	var last models.Resource
	for {
		if err := rc.DB.Select("type, image_path, stylized_image_path").First(&last, id).Error; err == nil {
			hasStylized := strings.TrimSpace(last.StylizedImagePath) != ""
			hasOriginal := strings.TrimSpace(last.ImagePath) != ""
			if preferStylized {
				if hasStylized {
					return "stylized"
				}
				if hasOriginal {
					if last.Type != "character" {
						return "original"
					}
					if originalAt.IsZero() {
						originalAt = time.Now()
					}
					if time.Since(originalAt) >= 3*time.Minute {
						return "original"
					}
				}
			} else if hasOriginal {
				return "original"
			} else if hasStylized {
				return "stylized"
			}
		}
		if !time.Now().Before(deadline) {
			if preferStylized && strings.TrimSpace(last.StylizedImagePath) != "" {
				return "stylized"
			}
			if strings.TrimSpace(last.ImagePath) != "" {
				return "original"
			}
			if strings.TrimSpace(last.StylizedImagePath) != "" {
				return "stylized"
			}
			return ""
		}
		if onWait != nil {
			if preferStylized {
				onWait("等待底模非真人图…衍生图必须参考底模")
			} else {
				onWait("等待底模参考图…")
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func (rc *ResourceController) targetIsDerivative(projectID, targetID uint) bool {
	if targetID == 0 {
		return false
	}
	var r models.Resource
	if err := rc.DB.Select("parent_id").First(&r, "id = ? AND project_id = ?", targetID, projectID).Error; err != nil {
		return false
	}
	return parentIDOf(r) > 0
}

func (rc *ResourceController) runImageGenerationJob(jobID, projectID uint, resType string, provider models.AIProvider, model models.AIModel, input imageGenJobInput, count int) {
	rc.patchImageGenJob(jobID, "running", 2, "正在准备参考图…", 0, "")

	var parentID uint
	if shouldInjectParentAsFirstRef(resType) {
		if (resType == "scene_grid" || resType == "scene_panorama") && input.ParentID == 0 {
			input.ParentID = rc.resolveSceneReverseParentID(projectID, input)
		}
		parentID = rc.injectParentReference(projectID, &input)
		if resType == "scene_panorama" {
			rc.appendScenePanoramaRefs(projectID, &input)
		}
	} else if resType == "scene_reverse" || resType == "scene_reverse_skeleton" {
		input.ParentID = rc.resolveSceneReverseParentID(projectID, input)
		rc.appendSceneOverheadRef(projectID, &input)
		if resType == "scene_reverse" {
			rc.appendSceneOppositeRef(projectID, &input)
		}
	}
	if parentID > 0 {
		wantOriginal := false
		for _, ref := range input.ResourceRefs {
			if ref.ID == parentID && strings.EqualFold(strings.TrimSpace(ref.Variant), "original") {
				wantOriginal = true
				break
			}
		}
		variant := rc.waitForResourceVariant(parentID, 8*time.Minute, !wantOriginal, func(msg string) {
			rc.patchImageGenJob(jobID, "running", 4, msg, 0, "")
		})
		if variant == "" {
			rc.patchImageGenJob(jobID, "failed", 0, "", 0, "底模还没有定妆照，请先生成底模再出衍生图")
			return
		}
		for i, ref := range input.ResourceRefs {
			if ref.ID != parentID {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(ref.Variant), "original") {
				continue
			}
			input.ResourceRefs[i].Variant = variant
		}
	}

	// Persist upload refs as resources immediately so job polling /「点开任务」can restore all
	// reference thumbnails (API omits raw base64 imageDataList).
	input = rc.promoteUploadImageData(projectID, input)
	if raw, err := json.Marshal(input); err == nil {
		_ = rc.DB.Model(&models.ImageGenerationJob{}).Where("id = ?", jobID).Update("input_json", string(raw)).Error
	}

	refImages, refErr := rc.resolveReferenceImages(provider, projectID, input.ImageDataList, input.ResourceRefs, input.ResourceIDs, input.ImageData, input.ResourceID)
	if refErr != nil {
		rc.patchImageGenJob(jobID, "failed", 0, "", 0, refErr.Error())
		return
	}
	lockIdentity := parentID > 0 || rc.targetIsDerivative(projectID, input.TargetResourceID)
	if lockIdentity && len(refImages) == 0 {
		rc.patchImageGenJob(jobID, "failed", 0, "", 0, "底模还没有定妆照，请先生成底模再出衍生图")
		return
	}
	applyPreservePromptForGen(&input)
	if resType == "scene_reverse" {
		input.Description = services.StripImageRefLegend(input.Description)
		input.Description = services.PrependImageRefLegend(input.Description, services.SceneReverseRefLegend)
	} else if resType != "scene_reverse_skeleton" {
		if legend := rc.imageRefLegend(projectID, input); legend != "" {
			input.Description = services.PrependImageRefLegend(input.Description, legend)
		}
	}
	rc.patchImageGenJob(jobID, "running", 8, fmt.Sprintf("参考图已就绪（%d 张）", len(refImages)), 0, "")

	waitMsg := fmt.Sprintf("AI 生成中（0/%d）", count)
	if services.IsXais(provider) {
		waitMsg = fmt.Sprintf("等待 Xais 出图（0/%d）…多参考图可能需数分钟", count)
	}
	rc.patchImageGenJob(jobID, "running", 10, waitMsg, 0, "")

	stopHeartbeat := make(chan struct{})
	if services.IsXais(provider) {
		go func() {
			ticker := time.NewTicker(20 * time.Second)
			defer ticker.Stop()
			elapsed := 0
			for {
				select {
				case <-stopHeartbeat:
					return
				case <-ticker.C:
					elapsed += 20
					rc.patchImageGenJob(jobID, "running", 10,
						fmt.Sprintf("等待 Xais 出图中…已约 %d 秒（模型排队/推理中）", elapsed), 0, "")
				}
			}
		}()
	}

	onProgress := func(done, total int, message string) {
		progress := 10 + (done*65)/total
		rc.patchImageGenJob(jobID, "running", progress, message, done, "")
	}

	var urls []string
	var prompt string
	var genErr error
	var project models.Project
	_ = rc.DB.Select("style", "video_ratio").First(&project, projectID).Error
	resolution := services.NormalizeImageResolution(input.Resolution)
	if strings.TrimSpace(input.Resolution) == "" {
		resolution = services.NormalizeImageResolution(input.Quality)
	}
	// Image jobs are always 16:9 landscape. Project videoRatio (9:16 etc.) applies to video export only.
	aspect := services.NormalizeImageAspect("16:9", resType)
	spec := services.ImageGenSpec{Quality: resolution, Resolution: resolution, Aspect: aspect}
	rawPrompt := input.RawPrompt && strings.TrimSpace(input.Description) != ""
	switch resType {
	case "character":
		urls, prompt, genErr = rc.Ark.GenerateCharacterCandidates(provider, model, services.CharacterImageInput{
			Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), Count: count,
			Quality: resolution, Aspect: aspect,
			ReferenceImages: refImages, LockIdentity: lockIdentity, RawPrompt: rawPrompt, OnProgress: onProgress,
		})
	case "scene":
		urls, prompt, genErr = rc.Ark.GenerateSceneCandidates(provider, model, services.SceneImageInput{
			Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description),
			Style: strings.TrimSpace(project.Style), Count: count,
			Quality: resolution, Aspect: aspect,
			ReferenceImages: refImages, LockIdentity: lockIdentity, RawPrompt: rawPrompt, OnProgress: onProgress,
		})
	case "prop":
		urls, prompt, genErr = rc.Ark.GeneratePropCandidates(provider, model, services.PropImageInput{
			Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), Count: count,
			Quality: resolution, Aspect: aspect,
			ReferenceImages: refImages, RawPrompt: rawPrompt, OnProgress: onProgress,
		})
	case "positioning":
		urls, prompt, genErr = rc.Ark.GeneratePositioningCandidates(
			provider, model, strings.TrimSpace(input.Description), refImages, count, spec, onProgress,
		)
	case "positioning_skeleton":
		if len(refImages) > 1 {
			refImages = refImages[:1]
		}
		urls, prompt, genErr = rc.Ark.GeneratePositioningSkeletonCandidates(
			provider, model, strings.TrimSpace(input.Description), refImages, count, spec, onProgress,
		)
	case "scene_grid":
		urls, prompt, genErr = rc.Ark.GenerateSceneGridCandidates(
			provider, model, strings.TrimSpace(input.Description), refImages, count, spec, onProgress,
		)
	case "scene_reverse_skeleton":
		if len(refImages) > 1 {
			refImages = refImages[:1]
		}
		spec.Aspect = "16:9"
		spec.Resolution = "1k"
		urls, prompt, genErr = rc.Ark.GenerateSceneReverseSkeletonCandidates(
			provider, model, strings.TrimSpace(input.Description), refImages, count, spec, onProgress,
		)
	case "scene_reverse":
		urls, prompt, genErr = rc.Ark.GenerateSceneReverseCandidates(
			provider, model, strings.TrimSpace(input.Description), refImages, count, spec, onProgress,
		)
	case "scene_panorama":
		urls, prompt, genErr = rc.Ark.GenerateScenePanoramaCandidates(
			provider, model, strings.TrimSpace(input.Description), refImages, count, spec, onProgress,
		)
	case "motion_grid":
		urls, prompt, genErr = rc.Ark.GenerateMotionGridCandidates(
			provider, model, strings.TrimSpace(input.Description), refImages, count, spec, onProgress,
		)
	default:
		close(stopHeartbeat)
		rc.patchImageGenJob(jobID, "failed", 0, "", 0, "未知的资源类型")
		return
	}
	close(stopHeartbeat)
	if genErr != nil {
		rc.patchImageGenJob(jobID, "failed", 0, "", 0, "图像生成失败："+genErr.Error())
		return
	}

	rc.patchImageGenJob(jobID, "running", 78, "正在保存候选图…", len(urls), "")
	if strings.TrimSpace(prompt) != "" {
		rc.DB.Model(&models.ImageGenerationJob{}).Where("id = ?", jobID).Update("prompt", prompt)
	}

	persistType := resType
	switch resType {
	case "positioning", "scene_grid", "scene_reverse", "scene_panorama":
		persistType = "scene"
	case "positioning_skeleton", "scene_reverse_skeleton":
		persistType = "other"
	case "motion_grid":
		persistType = "other"
	}
	genRefsJSON, _ := rc.materializeGenRefs(projectID, input)
	fillID := input.TargetResourceID
	if fillID == 0 {
		fillID = input.ResourceID
	}
	if input.CandidateOnly {
		fillID = 0
	}
	meta := candidatePersistMeta{
		GenType:        resType,
		GenPrompt:      strings.TrimSpace(prompt),
		GenRefsJSON:    genRefsJSON,
		FillResourceID: fillID,
		PreservePrompt: input.PreservePrompt,
		SavedPrompt:    strings.TrimSpace(input.SavedPrompt),
		Revision:       strings.TrimSpace(input.Revision),
		CandidateOnly:  input.CandidateOnly,
	}
	// A single-cell retry uses the normal scene generator. Even in candidate-only mode its
	// candidate must keep the cell family so confirmation can merge it into that exact cell.
	metadataTargetID := fillID
	if metadataTargetID == 0 && input.CandidateOnly {
		metadataTargetID = input.TargetResourceID
	}
	if metadataTargetID > 0 && resType == "scene" {
		var target models.Resource
		if err := rc.DB.Select("gen_type").First(&target, metadataTargetID).Error; err == nil && target.GenType == "scene_grid_cell" {
			meta.GenType = "scene_grid_cell"
		}
	}
	if input.ParentID > 0 {
		pid := input.ParentID
		meta.ParentID = &pid
	}
	if fillID > 0 {
		var target models.Resource
		if err := rc.DB.Select("parent_id").First(&target, fillID).Error; err == nil {
			meta.ParentID = target.ParentID
		}
	}
	saved, err := rc.persistCandidateImagesWithProgress(projectID, persistType, input.Name, input.Description, urls, meta, func(done, total int) {
		progress := 78 + (done*22)/total
		rc.patchImageGenJob(jobID, "running", progress, fmt.Sprintf("保存候选图（%d/%d）", done, total), done, "")
	})
	if err != nil {
		rc.patchImageGenJob(jobID, "failed", 0, "", 0, "保存候选图失败："+err.Error())
		return
	}

	if resType == "positioning_skeleton" && input.ShotID > 0 && len(saved) > 0 {
		for i := range saved {
			saved[i].ShotID = &input.ShotID
			_ = rc.DB.Model(&models.Resource{}).Where("id = ?", saved[i].ID).Update("shot_id", input.ShotID).Error
		}
	}

	// 9帧图：归属到分镜，并把主候选切成9个连续帧（帧9 作为下一镜的接戏锚点）。
	if resType == "motion_grid" && input.ShotID > 0 && len(saved) > 0 {
		for i := range saved {
			saved[i].ShotID = &input.ShotID
			_ = rc.DB.Model(&models.Resource{}).Where("id = ?", saved[i].ID).Update("shot_id", input.ShotID).Error
		}
		rc.patchImageGenJob(jobID, "running", 96, "正在切分9帧画面…", len(saved), "")
		cells, splitErr := rc.splitGridResource(projectID, saved[0], "motion_grid_cell", &input.ShotID)
		if splitErr != nil {
			// Non-fatal: the grid itself is still usable; user can re-split manually.
			log.Printf("motion grid job %d: auto-split failed: %v", jobID, splitErr)
		} else {
			saved = append(saved, cells...)
		}
	}

	images := make([]gin.H, 0, len(saved))
	for _, r := range saved {
		fillResourceFields(&r, rc.DB, rc.Storage)
		images = append(images, gin.H{"url": r.ImageURL, "resourceId": r.ID})
	}
	result := imageGenJobResult{
		Images:    images,
		Resources: saved,
		Prompt:    prompt,
		ProjectID: projectID,
	}
	resultJSON, _ := json.Marshal(result)
	rc.DB.Model(&models.ImageGenerationJob{}).Where("id = ?", jobID).Updates(map[string]any{
		"status":      "completed",
		"progress":    100,
		"message":     fmt.Sprintf("已写入资产（%d 张）", len(saved)),
		"done_count":  len(saved),
		"prompt":      prompt,
		"result_json": string(resultJSON),
	})
}

func (rc *ResourceController) patchImageGenJob(jobID uint, status string, progress int, message string, doneCount int, errMsg string) {
	updates := map[string]any{"status": status}
	if progress > 0 {
		updates["progress"] = progress
	}
	if message != "" {
		updates["message"] = message
	}
	if doneCount > 0 {
		updates["done_count"] = doneCount
	}
	if errMsg != "" {
		updates["error_message"] = errMsg
		updates["message"] = errMsg
	}
	rc.DB.Model(&models.ImageGenerationJob{}).Where("id = ?", jobID).Updates(updates)
}

func imageGenInputHasContent(input imageGenJobInput) bool {
	if strings.TrimSpace(input.Description) != "" {
		return true
	}
	if len(input.ImageDataList) > 0 || len(input.ResourceRefs) > 0 || len(input.ResourceIDs) > 0 {
		return true
	}
	if strings.TrimSpace(input.ImageData) != "" || input.ResourceID > 0 {
		return true
	}
	return false
}

func imageGenJobStartMessage(resType string) string {
	switch resType {
	case "positioning_skeleton":
		return "正在生成火柴人站位骨架…"
	case "positioning":
		return "按已确认的骨架生成正式站位图…"
	case "scene_reverse_skeleton":
		return "正在生成反打线稿（平视，对调前后景）…"
	case "scene_reverse":
		return "按已确认的线稿生成反打图…"
	case "scene_panorama":
		return "正在生成 2:1 场景全景图…"
	default:
		return "任务已创建，等待开始…"
	}
}

func nameRequiredMessage(resType string) string {
	switch resType {
	case "character":
		return "请填写角色名称"
	case "scene":
		return "请填写场景名称"
	case "prop":
		return "请填写道具名称"
	case "positioning":
		return "请填写站位图名称"
	case "positioning_skeleton":
		return "请填写骨架名称"
	case "scene_reverse", "scene_reverse_skeleton", "scene_panorama":
		return "请填写场景名称"
	default:
		return "请填写名称"
	}
}

func contentRequiredMessage(resType string) string {
	switch resType {
	case "character":
		return "请填写角色描述或选择参考图"
	case "scene":
		return "请填写场景描述或选择参考图"
	case "prop":
		return "请填写道具描述或选择参考图"
	case "positioning":
		return "请填写站位图提示词"
	case "positioning_skeleton":
		return "请填写站位图提示词"
	case "scene_reverse", "scene_reverse_skeleton":
		return "请填写反打提示词"
	case "scene_panorama":
		return "请填写全景提示词或选择场景参考图"
	default:
		return "请填写描述或选择参考图"
	}
}

// promoteUploadImageData saves paste/upload base64 refs as Resource rows and folds them into
// ResourceRefs, then clears ImageDataList so subsequent job GETs can restore every thumbnail.
func (rc *ResourceController) promoteUploadImageData(projectID uint, input imageGenJobInput) imageGenJobInput {
	if len(input.ImageDataList) == 0 && strings.TrimSpace(input.ImageData) == "" {
		return input
	}
	_, uploadRefs := rc.materializeGenRefs(projectID, imageGenJobInput{
		ImageDataList: input.ImageDataList,
		ImageData:     input.ImageData,
	})
	for _, ur := range uploadRefs {
		variant := strings.TrimSpace(ur.Variant)
		if variant == "" {
			variant = "original"
		}
		dup := false
		for _, existing := range input.ResourceRefs {
			if existing.ID == ur.ID {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		input.ResourceRefs = append(input.ResourceRefs, imageGenResourceRef{
			ID:      ur.ID,
			Variant: variant,
		})
	}
	input.ImageDataList = nil
	input.ImageData = ""
	return input
}

func (rc *ResourceController) materializeGenRefs(projectID uint, input imageGenJobInput) (string, []models.ResourceGenRef) {
	refs := make([]models.ResourceGenRef, 0, len(input.ResourceRefs)+len(input.ResourceIDs)+len(input.ImageDataList)+1)
	seen := map[string]bool{}
	add := func(id uint, variant, kind, label string) {
		if id == 0 {
			return
		}
		key := fmt.Sprintf("%d:%s", id, variant)
		if seen[key] {
			return
		}
		seen[key] = true
		refs = append(refs, models.ResourceGenRef{
			ID:      id,
			Variant: variant,
			Kind:    kind,
			Label:   label,
		})
	}
	for _, ref := range input.ResourceRefs {
		if ref.ID == 0 {
			continue
		}
		var res models.Resource
		if err := rc.DB.Unscoped().First(&res, "id = ? AND project_id = ?", ref.ID, projectID).Error; err != nil {
			continue
		}
		add(res.ID, strings.TrimSpace(ref.Variant), res.Type, res.Name)
	}
	for _, id := range input.ResourceIDs {
		var res models.Resource
		if err := rc.DB.Unscoped().First(&res, "id = ? AND project_id = ?", id, projectID).Error; err != nil {
			continue
		}
		add(res.ID, "", res.Type, res.Name)
	}
	if input.ResourceID > 0 {
		var res models.Resource
		if err := rc.DB.Unscoped().First(&res, "id = ? AND project_id = ?", input.ResourceID, projectID).Error; err == nil {
			add(res.ID, "", res.Type, res.Name)
		}
	}
	for i, data := range input.ImageDataList {
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}
		label := fmt.Sprintf("上传参考 %d", i+1)
		res := models.Resource{
			ProjectID:   projectID,
			Type:        "other",
			Source:      "upload",
			Name:        label,
			Description: "AI 生图参考（自动保存）",
		}
		if err := rc.DB.Create(&res).Error; err != nil {
			continue
		}
		path, err := rc.Storage.SaveResourceImage(projectID, res.ID, data)
		if err != nil {
			rc.DB.Delete(&res)
			continue
		}
		res.ImagePath = path
		_ = rc.DB.Save(&res)
		add(res.ID, "original", "other", label)
	}
	if legacy := strings.TrimSpace(input.ImageData); legacy != "" {
		label := "上传参考"
		res := models.Resource{
			ProjectID:   projectID,
			Type:        "other",
			Source:      "upload",
			Name:        label,
			Description: "AI 生图参考（自动保存）",
		}
		if err := rc.DB.Create(&res).Error; err == nil {
			if path, err := rc.Storage.SaveResourceImage(projectID, res.ID, legacy); err == nil {
				res.ImagePath = path
				_ = rc.DB.Save(&res)
				add(res.ID, "original", "other", label)
			} else {
				rc.DB.Delete(&res)
			}
		}
	}
	if len(refs) == 0 {
		return "", nil
	}
	raw, _ := json.Marshal(refs)
	return string(raw), refs
}

type candidatePersistMeta struct {
	GenType        string
	GenPrompt      string
	GenRefsJSON    string
	FillResourceID uint
	ParentID       *uint
	PreservePrompt bool
	SavedPrompt    string
	Revision       string
	CandidateOnly  bool
}

const promptRevisionMarker = "【本次修改·必须执行，优先于下文原文】"
const promptOriginalMarker = "【原定妆照要求，与本次修改冲突时以修改为准】"
const promptRevisionMarkerLegacy = "【本次修改，其余保持不变】"

func unwrapStoredGenPrompt(text string) string {
	s := strings.TrimSpace(text)
	for i := 0; i < 8 && s != ""; i++ {
		next := unwrapStoredGenPromptOnce(s)
		if next == s {
			return s
		}
		s = next
	}
	return s
}

func unwrapStoredGenPromptOnce(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.LastIndex(s, promptOriginalMarker); idx >= 0 {
		s = strings.TrimSpace(s[idx+len(promptOriginalMarker):])
	}
	cut := len(s)
	for _, m := range []string{promptRevisionMarker, promptRevisionMarkerLegacy} {
		if i := strings.Index(s, m); i >= 0 && i < cut {
			cut = i
		}
	}
	if cut < len(s) {
		s = strings.TrimSpace(s[:cut])
	}
	return s
}

func appendPromptRevision(base, revision string) string {
	base = strings.TrimSpace(base)
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return base
	}
	if base == "" {
		return revision
	}
	return promptRevisionMarker + "\n" + revision + "\n\n" + promptOriginalMarker + "\n" + base
}

func applyPreservePromptForGen(input *imageGenJobInput) {
	if input == nil || !input.PreservePrompt {
		return
	}
	if rev := strings.TrimSpace(input.Revision); rev != "" {
		// Image editing already receives the previous image as a reference. Send only the
		// one-time delta so the old full prompt cannot compete with or dilute this edit.
		// persistCandidateImagesWithProgress still keeps SavedPrompt unchanged.
		input.Description = rev
		input.RawPrompt = false
		return
	}
	if saved := unwrapStoredGenPrompt(input.SavedPrompt); saved != "" {
		input.Description = saved
		input.RawPrompt = true
	}
}

func (rc *ResourceController) imageRefLegend(projectID uint, input imageGenJobInput) string {
	metas := make([]services.ImageRefMeta, 0, len(input.ResourceRefs)+len(input.ResourceIDs)+1)
	add := func(id uint) {
		if id == 0 {
			return
		}
		var res models.Resource
		if err := rc.DB.Unscoped().Select("name, type").First(&res, "id = ? AND project_id = ?", id, projectID).Error; err != nil {
			return
		}
		metas = append(metas, services.ImageRefMeta{Label: res.Name, Kind: res.Type})
	}
	for _, ref := range input.ResourceRefs {
		add(ref.ID)
	}
	for _, id := range input.ResourceIDs {
		add(id)
	}
	if len(metas) == 0 && input.ResourceID > 0 {
		add(input.ResourceID)
	}
	return services.BuildResourceImageRefLegend(metas)
}

func (rc *ResourceController) persistCandidateImagesWithProgress(projectID uint, resType, name, description string, urls []string, meta candidatePersistMeta, onProgress func(done, total int)) ([]models.Resource, error) {
	if meta.PreservePrompt {
		if kept := unwrapStoredGenPrompt(meta.SavedPrompt); kept != "" {
			meta.SavedPrompt = kept
			meta.GenPrompt = kept
		}
	}
	baseName := resourceBaseName(name)
	desc := strings.TrimSpace(description)
	fillTarget, hasFill := rc.resolveFillResource(projectID, resType, baseName, meta)
	if hasFill && fillTarget.ParentID != nil {
		meta.ParentID = fillTarget.ParentID
	}
	voice := ""
	if hasFill {
		voice = strings.TrimSpace(fillTarget.VoicePrompt)
	}
	if voice == "" && meta.ParentID != nil && *meta.ParentID > 0 {
		var parent models.Resource
		if rc.DB.Select("voice_prompt").First(&parent, *meta.ParentID).Error == nil {
			voice = strings.TrimSpace(parent.VoicePrompt)
		}
	}
	saved := make([]models.Resource, 0, len(urls))
	total := len(urls)
	for i, url := range urls {
		var resource models.Resource
		fillExisting := i == 0 && hasFill
		if fillExisting {
			resource = fillTarget
		} else {
			resource = models.Resource{
				ProjectID:   projectID,
				Type:        resType,
				Source:      "ai",
				Name:        fmt.Sprintf("%s · 候选%d", baseName, i+1),
				Description: desc,
				VoicePrompt: voice,
				GenPrompt:   strings.TrimSpace(meta.GenPrompt),
				GenType:     strings.TrimSpace(meta.GenType),
				GenRefsJSON: meta.GenRefsJSON,
				ParentID:    meta.ParentID,
			}
			if resource.GenPrompt == "" {
				resource.GenPrompt = desc
			}
			if err := rc.DB.Create(&resource).Error; err != nil {
				return saved, err
			}
		}
		if fillExisting {
			if strings.TrimSpace(resource.VoicePrompt) == "" && voice != "" {
				resource.VoicePrompt = voice
			}
			if meta.PreservePrompt {
				kept := unwrapStoredGenPrompt(meta.SavedPrompt)
				if kept == "" {
					kept = unwrapStoredGenPrompt(resource.GenPrompt)
				}
				if kept != "" {
					resource.GenPrompt = kept
				}
			} else if strings.TrimSpace(meta.GenPrompt) != "" {
				resource.GenPrompt = strings.TrimSpace(meta.GenPrompt)
			}
			if strings.TrimSpace(meta.GenType) != "" {
				resource.GenType = strings.TrimSpace(meta.GenType)
			}
			if meta.GenRefsJSON != "" {
				resource.GenRefsJSON = meta.GenRefsJSON
			}
			resource.Source = "ai"
			resource.IsGroupPrimary = true
		}
		createdNew := !fillExisting
		data, err := rc.Ark.DownloadImagePreferPix(strings.TrimSpace(url), true)
		if err != nil {
			if createdNew {
				rc.DB.Delete(&resource)
			}
			return saved, fmt.Errorf("下载候选图 %d 失败：%w", i+1, err)
		}
		path, err := rc.Storage.SaveResourceImageBytes(projectID, resource.ID, data)
		if err != nil {
			if createdNew {
				rc.DB.Delete(&resource)
			}
			return saved, fmt.Errorf("保存候选图 %d 失败：%w", i+1, err)
		}
		resource.ImagePath = path
		if err = rc.DB.Save(&resource).Error; err != nil {
			return saved, err
		}
		fillResourceFields(&resource, rc.DB, rc.Storage)
		saved = append(saved, resource)
		if onProgress != nil {
			onProgress(i+1, total)
		}
	}
	if len(saved) == 0 {
		return saved, nil
	}
	if meta.CandidateOnly {
		// Keep generated rows as hidden candidates. UsePrimary is the only operation that
		// may copy the selected candidate onto the canonical resource.
		return saved, nil
	}
	kept, err := rc.activateCandidatePrimary(saved[0])
	if err != nil {
		return saved, err
	}
	fillResourceFields(&kept, rc.DB, rc.Storage)
	out := []models.Resource{kept}
	seen := map[uint]bool{kept.ID: true}
	for _, r := range saved {
		if seen[r.ID] {
			continue
		}
		var live models.Resource
		if err := rc.DB.First(&live, r.ID).Error; err != nil {
			continue
		}
		live.IsGroupPrimary = false
		fillResourceFields(&live, rc.DB, rc.Storage)
		out = append(out, live)
		seen[r.ID] = true
	}
	return out, nil
}
