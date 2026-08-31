package controllers

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"novaly/backend/models"
	"novaly/backend/services"

	"github.com/gin-gonic/gin"
)

// PreviousFrame extracts the last frame of the most recent finished shot video
// and adds it to the current shot as a transition reference image.
// The frame resource keeps the same visual style as the current shot's positioning
// reference (站位图), so characters/clothing stay consistent.
func (sc *ShotController) PreviousFrame(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}

	var episode models.Episode
	if err := sc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}
	projectID := episode.ProjectID

	// Find the most recent finished shot in this episode that has a video.
	var prev models.Shot
	err := sc.DB.Where(
		"episode_id = ? AND status = ? AND id != ? AND (sort_order < ? OR (sort_order = ? AND id < ?))",
		shot.EpisodeID, "done", shot.ID, shot.SortOrder, shot.SortOrder, shot.ID,
	).Order("sort_order DESC, id DESC").First(&prev).Error
	if err != nil {
		fail(c, 404, "当前分镜之前没有已生成的分镜视频")
		return
	}

	// Read the previous video bytes (local or COS).
	videoBytes, ext, err := sc.Storage.ReadShotVideo(projectID, prev.ID)
	if err != nil || len(videoBytes) == 0 {
		fail(c, 404, "未找到上一镜视频文件")
		return
	}

	// Extract the last frame via ffmpeg.
	frame, ferr := extractLastFrame(videoBytes, ext)
	if ferr != nil {
		fail(c, 502, "提取视频尾帧失败："+ferr.Error())
		return
	}

	// Create the frame resource. It inherits the current shot's positioning style
	// when present so the video model knows characters/clothing must match.
	res := models.Resource{
		ProjectID:   projectID,
		Type:        "scene",
		Source:      "transition",
		Name:        fmt.Sprintf("上一镜尾帧 · %s", prev.Label),
		Description: fmt.Sprintf("第 %s 镜视频最后一帧，用于转场衔接。人物需与当前站位图保持一致。", prev.Label),
		GenType:     "transition_frame",
		GenRefsJSON: shot.RefsJSON, // inherit current shot's refs (incl. positioning) for style/context
	}
	if err := sc.DB.Create(&res).Error; err != nil {
		fail(c, 500, "创建尾帧资源失败")
		return
	}
	path, err := sc.Storage.SaveResourceImageBytes(projectID, res.ID, frame)
	if err != nil {
		sc.DB.Delete(&res)
		fail(c, 500, "保存尾帧图片失败")
		return
	}
	res.ImagePath = path
	if err := sc.DB.Save(&res).Error; err != nil {
		fail(c, 500, "保存尾帧资源失败")
		return
	}

	// Mosaic faces in the BACKGROUND — xais image
	// gen can legitimately take minutes (overload retry + 1K→2K fallback), and doing
	// it synchronously blocked this endpoint for up to ~30 minutes. The raw frame is
	// usable immediately; the annotated version overwrites it when ready and bumps
	// updated_at so the versioned image URL refreshes in the UI.
	go func(projectID uint, shotSnapshot models.Shot, frameRes models.Resource) {
		annotated := sc.annotateTransitionFrame(projectID, shotSnapshot, frameRes)
		if annotated == nil {
			return
		}
		if _, err := sc.Storage.SaveResourceImageBytes(projectID, frameRes.ID, annotated); err != nil {
			log.Printf("transition frame annotate save failed (keeping raw frame): %v", err)
			return
		}
		if err := sc.DB.Model(&models.Resource{}).Where("id = ?", frameRes.ID).Update("updated_at", time.Now()).Error; err != nil {
			log.Printf("transition frame annotate touch failed: %v", err)
		}
		log.Printf("transition frame %d annotated (face mosaic)", frameRes.ID)
	}(projectID, shot, res)
	fillResourceURLs(&res, sc.Storage)

	// Prepend it as a scene ref so it becomes the first reference image for this shot.
	refs := decodeShotRefs(shot.RefsJSON, shot.CharacterRefsJSON, shot.CharacterIDsJSON, shot.SceneID)
	refIDs := make([]uint, 0, len(refs))
	for _, ref := range refs {
		refIDs = append(refIDs, ref.ID)
	}
	var oldTransitionIDs []uint
	if len(refIDs) > 0 {
		sc.DB.Unscoped().Model(&models.Resource{}).
			Where("id IN ? AND gen_type = ?", refIDs, "transition_frame").
			Pluck("id", &oldTransitionIDs)
	}
	oldTransition := make(map[uint]bool, len(oldTransitionIDs))
	for _, id := range oldTransitionIDs {
		oldTransition[id] = true
	}
	newRef := models.ShotRef{
		Kind:    "scene",
		ID:      res.ID,
		Variant: "original",
		Label:   "上一镜尾帧",
	}
	// Avoid duplicates.
	out := []models.ShotRef{newRef}
	for _, r := range refs {
		// Re-clicking replaces the prior transition frame instead of accumulating
		// multiple “上一镜尾帧” references.
		if r.ID == newRef.ID || oldTransition[r.ID] {
			continue
		}
		out = append(out, r)
	}
	shot.RefsJSON = encodeShotRefs(out)
	// Returning this endpoint also heals optimizer JSON artifacts already stored
	// on the shot, so a refresh cannot make them reappear.
	shot.Script = services.CleanOptimizedShotScript(shot.Script)
	if err := sc.DB.Save(&shot).Error; err != nil {
		fail(c, 500, "更新分镜参考图失败")
		return
	}

	fillShotFields(&shot, sc.Storage)
	c.JSON(200, gin.H{
		"resource":   res,
		"shot":       shot,
		"annotating": true,
		"message":    fmt.Sprintf("已提取第 %s 镜尾帧并设为当前分镜首张参考图；人脸马赛克后台处理中，稍后自动更新", prev.Label),
	})
}

// annotateTransitionFrame regenerates the frame with every face fully mosaicked.
// No names/text are added: identifying and labeling characters was unreliable.
func (sc *ShotController) annotateTransitionFrame(projectID uint, shot models.Shot, frameRes models.Resource) []byte {
	var provider models.AIProvider
	if err := sc.DB.Where("slug = ? AND enabled = ?", "volcengine-ark", true).First(&provider).Error; err != nil {
		log.Printf("transition frame mosaic skipped: Ark provider unavailable")
		return nil
	}
	// Mosaic-only work is pinned to Seedream 4.5. It preserves the source image
	// more faithfully for local edits and does not affect the user's default model.
	var model models.AIModel
	if err := sc.DB.Where(
		"provider_id = ? AND model_id = ? AND capability = ? AND enabled = ?",
		provider.ID, "doubao-seedream-4-5-251128", "image", true,
	).First(&model).Error; err != nil {
		log.Printf("transition frame mosaic skipped: Seedream 4.5 unavailable")
		return nil
	}

	// The frame is the only reference. Character identity is irrelevant because
	// every detected face receives the same mosaic treatment.
	frameImg, err := sc.Resource.resolveReferenceImage(provider, projectID, "", frameRes.ID, "original")
	if err != nil || frameImg == "" {
		log.Printf("transition frame annotate skipped: frame ref resolve failed: %v", err)
		return nil
	}
	refImgs := []string{frameImg}

	var project models.Project
	_ = sc.DB.Select("video_ratio").First(&project, projectID).Error
	aspect := strings.TrimSpace(project.VideoRatio)
	if aspect == "" {
		aspect = "16:9"
	}

	prompt := `图1是上一镜视频的最后一帧。严格保持图1的构图、场景、人物数量、人物站位、动作姿态、服装、镜头视角、光影与色调完全不变。
唯一允许的修改：自动检测画面中所有人物的脸部，在每张脸的完整五官区域覆盖高强度方块马赛克，必须彻底遮住眼睛、眉毛、鼻子、嘴巴及面部识别特征。
不要添加姓名、标签、字幕、文字、箭头、边框、水印或任何其它元素；不要重绘人物，不要换脸，不要改变画面。输出与图1相同宽高比与画质。`

	url, err := sc.Ark.GenerateImageEdit(provider, model, prompt, refImgs, services.ImageGenSpec{Aspect: aspect, Resolution: "1k"})
	if err != nil {
		log.Printf("transition frame annotate failed (keeping raw frame): %v", err)
		return nil
	}
	raw, err := sc.Ark.DownloadImage(url)
	if err != nil || len(raw) == 0 {
		log.Printf("transition frame annotate download failed (keeping raw frame): %v", err)
		return nil
	}
	return raw
}

// extractLastFrame returns the JPEG bytes of the last frame of a video.
// Requires ffmpeg to be installed on the server.
func extractLastFrame(video []byte, ext string) ([]byte, error) {
	if ext == "" {
		ext = "mp4"
	}
	tmp, err := writeTempFile(video, ext)
	if err != nil {
		return nil, err
	}
	defer removeTemp(tmp)

	// Seek 0.1s before the end and grab the last frame.
	out := tmp + ".jpg"
	cmd := exec.Command(
		"ffmpeg", "-y", "-sseof", "-0.1", "-i", tmp,
		"-frames:v", "1", "-q:v", "2", "-f", "image2", out,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg 失败: %v: %s", err, stderr.String())
	}

	// Re-encode to a clean JPEG (small, web-friendly).
	raw, err := readFile(out)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("解码尾帧失败: %w", err)
	}
	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// --- temp file helpers (kept local to avoid touching storage internals) ---
func writeTempFile(data []byte, ext string) (string, error) {
	f, err := osCreateTemp("", "novaly-frame-*."+ext)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		f.Close()
		removeTemp(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func osCreateTemp(dir, pattern string) (*os.File, error) {
	return os.CreateTemp(dir, pattern)
}

func removeTemp(name string) {
	_ = os.Remove(name)
}

func readFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}
