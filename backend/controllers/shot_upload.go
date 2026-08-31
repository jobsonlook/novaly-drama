package controllers

import (
	"io"

	"github.com/gin-gonic/gin"
	"novaly/backend/models"
)

func (sc *ShotController) UploadVideo(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	var episode models.Episode
	if err := sc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}
	file, err := c.FormFile("video")
	if err != nil {
		fail(c, 400, "请选择要上传的视频文件")
		return
	}
	ext, ok := videoUploadExt(file.Filename)
	if !ok {
		fail(c, 400, "不支持的视频格式（支持 mp4 / webm / mov / m4v）")
		return
	}
	src, err := file.Open()
	if err != nil {
		fail(c, 400, "读取视频文件失败")
		return
	}
	data, err := io.ReadAll(src)
	_ = src.Close()
	if err != nil {
		fail(c, 400, "读取视频文件失败")
		return
	}
	archivedResource, _, _ := archiveShotVideoBeforeReplace(sc.DB, sc.Storage, episode.ProjectID, shot)
	videoPath, err := sc.Storage.SaveVideo(episode.ProjectID, shot.ID, data, ext)
	if err != nil {
		fail(c, 500, "保存视频失败")
		return
	}
	shot.VideoURL = sc.Storage.PublicURL("videos", episode.ProjectID, shot.ID, ext)
	shot.Status = "done"
	shot.ErrorMessage = ""
	shot.VideoTaskID = ""
	if err = sc.DB.Save(&shot).Error; err != nil {
		fail(c, 500, "保存分镜失败")
		return
	}
	videoResource, err := createVideoResourceFrom(sc.DB, sc.Storage, episode.ProjectID, shot, data, "upload", ext, nil, videoPath)
	if err != nil {
		fail(c, 500, "保存视频资源失败："+err.Error())
		return
	}
	shot.ActiveVideoResourceID = &videoResource.ID
	sc.DB.Save(&shot)
	fillShotFields(&shot, sc.Storage)
	resp := gin.H{"shot": shot, "videoResource": videoResource}
	if archivedResource.ID != 0 {
		resp["archivedResource"] = archivedResource
	}
	c.JSON(200, resp)
}
