package controllers

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"novaly/backend/services"
)

type TTSController struct {
	TTS     *services.VolcTTSService
	Ark     *services.ArkService
	DataDir string
	DB      *gorm.DB

	mu   sync.Mutex
	jobs map[string]*ttsJob
}

type ttsJob struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"` // pending|running|done|error
	Total     int       `json:"total"`
	Done      int       `json:"done"`
	Error     string    `json:"error,omitempty"`
	Files     []string  `json:"files"`
	CreatedAt time.Time `json:"createdAt"`
}

func (t *TTSController) ensure() {
	if t.DataDir == "" {
		t.DataDir = "data/tts"
	}
	if t.jobs == nil {
		t.jobs = map[string]*ttsJob{}
	}
	_ = os.MkdirAll(t.DataDir, 0o755)
}

type synthesizeReq struct {
	Text            string  `json:"text"`
	VoiceType       string  `json:"voiceType"`
	SpeedRatio      float64 `json:"speedRatio"`
	SpeechRate      int     `json:"speechRate"`
	Pitch           int     `json:"pitch"`
	LoudnessRate    int     `json:"loudnessRate"`
	EnableEmotion   *bool   `json:"enableEmotion"`
	Emotion         string  `json:"emotion"`
	EmotionStrength int     `json:"emotionStrength"`
	EmotionHint     string  `json:"emotionHint"`
	Tone            string  `json:"tone"`
	Filename        string  `json:"filename"`
}

func (t *TTSController) Status(c *gin.Context) {
	t.ensure()
	c.JSON(http.StatusOK, gin.H{
		"configured": t.TTS != nil && t.TTS.Configured(),
	})
}

func (t *TTSController) Synthesize(c *gin.Context) {
	t.ensure()
	if t.TTS == nil || !t.TTS.Configured() {
		fail(c, http.StatusBadRequest, "未配置火山 TTS：请在 .env 设置 VOLC_TTS_API_KEY（或 APP_ID + ACCESS_TOKEN）")
		return
	}
	var req synthesizeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "无效请求")
		return
	}
	audio, err := t.TTS.Synthesize(services.TTSSynthesizeInput{
		Text:            req.Text,
		VoiceType:       req.VoiceType,
		SpeedRatio:      req.SpeedRatio,
		SpeechRate:      req.SpeechRate,
		Pitch:           req.Pitch,
		LoudnessRate:    req.LoudnessRate,
		EnableEmotion:   req.EnableEmotion == nil || *req.EnableEmotion,
		Emotion:         req.EmotionHint,
		Tone:            firstNonBlank(req.Emotion, req.Tone),
		EmotionStrength: req.EmotionStrength,
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}

	previewDir := filepath.Join(t.DataDir, "preview")
	_ = os.MkdirAll(previewDir, 0o755)
	name := sanitizeFilename(req.Filename)
	if name == "" {
		name = "preview_" + uuid.NewString() + ".mp3"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".mp3") {
		name += ".mp3"
	}
	rel := filepath.ToSlash(filepath.Join("preview", name))
	abs := filepath.Join(t.DataDir, "preview", name)
	if err := os.WriteFile(abs, audio, 0o644); err != nil {
		fail(c, http.StatusInternalServerError, "保存音频失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"audioUrl": "/api/tts/files/" + rel,
		"filename": name,
		"bytes":    len(audio),
	})
}

type batchLine struct {
	ID              string  `json:"id"`
	Text            string  `json:"text"`
	VoiceType       string  `json:"voiceType"`
	SpeedRatio      float64 `json:"speedRatio"`
	SpeechRate      int     `json:"speechRate"`
	Pitch           int     `json:"pitch"`
	LoudnessRate    int     `json:"loudnessRate"`
	EnableEmotion   *bool   `json:"enableEmotion"`
	Emotion         string  `json:"emotion"`
	EmotionStrength int     `json:"emotionStrength"`
	EmotionHint     string  `json:"emotionHint"`
	Tone            string  `json:"tone"`
	Filename        string  `json:"filename"`
}

type batchReq struct {
	Lines []batchLine `json:"lines"`
}

func (t *TTSController) Batch(c *gin.Context) {
	t.ensure()
	if t.TTS == nil || !t.TTS.Configured() {
		fail(c, http.StatusBadRequest, "未配置火山 TTS：请在 .env 设置 VOLC_TTS_API_KEY（或 APP_ID + ACCESS_TOKEN）")
		return
	}
	var req batchReq
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Lines) == 0 {
		fail(c, http.StatusBadRequest, "请提供 lines")
		return
	}
	jobID := uuid.NewString()
	jobDir := filepath.Join(t.DataDir, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		fail(c, http.StatusInternalServerError, "创建任务目录失败")
		return
	}
	job := &ttsJob{
		ID:        jobID,
		Status:    "running",
		Total:     len(req.Lines),
		Done:      0,
		Files:     []string{},
		CreatedAt: time.Now(),
	}
	t.mu.Lock()
	t.jobs[jobID] = job
	t.mu.Unlock()

	go t.runBatch(job, jobDir, req.Lines)
	c.JSON(http.StatusOK, gin.H{"jobId": jobID, "total": job.Total})
}

func (t *TTSController) runBatch(job *ttsJob, jobDir string, lines []batchLine) {
	usedNames := map[string]int{}
	for _, line := range lines {
		name := sanitizeFilename(line.Filename)
		if name == "" {
			name = sanitizeFilename(line.ID) + ".mp3"
		}
		if !strings.HasSuffix(strings.ToLower(name), ".mp3") {
			name += ".mp3"
		}
		if n, ok := usedNames[name]; ok {
			usedNames[name] = n + 1
			base := strings.TrimSuffix(name, ".mp3")
			name = fmt.Sprintf("%s_%d.mp3", base, n+1)
		} else {
			usedNames[name] = 1
		}

		audio, err := t.TTS.Synthesize(services.TTSSynthesizeInput{
			Text:            line.Text,
			VoiceType:       line.VoiceType,
			SpeedRatio:      line.SpeedRatio,
			SpeechRate:      line.SpeechRate,
			Pitch:           line.Pitch,
			LoudnessRate:    line.LoudnessRate,
			EnableEmotion:   line.EnableEmotion == nil || *line.EnableEmotion,
			Emotion:         line.EmotionHint,
			Tone:            firstNonBlank(line.Emotion, line.Tone),
			EmotionStrength: line.EmotionStrength,
		})
		if err != nil {
			t.mu.Lock()
			job.Status = "error"
			job.Error = fmt.Sprintf("%s: %v", line.ID, err)
			t.mu.Unlock()
			return
		}
		abs := filepath.Join(jobDir, name)
		if err := os.WriteFile(abs, audio, 0o644); err != nil {
			t.mu.Lock()
			job.Status = "error"
			job.Error = "写入文件失败: " + err.Error()
			t.mu.Unlock()
			return
		}
		t.mu.Lock()
		job.Done++
		job.Files = append(job.Files, name)
		t.mu.Unlock()
	}
	t.mu.Lock()
	job.Status = "done"
	t.mu.Unlock()
}

func (t *TTSController) GetJob(c *gin.Context) {
	t.ensure()
	id := c.Param("jobId")
	t.mu.Lock()
	job := t.jobs[id]
	t.mu.Unlock()
	if job == nil {
		fail(c, http.StatusNotFound, "任务不存在")
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	c.JSON(http.StatusOK, job)
}

func (t *TTSController) Download(c *gin.Context) {
	t.ensure()
	id := c.Param("jobId")
	jobDir := filepath.Join(t.DataDir, id)
	st, err := os.Stat(jobDir)
	if err != nil || !st.IsDir() {
		fail(c, http.StatusNotFound, "任务目录不存在")
		return
	}

	entries, err := os.ReadDir(jobDir)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取任务失败")
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"tts_%s.zip\"", id[:8]))
	zw := zip.NewWriter(c.Writer)
	defer zw.Close()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".mp3") {
			continue
		}
		w, err := zw.Create(name)
		if err != nil {
			return
		}
		f, err := os.Open(filepath.Join(jobDir, name))
		if err != nil {
			return
		}
		_, _ = io.Copy(w, f)
		_ = f.Close()
	}
}

func (t *TTSController) ServeFile(c *gin.Context) {
	t.ensure()
	rel := strings.TrimPrefix(c.Param("filepath"), "/")
	rel = filepath.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, "..") {
		fail(c, http.StatusBadRequest, "无效路径")
		return
	}
	abs := filepath.Join(t.DataDir, rel)
	if !strings.HasPrefix(abs, filepath.Clean(t.DataDir)+string(os.PathSeparator)) && abs != filepath.Clean(t.DataDir) {
		fail(c, http.StatusBadRequest, "无效路径")
		return
	}
	if st, err := os.Stat(abs); err != nil || st.IsDir() {
		c.Status(http.StatusNotFound)
		return
	}
	c.File(abs)
}

var unsafeFilename = regexp.MustCompile(`[^\w\p{Han}.\-]+`)

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = filepath.Base(name)
	name = unsafeFilename.ReplaceAllString(name, "_")
	return name
}
