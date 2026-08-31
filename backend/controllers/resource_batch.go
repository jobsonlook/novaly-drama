package controllers

import (
	"sort"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services/crew"

	"github.com/gin-gonic/gin"
)

func (rc *ResourceController) BatchPrompts(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	if projectID == 0 {
		fail(c, 404, "项目不存在")
		return
	}
	var input struct {
		ResourceIDs []uint `json:"resourceIds"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.ResourceIDs) == 0 {
		fail(c, 400, "请先选择要生成提示词的资源")
		return
	}
	var project models.Project
	if err := rc.DB.First(&project, projectID).Error; err != nil {
		fail(c, 404, "项目不存在")
		return
	}
	items := rc.loadBatchResources(projectID, input.ResourceIDs)
	if len(items) == 0 {
		fail(c, 400, "没有可处理的角色/场景/道具")
		return
	}
	provider, model, errMsg := rc.loadTextModel()
	if errMsg != "" {
		fail(c, 400, errMsg)
		return
	}
	assets := make([]crew.AssetItem, 0, len(items))
	parents := rc.loadParentMap(items)
	for _, r := range items {
		assets = append(assets, resourceToAssetItem(r, parents[parentIDOf(r)]))
	}
	polished, err := crew.PolishVisualPrompts(rc.Ark, provider, model, assets, project)
	if err != nil {
		fail(c, 502, "生成提示词失败："+err.Error())
		return
	}
	if voiced, vErr := crew.FillCharacterVoices(rc.Ark, provider, model, polished, ""); vErr == nil {
		polished = voiced
	}
	byID := map[uint]crew.AssetItem{}
	byKey := map[string]crew.AssetItem{}
	for _, a := range polished {
		if a.ResourceID > 0 {
			byID[a.ResourceID] = a
		}
		byKey[a.Type+":"+strings.ToLower(strings.TrimSpace(a.Name))] = a
	}
	updated := make([]models.Resource, 0, len(items))
	for i, r := range items {
		hit, ok := byID[r.ID]
		if !ok {
			hit, ok = byKey[r.Type+":"+strings.ToLower(strings.TrimSpace(r.Name))]
		}
		if !ok && i < len(polished) {
			hit = polished[i]
			ok = strings.TrimSpace(hit.Prompt) != ""
		}
		if !ok {
			continue
		}
		updates := map[string]any{"gen_type": r.Type}
		if strings.TrimSpace(hit.Prompt) != "" {
			updates["gen_prompt"] = strings.TrimSpace(hit.Prompt)
		}
		if strings.TrimSpace(hit.Description) != "" {
			updates["description"] = strings.TrimSpace(hit.Description)
		}
		if r.Type == "character" && strings.TrimSpace(r.VoicePrompt) == "" && strings.TrimSpace(hit.VoicePrompt) != "" {
			updates["voice_prompt"] = strings.TrimSpace(hit.VoicePrompt)
		}
		if err := rc.DB.Model(&r).Updates(updates).Error; err != nil {
			continue
		}
		_ = rc.DB.First(&r, r.ID).Error
		fillResourceFields(&r, rc.DB, rc.Storage)
		updated = append(updated, r)
	}
	c.JSON(200, gin.H{"items": updated, "count": len(updated)})
}

func (rc *ResourceController) BatchImages(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	if projectID == 0 {
		fail(c, 404, "项目不存在")
		return
	}
	var input struct {
		ResourceIDs []uint `json:"resourceIds"`
		Count       int    `json:"count"`
		Resolution  string `json:"resolution"`
		ModelID     uint   `json:"modelId"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.ResourceIDs) == 0 {
		fail(c, 400, "请先选择要生成图片的资源")
		return
	}
	items := rc.loadBatchResources(projectID, input.ResourceIDs)
	if len(items) == 0 {
		fail(c, 400, "没有可出图的角色/场景/道具")
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		pi, pj := parentIDOf(items[i]), parentIDOf(items[j])
		if pi == 0 && pj != 0 {
			return true
		}
		if pi != 0 && pj == 0 {
			return false
		}
		return items[i].ID < items[j].ID
	})
	count := input.Count
	if count < 1 {
		count = 2
	}
	resolution := strings.TrimSpace(input.Resolution)
	if resolution == "" {
		resolution = "1k"
	}
	jobs := make([]gin.H, 0, len(items))
	skipped := make([]gin.H, 0)
	for _, r := range items {
		prompt := strings.TrimSpace(r.GenPrompt)
		if prompt == "" {
			prompt = strings.TrimSpace(r.Description)
		}
		if prompt == "" {
			skipped = append(skipped, gin.H{"id": r.ID, "name": r.Name, "reason": "缺少提示词或描述"})
			continue
		}
		job, status, errMsg := rc.enqueueImageGenerationJob(projectID, r.Type, imageGenJobInput{
			Name:             r.Name,
			Description:      prompt,
			Count:            count,
			Resolution:       resolution,
			ModelID:          input.ModelID,
			TargetResourceID: r.ID,
			ResourceRefs:     parentImageRefs(r),
		})
		if errMsg != "" {
			skipped = append(skipped, gin.H{"id": r.ID, "name": r.Name, "reason": errMsg, "status": status})
			continue
		}
		jobs = append(jobs, gin.H{
			"jobId":      job.ID,
			"resourceId": r.ID,
			"name":       r.Name,
			"type":       r.Type,
			"status":     job.Status,
			"message":    job.Message,
		})
	}
	if len(jobs) == 0 {
		reason := "没有可生成的资源"
		if len(skipped) > 0 {
			if msg, ok := skipped[0]["reason"].(string); ok && msg != "" {
				reason = msg
			}
		}
		fail(c, 400, reason)
		return
	}
	c.JSON(202, gin.H{"jobs": jobs, "skipped": skipped})
}

func parentIDOf(r models.Resource) uint {
	if r.ParentID == nil {
		return 0
	}
	return *r.ParentID
}

func parentImageRefs(r models.Resource) []imageGenResourceRef {
	id := parentIDOf(r)
	if id == 0 {
		return nil
	}
	return []imageGenResourceRef{{ID: id, Variant: "original"}}
}

func (rc *ResourceController) loadParentMap(items []models.Resource) map[uint]models.Resource {
	ids := make([]uint, 0)
	seen := map[uint]bool{}
	for _, r := range items {
		id := parentIDOf(r)
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	out := map[uint]models.Resource{}
	if len(ids) == 0 {
		return out
	}
	var parents []models.Resource
	if err := rc.DB.Where("id IN ?", ids).Find(&parents).Error; err != nil {
		return out
	}
	for _, p := range parents {
		out[p.ID] = p
	}
	return out
}

func resourceToAssetItem(r models.Resource, parent models.Resource) crew.AssetItem {
	item := crew.AssetItem{
		Name:        r.Name,
		Type:        r.Type,
		Description: strings.TrimSpace(r.Description),
		VoicePrompt: strings.TrimSpace(r.VoicePrompt),
		Prompt:      strings.TrimSpace(r.GenPrompt),
		ResourceID:  r.ID,
	}
	if pid := parentIDOf(r); pid > 0 {
		item.ParentID = pid
		item.IsDerivative = true
		item.ParentName = strings.TrimSpace(parent.Name)
		item.ParentDescription = strings.TrimSpace(parent.Description)
	}
	return item
}

func (rc *ResourceController) loadBatchResources(projectID uint, ids []uint) []models.Resource {
	var items []models.Resource
	if err := rc.DB.Where("project_id = ? AND id IN ? AND type IN ?", projectID, ids, []string{"character", "scene", "prop"}).
		Order("id asc").Find(&items).Error; err != nil {
		return nil
	}
	return items
}

func (rc *ResourceController) loadTextModel() (models.AIProvider, models.AIModel, string) {
	var model models.AIModel
	err := rc.DB.Where("capability = ? AND enabled = ? AND is_default = ?", "text", true, true).First(&model).Error
	if err != nil {
		err = rc.DB.Where("capability = ? AND enabled = ?", "text", true).Order("id asc").First(&model).Error
	}
	if err != nil {
		return models.AIProvider{}, models.AIModel{}, "请先在设置中心启用一个文本模型"
	}
	var provider models.AIProvider
	if err := rc.DB.First(&provider, model.ProviderID).Error; err != nil {
		return models.AIProvider{}, models.AIModel{}, "文本模型服务商不存在"
	}
	return provider, model, ""
}
