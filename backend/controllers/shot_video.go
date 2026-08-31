package controllers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func applyResourceVideoToShot(db *gorm.DB, storage *services.Storage, shot *models.Shot, resource models.Resource, projectID uint) error {
	if resource.Type != "video" {
		return errInvalid("仅视频资源可设为分镜视频")
	}
	if resource.VideoPath == "" {
		return errInvalid("视频资源文件不存在")
	}
	data, err := storage.ReadFile(resource.VideoPath)
	if err != nil {
		return errInvalid("读取视频资源失败")
	}
	ext := "mp4"
	if i := strings.LastIndex(resource.VideoPath, "."); i >= 0 {
		ext = resource.VideoPath[i+1:]
	}
	if _, err = storage.SaveVideo(projectID, shot.ID, data, ext); err != nil {
		return err
	}
	rid := resource.ID
	shot.ActiveVideoResourceID = &rid
	shot.VideoURL = storage.PublicURL("videos", projectID, shot.ID, ext)
	shot.Status = "done"
	shot.ErrorMessage = ""
	shot.VideoTaskID = ""
	return db.Save(shot).Error
}

// Download streams the shot's current video file (what the player shows),
// not a possibly stale resource-library copy.
func (sc *ShotController) Download(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	var episode models.Episode
	if err := sc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}
	data, ext, err := sc.Storage.ReadShotVideo(episode.ProjectID, shot.ID)
	if err != nil || len(data) == 0 {
		fail(c, 404, "分镜视频不存在")
		return
	}
	if ext == "" {
		ext = "mp4"
	}
	contentType := "video/mp4"
	switch strings.ToLower(ext) {
	case "webm":
		contentType = "video/webm"
	case "mov":
		contentType = "video/quicktime"
	case "m4v":
		contentType = "video/x-m4v"
	}
	filename := fmt.Sprintf("shot-%d.%s", shot.ID, ext)
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.Itoa(len(data)))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "private, max-age=120")
	c.Data(http.StatusOK, contentType, data)
}

func errInvalid(msg string) error {
	return &invalidError{msg: msg}
}

type invalidError struct{ msg string }

func (e *invalidError) Error() string { return e.msg }
