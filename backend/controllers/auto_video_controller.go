package controllers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"novaly/backend/models"
	"novaly/backend/services"
)

type AutoVideoController struct {
	DB      *gorm.DB
	Ark     *services.ArkService
	Storage *services.Storage
	Shot    *ShotController
	mu      sync.Mutex
	running map[uint]bool
}

type videoReview struct {
	Passed     bool     `json:"passed"`
	Checks     []string `json:"checks"`
	Failures   []string `json:"failures"`
	Transcript string   `json:"transcript,omitempty"`
}

func activeAutoVideoStatuses() []string {
	return []string{"running", "pause_requested", "paused"}
}

func (a *AutoVideoController) Start(c *gin.Context) {
	var shot models.Shot
	if a.DB.First(&shot, c.Param("id")).Error != nil {
		fail(c, 404, "分镜不存在")
		return
	}
	var ep models.Episode
	if a.DB.First(&ep, shot.EpisodeID).Error != nil {
		fail(c, 404, "分集不存在")
		return
	}
	var existing models.AutoVideoRun
	if a.DB.Where("episode_id = ? AND status IN ?", ep.ID, activeAutoVideoStatuses()).Order("id desc").First(&existing).Error == nil {
		a.ensureRunWorker(existing)
		a.respondRun(c, existing.ID, true)
		return
	}
	var shots []models.Shot
	a.DB.Where("episode_id = ? AND (sort_order > ? OR (sort_order = ? AND id >= ?))", ep.ID, shot.SortOrder, shot.SortOrder, shot.ID).Order("sort_order,id").Find(&shots)
	if len(shots) == 0 {
		fail(c, 400, "没有可处理的分镜")
		return
	}
	if _, _, err := a.reviewModel(); err != nil {
		fail(c, 400, err.Error())
		return
	}
	if msg, ok := a.requireVideoProviderReady(shot.ID); !ok {
		fail(c, 503, msg)
		return
	}
	run := models.AutoVideoRun{ProjectID: ep.ProjectID, EpisodeID: ep.ID, StartShotID: shot.ID, CurrentShotID: shot.ID, Status: "running", Stage: "reviewing", TotalCount: len(shots), MaxRetries: 2}
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		for _, s := range shots {
			if err := tx.Create(&models.AutoVideoRunItem{RunID: run.ID, ShotID: s.ID, SortOrder: s.SortOrder, Status: "pending"}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(c, 500, "创建自动任务失败")
		return
	}
	a.launch(run.ID)
	a.respond(c, run.ID)
}

func (a *AutoVideoController) Get(c *gin.Context) {
	eid, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || eid == 0 {
		fail(c, 400, "分集无效")
		return
	}
	var run models.AutoVideoRun
	q := a.DB.Where("episode_id = ? AND status IN ?", eid, activeAutoVideoStatuses()).Order("id desc").First(&run)
	if q.Error != nil {
		if q.Error != gorm.ErrRecordNotFound {
			fail(c, 500, "读取任务失败")
			return
		}
		q = a.DB.Where("episode_id = ?", eid).Order("id desc").First(&run)
		if q.Error != nil {
			if q.Error == gorm.ErrRecordNotFound {
				c.JSON(200, gin.H{"run": nil})
				return
			}
			fail(c, 500, "读取任务失败")
			return
		}
	}
	a.ensureRunWorker(run)
	a.respond(c, run.ID)
}

func (a *AutoVideoController) Pause(c *gin.Context)  { a.setRunStatus(c, "pause_requested") }
func (a *AutoVideoController) Cancel(c *gin.Context) { a.setRunStatus(c, "cancelled") }
func (a *AutoVideoController) Resume(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var run models.AutoVideoRun
	if a.DB.First(&run, id).Error != nil {
		fail(c, 404, "任务不存在")
		return
	}
	var item models.AutoVideoRunItem
	if a.DB.Where("run_id = ? AND shot_id = ?", run.ID, run.CurrentShotID).First(&item).Error == nil && item.Attempts >= run.MaxRetries+1 {
		fail(c, 409, "当前分镜已达到自动重试上限，请点击“再次重试本镜”或人工采用候选版本")
		return
	}
	if msg, ok := a.requireVideoProviderReady(run.CurrentShotID); !ok {
		fail(c, 503, msg)
		return
	}
	if a.DB.Model(&models.AutoVideoRun{}).Where("id = ? AND status IN ?", id, []string{"paused", "pause_requested"}).Updates(map[string]any{"status": "running", "pause_reason": "", "error_message": ""}).RowsAffected == 0 {
		fail(c, 409, "任务当前不能继续")
		return
	}
	a.launch(uint(id))
	a.respond(c, uint(id))
}
func (a *AutoVideoController) Retry(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var run models.AutoVideoRun
	if a.DB.First(&run, id).Error != nil {
		fail(c, 404, "任务不存在")
		return
	}
	if msg, ok := a.requireVideoProviderReady(run.CurrentShotID); !ok {
		fail(c, 503, msg)
		return
	}
	itemUpdates := map[string]any{"status": "pending", "attempts": 0, "failure_summary": ""}
	if !isRetryableVideoDownloadMessage(run.PauseReason) && !isRetryableVideoDownloadMessage(run.ErrorMessage) {
		itemUpdates["video_task_id"] = ""
	}
	a.DB.Model(&models.AutoVideoRunItem{}).Where("run_id = ? AND shot_id = ?", run.ID, run.CurrentShotID).Updates(itemUpdates)
	a.DB.Model(&run).Updates(map[string]any{"status": "running", "pause_reason": "", "error_message": ""})
	a.launch(run.ID)
	a.respond(c, run.ID)
}
func (a *AutoVideoController) Accept(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var input struct {
		ResourceID uint `json:"resourceId"`
	}
	_ = c.ShouldBindJSON(&input)
	var run models.AutoVideoRun
	if a.DB.First(&run, id).Error != nil {
		fail(c, 404, "任务不存在")
		return
	}
	var item models.AutoVideoRunItem
	if a.DB.Where("run_id=? AND shot_id=?", run.ID, run.CurrentShotID).First(&item).Error != nil {
		fail(c, 404, "当前分镜记录不存在")
		return
	}
	if input.ResourceID == 0 && item.CandidateResourceID != nil {
		input.ResourceID = *item.CandidateResourceID
	}
	if input.ResourceID == 0 {
		fail(c, 400, "请选择一个候选视频")
		return
	}
	if err := a.promote(item.ShotID, input.ResourceID); err != nil {
		fail(c, 500, "采用视频失败："+err.Error())
		return
	}
	a.DB.Model(&item).Updates(map[string]any{"status": "passed", "accepted_resource_id": input.ResourceID})
	if a.DB.Model(&run).Where("id = ? AND status IN ?", run.ID, []string{"paused", "pause_requested", "running"}).Updates(map[string]any{"status": "running", "pause_reason": "", "error_message": ""}).RowsAffected == 0 {
		fail(c, 409, "任务当前不能继续")
		return
	}
	a.advance(run.ID, item.ShotID)
	a.launch(run.ID)
	a.respond(c, run.ID)
}

func (a *AutoVideoController) setRunStatus(c *gin.Context, status string) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var run models.AutoVideoRun
	if a.DB.First(&run, id).Error != nil {
		fail(c, 404, "任务不存在")
		return
	}
	if status == "cancelled" {
		a.DB.Model(&run).Updates(map[string]any{"status": "cancelled", "stage": ""})
	} else {
		a.DB.Model(&run).Update("status", status)
	}
	a.respond(c, uint(id))
}

func (a *AutoVideoController) respond(c *gin.Context, id uint) {
	a.respondRun(c, id, false)
}

func (a *AutoVideoController) respondRun(c *gin.Context, id uint, existing bool) {
	var run models.AutoVideoRun
	if a.DB.Preload("Items", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order,id") }).First(&run, id).Error != nil {
		fail(c, 404, "任务不存在")
		return
	}
	for i := range run.Items {
		if run.Items[i].ReviewJSON != "" {
			var v any
			if json.Unmarshal([]byte(run.Items[i].ReviewJSON), &v) == nil {
				run.Items[i].Review = v
			}
		}
		if run.Items[i].CandidateResourceID != nil {
			var resource models.Resource
			if a.DB.First(&resource, *run.Items[i].CandidateResourceID).Error == nil {
				fillResourceURLs(&resource, a.Storage)
				run.Items[i].CandidateVideoURL = resource.VideoURL
			}
		}
	}
	out := gin.H{"run": run}
	if existing {
		out["existing"] = true
	}
	c.JSON(200, out)
}

func (a *AutoVideoController) requireVideoProviderReady(shotID uint) (string, bool) {
	if a.Shot == nil || a.Ark == nil {
		return "", true
	}
	ctx, msg, ok := a.Shot.buildGenerateContext(models.Shot{ID: shotID})
	if !ok {
		return msg, false
	}
	if err := a.Ark.EnsureDoubaoWebReady(ctx.Provider); err != nil {
		return "doubao-web-api 未启动，请先前往设置中心启动，等待状态显示“运行中”后再生成视频", false
	}
	return "", true
}

func (a *AutoVideoController) launch(id uint) {
	a.mu.Lock()
	if a.running == nil {
		a.running = map[uint]bool{}
	}
	if a.running[id] {
		a.mu.Unlock()
		return
	}
	a.running[id] = true
	a.mu.Unlock()
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				msg := fmt.Sprintf("全自动任务异常中断：%v", rec)
				log.Printf("auto_video run %d panic: %v", id, rec)
				a.DB.Model(&models.AutoVideoRun{}).Where("id = ?", id).Updates(map[string]any{
					"status": "paused", "stage": "", "pause_reason": msg, "error_message": msg,
				})
			}
			a.mu.Lock()
			delete(a.running, id)
			a.mu.Unlock()
		}()
		a.execute(id)
	}()
}

func (a *AutoVideoController) ResumeInterrupted() {
	var runs []models.AutoVideoRun
	a.DB.Where("status IN ?", []string{"running", "pause_requested"}).Find(&runs)
	for _, r := range runs {
		if r.Status == "pause_requested" {
			a.DB.Model(&r).Updates(map[string]any{"status": "paused", "pause_reason": "服务重启时任务正在暂停"})
			continue
		}
		a.launch(r.ID)
	}
	go a.watchRunning()
}

func (a *AutoVideoController) watchRunning() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		var runs []models.AutoVideoRun
		if a.DB.Where("status = ?", "running").Find(&runs).Error != nil {
			continue
		}
		for _, r := range runs {
			a.ensureRunWorker(r)
		}
	}
}

func (a *AutoVideoController) ensureRunWorker(run models.AutoVideoRun) {
	if run.Status != "running" {
		return
	}
	a.mu.Lock()
	alive := a.running[run.ID]
	a.mu.Unlock()
	if alive {
		return
	}
	log.Printf("auto_video: reattaching worker for run %d", run.ID)
	a.launch(run.ID)
}

func (a *AutoVideoController) execute(id uint) {
	genFails := map[uint]int{}
	for {
		var run models.AutoVideoRun
		if a.DB.First(&run, id).Error != nil || run.Status == "cancelled" || run.Status == "completed" || run.Status == "paused" {
			return
		}
		if run.Status == "pause_requested" {
			a.DB.Model(&run).Updates(map[string]any{"status": "paused", "stage": "", "pause_reason": "已按要求暂停"})
			return
		}
		var item models.AutoVideoRunItem
		if a.DB.Where("run_id=? AND status NOT IN ?", id, []string{"passed"}).Order("sort_order,id").First(&item).Error != nil {
			a.DB.Model(&run).Updates(map[string]any{"status": "completed", "stage": "", "current_shot_id": 0})
			return
		}
		a.DB.Model(&run).Updates(map[string]any{"current_shot_id": item.ShotID, "stage": "reviewing"})
		var shot models.Shot
		if a.DB.First(&shot, item.ShotID).Error != nil {
			a.pauseError(&run, &item, "分镜不存在")
			return
		}
		// Existing official video is reviewed before spending another generation.
		if item.Attempts == 0 && shot.VideoURL != "" {
			review, err := a.reviewOfficial(run.ProjectID, shot)
			if err != nil {
				if isRetryableVideoDownloadError(err) {
					log.Printf("auto_video: shot %d official video unplayable, regenerating: %v", shot.ID, err)
				} else {
					a.pauseError(&run, &item, err.Error())
					return
				}
			} else {
				a.saveReview(&item, review)
				if review.Passed {
					a.DB.Model(&item).Update("status", "passed")
					a.advance(run.ID, shot.ID)
					continue
				}
			}
		}
		// A downloaded candidate survives a restart. Review it instead of submitting
		// another paid generation request.
		if item.CandidateResourceID != nil && item.Status == "reviewing" {
			var candidate models.Resource
			if a.DB.First(&candidate, *item.CandidateResourceID).Error == nil {
				review, err := a.reviewResource(run.ProjectID, shot, candidate)
				if err != nil {
					if isRetryableVideoDownloadError(err) {
						log.Printf("auto_video: shot %d candidate unplayable, regenerating: %v", shot.ID, err)
						a.DB.Model(&item).Updates(map[string]any{"candidate_resource_id": nil, "status": "pending"})
						item.CandidateResourceID = nil
					} else {
						a.pauseError(&run, &item, "自动审核失败："+err.Error())
						return
					}
				} else {
					a.saveReview(&item, review)
					if review.Passed {
						if err := a.promote(shot.ID, candidate.ID); err != nil {
							a.pauseError(&run, &item, "采用合格视频失败："+err.Error())
							return
						}
						a.DB.Model(&item).Updates(map[string]any{"status": "passed", "accepted_resource_id": candidate.ID})
						a.advance(run.ID, shot.ID)
						continue
					}
					if item.Attempts >= run.MaxRetries+1 {
						a.pauseFailed(&run, &item)
						return
					}
					item.FailureSummary = strings.Join(review.Failures, "；")
					a.DB.Model(&item).Updates(map[string]any{"failure_summary": item.FailureSummary, "status": "pending"})
					item.Status = "pending"
				}
			}
		}
		if item.Attempts >= run.MaxRetries+1 {
			a.pauseFailed(&run, &item)
			return
		}
		if a.hasDialogue(shot.Script) && !a.asrConfigured() {
			a.pauseError(&run, &item, "本镜包含对白，请先在设置中心配置火山引擎语音识别")
			return
		}
		a.DB.Model(&run).Updates(map[string]any{"stage": "generating", "error_message": ""})
		previousAttempts := item.Attempts
		item.Attempts = previousAttempts + 1
		a.DB.Model(&item).Updates(map[string]any{"status": "generating", "attempts": item.Attempts})
		resource, err := a.generateCandidate(&item, shot, item.FailureSummary)
		if a.itemPassed(item.ID) {
			continue
		}
		if err != nil {
			// A provider/browser failure did not produce a candidate version and must
			// not consume one of the three review attempts.
			genFails[item.ShotID]++
			genUpdates := map[string]any{"attempts": previousAttempts, "status": "pending"}
			if !isKeepVideoTaskError(err) {
				genUpdates["video_task_id"] = ""
				item.VideoTaskID = ""
			}
			a.DB.Model(&item).Updates(genUpdates)
			item.Attempts = previousAttempts
			if genFails[item.ShotID] >= 3 {
				a.pauseError(&run, &item, fmt.Sprintf("视频生成连续失败 %d 次：%s", genFails[item.ShotID], err))
				return
			}
			log.Printf("auto_video: shot %d generate retry %d/3: %v", item.ShotID, genFails[item.ShotID], err)
			a.DB.Model(&run).Update("error_message", fmt.Sprintf("第 %d 镜生成失败，正在自动重试（%d/3）：%s", item.SortOrder, genFails[item.ShotID], err))
			select {
			case <-time.After(2 * time.Second):
			}
			continue
		}
		genFails[item.ShotID] = 0
		a.DB.Model(&item).Updates(map[string]any{"candidate_resource_id": resource.ID, "status": "reviewing"})
		a.DB.Model(&run).Update("stage", "reviewing")
		if a.itemPassed(item.ID) {
			continue
		}
		review, err := a.reviewResource(run.ProjectID, shot, resource)
		if a.itemPassed(item.ID) {
			continue
		}
		if err != nil {
			if isRetryableVideoDownloadError(err) {
				genFails[item.ShotID]++
				a.DB.Model(&item).Updates(map[string]any{"attempts": previousAttempts, "status": "pending", "candidate_resource_id": nil})
				item.Attempts = previousAttempts
				item.CandidateResourceID = nil
				if genFails[item.ShotID] >= 3 {
					a.pauseError(&run, &item, fmt.Sprintf("视频下载连续不完整 %d 次：%s", genFails[item.ShotID], err))
					return
				}
				log.Printf("auto_video: shot %d incomplete download retry %d/3: %v", item.ShotID, genFails[item.ShotID], err)
				a.DB.Model(&run).Update("error_message", fmt.Sprintf("第 %d 镜下载不完整，正在自动重试（%d/3）：%s", item.SortOrder, genFails[item.ShotID], err))
				continue
			}
			a.pauseError(&run, &item, "自动审核失败："+err.Error())
			return
		}
		a.saveReview(&item, review)
		a.DB.Model(&item).Update("video_task_id", "")
		item.VideoTaskID = ""
		if a.itemPassed(item.ID) {
			continue
		}
		if !review.Passed {
			item.FailureSummary = strings.Join(review.Failures, "；")
			if item.Attempts >= run.MaxRetries+1 {
				a.pauseFailed(&run, &item)
				return
			}
			a.DB.Model(&item).Updates(map[string]any{"failure_summary": item.FailureSummary, "status": "pending"})
			item.Status = "pending"
			continue
		}
		if err := a.promote(shot.ID, resource.ID); err != nil {
			a.pauseError(&run, &item, "采用合格视频失败："+err.Error())
			return
		}
		a.DB.Model(&item).Updates(map[string]any{"status": "passed", "accepted_resource_id": resource.ID})
		a.advance(run.ID, shot.ID)
	}
}

func (a *AutoVideoController) generateCandidate(item *models.AutoVideoRunItem, shot models.Shot, feedback string) (models.Resource, error) {
	ctx, msg, ok := a.Shot.buildGenerateContext(shot)
	if !ok {
		return models.Resource{}, fmt.Errorf("%s", msg)
	}
	if strings.TrimSpace(feedback) != "" {
		ctx.Input.Script = "【上次成片审核未通过，本次必须修正】\n" + feedback + "\n\n【原分镜要求】\n" + ctx.Input.Script
	}
	task := strings.TrimSpace(item.VideoTaskID)
	var err error
	if task == "" {
		task, err = a.Ark.StartVideoTask(ctx.Provider, ctx.Model, ctx.Input)
		if err != nil {
			return models.Resource{}, err
		}
		item.VideoTaskID = task
		if err = a.DB.Model(item).Update("video_task_id", task).Error; err != nil {
			return models.Resource{}, fmt.Errorf("保存视频任务 ID 失败：%w", err)
		}
	}
	remote, err := a.Ark.WaitVideoTask(ctx.Provider, task, nil)
	if err != nil {
		return models.Resource{}, err
	}
	data, err := a.Ark.DownloadVideo(remote)
	if err != nil {
		return models.Resource{}, err
	}
	meta := &VideoGenMeta{Script: ctx.Input.Script, VisualStyle: ctx.Input.VisualStyle, ProjectStyle: firstNonEmpty(ctx.Input.LookPack, ctx.Input.Style), ModelName: ctx.Model.Name, ModelID: ctx.Model.ModelID, ProviderName: ctx.Provider.Name}
	res, err := createVideoResource(a.DB, a.Storage, ctx.Project.ID, shot, data, "auto_candidate", "mp4", meta)
	if err == nil {
		a.DB.Model(&res).Update("remark", "全自动生成候选，审核通过前不会覆盖当前成片")
	}
	return res, err
}

func (a *AutoVideoController) promote(shotID, resourceID uint) error {
	var shot models.Shot
	if err := a.DB.First(&shot, shotID).Error; err != nil {
		return err
	}
	var ep models.Episode
	if err := a.DB.First(&ep, shot.EpisodeID).Error; err != nil {
		return err
	}
	var r models.Resource
	if err := a.DB.First(&r, "id=? AND project_id=? AND shot_id=? AND type='video'", resourceID, ep.ProjectID, shotID).Error; err != nil {
		return err
	}
	data, err := a.Storage.ReadFile(r.VideoPath)
	if err != nil {
		return err
	}
	if err = services.ValidateDownloadedVideo(data, 0); err != nil {
		return fmt.Errorf("当前候选无法播放：%w", err)
	}
	ext := strings.TrimPrefix(filepath.Ext(r.VideoPath), ".")
	if ext == "" {
		ext = "mp4"
	}
	if _, err = a.Storage.SaveVideo(ep.ProjectID, shotID, data, ext); err != nil {
		return err
	}
	shot.VideoURL = a.Storage.PublicURL("videos", ep.ProjectID, shotID, ext)
	shot.ActiveVideoResourceID = &r.ID
	shot.Status = "done"
	shot.ErrorMessage = ""
	shot.VideoETA = ""
	return a.DB.Save(&shot).Error
}

func (a *AutoVideoController) advance(runID, shotID uint) {
	var run models.AutoVideoRun
	a.DB.First(&run, runID)
	run.PassedCount++
	var next models.AutoVideoRunItem
	if a.DB.Where("run_id=? AND status NOT IN ?", runID, []string{"passed"}).Order("sort_order,id").First(&next).Error != nil {
		a.DB.Model(&run).Updates(map[string]any{"passed_count": run.PassedCount, "status": "completed", "stage": "", "current_shot_id": 0})
		return
	}
	a.DB.Model(&run).Updates(map[string]any{
		"passed_count":    run.PassedCount,
		"current_shot_id": next.ShotID,
		"stage":           "extracting_frame",
		"pause_reason":    "",
		"error_message":   "",
	})
	if err := a.attachTailFrame(shotID, next.ShotID, run.ProjectID); err != nil {
		msg := "承接上一镜尾帧失败：" + err.Error()
		a.DB.Model(&run).Updates(map[string]any{"status": "paused", "pause_reason": msg, "error_message": msg, "stage": ""})
	}
}

func (a *AutoVideoController) attachTailFrame(prevID, nextID, projectID uint) error {
	data, ext, err := a.Storage.ReadShotVideo(projectID, prevID)
	if err != nil {
		return err
	}
	frame, err := extractLastFrame(data, ext)
	if err != nil {
		return err
	}
	var prev, next models.Shot
	a.DB.First(&prev, prevID)
	a.DB.First(&next, nextID)
	res := models.Resource{ProjectID: projectID, Type: "scene", Source: "transition", Name: fmt.Sprintf("上一镜尾帧 · %s", prev.Label), Description: "全自动任务提取的上一镜真实尾帧", GenType: "transition_frame", GenRefsJSON: next.RefsJSON}
	if err = a.DB.Create(&res).Error; err != nil {
		return err
	}
	path, err := a.Storage.SaveResourceImageBytes(projectID, res.ID, frame)
	if err != nil {
		return err
	}
	res.ImagePath = path
	a.DB.Save(&res)
	refs := decodeShotRefs(next.RefsJSON, next.CharacterRefsJSON, next.CharacterIDsJSON, next.SceneID)
	out := []models.ShotRef{{Kind: "scene", ID: res.ID, Variant: "original", Label: "上一镜尾帧"}}
	for _, r := range refs {
		var old models.Resource
		if a.DB.Select("gen_type").First(&old, r.ID).Error == nil && old.GenType == "transition_frame" {
			continue
		}
		out = append(out, r)
	}
	next.RefsJSON = encodeShotRefs(out)
	return a.DB.Save(&next).Error
}

func (a *AutoVideoController) reviewOfficial(projectID uint, shot models.Shot) (videoReview, error) {
	data, ext, err := a.Storage.ReadShotVideo(projectID, shot.ID)
	if err != nil {
		return videoReview{}, err
	}
	return a.reviewBytes(projectID, shot, data, ext)
}
func (a *AutoVideoController) reviewResource(_ uint, shot models.Shot, r models.Resource) (videoReview, error) {
	data, err := a.Storage.ReadFile(r.VideoPath)
	if err != nil {
		return videoReview{}, err
	}
	return a.reviewBytes(r.ProjectID, shot, data, strings.TrimPrefix(filepath.Ext(r.VideoPath), "."))
}

func (a *AutoVideoController) reviewBytes(projectID uint, shot models.Shot, data []byte, ext string) (videoReview, error) {
	if err := services.ValidateDownloadedVideo(data, 0); err != nil {
		return videoReview{}, fmt.Errorf("视频文件不完整：%w", err)
	}
	path, err := writeTempFile(data, ext)
	if err != nil {
		return videoReview{}, err
	}
	defer removeTemp(path)
	review := videoReview{Passed: true, Checks: []string{}}
	probe, err := probeVideo(path)
	if err != nil {
		return review, err
	}
	if d := probe.Duration - float64(shot.Duration); d > 0.5 || d < -0.5 {
		review.Failures = append(review.Failures, fmt.Sprintf("视频时长 %.1f 秒，与要求 %d 秒不符", probe.Duration, shot.Duration))
	} else {
		review.Checks = append(review.Checks, "时长正确")
	}
	if !probe.HasVideo {
		review.Failures = append(review.Failures, "没有可解码的视频轨道")
	} else {
		review.Checks = append(review.Checks, "视频轨道正常")
	}
	var project models.Project
	if a.DB.Select("video_ratio").First(&project, projectID).Error == nil && !ratioMatches(project.VideoRatio, probe.Width, probe.Height) {
		review.Failures = append(review.Failures, fmt.Sprintf("画面比例不符：实际 %d×%d，要求 %s", probe.Width, probe.Height, firstNonEmpty(project.VideoRatio, "16:9")))
	} else if probe.Width > 0 && probe.Height > 0 {
		review.Checks = append(review.Checks, "画面比例正确")
	}
	if want := resolutionPixels(shot.Resolution); want > 0 && minInt(probe.Width, probe.Height) < want-16 {
		review.Failures = append(review.Failures, fmt.Sprintf("分辨率不足：实际 %d×%d，要求 %s", probe.Width, probe.Height, shot.Resolution))
	} else if probe.Width > 0 {
		review.Checks = append(review.Checks, "分辨率符合要求")
	}
	black, frozen := detectBrokenVisual(path, probe.Duration)
	if black {
		review.Failures = append(review.Failures, "视频存在持续严重黑屏")
	} else {
		review.Checks = append(review.Checks, "无持续黑屏")
	}
	if frozen {
		review.Failures = append(review.Failures, "视频存在长时间静帧")
	} else {
		review.Checks = append(review.Checks, "无长时间静帧")
	}
	if a.hasDialogue(shot.Script) {
		if !probe.HasAudio {
			review.Failures = append(review.Failures, "有对白要求但视频没有音轨")
		} else {
			txt, e := a.transcribe(path)
			if e != nil {
				return review, e
			}
			review.Transcript = txt
			for _, want := range dialogueLines(shot.Script) {
				if !strings.Contains(normalizeSpeech(txt), normalizeSpeech(want)) {
					review.Failures = append(review.Failures, "对白缺失或错误："+want)
				}
			}
		}
	}
	frames, err := extractReviewFrames(path)
	if err != nil {
		return review, err
	}
	visual, err := a.visualReview(shot, frames)
	if err != nil {
		return review, err
	}
	review.Checks = append(review.Checks, visual.Checks...)
	review.Failures = append(review.Failures, visual.Failures...)
	review.Passed = len(review.Failures) == 0
	return review, nil
}

type videoProbe struct {
	Duration           float64
	HasVideo, HasAudio bool
	Width, Height      int
}

func probeVideo(path string) (videoProbe, error) {
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration:stream=codec_type,width,height", "-of", "json", path).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "moov atom not found") || strings.Contains(msg, "Invalid data found") {
			return videoProbe{}, fmt.Errorf("视频文件不完整（下载被截断，豆包页能播但本地文件缺索引）")
		}
		if msg != "" {
			if i := strings.IndexByte(msg, '\n'); i > 0 {
				msg = msg[:i]
			}
			return videoProbe{}, fmt.Errorf("视频无法解码：%s", msg)
		}
		return videoProbe{}, fmt.Errorf("视频无法解码：%w", err)
	}
	var v struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &v) != nil {
		return videoProbe{}, fmt.Errorf("ffprobe 输出无效")
	}
	d, _ := strconv.ParseFloat(v.Format.Duration, 64)
	p := videoProbe{Duration: d}
	for _, s := range v.Streams {
		p.HasVideo = p.HasVideo || s.CodecType == "video"
		p.HasAudio = p.HasAudio || s.CodecType == "audio"
		if s.CodecType == "video" {
			p.Width, p.Height = s.Width, s.Height
		}
	}
	return p, nil
}

func ratioMatches(want string, width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	parts := strings.Split(strings.TrimSpace(firstNonEmpty(want, "16:9")), ":")
	if len(parts) != 2 {
		return true
	}
	w, e1 := strconv.ParseFloat(parts[0], 64)
	h, e2 := strconv.ParseFloat(parts[1], 64)
	if e1 != nil || e2 != nil || h == 0 {
		return true
	}
	actual := float64(width) / float64(height)
	return actual >= w/h*0.97 && actual <= w/h*1.03
}

func resolutionPixels(s string) int {
	v := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "p")
	n, _ := strconv.Atoi(v)
	return n
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func detectBrokenVisual(path string, duration float64) (black, frozen bool) {
	out, _ := exec.Command("ffmpeg", "-hide_banner", "-i", path, "-vf", "blackdetect=d=1:pix_th=.10,freezedetect=n=-50dB:d=2", "-an", "-f", "null", "-").CombinedOutput()
	s := string(out)
	for _, m := range regexp.MustCompile(`black_duration:([0-9.]+)`).FindAllStringSubmatch(s, -1) {
		d, _ := strconv.ParseFloat(m[1], 64)
		if d >= duration*0.5 {
			black = true
		}
	}
	for _, m := range regexp.MustCompile(`freeze_duration: ([0-9.]+)`).FindAllStringSubmatch(s, -1) {
		d, _ := strconv.ParseFloat(m[1], 64)
		if d >= duration*0.5 {
			frozen = true
		}
	}
	return
}
func extractReviewFrames(path string) ([][]byte, error) {
	dir, err := os.MkdirTemp("", "novaly-review-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	pattern := filepath.Join(dir, "%02d.jpg")
	if out, e := exec.Command("ffmpeg", "-v", "error", "-i", path, "-vf", "fps=1/2,scale='min(640,iw)':-2", "-frames:v", "6", pattern).CombinedOutput(); e != nil {
		return nil, fmt.Errorf("抽帧失败：%v %s", e, out)
	}
	names, _ := filepath.Glob(filepath.Join(dir, "*.jpg"))
	var frames [][]byte
	for _, n := range names {
		b, _ := os.ReadFile(n)
		if len(b) > 0 {
			frames = append(frames, b)
		}
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("没有抽取到审核帧")
	}
	return frames, nil
}

func (a *AutoVideoController) reviewModel() (models.AIProvider, models.AIModel, error) {
	var p models.AIProvider
	if a.DB.Where("slug=? AND enabled=?", "volcengine-ark", true).First(&p).Error != nil || p.APIKey == "" {
		return p, models.AIModel{}, fmt.Errorf("请先在设置中心配置火山引擎 API Key，自动审核需要 Doubao Seed 2.0 Pro")
	}
	var m models.AIModel
	if a.DB.Where("provider_id=? AND model_id LIKE ? AND capability='text' AND enabled=?", p.ID, "doubao-seed-2-0-pro%", true).First(&m).Error != nil {
		return p, m, fmt.Errorf("请先启用火山引擎 Doubao Seed 2.0 Pro 文本模型")
	}
	return p, m, nil
}
func (a *AutoVideoController) visualReview(shot models.Shot, frames [][]byte) (videoReview, error) {
	p, m, err := a.reviewModel()
	if err != nil {
		return videoReview{}, err
	}
	parts := []map[string]any{{"type": "text", "text": "你是严格的短剧成片监制。先给出的是分镜参考图（若第一张名为上一镜尾帧，必须检查首帧连续性），其后是按时间排列的成片抽帧。检查人物身份与数量、场景、服装、主要动作和站位、画风、明显变形、字幕、水印、logo。只返回 JSON：{\"passed\":true,\"checks\":[\"...\"],\"failures\":[\"...\"]}。无法从抽帧确认的细节不要判失败。\n分镜要求：\n" + shot.Script}}
	var ep models.Episode
	if a.DB.First(&ep, shot.EpisodeID).Error == nil {
		refs := a.Shot.loadVideoRefs(ep.ProjectID, shot)
		for i, ref := range refs {
			if i >= 6 {
				break
			}
			path := ref.Resource.StylizedImagePath
			if ref.Variant == "original" || path == "" {
				path = ref.Resource.ImagePath
			}
			if b, e := a.Storage.ReadFile(path); e == nil && len(b) > 0 {
				parts = append(parts, map[string]any{"type": "text", "text": "参考图：" + ref.Label}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + http.DetectContentType(b) + ";base64," + base64.StdEncoding.EncodeToString(b)}})
			}
		}
	}
	parts = append(parts, map[string]any{"type": "text", "text": "以下为成片抽帧："})
	for _, b := range frames {
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)}})
	}
	raw, err := a.Ark.Chat(p, map[string]any{"model": m.ModelID, "messages": []map[string]any{{"role": "user", "content": parts}}, "temperature": 0})
	if err != nil {
		return videoReview{}, err
	}
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "```json"), "```"))
	var v videoReview
	if json.Unmarshal([]byte(raw), &v) != nil {
		return v, fmt.Errorf("审核模型返回格式错误")
	}
	return v, nil
}

func (a *AutoVideoController) hasDialogue(s string) bool { return len(dialogueLines(s)) > 0 }

var dialogueRE = regexp.MustCompile(`\{([^{}]+)\}`)

func dialogueLines(s string) []string {
	m := dialogueRE.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(m))
	for _, x := range m {
		if strings.TrimSpace(x[1]) != "" {
			out = append(out, strings.TrimSpace(x[1]))
		}
	}
	return out
}
func normalizeSpeech(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(" ，。！？、；：,.!?;:\"'“”‘’（）()【】[]\n\r\t", r) {
			return -1
		}
		return r
	}, strings.ToLower(s))
}
func (a *AutoVideoController) setting(k string) string {
	var s models.AppSetting
	if a.DB.First(&s, "key=?", k).Error != nil {
		return ""
	}
	return strings.TrimSpace(s.Value)
}
func (a *AutoVideoController) asrConfigured() bool {
	return a.setting("volc_asr_app_id") != "" && a.setting("volc_asr_token") != ""
}
func (a *AutoVideoController) transcribe(videoPath string) (string, error) {
	if !a.asrConfigured() {
		return "", fmt.Errorf("请先配置火山引擎语音识别")
	}
	wav := videoPath + ".wav"
	defer os.Remove(wav)
	if out, e := exec.Command("ffmpeg", "-v", "error", "-y", "-i", videoPath, "-ac", "1", "-ar", "16000", "-f", "wav", wav).CombinedOutput(); e != nil {
		return "", fmt.Errorf("提取音轨失败：%v %s", e, out)
	}
	b, e := os.ReadFile(wav)
	if e != nil {
		return "", e
	}
	body := map[string]any{"app": map[string]any{"appid": a.setting("volc_asr_app_id"), "token": a.setting("volc_asr_token"), "cluster": firstNonEmpty(a.setting("volc_asr_cluster"), "volcengine_input_common")}, "user": map[string]any{"uid": "novaly"}, "audio": map[string]any{"format": "wav", "rate": 16000, "bits": 16, "channel": 1, "language": "zh-CN", "data": base64.StdEncoding.EncodeToString(b)}, "request": map[string]any{"reqid": uuid.NewString(), "sequence": 1}}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", firstNonEmpty(a.setting("volc_asr_url"), "https://openspeech.bytedance.com/api/v1/vc/submit"), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("火山语音识别失败（HTTP %d）", resp.StatusCode)
	}
	var v struct {
		Result []struct {
			Text string `json:"text"`
		} `json:"result"`
		Message string `json:"message"`
	}
	if json.Unmarshal(rb, &v) != nil || len(v.Result) == 0 {
		return "", fmt.Errorf("火山语音识别没有返回文字：%s", v.Message)
	}
	return v.Result[0].Text, nil
}
func (a *AutoVideoController) saveReview(item *models.AutoVideoRunItem, r videoReview) {
	b, _ := json.Marshal(r)
	item.ReviewJSON = string(b)
	item.FailureSummary = strings.Join(r.Failures, "；")
	a.DB.Model(item).Where("status <> ?", "passed").Updates(map[string]any{"review_json": item.ReviewJSON, "failure_summary": item.FailureSummary, "status": "reviewing"})
}

func (a *AutoVideoController) itemPassed(id uint) bool {
	var latest models.AutoVideoRunItem
	if a.DB.Select("status").First(&latest, id).Error != nil {
		return false
	}
	return latest.Status == "passed"
}
func (a *AutoVideoController) pauseError(run *models.AutoVideoRun, item *models.AutoVideoRunItem, msg string) {
	a.DB.Model(item).Update("status", "paused")
	a.DB.Model(run).Updates(map[string]any{"status": "paused", "stage": "", "pause_reason": msg, "error_message": msg})
}
func isRetryableVideoDownloadError(err error) bool {
	if err == nil {
		return false
	}
	return isRetryableVideoDownloadMessage(err.Error())
}

func isKeepVideoTaskError(err error) bool {
	if err == nil {
		return false
	}
	if isRetryableVideoDownloadError(err) {
		return true
	}
	s := strings.ToLower(err.Error())
	keys := []string{"超时", "timeout", "timed out", "deadline exceeded", "connection reset", "broken pipe", "eof", "websocket", "cdp"}
	for _, key := range keys {
		if strings.Contains(s, key) {
			return true
		}
	}
	return false
}

func isRetryableVideoDownloadMessage(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, key := range []string{"截断", "缺少 moov", "不完整", "下载视频过短", "下载视频失败", "moov atom not found", "无法解码"} {
		if strings.Contains(s, key) {
			return true
		}
	}
	return false
}

func autoVideoPauseFailedReason(sortOrder, attempts int, summary string) string {
	n := attempts
	if n < 1 {
		n = 3
	}
	msg := fmt.Sprintf("第 %d 镜连续 %d 个版本未通过自动审核", sortOrder, n)
	if s := strings.TrimSpace(summary); s != "" {
		msg += "：" + s
	}
	return msg + "。可人工采用当前版本或再次重试。"
}

func (a *AutoVideoController) pauseFailed(run *models.AutoVideoRun, item *models.AutoVideoRunItem) {
	msg := autoVideoPauseFailedReason(item.SortOrder, item.Attempts, item.FailureSummary)
	a.DB.Model(item).Updates(map[string]any{"status": "failed", "failure_summary": item.FailureSummary, "attempts": item.Attempts})
	a.DB.Model(run).Updates(map[string]any{"status": "paused", "stage": "", "pause_reason": msg, "error_message": msg})
}

func (a *AutoVideoController) GetASRSettings(c *gin.Context) {
	c.JSON(200, gin.H{"configured": a.asrConfigured(), "appIdMasked": maskKey(a.setting("volc_asr_app_id")), "tokenMasked": maskKey(a.setting("volc_asr_token")), "cluster": firstNonEmpty(a.setting("volc_asr_cluster"), "volcengine_input_common"), "url": firstNonEmpty(a.setting("volc_asr_url"), "https://openspeech.bytedance.com/api/v1/vc/submit")})
}
func (a *AutoVideoController) SaveASRSettings(c *gin.Context) {
	var in struct{ AppID, Token, Cluster, URL string }
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	vals := map[string]string{"volc_asr_app_id": in.AppID, "volc_asr_token": in.Token, "volc_asr_cluster": in.Cluster, "volc_asr_url": in.URL}
	for k, v := range vals {
		if strings.TrimSpace(v) == "" && (k == "volc_asr_app_id" || k == "volc_asr_token") {
			continue
		}
		a.DB.Save(&models.AppSetting{Key: k, Value: strings.TrimSpace(v)})
	}
	a.GetASRSettings(c)
}
func (a *AutoVideoController) TestASR(c *gin.Context) {
	if !a.asrConfigured() {
		fail(c, 400, "请先填写 App ID 和 Access Token")
		return
	}
	c.JSON(200, gin.H{"message": "配置已保存；实际连通性会在首个含对白视频审核时验证"})
}
