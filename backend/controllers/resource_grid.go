package controllers

import (
	"fmt"
	"os"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"

	"github.com/gin-gonic/gin"
)

type sceneGridLegendAnalyzeInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ScenePrompt string `json:"scenePrompt"`
}

func (rc *ResourceController) loadDefaultTextModel() (models.AIModel, models.AIProvider, string, bool) {
	var model models.AIModel
	err := rc.DB.Where("capability = ? AND enabled = ? AND is_default = ?", "text", true, true).First(&model).Error
	if err != nil {
		err = rc.DB.Where("capability = ? AND enabled = ?", "text", true).Order("id asc").First(&model).Error
	}
	if err != nil {
		return models.AIModel{}, models.AIProvider{}, "请先在设置中心启用一个文本模型", false
	}
	var provider models.AIProvider
	if err := rc.DB.First(&provider, model.ProviderID).Error; err != nil {
		return models.AIModel{}, models.AIProvider{}, "文本模型服务商不存在", false
	}
	return model, provider, "", true
}

// AnalyzeSceneGridShapeLegend uses the text model to draft per-scene CAD shape legend.
func (rc *ResourceController) AnalyzeSceneGridShapeLegend(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	var input sceneGridLegendAnalyzeInput
	_ = c.ShouldBindJSON(&input)

	name := strings.TrimSpace(input.Name)
	desc := strings.TrimSpace(input.Description)
	if desc == "" {
		desc = strings.TrimSpace(input.ScenePrompt)
	}
	if name == "" && desc == "" {
		fail(c, 400, "请提供场景名称或描述")
		return
	}

	model, provider, msg, ok := rc.loadDefaultTextModel()
	if !ok {
		fail(c, 503, msg)
		return
	}

	var project models.Project
	_ = rc.DB.Select("style").First(&project, projectID).Error
	if style := strings.TrimSpace(project.Style); style != "" && desc != "" {
		desc = desc + "\n项目画面风格：" + style
	}

	legend, err := rc.Ark.AnalyzeSceneGridShapeLegend(provider, model, name, desc)
	if err != nil {
		fail(c, 502, "分析图形语义失败："+err.Error())
		return
	}
	c.JSON(200, gin.H{
		"legend":    legend,
		"projectId": projectID,
	})
}

// GenerateSceneGrid starts an image job that renders a scene 9-grid (camera matrix).
// When the client does not send an editable prompt, the default template is built here.
func (rc *ResourceController) GenerateSceneGrid(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	var input imageGenJobInput
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		fail(c, 400, "请填写场景名称")
		return
	}
	if strings.TrimSpace(input.Description) == "" {
		var project models.Project
		_ = rc.DB.Select("style").First(&project, projectID).Error
		input.Description = services.BuildSceneGridPrompt(input.Name, "", project.Style)
	}
	if !strings.Contains(input.Name, "9宫格") && !strings.Contains(input.Name, "九宫格") {
		input.Name = strings.TrimSpace(input.Name) + " · 9宫格"
	}
	if input.ParentID == 0 {
		input.ParentID = rc.resolveSceneReverseParentID(projectID, input)
	}
	rc.startImageGenerationJob(c, projectID, "scene_grid", input)
}

// GenerateSceneReverse starts a two-step reverse-angle job: spatial line drawing, then photoreal reverse plate.
func (rc *ResourceController) GenerateSceneReverse(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	var input struct {
		imageGenJobInput
		Stage string `json:"stage"`
	}
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		fail(c, 400, "请填写场景名称")
		return
	}
	base := strings.TrimSpace(strings.TrimSuffix(name, " · 反打骨架"))
	base = strings.TrimSpace(strings.TrimSuffix(base, " · 反打"))
	base = strings.TrimSpace(strings.TrimSuffix(base, " · 9宫格"))
	if base == "" {
		base = name
	}
	if input.ParentID == 0 {
		input.ParentID = rc.resolveSceneReverseParentID(projectID, input.imageGenJobInput)
	}
	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	jobType := "scene_reverse"
	if stage == "skeleton" {
		jobType = "scene_reverse_skeleton"
		input.Name = base + " · 反打骨架"
		if strings.TrimSpace(input.Description) == "" {
			input.Description = services.BuildSceneReverseSkeletonPrompt(base, "")
		}
		input.Count = 1
		input.Resolution = "1k"
		input.Quality = "1k"
	} else {
		input.Name = base + " · 反打"
		if strings.TrimSpace(input.Description) == "" {
			input.Description = services.BuildSceneReversePrompt(base, "")
		}
		if input.Count < 1 {
			input.Count = 1
		}
	}
	rc.startImageGenerationJob(c, projectID, jobType, input.imageGenJobInput)
}

// GenerateScenePanorama starts a 2:1 equirectangular panorama job for a scene plate.
func (rc *ResourceController) GenerateScenePanorama(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	var input imageGenJobInput
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		fail(c, 400, "请填写场景名称")
		return
	}
	base := strings.TrimSpace(strings.TrimSuffix(name, " · 全景图"))
	base = strings.TrimSpace(strings.TrimSuffix(base, " · 9宫格"))
	base = strings.TrimSpace(strings.TrimSuffix(base, " · 反打"))
	if base == "" {
		base = name
	}
	input.Name = base + " · 全景图"
	if input.ParentID == 0 {
		input.ParentID = rc.resolveSceneReverseParentID(projectID, input)
	}
	if strings.TrimSpace(input.Description) == "" {
		input.Description = services.BuildScenePanoramaPrompt(base, "")
	}
	if input.Count < 1 {
		input.Count = 1
	}
	if strings.TrimSpace(input.Resolution) == "" {
		input.Resolution = "2k"
		input.Quality = "2k"
	}
	rc.startImageGenerationJob(c, projectID, "scene_panorama", input)
}

// SplitGrid manually splits a grid resource (scene 9-grid or shot 9-frame grid) into
// 9 cell resources. Idempotent: previous cells of the same grid are soft-deleted first.
func (rc *ResourceController) SplitGrid(c *gin.Context) {
	grid, ok := rc.findIncludingTrash(c)
	if !ok {
		return
	}
	genType := strings.TrimSpace(grid.GenType)
	if genType != "scene_grid" && genType != "motion_grid" {
		fail(c, 400, "仅9宫格/9帧图资源支持切分")
		return
	}
	cellGenType := genType + "_cell"
	cells, err := rc.splitGridResource(grid.ProjectID, grid, cellGenType, grid.ShotID)
	if err != nil {
		fail(c, 500, "切分9宫格失败："+err.Error())
		return
	}
	for i := range cells {
		fillResourceFields(&cells[i], rc.DB, rc.Storage)
	}
	c.JSON(200, gin.H{"cells": cells, "gridId": grid.ID})
}

// SplitPanorama crops an equirectangular scene panorama into perspective camera views.
// Idempotent: previous views of the same panorama (grid_id) are soft-deleted first.
func (rc *ResourceController) SplitPanorama(c *gin.Context) {
	pano, ok := rc.findIncludingTrash(c)
	if !ok {
		return
	}
	if strings.TrimSpace(pano.GenType) != "scene_panorama" {
		fail(c, 400, "仅场景全景图资源支持切出机位")
		return
	}
	views, err := rc.splitPanoramaResource(pano.ProjectID, pano)
	if err != nil {
		fail(c, 500, "切出全景机位失败："+err.Error())
		return
	}
	for i := range views {
		fillResourceFields(&views[i], rc.DB, rc.Storage)
	}
	c.JSON(200, gin.H{"views": views, "panoramaId": pano.ID})
}

// GridCells returns all active cells without relying on library pagination.
func (rc *ResourceController) GridCells(c *gin.Context) {
	grid, ok := rc.findIncludingTrash(c)
	if !ok {
		return
	}
	var cells []models.Resource
	if err := rc.DB.Where("project_id = ? AND grid_id = ?", grid.ProjectID, grid.ID).
		Order("grid_cell asc, id desc").Find(&cells).Error; err != nil {
		fail(c, 500, "读取9宫格切分结果失败")
		return
	}
	for i := range cells {
		fillResourceFields(&cells[i], rc.DB, rc.Storage)
	}
	c.JSON(200, gin.H{"cells": cells, "gridId": grid.ID})
}

// splitGridResource crops the grid image into 9 cell resources (row-major, 帧1..帧9).
// Existing cells of the grid are soft-deleted first so re-splitting stays clean.
func (rc *ResourceController) splitGridResource(projectID uint, grid models.Resource, cellGenType string, shotID *uint) ([]models.Resource, error) {
	if strings.TrimSpace(grid.ImagePath) == "" {
		return nil, fmt.Errorf("9宫格资源没有图片文件")
	}
	data, err := os.ReadFile(grid.ImagePath)
	if err != nil {
		return nil, fmt.Errorf("读取9宫格图片失败：%w", err)
	}
	cellBytes, err := services.SplitGridImage(data)
	if err != nil {
		return nil, err
	}
	if err := rc.DB.Where("grid_id = ?", grid.ID).Delete(&models.Resource{}).Error; err != nil {
		return nil, err
	}
	baseName := strings.TrimSpace(grid.Name)
	cellWord := "帧"
	if cellGenType == "scene_grid_cell" {
		cellWord = "格"
	}
	// 场景宫格机位按矩阵顺序固定对应，切分格子以机位命名，替代原先烧进画面的文字标注
	cells := make([]models.Resource, 0, len(cellBytes))
	for i, cb := range cellBytes {
		cellLabel := fmt.Sprintf("%s%d", cellWord, i+1)
		if cellGenType == "scene_grid_cell" {
			if angle := services.SceneAngleLabel(i + 1); angle != "" {
				cellLabel = fmt.Sprintf("%s%d·%s", cellWord, i+1, angle)
			}
		}
		name := fmt.Sprintf("%s · %s", baseName, cellLabel)
		if cellGenType == "scene_grid_cell" {
			// 统一短格式：场景名·机位名（机位由 gridCell 固定对应，全库一致）
			if short := services.SceneGridCellName(grid.Name, i+1); short != "" {
				name = short
			}
		}
		cell := models.Resource{
			ProjectID:   projectID,
			Type:        grid.Type,
			Source:      "ai",
			Name:        name,
			Description: fmt.Sprintf("%s 第%d格（共9格）", baseName, i+1),
			GenType:     cellGenType,
			GridID:      grid.ID,
			GridCell:    i + 1,
			ShotID:      shotID,
		}
		if err := rc.DB.Create(&cell).Error; err != nil {
			return cells, err
		}
		path, err := rc.Storage.SaveResourceImageBytes(projectID, cell.ID, cb)
		if err != nil {
			rc.DB.Delete(&cell)
			return cells, fmt.Errorf("保存第%d格失败：%w", i+1, err)
		}
		cell.ImagePath = path
		if err := rc.DB.Save(&cell).Error; err != nil {
			return cells, err
		}
		cells = append(cells, cell)
	}
	return cells, nil
}

// splitPanoramaResource projects the equirectangular image into default camera views.
// Existing views of the panorama are soft-deleted first so re-splitting stays clean.
func (rc *ResourceController) splitPanoramaResource(projectID uint, pano models.Resource) ([]models.Resource, error) {
	if strings.TrimSpace(pano.ImagePath) == "" {
		return nil, fmt.Errorf("全景资源没有图片文件")
	}
	data, err := rc.Storage.ReadFile(pano.ImagePath)
	if err != nil {
		// Local-path fallback (matches SplitGrid behavior when Storage is unavailable).
		data, err = os.ReadFile(pano.ImagePath)
		if err != nil {
			return nil, fmt.Errorf("读取全景图片失败：%w", err)
		}
	}
	viewBytes, err := services.SplitPanoramaViews(data, 1280, 720)
	if err != nil {
		return nil, err
	}
	if err := rc.DB.Where("grid_id = ?", pano.ID).Delete(&models.Resource{}).Error; err != nil {
		return nil, err
	}
	baseName := strings.TrimSpace(pano.Name)
	baseName = strings.TrimSuffix(baseName, " · 全景图")
	baseName = strings.TrimSuffix(baseName, "·全景图")
	baseName = strings.TrimSpace(baseName)
	if baseName == "" {
		baseName = "场景"
	}
	views := make([]models.Resource, 0, len(viewBytes))
	// Hang views under the base scene plate when the panorama is already a
	// derivative; otherwise under the panorama itself so 「查看衍生」can find them.
	var viewParentID *uint
	if pano.ParentID != nil && *pano.ParentID > 0 {
		viewParentID = pano.ParentID
	} else {
		pid := pano.ID
		viewParentID = &pid
	}
	for i, vb := range viewBytes {
		name := fmt.Sprintf("%s · 全景·%s", baseName, vb.Label)
		cell := models.Resource{
			ProjectID:   projectID,
			ParentID:    viewParentID,
			Type:        pano.Type,
			Source:      "ai",
			Name:        name,
			Description: fmt.Sprintf("%s 全景机位：%s", baseName, vb.Label),
			GenType:     "scene_panorama_view",
			GridID:      pano.ID,
			GridCell:    i + 1,
			ShotID:      pano.ShotID,
		}
		if err := rc.DB.Create(&cell).Error; err != nil {
			return views, err
		}
		path, err := rc.Storage.SaveResourceImageBytes(projectID, cell.ID, vb.JPEG)
		if err != nil {
			rc.DB.Delete(&cell)
			return views, fmt.Errorf("保存机位 %s 失败：%w", vb.Label, err)
		}
		cell.ImagePath = path
		if err := rc.DB.Save(&cell).Error; err != nil {
			return views, err
		}
		views = append(views, cell)
	}
	return views, nil
}
