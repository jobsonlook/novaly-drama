package controllers

import (
	"github.com/gin-gonic/gin"
	"novaly/backend/models"
)

func (sc *ShotController) UseVideo(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	var input struct {
		ResourceID uint `json:"resourceId"`
	}
	if c.ShouldBindJSON(&input) != nil || input.ResourceID == 0 {
		fail(c, 400, "请指定视频资源")
		return
	}
	var episode models.Episode
	if err := sc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}
	var resource models.Resource
	if err := sc.DB.Where("id = ? AND project_id = ? AND type = ?", input.ResourceID, episode.ProjectID, "video").First(&resource).Error; err != nil {
		fail(c, 404, "视频资源不存在")
		return
	}
	ensureResourceVideoCopy(sc.DB, sc.Storage, &resource)
	if err := applyResourceVideoToShot(sc.DB, sc.Storage, &shot, resource, episode.ProjectID); err != nil {
		if _, ok := err.(*invalidError); ok {
			fail(c, 400, err.Error())
			return
		}
		fail(c, 500, "设置分镜视频失败")
		return
	}
	fillResourceURLs(&resource, sc.Storage)
	fillShotFields(&shot, sc.Storage)
	c.JSON(200, gin.H{"shot": shot, "resource": resource})
}
