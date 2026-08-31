package controllers

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"novaly/backend/models"
	"novaly/backend/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Download streams a video resource through the app origin so the browser
// can export without hitting COS CORS restrictions.
func (rc *ResourceController) Download(c *gin.Context) {
	resource, ok := rc.find(c)
	if !ok {
		return
	}
	if resource.Type != "video" {
		fail(c, 400, "仅支持下载视频资源")
		return
	}
	ensureResourceVideoCopy(rc.DB, rc.Storage, &resource)
	data, ext, err := resolveResourceVideoBytes(rc.DB, rc.Storage, &resource)
	if err != nil || len(data) == 0 {
		fail(c, 404, "视频文件不存在")
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
	filename := fmt.Sprintf("resource-%d.%s", resource.ID, ext)
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.Itoa(len(data)))
	disposition := "attachment"
	if c.Query("inline") == "1" {
		disposition = "inline"
	}
	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, filename))
	c.Header("Cache-Control", "private, max-age=120")
	// ServeContent implements byte ranges required by browser video seeking.
	http.ServeContent(c.Writer, c.Request, filename, time.Time{}, bytes.NewReader(data))
}

// resolveResourceVideoBytes loads video bytes for a library resource.
// Falls back to conventional COS/local paths and linked shot videos when VideoPath is empty/stale.
func resolveResourceVideoBytes(db *gorm.DB, storage *services.Storage, r *models.Resource) ([]byte, string, error) {
	if r == nil {
		return nil, "", fmt.Errorf("资源不存在")
	}
	tryPath := func(path string) ([]byte, string, bool) {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, "", false
		}
		data, err := storage.ReadFile(path)
		if err != nil || len(data) == 0 {
			return nil, "", false
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if ext == "" {
			ext = "mp4"
		}
		return data, ext, true
	}

	if data, ext, ok := tryPath(r.VideoPath); ok {
		return data, ext, nil
	}

	// Conventional resource library keys (may exist on COS even if DB path is blank).
	for _, e := range []string{"mp4", "webm", "mov", "m4v"} {
		p := storage.ResourceVideoPath(r.ProjectID, r.ID, e)
		if data, ext, ok := tryPath(p); ok {
			if r.VideoPath != p {
				r.VideoPath = p
				_ = db.Model(r).Update("video_path", p).Error
			}
			return data, ext, nil
		}
	}

	// Recover from the linked shot video when the library copy is missing.
	if r.ShotID != nil && *r.ShotID > 0 {
		data, ext, err := storage.ReadShotVideo(r.ProjectID, *r.ShotID)
		if err == nil && len(data) > 0 {
			if path, saveErr := storage.SaveResourceVideoBytes(r.ProjectID, r.ID, data, ext); saveErr == nil {
				r.VideoPath = path
				_ = db.Model(r).Update("video_path", path).Error
			} else {
				log.Printf("resource %d: recovered shot video but failed to archive copy: %v", r.ID, saveErr)
			}
			return data, ext, nil
		}
	}

	return nil, "", fmt.Errorf("视频文件不存在")
}
