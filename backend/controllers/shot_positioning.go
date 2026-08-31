package controllers

import (
	"encoding/json"
	"fmt"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"
	"novaly/backend/services/crew"

	"github.com/gin-gonic/gin"
)

type positioningAnalyzeInput struct {
	RefLabels []string `json:"refLabels"`
}

type positioningGenerateInput struct {
	Name          string                `json:"name"`
	Prompt        string                `json:"prompt"`
	Count         int                   `json:"count"`
	Quality       string                `json:"quality"`
	Resolution    string                `json:"resolution"`
	ModelID       uint                  `json:"modelId"`
	ImageDataList []string              `json:"imageDataList"`
	ResourceRefs  []imageGenResourceRef `json:"resourceRefs"`
	Stage         string                `json:"stage"` // skeleton | final
}

func (sc *ShotController) loadDefaultTextModel() (models.AIModel, models.AIProvider, string, bool) {
	var model models.AIModel
	err := sc.DB.Where("capability = ? AND enabled = ? AND is_default = ?", "text", true, true).First(&model).Error
	if err != nil {
		err = sc.DB.Where("capability = ? AND enabled = ?", "text", true).Order("id asc").First(&model).Error
	}
	if err != nil {
		return models.AIModel{}, models.AIProvider{}, "请先在设置中心启用一个文本模型", false
	}
	var provider models.AIProvider
	if err := sc.DB.First(&provider, model.ProviderID).Error; err != nil {
		return models.AIModel{}, models.AIProvider{}, "文本模型服务商不存在", false
	}
	return model, provider, "", true
}

// loadLiteTextModel prefers an enabled *lite* text model for cheap tasks (ref matching).
func (sc *ShotController) loadLiteTextModel() (models.AIModel, models.AIProvider, string, bool) {
	var model models.AIModel
	err := sc.DB.Where(
		"capability = ? AND enabled = ? AND (LOWER(name) LIKE ? OR LOWER(model_id) LIKE ? OR LOWER(name) LIKE ? OR LOWER(model_id) LIKE ?)",
		"text", true, "%lite%", "%lite%", "%flash%", "%flash%",
	).Order("is_default desc, id asc").First(&model).Error
	if err != nil {
		return sc.loadDefaultTextModel()
	}
	var provider models.AIProvider
	if err := sc.DB.First(&provider, model.ProviderID).Error; err != nil {
		return sc.loadDefaultTextModel()
	}
	return model, provider, "", true
}

// previousShotContexts returns up to the last 10 prior shots in the same episode,
// each entry combining label, scene names from refs, and script for continuity.
func (sc *ShotController) previousShotContexts(shot models.Shot) []string {
	prev, _ := sc.neighboringShotContexts(shot, 10, 0)
	return prev
}

// neighboringShotContexts returns prior/next shot context blocks for continuity prompts.
func (sc *ShotController) neighboringShotContexts(shot models.Shot, maxPrev, maxNext int) (prev, next []string) {
	var shots []models.Shot
	sc.DB.Where("episode_id = ?", shot.EpisodeID).
		Order("sort_order asc, id asc").
		Find(&shots)

	type block struct {
		label  string
		script string
		scenes []string
	}
	format := func(b block) string {
		var sb strings.Builder
		sb.WriteString(b.label)
		if len(b.scenes) > 0 {
			sb.WriteString("｜场景：")
			sb.WriteString(strings.Join(b.scenes, "、"))
		}
		if b.script != "" {
			sb.WriteString("\n")
			sb.WriteString(b.script)
		}
		return sb.String()
	}
	toBlock := func(s models.Shot) (block, bool) {
		script := strings.TrimSpace(s.Script)
		refs := decodeShotRefs(s.RefsJSON, s.CharacterRefsJSON, s.CharacterIDsJSON, s.SceneID)
		sceneNames := make([]string, 0)
		seen := map[uint]bool{}
		for _, ref := range refs {
			if ref.Kind != "scene" || ref.ID == 0 || seen[ref.ID] {
				continue
			}
			seen[ref.ID] = true
			var res models.Resource
			if err := sc.DB.Select("id", "name").First(&res, ref.ID).Error; err != nil {
				continue
			}
			if name := strings.TrimSpace(res.Name); name != "" {
				sceneNames = append(sceneNames, name)
			}
		}
		if script == "" && len(sceneNames) == 0 {
			return block{}, false
		}
		label := strings.TrimSpace(s.Label)
		if label == "" {
			label = fmt.Sprintf("分镜%d", s.SortOrder)
		}
		return block{label: label, script: script, scenes: sceneNames}, true
	}

	idx := -1
	for i, s := range shots {
		if s.ID == shot.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, nil
	}

	priors := make([]block, 0)
	for i := 0; i < idx; i++ {
		if b, ok := toBlock(shots[i]); ok {
			priors = append(priors, b)
		}
	}
	if maxPrev > 0 && len(priors) > maxPrev {
		priors = priors[len(priors)-maxPrev:]
	}
	prev = make([]string, 0, len(priors))
	for _, p := range priors {
		prev = append(prev, format(p))
	}

	if maxNext > 0 {
		followers := make([]block, 0)
		for i := idx + 1; i < len(shots) && len(followers) < maxNext; i++ {
			if b, ok := toBlock(shots[i]); ok {
				followers = append(followers, b)
			}
		}
		next = make([]string, 0, len(followers))
		for _, n := range followers {
			next = append(next, format(n))
		}
	}
	return prev, next
}

func (sc *ShotController) polishOptimizedScript(shot models.Shot, script string) string {
	script = strings.TrimSpace(script)
	if script == "" {
		return script
	}
	refs := decodeShotRefs(shot.RefsJSON, shot.CharacterRefsJSON, shot.CharacterIDsJSON, shot.SceneID)
	info := make([]crew.ShotRefInfo, 0, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Label)
		if name == "" {
			name = fmt.Sprintf("#%d", ref.ID)
		}
		info = append(info, crew.ShotRefInfo{
			Kind:        ref.Kind,
			ResourceID:  ref.ID,
			Name:        name,
			DisplayName: name,
		})
	}
	polished := crew.PolishShotForQC(crew.ShotContext{
		ID:       shot.ID,
		Label:    shot.Label,
		Script:   script,
		Duration: shot.Duration,
		Refs:     info,
	}, nil)
	if flat := crew.FlattenPackedScript(polished); flat != "" {
		// Retime / strip meta junk, but keep overflow so packEpisodeOverflow
		// can move 【10-13秒】 to the next shot; caller Finalizes after packing.
		return crew.NormalizeShotTimeline(flat, shot.Duration)
	}
	return crew.NormalizeShotTimeline(script, shot.Duration)
}

// OptimizeScript rewrites the shot script with neighboring continuity (prev 10 + next 2).
// When the project has scene 9-grid cells for related scenes, the model also picks
// camera angles and returns them as suggestedRefs for the client to attach.
func (sc *ShotController) OptimizeScript(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	if strings.TrimSpace(shot.Script) == "" {
		fail(c, 400, "请先填写当前分镜文案")
		return
	}
	model, provider, msg, okModel := sc.loadDefaultTextModel()
	if !okModel {
		fail(c, 503, msg)
		return
	}

	var episode models.Episode
	if err := sc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}
	var project models.Project
	_ = sc.DB.Select("style").First(&project, episode.ProjectID).Error

	prev, next := sc.neighboringShotContexts(shot, 10, 2)
	prevRefs := sc.previousShotRefsSummary(shot, 10)
	currentRefs := sc.currentShotRefsSummary(episode.ProjectID, shot)
	angles := sc.collectSceneAngleCandidates(episode.ProjectID, shot)

	result, err := sc.Ark.OptimizeShotScript(
		provider,
		model,
		shot.Script,
		prev,
		next,
		project.Style,
		prevRefs,
		currentRefs,
		angles,
		shot.Duration,
	)
	if err != nil {
		fail(c, 502, "优化分镜文案失败："+err.Error())
		return
	}

	result.Script = sc.polishOptimizedScript(shot, result.Script)

	if err := sc.DB.Model(&shot).Update("script", strings.TrimSpace(result.Script)).Error; err == nil {
		sc.packEpisodeOverflow(shot.EpisodeID)
		_ = sc.DB.First(&shot, shot.ID)
		final := crew.FinalizeShotScript(shot.Script, shot.Duration)
		if final != shot.Script {
			_ = sc.DB.Model(&shot).Update("script", final).Error
			shot.Script = final
		}
		result.Script = shot.Script
	}

	suggested := make([]gin.H, 0, len(result.Angles))
	for _, a := range result.Angles {
		suggested = append(suggested, gin.H{
			"id":      a.ID,
			"label":   a.Label,
			"beats":   a.Beats,
			"kind":    "scene",
			"variant": "original",
		})
	}
	c.JSON(200, gin.H{
		"script":        result.Script,
		"shotId":        shot.ID,
		"shotLabel":     shot.Label,
		"suggestedRefs": suggested,
	})
}

// previousShotRefsSummary lists refs used by the previous N shots (for angle continuity).
func (sc *ShotController) previousShotRefsSummary(shot models.Shot, maxPrev int) string {
	var shots []models.Shot
	sc.DB.Where("episode_id = ?", shot.EpisodeID).
		Order("sort_order asc, id asc").
		Find(&shots)
	idx := -1
	for i, s := range shots {
		if s.ID == shot.ID {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return ""
	}
	start := 0
	if maxPrev > 0 && idx > maxPrev {
		start = idx - maxPrev
	}
	var b strings.Builder
	for i := start; i < idx; i++ {
		s := shots[i]
		refs := decodeShotRefs(s.RefsJSON, s.CharacterRefsJSON, s.CharacterIDsJSON, s.SceneID)
		if len(refs) == 0 {
			continue
		}
		label := strings.TrimSpace(s.Label)
		if label == "" {
			label = fmt.Sprintf("分镜%d", s.SortOrder)
		}
		parts := make([]string, 0, len(refs))
		seen := map[uint]bool{}
		for _, ref := range refs {
			if ref.ID == 0 || seen[ref.ID] {
				continue
			}
			seen[ref.ID] = true
			name := strings.TrimSpace(ref.Label)
			var res models.Resource
			if err := sc.DB.Select("id", "name", "gen_type", "grid_cell").First(&res, ref.ID).Error; err == nil {
				if name == "" {
					name = strings.TrimSpace(res.Name)
				}
				if res.GenType == "scene_grid_cell" && res.GridCell > 0 {
					if angle := services.SceneAngleLabel(res.GridCell); angle != "" {
						name = fmt.Sprintf("%s（机位·%s）", shortSceneCellName(res.Name), angle)
					}
				} else if res.GenType == "scene_grid" {
					name = shortSceneCellName(res.Name) + "（9宫格整图）"
				}
			}
			if name == "" {
				continue
			}
			kind := ref.Kind
			if kind == "" {
				kind = "ref"
			}
			parts = append(parts, fmt.Sprintf("%s:%s", kind, name))
		}
		if len(parts) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s → %s\n", label, strings.Join(parts, "；")))
	}
	return strings.TrimSpace(b.String())
}

func (sc *ShotController) currentShotRefsSummary(projectID uint, shot models.Shot) string {
	refs := sc.loadVideoRefs(projectID, shot)
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range refs {
		label := services.VideoRefIdentityLabel(r)
		if label == "" {
			label = strings.TrimSpace(r.Resource.Name)
		}
		kind := strings.TrimSpace(r.Kind)
		if kind == "" {
			kind = r.Resource.Type
		}
		if kind == "" {
			kind = "ref"
		}
		tag := kind
		if isPositioningShotRefName(label, r.Resource.GenType, r.Resource.Name) {
			tag = "站位示意图"
		}
		fmt.Fprintf(&b, "图%d %s（%s）\n", i+1, label, tag)
	}
	return strings.TrimSpace(b.String())
}

func isPositioningShotRefName(label, genType, name string) bool {
	if strings.EqualFold(strings.TrimSpace(genType), "positioning") ||
		strings.EqualFold(strings.TrimSpace(genType), "positioning_skeleton") {
		return true
	}
	blob := label + " " + name
	return strings.Contains(blob, "站位") || strings.Contains(blob, "火柴人") || strings.Contains(blob, "骨架")
}

func shortSceneCellName(name string) string {
	return services.SceneGridBaseName(name)
}

// collectSceneAngleCandidates finds scene_grid_cell resources related to the current
// shot (by existing scene refs / name hits in script / previous scene refs).
func (sc *ShotController) collectSceneAngleCandidates(projectID uint, shot models.Shot) []services.SceneAngleCandidate {
	sceneIDs := map[uint]bool{}
	addScene := func(id uint) {
		if id == 0 {
			return
		}
		var r models.Resource
		if err := sc.DB.Select("id", "type", "gen_type", "grid_id").First(&r, id).Error; err != nil {
			return
		}
		if r.Type != "scene" {
			return
		}
		if r.GenType == "scene_grid_cell" && r.GridID > 0 {
			// Walk up: cells belong to a grid; grid's genRefs point at the scene.
			var grid models.Resource
			if err := sc.DB.Select("id", "gen_refs_json", "name").First(&grid, r.GridID).Error; err == nil {
				for _, sid := range sceneIDsFromGenRefs(grid.GenRefsJSON) {
					sceneIDs[sid] = true
				}
			}
			return
		}
		if r.GenType == "scene_grid" {
			for _, sid := range sceneIDsFromGenRefs(r.GenRefsJSON) {
				sceneIDs[sid] = true
			}
			return
		}
		if r.GenType == "" || r.GenType == "scene" || r.GenType == "stylize" {
			sceneIDs[r.ID] = true
		}
	}

	for _, ref := range decodeShotRefs(shot.RefsJSON, shot.CharacterRefsJSON, shot.CharacterIDsJSON, shot.SceneID) {
		if ref.Kind == "scene" {
			addScene(ref.ID)
		}
	}

	// Also pull scene ids from previous 10 shots for continuity.
	var episodeShots []models.Shot
	sc.DB.Where("episode_id = ?", shot.EpisodeID).Order("sort_order asc, id asc").Find(&episodeShots)
	idx := -1
	for i, s := range episodeShots {
		if s.ID == shot.ID {
			idx = i
			break
		}
	}
	if idx > 0 {
		start := 0
		if idx > 10 {
			start = idx - 10
		}
		for i := start; i < idx; i++ {
			for _, ref := range decodeShotRefs(episodeShots[i].RefsJSON, episodeShots[i].CharacterRefsJSON, episodeShots[i].CharacterIDsJSON, episodeShots[i].SceneID) {
				if ref.Kind == "scene" {
					addScene(ref.ID)
				}
			}
		}
	}

	// Match plain scene resources whose names appear in the script.
	script := strings.TrimSpace(shot.Script)
	if script != "" {
		var scenes []models.Resource
		sc.DB.Where("project_id = ? AND type = ? AND (gen_type = '' OR gen_type = 'scene' OR gen_type IS NULL OR gen_type = 'stylize')", projectID, "scene").
			Select("id", "name").
			Limit(80).
			Find(&scenes)
		for _, s := range scenes {
			base := strings.TrimSpace(s.Name)
			if p := strings.Index(base, " · 候选"); p > 0 {
				base = strings.TrimSpace(base[:p])
			}
			if len([]rune(base)) < 2 {
				continue
			}
			if strings.Contains(script, base) {
				sceneIDs[s.ID] = true
			}
		}
	}

	if len(sceneIDs) == 0 {
		return nil
	}

	ids := make([]uint, 0, len(sceneIDs))
	for id := range sceneIDs {
		ids = append(ids, id)
	}

	// Find scene_grid resources referencing these scenes.
	var grids []models.Resource
	sc.DB.Where("project_id = ? AND gen_type = ?", projectID, "scene_grid").
		Select("id", "name", "gen_refs_json").
		Find(&grids)
	gridIDs := map[uint]string{} // gridID -> scene display name
	for _, g := range grids {
		linked := sceneIDsFromGenRefs(g.GenRefsJSON)
		hit := false
		for _, sid := range linked {
			if sceneIDs[sid] {
				hit = true
				break
			}
		}
		// Fallback: name prefix "场景名 · 9宫格"
		if !hit {
			base := shortSceneCellName(g.Name)
			for sid := range sceneIDs {
				var s models.Resource
				if err := sc.DB.Select("name").First(&s, sid).Error; err != nil {
					continue
				}
				sn := strings.TrimSpace(s.Name)
				if p := strings.Index(sn, " · 候选"); p > 0 {
					sn = strings.TrimSpace(sn[:p])
				}
				if sn != "" && (base == sn || strings.HasPrefix(base, sn) || strings.HasPrefix(sn, base)) {
					hit = true
					break
				}
			}
		}
		if !hit {
			continue
		}
		gridIDs[g.ID] = shortSceneCellName(g.Name)
	}
	if len(gridIDs) == 0 {
		return nil
	}

	gids := make([]uint, 0, len(gridIDs))
	for id := range gridIDs {
		gids = append(gids, id)
	}
	var cells []models.Resource
	sc.DB.Where("project_id = ? AND gen_type = ? AND grid_id IN ? AND image_path <> ''", projectID, "scene_grid_cell", gids).
		Order("grid_id asc, grid_cell asc").
		Find(&cells)

	out := make([]services.SceneAngleCandidate, 0, len(cells))
	seen := map[uint]bool{}
	for _, cell := range cells {
		if seen[cell.ID] || cell.GridCell < 1 {
			continue
		}
		angle := services.SceneAngleLabel(cell.GridCell)
		if angle == "" {
			// Try parse from name suffix "格3·侧面全景"
			if i := strings.LastIndex(cell.Name, "·"); i >= 0 {
				angle = strings.TrimSpace(cell.Name[i+len("·"):])
			}
		}
		if angle == "" {
			angle = fmt.Sprintf("格%d", cell.GridCell)
		}
		seen[cell.ID] = true
		out = append(out, services.SceneAngleCandidate{
			ID:        cell.ID,
			SceneName: gridIDs[cell.GridID],
			Angle:     angle,
			Cell:      cell.GridCell,
		})
		if len(out) >= 27 { // at most 3 grids × 9
			break
		}
	}
	return out
}

func sceneIDsFromGenRefs(raw string) []uint {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var refs []models.ResourceGenRef
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return nil
	}
	out := make([]uint, 0, len(refs))
	seen := map[uint]bool{}
	for _, r := range refs {
		if r.ID == 0 || seen[r.ID] {
			continue
		}
		if r.Kind != "" && r.Kind != "scene" {
			continue
		}
		seen[r.ID] = true
		out = append(out, r.ID)
	}
	return out
}

type matchShotRefsInput struct {
	Script     string                       `json:"script"`
	Candidates []services.RefMatchCandidate `json:"candidates"`
}

// MatchRefs asks a cheap text model which library resources fit the shot script.
// Candidates must be pre-filtered by the client to keep tokens low.
func (sc *ShotController) MatchRefs(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	var input matchShotRefsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, 400, "参数错误")
		return
	}
	script := strings.TrimSpace(input.Script)
	if script == "" {
		script = strings.TrimSpace(shot.Script)
	}
	if script == "" {
		fail(c, 400, "请先填写当前分镜文案")
		return
	}
	if len(input.Candidates) == 0 {
		c.JSON(200, gin.H{"refs": []any{}, "shotId": shot.ID})
		return
	}
	model, provider, msg, okModel := sc.loadLiteTextModel()
	if !okModel {
		fail(c, 503, msg)
		return
	}
	picks, err := sc.Ark.MatchShotLibraryRefs(provider, model, script, input.Candidates)
	if err != nil {
		fail(c, 502, "参考图匹配失败："+err.Error())
		return
	}
	c.JSON(200, gin.H{
		"refs":      picks,
		"shotId":    shot.ID,
		"modelId":   model.ID,
		"modelName": model.Name,
	})
}

// AnalyzePositioning uses the text model to draft an editable 站位图 prompt.
func (sc *ShotController) AnalyzePositioning(c *gin.Context) {
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	if strings.TrimSpace(shot.Script) == "" {
		fail(c, 400, "请先填写当前分镜文案")
		return
	}
	var input positioningAnalyzeInput
	_ = c.ShouldBindJSON(&input)

	model, provider, msg, okModel := sc.loadDefaultTextModel()
	if !okModel {
		fail(c, 503, msg)
		return
	}

	var episode models.Episode
	if err := sc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}
	var project models.Project
	_ = sc.DB.Select("style").First(&project, episode.ProjectID).Error

	previous, next := sc.neighboringShotContexts(shot, 12, 4)
	prompt, err := sc.Ark.AnalyzeShotPositioning(
		provider,
		model,
		shot.Script,
		previous,
		next,
		project.Style,
		input.RefLabels,
	)
	if err != nil {
		fail(c, 502, "分析站位文案失败："+err.Error())
		return
	}
	c.JSON(200, gin.H{
		"prompt":    prompt,
		"shotId":    shot.ID,
		"shotLabel": shot.Label,
	})
}

// GeneratePositioning starts an image job for the edited 站位图 prompt + refs.
func (sc *ShotController) GeneratePositioning(c *gin.Context) {
	if sc.Resource == nil {
		fail(c, 500, "服务未就绪")
		return
	}
	shot, ok := sc.find(c)
	if !ok {
		return
	}
	var input positioningGenerateInput
	if c.ShouldBindJSON(&input) != nil {
		fail(c, 400, "请求格式错误")
		return
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		fail(c, 400, "请填写站位图提示词")
		return
	}

	var episode models.Episode
	if err := sc.DB.First(&episode, shot.EpisodeID).Error; err != nil {
		fail(c, 404, "分集不存在")
		return
	}

	name := strings.TrimSpace(input.Name)
	if name == "" || name == "站位图" || name == "火柴人骨架" {
		name = strings.TrimSpace(shot.Label)
	}
	if name == "" {
		name = fmt.Sprintf("分镜%d", shot.SortOrder)
	}
	name = strings.TrimSpace(strings.TrimSuffix(name, " · 站位图"))
	name = strings.TrimSpace(strings.TrimSuffix(name, " · 火柴人骨架"))
	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	jobType := "positioning"
	if stage == "skeleton" {
		jobType = "positioning_skeleton"
		if !strings.Contains(name, "骨架") {
			name = name + " · 火柴人骨架"
		}
		input.ResourceRefs = sc.pickSkeletonSpatialRefs(shot, episode.ProjectID, input.ResourceRefs, prompt)
		input.ImageDataList = nil
	} else if !strings.Contains(name, "站位") {
		name = name + " · 站位图"
	}

	// Soft cap — image APIs may degrade with too many refs; keep aligned with frontend.
	const maxRefs = 12
	if len(input.ResourceRefs) > maxRefs {
		input.ResourceRefs = input.ResourceRefs[:maxRefs]
	}
	if len(input.ImageDataList) > maxRefs {
		input.ImageDataList = input.ImageDataList[:maxRefs]
	}
	if len(input.ResourceRefs)+len(input.ImageDataList) > maxRefs {
		// Prefer library refs first, then uploads.
		remain := maxRefs - len(input.ResourceRefs)
		if remain < 0 {
			input.ResourceRefs = input.ResourceRefs[:maxRefs]
			input.ImageDataList = nil
		} else if remain < len(input.ImageDataList) {
			input.ImageDataList = input.ImageDataList[:remain]
		}
	}

	count := input.Count
	if count < 1 || jobType == "positioning_skeleton" {
		count = 1
	}
	if count > 4 {
		count = 4
	}

	jobInput := imageGenJobInput{
		Name:          name,
		Description:   prompt,
		Count:         count,
		Quality:       input.Quality,
		Resolution:    input.Resolution,
		ModelID:       input.ModelID,
		ImageDataList: input.ImageDataList,
		ResourceRefs:  input.ResourceRefs,
		ShotID:        shot.ID,
	}
	sc.Resource.startImageGenerationJob(c, episode.ProjectID, jobType, jobInput)
}

// pickSkeletonSpatialRefs keeps at most 1 scene plate / 9-grid cell for the stick-figure pass.
// Character sheets are dropped. Prefers a cell matching 机位/景别; never the full 3×3 collage.
func (sc *ShotController) pickSkeletonSpatialRefs(
	shot models.Shot,
	projectID uint,
	input []imageGenResourceRef,
	prompt string,
) []imageGenResourceRef {
	type src struct {
		id      uint
		variant string
	}
	ordered := make([]src, 0, 12)
	seen := map[uint]bool{}
	push := func(id uint, variant string) {
		if id == 0 || seen[id] {
			return
		}
		seen[id] = true
		v := strings.TrimSpace(variant)
		if v == "" {
			v = "original"
		}
		ordered = append(ordered, src{id, v})
	}
	for _, r := range input {
		push(r.ID, r.Variant)
	}
	for _, r := range decodeResourceGenRefs(shot.PositioningRefsJSON) {
		push(r.ID, r.Variant)
	}
	for _, r := range decodeShotRefs(shot.RefsJSON, shot.CharacterRefsJSON, shot.CharacterIDsJSON, shot.SceneID) {
		if r.Kind == "scene" {
			push(r.ID, r.Variant)
		}
	}

	ids := make([]uint, 0, len(ordered))
	variantOf := map[uint]string{}
	for _, s := range ordered {
		ids = append(ids, s.id)
		variantOf[s.id] = s.variant
	}
	byID := map[uint]models.Resource{}
	if len(ids) > 0 {
		var resources []models.Resource
		sc.DB.Where("project_id = ? AND id IN ?", projectID, ids).Find(&resources)
		for _, r := range resources {
			byID[r.ID] = r
		}
	}

	cands := make([]services.SkeletonSceneCandidate, 0, len(ordered)+9)
	appendRes := func(r models.Resource) {
		if strings.TrimSpace(r.ImagePath) == "" && strings.TrimSpace(r.StylizedImagePath) == "" {
			return
		}
		if r.GenType == "scene_grid" {
			return
		}
		if r.Type != "scene" && r.GenType != "scene_grid_cell" {
			return
		}
		cands = append(cands, services.SkeletonSceneCandidate{
			ID: r.ID, Type: r.Type, GenType: r.GenType, Name: r.Name, GridCell: r.GridCell,
		})
		if _, ok := variantOf[r.ID]; !ok {
			variantOf[r.ID] = "original"
		}
	}
	for _, s := range ordered {
		if r, ok := byID[s.id]; ok {
			appendRes(r)
		}
	}
	for _, a := range sc.collectSceneAngleCandidates(projectID, shot) {
		var r models.Resource
		if err := sc.DB.Select("id", "type", "gen_type", "name", "grid_cell", "image_path", "stylized_image_path").
			First(&r, a.ID).Error; err != nil {
			continue
		}
		appendRes(r)
	}

	haystack := strings.TrimSpace(shot.Script) + "\n" + strings.TrimSpace(prompt)
	pick := services.PickSkeletonSceneRef(cands, haystack)
	if pick == nil {
		return nil
	}
	return []imageGenResourceRef{{ID: pick.ID, Variant: variantOf[pick.ID]}}
}
