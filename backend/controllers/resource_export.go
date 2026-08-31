package controllers

import (
	"archive/zip"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"novaly/backend/models"

	"github.com/gin-gonic/gin"
)

// ExportVideos streams a zip of library videos so the browser can download
// without holding the whole archive in JavaScript memory (needed on plain HTTP
// where the File System Access API is unavailable).
//
// GET /api/projects/:id/resources/export-videos
//
//	q        optional search (same as resource list)
//	ids      optional comma-separated resource IDs (selected export)
//	meta=1   return JSON {count} only, do not stream the zip
func (rc *ResourceController) ExportVideos(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	if projectID == 0 {
		fail(c, 400, "项目不存在")
		return
	}
	search := strings.TrimSpace(c.Query("q"))
	ids, err := parseExportIDs(c.Query("ids"))
	if err != nil {
		fail(c, 400, err.Error())
		return
	}

	var items []models.Resource
	if len(ids) > 0 {
		if err := rc.DB.Where("project_id = ? AND type = ? AND id IN ?", projectID, "video", ids).
			Order("created_at desc, id desc").
			Find(&items).Error; err != nil {
			fail(c, 500, "读取资源失败")
			return
		}
		byID := make(map[uint]models.Resource, len(items))
		for _, it := range items {
			byID[it.ID] = it
		}
		ordered := make([]models.Resource, 0, len(ids))
		for _, id := range ids {
			if it, ok := byID[id]; ok {
				ordered = append(ordered, it)
			}
		}
		items = ordered
	} else {
		q := libraryResourcesQuery(rc.DB, projectID, "video", search, false, 0, true)
		if err := q.Order("created_at desc, id desc").Find(&items).Error; err != nil {
			fail(c, 500, "读取资源失败")
			return
		}
	}

	if strings.TrimSpace(c.Query("meta")) == "1" {
		c.JSON(http.StatusOK, gin.H{"count": len(items)})
		return
	}
	if len(items) == 0 {
		fail(c, 404, "没有可导出的视频")
		return
	}

	stamp := time.Now().Format("20060102_150405")
	zipName := fmt.Sprintf("videos_export_%d_%s.zip", len(items), stamp)
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipName))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	if f, ok := c.Writer.(http.Flusher); ok {
		f.Flush()
	}

	zw := zip.NewWriter(c.Writer)
	defer zw.Close()

	used := map[string]int{}
	packed := 0
	for i := range items {
		item := &items[i]
		ensureResourceVideoCopy(rc.DB, rc.Storage, item)
		data, ext, err := resolveResourceVideoBytes(rc.DB, rc.Storage, item)
		if err != nil || len(data) == 0 {
			continue
		}
		if ext == "" {
			ext = "mp4"
		}
		name := uniqueExportZipName(videoExportZipStem(item), ext, used)
		h := &zip.FileHeader{
			Name:   name,
			Method: zip.Store, // videos are already compressed
		}
		h.SetModTime(time.Now())
		w, err := zw.CreateHeader(h)
		if err != nil {
			return
		}
		if _, err := w.Write(data); err != nil {
			return
		}
		packed++
		if f, ok := c.Writer.(http.Flusher); ok {
			f.Flush()
		}
	}
	_ = packed
}

func parseExportIDs(raw string) ([]uint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]uint, 0, len(parts))
	seen := make(map[uint]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil || n == 0 {
			return nil, fmt.Errorf("无效的资源 ID")
		}
		id := uint(n)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) > 5000 {
		return nil, fmt.Errorf("一次最多导出 5000 个视频")
	}
	return out, nil
}

func sanitizeExportZipName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "视频"
	}
	var b strings.Builder
	b.Grow(len(name))
	prevSpace := false
	for _, r := range name {
		switch {
		case strings.ContainsRune(`\/:*?"<>|`, r):
			b.WriteByte('_')
			prevSpace = false
		case unicode.IsSpace(r):
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.TrimLeft(out, ".")
	if out == "" {
		return "视频"
	}
	if len([]rune(out)) > 120 {
		runes := []rune(out)
		out = string(runes[:120])
	}
	return out
}

func videoExportZipStem(item *models.Resource) string {
	base := strings.TrimSpace(item.Name)
	if base == "" {
		base = "视频"
	}
	remark := strings.TrimSpace(item.Remark)
	if remark != "" {
		base = base + "+" + remark
	}
	return sanitizeExportZipName(base)
}

func uniqueExportZipName(stem, ext string, used map[string]int) string {
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		ext = "mp4"
	}
	filename := stem + "." + ext
	n := used[filename] + 1
	used[filename] = n
	if n == 1 {
		return filename
	}
	return fmt.Sprintf("%s_%d.%s", stem, n, ext)
}
