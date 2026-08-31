package controllers

import (
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"novaly/backend/models"
	"novaly/backend/services"
)

type EditorController struct {
	DB      *gorm.DB
	Storage *services.Storage
}

func (ec *EditorController) Load(c *gin.Context) {
	var episode models.Episode
	if err := ec.DB.First(&episode, c.Param("id")).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}
	var project models.Project
	if err := ec.DB.First(&project, episode.ProjectID).Error; err != nil {
		fail(c, 404, "项目不存在")
		return
	}
	var edit models.EditProject
	data := json.RawMessage(`{"tracks":[]}`)
	if err := ec.DB.Where("episode_id = ?", episode.ID).First(&edit).Error; err == nil && strings.TrimSpace(edit.DataJSON) != "" {
		data = json.RawMessage(edit.DataJSON)
	}
	var shots []models.Shot
	ec.DB.Where("episode_id = ?", episode.ID).Order("sort_order asc, id asc").Find(&shots)
	for i := range shots {
		fillShotFields(&shots[i], ec.Storage)
	}
	var resources []models.Resource
	ec.DB.Where("project_id = ? AND deleted_at IS NULL", episode.ProjectID).Order("id desc").Find(&resources)
	for i := range resources {
		fillResourceFields(&resources[i], ec.DB, ec.Storage)
	}
	c.JSON(200, gin.H{
		"project": project, "episode": episode, "edit": data,
		"shots": shots, "resources": resources,
	})
}

func (ec *EditorController) Save(c *gin.Context) {
	var episode models.Episode
	if err := ec.DB.First(&episode, c.Param("id")).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}
	var input struct {
		Data json.RawMessage `json:"data"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || len(input.Data) == 0 || !json.Valid(input.Data) {
		fail(c, 400, "剪辑工程数据无效")
		return
	}
	const maxProjectBytes = 8 << 20
	if len(input.Data) > maxProjectBytes {
		fail(c, 413, "剪辑工程过大")
		return
	}
	var edit models.EditProject
	err := ec.DB.Where("episode_id = ?", episode.ID).First(&edit).Error
	if err != nil {
		edit = models.EditProject{ProjectID: episode.ProjectID, EpisodeID: episode.ID, DataJSON: string(input.Data)}
		err = ec.DB.Create(&edit).Error
	} else {
		err = ec.DB.Model(&edit).Update("data_json", string(input.Data)).Error
	}
	if err != nil {
		fail(c, 500, "保存剪辑工程失败")
		return
	}
	c.JSON(200, gin.H{"ok": true, "updatedAt": edit.UpdatedAt})
}
