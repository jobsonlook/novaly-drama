package controllers

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type LogsController struct {
	LogPath string
}

func (lc *LogsController) resolvePath() string {
	p := strings.TrimSpace(lc.LogPath)
	if p == "" {
		p = "../logs/novaly.log"
	}
	if filepath.IsAbs(p) {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// Tail returns the last N lines of the application log as plain text JSON.
func (lc *LogsController) Tail(c *gin.Context) {
	path := lc.resolvePath()
	lines := 400
	if raw := strings.TrimSpace(c.Query("lines")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > 5000 {
				n = 5000
			}
			lines = n
		}
	}
	text, size, mod, err := readLogTail(path, lines)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(200, gin.H{
				"path":    path,
				"exists":  false,
				"lines":   0,
				"content": "",
				"message": "日志文件尚不存在",
			})
			return
		}
		fail(c, 500, "读取日志失败："+err.Error())
		return
	}
	c.JSON(200, gin.H{
		"path":      path,
		"exists":    true,
		"lines":     lines,
		"size":      size,
		"updatedAt": mod.UTC().Format(time.RFC3339),
		"content":   text,
	})
}

// Download streams the last portion of the log file (capped) as an attachment.
func (lc *LogsController) Download(c *gin.Context) {
	path := lc.resolvePath()
	const maxBytes = 8 << 20 // 8 MiB
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			fail(c, 404, "日志文件不存在")
			return
		}
		fail(c, 500, "打开日志失败："+err.Error())
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		fail(c, 500, "读取日志失败："+err.Error())
		return
	}
	start := int64(0)
	if st.Size() > maxBytes {
		start = st.Size() - maxBytes
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			fail(c, 500, "读取日志失败："+err.Error())
			return
		}
	}
	name := "novaly-" + time.Now().Format("20060102-150405") + ".log"
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Status(200)
	_, _ = io.Copy(c.Writer, f)
}

func readLogTail(path string, maxLines int) (string, int64, time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, time.Time{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", 0, time.Time{}, err
	}
	const window = 2 << 20 // read last 2 MiB then take lines
	size := st.Size()
	start := int64(0)
	if size > window {
		start = size - window
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", 0, time.Time{}, err
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return "", 0, time.Time{}, err
	}
	text := string(raw)
	if start > 0 {
		if i := strings.IndexByte(text, '\n'); i >= 0 && i+1 < len(text) {
			text = text[i+1:]
		}
	}
	parts := strings.Split(text, "\n")
	if len(parts) > maxLines {
		parts = parts[len(parts)-maxLines:]
	}
	return strings.Join(parts, "\n"), size, st.ModTime(), nil
}
