package controllers

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"novaly/backend/models"
	"novaly/backend/services"
)

var (
	ttsTimeMarkerRE   = regexp.MustCompile(`(?:[【\[]|◆)\s*(\d+(?:\.\d+)?)\s*[-~—至]\s*(\d+(?:\.\d+)?)\s*秒?\s*(?:[】\]]|\|)`)
	ttsDialogueRE     = regexp.MustCompile(`(?m)(?:^|[\n；;])\s*(?:(?:台词|对白)\s*[：:]\s*)?([\p{Han}A-Za-z0-9_·]{1,16})(?:\s*[（(]([^）)\n]{1,80})[）)])?\s*(?:说道?|喊道?|问道?|低语道?)?\s*[：:]\s*[“"「『]?([^；;\n]+)`)
	ttsSaysRE         = regexp.MustCompile(`(?m)(?:^|[\n；;])\s*([\p{Han}A-Za-z0-9_·]{1,16})(?:\s*[（(]([^）)\n]{1,80})[）)])?\s*(?:说道?|喊道?|问道?|低语道?)\s*[：:]?\s*[“"「『]([^”"」』\n]+)`)
	ttsUnattributedRE = regexp.MustCompile(`(?m)^\s*(台词|对白|内心戏)\s*[：:]\s*[（(]([^）)\n]*)[）)]\s*[：:]?\s*(.+?)\s*$`)
	ttsRoleLineRE     = regexp.MustCompile(`(?m)^\s*角色\s*[：:]\s*(.+?)\s*$`)
	ttsRolePartRE     = regexp.MustCompile(`^\s*([^（(，,、。]+?)\s*(?:[（(]([^）)]*)[）)])?\s*$`)
	ttsClosingRE      = regexp.MustCompile(`[”"」』]\s*$`)
)

var nonSpeakerLabels = map[string]bool{
	"镜头": true, "画面": true, "场景": true, "动作": true, "音效": true,
	"音乐": true, "转场": true, "运镜": true, "构图": true, "光线": true,
	"光影": true, "氛围": true, "色调": true, "环境": true, "字幕": true,
	"提示": true, "说明": true, "角色": true, "人物": true, "台词": true,
	"对白": true, "内心戏": true, "角色标签映射": true, "衔接前置指令": true,
}

type extractTTSProjectReq struct {
	TTSProjectID string `json:"ttsProjectId"`
	UseAI        bool   `json:"useAI"`
	ShotIDs      []uint `json:"shotIds"`
}

type ttsExtractShotOption struct {
	ID         uint   `json:"id"`
	GlobalShot int    `json:"globalShot"`
	Label      string `json:"label"`
	Script     string `json:"script"`
}

type shotScriptSegment struct {
	Time string
	Text string
}

type scriptRole struct {
	Name        string
	Description string
}

// ListExtractShots returns every shot in a storyboard project with the script
// text used for AI dialogue extraction (shot.script, else active/latest video genScript).
func (t *TTSController) ListExtractShots(c *gin.Context) {
	if t.DB == nil {
		fail(c, http.StatusServiceUnavailable, "数据库未初始化")
		return
	}
	projectID := parseID(c.Param("projectId"))
	if projectID == 0 {
		fail(c, http.StatusBadRequest, "无效项目")
		return
	}

	var source models.Project
	err := t.DB.Preload("Episodes", func(db *gorm.DB) *gorm.DB {
		return db.Order("number asc, id asc")
	}).Preload("Episodes.Shots", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order asc, id asc")
	}).First(&source, projectID).Error
	if err != nil {
		fail(c, http.StatusNotFound, "项目不存在")
		return
	}

	scriptByShot := loadShotDialogueScripts(t.DB, source)
	out := make([]ttsExtractShotOption, 0)
	globalShot := 0
	for _, episode := range source.Episodes {
		for _, shot := range episode.Shots {
			globalShot++
			label := strings.TrimSpace(shot.Label)
			if label == "" {
				label = fmt.Sprintf("分镜 %d", globalShot)
			}
			out = append(out, ttsExtractShotOption{
				ID:         shot.ID,
				GlobalShot: globalShot,
				Label:      label,
				Script:     scriptByShot[shot.ID],
			})
		}
	}
	c.JSON(http.StatusOK, out)
}

// ExtractProject builds a persistent TTS project directly from storyboard scripts.
// Re-extracting into the same TTS project keeps character voice assignments and
// already generated audio when the source line text has not changed.
func (t *TTSController) ExtractProject(c *gin.Context) {
	if t.DB == nil {
		fail(c, http.StatusServiceUnavailable, "数据库未初始化")
		return
	}
	projectID := parseID(c.Param("projectId"))
	if projectID == 0 {
		fail(c, http.StatusBadRequest, "无效项目")
		return
	}

	var source models.Project
	err := t.DB.Preload("Episodes", func(db *gorm.DB) *gorm.DB {
		return db.Order("number asc, id asc")
	}).Preload("Episodes.Shots", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order asc, id asc")
	}).First(&source, projectID).Error
	if err != nil {
		fail(c, http.StatusNotFound, "项目不存在")
		return
	}

	var req extractTTSProjectReq
	_ = c.ShouldBindJSON(&req)
	var old *ttsProject
	if id := strings.TrimSpace(req.TTSProjectID); id != "" {
		if loaded, loadErr := t.loadProject(id); loadErr == nil && loaded.SourceProjectID == projectID {
			old = loaded
		}
	}

	var extracted *ttsProject
	if req.UseAI {
		if len(req.ShotIDs) == 0 {
			fail(c, http.StatusBadRequest, "请选择要用 AI 提取的分镜")
			return
		}
		if len(req.ShotIDs) > 100 {
			fail(c, http.StatusBadRequest, "单次最多选择 100 个分镜")
			return
		}
		extracted, err = t.extractStoryboardTTSProjectAI(source, req.ShotIDs)
		if err != nil {
			fail(c, http.StatusBadGateway, err.Error())
			return
		}
	} else {
		extracted = extractStoryboardTTSProject(source)
	}
	if old != nil {
		if req.UseAI {
			keepUnselectedTTSLines(extracted, old, req.ShotIDs)
		}
		extracted.ID = old.ID
		extracted.Name = old.Name
		mergeExtractedTTSProject(extracted, old)
	} else {
		extracted.ID = uuid.NewString()
	}
	if err := t.saveProject(extracted); err != nil {
		fail(c, http.StatusInternalServerError, "保存分镜配音项目失败："+err.Error())
		return
	}
	c.JSON(http.StatusOK, extracted)
}

func extractStoryboardTTSProject(source models.Project) *ttsProject {
	out := &ttsProject{
		SourceProjectID: source.ID,
		ExtractionMode:  "rules",
		Name:            strings.TrimSpace(source.Title) + " · 分镜配音",
		Characters:      []ttsCharacter{},
		Lines:           []ttsLine{},
	}
	charNames := map[string]bool{}
	globalShot := 0
	for _, episode := range source.Episodes {
		for _, shot := range episode.Shots {
			globalShot++
			lineNumber := 0
			script := strings.TrimSpace(shot.Script)
			roles := parseScriptRoles(script)
			for _, segment := range splitShotScriptSegments(script) {
				for _, parsed := range parseDialogueSegment(segment.Text, roles) {
					lineNumber++
					tone, speechRate, pitch, loudness := inferDialoguePerformance(parsed.Tone, parsed.Text, segment.Text)
					speaker := strings.TrimSpace(parsed.Speaker)
					charNames[speaker] = true
					out.Lines = append(out.Lines, ttsLine{
						ID:              fmt.Sprintf("shot_%d_line_%02d", shot.ID, lineNumber),
						SourceShotID:    shot.ID,
						Episode:         episode.Number,
						Shot:            shot.SortOrder,
						GlobalShot:      globalShot,
						Time:            segment.Time,
						Type:            parsed.Type,
						Speaker:         speaker,
						Text:            parsed.Text,
						Emotion:         tone,
						EmotionStrength: 4,
						EnableEmotion:   boolPtr(true),
						Pitch:           pitch,
						SpeechRate:      speechRate,
						LoudnessRate:    loudness,
						SpeedRatio:      1,
						NeedsReview:     parsed.NeedsReview || strings.TrimSpace(parsed.Tone) == "",
						Filename:        buildLineAudioFilename(globalShot, speaker, parsed.Text),
					})
				}
			}
		}
	}
	names := make([]string, 0, len(charNames))
	for name := range charNames {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		out.Characters = append(out.Characters, ttsCharacter{
			ID:           "char_" + strconv.Itoa(len(out.Characters)+1),
			Name:         name,
			DefaultSpeed: 1,
		})
	}
	return out
}

func (t *TTSController) extractStoryboardTTSProjectAI(source models.Project, shotIDs []uint) (*ttsProject, error) {
	if t.Ark == nil {
		return nil, fmt.Errorf("AI 服务未初始化")
	}
	var model models.AIModel
	err := t.DB.Where("capability = ? AND enabled = ? AND is_default = ?", "text", true, true).First(&model).Error
	if err != nil {
		err = t.DB.Where("capability = ? AND enabled = ?", "text", true).Order("id asc").First(&model).Error
	}
	if err != nil {
		return nil, fmt.Errorf("请先在设置中心启用一个文本大模型")
	}
	var provider models.AIProvider
	if err := t.DB.Where("id = ? AND enabled = ?", model.ProviderID, true).First(&provider).Error; err != nil {
		return nil, fmt.Errorf("文本模型服务商不可用")
	}

	wanted := map[uint]bool{}
	for _, id := range shotIDs {
		wanted[id] = true
	}
	type workItem struct {
		shot         models.Shot
		episodeNum   int
		globalShot   int
		script       string
	}
	scriptByShot := loadShotDialogueScripts(t.DB, source)
	items := make([]workItem, 0, len(shotIDs))
	globalShot := 0
	for _, episode := range source.Episodes {
		for _, shot := range episode.Shots {
			globalShot++
			if !wanted[shot.ID] {
				continue
			}
			script := scriptByShot[shot.ID]
			if script == "" {
				return nil, fmt.Errorf("分镜 %d 没有可提取的文案", globalShot)
			}
			items = append(items, workItem{
				shot:       shot,
				episodeNum: episode.Number,
				globalShot: globalShot,
				script:     truncateDialogueScript(script),
			})
		}
	}
	if len(items) != len(wanted) {
		return nil, fmt.Errorf("部分分镜不存在或不属于当前项目")
	}

	type workResult struct {
		item     workItem
		analysis services.StoryboardDialogueAnalysis
		err      error
	}
	results := make([]workResult, len(items))
	workers := dialogueExtractConcurrency
	if workers > len(items) {
		workers = len(items)
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int, len(items))
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				item := items[idx]
				analysis, analyzeErr := t.Ark.AnalyzeStoryboardDialogue(provider, model, item.script)
				results[idx] = workResult{item: item, analysis: analysis, err: analyzeErr}
			}
		}()
	}
	for i := range items {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	out := &ttsProject{
		SourceProjectID: source.ID,
		ExtractionMode:  "ai",
		Name:            strings.TrimSpace(source.Title) + " · 分镜配音",
		Characters:      []ttsCharacter{},
		Lines:           []ttsLine{},
	}
	charHints := map[string]string{}
	var failures []string
	for _, result := range results {
		item := result.item
		if result.err != nil {
			msg := fmt.Sprintf("分镜 %d：%v", item.globalShot, result.err)
			failures = append(failures, msg)
			log.Printf("tts dialogue extract skipped: %s", msg)
			continue
		}
		analysis := result.analysis
		for _, character := range analysis.Characters {
			name := strings.TrimSpace(character.Name)
			if name != "" && charHints[name] == "" {
				charHints[name] = strings.TrimSpace(character.VoiceHint)
			}
		}
		for i, line := range analysis.Lines {
			text := cleanDialogueText(line.Text)
			if text == "" {
				continue
			}
			speaker := strings.TrimSpace(line.Speaker)
			needsReview := line.NeedsReview
			if speaker == "" {
				speaker = "待标注"
				needsReview = true
			}
			charHints[speaker] = firstNonBlank(charHints[speaker], "待确认角色音色")
			lineType := normalizeDialogueType(line.Type, speaker)
			tone := strings.TrimSpace(line.Tone)
			emotion := strings.TrimSpace(line.Emotion)
			if emotion == "" {
				emotion, _, _, _ = inferDialoguePerformance(tone, text, item.script)
			}
			out.Lines = append(out.Lines, ttsLine{
				ID:              fmt.Sprintf("shot_%d_line_%02d", item.shot.ID, i+1),
				SourceShotID:    item.shot.ID,
				Episode:         item.episodeNum,
				Shot:            item.shot.SortOrder,
				GlobalShot:      item.globalShot,
				Time:            strings.TrimSpace(line.Time),
				Type:            lineType,
				Speaker:         speaker,
				Text:            text,
				Tone:            tone,
				Emotion:         emotion,
				EmotionStrength: 4,
				EmotionHint:     normalizeEmotionHint(line.EmotionHint),
				EnableEmotion:   boolPtr(true),
				Pitch:           clampTTSInt(line.Pitch, -12, 12),
				SpeechRate:      clampTTSInt(line.SpeechRate, -50, 100),
				LoudnessRate:    clampTTSInt(line.LoudnessRate, -50, 100),
				SpeedRatio:      1,
				NeedsReview:     needsReview,
				Filename:        buildLineAudioFilename(item.globalShot, speaker, text),
			})
		}
	}
	if len(failures) == len(items) {
		return nil, fmt.Errorf("全部 %d 个分镜 AI 提取失败：%s", len(items), failures[0])
	}
	if len(failures) > 0 {
		log.Printf("tts dialogue extract partial success: ok=%d failed=%d first=%s",
			len(items)-len(failures), len(failures), failures[0])
	}
	names := make([]string, 0, len(charHints))
	for name := range charHints {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		out.Characters = append(out.Characters, ttsCharacter{
			ID: "char_" + strconv.Itoa(i+1), Name: name,
			VoiceHint: charHints[name], DefaultSpeed: 1,
		})
	}
	return out, nil
}

const dialogueExtractConcurrency = 3
const dialogueScriptMaxRunes = 4500

func truncateDialogueScript(script string) string {
	script = strings.TrimSpace(script)
	if utf8.RuneCountInString(script) <= dialogueScriptMaxRunes {
		return script
	}
	runes := []rune(script)
	return string(runes[:dialogueScriptMaxRunes]) + "\n…(文案过长已截断)"
}

// loadShotDialogueScripts prefers shot.script; falls back to the active or latest
// video genScript/description for each shot in a single pass.
func loadShotDialogueScripts(db *gorm.DB, source models.Project) map[uint]string {
	out := map[uint]string{}
	activeIDs := make([]uint, 0)
	shotIDs := make([]uint, 0)
	for _, episode := range source.Episodes {
		for _, shot := range episode.Shots {
			shotIDs = append(shotIDs, shot.ID)
			if s := strings.TrimSpace(shot.Script); s != "" {
				out[shot.ID] = s
			}
			if shot.ActiveVideoResourceID != nil && *shot.ActiveVideoResourceID != 0 {
				activeIDs = append(activeIDs, *shot.ActiveVideoResourceID)
			}
		}
	}
	if db == nil || len(shotIDs) == 0 {
		return out
	}

	if len(activeIDs) > 0 {
		var activeVideos []models.Resource
		_ = db.Where("id IN ?", activeIDs).Find(&activeVideos).Error
		for _, video := range activeVideos {
			if video.ShotID == nil {
				continue
			}
			if out[*video.ShotID] != "" {
				continue
			}
			if s := strings.TrimSpace(video.GenScript); s != "" {
				out[*video.ShotID] = s
				continue
			}
			if s := strings.TrimSpace(video.Description); s != "" {
				out[*video.ShotID] = s
			}
		}
	}

	missing := make([]uint, 0)
	for _, id := range shotIDs {
		if out[id] == "" {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return out
	}

	var videos []models.Resource
	_ = db.Where("type = ? AND shot_id IN ?", "video", missing).
		Order("id desc").
		Find(&videos).Error
	for _, video := range videos {
		if video.ShotID == nil || out[*video.ShotID] != "" {
			continue
		}
		if s := strings.TrimSpace(video.GenScript); s != "" {
			out[*video.ShotID] = s
			continue
		}
		if s := strings.TrimSpace(video.Description); s != "" {
			out[*video.ShotID] = s
		}
	}
	return out
}

func keepUnselectedTTSLines(current, old *ttsProject, selected []uint) {
	selectedSet := map[uint]bool{}
	for _, id := range selected {
		selectedSet[id] = true
	}
	for _, line := range old.Lines {
		if !selectedSet[line.SourceShotID] {
			current.Lines = append(current.Lines, line)
		}
	}
	sort.SliceStable(current.Lines, func(i, j int) bool {
		if current.Lines[i].GlobalShot != current.Lines[j].GlobalShot {
			return current.Lines[i].GlobalShot < current.Lines[j].GlobalShot
		}
		if current.Lines[i].Time != current.Lines[j].Time {
			return current.Lines[i].Time < current.Lines[j].Time
		}
		return current.Lines[i].ID < current.Lines[j].ID
	})

	present := map[string]bool{}
	for _, character := range current.Characters {
		present[character.Name] = true
	}
	used := map[string]bool{}
	for _, line := range current.Lines {
		used[line.Speaker] = true
	}
	for _, character := range old.Characters {
		if used[character.Name] && !present[character.Name] {
			current.Characters = append(current.Characters, character)
			present[character.Name] = true
		}
	}
}

func normalizeDialogueType(value, speaker string) string {
	switch strings.TrimSpace(value) {
	case "台词", "内心戏", "旁白", "画外音", "系统":
		return strings.TrimSpace(value)
	default:
		return dialogueType(speaker)
	}
}

func normalizeEmotionHint(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "neutral", "angry", "fearful", "sad", "happy", "cold", "narration":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "neutral"
	}
}

func clampTTSInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func mergeExtractedTTSProject(current, old *ttsProject) {
	oldChars := map[string]ttsCharacter{}
	for _, character := range old.Characters {
		oldChars[character.Name] = character
	}
	for i := range current.Characters {
		if previous, ok := oldChars[current.Characters[i].Name]; ok {
			current.Characters[i].ID = previous.ID
			current.Characters[i].VoiceHint = previous.VoiceHint
			current.Characters[i].VoiceType = previous.VoiceType
			current.Characters[i].DefaultSpeed = previous.DefaultSpeed
		}
	}

	oldLines := map[string]ttsLine{}
	for _, line := range old.Lines {
		oldLines[line.ID] = line
	}
	for i := range current.Lines {
		previous, ok := oldLines[current.Lines[i].ID]
		if !ok {
			continue
		}
		current.Lines[i].VoiceType = previous.VoiceType
		if previous.Text == current.Lines[i].Text {
			current.Lines[i].Filename = previous.Filename
			current.Lines[i].AudioURL = previous.AudioURL
			current.Lines[i].AudioReady = previous.AudioReady
		}
	}
}

func splitShotScriptSegments(script string) []shotScriptSegment {
	script = strings.ReplaceAll(script, "\r\n", "\n")
	matches := ttsTimeMarkerRE.FindAllStringSubmatchIndex(script, -1)
	if len(matches) == 0 {
		return []shotScriptSegment{{Time: "", Text: script}}
	}
	segments := make([]shotScriptSegment, 0, len(matches))
	for i, match := range matches {
		end := len(script)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		timeLabel := script[match[2]:match[3]] + "-" + script[match[4]:match[5]] + "秒"
		segments = append(segments, shotScriptSegment{
			Time: timeLabel,
			Text: strings.TrimSpace(script[match[1]:end]),
		})
	}
	return segments
}

type parsedDialogue struct {
	Speaker     string
	Tone        string
	Text        string
	Type        string
	NeedsReview bool
}

func parseScriptRoles(script string) []scriptRole {
	match := ttsRoleLineRE.FindStringSubmatch(script)
	if len(match) < 2 {
		return nil
	}
	parts := regexp.MustCompile(`[、,，]`).Split(strings.TrimSpace(match[1]), -1)
	roles := make([]scriptRole, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimRight(part, "。"))
		parsed := ttsRolePartRE.FindStringSubmatch(part)
		if len(parsed) < 2 || strings.TrimSpace(parsed[1]) == "" {
			continue
		}
		description := ""
		if len(parsed) > 2 {
			description = strings.TrimSpace(parsed[2])
		}
		roles = append(roles, scriptRole{Name: strings.TrimSpace(parsed[1]), Description: description})
	}
	return roles
}

func parseDialogueSegment(segment string, roles []scriptRole) []parsedDialogue {
	out := []parsedDialogue{}
	seen := map[string]bool{}
	appendMatches := func(re *regexp.Regexp) {
		for _, match := range re.FindAllStringSubmatch(segment, -1) {
			speaker := strings.TrimSpace(match[1])
			if speaker == "" || nonSpeakerLabels[speaker] || utf8.RuneCountInString(speaker) > 16 {
				continue
			}
			tone := strings.TrimSpace(match[2])
			text := cleanDialogueText(match[3])
			if text == "" {
				continue
			}
			key := speaker + "\x00" + text
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, parsedDialogue{Speaker: speaker, Tone: tone, Text: text, Type: dialogueType(speaker)})
		}
	}
	appendMatches(ttsDialogueRE)
	appendMatches(ttsSaysRE)

	for _, index := range ttsUnattributedRE.FindAllStringSubmatchIndex(segment, -1) {
		lineType := segment[index[2]:index[3]]
		tone := strings.TrimSpace(segment[index[4]:index[5]])
		text := cleanDialogueText(segment[index[6]:index[7]])
		if text == "" || text == "无" || tone == "无" {
			continue
		}
		speaker, needsReview := inferUnattributedSpeaker(lineType, tone, segment[:index[0]], roles)
		key := speaker + "\x00" + text
		if seen[key] {
			continue
		}
		seen[key] = true
		needsType := lineType
		if needsType == "对白" {
			needsType = "台词"
		}
		out = append(out, parsedDialogue{
			Speaker: speaker, Tone: tone, Text: text, Type: needsType, NeedsReview: needsReview,
		})
	}
	return out
}

func inferUnattributedSpeaker(lineType, tone, before string, roles []scriptRole) (string, bool) {
	if ttsContainsAny(tone, "系统音", "系统电子音", "系统冷酷音", "系统播报") {
		return "系统", false
	}
	// The closest role named in the current timed shot is usually the speaker.
	bestName, bestIndex := "", -1
	for _, role := range roles {
		if idx := strings.LastIndex(before, role.Name); idx > bestIndex {
			bestName, bestIndex = role.Name, idx
		}
	}
	if bestName != "" {
		return bestName, false
	}
	// Match explicit emotional descriptions from the cast line.
	for _, role := range roles {
		for _, word := range strings.FieldsFunc(role.Description, func(r rune) bool {
			return r == '，' || r == ',' || r == '、' || r == ' '
		}) {
			if len([]rune(word)) >= 2 && strings.Contains(tone, word) {
				return role.Name, false
			}
		}
	}
	if len(roles) > 0 {
		// Inner monologue and single-speaker shots conventionally belong to the
		// first billed role. Marking happens at line level for manual review.
		return roles[0].Name, len(roles) > 1
	}
	if lineType == "内心戏" {
		return "旁白", true
	}
	return "待标注", true
}

func cleanDialogueText(text string) string {
	text = strings.TrimSpace(text)
	text = ttsClosingRE.ReplaceAllString(text, "")
	text = strings.TrimSpace(strings.Trim(text, "“”\"「」『』"))
	return text
}

func dialogueType(speaker string) string {
	switch speaker {
	case "旁白", "画外音", "系统":
		return speaker
	default:
		return "台词"
	}
}

func inferDialoguePerformance(explicit, text, context string) (string, int, int, int) {
	source := strings.TrimSpace(explicit)
	all := source + " " + text + " " + context
	tone := source
	speechRate, pitch, loudness := 0, 0, 0
	switch {
	case ttsContainsAny(all, "嘶吼", "咆哮", "怒吼", "暴怒", "震怒", "愤怒", "怒"):
		if tone == "" {
			tone = "愤怒爆发"
		}
		speechRate, pitch, loudness = 15, 3, 20
	case ttsContainsAny(all, "惊恐", "惊慌", "慌张", "震惊", "错愕", "颤抖"):
		if tone == "" {
			tone = "惊恐慌乱"
		}
		speechRate, pitch, loudness = 20, 3, 5
	case ttsContainsAny(all, "虚弱", "喘息", "奄奄", "无力", "疲惫"):
		if tone == "" {
			tone = "虚弱喘息"
		}
		speechRate, pitch, loudness = -20, -2, -15
	case ttsContainsAny(all, "悲伤", "哽咽", "哭", "绝望", "委屈", "低落"):
		if tone == "" {
			tone = "悲伤克制"
		}
		speechRate, pitch, loudness = -15, -2, -8
	case ttsContainsAny(all, "冰冷", "冷冷", "冷酷", "威严", "警告", "低沉", "决绝"):
		if tone == "" {
			tone = "低沉冷峻"
		}
		speechRate, pitch, loudness = -10, -2, 0
	case ttsContainsAny(all, "嘲讽", "讥笑", "冷笑", "嗤笑", "阴阳怪气"):
		if tone == "" {
			tone = "轻蔑嘲讽"
		}
		speechRate, pitch, loudness = -5, 1, 0
	case ttsContainsAny(all, "兴奋", "激动", "惊喜", "欢快", "开心"):
		if tone == "" {
			tone = "兴奋明快"
		}
		speechRate, pitch, loudness = 15, 2, 8
	case strings.Contains(text, "！") || strings.Contains(text, "!"):
		if tone == "" {
			tone = "有力强调"
		}
		speechRate, pitch, loudness = 5, 1, 8
	case dialogueTypeFromContext(all):
		if tone == "" {
			tone = "沉稳旁白"
		}
		speechRate, pitch, loudness = -5, -1, 0
	default:
		if tone == "" {
			tone = "自然叙述"
		}
	}
	return tone, speechRate, pitch, loudness
}

func dialogueTypeFromContext(value string) bool {
	return ttsContainsAny(value, "旁白", "画外音", "系统播报")
}

func ttsContainsAny(value string, words ...string) bool {
	for _, word := range words {
		if strings.Contains(value, word) {
			return true
		}
	}
	return false
}
