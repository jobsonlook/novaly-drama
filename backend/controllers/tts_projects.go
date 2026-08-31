package controllers

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"novaly/backend/services"
)

type ttsCharacter struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	VoiceHint    string  `json:"voice_hint"`
	VoiceType    string  `json:"voice_type"`
	DefaultSpeed float64 `json:"default_speed"`
}

type ttsLine struct {
	ID              string  `json:"id"`
	SourceShotID    uint    `json:"source_shot_id,omitempty"`
	Episode         int     `json:"episode,omitempty"`
	Shot            int     `json:"shot,omitempty"`
	GlobalShot      int     `json:"global_shot"`
	Time            string  `json:"time"`
	Type            string  `json:"type"`
	Speaker         string  `json:"speaker"`
	Text            string  `json:"text"`
	Tone            string  `json:"tone,omitempty"` // legacy short tone
	Emotion         string  `json:"emotion,omitempty"`
	EmotionStrength int     `json:"emotion_strength,omitempty"`
	EnableEmotion   *bool   `json:"enable_emotion,omitempty"`
	Pitch           int     `json:"pitch"`
	SpeechRate      int     `json:"speech_rate"`
	LoudnessRate    int     `json:"loudness_rate"`
	SpeedRatio      float64 `json:"speed_ratio,omitempty"` // legacy
	EmotionHint     string  `json:"emotion_hint,omitempty"`
	NeedsReview     bool    `json:"needs_review,omitempty"`
	Filename        string  `json:"filename"`
	VoiceType       string  `json:"voice_type"`
	AudioURL        string  `json:"audioUrl,omitempty"`
	AudioReady      bool    `json:"audioReady,omitempty"`
}

func boolPtr(v bool) *bool {
	return &v
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func shortTextForFile(text string) string {
	re := regexp.MustCompile(`[^\w\p{Han}]+`)
	s := re.ReplaceAllString(strings.TrimSpace(text), "")
	runes := []rune(s)
	if len(runes) > 16 {
		runes = runes[:16]
	}
	if len(runes) == 0 {
		return "line"
	}
	return string(runes)
}

// buildLineAudioFilename uses current 总分镜 + 说话人 + 台词.
func buildLineAudioFilename(globalShot int, speaker, text string) string {
	sp := strings.TrimSpace(speaker)
	if sp == "" {
		sp = "未知"
	}
	name := fmt.Sprintf("分镜%02d_%s_%s.mp3", globalShot, sp, shortTextForFile(text))
	return sanitizeFilename(name)
}

type ttsProject struct {
	ID              string         `json:"id"`
	SourceProjectID uint           `json:"sourceProjectId,omitempty"`
	ExtractionMode  string         `json:"extractionMode,omitempty"`
	Name            string         `json:"name"`
	UpdatedAt       string         `json:"updatedAt"`
	Characters      []ttsCharacter `json:"characters"`
	Lines           []ttsLine      `json:"lines"`
}

type ttsProjectSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	UpdatedAt  string `json:"updatedAt"`
	LineCount  int    `json:"lineCount"`
	AudioCount int    `json:"audioCount"`
}

func (t *TTSController) projectsRoot() string {
	t.ensure()
	return filepath.Join(t.DataDir, "projects")
}

func (t *TTSController) projectDir(id string) string {
	return filepath.Join(t.projectsRoot(), id)
}

func (t *TTSController) projectJSONPath(id string) string {
	return filepath.Join(t.projectDir(id), "project.json")
}

func (t *TTSController) loadProject(id string) (*ttsProject, error) {
	b, err := os.ReadFile(t.projectJSONPath(id))
	if err != nil {
		return nil, err
	}
	var p ttsProject
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (t *TTSController) saveProject(p *ttsProject) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	dir := t.projectDir(p.ID)
	if err := os.MkdirAll(filepath.Join(dir, "audio"), 0o755); err != nil {
		return err
	}
	p.UpdatedAt = time.Now().Format(time.RFC3339)
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(t.projectJSONPath(p.ID), append(b, '\n'), 0o644)
}

func (t *TTSController) ListProjects(c *gin.Context) {
	root := t.projectsRoot()
	_ = os.MkdirAll(root, 0o755)
	entries, err := os.ReadDir(root)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取项目失败")
		return
	}
	out := []ttsProjectSummary{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p, err := t.loadProject(e.Name())
		if err != nil {
			continue
		}
		audioN := 0
		for _, line := range p.Lines {
			if line.AudioReady || line.AudioURL != "" {
				audioN++
			}
		}
		out = append(out, ttsProjectSummary{
			ID:         p.ID,
			Name:       p.Name,
			UpdatedAt:  p.UpdatedAt,
			LineCount:  len(p.Lines),
			AudioCount: audioN,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	c.JSON(http.StatusOK, gin.H{"projects": out})
}

func (t *TTSController) GetProject(c *gin.Context) {
	id := c.Param("id")
	p, err := t.loadProject(id)
	if err != nil {
		fail(c, http.StatusNotFound, "项目不存在")
		return
	}
	c.JSON(http.StatusOK, p)
}

func (t *TTSController) SaveProject(c *gin.Context) {
	var p ttsProject
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, http.StatusBadRequest, "无效请求")
		return
	}
	if id := c.Param("id"); id != "" {
		p.ID = id
	}
	if strings.TrimSpace(p.Name) == "" {
		p.Name = "未命名台词项目"
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Characters == nil {
		p.Characters = []ttsCharacter{}
	}
	if p.Lines == nil {
		p.Lines = []ttsLine{}
	}
	// preserve existing audio flags if client omitted them for same line ids
	if old, err := t.loadProject(p.ID); err == nil {
		audioByID := map[string]ttsLine{}
		for _, l := range old.Lines {
			audioByID[l.ID] = l
		}
		for i := range p.Lines {
			if prev, ok := audioByID[p.Lines[i].ID]; ok {
				if p.Lines[i].AudioURL == "" && prev.AudioURL != "" {
					p.Lines[i].AudioURL = prev.AudioURL
					p.Lines[i].AudioReady = prev.AudioReady
				}
			}
		}
	}
	if err := t.saveProject(&p); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, p)
}

func (t *TTSController) DeleteProject(c *gin.Context) {
	id := c.Param("id")
	dir := t.projectDir(id)
	if err := os.RemoveAll(dir); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type projectSynthReq struct {
	LineID          string  `json:"lineId"`
	Text            string  `json:"text"`
	VoiceType       string  `json:"voiceType"`
	SpeedRatio      float64 `json:"speedRatio"`
	SpeechRate      int     `json:"speechRate"`
	Pitch           int     `json:"pitch"`
	LoudnessRate    int     `json:"loudnessRate"`
	EnableEmotion   bool    `json:"enableEmotion"`
	Emotion         string  `json:"emotion"`
	EmotionStrength int     `json:"emotionStrength"`
	EmotionHint     string  `json:"emotionHint"`
	Tone            string  `json:"tone"` // legacy
	Filename        string  `json:"filename"`
	GlobalShot      int     `json:"globalShot"`
	Speaker         string  `json:"speaker"`
}

func (t *TTSController) ProjectSynthesize(c *gin.Context) {
	t.ensure()
	if t.TTS == nil || !t.TTS.Configured() {
		fail(c, http.StatusBadRequest, "未配置火山 TTS：请在 .env 设置 VOLC_TTS_API_KEY")
		return
	}
	id := c.Param("id")
	p, err := t.loadProject(id)
	if err != nil {
		fail(c, http.StatusNotFound, "项目不存在")
		return
	}
	var req projectSynthReq
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
		EnableEmotion:   req.EnableEmotion,
		Emotion:         req.EmotionHint,
		Tone:            firstNonBlank(req.Emotion, req.Tone),
		EmotionStrength: req.EmotionStrength,
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	gs := req.GlobalShot
	speaker := strings.TrimSpace(req.Speaker)
	// Prefer live fields from project line if request omitted them.
	for _, l := range p.Lines {
		if l.ID == req.LineID {
			if gs <= 0 {
				gs = l.GlobalShot
			}
			if speaker == "" {
				speaker = l.Speaker
			}
			break
		}
	}
	name := buildLineAudioFilename(gs, speaker, req.Text)
	audioDir := filepath.Join(t.projectDir(id), "audio")
	_ = os.MkdirAll(audioDir, 0o755)
	abs := filepath.Join(audioDir, name)
	if err := os.WriteFile(abs, audio, 0o644); err != nil {
		fail(c, http.StatusInternalServerError, "保存音频失败")
		return
	}
	rel := filepath.ToSlash(filepath.Join("projects", id, "audio", name))
	audioURL := "/api/tts/files/" + rel
	for i := range p.Lines {
		if p.Lines[i].ID == req.LineID {
			p.Lines[i].Text = req.Text
			p.Lines[i].Tone = req.Tone
			p.Lines[i].Emotion = req.Emotion
			p.Lines[i].EmotionStrength = req.EmotionStrength
			p.Lines[i].EmotionHint = req.EmotionHint
			p.Lines[i].EnableEmotion = boolPtr(req.EnableEmotion)
			p.Lines[i].Pitch = req.Pitch
			p.Lines[i].SpeechRate = req.SpeechRate
			p.Lines[i].LoudnessRate = req.LoudnessRate
			if speaker != "" {
				p.Lines[i].Speaker = speaker
			}
			if gs > 0 {
				p.Lines[i].GlobalShot = gs
			}
			p.Lines[i].Filename = name
			p.Lines[i].AudioURL = audioURL
			p.Lines[i].AudioReady = true
			break
		}
	}
	_ = t.saveProject(p)
	c.JSON(http.StatusOK, gin.H{
		"audioUrl": audioURL,
		"filename": name,
		"bytes":    len(audio),
		"project":  p,
	})
}

type projectBatchReq struct {
	LineIDs []string `json:"lineIds"` // empty = all
}

func (t *TTSController) ProjectBatch(c *gin.Context) {
	t.ensure()
	if t.TTS == nil || !t.TTS.Configured() {
		fail(c, http.StatusBadRequest, "未配置火山 TTS：请在 .env 设置 VOLC_TTS_API_KEY")
		return
	}
	id := c.Param("id")
	p, err := t.loadProject(id)
	if err != nil {
		fail(c, http.StatusNotFound, "项目不存在")
		return
	}
	var req projectBatchReq
	_ = c.ShouldBindJSON(&req)

	want := map[string]bool{}
	for _, lid := range req.LineIDs {
		want[lid] = true
	}
	var targets []ttsLine
	for _, line := range p.Lines {
		if len(want) > 0 && !want[line.ID] {
			continue
		}
		targets = append(targets, line)
	}
	if len(targets) == 0 {
		fail(c, http.StatusBadRequest, "没有可合成的台词")
		return
	}

	jobID := "proj_" + id + "_" + uuid.NewString()[:8]
	job := &ttsJob{
		ID:        jobID,
		Status:    "running",
		Total:     len(targets),
		Done:      0,
		Files:     []string{},
		CreatedAt: time.Now(),
	}
	t.mu.Lock()
	t.jobs[jobID] = job
	t.mu.Unlock()

	go t.runProjectBatch(job, id, targets)
	c.JSON(http.StatusOK, gin.H{"jobId": jobID, "total": job.Total})
}

func (t *TTSController) runProjectBatch(job *ttsJob, projectID string, targets []ttsLine) {
	p, err := t.loadProject(projectID)
	if err != nil {
		t.mu.Lock()
		job.Status = "error"
		job.Error = "项目不存在"
		t.mu.Unlock()
		return
	}
	charVoice := map[string]string{}
	charSpeed := map[string]float64{}
	for _, c := range p.Characters {
		charVoice[c.Name] = c.VoiceType
		charSpeed[c.Name] = c.DefaultSpeed
	}
	audioDir := filepath.Join(t.projectDir(projectID), "audio")
	_ = os.MkdirAll(audioDir, 0o755)
	lineIndex := map[string]int{}
	for i, l := range p.Lines {
		lineIndex[l.ID] = i
	}

	for _, line := range targets {
		voice := strings.TrimSpace(line.VoiceType)
		if voice == "" {
			voice = strings.TrimSpace(charVoice[line.Speaker])
		}
		speed := line.SpeedRatio
		if speed <= 0 {
			if s := charSpeed[line.Speaker]; s > 0 {
				speed = s
			} else {
				speed = 1
			}
		}
		if voice == "" {
			t.mu.Lock()
			job.Status = "error"
			job.Error = line.ID + ": 缺少 voice_type"
			t.mu.Unlock()
			return
		}
		name := buildLineAudioFilename(line.GlobalShot, line.Speaker, line.Text)
		audio, err := t.TTS.Synthesize(services.TTSSynthesizeInput{
			Text:            line.Text,
			VoiceType:       voice,
			SpeedRatio:      speed,
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
		abs := filepath.Join(audioDir, name)
		if err := os.WriteFile(abs, audio, 0o644); err != nil {
			t.mu.Lock()
			job.Status = "error"
			job.Error = "写入失败: " + err.Error()
			t.mu.Unlock()
			return
		}
		rel := filepath.ToSlash(filepath.Join("projects", projectID, "audio", name))
		audioURL := "/api/tts/files/" + rel
		if idx, ok := lineIndex[line.ID]; ok {
			p.Lines[idx].Filename = name
			p.Lines[idx].AudioURL = audioURL
			p.Lines[idx].AudioReady = true
		}
		t.mu.Lock()
		job.Done++
		job.Files = append(job.Files, name)
		t.mu.Unlock()
		_ = t.saveProject(p)
	}
	t.mu.Lock()
	job.Status = "done"
	t.mu.Unlock()
}

func (t *TTSController) DownloadProjectZip(c *gin.Context) {
	id := c.Param("id")
	p, err := t.loadProject(id)
	if err != nil {
		fail(c, http.StatusNotFound, "项目不存在")
		return
	}
	audioDir := filepath.Join(t.projectDir(id), "audio")
	used := map[string]int{}
	type entry struct{ src, dest string }
	var files []entry
	for _, line := range p.Lines {
		if !line.AudioReady && line.AudioURL == "" {
			continue
		}
		srcName := filepath.Base(line.Filename)
		if srcName == "" || srcName == "." {
			if u := strings.TrimPrefix(line.AudioURL, "/api/tts/files/"); u != "" {
				srcName = filepath.Base(u)
			}
		}
		if srcName == "" || srcName == "." {
			continue
		}
		srcPath := filepath.Join(audioDir, srcName)
		if _, err := os.Stat(srcPath); err != nil {
			continue
		}
		dest := buildLineAudioFilename(line.GlobalShot, line.Speaker, line.Text)
		if n, ok := used[dest]; ok {
			used[dest] = n + 1
			base := strings.TrimSuffix(dest, ".mp3")
			dest = fmt.Sprintf("%s_%d.mp3", base, n+1)
		} else {
			used[dest] = 1
		}
		files = append(files, entry{src: srcPath, dest: dest})
	}
	if len(files) == 0 {
		fail(c, http.StatusNotFound, "暂无已生成音频")
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"tts_project_%s.zip\"", id[:min(8, len(id))]))
	zw := zip.NewWriter(c.Writer)
	defer zw.Close()
	for _, fentry := range files {
		w, err := zw.Create(fentry.dest)
		if err != nil {
			return
		}
		f, err := os.Open(fentry.src)
		if err != nil {
			continue
		}
		_, _ = io.Copy(w, f)
		_ = f.Close()
	}
}
