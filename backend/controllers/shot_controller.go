package controllers

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"novaly/backend/models"
	"novaly/backend/services"
	"novaly/backend/services/crew"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type shotGenerateContext struct {
	Shot     models.Shot
	Project  models.Project
	Model    models.AIModel
	Provider models.AIProvider
	Input    services.VideoInput
}

func (sc *ShotController) buildGenerateContext(shot models.Shot) (shotGenerateContext, string, bool) {
	if err := sc.DB.First(&shot, shot.ID).Error; err != nil {
		return shotGenerateContext{}, "分镜不存在", false
	}
	var episode models.Episode
	if err := sc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		return shotGenerateContext{}, "分集不存在", false
	}
	var project models.Project
	if err := sc.DB.First(&project, episode.ProjectID).Error; err != nil {
		return shotGenerateContext{}, "项目不存在", false
	}
	var model models.AIModel
	if shot.VideoModelID != nil {
		if err := sc.DB.Where("id = ? AND capability = ? AND enabled = ?", *shot.VideoModelID, "video", true).First(&model).Error; err != nil {
			return shotGenerateContext{}, "所选视频模型不可用，请重新选择", false
		}
	} else if err := sc.DB.Where("capability = ? AND enabled = ? AND is_default = ?", "video", true, true).First(&model).Error; err != nil {
		return shotGenerateContext{}, "请先在设置中心启用一个默认视频模型", false
	}
	var provider models.AIProvider
	if err := sc.DB.First(&provider, model.ProviderID).Error; err != nil {
		return shotGenerateContext{}, "模型服务商不存在", false
	}
	videoRefs := sc.loadVideoRefs(project.ID, shot)
	duration := crew.ShotMaxSeconds(shot.Duration)
	resolution := shot.Resolution
	if resolution == "" {
		resolution = "720p"
	}
	return shotGenerateContext{
		Shot: shot, Project: project, Model: model, Provider: provider,
		Input: services.VideoInput{
			Script: shot.Script, VisualStyle: shot.VisualStyle, ImageRefs: shot.ImageRefs,
			Style: project.Style, LookPack: crew.VideoLookPack(project, shot.VisualStyle),
			Ratio:    firstNonEmpty(project.VideoRatio, "16:9"),
			Duration: duration, Resolution: resolution,
			Refs: videoRefs, CharacterVoices: sc.characterVoices(videoRefs), Storage: sc.Storage,
		},
	}, "", true
}

func (sc *ShotController) loadVideoRefs(projectID uint, shot models.Shot) []services.VideoRef {
	refs := decodeShotRefs(shot.RefsJSON, shot.CharacterRefsJSON, shot.CharacterIDsJSON, shot.SceneID)
	resourceIDs := make([]uint, 0, len(refs))
	for _, ref := range refs {
		resourceIDs = append(resourceIDs, ref.ID)
	}
	var resources []models.Resource
	if len(resourceIDs) > 0 {
		sc.DB.Unscoped().Where("project_id = ? AND id IN ?", projectID, resourceIDs).Find(&resources)
	}
	byID := map[uint]models.Resource{}
	parentIDs := make([]uint, 0)
	for _, r := range resources {
		if r.ParentID != nil && *r.ParentID > 0 {
			parentIDs = append(parentIDs, *r.ParentID)
		}
	}
	if len(parentIDs) > 0 {
		var parents []models.Resource
		if err := sc.DB.Select("id", "name").Where("id IN ?", parentIDs).Find(&parents).Error; err == nil {
			names := map[uint]string{}
			for _, p := range parents {
				names[p.ID] = strings.TrimSpace(p.Name)
			}
			for i := range resources {
				if resources[i].ParentID != nil && *resources[i].ParentID > 0 {
					resources[i].ParentName = names[*resources[i].ParentID]
				}
			}
		}
	}
	for _, r := range resources {
		byID[r.ID] = r
	}
	// Keep shot ref order, but promote motion_grid to the front so the model treats it
	// as the primary storyboard image instead of just another style ref.
	type vidRefIdx struct {
		idx int
		ref models.ShotRef
	}
	ordered := make([]vidRefIdx, 0, len(refs))
	for i, ref := range refs {
		ordered = append(ordered, vidRefIdx{idx: i, ref: ref})
	}
	isMotionGrid := func(id uint) bool {
		r, ok := byID[id]
		return ok && strings.EqualFold(strings.TrimSpace(r.GenType), "motion_grid")
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		mi := isMotionGrid(ordered[i].ref.ID)
		mj := isMotionGrid(ordered[j].ref.ID)
		if mi == mj {
			return ordered[i].idx < ordered[j].idx
		}
		return mi
	})
	videoRefs := make([]services.VideoRef, 0, len(refs))
	for _, oi := range ordered {
		ref := oi.ref
		r, ok := byID[ref.ID]
		if !ok {
			continue
		}
		if ref.Kind == "scene" && r.Type != "scene" {
			continue
		}
		if ref.Kind == "prop" && r.Type != "prop" {
			continue
		}
		if ref.Kind == "other" && r.Type != "other" {
			continue
		}
		if ref.Kind == "character" && r.Type != "character" {
			continue
		}
		variant := ref.Variant
		if ref.Kind == "character" {
			variant = services.PreferredCharacterVideoVariant(r)
		}
		if ref.Kind == "scene" && variant == "" {
			variant = "original"
		}
		if ref.Kind == "other" && variant == "" {
			variant = "stylized"
		}
		videoRefs = append(videoRefs, services.VideoRef{
			Resource: r,
			Kind:     ref.Kind,
			Variant:  variant,
			Label:    strings.TrimSpace(ref.Label),
		})
	}
	return services.NormalizeVideoRefs(videoRefs)
}

func (sc *ShotController) characterVoices(refs []services.VideoRef) []services.CharacterVoice {
	parentIDs := make([]uint, 0)
	for _, r := range refs {
		if !isCharacterVideoRef(r) {
			continue
		}
		if strings.TrimSpace(r.Resource.VoicePrompt) == "" && r.Resource.ParentID != nil && *r.Resource.ParentID > 0 {
			parentIDs = append(parentIDs, *r.Resource.ParentID)
		}
	}
	parents := map[uint]string{}
	if len(parentIDs) > 0 {
		var list []models.Resource
		if err := sc.DB.Select("id", "voice_prompt", "name").Where("id IN ?", parentIDs).Find(&list).Error; err == nil {
			for _, p := range list {
				parents[p.ID] = strings.TrimSpace(p.VoicePrompt)
			}
		}
	}
	seen := map[string]bool{}
	out := make([]services.CharacterVoice, 0)
	for _, r := range refs {
		if !isCharacterVideoRef(r) {
			continue
		}
		voice := strings.TrimSpace(r.Resource.VoicePrompt)
		if voice == "" && r.Resource.ParentID != nil {
			voice = parents[*r.Resource.ParentID]
		}
		if voice == "" {
			continue
		}
		name := characterVoiceName(r.Resource)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, services.CharacterVoice{Name: name, Prompt: voice})
	}
	return out
}

func isCharacterVideoRef(r services.VideoRef) bool {
	return r.Kind == "character" || r.Resource.Type == "character"
}

func characterVoiceName(r models.Resource) string {
	if p := strings.TrimSpace(r.ParentName); p != "" {
		return p
	}
	name := strings.TrimSpace(r.Name)
	if i := strings.Index(name, " · "); i > 0 {
		return strings.TrimSpace(name[:i])
	}
	return name
}

func (sc *ShotController) PreviewPrompt(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	ctx, msg, ok := sc.buildGenerateContext(shot)
	if !ok {
		fail(c, 503, msg)
		return
	}
	c.JSON(200, services.PreviewVideoRequest(ctx.Model, ctx.Input))
}

type ShotController struct {
	DB       *gorm.DB
	Ark      *services.ArkService
	Storage  *services.Storage
	Resource *ResourceController
}

func (sc *ShotController) Get(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	fillShotFields(&shot, sc.Storage)
	c.JSON(200, shot)
}

func (sc *ShotController) Move(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	var input struct {
		Delta int `json:"delta"`
	}
	if c.ShouldBindJSON(&input) != nil || (input.Delta != -1 && input.Delta != 1) {
		fail(c, 400, "请提供 delta: -1 或 1")
		return
	}

	var shots []models.Shot
	if err := sc.DB.Where("episode_id = ?", shot.EpisodeID).Order("sort_order asc, id asc").Find(&shots).Error; err != nil {
		fail(c, 500, "读取分镜失败")
		return
	}
	idx := -1
	for i := range shots {
		if shots[i].ID == shot.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		fail(c, 404, "分镜不存在")
		return
	}
	newIdx := idx + input.Delta
	if newIdx < 0 || newIdx >= len(shots) {
		fillShotFields(&shot, sc.Storage)
		c.JSON(200, gin.H{"shot": shot, "moved": false})
		return
	}

	ids := make([]uint, len(shots))
	for i, s := range shots {
		ids[i] = s.ID
	}
	ids[idx], ids[newIdx] = ids[newIdx], ids[idx]
	if err := sc.DB.Transaction(func(tx *gorm.DB) error {
		return renumberShots(tx, shot.EpisodeID, ids)
	}); err != nil {
		fail(c, 500, "调整分镜顺序失败")
		return
	}
	if err := sc.DB.First(&shot, shot.ID).Error; err != nil {
		fail(c, 500, "调整分镜顺序失败")
		return
	}
	fillShotFields(&shot, sc.Storage)
	c.JSON(200, gin.H{"shot": shot, "moved": true, "sortOrder": shot.SortOrder})
}

func (sc *ShotController) Update(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	var input struct {
		Label             string                   `json:"label"`
		Script            string                   `json:"script"`
		Note              string                   `json:"note"`
		VisualStyle       string                   `json:"visualStyle"`
		ImageRefs         string                   `json:"imageRefs"`
		Refs              []models.ShotRef         `json:"refs"`
		CharacterRefs     []models.CharacterRef    `json:"characterRefs"`
		SceneID           *uint                    `json:"sceneId"`
		Duration          *int                     `json:"duration"`
		Resolution        string                   `json:"resolution"`
		VideoModelID      *uint                    `json:"videoModelId"`
		PositioningPrompt *string                  `json:"positioningPrompt"`
		PositioningRefs   *[]models.ResourceGenRef `json:"positioningRefs"`
		MotionGridPrompt  *string                  `json:"motionGridPrompt"`
		MotionGridRefs    *[]models.ResourceGenRef `json:"motionGridRefs"`
	}
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	shot.Label = strings.TrimSpace(input.Label)
	shot.Script = strings.TrimSpace(input.Script)
	shot.Note = strings.TrimSpace(input.Note)
	shot.VisualStyle = strings.TrimSpace(input.VisualStyle)
	shot.ImageRefs = strings.TrimSpace(input.ImageRefs)
	refs := input.Refs
	if len(refs) == 0 && (len(input.CharacterRefs) > 0 || input.SceneID != nil) {
		refs = make([]models.ShotRef, 0, len(input.CharacterRefs)+1)
		for _, r := range input.CharacterRefs {
			variant := r.Variant
			if variant == "" {
				variant = "stylized"
			}
			refs = append(refs, models.ShotRef{Kind: "character", ID: r.ID, Variant: variant})
		}
		if input.SceneID != nil {
			refs = append(refs, models.ShotRef{Kind: "scene", ID: *input.SceneID, Variant: "stylized"})
		}
	}
	shot.RefsJSON = encodeShotRefs(refs)
	shot.CharacterRefsJSON = encodeCharacterRefs(shotRefsToCharacterRefs(refs))
	shot.SceneID = shotRefsFirstSceneID(refs)
	if input.Duration != nil && *input.Duration > 0 {
		shot.Duration = crew.ShotMaxSeconds(*input.Duration)
	}
	if v := strings.TrimSpace(input.Resolution); v != "" {
		shot.Resolution = v
	}
	shot.VideoModelID = input.VideoModelID
	if input.PositioningPrompt != nil {
		shot.PositioningPrompt = strings.TrimSpace(*input.PositioningPrompt)
	}
	if input.PositioningRefs != nil {
		shot.PositioningRefsJSON = encodeResourceGenRefs(*input.PositioningRefs)
	}
	if input.MotionGridPrompt != nil {
		shot.MotionGridPrompt = strings.TrimSpace(*input.MotionGridPrompt)
	}
	if input.MotionGridRefs != nil {
		shot.MotionGridRefsJSON = encodeResourceGenRefs(*input.MotionGridRefs)
	}
	if err := sc.DB.Save(&shot).Error; err != nil {
		fail(c, 500, "保存分镜失败")
		return
	}
	// Manual edits must not run episode-wide Pack — it rewrites every shot and
	// undoes in-progress fixes (e.g. correcting wrong dialogue on one row).
	if polished := crew.PolishSavedShotScript(shot.Script, shot.Duration); polished != shot.Script {
		_ = sc.DB.Model(&shot).Update("script", polished).Error
		shot.Script = polished
	}
	fillShotFields(&shot, sc.Storage)
	c.JSON(200, shotUpdatePayload(shot, nil))
}

func (sc *ShotController) Delete(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	if err := sc.DB.Delete(&shot).Error; err != nil {
		fail(c, 500, "删除分镜失败")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (sc *ShotController) Generate(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	ctx, msg, ok := sc.buildGenerateContext(shot)
	if !ok {
		fail(c, 503, msg)
		return
	}
	shot = ctx.Shot
	project := ctx.Project
	shot.Status = "generating"
	shot.ErrorMessage = ""
	shot.VideoETA = ""
	sc.DB.Save(&shot)
	taskID, _, err := sc.Ark.GenerateVideo(ctx.Provider, ctx.Model, ctx.Input)
	if err != nil {
		shot.Status = "error"
		shot.ErrorMessage = err.Error()
		shot.VideoETA = ""
		sc.DB.Save(&shot)
		failWithShot(c, 502, err.Error(), shot, sc.Storage)
		return
	}
	shot.VideoTaskID = taskID
	if err = sc.DB.Save(&shot).Error; err != nil {
		fail(c, 500, "保存任务 ID 失败")
		return
	}
	remoteURL, err := sc.Ark.WaitVideoTask(ctx.Provider, taskID, func(eta string) {
		sc.persistShotVideoETA(shot.ID, eta)
	})
	if err != nil {
		shot.Status = "error"
		shot.ErrorMessage = err.Error()
		sc.DB.Save(&shot)
		failWithShot(c, 502, err.Error(), shot, sc.Storage)
		return
	}

	archivedResource, _, _ := archiveShotVideoBeforeReplace(sc.DB, sc.Storage, project.ID, shot)

	shotOut, videoResource, err := sc.finalizeShotVideo(shot, project, ctx, taskID, remoteURL, archivedResource)
	if err != nil {
		failWithShot(c, 502, err.Error(), shotOut, sc.Storage)
		return
	}
	resp := gin.H{"shot": shotOut, "videoResource": videoResource}
	if archivedResource.ID != 0 {
		resp["archivedResource"] = archivedResource
	}
	c.JSON(200, resp)
}

// ResumeInterruptedVideoJobs re-attaches to doubao/ark tasks left as generating after a crash/restart.
func (sc *ShotController) ResumeInterruptedVideoJobs() {
	var shots []models.Shot
	if err := sc.DB.Where("status = ?", "generating").Find(&shots).Error; err != nil {
		log.Printf("resume video jobs: query failed: %v", err)
		return
	}
	if len(shots) == 0 {
		return
	}
	log.Printf("resume video jobs: found %d interrupted shot(s)", len(shots))
	for i := range shots {
		shotID := shots[i].ID
		go sc.resumeInterruptedVideoJob(shotID)
	}
}

func (sc *ShotController) resumeInterruptedVideoJob(shotID uint) {
	var shot models.Shot
	if err := sc.DB.First(&shot, shotID).Error; err != nil {
		return
	}
	if shot.Status != "generating" {
		return
	}
	taskID := strings.TrimSpace(shot.VideoTaskID)
	if taskID == "" {
		log.Printf("resume shot %d: no video_task_id (created before resume fix); marking error", shotID)
		shot.Status = "error"
		shot.ErrorMessage = "服务重启导致生成中断，请重新点击生成"
		_ = sc.DB.Save(&shot).Error
		return
	}
	ctx, msg, ok := sc.buildGenerateContext(shot)
	if !ok {
		log.Printf("resume shot %d: cannot rebuild context (%s)", shotID, msg)
		shot.Status = "error"
		shot.ErrorMessage = "生成中断后无法恢复：" + msg
		_ = sc.DB.Save(&shot).Error
		return
	}
	log.Printf("resume shot %d: waiting task %s", shotID, taskID)
	remoteURL, err := sc.Ark.WaitVideoTask(ctx.Provider, taskID, func(eta string) {
		sc.persistShotVideoETA(shotID, eta)
	})
	if err != nil {
		// Doubao-web may already have uploaded to COS even if task poll fails.
		if cosURL, ok := sc.tryDoubaoWebCOSURL(taskID); ok {
			log.Printf("resume shot %d: wait failed (%v), adopting COS object %s", shotID, err, cosURL)
			remoteURL = cosURL
		} else {
			log.Printf("resume shot %d: wait failed: %v", shotID, err)
			shot.Status = "error"
			shot.ErrorMessage = err.Error()
			_ = sc.DB.Save(&shot).Error
			return
		}
	}
	archived, _, _ := archiveShotVideoBeforeReplace(sc.DB, sc.Storage, ctx.Project.ID, shot)
	if _, _, err := sc.finalizeShotVideo(shot, ctx.Project, ctx, taskID, remoteURL, archived); err != nil {
		log.Printf("resume shot %d: finalize failed: %v", shotID, err)
	} else {
		log.Printf("resume shot %d: recovered successfully", shotID)
	}
}

func (sc *ShotController) tryDoubaoWebCOSURL(taskID string) (string, bool) {
	if !sc.Storage.COSEnabled() || strings.TrimSpace(taskID) == "" {
		return "", false
	}
	now := time.Now()
	for _, t := range []time.Time{now, now.AddDate(0, -1, 0)} {
		key := fmt.Sprintf("doubao-web/videos/%04d/%02d/%s.mp4", t.Year(), int(t.Month()), taskID)
		if sc.Storage.COS.Exists(key) {
			return sc.Storage.COS.PublicURL(key), true
		}
	}
	return "", false
}

func (sc *ShotController) finalizeShotVideo(
	shot models.Shot,
	project models.Project,
	ctx shotGenerateContext,
	taskID, remoteURL string,
	archivedResource models.Resource,
) (models.Shot, models.Resource, error) {
	var (
		path string
		data []byte
		err  error
	)
	trimExact := services.IsDoubaoWebAPI(ctx.Provider) && needsExactDoubaoTrim(ctx.Input.Duration)
	if srcKey, ok := sc.tryCOSSourceKey(remoteURL); ok && !trimExact {
		adoptStart := time.Now()
		var adoptErr error
		path, adoptErr = sc.Storage.SaveVideoFromCOSKey(project.ID, shot.ID, srcKey, "mp4")
		if adoptErr != nil {
			log.Printf("shot %d: COS adopt failed (%v), falling back to download", shot.ID, adoptErr)
			path = ""
		} else {
			log.Printf("shot %d: adopted COS video via Copy in %s (src=%s)", shot.ID, time.Since(adoptStart).Round(time.Millisecond), srcKey)
		}
	}
	if path == "" {
		dlStart := time.Now()
		data, err = sc.Ark.DownloadVideo(remoteURL)
		if err != nil {
			shot.Status = "error"
			shot.ErrorMessage = "下载视频失败：" + err.Error()
			shot.VideoTaskID = taskID
			_ = sc.DB.Save(&shot).Error
			return shot, models.Resource{}, err
		}
		log.Printf("shot %d: download video %d bytes in %s", shot.ID, len(data), time.Since(dlStart).Round(time.Millisecond))
		if trimExact {
			trimStart := time.Now()
			data, err = trimVideoBytes(data, ctx.Input.Duration)
			if err != nil {
				shot.Status = "error"
				shot.ErrorMessage = fmt.Sprintf("将豆包视频裁剪为 %d 秒失败：%v", ctx.Input.Duration, err)
				_ = sc.DB.Save(&shot).Error
				return shot, models.Resource{}, err
			}
			log.Printf("shot %d: trimmed Doubao source to %ds in %s", shot.ID, ctx.Input.Duration, time.Since(trimStart).Round(time.Millisecond))
		}

		saveStart := time.Now()
		path, err = sc.Storage.SaveVideo(project.ID, shot.ID, data, "mp4")
		if err != nil {
			shot.Status = "error"
			shot.ErrorMessage = "保存视频失败：" + err.Error()
			_ = sc.DB.Save(&shot).Error
			return shot, models.Resource{}, err
		}
		log.Printf("shot %d: save video took %s", shot.ID, time.Since(saveStart).Round(time.Millisecond))
	}

	shot.VideoTaskID = taskID
	shot.VideoURL = sc.Storage.PublicURL("videos", project.ID, shot.ID, "mp4")
	shot.Status = "done"
	shot.ErrorMessage = ""
	shot.VideoETA = ""
	if err = sc.DB.Save(&shot).Error; err != nil {
		return shot, models.Resource{}, err
	}

	meta := &VideoGenMeta{
		Script:       ctx.Input.Script,
		VisualStyle:  ctx.Input.VisualStyle,
		ProjectStyle: firstNonEmpty(ctx.Input.LookPack, ctx.Input.Style),
		ModelName:    ctx.Model.Name,
		ModelID:      ctx.Model.ModelID,
		ProviderName: ctx.Provider.Name,
	}
	var videoResource models.Resource
	if data == nil && sc.Storage.COSEnabled() {
		videoResource, err = createVideoResourceFromCOS(sc.DB, sc.Storage, project.ID, shot, "ai", "mp4", meta, path)
	} else {
		videoResource, err = createVideoResourceFrom(sc.DB, sc.Storage, project.ID, shot, data, "ai", "mp4", meta, path)
	}
	if err != nil {
		if bytes, ext, readErr := sc.Storage.ReadShotVideo(project.ID, shot.ID); readErr == nil && len(bytes) > 0 {
			videoResource, err = createVideoResourceFrom(sc.DB, sc.Storage, project.ID, shot, bytes, "ai", ext, meta, "")
		}
	}
	if err != nil {
		return shot, models.Resource{}, err
	}
	shot.ActiveVideoResourceID = &videoResource.ID
	_ = sc.DB.Save(&shot).Error
	fillShotFields(&shot, sc.Storage)
	_ = archivedResource
	return shot, videoResource, nil
}

func (sc *ShotController) persistShotVideoETA(shotID uint, eta string) {
	eta = strings.TrimSpace(eta)
	if shotID == 0 || eta == "" {
		return
	}
	if err := sc.DB.Model(&models.Shot{}).Where("id = ? AND status = ?", shotID, "generating").Update("video_eta", eta).Error; err != nil {
		log.Printf("shot %d: persist video eta failed: %v", shotID, err)
		return
	}
	log.Printf("shot %d: video eta %q", shotID, eta)
}

func (sc *ShotController) tryCOSSourceKey(remoteURL string) (string, bool) {
	if !sc.Storage.COSEnabled() {
		return "", false
	}
	return sc.Storage.COS.KeyFromPublicURL(remoteURL)
}

func (sc *ShotController) find(c *gin.Context) (models.Shot, bool) {
	var shot models.Shot
	if err := sc.DB.First(&shot, c.Param("id")).Error; err != nil {
		fail(c, 404, "分镜不存在")
		return shot, false
	}
	return shot, true
}

type shotSaveDTO struct {
	models.Shot
	PackedShots []models.Shot `json:"packedShots,omitempty"`
}

func shotUpdatePayload(shot models.Shot, neighbors []models.Shot) shotSaveDTO {
	return shotSaveDTO{Shot: shot, PackedShots: neighbors}
}

func (sc *ShotController) packEpisodeOverflow(episodeID uint) []models.Shot {
	if episodeID == 0 {
		return nil
	}
	var shots []models.Shot
	if err := sc.DB.Where("episode_id = ?", episodeID).Order("sort_order asc, id asc").Find(&shots).Error; err != nil || len(shots) == 0 {
		return nil
	}
	contexts := make([]crew.ShotContext, 0, len(shots))
	for i, s := range shots {
		contexts = append(contexts, crew.ShotContext{
			ID:       s.ID,
			Index:    i + 1,
			Label:    s.Label,
			Script:   s.Script,
			Duration: s.Duration,
		})
	}
	packed := crew.PackShotContexts(contexts)
	byID := map[uint]string{}
	for _, ctx := range packed {
		if ctx.ID > 0 {
			byID[ctx.ID] = ctx.Script
		}
	}
	touched := make([]models.Shot, 0)
	for i := range shots {
		script, ok := byID[shots[i].ID]
		if !ok || script == shots[i].Script {
			continue
		}
		if err := sc.DB.Model(&shots[i]).Update("script", script).Error; err != nil {
			continue
		}
		shots[i].Script = script
		touched = append(touched, shots[i])
	}
	maxOrder := 0
	refSource := shots[len(shots)-1]
	for _, s := range shots {
		if s.SortOrder > maxOrder {
			maxOrder = s.SortOrder
		}
	}
	for _, ctx := range packed {
		if ctx.ID != 0 {
			for _, s := range shots {
				if s.ID == ctx.ID {
					refSource = s
					break
				}
			}
			continue
		}
		if strings.TrimSpace(ctx.Script) == "" {
			continue
		}
		maxOrder++
		created := models.Shot{
			EpisodeID:         episodeID,
			SortOrder:         maxOrder,
			Label:             firstNonEmpty(ctx.Label, "续"),
			Script:            ctx.Script,
			Duration:          crew.ShotMaxSeconds(ctx.Duration),
			Resolution:        firstNonEmpty(refSource.Resolution, "720p"),
			Status:            "draft",
			RefsJSON:          refSource.RefsJSON,
			CharacterRefsJSON: refSource.CharacterRefsJSON,
			CharacterIDsJSON:  "[]",
			SceneID:           refSource.SceneID,
		}
		if err := sc.DB.Create(&created).Error; err != nil {
			continue
		}
		touched = append(touched, created)
	}
	return touched
}
