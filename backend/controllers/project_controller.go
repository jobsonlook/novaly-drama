package controllers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"novaly/backend/models"
	"novaly/backend/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ProjectController struct {
	DB      *gorm.DB
	Storage *services.Storage
}

func (pc *ProjectController) List(c *gin.Context) {
	c.JSON(200, pc.summaries(pc.DB.Order("created_at desc")))
}

func (pc *ProjectController) ListTrash(c *gin.Context) {
	c.JSON(200, pc.summaries(pc.DB.Unscoped().Where("deleted_at IS NOT NULL").Order("deleted_at desc")))
}

func (pc *ProjectController) summaries(q *gorm.DB) []models.ProjectSummary {
	var items []models.Project
	if err := q.Find(&items).Error; err != nil {
		return []models.ProjectSummary{}
	}
	result := make([]models.ProjectSummary, 0, len(items))
	for _, p := range items {
		var shotCount int64
		pc.DB.Model(&models.Shot{}).Joins("JOIN episodes ON episodes.id = shots.episode_id").Where("episodes.project_id = ?", p.ID).Count(&shotCount)
		summary := models.ProjectSummary{
			ID: p.ID, Title: p.Title, EpisodeCount: p.EpisodeCount,
			Kind: firstNonEmpty(p.Kind, "script"), Genre: p.Genre, Synopsis: p.Synopsis,
			VisualManual: p.VisualManual, DirectorManual: p.DirectorManual,
			Style: p.Style, VideoRatio: firstNonEmpty(p.VideoRatio, "16:9"),
			StoryboardPace: firstNonEmpty(p.StoryboardPace, "fine"),
			ShotCount: int(shotCount), CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		}
		if p.DeletedAt.Valid {
			t := p.DeletedAt.Time
			summary.DeletedAt = &t
		}
		result = append(result, summary)
	}
	return result
}

func (pc *ProjectController) Get(c *gin.Context) {
	id := parseID(c.Param("id"))
	if id == 0 {
		fail(c, 404, "项目不存在")
		return
	}
	_ = pc.resyncVideoResourceNames(id)
	project, ok := pc.loadProjectByID(id)
	if !ok {
		fail(c, 404, "项目不存在")
		return
	}
	c.JSON(200, projectDTO(project, pc.DB, pc.Storage))
}

func (pc *ProjectController) Create(c *gin.Context) {
	var input struct {
		Title          string `json:"title"`
		Kind           string `json:"kind"`
		Genre          string `json:"genre"`
		Synopsis       string `json:"synopsis"`
		VisualManual   string `json:"visualManual"`
		DirectorManual string `json:"directorManual"`
		Style          string `json:"style"`
		VideoRatio     string `json:"videoRatio"`
		StoryboardPace string `json:"storyboardPace"`
	}
	if c.ShouldBindJSON(&input) != nil || strings.TrimSpace(input.Title) == "" {
		fail(c, 400, "请填写项目名称")
		return
	}
	kind := strings.TrimSpace(input.Kind)
	if kind != "novel" {
		kind = "script"
	}
	project := models.Project{
		Title:          strings.TrimSpace(input.Title),
		EpisodeCount:   1,
		Kind:           kind,
		Genre:          strings.TrimSpace(input.Genre),
		Synopsis:       strings.TrimSpace(input.Synopsis),
		VisualManual:   strings.TrimSpace(input.VisualManual),
		DirectorManual: strings.TrimSpace(input.DirectorManual),
		Style:          strings.TrimSpace(input.Style),
		VideoRatio:     firstNonEmpty(strings.TrimSpace(input.VideoRatio), "16:9"),
		StoryboardPace: firstNonEmpty(strings.TrimSpace(input.StoryboardPace), "fine"),
	}
	err := pc.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&project).Error; err != nil {
			return err
		}
		episode := models.Episode{
			ProjectID: project.ID,
			Number:    1,
			Title:     "第1集",
		}
		return tx.Create(&episode).Error
	})
	if err != nil {
		fail(c, 500, "创建项目失败")
		return
	}
	project, _ = pc.loadProjectByID(project.ID)
	c.JSON(201, projectDTO(project, pc.DB, pc.Storage))
}

var autoVideoNameRE = regexp.MustCompile(`^(?:第\d+集 · )?分镜\d+(?: · 版本\d+)?$`)

// resyncVideoResourceNames renames auto-generated video titles to match the current shot order per episode.
// Manually renamed titles are left untouched.
func (pc *ProjectController) resyncVideoResourceNames(projectID uint) error {
	var episodes []models.Episode
	if err := pc.DB.Where("project_id = ?", projectID).Order("number asc, id asc").Find(&episodes).Error; err != nil {
		return err
	}
	multi := len(episodes) > 1
	for _, episode := range episodes {
		var shots []models.Shot
		if err := pc.DB.Where("episode_id = ?", episode.ID).Order("sort_order asc, id asc").Find(&shots).Error; err != nil {
			return err
		}
		for i, shot := range shots {
			var resources []models.Resource
			if err := pc.DB.Where("project_id = ? AND shot_id = ? AND type = ?", projectID, shot.ID, "video").
				Order("id asc").Find(&resources).Error; err != nil {
				return err
			}
			base := fmt.Sprintf("分镜%02d", i+1)
			if multi {
				base = fmt.Sprintf("第%d集 · 分镜%02d", episode.Number, i+1)
			}
			for vi, r := range resources {
				if !autoVideoNameRE.MatchString(r.Name) {
					continue
				}
				name := base
				if vi > 0 {
					name = fmt.Sprintf("%s · 版本%d", base, vi+1)
				}
				if r.Name == name {
					continue
				}
				if err := pc.DB.Model(&r).Update("name", name).Error; err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (pc *ProjectController) Update(c *gin.Context) {
	project, ok := pc.find(c)
	if !ok {
		return
	}
	var input struct {
		Title          string `json:"title"`
		Kind           string `json:"kind"`
		Genre          string `json:"genre"`
		Synopsis       string `json:"synopsis"`
		VisualManual   string `json:"visualManual"`
		DirectorManual string `json:"directorManual"`
		Style          string `json:"style"`
		VideoRatio     string `json:"videoRatio"`
		StoryboardPace string `json:"storyboardPace"`
	}
	if c.ShouldBindJSON(&input) != nil || strings.TrimSpace(input.Title) == "" {
		fail(c, 400, "请求格式错误")
		return
	}
	kind := strings.TrimSpace(input.Kind)
	if kind != "novel" {
		kind = "script"
	}
	project.Title = strings.TrimSpace(input.Title)
	project.Kind = kind
	project.Genre = strings.TrimSpace(input.Genre)
	project.Synopsis = strings.TrimSpace(input.Synopsis)
	project.VisualManual = strings.TrimSpace(input.VisualManual)
	project.DirectorManual = strings.TrimSpace(input.DirectorManual)
	project.Style = strings.TrimSpace(input.Style)
	if v := strings.TrimSpace(input.VideoRatio); v != "" {
		project.VideoRatio = v
	}
	if p := strings.TrimSpace(input.StoryboardPace); p != "" {
		project.StoryboardPace = p
	}
	if err := pc.DB.Save(&project).Error; err != nil {
		fail(c, 500, "保存项目失败")
		return
	}
	loaded, _ := pc.loadProjectByID(project.ID)
	c.JSON(200, projectDTO(loaded, pc.DB, pc.Storage))
}

func (pc *ProjectController) Delete(c *gin.Context) {
	project, ok := pc.find(c)
	if !ok {
		return
	}
	if err := pc.DB.Delete(&project).Error; err != nil {
		fail(c, 500, "删除项目失败")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (pc *ProjectController) Restore(c *gin.Context) {
	id := parseID(c.Param("id"))
	var project models.Project
	if err := pc.DB.Unscoped().First(&project, id).Error; err != nil {
		fail(c, 404, "项目不存在")
		return
	}
	if !project.DeletedAt.Valid {
		fail(c, 400, "项目未在回收站中")
		return
	}
	if err := pc.DB.Unscoped().Model(&project).Update("deleted_at", nil).Error; err != nil {
		fail(c, 500, "恢复项目失败")
		return
	}
	loaded, ok := pc.loadProjectByID(id)
	if !ok {
		fail(c, 500, "恢复后读取项目失败")
		return
	}
	c.JSON(200, projectDTO(loaded, pc.DB, pc.Storage))
}

func (pc *ProjectController) Purge(c *gin.Context) {
	id := parseID(c.Param("id"))
	var project models.Project
	if err := pc.DB.Unscoped().First(&project, id).Error; err != nil {
		fail(c, 404, "项目不存在")
		return
	}
	if !project.DeletedAt.Valid {
		fail(c, 400, "请先将项目移入回收站")
		return
	}
	if err := pc.purgeProject(project.ID); err != nil {
		fail(c, 500, "彻底删除失败")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (pc *ProjectController) purgeProject(id uint) error {
	return pc.DB.Unscoped().Transaction(func(tx *gorm.DB) error {
		var episodeIDs []uint
		tx.Model(&models.Episode{}).Where("project_id = ?", id).Pluck("id", &episodeIDs)
		if len(episodeIDs) > 0 {
			if err := tx.Unscoped().Where("episode_id IN ?", episodeIDs).Delete(&models.Shot{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Unscoped().Where("project_id = ?", id).Delete(&models.Episode{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("project_id = ?", id).Delete(&models.Resource{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&models.Project{}, id).Error
	})
}

func (pc *ProjectController) find(c *gin.Context) (models.Project, bool) {
	var project models.Project
	if err := pc.DB.First(&project, c.Param("id")).Error; err != nil {
		fail(c, 404, "项目不存在")
		return project, false
	}
	return project, true
}

func (pc *ProjectController) loadProject(c *gin.Context) (models.Project, bool) {
	return pc.loadProjectByID(parseID(c.Param("id")))
}

func (pc *ProjectController) loadProjectByID(id uint) (models.Project, bool) {
	var project models.Project
	// Shots are loaded per-page via GET /episodes/:id?page=&pageSize= — avoid shipping hundreds of scripts on open.
	if err := pc.DB.Preload("Episodes", func(db *gorm.DB) *gorm.DB {
		return db.Order("number asc")
	}).Preload("Resources", func(db *gorm.DB) *gorm.DB {
		return db.Order("created_at desc, id desc")
	}).First(&project, id).Error; err != nil {
		return project, false
	}
	if project.Episodes == nil {
		project.Episodes = []models.Episode{}
	}
	for i := range project.Episodes {
		project.Episodes[i].Shots = []models.Shot{}
		var shotTotal int64
		pc.DB.Model(&models.Shot{}).Where("episode_id = ?", project.Episodes[i].ID).Count(&shotTotal)
		project.Episodes[i].ShotTotal = int(shotTotal)
		fillEpisodeExtract(&project.Episodes[i], pc.DB)
	}
	if project.Resources == nil {
		project.Resources = []models.Resource{}
	}
	return project, true
}

func projectDTO(project models.Project, db *gorm.DB, storage *services.Storage) models.ProjectDTO {
	// Fast path for project open: URLs only. Heavy genRefs / video copy run on paged library loads.
	for i := range project.Resources {
		fillResourceURLs(&project.Resources[i], storage)
	}
	return models.ProjectDTO{
		ID: project.ID, Title: project.Title, EpisodeCount: project.EpisodeCount,
		Kind: firstNonEmpty(project.Kind, "script"), Genre: project.Genre, Synopsis: project.Synopsis,
		VisualManual: project.VisualManual, DirectorManual: project.DirectorManual,
		Style: project.Style, VideoRatio: firstNonEmpty(project.VideoRatio, "16:9"),
		StoryboardPace: firstNonEmpty(project.StoryboardPace, "fine"),
		Episodes: project.Episodes, Resources: project.Resources,
		CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
	}
}

func fillEpisodeExtract(ep *models.Episode, db *gorm.DB) {
	if ep == nil {
		return
	}
	if strings.TrimSpace(ep.AssetsJSON) != "" {
		var assets []models.EpisodeAsset
		if json.Unmarshal([]byte(ep.AssetsJSON), &assets) == nil {
			ep.Assets = assets
		}
	}
	var job models.CrewJob
	if err := db.Select("status", "stage", "assets_json").Where("episode_id = ?", ep.ID).Order("id desc").First(&job).Error; err != nil {
		return
	}
	ep.CrewStatus = job.Status
	ep.CrewStage = job.Stage
	if len(ep.Assets) == 0 && strings.TrimSpace(job.AssetsJSON) != "" {
		var assets []models.EpisodeAsset
		if json.Unmarshal([]byte(job.AssetsJSON), &assets) == nil {
			ep.Assets = assets
		}
	}
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func resourceImageURL(r models.Resource, storage *services.Storage) string {
	if r.ImagePath == "" || !storage.FileExists(r.ImagePath) {
		return ""
	}
	key := storage.ObjectKey(r.ImagePath)
	if key == "" {
		return ""
	}
	// Prefer local bytes when available (COS may lag or point at a stale key).
	if _, err := os.Stat(r.ImagePath); err == nil {
		return "/api/uploads/" + key
	}
	if storage.COSEnabled() {
		return storage.COS.PublicURL(key)
	}
	return "/api/uploads/" + key
}

func resourceStylizedImageURL(r models.Resource, storage *services.Storage) string {
	if r.StylizedImagePath == "" || !storage.FileExists(r.StylizedImagePath) {
		return ""
	}
	url := storage.StylizedPublicURL(r.ProjectID, r.ID)
	// Same path is overwritten on regenerate; bust browser cache with UpdatedAt.
	v := r.UpdatedAt.UnixMilli()
	if v <= 0 {
		v = time.Now().UnixMilli()
	}
	return fmt.Sprintf("%s?v=%d", url, v)
}

func resourceVideoURL(r models.Resource, storage *services.Storage) string {
	if r.VideoPath == "" || !storage.FileExists(r.VideoPath) {
		return ""
	}
	ext := "mp4"
	if i := strings.LastIndex(r.VideoPath, "."); i >= 0 {
		ext = r.VideoPath[i+1:]
	}
	if strings.Contains(r.VideoPath, string(filepath.Separator)+"resources"+string(filepath.Separator)) {
		return storage.PublicURL("resources", r.ProjectID, r.ID, ext)
	}
	return ""
}

func fillResourceURLs(r *models.Resource, storage *services.Storage) {
	r.ImageURL = resourceImageURL(*r, storage)
	// Version the image URL by updated_at so async overwrites (e.g. transition-frame
	// annotation replacing the raw frame at the same path) bust the browser cache.
	if r.ImageURL != "" && !r.UpdatedAt.IsZero() {
		sep := "?"
		if strings.Contains(r.ImageURL, "?") {
			sep = "&"
		}
		r.ImageURL = fmt.Sprintf("%s%sv=%d", r.ImageURL, sep, r.UpdatedAt.Unix())
	}
	r.StylizedImageURL = resourceStylizedImageURL(*r, storage)
	if r.Type == "video" {
		r.VideoURL = resourceVideoURL(*r, storage)
	}
}

func fillResourceGenRefs(r *models.Resource, db *gorm.DB, storage *services.Storage) {
	if r == nil || db == nil || strings.TrimSpace(r.GenRefsJSON) == "" {
		r.GenRefs = nil
		return
	}
	var refs []models.ResourceGenRef
	if err := json.Unmarshal([]byte(r.GenRefsJSON), &refs); err != nil || len(refs) == 0 {
		r.GenRefs = nil
		return
	}
	for i := range refs {
		if refs[i].ID == 0 {
			continue
		}
		var src models.Resource
		if err := db.Unscoped().First(&src, refs[i].ID).Error; err != nil {
			continue
		}
		fillResourceURLs(&src, storage)
		if refs[i].Variant == "stylized" && src.StylizedImageURL != "" {
			refs[i].ImageURL = src.StylizedImageURL
		} else {
			refs[i].ImageURL = src.ImageURL
		}
		if refs[i].Label == "" {
			refs[i].Label = src.Name
		}
		if refs[i].Kind == "" {
			refs[i].Kind = src.Type
		}
	}
	r.GenRefs = refs
}

func fillResourceFields(r *models.Resource, db *gorm.DB, storage *services.Storage) {
	fillResourceURLs(r, storage)
	fillResourceGenRefs(r, db, storage)
	fillResourceParentName(r, db)
}

func fillResourceParentName(r *models.Resource, db *gorm.DB) {
	if r == nil || db == nil || r.ParentID == nil || *r.ParentID == 0 {
		return
	}
	var parent models.Resource
	if err := db.Select("name").First(&parent, *r.ParentID).Error; err != nil {
		return
	}
	r.ParentName = strings.TrimSpace(parent.Name)
}

func encodeShotRefs(refs []models.ShotRef) string {
	if len(refs) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(refs)
	return string(b)
}

func decodeShotRefs(refsJSON, charRefsJSON, legacyIDsJSON string, sceneID *uint) []models.ShotRef {
	if refsJSON != "" && refsJSON != "[]" {
		var refs []models.ShotRef
		if json.Unmarshal([]byte(refsJSON), &refs) == nil {
			return refs
		}
	}
	refs := make([]models.ShotRef, 0)
	for _, c := range decodeCharacterRefs(charRefsJSON, legacyIDsJSON) {
		variant := c.Variant
		if variant == "" {
			variant = "stylized"
		}
		refs = append(refs, models.ShotRef{Kind: "character", ID: c.ID, Variant: variant})
	}
	if sceneID != nil {
		refs = append(refs, models.ShotRef{Kind: "scene", ID: *sceneID, Variant: "original"})
	}
	return refs
}

func shotRefsToCharacterRefs(refs []models.ShotRef) []models.CharacterRef {
	out := make([]models.CharacterRef, 0)
	for _, r := range refs {
		if r.Kind != "character" {
			continue
		}
		variant := r.Variant
		if variant == "" {
			variant = "stylized"
		}
		out = append(out, models.CharacterRef{ID: r.ID, Variant: variant})
	}
	return out
}

func shotRefsFirstSceneID(refs []models.ShotRef) *uint {
	for _, r := range refs {
		if r.Kind == "scene" {
			id := r.ID
			return &id
		}
	}
	return nil
}

func fillShotFields(shot *models.Shot, storage *services.Storage) {
	shot.Refs = decodeShotRefs(shot.RefsJSON, shot.CharacterRefsJSON, shot.CharacterIDsJSON, shot.SceneID)
	shot.CharacterRefs = shotRefsToCharacterRefs(shot.Refs)
	shot.PositioningRefs = decodeResourceGenRefs(shot.PositioningRefsJSON)
	shot.MotionGridRefs = decodeResourceGenRefs(shot.MotionGridRefsJSON)
	if shot.Duration <= 0 {
		shot.Duration = 10
	}
	if shot.Resolution == "" {
		shot.Resolution = "720p"
	}
	if storage != nil {
		shot.VideoURL = storage.RewriteUploadURL(shot.VideoURL)
	}
}

func encodeResourceGenRefs(refs []models.ResourceGenRef) string {
	if len(refs) == 0 {
		return ""
	}
	clean := make([]models.ResourceGenRef, 0, len(refs))
	for _, r := range refs {
		if r.ID == 0 {
			continue
		}
		clean = append(clean, models.ResourceGenRef{
			ID: r.ID, Variant: r.Variant, Kind: r.Kind, Label: r.Label,
		})
	}
	if len(clean) == 0 {
		return ""
	}
	b, _ := json.Marshal(clean)
	return string(b)
}

func decodeResourceGenRefs(raw string) []models.ResourceGenRef {
	if strings.TrimSpace(raw) == "" || raw == "[]" {
		return nil
	}
	var refs []models.ResourceGenRef
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return nil
	}
	return refs
}

func encodeCharacterRefs(refs []models.CharacterRef) string {
	if len(refs) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(refs)
	return string(b)
}

func decodeCharacterRefs(refsJSON, legacyIDsJSON string) []models.CharacterRef {
	if refsJSON != "" && refsJSON != "[]" {
		var refs []models.CharacterRef
		if json.Unmarshal([]byte(refsJSON), &refs) == nil {
			return refs
		}
	}
	ids := decodeIDs(legacyIDsJSON)
	refs := make([]models.CharacterRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, models.CharacterRef{ID: id, Variant: "stylized"})
	}
	return refs
}

func decodeIDs(raw string) []uint {
	if raw == "" {
		return []uint{}
	}
	var ids []uint
	_ = json.Unmarshal([]byte(raw), &ids)
	return ids
}

func encodeIDs(ids []uint) string {
	if len(ids) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

func parseID(v string) uint {
	id, _ := strconv.ParseUint(v, 10, 64)
	return uint(id)
}

func fail(c *gin.Context, status int, message string) { c.JSON(status, gin.H{"error": message}) }

func failWithShot(c *gin.Context, status int, message string, shot models.Shot, storage *services.Storage) {
	fillShotFields(&shot, storage)
	c.JSON(status, gin.H{"error": message, "shot": shot})
}
