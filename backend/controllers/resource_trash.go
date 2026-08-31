package controllers

import (
	"strings"

	"novaly/backend/models"

	"github.com/gin-gonic/gin"
)

func (rc *ResourceController) ListTrash(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	search := strings.TrimSpace(c.Query("q"))
	page := parsePositiveInt(c.Query("page"), 1)
	pageSize := parsePositiveInt(c.Query("pageSize"), 12)
	if pageSize > 48 {
		pageSize = 48
	}

	q := rc.DB.Unscoped().Model(&models.Resource{}).
		Where("project_id = ? AND deleted_at IS NOT NULL", projectID)
	if search != "" {
		like := "%" + search + "%"
		q = q.Where("(name LIKE ? OR description LIKE ? OR COALESCE(remark, '') LIKE ?)", like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		fail(c, 500, "读取回收站失败")
		return
	}

	var items []models.Resource
	if err := q.Order("deleted_at desc, id desc").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&items).Error; err != nil {
		fail(c, 500, "读取回收站失败")
		return
	}
	if items == nil {
		items = []models.Resource{}
	}
	for i := range items {
		fillResourceFields(&items[i], rc.DB, rc.Storage)
	}
	c.JSON(200, gin.H{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (rc *ResourceController) Purge(c *gin.Context) {
	resource, ok := rc.findIncludingTrash(c)
	if !ok {
		return
	}
	if !resource.DeletedAt.Valid {
		fail(c, 400, "资源未在回收站中")
		return
	}
	if resource.ImagePath != "" {
		rc.Storage.DeleteFile(resource.ImagePath)
	}
	if resource.StylizedImagePath != "" {
		rc.Storage.DeleteFile(resource.StylizedImagePath)
	}
	if resource.VideoPath != "" {
		rc.Storage.DeleteFile(resource.VideoPath)
	}
	if err := rc.DB.Unscoped().Delete(&resource).Error; err != nil {
		fail(c, 500, "彻底删除资源失败")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (rc *ResourceController) findIncludingTrash(c *gin.Context) (models.Resource, bool) {
	var resource models.Resource
	if err := rc.DB.Unscoped().First(&resource, c.Param("id")).Error; err != nil {
		fail(c, 404, "资源不存在")
		return resource, false
	}
	return resource, true
}
