package database

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"

	"gorm.io/gorm"
)

func BackfillVideoResources(db *gorm.DB, storage *services.Storage) error {
	var shots []models.Shot
	if err := db.Where("status = ? AND video_url != ?", "done", "").Find(&shots).Error; err != nil {
		return err
	}
	for _, shot := range shots {
		var count int64
		db.Model(&models.Resource{}).Where("shot_id = ? AND type = ?", shot.ID, "video").Count(&count)
		if count > 0 {
			continue
		}
		var episode models.Episode
		if err := db.First(&episode, shot.EpisodeID).Error; err != nil {
			continue
		}
		var episodeShots []models.Shot
		db.Where("episode_id = ?", episode.ID).Order("sort_order asc, id asc").Find(&episodeShots)
		shotIndex := 0
		for i, s := range episodeShots {
			if s.ID == shot.ID {
				shotIndex = i
				break
			}
		}
		desc := strings.TrimSpace(shot.Script)
		if len(desc) > 200 {
			desc = desc[:200] + "…"
		}
		duration := shot.Duration
		if duration <= 0 {
			duration = 10
		}
		resolution := shot.Resolution
		if resolution == "" {
			resolution = "720p"
		}
		shotID := shot.ID
		resource := models.Resource{
			ProjectID:   episode.ProjectID,
			Type:        "video",
			Source:      "ai",
			Name:        fmt.Sprintf("分镜%02d", shotIndex+1),
			Description: desc,
			ShotID:      &shotID,
			Duration:    duration,
			Resolution:  resolution,
		}
		if err := db.Create(&resource).Error; err != nil {
			return err
		}
		videoPath := fmt.Sprintf("data/uploads/projects/%d/videos/%d.mp4", episode.ProjectID, shot.ID)
		data, err := os.ReadFile(videoPath)
		if err == nil {
			path, saveErr := storage.SaveResourceVideoBytes(episode.ProjectID, resource.ID, data, "mp4")
			if saveErr == nil {
				resource.VideoPath = path
			}
		}
		if resource.VideoPath == "" {
			db.Delete(&resource)
			continue
		}
		if err := db.Save(&resource).Error; err != nil {
			return err
		}
	}
	return nil
}
func BackfillVideoGenMeta(db *gorm.DB) error {
	var resources []models.Resource
	if err := db.Where("type = ? AND gen_script = ?", "video", "").Find(&resources).Error; err != nil {
		return err
	}
	for _, resource := range resources {
		if resource.ShotID == nil {
			continue
		}
		var shot models.Shot
		if err := db.First(&shot, *resource.ShotID).Error; err != nil {
			continue
		}
		source := resource.Source
		if source == "" {
			source = "ai"
		}
		meta := videoGenMetaFromShot(db, shot, resource.ProjectID, source)
		script := strings.TrimSpace(meta.Script)
		if script == "" {
			script = strings.TrimSpace(resource.Description)
		}
		updates := map[string]any{
			"gen_script":        script,
			"gen_visual_style":  meta.VisualStyle,
			"gen_project_style": meta.ProjectStyle,
			"gen_model_name":    meta.ModelName,
			"gen_model_id":      meta.ModelID,
			"gen_provider_name": meta.ProviderName,
		}
		if err := db.Model(&resource).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func videoGenMetaFromShot(db *gorm.DB, shot models.Shot, projectID uint, source string) struct {
	Script, VisualStyle, ProjectStyle, ModelName, ModelID, ProviderName string
} {
	meta := struct {
		Script, VisualStyle, ProjectStyle, ModelName, ModelID, ProviderName string
	}{
		Script:      shot.Script,
		VisualStyle: shot.VisualStyle,
	}
	var project models.Project
	if err := db.First(&project, projectID).Error; err == nil {
		meta.ProjectStyle = project.Style
	}
	if source == "upload" {
		meta.ModelName = "本地上传"
		return meta
	}
	var model models.AIModel
	if shot.VideoModelID != nil {
		_ = db.First(&model, *shot.VideoModelID).Error
	} else {
		_ = db.Where("capability = ? AND enabled = ? AND is_default = ?", "video", true, true).First(&model).Error
	}
	if model.ID != 0 {
		meta.ModelName = model.Name
		meta.ModelID = model.ModelID
		var provider models.AIProvider
		if db.First(&provider, model.ProviderID).Error == nil {
			meta.ProviderName = provider.Name
		}
	}
	return meta
}

var candidateNameRE = regexp.MustCompile(`^(.+?)\s*·\s*候选(\d+)$`)

func isGridFamilyGen(genType string) bool {
	switch strings.TrimSpace(genType) {
	case "scene_grid", "scene_grid_cell", "motion_grid", "motion_grid_cell":
		return true
	default:
		return false
	}
}

func candidateGroupKey(r models.Resource) string {
	if isGridFamilyGen(r.GenType) || r.GridID != 0 {
		return ""
	}
	m := candidateNameRE.FindStringSubmatch(strings.TrimSpace(r.Name))
	if len(m) < 2 {
		return ""
	}
	return r.Type + ":" + strings.TrimSpace(m[1])
}

func extractCandidateBase(name string) (string, bool) {
	m := candidateNameRE.FindStringSubmatch(strings.TrimSpace(name))
	if len(m) < 2 {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

func resourceParentKey(r models.Resource) uint {
	if r.ParentID == nil {
		return 0
	}
	return *r.ParentID
}

func BackfillResourceCandidates(db *gorm.DB) error {
	var items []models.Resource
	if err := db.Unscoped().Find(&items).Error; err != nil {
		return err
	}
	groups := map[string][]models.Resource{}
	for _, item := range items {
		key := candidateGroupKey(item)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], item)
	}
	for _, group := range groups {
		var active []models.Resource
		for _, item := range group {
			if !item.DeletedAt.Valid {
				active = append(active, item)
			} else if item.IsGroupPrimary {
				// Soft-deleted items must not stay marked as primary.
				if err := db.Unscoped().Model(&item).Update("is_group_primary", false).Error; err != nil {
					return err
				}
			}
		}
		// Entire group is in trash — leave it there; never revive deleted_at.
		if len(active) == 0 {
			continue
		}
		keep := active[0]
		for _, item := range active {
			if item.IsGroupPrimary {
				keep = item
				break
			}
		}
		for _, item := range active {
			if item.ID == keep.ID {
				if !item.IsGroupPrimary {
					if err := db.Model(&item).Update("is_group_primary", true).Error; err != nil {
						return err
					}
				}
				continue
			}
			if item.IsGroupPrimary {
				if err := db.Model(&item).Update("is_group_primary", false).Error; err != nil {
					return err
				}
			}
			if err := db.Delete(&item).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// BackfillMergeExtractCandidates folds "Name · 候选N" library cards back into the
// extracted draft of the same name so generating 底模 does not leave two 韩铮.
func BackfillMergeExtractCandidates(db *gorm.DB) error {
	var items []models.Resource
	if err := db.Find(&items).Error; err != nil {
		return err
	}
	type groupKey struct {
		project uint
		typ     string
		parent  uint
		base    string
	}
	canonicals := map[groupKey]models.Resource{}
	candidates := map[groupKey][]models.Resource{}
	for _, item := range items {
		if item.Type != "character" && item.Type != "scene" && item.Type != "prop" {
			continue
		}
		if isGridFamilyGen(item.GenType) || item.GridID != 0 {
			continue
		}
		base, isCand := extractCandidateBase(item.Name)
		if !isCand {
			base = strings.TrimSpace(item.Name)
		}
		if base == "" {
			continue
		}
		key := groupKey{item.ProjectID, item.Type, resourceParentKey(item), strings.ToLower(base)}
		if isCand {
			candidates[key] = append(candidates[key], item)
			continue
		}
		prev, ok := canonicals[key]
		if !ok || item.ID < prev.ID {
			canonicals[key] = item
		}
	}
	for key, cands := range candidates {
		can, ok := canonicals[key]
		if !ok {
			continue
		}
		best := cands[0]
		for _, c := range cands {
			if c.IsGroupPrimary {
				best = c
				break
			}
			if strings.TrimSpace(can.ImagePath) == "" && strings.TrimSpace(c.ImagePath) != "" && strings.TrimSpace(best.ImagePath) == "" {
				best = c
			}
		}
		updates := map[string]any{"is_group_primary": true}
		if strings.TrimSpace(can.ImagePath) == "" && strings.TrimSpace(best.ImagePath) != "" {
			updates["image_path"] = best.ImagePath
			if strings.TrimSpace(best.StylizedImagePath) != "" {
				updates["stylized_image_path"] = best.StylizedImagePath
			}
			if strings.TrimSpace(can.GenPrompt) == "" && strings.TrimSpace(best.GenPrompt) != "" {
				updates["gen_prompt"] = best.GenPrompt
			}
			if strings.TrimSpace(best.GenType) != "" {
				updates["gen_type"] = best.GenType
			}
			if strings.TrimSpace(best.GenRefsJSON) != "" {
				updates["gen_refs_json"] = best.GenRefsJSON
			}
			if strings.TrimSpace(best.Source) != "" {
				updates["source"] = best.Source
			}
		}
		if err := db.Model(&can).Updates(updates).Error; err != nil {
			return err
		}
		for _, c := range cands {
			if c.IsGroupPrimary {
				if err := db.Model(&c).Update("is_group_primary", false).Error; err != nil {
					return err
				}
			}
			if err := db.Delete(&c).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// BackfillTransitionFramePaths repairs transition-frame resources whose image bytes
// were written to storage but whose image_path was never recorded (bug in the first
// version of the previous-frame extractor), which left imageUrl empty.
func BackfillTransitionFramePaths(db *gorm.DB, storage *services.Storage) error {
	var items []models.Resource
	if err := db.Where("gen_type = ? AND (image_path IS NULL OR image_path = '')", "transition_frame").Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		path := storage.ResourceImagePath(item.ProjectID, item.ID, "jpg")
		if !storage.FileExists(path) {
			continue
		}
		if err := db.Model(&item).Update("image_path", path).Error; err != nil {
			return err
		}
	}
	return nil
}

// BackfillSceneGridCellNames renames scene grid cells to the canonical short format
// 场景名·机位名. Older cells carry the full provenance in their name
// ("场景名 · 9宫格 · 候选1 · 格8·俯视近景"), which is noisy and inconsistent with
// cells split before the 格N·机位 naming convention.
func BackfillSceneGridCellNames(db *gorm.DB) error {
	var cells []models.Resource
	if err := db.Unscoped().Where("gen_type = ?", "scene_grid_cell").Find(&cells).Error; err != nil {
		return err
	}
	gridNames := map[uint]string{}
	for _, cell := range cells {
		gridName, ok := gridNames[cell.GridID]
		if !ok {
			var g models.Resource
			if err := db.Unscoped().Select("id", "name").First(&g, cell.GridID).Error; err != nil {
				gridName = ""
			} else {
				gridName = g.Name
			}
			gridNames[cell.GridID] = gridName
		}
		if gridName == "" {
			continue
		}
		name := services.SceneGridCellName(gridName, cell.GridCell)
		if name == "" || name == cell.Name {
			continue
		}
		if err := db.Model(&cell).Update("name", name).Error; err != nil {
			return err
		}
	}
	return nil
}

// BackfillTransitionFrameRefLabels removes implementation details such as
// “（衔接自245）” from shot reference labels. The UI only needs “上一镜尾帧”.
func BackfillTransitionFrameRefLabels(db *gorm.DB) error {
	var shots []models.Shot
	if err := db.Where("refs_json LIKE ?", "%上一镜尾帧%").Find(&shots).Error; err != nil {
		return err
	}
	for _, shot := range shots {
		var refs []models.ShotRef
		if err := json.Unmarshal([]byte(shot.RefsJSON), &refs); err != nil {
			continue
		}
		changed := false
		for i := range refs {
			if strings.HasPrefix(strings.TrimSpace(refs[i].Label), "上一镜尾帧") && refs[i].Label != "上一镜尾帧" {
				refs[i].Label = "上一镜尾帧"
				changed = true
			}
		}
		if !changed {
			continue
		}
		raw, err := json.Marshal(refs)
		if err != nil {
			return err
		}
		if err := db.Model(&shot).Update("refs_json", string(raw)).Error; err != nil {
			return err
		}
	}
	return nil
}

// BackfillShotScriptArtifacts removes leaked `angles` JSON/pseudo-JSON tails from
// scripts saved by older optimizer parsing. It is safe to rerun on every startup.
func BackfillShotScriptArtifacts(db *gorm.DB) error {
	var shots []models.Shot
	if err := db.Where("script LIKE ? OR script LIKE ?", `%"angles"%`, "%'angles'%").Find(&shots).Error; err != nil {
		return err
	}
	for _, shot := range shots {
		clean := services.CleanOptimizedShotScript(shot.Script)
		if clean == "" || clean == shot.Script {
			continue
		}
		if err := db.Model(&shot).Update("script", clean).Error; err != nil {
			return err
		}
	}
	return nil
}

// BackfillShotScriptRepetition removes global render/style boilerplate repeated
// inside every timing beat of existing optimized scripts.
func BackfillShotScriptRepetition(db *gorm.DB) error {
	var shots []models.Shot
	if err := db.Where("script != ?", "").Find(&shots).Error; err != nil {
		return err
	}
	episodeProject := map[uint]uint{}
	projectStyle := map[uint]string{}
	for _, shot := range shots {
		projectID, ok := episodeProject[shot.EpisodeID]
		if !ok {
			var episode models.Episode
			if err := db.Select("id", "project_id").First(&episode, shot.EpisodeID).Error; err != nil {
				continue
			}
			projectID = episode.ProjectID
			episodeProject[shot.EpisodeID] = projectID
		}
		style, ok := projectStyle[projectID]
		if !ok {
			var project models.Project
			if err := db.Select("id", "style").First(&project, projectID).Error; err == nil {
				style = project.Style
			}
			projectStyle[projectID] = style
		}
		clean := services.CleanOptimizeShotRepetition(shot.Script, style)
		if clean == "" || clean == shot.Script {
			continue
		}
		if err := db.Model(&shot).Update("script", clean).Error; err != nil {
			return err
		}
	}
	return nil
}

func isReverseLikeGen(gen string) bool {
	switch strings.TrimSpace(gen) {
	case "scene_reverse", "scene_reverse_skeleton":
		return true
	default:
		return false
	}
}

func isSceneDerivedPlateGen(gen string) bool {
	switch strings.TrimSpace(gen) {
	case "scene_grid", "scene_grid_cell", "scene_reverse", "scene_reverse_skeleton", "scene_panorama", "positioning", "positioning_skeleton":
		return true
	default:
		return false
	}
}

func sceneReversePlateName(name string) string {
	n := strings.TrimSpace(name)
	if base, ok := extractCandidateBase(n); ok {
		n = base
	}
	for _, suf := range []string{" · 反打骨架", " · 反打"} {
		n = strings.TrimSpace(strings.TrimSuffix(n, suf))
	}
	return n
}

func walkResourceToRoot(db *gorm.DB, projectID, id uint) models.Resource {
	var cur models.Resource
	seen := map[uint]bool{}
	next := id
	for next > 0 && !seen[next] {
		seen[next] = true
		if err := db.Select("id, project_id, parent_id, type, gen_type, name, grid_id").
			First(&cur, "id = ? AND project_id = ?", next, projectID).Error; err != nil {
			return models.Resource{}
		}
		if pid := resourceParentKey(cur); pid > 0 {
			next = pid
			continue
		}
		if strings.TrimSpace(cur.GenType) == "scene_grid_cell" && cur.GridID > 0 {
			next = cur.GridID
			continue
		}
		return cur
	}
	return cur
}

func usableSceneReverseParent(db *gorm.DB, projectID, id uint) uint {
	root := walkResourceToRoot(db, projectID, id)
	if root.ID == 0 || root.Type != "scene" || isSceneDerivedPlateGen(root.GenType) {
		return 0
	}
	return root.ID
}

func sceneReverseParentFromRefs(db *gorm.DB, projectID uint, raw string) uint {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return 0
	}
	var refs []models.ResourceGenRef
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return 0
	}
	for _, ref := range refs {
		if id := usableSceneReverseParent(db, projectID, ref.ID); id > 0 {
			return id
		}
	}
	return 0
}

func sceneReverseParentFromName(db *gorm.DB, projectID uint, selfID uint, name string) uint {
	plate := sceneReversePlateName(name)
	if plate == "" {
		return 0
	}
	var matches []models.Resource
	if err := db.Select("id, type, gen_type, parent_id, name").
		Where("project_id = ? AND id != ? AND type = ? AND name = ?", projectID, selfID, "scene", plate).
		Order("id asc").
		Find(&matches).Error; err != nil {
		return 0
	}
	for _, m := range matches {
		if isReverseLikeGen(m.GenType) {
			continue
		}
		if id := usableSceneReverseParent(db, projectID, m.ID); id > 0 {
			return id
		}
	}
	return 0
}

// BackfillSceneReverseParents hangs reverse plates and skeletons under the original
// scene so they show up in 衍生图 instead of the top-level library.
func BackfillSceneReverseParents(db *gorm.DB) error {
	var items []models.Resource
	if err := db.Select("id, project_id, parent_id, type, gen_type, name, gen_refs_json").
		Where("gen_type IN ?", []string{"scene_reverse", "scene_reverse_skeleton"}).
		Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if resourceParentKey(item) > 0 {
			continue
		}
		parentID := sceneReverseParentFromRefs(db, item.ProjectID, item.GenRefsJSON)
		if parentID == 0 || parentID == item.ID {
			parentID = sceneReverseParentFromName(db, item.ProjectID, item.ID, item.Name)
		}
		if parentID == 0 || parentID == item.ID {
			continue
		}
		if err := db.Model(&item).Update("parent_id", parentID).Error; err != nil {
			return err
		}
	}
	return nil
}

func sceneGridPlateName(name string) string {
	return strings.TrimSpace(services.SceneGridBaseName(name))
}

func sceneGridParentFromName(db *gorm.DB, projectID, selfID uint, name string) uint {
	plate := sceneGridPlateName(name)
	if plate == "" {
		return 0
	}
	var matches []models.Resource
	if err := db.Select("id, type, gen_type, parent_id, name").
		Where("project_id = ? AND id != ? AND type = ? AND name = ?", projectID, selfID, "scene", plate).
		Order("id asc").
		Find(&matches).Error; err != nil {
		return 0
	}
	for _, m := range matches {
		if isSceneDerivedPlateGen(m.GenType) {
			continue
		}
		if id := usableSceneReverseParent(db, projectID, m.ID); id > 0 {
			return id
		}
	}
	return 0
}

// BackfillSceneGridParents hangs 9-grid composites under the original scene so
// they show up in 衍生图 instead of the top-level library.
func BackfillSceneGridParents(db *gorm.DB) error {
	var items []models.Resource
	if err := db.Select("id, project_id, parent_id, type, gen_type, name, gen_refs_json").
		Where("gen_type = ?", "scene_grid").
		Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if resourceParentKey(item) > 0 {
			continue
		}
		parentID := sceneReverseParentFromRefs(db, item.ProjectID, item.GenRefsJSON)
		if parentID == 0 || parentID == item.ID {
			parentID = sceneGridParentFromName(db, item.ProjectID, item.ID, item.Name)
		}
		if parentID == 0 || parentID == item.ID {
			continue
		}
		if err := db.Model(&item).Update("parent_id", parentID).Error; err != nil {
			return err
		}
	}
	return nil
}

// BackfillScenePanoramaParents hangs panoramas under the original scene plate.
func BackfillScenePanoramaParents(db *gorm.DB) error {
	var items []models.Resource
	if err := db.Select("id, project_id, parent_id, type, gen_type, name, gen_refs_json").
		Where("gen_type = ?", "scene_panorama").
		Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if resourceParentKey(item) > 0 {
			continue
		}
		parentID := sceneReverseParentFromRefs(db, item.ProjectID, item.GenRefsJSON)
		if parentID == 0 || parentID == item.ID {
			plate := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(item.Name, " · 全景图"), "·全景图"))
			parentID = sceneGridParentFromName(db, item.ProjectID, item.ID, plate)
		}
		if parentID == 0 || parentID == item.ID {
			continue
		}
		if err := db.Model(&item).Update("parent_id", parentID).Error; err != nil {
			return err
		}
	}
	return nil
}

// BackfillScenePanoramaViewParents hangs perspective crops under the plate (or
// the panorama) so they only appear in 衍生图, not the top-level library.
func BackfillScenePanoramaViewParents(db *gorm.DB) error {
	var items []models.Resource
	if err := db.Select("id, project_id, parent_id, grid_id, gen_type").
		Where("gen_type = ?", "scene_panorama_view").
		Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if resourceParentKey(item) > 0 {
			continue
		}
		if item.GridID == 0 {
			continue
		}
		var pano models.Resource
		if err := db.Select("id, parent_id").First(&pano, item.GridID).Error; err != nil {
			continue
		}
		parentID := pano.ID
		if pano.ParentID != nil && *pano.ParentID > 0 {
			parentID = *pano.ParentID
		}
		if parentID == 0 || parentID == item.ID {
			continue
		}
		if err := db.Model(&item).Update("parent_id", parentID).Error; err != nil {
			return err
		}
	}
	return nil
}
