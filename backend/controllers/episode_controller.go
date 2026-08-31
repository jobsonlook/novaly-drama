package controllers

import (
	"strconv"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type EpisodeController struct {
	DB      *gorm.DB
	Storage *services.Storage
}

func (ec *EpisodeController) Add(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	var project models.Project
	if err := ec.DB.First(&project, projectID).Error; err != nil {
		fail(c, 404, "项目不存在")
		return
	}
	if project.EpisodeCount >= 100 {
		fail(c, 400, "集数不能超过 100")
		return
	}
	var maxNumber int
	ec.DB.Model(&models.Episode{}).Where("project_id = ?", projectID).Select("coalesce(max(number), 0)").Scan(&maxNumber)
	number := maxNumber + 1
	episode := models.Episode{
		ProjectID: projectID,
		Number:    number,
		Title:     "第" + strconv.Itoa(number) + "集",
	}
	err := ec.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&episode).Error; err != nil {
			return err
		}
		return tx.Model(&project).Update("episode_count", number).Error
	})
	if err != nil {
		fail(c, 500, "添加分集失败")
		return
	}
	episode.Shots = []models.Shot{}
	c.JSON(201, episode)
}

func (ec *EpisodeController) Delete(c *gin.Context) {
	episode, ok := ec.find(c)
	if !ok {
		return
	}
	var count int64
	ec.DB.Model(&models.Episode{}).Where("project_id = ?", episode.ProjectID).Count(&count)
	if count <= 1 {
		fail(c, 400, "至少保留 1 集")
		return
	}
	err := ec.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("episode_id = ?", episode.ID).Delete(&models.Shot{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&episode).Error; err != nil {
			return err
		}
		return renumberEpisodes(tx, episode.ProjectID)
	})
	if err != nil {
		fail(c, 500, "删除分集失败")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func renumberEpisodes(tx *gorm.DB, projectID uint) error {
	var episodes []models.Episode
	if err := tx.Where("project_id = ?", projectID).Order("number asc").Find(&episodes).Error; err != nil {
		return err
	}
	for i, ep := range episodes {
		n := i + 1
		title := "第" + strconv.Itoa(n) + "集"
		if ep.Number != n || ep.Title != title {
			if err := tx.Model(&ep).Updates(map[string]any{"number": n, "title": title}).Error; err != nil {
				return err
			}
		}
	}
	return tx.Model(&models.Project{}).Where("id = ?", projectID).Update("episode_count", len(episodes)).Error
}

func (ec *EpisodeController) Get(c *gin.Context) {
	episode, ok := ec.find(c)
	if !ok {
		return
	}

	var total int64
	if err := ec.DB.Model(&models.Shot{}).Where("episode_id = ?", episode.ID).Count(&total).Error; err != nil {
		fail(c, 500, "读取分镜失败")
		return
	}
	episode.ShotTotal = int(total)

	page := parsePositiveInt(c.Query("page"), 0)
	pageSize := parsePositiveInt(c.Query("pageSize"), 0)

	q := ec.DB.Where("episode_id = ?", episode.ID).Order("sort_order asc, id asc")
	if page > 0 {
		if pageSize <= 0 {
			pageSize = 10
		}
		if pageSize > 50 {
			pageSize = 50
		}
		q = q.Offset((page - 1) * pageSize).Limit(pageSize)
	}

	var shots []models.Shot
	if err := q.Find(&shots).Error; err != nil {
		fail(c, 500, "读取分镜失败")
		return
	}
	if shots == nil {
		shots = []models.Shot{}
	}
	for i := range shots {
		fillShotFields(&shots[i], ec.Storage)
	}
	episode.Shots = shots
	c.JSON(200, episode)
}

func (ec *EpisodeController) Update(c *gin.Context) {
	episode, ok := ec.find(c)
	if !ok {
		return
	}
	var input struct {
		Title        *string `json:"title"`
		Script       *string `json:"script"`
		DirectorPlan *string `json:"directorPlan"`
	}
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	updates := map[string]any{}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			title = "第" + strconv.Itoa(episode.Number) + "集"
		}
		updates["title"] = title
	}
	if input.Script != nil {
		updates["script"] = strings.TrimSpace(*input.Script)
	}
	if input.DirectorPlan != nil {
		updates["director_plan"] = strings.TrimSpace(*input.DirectorPlan)
	}
	if len(updates) == 0 {
		c.JSON(200, episode)
		return
	}
	if err := ec.DB.Model(&episode).Updates(updates).Error; err != nil {
		fail(c, 500, "保存分集失败")
		return
	}
	if err := ec.DB.First(&episode, episode.ID).Error; err != nil {
		fail(c, 500, "保存分集失败")
		return
	}
	c.JSON(200, episode)
}

func (ec *EpisodeController) AddShot(c *gin.Context) {
	episode, ok := ec.find(c)
	if !ok {
		return
	}
	var input struct {
		Script   string `json:"script"`
		InsertAt *int   `json:"insertAt"`
	}
	_ = c.ShouldBindJSON(&input)

	var shots []models.Shot
	ec.DB.Where("episode_id = ?", episode.ID).Order("sort_order asc, id asc").Find(&shots)

	insertAt := len(shots)
	if input.InsertAt != nil {
		insertAt = *input.InsertAt
		if insertAt < 0 {
			insertAt = 0
		}
		if insertAt > len(shots) {
			insertAt = len(shots)
		}
	}

	shot := models.Shot{
		EpisodeID:         episode.ID,
		SortOrder:         insertAt + 1,
		Script:            strings.TrimSpace(input.Script),
		Duration:          10,
		Resolution:        "720p",
		Status:            "draft",
		RefsJSON:          "[]",
		CharacterRefsJSON: "[]",
		CharacterIDsJSON:  "[]",
	}
	err := ec.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&shot).Error; err != nil {
			return err
		}
		ids := make([]uint, 0, len(shots)+1)
		for i := 0; i <= len(shots); i++ {
			if i == insertAt {
				ids = append(ids, shot.ID)
			}
			if i < len(shots) {
				ids = append(ids, shots[i].ID)
			}
		}
		return renumberShots(tx, episode.ID, ids)
	})
	if err != nil {
		fail(c, 500, "添加分镜失败："+err.Error())
		return
	}
	fillShotFields(&shot, ec.Storage)
	shot.Refs = []models.ShotRef{}
	shot.CharacterRefs = []models.CharacterRef{}
	c.JSON(201, shot)
}

func (ec *EpisodeController) ReorderShots(c *gin.Context) {
	episode, ok := ec.find(c)
	if !ok {
		return
	}
	var input struct {
		ShotIDs []uint `json:"shotIds"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.ShotIDs) == 0 {
		fail(c, 400, "请提供分镜顺序")
		return
	}

	var existing []models.Shot
	ec.DB.Where("episode_id = ?", episode.ID).Find(&existing)
	if len(existing) != len(input.ShotIDs) {
		fail(c, 400, "分镜列表不完整")
		return
	}
	existingIDs := make(map[uint]bool, len(existing))
	for _, s := range existing {
		existingIDs[s.ID] = true
	}
	for _, id := range input.ShotIDs {
		if !existingIDs[id] {
			fail(c, 400, "包含无效分镜")
			return
		}
	}

	if err := ec.DB.Transaction(func(tx *gorm.DB) error {
		return renumberShots(tx, episode.ID, input.ShotIDs)
	}); err != nil {
		fail(c, 500, "调整分镜顺序失败")
		return
	}

	var shots []models.Shot
	ec.DB.Where("episode_id = ?", episode.ID).Order("sort_order asc, id asc").Find(&shots)
	for i := range shots {
		fillShotFields(&shots[i], ec.Storage)
	}
	c.JSON(200, shots)
}

func renumberShots(tx *gorm.DB, episodeID uint, ids []uint) error {
	for i, id := range ids {
		if err := tx.Model(&models.Shot{}).Where("id = ? AND episode_id = ?", id, episodeID).Update("sort_order", i+1).Error; err != nil {
			return err
		}
	}
	return nil
}

func (ec *EpisodeController) find(c *gin.Context) (models.Episode, bool) {
	var episode models.Episode
	if err := ec.DB.First(&episode, c.Param("id")).Error; err != nil {
		fail(c, 404, "分集不存在")
		return episode, false
	}
	return episode, true
}
