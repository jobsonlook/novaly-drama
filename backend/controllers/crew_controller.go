package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"novaly/backend/models"
	"novaly/backend/services"
	"novaly/backend/services/crew"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type CrewController struct {
	DB       *gorm.DB
	Ark      *services.ArkService
	Storage  *services.Storage
	Resource *ResourceController
}

func (cc *CrewController) Extract(c *gin.Context) {
	projectID := parseID(c.Param("id"))
	if projectID == 0 {
		fail(c, 404, "项目不存在")
		return
	}
	var project models.Project
	if err := cc.DB.First(&project, projectID).Error; err != nil {
		fail(c, 404, "项目不存在")
		return
	}
	var input struct {
		EpisodeIDs []uint `json:"episodeIds"`
	}
	_ = c.ShouldBindJSON(&input)

	q := cc.DB.Where("project_id = ?", projectID)
	if len(input.EpisodeIDs) > 0 {
		q = q.Where("id IN ?", input.EpisodeIDs)
	}
	var episodes []models.Episode
	if err := q.Order("number asc").Find(&episodes).Error; err != nil {
		fail(c, 500, "读取分集失败")
		return
	}
	if len(episodes) == 0 {
		fail(c, 400, "请先选择要提取的剧本")
		return
	}

	started := make([]uint, 0, len(episodes))
	skipped := make([]gin.H, 0)
	sem := make(chan struct{}, 2)
	for _, ep := range episodes {
		if strings.TrimSpace(ep.Script) == "" {
			skipped = append(skipped, gin.H{"id": ep.ID, "number": ep.Number, "reason": "未填写剧本"})
			continue
		}
		var running int64
		cc.DB.Model(&models.CrewJob{}).Where("episode_id = ? AND status = ?", ep.ID, "running").Count(&running)
		if running > 0 {
			skipped = append(skipped, gin.H{"id": ep.ID, "number": ep.Number, "reason": "提取进行中"})
			continue
		}
		job := models.CrewJob{
			ProjectID:    projectID,
			EpisodeID:    ep.ID,
			Status:       "running",
			Stage:        "director",
			SourceScript: ep.Script,
			ScriptDraft:  ep.Script,
		}
		if err := cc.DB.Create(&job).Error; err != nil {
			skipped = append(skipped, gin.H{"id": ep.ID, "number": ep.Number, "reason": "创建任务失败"})
			continue
		}
		started = append(started, ep.ID)
		jobID := job.ID
		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			cc.runDirectorAndConsistency(jobID)
		}()
	}
	if len(started) == 0 {
		fail(c, 400, "没有可提取的剧本，请先填写内容")
		return
	}
	c.JSON(202, gin.H{"started": started, "skipped": skipped})
}

func (cc *CrewController) Get(c *gin.Context) {
	episode, ok := cc.findEpisode(c)
	if !ok {
		return
	}
	job, ok := cc.latestJob(episode.ID)
	if !ok {
		c.JSON(200, gin.H{"job": nil, "shotCount": cc.shotCount(episode.ID)})
		return
	}
	c.JSON(200, cc.jobResponse(job, episode))
}

func (cc *CrewController) Chat(c *gin.Context) {
	episode, ok := cc.findEpisode(c)
	if !ok {
		return
	}
	var input struct {
		Text          string `json:"text"`
		ThinkingLevel string `json:"thinkingLevel"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, 400, "参数无效")
		return
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		fail(c, 400, "请输入内容")
		return
	}
	job, err := cc.ensureChatJob(episode)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	if job.Status == "running" {
		fail(c, 409, "剧组任务进行中，请稍后再聊")
		return
	}

	provider, model, errMsg := cc.loadTextModel()
	if errMsg != "" {
		fail(c, 400, errMsg)
		return
	}
	script := strings.TrimSpace(job.ScriptDraft)
	if script == "" {
		script = strings.TrimSpace(episode.Script)
	}
	assets := cc.enrichFixAssets(job.ProjectID, cc.decodeAssets(job))
	shots, contexts := cc.loadQCContexts(episode.ID)
	history := crew.DecodeChat(job.ChatJSON)
	lastIssues := cc.decodeQCIssues(job)
	plan := crew.PlanChat(cc.Ark, provider, model, text, script, contexts, assets, history, lastIssues, input.ThinkingLevel)

	userMsg := crew.NewChatMessage("user", "", text, "")
	messages := crew.AppendChat(history, userMsg)
	if plan.Action == "reply" {
		reply := strings.TrimSpace(plan.Reply)
		if reply == "" {
			reply = "可以说「开始拆镜」「质检本集」「按上次建议修改」，或指定某一镜要改什么。"
		}
		messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNamePlanner, reply, "reply"))
	}

	switch plan.Action {
	case "split":
		if len(shots) > 0 && !plan.Replace {
			messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNamePlanner, "本集已有分镜。要整集重拆请说「替换分镜」。", "reply"))
			break
		}
		created, splitErr := cc.splitStoryboardForChat(job, episode, plan.Replace)
		if splitErr != nil {
			messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNameDirector, "拆镜失败："+splitErr.Error(), "split"))
			break
		}
		messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNameDirector, fmt.Sprintf("已写入 %d 条分镜。监制开始对照剧本审核。", len(created)), "split"))
		job, _ = cc.reload(job.ID)
		report, qcErr := cc.reviewQCNow(job, nil, nil)
		if qcErr != nil {
			messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNameSupervisor, "质检失败："+qcErr.Error(), "qc"))
			break
		}
		messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNameSupervisor, crew.FormatQCReport(report), "qc"))
	case "qc":
		report, qcErr := cc.reviewQCNow(job, nil, nil)
		if qcErr != nil {
			messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNameSupervisor, "质检失败："+qcErr.Error(), "qc"))
			break
		}
		messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNameSupervisor, crew.FormatQCReport(report), "qc"))
	case "fix":
		issues := crew.ResolvePlanIssues(plan, contexts, lastIssues)
		if len(issues) == 0 {
			messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNamePlanner, "没有可改的质检项。先说「质检本集」，或指定要改的镜号。", "reply"))
			break
		}
		leftover, err := cc.applyFixesNow(job, issues)
		if err != nil {
			messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNameDirector, "改稿失败："+err.Error(), "fix"))
			break
		}
		messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNameDirector, fmt.Sprintf("已按确认项改了 %d 处，其它镜头未动。监制复检这些位置。", len(issues)), "fix"))
		job, _ = cc.reload(job.ID)
		report, qcErr := cc.reviewQCNow(job, leftover, issues)
		if qcErr != nil {
			messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNameSupervisor, "复检失败："+qcErr.Error(), "qc"))
			break
		}
		messages = crew.AppendChat(messages, crew.NewChatMessage("assistant", crew.ChatNameSupervisor, crew.FormatQCReport(report), "qc"))
	}

	cc.patchJob(job.ID, map[string]any{"chat_json": crew.EncodeChat(messages)})
	updated, _ := cc.reload(job.ID)
	c.JSON(200, cc.jobResponse(updated, episode))
}

// ClearMemory clears conversational context without touching scripts, shots,
// assets, or generated media. The QC report acts as the workflow's summary
// memory; chat_json is the verbatim message memory.
func (cc *CrewController) ClearMemory(c *gin.Context) {
	episode, ok := cc.findEpisode(c)
	if !ok {
		return
	}
	job, ok := cc.latestJob(episode.ID)
	if !ok {
		c.JSON(200, gin.H{"job": nil, "shotCount": cc.shotCount(episode.ID)})
		return
	}
	if job.Status == "running" {
		fail(c, 409, "剧组任务进行中，暂不能清除记忆")
		return
	}
	var input struct {
		Scope string `json:"scope"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, 400, "参数无效")
		return
	}
	updates := map[string]any{}
	switch strings.ToLower(strings.TrimSpace(input.Scope)) {
	case "messages":
		updates["chat_json"] = "[]"
	case "summary":
		updates["qc_report_json"] = ""
	case "all":
		updates["chat_json"] = "[]"
		updates["qc_report_json"] = ""
	default:
		fail(c, 400, "scope 仅支持 messages、summary 或 all")
		return
	}
	cc.patchJob(job.ID, updates)
	updated, _ := cc.reload(job.ID)
	c.JSON(200, cc.jobResponse(updated, episode))
}

func (cc *CrewController) Start(c *gin.Context) {
	episode, ok := cc.findEpisode(c)
	if !ok {
		return
	}
	if existing, found := cc.latestJob(episode.ID); found {
		if existing.Status == "running" || existing.Status == "waiting_review" || existing.Status == "failed" {
			c.JSON(200, cc.jobResponse(existing, episode))
			return
		}
	}

	var input struct {
		Script string `json:"script"`
	}
	_ = c.ShouldBindJSON(&input)
	script := strings.TrimSpace(input.Script)
	if script == "" {
		script = strings.TrimSpace(episode.Script)
	}
	if script == "" {
		fail(c, 400, "请先粘贴本集剧本")
		return
	}
	if err := cc.DB.Model(&episode).Update("script", script).Error; err != nil {
		fail(c, 500, "保存剧本失败")
		return
	}

	job := models.CrewJob{
		ProjectID:    episode.ProjectID,
		EpisodeID:    episode.ID,
		Status:       "running",
		Stage:        "screenwriter",
		SourceScript: script,
		ScriptDraft:  script,
	}
	if err := cc.DB.Create(&job).Error; err != nil {
		fail(c, 500, "创建剧组任务失败")
		return
	}
	go cc.runScreenwriter(job.ID)
	c.JSON(202, cc.jobResponse(job, episode))
}

func (cc *CrewController) Continue(c *gin.Context) {
	episode, ok := cc.findEpisode(c)
	if !ok {
		return
	}
	job, ok := cc.latestJob(episode.ID)
	if !ok {
		fail(c, 404, "还没有剧组任务")
		return
	}
	if job.Status == "running" {
		c.JSON(200, cc.jobResponse(job, episode))
		return
	}
	if job.Status == "completed" {
		fail(c, 400, "本轮剧组任务已完成")
		return
	}
	if job.Status != "waiting_review" {
		fail(c, 400, "当前阶段完成后才能继续")
		return
	}

	var input struct {
		Script     string           `json:"script"`
		Plan       string           `json:"plan"`
		Assets     []crew.AssetItem `json:"assets"`
		ShotMode   string           `json:"shotMode"`
		SkipImages bool             `json:"skipImages"`
	}
	_ = c.ShouldBindJSON(&input)

	switch job.Stage {
	case "screenwriter":
		script := strings.TrimSpace(input.Script)
		if script == "" {
			script = strings.TrimSpace(job.ScriptDraft)
		}
		if script == "" {
			fail(c, 400, "剧本为空")
			return
		}
		cc.patchJob(job.ID, map[string]any{
			"script_draft":  script,
			"status":        "running",
			"stage":         "director",
			"error_message": "",
		})
		_ = cc.DB.Model(&episode).Update("script", script).Error
		go cc.runDirectorAndConsistency(job.ID)
	case "director", "consistency":
		assets := input.Assets
		if len(assets) == 0 {
			assets = cc.decodeAssets(job)
		}
		plan := strings.TrimSpace(input.Plan)
		if plan == "" {
			plan = job.DirectorPlan
		}
		cc.upsertExtractedResources(episode.ProjectID, assets)
		assets = cc.markReusableAssets(episode.ProjectID, assets)
		if input.SkipImages || crewAssetsAllImageReady(assets) {
			for i := range assets {
				assets[i].Skipped = true
			}
			input.SkipImages = true
		}
		raw, _ := json.Marshal(assets)
		if input.SkipImages {
			shotCount := cc.shotCount(episode.ID)
			mode := strings.TrimSpace(input.ShotMode)
			if shotCount > 0 && mode != "replace" && mode != "append" {
				cc.patchJob(job.ID, map[string]any{
					"director_plan": plan,
					"assets_json":   string(raw),
				})
				c.JSON(409, gin.H{
					"error":     "本集已有分镜，请选择替换或追加",
					"shotCount": shotCount,
					"job":       cc.jobResponse(job, episode)["job"],
				})
				return
			}
			if mode == "" {
				mode = "replace"
			}
			cc.patchJob(job.ID, map[string]any{
				"director_plan": plan,
				"assets_json":   string(raw),
				"shot_mode":     mode,
				"status":        "running",
				"stage":         "storyboard",
				"error_message": "",
			})
			if plan != "" {
				_ = cc.DB.Model(&episode).Update("director_plan", plan).Error
			}
			go cc.runStoryboard(job.ID)
			break
		}
		cc.patchJob(job.ID, map[string]any{
			"director_plan": plan,
			"assets_json":   string(raw),
			"status":        "running",
			"stage":         "assets",
			"error_message": "",
		})
		if plan != "" {
			_ = cc.DB.Model(&episode).Update("director_plan", plan).Error
		}
		go cc.runAssetImages(job.ID)
	case "assets":
		shotCount := cc.shotCount(episode.ID)
		mode := strings.TrimSpace(job.ShotMode)
		if shotCount > 0 && mode != "replace" && mode != "append" {
			c.JSON(409, gin.H{
				"error":     "本集已有分镜，请选择替换或追加",
				"shotCount": shotCount,
				"job":       cc.jobResponse(job, episode)["job"],
			})
			return
		}
		if mode == "" {
			mode = "replace"
		}
		cc.patchJob(job.ID, map[string]any{
			"shot_mode":     mode,
			"status":        "running",
			"stage":         "storyboard",
			"error_message": "",
		})
		go cc.runStoryboard(job.ID)
	case "storyboard":
		cc.patchJob(job.ID, map[string]any{
			"status":        "running",
			"stage":         "qc",
			"error_message": "",
		})
		go cc.runQC(job.ID)
	case "qc":
		cc.patchJob(job.ID, map[string]any{"status": "completed", "error_message": ""})
	default:
		fail(c, 400, "未知阶段")
		return
	}

	updated, _ := cc.reload(job.ID)
	c.JSON(202, cc.jobResponse(updated, episode))
}

// ResplitFrom keeps shots before fromShotId (including their videos) and
// re-splits the episode story from that shot onward.
func (cc *CrewController) ResplitFrom(c *gin.Context) {
	episode, ok := cc.findEpisode(c)
	if !ok {
		return
	}
	var input struct {
		FromShotID uint `json:"fromShotId"`
	}
	if c.ShouldBindJSON(&input) != nil || input.FromShotID == 0 {
		fail(c, 400, "请指定 fromShotId")
		return
	}
	var from models.Shot
	if err := cc.DB.First(&from, input.FromShotID).Error; err != nil || from.EpisodeID != episode.ID {
		fail(c, 404, "分镜不存在")
		return
	}
	job, err := cc.ensureChatJob(episode)
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	if job.Status == "running" {
		fail(c, 409, "剧组任务进行中，请稍后再试")
		return
	}
	script := strings.TrimSpace(job.ScriptDraft)
	if script == "" {
		script = strings.TrimSpace(episode.Script)
	}
	if script == "" {
		fail(c, 400, "本集还没有剧本，无法续拆分镜")
		return
	}
	plan := strings.TrimSpace(job.DirectorPlan)
	assets := cc.enrichFixAssets(episode.ProjectID, cc.decodeAssets(job))
	if raw, mErr := json.Marshal(assets); mErr == nil {
		cc.patchJob(job.ID, map[string]any{"assets_json": string(raw)})
	}
	cc.patchJob(job.ID, map[string]any{
		"script_draft":  script,
		"director_plan": plan,
		"shot_mode":     "from",
		"from_shot_id":  from.ID,
		"status":        "running",
		"stage":         "storyboard",
		"error_message": "",
	})
	go cc.runStoryboard(job.ID)
	updated, _ := cc.reload(job.ID)
	c.JSON(202, cc.jobResponse(updated, episode))
}

func (cc *CrewController) Retry(c *gin.Context) {
	episode, ok := cc.findEpisode(c)
	if !ok {
		return
	}
	job, ok := cc.latestJob(episode.ID)
	if !ok {
		fail(c, 404, "还没有剧组任务")
		return
	}
	if job.Status == "running" {
		fail(c, 400, "任务进行中")
		return
	}
	stage := job.Stage
	patch := map[string]any{"status": "running", "error_message": ""}
	if stage == "storyboard" && cc.shotCount(episode.ID) > 0 {
		patch["shot_mode"] = "replace"
	}
	cc.patchJob(job.ID, patch)
	switch stage {
	case "screenwriter":
		go cc.runScreenwriter(job.ID)
	case "director", "consistency":
		cc.patchJob(job.ID, map[string]any{"stage": "director"})
		go cc.runDirectorAndConsistency(job.ID)
	case "assets":
		go cc.runAssetImages(job.ID)
	case "storyboard":
		go cc.runStoryboard(job.ID)
	case "qc":
		go cc.runQC(job.ID)
	default:
		fail(c, 400, "未知阶段")
		return
	}
	updated, _ := cc.reload(job.ID)
	c.JSON(202, cc.jobResponse(updated, episode))
}

func crewStageIndex(stage string) int {
	switch stage {
	case "screenwriter":
		return 0
	case "director", "consistency":
		return 1
	case "assets":
		return 2
	case "storyboard":
		return 3
	case "qc":
		return 4
	default:
		return 0
	}
}

func (cc *CrewController) Rewind(c *gin.Context) {
	episode, ok := cc.findEpisode(c)
	if !ok {
		return
	}
	job, ok := cc.latestJob(episode.ID)
	if !ok {
		fail(c, 404, "还没有剧组任务")
		return
	}
	if job.Status == "running" {
		fail(c, 400, "任务进行中")
		return
	}
	var input struct {
		Stage string `json:"stage"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, 400, "参数无效")
		return
	}
	stage := strings.TrimSpace(input.Stage)
	if stage == "director" {
		stage = "consistency"
	}
	switch stage {
	case "screenwriter", "consistency", "assets", "storyboard", "qc":
	default:
		fail(c, 400, "未知阶段")
		return
	}
	reached := crewStageIndex(job.Stage)
	if job.Status == "completed" {
		reached = 4
	}
	if crewStageIndex(stage) > reached {
		fail(c, 400, "还没到这一步")
		return
	}
	patch := map[string]any{
		"status":        "waiting_review",
		"stage":         stage,
		"error_message": "",
	}
	if stage == "consistency" || stage == "assets" || stage == "storyboard" {
		patch["shot_mode"] = "replace"
	}
	cc.patchJob(job.ID, patch)
	updated, _ := cc.reload(job.ID)
	c.JSON(200, cc.jobResponse(updated, episode))
}

func (cc *CrewController) runScreenwriter(jobID uint) {
	job, ok := cc.reload(jobID)
	if !ok {
		return
	}
	provider, model, errMsg := cc.loadTextModel()
	if errMsg != "" {
		cc.failJob(jobID, errMsg)
		return
	}
	var project models.Project
	_ = cc.DB.First(&project, job.ProjectID).Error
	source := strings.TrimSpace(job.SourceScript)
	if source == "" {
		source = job.ScriptDraft
	}
	script, err := crew.PolishScript(cc.Ark, provider, model, source, project)
	if err != nil {
		cc.failJob(jobID, "编剧失败："+err.Error())
		return
	}
	cc.patchJob(jobID, map[string]any{
		"script_draft":  script,
		"status":        "waiting_review",
		"stage":         "screenwriter",
		"error_message": "",
	})
	_ = cc.DB.Model(&models.Episode{}).Where("id = ?", job.EpisodeID).Update("script", script).Error
}

func (cc *CrewController) runDirectorAndConsistency(jobID uint) {
	job, ok := cc.reload(jobID)
	if !ok {
		return
	}
	provider, model, errMsg := cc.loadTextModel()
	if errMsg != "" {
		cc.failJob(jobID, errMsg)
		return
	}
	var project models.Project
	_ = cc.DB.First(&project, job.ProjectID).Error
	script := strings.TrimSpace(job.ScriptDraft)
	if script == "" {
		var ep models.Episode
		if err := cc.DB.Select("script").First(&ep, job.EpisodeID).Error; err == nil {
			script = ep.Script
		}
	}
	plan, err := crew.PlanAndExtract(cc.Ark, provider, model, script, project)
	if err != nil {
		cc.failJob(jobID, "导演失败："+err.Error())
		return
	}
	assets := crew.MergeExported(plan)
	assets = cc.fillRecurringCharacterDetails(assets, script, provider, model, project)
	polished, consErr := crew.PolishVisualPrompts(cc.Ark, provider, model, assets, project)
	if consErr != nil {
		log.Printf("crew %d consistency: %v", jobID, consErr)
	} else {
		assets = polished
	}
	if voiced, vErr := crew.FillCharacterVoices(cc.Ark, provider, model, assets, script); vErr != nil {
		log.Printf("crew %d voices: %v", jobID, vErr)
	} else {
		assets = voiced
	}
	raw, _ := json.Marshal(assets)
	cc.patchJob(jobID, map[string]any{
		"director_plan": plan.Plan,
		"assets_json":   string(raw),
		"status":        "waiting_review",
		"stage":         "consistency",
		"error_message": "",
	})
	cc.upsertExtractedResources(job.ProjectID, assets)
	raw, _ = json.Marshal(assets)
	cc.patchJob(jobID, map[string]any{"assets_json": string(raw)})

	derived, derErr := crew.AnalyzeDerivatives(cc.Ark, provider, model, script, assets, project)
	if derErr != nil {
		log.Printf("crew %d derive: %v", jobID, derErr)
	} else if len(derived) > 0 {
		polishedDer, pErr := crew.PolishVisualPrompts(cc.Ark, provider, model, derived, project)
		if pErr != nil {
			log.Printf("crew %d derive polish: %v", jobID, pErr)
		} else {
			derived = polishedDer
		}
		cc.upsertExtractedResources(job.ProjectID, derived)
		assets = append(assets, derived...)
		raw, _ = json.Marshal(assets)
		cc.patchJob(jobID, map[string]any{"assets_json": string(raw)})
	}

	_ = cc.DB.Model(&models.Episode{}).Where("id = ?", job.EpisodeID).Updates(map[string]any{
		"director_plan": plan.Plan,
		"assets_json":   string(raw),
	}).Error

	assets = cc.markReusableAssets(job.ProjectID, assets)
	raw, _ = json.Marshal(assets)
	cc.patchJob(jobID, map[string]any{"assets_json": string(raw)})
}

func (cc *CrewController) runAssetImages(jobID uint) {
	job, ok := cc.reload(jobID)
	if !ok {
		return
	}
	assets := cc.decodeAssets(job)
	if len(assets) == 0 {
		cc.patchJob(jobID, map[string]any{
			"status": "waiting_review",
			"stage":  "assets",
		})
		return
	}

	sem := make(chan struct{}, 2)
	var mu sync.Mutex
	var wg sync.WaitGroup
	jobIDs := make([]uint, 0)

	for i := range assets {
		item := &assets[i]
		if item.Skipped || item.Reused {
			continue
		}
		if cc.tryReuseAssetImage(job.ProjectID, item) {
			continue
		}
		prompt := strings.TrimSpace(item.Prompt)
		if prompt == "" {
			prompt = strings.TrimSpace(item.Description)
		}
		if prompt == "" {
			item.Error = "缺少描述，已跳过生图"
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			input := imageGenJobInput{
				Name:             assets[idx].Name,
				Description:      firstNonEmpty(assets[idx].Prompt, assets[idx].Description),
				Count:            1,
				Resolution:       "1k",
				TargetResourceID: assets[idx].ResourceID,
			}
			if assets[idx].ParentID > 0 {
				input.ResourceRefs = []imageGenResourceRef{{ID: assets[idx].ParentID, Variant: "original"}}
			}
			imgJob, _, errMsg := cc.Resource.enqueueImageGenerationJob(job.ProjectID, assets[idx].Type, input)
			if errMsg != "" {
				mu.Lock()
				assets[idx].Error = errMsg
				mu.Unlock()
				return
			}
			mu.Lock()
			assets[idx].JobID = imgJob.ID
			jobIDs = append(jobIDs, imgJob.ID)
			mu.Unlock()
			finished := cc.waitImageJob(imgJob.ID, 25*time.Minute)
			if finished.Status != "completed" {
				mu.Lock()
				assets[idx].Error = firstNonEmpty(finished.ErrorMessage, firstNonEmpty(finished.Message, "生图失败"))
				mu.Unlock()
				return
			}
			if res, found := cc.findLibraryResource(job.ProjectID, assets[idx].Type, assets[idx].Name); found {
				mu.Lock()
				assets[idx].ResourceID = res.ID
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	raw, _ := json.Marshal(assets)
	idsJSON, _ := json.Marshal(jobIDs)
	// Nothing left to generate — skip the empty「生图」review and go straight to split.
	if len(jobIDs) == 0 && crewAssetsAllImageReady(assets) {
		cc.patchJob(jobID, map[string]any{
			"assets_json":        string(raw),
			"image_job_ids_json": string(idsJSON),
			"status":             "running",
			"stage":              "storyboard",
			"error_message":      "",
		})
		go cc.runStoryboard(jobID)
		return
	}
	cc.patchJob(jobID, map[string]any{
		"assets_json":        string(raw),
		"image_job_ids_json": string(idsJSON),
		"status":             "waiting_review",
		"stage":              "assets",
		"error_message":      "",
	})
}

func (cc *CrewController) runStoryboard(jobID uint) {
	job, ok := cc.reload(jobID)
	if !ok {
		return
	}
	provider, model, errMsg := cc.loadTextModel()
	if errMsg != "" {
		cc.failJob(jobID, errMsg)
		return
	}
	var project models.Project
	_ = cc.DB.First(&project, job.ProjectID).Error
	script := strings.TrimSpace(job.ScriptDraft)
	if script == "" {
		var ep models.Episode
		if err := cc.DB.Select("script").First(&ep, job.EpisodeID).Error; err == nil {
			script = ep.Script
		}
	}
	assets := cc.decodeAssets(job)
	kept := []crew.KeptShot{}
	mode := strings.TrimSpace(job.ShotMode)
	if mode == "from" {
		var from models.Shot
		if job.FromShotID == 0 || cc.DB.First(&from, job.FromShotID).Error != nil || from.EpisodeID != job.EpisodeID {
			cc.failJob(jobID, "分镜失败：未找到续拆起点分镜")
			return
		}
		var prior []models.Shot
		cc.DB.Where("episode_id = ? AND (sort_order < ? OR (sort_order = ? AND id < ?))",
			job.EpisodeID, from.SortOrder, from.SortOrder, from.ID).
			Order("sort_order asc, id asc").Find(&prior)
		for _, s := range prior {
			kept = append(kept, crew.KeptShot{Label: s.Label, Script: s.Script})
		}
	}
	result, err := crew.SplitStoryboardContinuing(cc.Ark, provider, model, script, job.DirectorPlan, project, assets, kept)
	if err != nil {
		cc.failJob(jobID, "分镜失败："+err.Error())
		return
	}
	shotScripts := make([]string, 0, len(result.Shots))
	for _, sh := range result.Shots {
		shotScripts = append(shotScripts, sh.Script)
	}
	combinedScript := crew.CombineScripts(append([]string{script}, shotScripts...)...)
	assets, addedNames := cc.enrichRecurringCharactersFromScript(job.ProjectID, assets, combinedScript, provider, model, project)
	if len(addedNames) > 0 {
		raw, _ := json.Marshal(assets)
		cc.patchJob(jobID, map[string]any{"assets_json": string(raw)})
		cc.enqueueRecurringCharacterImages(job.ProjectID, assets, addedNames)
	}
	// Assets JSON can predate derivatives generated later in the resource
	// library. Include every image-ready costume/state before auto binding.
	assets = cc.enrichFixAssets(job.ProjectID, assets)

	if mode == "from" {
		var from models.Shot
		if err := cc.DB.First(&from, job.FromShotID).Error; err != nil {
			cc.failJob(jobID, "分镜失败：续拆起点已不存在")
			return
		}
		if err := cc.DB.Where("episode_id = ? AND (sort_order > ? OR (sort_order = ? AND id >= ?))",
			job.EpisodeID, from.SortOrder, from.SortOrder, from.ID).
			Delete(&models.Shot{}).Error; err != nil {
			cc.failJob(jobID, "删除后续分镜失败："+err.Error())
			return
		}
	} else if mode != "append" {
		if err := cc.DB.Where("episode_id = ?", job.EpisodeID).Delete(&models.Shot{}).Error; err != nil {
			cc.failJob(jobID, "清空旧分镜失败："+err.Error())
			return
		}
	}

	var maxOrder int
	cc.DB.Model(&models.Shot{}).Where("episode_id = ?", job.EpisodeID).Select("coalesce(max(sort_order), 0)").Scan(&maxOrder)

	createdIDs := make([]uint, 0, len(result.Shots))
	for i, item := range result.Shots {
		refs := cc.bindShotRefs(job.ProjectID, assets, item)
		shot := models.Shot{
			EpisodeID:         job.EpisodeID,
			SortOrder:         maxOrder + i + 1,
			Label:             item.Label,
			Script:            item.Script,
			Duration:          item.Duration,
			Resolution:        "720p",
			Status:            "draft",
			RefsJSON:          encodeShotRefs(refs),
			CharacterRefsJSON: encodeCharacterRefs(shotRefsToCharacterRefs(refs)),
			CharacterIDsJSON:  "[]",
		}
		if sid := shotRefsFirstSceneID(refs); sid != nil {
			shot.SceneID = sid
		}
		if err := cc.DB.Create(&shot).Error; err != nil {
			cc.failJob(jobID, "创建分镜失败："+err.Error())
			return
		}
		createdIDs = append(createdIDs, shot.ID)
	}
	// Resolve deterministic QC issues before showing the supervisor report.
	// This binds refs missed by the model and splits dialogue to the available
	// beat duration, instead of asking the user to approve mechanical fixes.
	coveredScripts := make([]string, 0, len(kept))
	for _, k := range kept {
		coveredScripts = append(coveredScripts, k.Script)
	}
	if mode == "from" && len(createdIDs) > 0 {
		var newShots []models.Shot
		cc.DB.Where("id IN ?", createdIDs).Order("sort_order asc, id asc").Find(&newShots)
		contexts := make([]crew.ShotContext, 0, len(newShots))
		for i, s := range newShots {
			contexts = append(contexts, crew.ShotContext{
				ID: s.ID, Index: i + 1, Label: s.Label, Script: s.Script, Duration: s.Duration,
			})
		}
		prepared := crew.PrepareShotsForQCCovered(contexts, cc.enrichFixAssets(job.ProjectID, assets), script, coveredScripts)
		if err := cc.saveQCContexts(job.ProjectID, newShots, prepared); err != nil {
			cc.failJob(jobID, "分镜自动校正失败："+err.Error())
			return
		}
	} else if shots, contexts := cc.loadQCContexts(job.EpisodeID); len(shots) > 0 {
		prepared := crew.PrepareShotsForQC(contexts, cc.enrichFixAssets(job.ProjectID, assets), script)
		if err := cc.saveQCContexts(job.ProjectID, shots, prepared); err != nil {
			cc.failJob(jobID, "分镜自动校正失败："+err.Error())
			return
		}
	}
	idsJSON, _ := json.Marshal(createdIDs)
	chat := crew.DecodeChat(job.ChatJSON)
	msg := fmt.Sprintf("已写入 %d 条分镜。可以说「质检本集」或「替换分镜」。", len(createdIDs))
	if mode == "from" {
		msg = fmt.Sprintf("已从指定分镜续拆，新写入 %d 条；前面分镜与成片未改动。可以说「质检本集」。", len(createdIDs))
	}
	if len(chat) == 0 {
		chat = crew.AppendChat(chat, crew.NewChatMessage("assistant", crew.ChatNameDirector, msg, "split"))
	}
	cc.patchJob(jobID, map[string]any{
		"shot_ids_json": string(idsJSON),
		"status":        "waiting_review",
		"stage":         "storyboard",
		"error_message": "",
		"chat_json":     crew.EncodeChat(chat),
	})
}

func (cc *CrewController) runQC(jobID uint) {
	cc.runQCMerged(jobID, nil, nil)
}

func (cc *CrewController) runQCMerged(jobID uint, leftover, previous []crew.QCIssue) {
	job, ok := cc.reload(jobID)
	if !ok {
		return
	}
	provider, model, errMsg := cc.loadTextModel()
	if errMsg != "" {
		cc.failJob(jobID, errMsg)
		return
	}
	script := strings.TrimSpace(job.ScriptDraft)
	if script == "" {
		var ep models.Episode
		if err := cc.DB.Select("script").First(&ep, job.EpisodeID).Error; err == nil {
			script = ep.Script
		}
	}
	packed := false
	var shots []models.Shot
	var contexts []crew.ShotContext
	if didPack, packedShots, packedContexts, packErr := cc.packQCDuration(job.ProjectID, job.EpisodeID); packErr != nil {
		cc.failJob(jobID, "质检失败："+packErr.Error())
		return
	} else {
		packed = didPack
		shots, contexts = packedShots, packedContexts
	}
	assets := cc.enrichFixAssets(job.ProjectID, cc.decodeAssets(job))
	report, err := crew.ReviewQualityAgainst(cc.Ark, provider, model, script, assets, contexts, previous)
	if err != nil {
		cc.failJob(jobID, "质检失败："+err.Error())
		return
	}
	for i := range report.Issues {
		if report.Issues[i].ShotID == 0 && report.Issues[i].ShotIndex > 0 && report.Issues[i].ShotIndex <= len(shots) {
			report.Issues[i].ShotID = shots[report.Issues[i].ShotIndex-1].ID
		}
	}
	if len(previous) > 0 {
		report = crew.MergeQCAfterFix(report, previous, leftover)
	}
	report = withDurationPackNote(report, packed)
	raw, _ := json.Marshal(report)
	cc.patchJob(jobID, map[string]any{
		"qc_report_json": string(raw),
		"status":         "waiting_review",
		"stage":          "qc",
		"error_message":  "",
	})
}

func (cc *CrewController) Fix(c *gin.Context) {
	episode, ok := cc.findEpisode(c)
	if !ok {
		return
	}
	job, ok := cc.latestJob(episode.ID)
	if !ok {
		fail(c, 404, "还没有剧组任务")
		return
	}
	if job.Status == "running" {
		c.JSON(200, cc.jobResponse(job, episode))
		return
	}
	if job.Stage != "qc" {
		fail(c, 400, "当前不是质检阶段")
		return
	}
	if job.Status != "waiting_review" && job.Status != "completed" && job.Status != "failed" {
		fail(c, 400, "质检完成后才能按建议修改")
		return
	}
	var input struct {
		Issues []crew.QCIssue `json:"issues"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, 400, "参数无效")
		return
	}
	issues := input.Issues
	if len(issues) == 0 {
		if strings.TrimSpace(job.QCReportJSON) != "" {
			var report crew.QCReport
			if json.Unmarshal([]byte(job.QCReportJSON), &report) == nil {
				issues = report.Issues
			}
		}
	}
	if len(issues) == 0 {
		fail(c, 400, "没有可修改的质检问题")
		return
	}
	raw, _ := json.Marshal(issues)
	n := len(issues)
	messages := crew.AppendChat(crew.DecodeChat(job.ChatJSON),
		crew.NewChatMessage("user", "", fmt.Sprintf("按建议修改选中的 %d 项", n), "fix"),
		crew.NewChatMessage("assistant", crew.ChatNameDirector, fmt.Sprintf("收到，正在改这 %d 项，其它镜头不动。改完监制会复检。", n), "fix"),
	)
	cc.patchJob(job.ID, map[string]any{
		"status":        "running",
		"stage":         "qc",
		"error_message": "",
		"chat_json":     crew.EncodeChat(messages),
	})
	go cc.runQCFix(job.ID, raw)
	updated, _ := cc.reload(job.ID)
	c.JSON(202, cc.jobResponse(updated, episode))
}

func (cc *CrewController) runQCFix(jobID uint, issuesJSON []byte) {
	cc.runQCFixPass(jobID, issuesJSON, 0)
}

func (cc *CrewController) runQCFixPass(jobID uint, issuesJSON []byte, pass int) {
	job, ok := cc.reload(jobID)
	if !ok {
		return
	}
	var issues []crew.QCIssue
	if err := json.Unmarshal(issuesJSON, &issues); err != nil || len(issues) == 0 {
		cc.failJob(jobID, "改稿失败：没有可应用的问题")
		cc.appendChat(jobID, crew.NewChatMessage("assistant", crew.ChatNameDirector, "改稿失败：没有可应用的问题。", "fix"))
		return
	}
	assets := cc.enrichFixAssets(job.ProjectID, cc.decodeAssets(job))
	shots, contexts := cc.loadQCContexts(job.EpisodeID)
	if len(shots) == 0 {
		cc.failJob(jobID, "改稿失败：本集还没有分镜")
		cc.appendChat(jobID, crew.NewChatMessage("assistant", crew.ChatNameDirector, "改稿失败：本集还没有分镜。", "fix"))
		return
	}
	for i := range issues {
		if issues[i].ShotID == 0 && issues[i].ShotIndex > 0 && issues[i].ShotIndex <= len(shots) {
			issues[i].ShotID = shots[issues[i].ShotIndex-1].ID
		}
	}
	var previous crew.QCReport
	if strings.TrimSpace(job.QCReportJSON) != "" {
		_ = json.Unmarshal([]byte(job.QCReportJSON), &previous)
	}
	for i := range previous.Issues {
		if previous.Issues[i].ShotID == 0 && previous.Issues[i].ShotIndex > 0 && previous.Issues[i].ShotIndex <= len(shots) {
			previous.Issues[i].ShotID = shots[previous.Issues[i].ShotIndex-1].ID
		}
	}
	leftover := crew.LeftoverQCIssues(previous.Issues, issues)
	script := strings.TrimSpace(job.ScriptDraft)
	if script == "" {
		var ep models.Episode
		if err := cc.DB.Select("script").First(&ep, job.EpisodeID).Error; err == nil {
			script = ep.Script
		}
	}
	patched := crew.ApplyQCFixes(contexts, assets, issues)
	if crew.QCIssuesNeedRewrite(issues) {
		provider, model, errMsg := cc.loadTextModel()
		if errMsg != "" {
			cc.failJob(jobID, errMsg)
			cc.appendChat(jobID, crew.NewChatMessage("assistant", crew.ChatNameDirector, "改稿失败："+errMsg, "fix"))
			return
		}
		rewritten, err := crew.RewriteShotsForQC(cc.Ark, provider, model, script, patched, issues)
		if err != nil {
			log.Printf("crew %d qc rewrite: %v", jobID, err)
		} else {
			patched = rewritten
		}
	}
	patched = crew.ApplyQCFixes(patched, assets, issues)
	patched = crew.PrepareShotsAfterFix(patched, assets, script, nil, issues)
	if err := cc.saveQCContexts(job.ProjectID, shots, patched); err != nil {
		cc.failJob(jobID, "改稿写入失败："+err.Error())
		cc.appendChat(jobID, crew.NewChatMessage("assistant", crew.ChatNameDirector, "改稿失败：写入分镜失败。", "fix"))
		return
	}
	cc.runQCMerged(jobID, leftover, previous.Issues)
	updated, ok := cc.reload(jobID)
	if !ok {
		return
	}
	report := crew.QCReport{}
	if strings.TrimSpace(updated.QCReportJSON) != "" {
		_ = json.Unmarshal([]byte(updated.QCReportJSON), &report)
	}
	// Missing-dialogue QC is intentionally bounded per scan. Continue applying
	// newly exposed missing lines in the same user-authorized operation instead
	// of making the user click once for every batch.
	if pass < 7 {
		next := make([]crew.QCIssue, 0)
		for _, issue := range report.Issues {
			if strings.EqualFold(strings.TrimSpace(issue.Code), "R2") && strings.Contains(issue.Message, "未进分镜") {
				next = append(next, issue)
			}
		}
		if len(next) > 0 {
			raw, _ := json.Marshal(next)
			cc.patchJob(jobID, map[string]any{
				"status":        "running",
				"stage":         "qc",
				"error_message": "",
			})
			cc.runQCFixPass(jobID, raw, pass+1)
			return
		}
	}
	done := fmt.Sprintf("已按确认项改了 %d 处，其它镜头未动。监制复检这些位置。", len(issues))
	if updated.Status == "failed" {
		done = "改稿失败：" + strings.TrimSpace(updated.ErrorMessage)
		cc.appendChat(jobID, crew.NewChatMessage("assistant", crew.ChatNameDirector, done, "fix"))
		return
	}
	cc.appendChat(jobID,
		crew.NewChatMessage("assistant", crew.ChatNameDirector, done, "fix"),
		crew.NewChatMessage("assistant", crew.ChatNameSupervisor, crew.FormatQCReport(report), "qc"),
	)
}

func (cc *CrewController) loadQCContexts(episodeID uint) ([]models.Shot, []crew.ShotContext) {
	var shots []models.Shot
	cc.DB.Where("episode_id = ?", episodeID).Order("sort_order asc, id asc").Find(&shots)
	contexts := make([]crew.ShotContext, 0, len(shots))
	for _, s := range shots {
		fillShotFields(&s, cc.Storage)
		refs := make([]crew.ShotRefInfo, 0, len(s.Refs))
		for _, ref := range s.Refs {
			info := crew.ShotRefInfo{Kind: ref.Kind, ResourceID: ref.ID, Name: fmt.Sprintf("#%d", ref.ID)}
			var res models.Resource
			if err := cc.DB.Select("id", "name", "parent_id", "gen_prompt", "description").First(&res, ref.ID).Error; err == nil {
				fillResourceParentName(&res, cc.DB)
				info.Name = strings.TrimSpace(res.Name)
				info.ParentName = strings.TrimSpace(res.ParentName)
				info.DisplayName = services.ResourceIdentityLabel(res)
				info.Prompt = strings.TrimSpace(firstNonEmpty(res.GenPrompt, res.Description))
				if res.ParentID != nil && *res.ParentID > 0 {
					info.IsDerivative = true
					info.ParentID = *res.ParentID
					var parent models.Resource
					if err := cc.DB.Select("gen_prompt", "description").First(&parent, *res.ParentID).Error; err == nil {
						info.ParentPrompt = strings.TrimSpace(firstNonEmpty(parent.GenPrompt, parent.Description))
					}
				}
			}
			refs = append(refs, info)
		}
		label := strings.TrimSpace(s.Label)
		if label == "" {
			label = fmt.Sprintf("分镜%d", s.SortOrder)
		}
		contexts = append(contexts, crew.ShotContext{
			ID:       s.ID,
			Index:    len(contexts) + 1,
			Label:    label,
			Note:     strings.TrimSpace(s.Note),
			Script:   s.Script,
			Duration: s.Duration,
			Refs:     refs,
		})
	}
	return shots, contexts
}

func (cc *CrewController) packQCDuration(projectID, episodeID uint) (bool, []models.Shot, []crew.ShotContext, error) {
	shots, contexts := cc.loadQCContexts(episodeID)
	packed := crew.PackShotContexts(contexts)
	if !crew.ShotScriptsChanged(contexts, packed) {
		return false, shots, contexts, nil
	}
	if err := cc.saveQCContexts(projectID, shots, packed); err != nil {
		return false, shots, contexts, err
	}
	shots, contexts = cc.loadQCContexts(episodeID)
	return true, shots, contexts, nil
}

func withDurationPackNote(report crew.QCReport, packed bool) crew.QCReport {
	if !packed {
		return report
	}
	note := "已将超出 10 秒的拍挪到下一镜。"
	if strings.TrimSpace(report.Summary) == "" {
		report.Summary = note
	} else {
		report.Summary = note + strings.TrimSpace(report.Summary)
	}
	return report
}

func (cc *CrewController) saveQCContexts(projectID uint, shots []models.Shot, patched []crew.ShotContext) error {
	byID := map[uint]crew.ShotContext{}
	for _, ctx := range patched {
		byID[ctx.ID] = ctx
	}
	for i := range shots {
		ctx, ok := byID[shots[i].ID]
		if !ok {
			continue
		}
		fillShotFields(&shots[i], cc.Storage)
		refs := cc.shotRefsFromInfo(projectID, ctx.Refs)
		refs = cc.dropParentCharacterRefs(refs)
		updates := map[string]any{
			// QC preparation has already normalized the timeline. Preserve locked
			// source dialogue here; the ordinary finalizer can trim it and recreate
			// the same R2 "台词未进分镜" issue immediately after saving.
			"script":              crew.FinalizeShotScriptPreservingDialogue(ctx.Script, shots[i].Duration),
			"refs_json":           encodeShotRefs(refs),
			"character_refs_json": encodeCharacterRefs(shotRefsToCharacterRefs(refs)),
		}
		if label := strings.TrimSpace(ctx.Label); label != "" {
			updates["label"] = label
		}
		if ctx.Note != shots[i].Note {
			updates["note"] = ctx.Note
		}
		if sid := shotRefsFirstSceneID(refs); sid != nil {
			updates["scene_id"] = *sid
		}
		if err := cc.DB.Model(&shots[i]).Updates(updates).Error; err != nil {
			return err
		}
	}
	if len(shots) == 0 {
		return nil
	}
	episodeID := shots[0].EpisodeID
	resolution := shots[0].Resolution
	if resolution == "" {
		resolution = "720p"
	}
	maxOrder := 0
	for _, s := range shots {
		if s.SortOrder > maxOrder {
			maxOrder = s.SortOrder
		}
	}
	for _, ctx := range patched {
		if ctx.ID != 0 || strings.TrimSpace(ctx.Script) == "" {
			continue
		}
		maxOrder++
		refs := cc.shotRefsFromInfo(projectID, ctx.Refs)
		refs = cc.dropParentCharacterRefs(refs)
		shot := models.Shot{
			EpisodeID:         episodeID,
			SortOrder:         maxOrder,
			Label:             firstNonEmpty(ctx.Label, "续"),
			Note:              ctx.Note,
			Script:            ctx.Script,
			Duration:          crew.ShotMaxSeconds(ctx.Duration),
			Resolution:        resolution,
			Status:            "draft",
			RefsJSON:          encodeShotRefs(refs),
			CharacterRefsJSON: encodeCharacterRefs(shotRefsToCharacterRefs(refs)),
			CharacterIDsJSON:  "[]",
		}
		if sid := shotRefsFirstSceneID(refs); sid != nil {
			shot.SceneID = sid
		}
		if err := cc.DB.Create(&shot).Error; err != nil {
			return err
		}
	}
	return nil
}

func (cc *CrewController) shotRefsFromInfo(projectID uint, infos []crew.ShotRefInfo) []models.ShotRef {
	refs := make([]models.ShotRef, 0, len(infos))
	seen := map[uint]bool{}
	for _, info := range infos {
		id := info.ResourceID
		if id == 0 {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		kind := strings.TrimSpace(info.Kind)
		if kind == "" {
			kind = "character"
		}
		variant := "original"
		label := firstNonEmpty(info.DisplayName, info.Name)
		var res models.Resource
		if err := cc.DB.First(&res, id).Error; err != nil {
			continue
		}
		if !resourceHasImage(res) {
			continue
		}
		fillResourceParentName(&res, cc.DB)
		if res.ProjectID != 0 && projectID != 0 && res.ProjectID != projectID {
			continue
		}
		if kind == "character" && res.StylizedImagePath != "" {
			variant = "stylized"
		}
		if ident := services.ResourceIdentityLabel(res); ident != "" {
			label = ident
		}
		if kind == "" {
			kind = res.Type
		}
		refs = append(refs, models.ShotRef{Kind: kind, ID: id, Variant: variant, Label: label})
	}
	return refs
}

func (cc *CrewController) bindShotRefs(projectID uint, assets []crew.AssetItem, shot crew.StoryboardShot) []models.ShotRef {
	refs := make([]models.ShotRef, 0)
	seen := map[uint]bool{}
	add := func(kind, name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		id := uint(0)
		if kind == "character" {
			if selected, ok := crew.SelectCharacterAssetForShot(assets, name, shot); ok {
				id = selected.ResourceID
			}
		}
		if id == 0 {
			id = cc.resourceIDForName(projectID, assets, kind, name)
		}
		if id == 0 || seen[id] {
			return
		}
		variant := "original"
		label := name
		var res models.Resource
		if err := cc.DB.First(&res, id).Error; err != nil || !resourceHasImage(res) {
			return
		}
		fillResourceParentName(&res, cc.DB)
		if kind == "character" && res.StylizedImagePath != "" {
			variant = "stylized"
		}
		if ident := services.ResourceIdentityLabel(res); ident != "" {
			label = ident
		}
		seen[id] = true
		refs = append(refs, models.ShotRef{Kind: kind, ID: id, Variant: variant, Label: label})
	}
	for _, n := range shot.CharacterNames {
		add("character", n)
	}
	// Models often omit names from characterNames while still writing 人名(九格)/人名说
	// in the script. Hang those too — same rule as manual rematch, no 5-person cap.
	for _, n := range services.MentionedCharacterNames(shot.Script) {
		add("character", n)
	}
	sceneRefs := cc.pickSceneRefsForShot(projectID, assets, shot)
	for _, r := range sceneRefs {
		if r.ID == 0 || seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		refs = append(refs, r)
	}
	if len(sceneRefs) == 0 {
		add("scene", shot.SceneName)
	}
	for _, n := range shot.PropNames {
		add("prop", n)
	}
	return cc.dropParentCharacterRefs(refs)
}

// pickSceneRefsForShot prefers split 9-grid angle cells matching the script's
// 景别/机位; falls back to the master scene plate when no cells exist.
func (cc *CrewController) pickSceneRefsForShot(projectID uint, assets []crew.AssetItem, shot crew.StoryboardShot) []models.ShotRef {
	sceneName := strings.TrimSpace(shot.SceneName)
	if sceneName == "" {
		return nil
	}
	cells := cc.listSceneGridCellsForScene(projectID, sceneName)
	if len(cells) > 0 {
		picks := services.PickSceneGridCellsForScript(cells, shot.Script, 3)
		out := make([]models.ShotRef, 0, len(picks))
		for _, p := range picks {
			var res models.Resource
			if err := cc.DB.First(&res, p.ID).Error; err != nil || !resourceHasImage(res) {
				continue
			}
			label := strings.TrimSpace(p.Label)
			if label == "" {
				label = services.SceneGridCellName(res.Name, res.GridCell)
			}
			if label == "" {
				label = sceneName
			}
			out = append(out, models.ShotRef{
				Kind:    "scene",
				ID:      p.ID,
				Variant: "original",
				Label:   label,
			})
		}
		if len(out) > 0 {
			return out
		}
	}
	id := cc.resourceIDForName(projectID, assets, "scene", sceneName)
	if id == 0 {
		return nil
	}
	var res models.Resource
	if err := cc.DB.First(&res, id).Error; err != nil || !resourceHasImage(res) {
		return nil
	}
	label := sceneName
	if ident := services.ResourceIdentityLabel(res); ident != "" {
		label = ident
	}
	return []models.ShotRef{{Kind: "scene", ID: id, Variant: "original", Label: label}}
}

func (cc *CrewController) listSceneGridCellsForScene(projectID uint, sceneName string) []services.SceneGridCellCandidate {
	sceneName = strings.TrimSpace(sceneName)
	if sceneName == "" || projectID == 0 {
		return nil
	}
	sceneID := uint(0)
	if res, ok := cc.findLibraryResourceAny(projectID, "scene", sceneName); ok {
		if res.GenType == "" || res.GenType == "scene" || res.GenType == "stylize" {
			sceneID = res.ID
		}
	}
	var grids []models.Resource
	cc.DB.Where("project_id = ? AND gen_type = ?", projectID, "scene_grid").
		Order("id desc").Limit(40).Find(&grids)
	gridIDs := make([]uint, 0)
	for _, g := range grids {
		hit := false
		if sceneID > 0 {
			for _, sid := range sceneIDsFromGenRefs(g.GenRefsJSON) {
				if sid == sceneID {
					hit = true
					break
				}
			}
		}
		if !hit {
			base := services.SceneGridBaseName(g.Name)
			if base != "" && (base == sceneName || strings.Contains(base, sceneName) || strings.Contains(sceneName, base)) {
				hit = true
			}
		}
		if hit {
			gridIDs = append(gridIDs, g.ID)
		}
	}
	if len(gridIDs) == 0 {
		// Name-prefix fallback on cells themselves.
		var cells []models.Resource
		like := sceneName + "·%"
		cc.DB.Where("project_id = ? AND gen_type = ? AND name LIKE ? AND image_path <> ''", projectID, "scene_grid_cell", like).
			Order("grid_id desc, grid_cell asc").Limit(18).Find(&cells)
		return sceneCellsToCandidates(cells)
	}
	// Prefer the newest grid that has the most cells with images.
	var best []models.Resource
	for _, gid := range gridIDs {
		var cells []models.Resource
		cc.DB.Where("project_id = ? AND gen_type = ? AND grid_id = ? AND image_path <> ''", projectID, "scene_grid_cell", gid).
			Order("grid_cell asc").Find(&cells)
		if len(cells) > len(best) {
			best = cells
		}
		if len(best) >= 9 {
			break
		}
	}
	return sceneCellsToCandidates(best)
}

func sceneCellsToCandidates(cells []models.Resource) []services.SceneGridCellCandidate {
	out := make([]services.SceneGridCellCandidate, 0, len(cells))
	for _, c := range cells {
		out = append(out, services.SceneGridCellCandidate{
			ID:       c.ID,
			Name:     strings.TrimSpace(c.Name),
			GridCell: c.GridCell,
			GridID:   c.GridID,
		})
	}
	return out
}

func (cc *CrewController) dropParentCharacterRefs(refs []models.ShotRef) []models.ShotRef {
	ids := make([]uint, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	if len(ids) == 0 {
		return refs
	}
	var resources []models.Resource
	if err := cc.DB.Select("id", "parent_id").Where("id IN ?", ids).Find(&resources).Error; err != nil {
		return refs
	}
	childParents := map[uint]bool{}
	for _, r := range resources {
		if r.ParentID != nil && *r.ParentID > 0 {
			childParents[*r.ParentID] = true
		}
	}
	if len(childParents) == 0 {
		return refs
	}
	out := make([]models.ShotRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind == "character" && childParents[ref.ID] {
			continue
		}
		out = append(out, ref)
	}
	return out
}

func (cc *CrewController) resourceIDForName(projectID uint, assets []crew.AssetItem, kind, name string) uint {
	for _, a := range assets {
		if a.Type != kind || a.ResourceID == 0 {
			continue
		}
		if crew.AssetNameMatches(a, name) {
			return a.ResourceID
		}
	}
	if res, ok := cc.findLibraryResource(projectID, kind, name); ok {
		return res.ID
	}
	return 0
}

func (cc *CrewController) findLibraryResource(projectID uint, typ, name string) (models.Resource, bool) {
	return cc.findLibraryResourceMatch(projectID, typ, name, true)
}

func resourceHasImage(r models.Resource) bool {
	return strings.TrimSpace(r.ImagePath) != "" || strings.TrimSpace(r.StylizedImagePath) != ""
}

// markReusableAssets flags assets that already have a same-name image in the
// library so the UI can show「复用」and Continue can skip empty image jobs.
func (cc *CrewController) markReusableAssets(projectID uint, assets []crew.AssetItem) []crew.AssetItem {
	for i := range assets {
		cc.tryReuseAssetImage(projectID, &assets[i])
	}
	return assets
}

func (cc *CrewController) tryReuseAssetImage(projectID uint, item *crew.AssetItem) bool {
	if item == nil {
		return false
	}
	name := strings.TrimSpace(item.Name)
	typ := strings.TrimSpace(item.Type)
	if name == "" || (typ != "character" && typ != "scene" && typ != "prop") {
		return false
	}
	if item.Skipped || item.Reused {
		if item.ResourceID == 0 {
			if existing, found := cc.findLibraryResourceAny(projectID, typ, name); found {
				item.ResourceID = existing.ID
			}
		}
		return item.Reused || item.Skipped
	}
	var existing models.Resource
	found := false
	if item.ParentID > 0 {
		existing, found = cc.findDerivedResource(projectID, item.ParentID, typ, name)
		if found && !resourceHasImage(existing) {
			found = false
		}
	}
	if !found {
		existing, found = cc.findLibraryResource(projectID, typ, name)
	}
	if !found {
		if item.ResourceID == 0 {
			if any, ok := cc.findLibraryResourceAny(projectID, typ, name); ok {
				item.ResourceID = any.ID
			}
		}
		return false
	}
	item.ResourceID = existing.ID
	item.Reused = true
	item.Skipped = true
	item.Error = ""
	return true
}

func crewAssetsAllImageReady(assets []crew.AssetItem) bool {
	if len(assets) == 0 {
		return true
	}
	for _, a := range assets {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		if a.Skipped || a.Reused {
			continue
		}
		return false
	}
	return true
}

func (cc *CrewController) findLibraryResourceAny(projectID uint, typ, name string) (models.Resource, bool) {
	return cc.findLibraryResourceMatch(projectID, typ, name, false)
}

func (cc *CrewController) findLibraryResourceMatch(projectID uint, typ, name string, requireImage bool) (models.Resource, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Resource{}, false
	}
	var items []models.Resource
	q := cc.DB.Where("project_id = ? AND type = ?", projectID, typ)
	if err := q.Order("is_group_primary desc, id desc").Find(&items).Error; err != nil {
		return models.Resource{}, false
	}
	parentIDs := make([]uint, 0)
	for _, r := range items {
		if r.ParentID != nil && *r.ParentID > 0 {
			parentIDs = append(parentIDs, *r.ParentID)
		}
	}
	parentNames := map[uint]string{}
	if len(parentIDs) > 0 {
		var parents []models.Resource
		if err := cc.DB.Select("id", "name").Where("id IN ?", parentIDs).Find(&parents).Error; err == nil {
			for _, p := range parents {
				parentNames[p.ID] = strings.TrimSpace(p.Name)
			}
		}
	}
	var fallback models.Resource
	foundFallback := false
	for _, r := range items {
		if requireImage && r.ImagePath == "" && r.StylizedImagePath == "" {
			continue
		}
		parentName := ""
		if r.ParentID != nil && *r.ParentID > 0 {
			parentName = parentNames[*r.ParentID]
		}
		base := strings.TrimSpace(r.Name)
		if b, ok := parseCandidateBase(r.Name); ok {
			base = b
		}
		if !services.ResourceQueryMatches(base, parentName, name) && !services.ResourceQueryMatches(r.Name, parentName, name) {
			continue
		}
		if r.IsGroupPrimary || !strings.Contains(r.Name, " · 候选") {
			return r, true
		}
		if !foundFallback {
			fallback = r
			foundFallback = true
		}
	}
	return fallback, foundFallback
}

func (cc *CrewController) upsertExtractedResources(projectID uint, assets []crew.AssetItem) {
	for i := range assets {
		item := &assets[i]
		name := strings.TrimSpace(item.Name)
		typ := strings.TrimSpace(item.Type)
		if name == "" || (typ != "character" && typ != "scene" && typ != "prop") {
			continue
		}
		desc := strings.TrimSpace(item.Description)
		prompt := strings.TrimSpace(item.Prompt)
		voice := strings.TrimSpace(item.VoicePrompt)
		if typ == "character" && voice == "" && item.ParentID > 0 {
			var parent models.Resource
			if cc.DB.Select("voice_prompt").First(&parent, item.ParentID).Error == nil {
				voice = strings.TrimSpace(parent.VoicePrompt)
			}
		}
		var existing models.Resource
		found := false
		if item.ParentID > 0 {
			existing, found = cc.findDerivedResource(projectID, item.ParentID, typ, name)
		} else {
			existing, found = cc.findLibraryResourceAny(projectID, typ, name)
		}
		if found {
			updates := map[string]any{}
			if desc != "" {
				updates["description"] = desc
			}
			if prompt != "" {
				updates["gen_prompt"] = prompt
				updates["gen_type"] = typ
			}
			if typ == "character" && voice != "" {
				updates["voice_prompt"] = voice
			}
			if item.ParentID > 0 {
				updates["parent_id"] = item.ParentID
			}
			if !existing.IsGroupPrimary && !strings.Contains(existing.Name, " · 候选") {
				updates["is_group_primary"] = true
			}
			if len(updates) > 0 {
				_ = cc.DB.Model(&existing).Updates(updates).Error
			}
			item.ResourceID = existing.ID
			continue
		}
		res := models.Resource{
			ProjectID:      projectID,
			Type:           typ,
			Source:         "ai",
			Name:           name,
			Description:    desc,
			VoicePrompt:    voice,
			GenPrompt:      prompt,
			GenType:        typ,
			IsGroupPrimary: true,
		}
		if item.ParentID > 0 {
			pid := item.ParentID
			res.ParentID = &pid
		}
		if err := cc.DB.Create(&res).Error; err == nil {
			item.ResourceID = res.ID
		}
	}
}

func (cc *CrewController) findDerivedResource(projectID, parentID uint, typ, name string) (models.Resource, bool) {
	name = strings.TrimSpace(name)
	if parentID == 0 || name == "" {
		return models.Resource{}, false
	}
	var item models.Resource
	err := cc.DB.Where("project_id = ? AND parent_id = ? AND type = ? AND name = ?", projectID, parentID, typ, name).
		Order("id desc").First(&item).Error
	if err != nil {
		return models.Resource{}, false
	}
	return item, true
}

func (cc *CrewController) waitImageJob(jobID uint, timeout time.Duration) models.ImageGenerationJob {
	deadline := time.Now().Add(timeout)
	var job models.ImageGenerationJob
	for time.Now().Before(deadline) {
		if err := cc.DB.First(&job, jobID).Error; err != nil {
			return job
		}
		if job.Status == "completed" || job.Status == "failed" {
			return job
		}
		time.Sleep(2 * time.Second)
	}
	if job.Status == "" || job.Status == "pending" || job.Status == "running" {
		job.Status = "failed"
		job.ErrorMessage = "生图超时"
	}
	return job
}

func (cc *CrewController) jobResponse(job models.CrewJob, episode models.Episode) gin.H {
	assets := cc.decodeAssets(job)
	if job.Stage == "director" || job.Stage == "consistency" {
		assets = cc.markReusableAssets(job.ProjectID, assets)
	}
	var qc *crew.QCReport
	if strings.TrimSpace(job.QCReportJSON) != "" {
		var report crew.QCReport
		if json.Unmarshal([]byte(job.QCReportJSON), &report) == nil {
			qc = &report
		}
	}
	imageJobs := cc.imageJobViews(job)
	shotIDs := []uint{}
	if job.ShotIDsJSON != "" {
		_ = json.Unmarshal([]byte(job.ShotIDsJSON), &shotIDs)
	}
	return gin.H{
		"job": gin.H{
			"id":           job.ID,
			"projectId":    job.ProjectID,
			"episodeId":    job.EpisodeID,
			"status":       job.Status,
			"stage":        job.Stage,
			"sourceScript": job.SourceScript,
			"scriptDraft":  job.ScriptDraft,
			"directorPlan": job.DirectorPlan,
			"assets":       assets,
			"qc":           qc,
			"imageJobs":    imageJobs,
			"shotIds":      shotIDs,
			"shotMode":     job.ShotMode,
			"fromShotId":   job.FromShotID,
			"chat":         crew.DecodeChat(job.ChatJSON),
			"errorMessage": job.ErrorMessage,
			"createdAt":    job.CreatedAt,
			"updatedAt":    job.UpdatedAt,
		},
		"episodeScript": episode.Script,
		"shotCount":     cc.shotCount(episode.ID),
	}
}

func (cc *CrewController) imageJobViews(job models.CrewJob) []gin.H {
	var ids []uint
	if job.ImageJobIDsJSON != "" {
		_ = json.Unmarshal([]byte(job.ImageJobIDsJSON), &ids)
	}
	if len(ids) == 0 {
		return []gin.H{}
	}
	var jobs []models.ImageGenerationJob
	cc.DB.Where("id IN ?", ids).Order("id asc").Find(&jobs)
	out := make([]gin.H, 0, len(jobs))
	for i := range jobs {
		if cc.Resource != nil {
			out = append(out, cc.Resource.imageGenJobResponse(&jobs[i]))
		} else {
			out = append(out, gin.H{"id": jobs[i].ID, "status": jobs[i].Status, "progress": jobs[i].Progress, "message": jobs[i].Message})
		}
	}
	return out
}

func (cc *CrewController) decodeAssets(job models.CrewJob) []crew.AssetItem {
	if strings.TrimSpace(job.AssetsJSON) == "" {
		return []crew.AssetItem{}
	}
	var assets []crew.AssetItem
	if err := json.Unmarshal([]byte(job.AssetsJSON), &assets); err != nil {
		return []crew.AssetItem{}
	}
	return assets
}

func (cc *CrewController) fillRecurringCharacterDetails(assets []crew.AssetItem, script string, provider models.AIProvider, model models.AIModel, project models.Project) []crew.AssetItem {
	needDescribe := make([]string, 0, 4)
	for _, name := range services.RecurringCharacterNames(script, crew.MinRecurringCharacterMentions) {
		for _, a := range assets {
			if strings.EqualFold(strings.TrimSpace(a.Name), name) && strings.TrimSpace(a.Description) == "" {
				needDescribe = append(needDescribe, name)
				break
			}
		}
	}
	if len(needDescribe) == 0 {
		return assets
	}
	details, err := crew.DescribeRecurringCharacters(cc.Ark, provider, model, needDescribe, script, project)
	if err != nil {
		log.Printf("crew recurring describe: %v", err)
		return assets
	}
	crew.ApplyRecurringCharacterDetails(assets, details)
	return assets
}

func (cc *CrewController) enrichRecurringCharactersFromScript(projectID uint, assets []crew.AssetItem, script string, provider models.AIProvider, model models.AIModel, project models.Project) ([]crew.AssetItem, []string) {
	assets, added := crew.EnsureRecurringCharactersInAssets(assets, script)
	if len(added) == 0 {
		return assets, nil
	}
	if details, err := crew.DescribeRecurringCharacters(cc.Ark, provider, model, added, script, project); err != nil {
		log.Printf("crew %d recurring describe: %v", projectID, err)
	} else {
		crew.ApplyRecurringCharacterDetails(assets, details)
	}
	if polished, err := crew.PolishVisualPrompts(cc.Ark, provider, model, assets, project); err != nil {
		log.Printf("crew %d recurring polish: %v", projectID, err)
	} else {
		assets = polished
	}
	if voiced, err := crew.FillCharacterVoices(cc.Ark, provider, model, assets, script); err != nil {
		log.Printf("crew %d recurring voices: %v", projectID, err)
	} else {
		assets = voiced
	}
	cc.upsertExtractedResources(projectID, assets)
	return assets, added
}

func (cc *CrewController) enqueueRecurringCharacterImages(projectID uint, assets []crew.AssetItem, names []string) {
	want := map[string]bool{}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			want[n] = true
		}
	}
	for i := range assets {
		item := assets[i]
		if item.Type != "character" || item.ParentID > 0 || item.IsDerivative {
			continue
		}
		if !want[strings.TrimSpace(item.Name)] {
			continue
		}
		if existing, found := cc.findLibraryResource(projectID, "character", item.Name); found && resourceHasImage(existing) {
			continue
		}
		prompt := strings.TrimSpace(firstNonEmpty(item.Prompt, item.Description))
		if prompt == "" {
			continue
		}
		idx := i
		go func() {
			input := imageGenJobInput{
				Name:        assets[idx].Name,
				Description: prompt,
				Count:       1,
				Resolution:  "1k",
			}
			if assets[idx].ResourceID > 0 {
				input.TargetResourceID = assets[idx].ResourceID
			}
			if _, _, errMsg := cc.Resource.enqueueImageGenerationJob(projectID, "character", input); errMsg != "" {
				log.Printf("crew recurring image %s: %s", assets[idx].Name, errMsg)
			}
		}()
	}
}

func (cc *CrewController) enrichFixAssets(projectID uint, assets []crew.AssetItem) []crew.AssetItem {
	if projectID == 0 {
		return assets
	}
	var resources []models.Resource
	if err := cc.DB.Where("project_id = ? AND type IN ?", projectID, []string{"character", "scene", "prop"}).
		Order("is_group_primary desc, id asc").Find(&resources).Error; err != nil {
		return assets
	}
	hasImage := map[uint]bool{}
	for _, r := range resources {
		hasImage[r.ID] = resourceHasImage(r)
	}
	seen := map[uint]bool{}
	promptByID := map[uint]string{}
	for _, r := range resources {
		promptByID[r.ID] = strings.TrimSpace(firstNonEmpty(r.GenPrompt, r.Description))
	}
	for _, a := range assets {
		if a.ResourceID > 0 {
			seen[a.ResourceID] = true
		}
	}
	out := append([]crew.AssetItem{}, assets...)
	for i := range out {
		if p := promptByID[out[i].ResourceID]; p != "" {
			out[i].Prompt = p
		}
		if out[i].ResourceID > 0 && !hasImage[out[i].ResourceID] {
			out[i].ResourceID = 0
		}
	}
	for _, r := range resources {
		if seen[r.ID] || !hasImage[r.ID] {
			continue
		}
		fillResourceParentName(&r, cc.DB)
		parentID := uint(0)
		if r.ParentID != nil && *r.ParentID > 0 {
			parentID = *r.ParentID
		}
		out = append(out, crew.AssetItem{
			Name:         strings.TrimSpace(r.Name),
			Type:         r.Type,
			Prompt:       promptByID[r.ID],
			Description:  strings.TrimSpace(r.Description),
			ResourceID:   r.ID,
			ParentID:     parentID,
			ParentName:   strings.TrimSpace(r.ParentName),
			IsDerivative: parentID > 0,
		})
		seen[r.ID] = true
	}
	return out
}

func (cc *CrewController) loadTextModel() (models.AIProvider, models.AIModel, string) {
	var model models.AIModel
	err := cc.DB.Where("capability = ? AND enabled = ? AND is_default = ?", "text", true, true).First(&model).Error
	if err != nil {
		err = cc.DB.Where("capability = ? AND enabled = ?", "text", true).Order("id asc").First(&model).Error
	}
	if err != nil {
		return models.AIProvider{}, models.AIModel{}, "请先在设置中心启用一个文本模型"
	}
	var provider models.AIProvider
	if err := cc.DB.First(&provider, model.ProviderID).Error; err != nil {
		return models.AIProvider{}, models.AIModel{}, "文本模型服务商不存在"
	}
	return provider, model, ""
}

func (cc *CrewController) findEpisode(c *gin.Context) (models.Episode, bool) {
	var episode models.Episode
	if err := cc.DB.First(&episode, c.Param("id")).Error; err != nil {
		fail(c, 404, "分集不存在")
		return episode, false
	}
	return episode, true
}

func (cc *CrewController) latestJob(episodeID uint) (models.CrewJob, bool) {
	var job models.CrewJob
	if err := cc.DB.Where("episode_id = ?", episodeID).Order("id desc").First(&job).Error; err != nil {
		return job, false
	}
	return job, true
}

func (cc *CrewController) ensureChatJob(episode models.Episode) (models.CrewJob, error) {
	if job, ok := cc.latestJob(episode.ID); ok {
		return job, nil
	}
	script := strings.TrimSpace(episode.Script)
	job := models.CrewJob{
		ProjectID:    episode.ProjectID,
		EpisodeID:    episode.ID,
		Status:       "waiting_review",
		Stage:        "storyboard",
		SourceScript: script,
		ScriptDraft:  script,
		ChatJSON:     "[]",
	}
	if err := cc.DB.Create(&job).Error; err != nil {
		return job, fmt.Errorf("创建剧组聊天失败")
	}
	return job, nil
}

func (cc *CrewController) decodeQCIssues(job models.CrewJob) []crew.QCIssue {
	if strings.TrimSpace(job.QCReportJSON) == "" {
		return nil
	}
	var report crew.QCReport
	if json.Unmarshal([]byte(job.QCReportJSON), &report) != nil {
		return nil
	}
	return report.Issues
}

func (cc *CrewController) splitStoryboardForChat(job models.CrewJob, episode models.Episode, replace bool) ([]uint, error) {
	provider, model, errMsg := cc.loadTextModel()
	if errMsg != "" {
		return nil, fmt.Errorf("%s", errMsg)
	}
	var project models.Project
	_ = cc.DB.First(&project, job.ProjectID).Error
	script := strings.TrimSpace(job.ScriptDraft)
	if script == "" {
		script = strings.TrimSpace(episode.Script)
	}
	assets := cc.decodeAssets(job)
	result, err := crew.SplitStoryboard(cc.Ark, provider, model, script, job.DirectorPlan, project, assets)
	if err != nil {
		return nil, err
	}
	shotScripts := make([]string, 0, len(result.Shots))
	for _, sh := range result.Shots {
		shotScripts = append(shotScripts, sh.Script)
	}
	combinedScript := crew.CombineScripts(append([]string{script}, shotScripts...)...)
	assets, addedNames := cc.enrichRecurringCharactersFromScript(job.ProjectID, assets, combinedScript, provider, model, project)
	if len(addedNames) > 0 {
		raw, _ := json.Marshal(assets)
		cc.patchJob(job.ID, map[string]any{"assets_json": string(raw)})
		cc.enqueueRecurringCharacterImages(job.ProjectID, assets, addedNames)
	}
	assets = cc.enrichFixAssets(job.ProjectID, assets)
	if replace {
		if err := cc.DB.Where("episode_id = ?", job.EpisodeID).Delete(&models.Shot{}).Error; err != nil {
			return nil, fmt.Errorf("清空旧分镜失败：%w", err)
		}
	}
	var maxOrder int
	cc.DB.Model(&models.Shot{}).Where("episode_id = ?", job.EpisodeID).Select("coalesce(max(sort_order), 0)").Scan(&maxOrder)
	createdIDs := make([]uint, 0, len(result.Shots))
	for i, item := range result.Shots {
		refs := cc.bindShotRefs(job.ProjectID, assets, item)
		shot := models.Shot{
			EpisodeID:         job.EpisodeID,
			SortOrder:         maxOrder + i + 1,
			Label:             item.Label,
			Script:            item.Script,
			Duration:          item.Duration,
			Resolution:        "720p",
			Status:            "draft",
			RefsJSON:          encodeShotRefs(refs),
			CharacterRefsJSON: encodeCharacterRefs(shotRefsToCharacterRefs(refs)),
			CharacterIDsJSON:  "[]",
		}
		if sid := shotRefsFirstSceneID(refs); sid != nil {
			shot.SceneID = sid
		}
		if err := cc.DB.Create(&shot).Error; err != nil {
			return createdIDs, fmt.Errorf("创建分镜失败：%w", err)
		}
		createdIDs = append(createdIDs, shot.ID)
	}
	idsJSON, _ := json.Marshal(createdIDs)
	cc.patchJob(job.ID, map[string]any{
		"shot_ids_json": string(idsJSON),
		"shot_mode":     "replace",
		"status":        "waiting_review",
		"stage":         "storyboard",
		"error_message": "",
	})
	return createdIDs, nil
}

func (cc *CrewController) applyFixesNow(job models.CrewJob, issues []crew.QCIssue) ([]crew.QCIssue, error) {
	assets := cc.enrichFixAssets(job.ProjectID, cc.decodeAssets(job))
	shots, contexts := cc.loadQCContexts(job.EpisodeID)
	if len(shots) == 0 {
		return nil, fmt.Errorf("本集还没有分镜")
	}
	for i := range issues {
		if issues[i].ShotID == 0 && issues[i].ShotIndex > 0 && issues[i].ShotIndex <= len(shots) {
			issues[i].ShotID = shots[issues[i].ShotIndex-1].ID
		}
	}
	previous := cc.decodeQCIssues(job)
	for i := range previous {
		if previous[i].ShotID == 0 && previous[i].ShotIndex > 0 && previous[i].ShotIndex <= len(shots) {
			previous[i].ShotID = shots[previous[i].ShotIndex-1].ID
		}
	}
	leftover := crew.LeftoverQCIssues(previous, issues)
	script := strings.TrimSpace(job.ScriptDraft)
	if script == "" {
		var ep models.Episode
		if err := cc.DB.Select("script").First(&ep, job.EpisodeID).Error; err == nil {
			script = ep.Script
		}
	}
	patched := crew.ApplyQCFixes(contexts, assets, issues)
	if crew.QCIssuesNeedRewrite(issues) {
		provider, model, errMsg := cc.loadTextModel()
		if errMsg != "" {
			return nil, fmt.Errorf("%s", errMsg)
		}
		rewritten, err := crew.RewriteShotsForQC(cc.Ark, provider, model, script, patched, issues)
		if err != nil {
			log.Printf("crew %d chat rewrite: %v", job.ID, err)
		} else {
			patched = rewritten
		}
	}
	patched = crew.ApplyQCFixes(patched, assets, issues)
	patched = crew.PrepareShotsAfterFix(patched, assets, script, nil, issues)
	if err := cc.saveQCContexts(job.ProjectID, shots, patched); err != nil {
		return nil, err
	}
	return leftover, nil
}

func (cc *CrewController) reviewQCNow(job models.CrewJob, leftover, previous []crew.QCIssue) (crew.QCReport, error) {
	provider, model, errMsg := cc.loadTextModel()
	if errMsg != "" {
		return crew.QCReport{}, fmt.Errorf("%s", errMsg)
	}
	script := strings.TrimSpace(job.ScriptDraft)
	if script == "" {
		var ep models.Episode
		if err := cc.DB.Select("script").First(&ep, job.EpisodeID).Error; err == nil {
			script = ep.Script
		}
	}
	packed := false
	var shots []models.Shot
	var contexts []crew.ShotContext
	if didPack, packedShots, packedContexts, packErr := cc.packQCDuration(job.ProjectID, job.EpisodeID); packErr != nil {
		return crew.QCReport{}, packErr
	} else {
		packed = didPack
		shots, contexts = packedShots, packedContexts
	}
	assets := cc.enrichFixAssets(job.ProjectID, cc.decodeAssets(job))
	report, err := crew.ReviewQualityAgainst(cc.Ark, provider, model, script, assets, contexts, previous)
	if err != nil {
		return crew.QCReport{}, err
	}
	for i := range report.Issues {
		if report.Issues[i].ShotID == 0 && report.Issues[i].ShotIndex > 0 && report.Issues[i].ShotIndex <= len(shots) {
			report.Issues[i].ShotID = shots[report.Issues[i].ShotIndex-1].ID
		}
	}
	if len(previous) > 0 {
		report = crew.MergeQCAfterFix(report, previous, leftover)
	}
	report = withDurationPackNote(report, packed)
	raw, _ := json.Marshal(report)
	cc.patchJob(job.ID, map[string]any{
		"qc_report_json": string(raw),
		"status":         "waiting_review",
		"stage":          "qc",
		"error_message":  "",
	})
	return report, nil
}

func (cc *CrewController) reload(id uint) (models.CrewJob, bool) {
	var job models.CrewJob
	if err := cc.DB.First(&job, id).Error; err != nil {
		return job, false
	}
	return job, true
}

func (cc *CrewController) patchJob(id uint, updates map[string]any) {
	if err := cc.DB.Model(&models.CrewJob{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		log.Printf("crew patch %d: %v", id, err)
	}
}

func (cc *CrewController) failJob(id uint, message string) {
	cc.patchJob(id, map[string]any{
		"status":        "failed",
		"error_message": message,
	})
}

func (cc *CrewController) appendChat(id uint, extra ...crew.ChatMessage) {
	if len(extra) == 0 {
		return
	}
	job, ok := cc.reload(id)
	if !ok {
		return
	}
	cc.patchJob(id, map[string]any{
		"chat_json": crew.EncodeChat(crew.AppendChat(crew.DecodeChat(job.ChatJSON), extra...)),
	})
}

func (cc *CrewController) shotCount(episodeID uint) int64 {
	var n int64
	cc.DB.Model(&models.Shot{}).Where("episode_id = ?", episodeID).Count(&n)
	return n
}
