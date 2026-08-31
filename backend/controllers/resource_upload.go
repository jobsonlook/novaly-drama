package controllers

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"novaly/backend/models"

	"github.com/gin-gonic/gin"
)

var allowedVideoExts = map[string]bool{
	"mp4": true, "webm": true, "mov": true, "m4v": true,
}

func (rc *ResourceController) UploadVideos(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	baseName := strings.TrimSpace(c.PostForm("name"))
	if baseName == "" {
		baseName = "上传视频"
	}
	description := strings.TrimSpace(c.PostForm("description"))
	remark := strings.TrimSpace(c.PostForm("remark"))
	if remark == "" {
		remark = description
	}
	form, err := c.MultipartForm()
	if err != nil {
		fail(c, 400, "请上传视频文件")
		return
	}
	files := form.File["videos"]
	if len(files) == 0 {
		fail(c, 400, "请至少选择一个视频文件")
		return
	}
	if len(files) > 20 {
		fail(c, 400, "单次最多上传 20 个视频")
		return
	}
	saved := make([]models.Resource, 0, len(files))
	for i, file := range files {
		ext, ok := videoUploadExt(file.Filename)
		if !ok {
			fail(c, 400, fmt.Sprintf("不支持的视频格式：%s（支持 mp4 / webm / mov / m4v）", file.Filename))
			return
		}
		resource := models.Resource{
			ProjectID:   projectID,
			Type:        "video",
			Source:      "upload",
			Name:        videoUploadName(baseName, file.Filename, i, len(files)),
			Description: description,
			Remark:      remark,
		}
		if err := rc.DB.Create(&resource).Error; err != nil {
			fail(c, 500, "创建视频资源失败")
			return
		}
		src, err := file.Open()
		if err != nil {
			rc.DB.Delete(&resource)
			fail(c, 400, "读取视频文件失败")
			return
		}
		data, err := io.ReadAll(src)
		_ = src.Close()
		if err != nil {
			rc.DB.Delete(&resource)
			fail(c, 400, "读取视频文件失败")
			return
		}
		path, err := rc.Storage.SaveResourceVideoBytes(projectID, resource.ID, data, ext)
		if err != nil {
			rc.DB.Delete(&resource)
			fail(c, 500, "保存视频失败")
			return
		}
		resource.VideoPath = path
		if err = rc.DB.Save(&resource).Error; err != nil {
			fail(c, 500, "保存视频资源失败")
			return
		}
		fillResourceURLs(&resource, rc.Storage)
		saved = append(saved, resource)
	}
	c.JSON(201, gin.H{"resources": saved, "count": len(saved)})
}

func videoUploadExt(filename string) (string, bool) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if allowedVideoExts[ext] {
		return ext, true
	}
	return "", false
}

func videoUploadName(baseName, filename string, index, total int) string {
	if total == 1 {
		return baseName
	}
	stem := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	if stem != "" {
		return fmt.Sprintf("%s · %s", baseName, stem)
	}
	return fmt.Sprintf("%s · 视频%d", baseName, index+1)
}
