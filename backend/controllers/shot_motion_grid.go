package controllers

import (
	"fmt"
	"strings"

	"novaly/backend/models"

	"github.com/gin-gonic/gin"
)

type motionGridAnalyzeInput struct {
	RefLabels []string `json:"refLabels"`
}

// motionGridAnchor locates the previous shot's latest outro frame (帧9 of its newest
// 9-frame grid) so the next shot's grid can start from the exact same pose.
func (sc *ShotController) motionGridAnchor(shot models.Shot) (models.Resource, models.Shot, bool) {
	var prevShot models.Shot
	if err := sc.DB.Where("episode_id = ? AND sort_order < ?", shot.EpisodeID, shot.SortOrder).
		Order("sort_order desc, id desc").First(&prevShot).Error; err != nil {
		return models.Resource{}, prevShot, false
	}
	var cell models.Resource
	if err := sc.DB.Where("shot_id = ? AND gen_type = ? AND grid_cell = ?", prevShot.ID, "motion_grid_cell", 9).
		Order("grid_id desc, id desc").First(&cell).Error; err != nil {
		return models.Resource{}, prevShot, false
	}
	return cell, prevShot, true
}

// AnalyzeMotionGrid drafts an editable 9-frame (3×3 temporal grid) prompt for a shot,
// returning the auto-anchored previous-shot outro frame as the suggested first ref.
func (sc *ShotController) AnalyzeMotionGrid(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	if strings.TrimSpace(shot.Script) == "" {
		fail(c, 400, "请先填写当前分镜文案")
		return
	}
	var input motionGridAnalyzeInput
	_ = c.ShouldBindJSON(&input)

	model, provider, msg, okModel := sc.loadDefaultTextModel()
	if !okModel {
		fail(c, 503, msg)
		return
	}

	var episode models.Episode
	if err := sc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}
	var project models.Project
	_ = sc.DB.Select("style").First(&project, episode.ProjectID).Error

	labels := make([]string, 0, len(input.RefLabels)+1)
	prevOutroHint := ""
	var anchorRef *models.ResourceGenRef
	if cell, prevShot, found := sc.motionGridAnchor(shot); found {
		prevLabel := strings.TrimSpace(prevShot.Label)
		if prevLabel == "" {
			prevLabel = fmt.Sprintf("分镜%d", prevShot.SortOrder)
		}
		fillResourceURLs(&cell, sc.Storage)
		anchorRef = &models.ResourceGenRef{
			ID:       cell.ID,
			Variant:  "original",
			Kind:     "other",
			Label:    "上一镜收势帧",
			ImageURL: cell.ImageURL,
		}
		labels = append(labels, "上一镜收势帧")
		prevOutroHint = fmt.Sprintf("上一镜「%s」的收势画面见参考图图1（上一镜收势帧），其中的人物位置、朝向、姿态与场面状态是本镜头的直接前情。", prevLabel)
	}
	labels = append(labels, input.RefLabels...)

	prompt, err := sc.Ark.AnalyzeMotionGrid(
		provider,
		model,
		shot.Script,
		sc.previousShotContexts(shot),
		project.Style,
		labels,
		prevOutroHint,
	)
	if err != nil {
		fail(c, 502, "分析9帧文案失败："+err.Error())
		return
	}
	resp := gin.H{
		"prompt":    prompt,
		"shotId":    shot.ID,
		"shotLabel": shot.Label,
		"refLabels": labels,
	}
	if anchorRef != nil {
		resp["anchorRef"] = anchorRef
	}
	c.JSON(200, resp)
}

// GenerateMotionGrid starts an image job for the edited 9-frame prompt + refs.
func (sc *ShotController) GenerateMotionGrid(c *gin.Context) {
	if sc.Resource == nil {
		fail(c, 500, "服务未就绪")
		return
	}
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	var input positioningGenerateInput
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		fail(c, 400, "请填写9帧图提示词")
		return
	}
	if len(input.ResourceRefs) == 0 && len(input.ImageDataList) == 0 {
		fail(c, 400, "请至少选择一张参考图（有上一镜时会自动带入其收势帧）")
		return
	}

	var episode models.Episode
	if err := sc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}

	name := strings.TrimSpace(input.Name)
	if name == "" || name == "9帧图" {
		name = strings.TrimSpace(shot.Label)
	}
	if name == "" {
		name = fmt.Sprintf("分镜%d", shot.SortOrder)
	}
	if !strings.Contains(name, "9帧") {
		name = name + " · 9帧图"
	}

	const maxRefs = 12
	if len(input.ResourceRefs) > maxRefs {
		input.ResourceRefs = input.ResourceRefs[:maxRefs]
	}
	if len(input.ImageDataList) > maxRefs {
		input.ImageDataList = input.ImageDataList[:maxRefs]
	}
	if len(input.ResourceRefs)+len(input.ImageDataList) > maxRefs {
		remain := maxRefs - len(input.ResourceRefs)
		if remain < 0 {
			input.ResourceRefs = input.ResourceRefs[:maxRefs]
			input.ImageDataList = nil
		} else if remain < len(input.ImageDataList) {
			input.ImageDataList = input.ImageDataList[:remain]
		}
	}

	count := input.Count
	if count < 1 {
		count = 1
	}
	if count > 4 {
		count = 4
	}

	jobInput := imageGenJobInput{
		Name:          name,
		Description:   prompt,
		Count:         count,
		Quality:       input.Quality,
		Resolution:    input.Resolution,
		ModelID:       input.ModelID,
		ImageDataList: input.ImageDataList,
		ResourceRefs:  input.ResourceRefs,
		ShotID:        shot.ID,
	}
	sc.Resource.startImageGenerationJob(c, episode.ProjectID, "motion_grid", jobInput)
}
