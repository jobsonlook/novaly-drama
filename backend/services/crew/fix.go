package crew

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"novaly/backend/models"
	"novaly/backend/services"
)

const maxShotFixRefs = 12

var (
	quotedNameRE        = regexp.MustCompile(`「([^」]+)」`)
	sfxFieldRE          = regexp.MustCompile(`音效：([^；\n「]*)`)
	beatHeaderRE        = regexp.MustCompile(`【[^】]*秒】`)
	shirtlessLeadRE     = regexp.MustCompile(`(?:赤膊|裸上身|没穿上衣|未穿上衣|不穿上衣|光着上身|不着上衣)的`)
	sfxSplitRE          = regexp.MustCompile(`[、，,]`)
	extraPunctRE        = regexp.MustCompile(`[，、；]{2,}`)
	danglingDeRE        = regexp.MustCompile(`的{2,}`)
	speakerSayRE        = regexp.MustCompile(`([\p{Han}A-Za-z0-9甲乙丙丁]{1,12}(?:（[^）]{1,12}）)?)说：`)
	speakerPossessiveRE = regexp.MustCompile(`([\p{Han}A-Za-z0-9甲乙丙丁]{1,12})(?:的(?:完整)?台词|那句|的对白)`)
	appearNameRE        = regexp.MustCompile(`(?:文案出现|角色|人物)[「"]?([\p{Han}A-Za-z0-9甲乙丙丁]{2,8}?)(?:」|出现|未|，|。|在)`)
	dialogueSpanHintRE  = regexp.MustCompile(`拆|后半|两段|另一镜|相邻|派给|分给|截断`)
	overlongHintRE      = regexp.MustCompile(`说不完|时长|字/秒|秒，台词`)
)

func ApplyQCFixes(shots []ShotContext, assets []AssetItem, issues []QCIssue) []ShotContext {
	if len(shots) == 0 {
		return shots
	}
	if len(issues) == 0 {
		return PackShotContexts(cloneShotContexts(shots))
	}
	out := cloneShotContexts(shots)
	index := map[uint]int{}
	for i, shot := range out {
		if shot.ID > 0 {
			index[shot.ID] = i
		}
	}
	needBGM := false
	for _, issue := range issues {
		code := strings.ToUpper(strings.TrimSpace(issue.Code))
		if code == "R5" {
			needBGM = true
			continue
		}
		i, ok := shotIndexForIssue(out, index, issue)
		if !ok {
			continue
		}
		switch code {
		case "R2":
			applyDialogueFix(out, i, issue)
		case "R3":
			if r3IsLensFormatIssue(issue) {
				applyIssueFix(&out[i], assets, issue)
			} else {
				applyContinuityFix(out, i, assets, issue)
			}
		default:
			applyIssueFix(&out[i], assets, issue)
		}
	}
	if needBGM {
		unifyBGMAcrossShots(out)
	}
	for i := range out {
		collapseCharacterIdentityRefs(&out[i], assets, 0)
	}
	syncShotCaptions(out, assets)
	out = DedupeDialogueAcrossShots(out)
	return PackShotContexts(out)
}

// ApplyQCRefFixesPreservingDialogue binds deterministic R1 references without
// running legacy dialogue dedupe/packing. Use after dialogue was rebuilt from
// the locked screenplay.
func ApplyQCRefFixesPreservingDialogue(shots []ShotContext, assets []AssetItem, issues []QCIssue) []ShotContext {
	out := cloneShotContexts(shots)
	index := map[uint]int{}
	for i, shot := range out {
		if shot.ID > 0 {
			index[shot.ID] = i
		}
	}
	for _, issue := range issues {
		if strings.ToUpper(strings.TrimSpace(issue.Code)) != "R1" {
			continue
		}
		i, ok := shotIndexForIssue(out, index, issue)
		if !ok {
			continue
		}
		applyIssueFix(&out[i], assets, issue)
	}
	for i := range out {
		collapseCharacterIdentityRefs(&out[i], assets, 0)
	}
	syncShotCaptions(out, assets)
	return out
}

// DedupeDialogueAcrossShots keeps the first 「」 and strips later copies / long fragments.
func DedupeDialogueAcrossShots(shots []ShotContext) []ShotContext {
	if len(shots) == 0 {
		return shots
	}
	out := cloneShotContexts(shots)
	seen := make([]string, 0)
	for i := range out {
		// Collapse within-shot repeats first (exact short reminders + long+tail).
		out[i].Script = stripWithinShotDuplicateQuotes(out[i].Script)
		for _, q := range quotesInScript(out[i].Script) {
			if speechRunes(q) < 4 {
				continue
			}
			dup := false
			for _, prev := range seen {
				if quotesSubstantivelyDuplicate(q, prev) {
					out[i].Script = stripDialogueQuote(out[i].Script, q)
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			seen = append(seen, q)
		}
		stripEmptyQuotes(&out[i])
	}
	return out
}

func editorialQCIssues(issues []QCIssue) []QCIssue {
	out := make([]QCIssue, 0, len(issues))
	for _, issue := range issues {
		if !mechanicalQCIssue(issue) {
			out = append(out, issue)
		}
	}
	return out
}

func QCIssuesNeedRewrite(issues []QCIssue) bool {
	return len(editorialQCIssues(issues)) > 0
}

// dialoguePipelineShotIDs returns shots that need restore/split (R2), plus next
// shot when overflow may land there. Ref/R5-only batches return empty.
func dialoguePipelineShotIDs(shots []ShotContext, issues []QCIssue) map[uint]bool {
	index := map[uint]int{}
	for i, shot := range shots {
		if shot.ID > 0 {
			index[shot.ID] = i
		}
	}
	out := map[uint]bool{}
	for _, issue := range issues {
		if strings.ToUpper(strings.TrimSpace(issue.Code)) != "R2" {
			continue
		}
		i, ok := shotIndexForIssue(shots, index, issue)
		if r2Kind(issue.Message) == "missing" {
			if m := missingDialogueIssueRE.FindStringSubmatch(issue.Message); len(m) >= 3 {
				locator := missingDialogueLocatorText(m[2])
				for candidate := range shots {
					if locator != "" && strings.Contains(shots[candidate].Script, locator) {
						i, ok = candidate, true
						break
					}
				}
			}
		}
		if !ok {
			continue
		}
		if shots[i].ID > 0 {
			out[shots[i].ID] = true
		}
		blob := issue.Message + issue.Suggestion
		// A missing source line may need more than the reported shot to fit. Give
		// the scoped restore one following shot so it can preserve the full line
		// instead of restoring only another clipped tail.
		missingDialogue := r2Kind(issue.Message) == "missing"
		if i+1 < len(shots) && (missingDialogue || dialogueSpanHintRE.MatchString(blob) || overlongHintRE.MatchString(blob)) {
			out[shots[i+1].ID] = true
		}
		if i > 0 && strings.Contains(blob, "上一") {
			out[shots[i-1].ID] = true
		}
	}
	return out
}

func missingDialogueLocatorText(text string) string {
	return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(text), "…⋯.。· "))
}

// IssuesNeedDialoguePipeline is true when the batch includes R2 dialogue fixes.
func IssuesNeedDialoguePipeline(issues []QCIssue) bool {
	for _, issue := range issues {
		if strings.ToUpper(strings.TrimSpace(issue.Code)) == "R2" {
			return true
		}
	}
	return false
}

func r3IsLensFormatIssue(issue QCIssue) bool {
	msg := issue.Message + issue.Suggestion
	return strings.Contains(msg, "时序行缺少「镜头") || strings.Contains(msg, "缺少「镜头")
}

func r3NeedsEditorialRewrite(issue QCIssue) bool {
	msg := issue.Message + issue.Suggestion
	return strings.Contains(msg, "模糊指代") ||
		strings.Contains(msg, "听者/对方") ||
		strings.Contains(msg, "对面的人")
}

func RewriteShotsForQC(ark *services.ArkService, provider models.AIProvider, model models.AIModel, episodeScript string, shots []ShotContext, issues []QCIssue) ([]ShotContext, error) {
	issues = editorialQCIssues(issues)
	if ark == nil || len(shots) == 0 || len(issues) == 0 {
		return shots, nil
	}
	if services.ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return shots, fmt.Errorf("文本模型服务商未配置 API Key")
	}

	lines := make([]string, 0, len(issues))
	for i, issue := range issues {
		lines = append(lines, fmt.Sprintf("%d. [%s] 分镜%d：%s → 修改为：%s", i+1, firstNonEmpty(issue.Code, "QC"), issue.ShotIndex, issue.Message, firstNonEmpty(issue.Suggestion, "按建议修正")))
	}
	allowed := rewriteAllowedIDs(shots, issues)
	editable, readonly := splitRewriteShots(shots, allowed)
	editJSON, _ := json.Marshal(editable)
	readJSON, _ := json.Marshal(readonly)
	prompt := `你是执行层 Agent，请修复【分镜】的以下问题。
用户确认的修复项：
` + strings.Join(lines, "\n") + `

只改「可改镜头」。相邻镜头只读，用来对照，禁止输出它们。
不要新增镜头。不要改参考图/refs。不要顺手修没点名的问题。
规则：
- 「」内台词必须对照剧本草稿，说话人不能换。一句拆在两拍/两镜时，后半句仍归原说话人，不要改派给旁边的人。
- 台词按时长拆，不按时长删：3秒拍大约12个汉字，单拍超过20字必须切开换景别。禁止为了塞进3秒而精简原文。
- 每句写成 阿彪说：「……」。无台词删掉空「」。
- 允许删旁白，不允许改人物说过的话、不编造情节、不把动作说明写进「」里。
- 音效 = 配乐床 + 环境音 + 动作声。配乐要写，不要删。相邻镜沿用同一曲风，只改强弱。
- 固有服装发型不写进文案；赤膊靠参考图，文案只写动作（浸冰水、接衬衫、扣扣子）。
- 已经穿衣/扣扣子后不要退回赤膊或改成脱衣服。
- 时序格式保持【0-3秒】镜头：…；音效：…；角色名说：「台词」。单镜只写到【7-10秒】，禁止【10-13秒】；多出的拍放到下一镜。
- 单人和多人镜都补九格站位+朝向：每个【秒】行里人物首次出现时写 人名(左前)3/4正面朝右；同场没写走位/转身就沿用原格子和朝向。只有空镜、纯物件、纯手部或脸部极特写可省略，人物极特写须写「承接上一拍人物位置不变」。有站位参考图时按图写格子。
- 一镜具名角色不超过 5 人；点名的人都应能挂参考图。同屏超过 5 人按时间拆到下一镜，群演走站位图。
- 每句台词、每个动作都要承接前一拍的信息并服务于人物的当下目标，再给下一拍留下回应点。用动作、表情、视线和说话方式呈现试探/逼问/说服/隐瞒/拒绝/确认等意图，不要写「目标：」。动作、视线和反应对象必须写具体角色姓名，禁止“听者、对方、对面的人、另一人、其眼神/态度”；不得用停顿、陷入沉思凑时长或为补目标另编剧情。
` + DialogueCraftRules + `

只输出 JSON：{"shots":[{"id":1,"script":"改后的时序文案"}]}
id 必须来自可改镜头。只列出真正改过的镜头。

剧本草稿：
` + clipRunes(episodeScript, 6000) + `

可改镜头：
` + string(editJSON) + `

相邻镜头（只读）：
` + string(readJSON)

	content, err := ark.Chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.2,
		"max_tokens":  4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return shots, err
	}
	var result struct {
		Shots []rewriteShotPatch `json:"shots"`
	}
	if err := unmarshalObject(content, &result); err != nil {
		return shots, fmt.Errorf("改稿解析失败: %w", err)
	}
	return applyRewritePatches(shots, result.Shots, allowed), nil
}

type rewriteShotPatch struct {
	ID     uint   `json:"id"`
	Script string `json:"script"`
}

func applyRewritePatches(shots []ShotContext, patches []rewriteShotPatch, allowed map[uint]bool) []ShotContext {
	out := cloneShotContexts(shots)
	byID := map[uint]int{}
	for i, shot := range out {
		byID[shot.ID] = i
	}
	for _, patch := range patches {
		script := strings.TrimSpace(patch.Script)
		if patch.ID == 0 || script == "" || !allowed[patch.ID] {
			continue
		}
		i, ok := byID[patch.ID]
		if !ok {
			continue
		}
		out[i].Script = script
	}
	return out
}

func rewriteAllowedIDs(shots []ShotContext, issues []QCIssue) map[uint]bool {
	out := shotIDsForIssues(shots, issues)
	index := map[uint]int{}
	for i, shot := range shots {
		if shot.ID > 0 {
			index[shot.ID] = i
		}
	}
	for _, issue := range issues {
		code := strings.ToUpper(strings.TrimSpace(issue.Code))
		if code != "R2" && code != "R3" {
			continue
		}
		i, ok := shotIndexForIssue(shots, index, issue)
		if !ok {
			continue
		}
		blob := issue.Message + issue.Suggestion
		if i+1 < len(shots) && (code == "R3" || dialogueSpanHintRE.MatchString(blob) || overlongHintRE.MatchString(blob)) {
			out[shots[i+1].ID] = true
		}
		if i > 0 && (code == "R3" || strings.Contains(blob, "上一")) {
			out[shots[i-1].ID] = true
		}
	}
	return out
}

func shotIDsForIssues(shots []ShotContext, issues []QCIssue) map[uint]bool {
	index := map[uint]int{}
	for i, shot := range shots {
		if shot.ID > 0 {
			index[shot.ID] = i
		}
	}
	out := map[uint]bool{}
	for _, issue := range issues {
		i, ok := shotIndexForIssue(shots, index, issue)
		if !ok {
			continue
		}
		if shots[i].ID > 0 {
			out[shots[i].ID] = true
		}
	}
	return out
}

func splitRewriteShots(shots []ShotContext, allowed map[uint]bool) (editable, readonly []ShotContext) {
	indexOf := map[uint]int{}
	for i, shot := range shots {
		indexOf[shot.ID] = i
	}
	readonlyIDs := map[uint]bool{}
	for id := range allowed {
		i, ok := indexOf[id]
		if !ok {
			continue
		}
		if i > 0 {
			readonlyIDs[shots[i-1].ID] = true
		}
		if i+1 < len(shots) {
			readonlyIDs[shots[i+1].ID] = true
		}
	}
	for _, shot := range shots {
		switch {
		case allowed[shot.ID]:
			editable = append(editable, shot)
		case readonlyIDs[shot.ID]:
			readonly = append(readonly, shot)
		}
	}
	return editable, readonly
}

func applyIssueFix(shot *ShotContext, assets []AssetItem, issue QCIssue) {
	code := strings.ToUpper(strings.TrimSpace(issue.Code))
	switch code {
	case "R6":
		shot.Script = stripAppearanceFromScript(shot.Script)
	case "R9":
		shot.Script = services.SanitizePlatformViolence(shot.Script)
	case "R1":
		applyMissingOrOverlapRefs(shot, assets, issue)
		bindUnboundMentions(shot, assets)
		bindScriptMentionedCharacters(shot, assets)
		collapseCharacterIdentityRefs(shot, assets, keepResourceIDFromIssue(issue))
	case "R4":
		blob := issue.Message + issue.Suggestion
		if strings.Contains(blob, "配饰") || strings.Contains(blob, "奖牌") || strings.Contains(blob, "项链") || strings.Contains(blob, "绶带") {
			bindNamedProp(shot, assets, "奖牌", "奖章", "奖杯")
			break
		}
		applyCostumeRefSwap(shot, assets)
		collapseCharacterIdentityRefs(shot, assets, 0)
	case "R3":
		blob := issue.Message + issue.Suggestion
		if shirtlessRE.MatchString(blob) || puttingOnRE.MatchString(blob) || takingOffRE.MatchString(blob) {
			applyCostumeScriptFix(shot)
			if strings.Contains(blob, "又变成") || strings.Contains(blob, "退回") {
				shot.Script = stripShirtlessPhrases(shot.Script)
			}
		}
	}
	if strings.Contains(issue.Message, "镜头：") || strings.Contains(issue.Message, "缺少「镜头") {
		shot.Script = ensureLensLines(shot.Script)
	}
}

func applyMissingOrOverlapRefs(shot *ShotContext, assets []AssetItem, issue QCIssue) {
	keepID := keepResourceIDFromIssue(issue)
	dropID := dropResourceIDFromIssue(issue)
	if dropID > 0 {
		shot.Refs = filterRefsExcept(shot.Refs, dropID)
	}
	if isOverlapR1(issue) {
		collapseCharacterIdentityRefs(shot, assets, keepID)
		return
	}
	mismatchScene := strings.Contains(issue.Message, "参考图绑的是") || strings.Contains(issue.Message, "文案地点")
	if mismatchScene || (strings.Contains(issue.Message, "场景") && !hasKind(shot.Refs, "scene")) {
		if mismatchScene {
			shot.Refs = filterRefsByKind(shot.Refs, "scene", false)
		}
		if scene := pickSceneAsset(shot.Script, assets); scene != nil {
			addRef(shot, refFromAsset(*scene))
		}
	}
	if issue.ResourceID > 0 && issue.ResourceID != dropID {
		if a := assetByResourceID(assets, issue.ResourceID); a != nil {
			addRef(shot, refFromAsset(*a))
		}
	}
	for _, name := range namesFromQCText(issue.Message + issue.Suggestion) {
		if a := assetByName(assets, name); a != nil {
			if mismatchScene && normalizeAssetType(a.Type) == "scene" {
				continue
			}
			addRef(shot, refFromAsset(*a))
		}
	}
	if keepID > 0 {
		collapseCharacterIdentityRefs(shot, assets, keepID)
	}
}

func namesFromQCText(text string) []string {
	out := quotedNames(text)
	seen := map[string]bool{}
	for _, name := range out {
		seen[name] = true
	}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || len([]rune(name)) > 12 {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, m := range appearNameRE.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 {
			add(strings.Trim(m[1], "「」\"'"))
		}
	}
	for _, m := range speakerPossessiveRE.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	return out
}

func bindUnboundMentions(shot *ShotContext, assets []AssetItem) {
	allNames := make([]string, 0, len(assets))
	for _, a := range assets {
		if n := strings.TrimSpace(a.Name); n != "" {
			allNames = append(allNames, n)
		}
	}
	for _, a := range assets {
		if a.ResourceID == 0 || normalizeAssetType(a.Type) == "" {
			continue
		}
		if !standaloneMention(shot.Script, a.Name, allNames) {
			continue
		}
		addRef(shot, refFromAsset(a))
	}
}

// bindScriptMentionedCharacters hangs 人名(九格)/人名说 even when the model left
// them out of characterNames. Crowd extras stay on 站位图.
func bindScriptMentionedCharacters(shot *ShotContext, assets []AssetItem) {
	for _, name := range services.MentionedCharacterNames(shot.Script) {
		if a := assetByName(assets, name); a != nil && normalizeAssetType(a.Type) == "character" {
			addRef(shot, refFromAsset(*a))
		}
	}
}

func applyDialogueFix(shots []ShotContext, i int, issue QCIssue) {
	blob := issue.Message + issue.Suggestion
	if strings.Contains(blob, "同一镜内重复") {
		for _, q := range quotedNames(blob) {
			if speechRunes(q) < 6 {
				continue
			}
			shots[i].Script = stripDuplicateDialogueQuote(shots[i].Script, q)
		}
		stripEmptyQuotes(&shots[i])
		return
	}
	if strings.Contains(blob, "重复同一句") || strings.Contains(blob, "两镜台词重复") {
		for _, q := range quotedNames(blob) {
			if speechRunes(q) < 6 {
				continue
			}
			shots[i].Script = stripDialogueQuote(shots[i].Script, q)
		}
		stripEmptyQuotes(&shots[i])
		return
	}
	if strings.Contains(blob, "改派说话人") || strings.Contains(blob, "说话人应为") {
		want := wantSpeakerFromMisattrIssue(blob)
		if want != "" {
			quotes := quotedNames(blob)
			if len(quotes) > 0 {
				shots[i].Script = relabelQuoteSpeakerForText(shots[i].Script, quotes[0], want)
			} else {
				relabelQuoteSpeaker(&shots[i], want)
			}
			ensureOnscreenSpokenSpeakers(&shots[i])
			return
		}
	}
	if strings.Contains(blob, "说话人未进画面") || strings.Contains(blob, "对错口型") {
		ensureOnscreenSpokenSpeakers(&shots[i])
		return
	}
	speakers := speakersFromIssue(blob)
	var next *ShotContext
	if i+1 < len(shots) {
		next = &shots[i+1]
	}
	if len(speakers) > 0 && (dialogueSpanHintRE.MatchString(blob) || quoteLooksTruncated(lastQuote(shots[i].Script))) {
		if next != nil {
			pullContinuationQuote(&shots[i], next, speakers[0])
		}
	}
	splitMergedQuotes(&shots[i])
	ensureQuoteSpeakers(&shots[i], speakers)
	splitOverlongDialogue(&shots[i], next)
	stripEmptyQuotes(&shots[i])
	ensureQuoteSpeakers(&shots[i], speakers)
}

var wantSpeakerMisattrRE = regexp.MustCompile(`(?:剧本是|改回|应为)\s*([\p{Han}A-Za-z0-9甲乙丙丁]{1,12})\s*说`)

func wantSpeakerFromMisattrIssue(text string) string {
	if m := wantSpeakerMisattrRE.FindStringSubmatch(text); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func relabelQuoteSpeakerForText(script, quote, speaker string) string {
	quote = strings.TrimSpace(quote)
	speaker = strings.TrimSpace(speaker)
	if quote == "" || speaker == "" {
		return script
	}
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "「"+quote) && !strings.Contains(quoteKey(line), quoteKey(quote)) {
			continue
		}
		lines[i] = lockSpokenIntoLine(line, SpokenLine{Speaker: speaker, Text: firstNonEmpty(firstQuoteInLine(line), quote)})
	}
	return strings.Join(lines, "\n")
}

func applyContinuityFix(shots []ShotContext, i int, assets []AssetItem, issue QCIssue) {
	if r3NeedsEditorialRewrite(issue) {
		concretizeAmbiguousTargets(&shots[i])
		return
	}
	applyCostumeScriptFix(&shots[i])
	applyCostumeRefSwap(&shots[i], assets)
	blob := issue.Message + issue.Suggestion
	if strings.Contains(blob, "又变成") || strings.Contains(blob, "退回") {
		shots[i].Script = stripShirtlessPhrases(shots[i].Script)
	}
	if i+1 < len(shots) {
		applyCostumeScriptFix(&shots[i+1])
		applyCostumeRefSwap(&shots[i+1], assets)
	}
	if i > 0 && (shirtlessRE.MatchString(blob) || puttingOnRE.MatchString(blob)) {
		applyCostumeRefSwap(&shots[i-1], assets)
	}
	applyIssueFix(&shots[i], assets, issue)
}

func concretizeAmbiguousTargets(shot *ShotContext) {
	if shot == nil || !ambiguousInteractionTargetRE.MatchString(stripQuotedDialogue(shot.Script)) {
		return
	}
	names := make([]string, 0, len(shot.Refs))
	seen := map[string]bool{}
	for _, ref := range shot.Refs {
		if !strings.EqualFold(strings.TrimSpace(ref.Kind), "character") {
			continue
		}
		name := strings.TrimSpace(firstNonEmpty(ref.DisplayName, ref.ParentName, ref.Name))
		if cut := strings.IndexAny(name, "·（("); cut > 0 {
			name = strings.TrimSpace(name[:cut])
		}
		if name == "" || strings.HasPrefix(name, "#") || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) < 2 {
		return
	}
	lines := strings.Split(shot.Script, "\n")
	for li, line := range lines {
		clean := stripQuotedDialogue(line)
		if !ambiguousInteractionTargetRE.MatchString(clean) {
			continue
		}
		firstAmbiguous := ambiguousInteractionTargetRE.FindStringIndex(clean)
		actor := ""
		actorPos := -1
		if firstAmbiguous != nil {
			prefix := clean[:firstAmbiguous[0]]
			for _, name := range names {
				if pos := strings.LastIndex(prefix, name); pos > actorPos {
					actor, actorPos = name, pos
				}
			}
		}
		target := ""
		for _, name := range names {
			if name != actor {
				target = name
				break
			}
		}
		if target == "" {
			continue
		}
		// Only replace action prose. Dialogue may legitimately contain “对方”.
		lines[li] = replaceOutsideDialogue(line, func(part string) string {
			for _, noun := range []string{"眼神", "反应", "态度", "回应"} {
				part = strings.ReplaceAll(part, "其"+noun, target+"的"+noun)
			}
			return regexp.MustCompile(`听者|对方|对面(?:的人|角色)?|另一(?:人|角色)`).ReplaceAllString(part, target)
		})
	}
	shot.Script = strings.Join(lines, "\n")
}

func replaceOutsideDialogue(line string, replace func(string) string) string {
	locs := dialogueRE.FindAllStringIndex(line, -1)
	if len(locs) == 0 {
		return replace(line)
	}
	var b strings.Builder
	cursor := 0
	for _, loc := range locs {
		b.WriteString(replace(line[cursor:loc[0]]))
		b.WriteString(line[loc[0]:loc[1]])
		cursor = loc[1]
	}
	b.WriteString(replace(line[cursor:]))
	return b.String()
}

func applyCostumeRefSwap(shot *ShotContext, assets []AssetItem) {
	puttingOn := puttingOnRE.MatchString(shot.Script)
	shirtless := shirtlessRE.MatchString(shot.Script)
	if puttingOn && !shirtless {
		replaceShirtlessWithParent(shot, assets)
		return
	}
	if shirtless && !puttingOn {
		replaceParentWithShirtless(shot, assets)
	}
}

func applyCostumeScriptFix(shot *ShotContext) {
	shirtless := shirtlessRE.MatchString(shot.Script)
	puttingOn := puttingOnRE.MatchString(shot.Script)
	takingOff := takingOffRE.MatchString(shot.Script)
	if shirtless && puttingOn {
		shot.Script = stripShirtlessPhrases(shot.Script)
	}
	if shirtless && takingOff {
		shot.Script = takingOffRE.ReplaceAllString(shot.Script, "")
		shot.Script = cleanupScript(shot.Script)
	}
	if puttingOn && takingOff {
		shot.Script = takingOffRE.ReplaceAllString(shot.Script, "")
		shot.Script = cleanupScript(shot.Script)
	}
}

func speakersFromIssue(text string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || name == "角色" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, m := range speakerSayRE.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	add(speakerFromIssue(text))
	return out
}

func shotSpeakerNames(shot ShotContext) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(shot.Refs))
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if i := strings.Index(name, "·"); i > 0 {
			name = strings.TrimSpace(name[:i])
		}
		if i := strings.Index(name, "（"); i > 0 {
			name = strings.TrimSpace(name[:i])
		}
		key := strings.ToLower(name)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, name)
	}
	for _, r := range shot.Refs {
		if r.Kind != "character" {
			continue
		}
		add(r.ParentName)
		add(r.DisplayName)
		add(r.Name)
	}
	return out
}

func ensureQuoteSpeakers(shot *ShotContext, preferred []string) {
	if shot == nil {
		return
	}
	fallback := shotSpeakerNames(*shot)
	lines := strings.Split(shot.Script, "\n")
	used := 0
	for i, line := range lines {
		lines[i], used = ensureQuoteSpeakersOnLine(line, preferred, fallback, used)
	}
	shot.Script = strings.Join(lines, "\n")
}

func ensureQuoteSpeakersOnLine(line string, preferred, fallback []string, used int) (string, int) {
	if !strings.Contains(line, "「") {
		return line, used
	}
	var b strings.Builder
	i := 0
	for i < len(line) {
		if strings.HasPrefix(line[i:], "「") {
			if !prefixHasSpeaker(line[:i]) {
				speaker := pickQuoteSpeaker(line, preferred, fallback, used)
				if speaker != "" {
					b.WriteString(speaker)
					b.WriteString("说：")
					used++
				}
			}
			b.WriteString("「")
			i += len("「")
			continue
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String(), used
}

func pickQuoteSpeaker(line string, preferred, fallback []string, used int) string {
	if used < len(preferred) {
		return preferred[used]
	}
	for _, name := range fallback {
		if strings.Contains(line, name) {
			return name
		}
	}
	if len(preferred) > 0 {
		return preferred[len(preferred)-1]
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return ""
}

func splitMergedQuotes(shot *ShotContext) {
	if shot == nil {
		return
	}
	lines := strings.Split(shot.Script, "\n")
	type extraQuote struct {
		text string
		say  string
	}
	extras := make([]extraQuote, 0)
	for i, line := range lines {
		qs := dialogueRE.FindAllStringSubmatchIndex(line, -1)
		if len(qs) <= 1 {
			continue
		}
		firstEnd := qs[0][1]
		lines[i] = strings.TrimRight(line[:firstEnd], "；;，, ")
		for _, loc := range qs[1:] {
			if loc[2] < 0 || loc[3] > len(line) {
				continue
			}
			text := strings.TrimSpace(line[loc[2]:loc[3]])
			if quoteIsEmpty(text) {
				continue
			}
			say := speakerPrefixBefore(line[:loc[0]])
			extras = append(extras, extraQuote{text: text, say: say})
		}
	}
	ei := 0
	for i, line := range lines {
		if ei >= len(extras) {
			break
		}
		if !strings.Contains(line, "秒】") {
			continue
		}
		if dialogueRE.MatchString(line) {
			continue
		}
		item := extras[ei]
		prefix := item.say
		if prefix != "" && !strings.HasSuffix(prefix, "说：") && !strings.HasSuffix(prefix, "：") {
			prefix += "说："
		}
		lines[i] = strings.TrimRight(line, "。； ") + "；" + prefix + "「" + item.text + "」"
		ei++
	}
	for ei < len(extras) {
		item := extras[ei]
		prefix := item.say
		if prefix == "" {
			prefix = ""
		} else if !strings.HasSuffix(prefix, "说：") {
			prefix += "说："
		}
		shot.Script = strings.Join(lines, "\n")
		shot.Script = appendTimedBeat(shot.Script, "镜头：反应；"+prefix+"「"+item.text+"」", shot.Duration)
		lines = strings.Split(shot.Script, "\n")
		ei++
	}
	shot.Script = strings.Join(lines, "\n")
}

func speakerPrefixBefore(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if m := speakerSayRE.FindAllStringSubmatch(prefix, -1); len(m) > 0 {
		return strings.TrimSpace(m[len(m)-1][1]) + "说："
	}
	return ""
}

func speakerFromIssue(text string) string {
	if m := speakerPossessiveRE.FindStringSubmatch(text); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	for _, name := range quotedNames(text) {
		if len([]rune(name)) <= 8 {
			return name
		}
	}
	return ""
}

func lastQuote(script string) string {
	matches := dialogueRE.FindAllStringSubmatch(script, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

func firstQuote(script string) string {
	m := dialogueRE.FindStringSubmatch(script)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func quoteLooksTruncated(q string) bool {
	q = strings.TrimSpace(q)
	return strings.HasSuffix(q, "…") || strings.HasSuffix(q, "...") ||
		strings.HasSuffix(q, "—") || strings.HasSuffix(q, "——") || strings.HasSuffix(q, "–")
}

func relabelQuoteSpeaker(shot *ShotContext, speaker string) {
	speaker = strings.TrimSpace(speaker)
	if speaker == "" || shot == nil {
		return
	}
	lines := strings.Split(shot.Script, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "「") || strings.Contains(line, speaker) {
			continue
		}
		if loc := strings.Index(line, "镜头："); loc >= 0 {
			head, rest := line[:loc+len("镜头：")], strings.TrimSpace(line[loc+len("镜头："):])
			lines[i] = head + speaker + "说话，" + rest
		}
	}
	shot.Script = strings.Join(lines, "\n")
}

func pullContinuationQuote(curr, next *ShotContext, speaker string) {
	q := strings.TrimSpace(firstQuote(next.Script))
	if q == "" {
		return
	}
	if speaker != "" && strings.Contains(firstScriptBeat(next.Script), speaker) && !quoteLooksTruncated(lastQuote(curr.Script)) {
		return
	}
	curr.Script = appendQuoteToLastBeat(curr.Script, q)
	next.Script = removeFirstQuote(next.Script)
}

func appendQuoteToLastBeat(script, quote string) string {
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return script
	}
	lines := strings.Split(script, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.Contains(lines[i], "秒】") {
			continue
		}
		if strings.Contains(lines[i], "「") {
			lines[i] = strings.TrimRight(lines[i], "。； ") + "「" + quote + "」"
		} else {
			lines[i] = strings.TrimRight(lines[i], "。； ") + "；「" + quote + "」"
		}
		return strings.Join(lines, "\n")
	}
	return strings.TrimRight(script, "\n") + "\n「" + quote + "」"
}

func removeFirstQuote(script string) string {
	replaced := false
	return dialogueRE.ReplaceAllStringFunc(script, func(m string) string {
		if replaced {
			return m
		}
		replaced = true
		return ""
	})
}

func splitOverlongDialogue(shot *ShotContext, next *ShotContext) {
	if shot == nil {
		return
	}
	shot.Script = normalizeScriptBeatStructure(shot.Script)
	lines := strings.Split(shot.Script, "\n")
	lines = relocateTrailingQuoteToEarlierEmptyBeat(lines)
	speaker := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		secs := beatSeconds(line)
		m := dialogueRE.FindStringSubmatch(line)
		if secs <= 0 || len(m) < 2 {
			continue
		}
		if s := spokenSpeakerName(line); s != "" {
			speaker = s
		}
		if speechRunes(m[1]) <= maxSpeechRunes(secs) {
			continue
		}
		quote := m[1]
		lines[i] = cleanupScript(stripDialogueQuote(line, quote))
		if strings.TrimSpace(beatHeaderRE.ReplaceAllString(lines[i], "")) == "" {
			lines[i] = beatHeaderRE.FindString(lines[i]) + "镜头：反应"
		}
		lines, quote = fillQuoteIntoBeats(lines, i, quote, speaker)
		if quote != "" {
			lines, quote = appendQuoteOverflowLines(lines, quote, speaker, shot)
			if quote != "" && next != nil {
				placeDialogueOverflow(next, quote, speaker, -1)
			}
		}
	}
	shot.Script = strings.Join(lines, "\n")
}

// fillQuoteIntoBeats places as much of quote as each beat's duration allows,
// starting at start. Skips beats that already carry dialogue or are too short
// for even one clause; never splits mid-word across beats.
func fillQuoteIntoBeats(lines []string, start int, quote, speaker string) ([]string, string) {
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return lines, ""
	}
	joined := strings.Join(lines, "\n")
	for i := start; i < len(lines) && quote != ""; i++ {
		line := lines[i]
		if !strings.Contains(line, "秒】") || beatHasSpokenQuote(line) {
			continue
		}
		secs := beatSeconds(line)
		if secs <= 0 {
			continue
		}
		if scriptAlreadyCoversQuote(joined, quote) {
			return lines, ""
		}
		keep, rest := splitQuoteForBeat(quote, secs)
		if keep == "" {
			continue
		}
		line = insertQuote(line, keep)
		if speaker != "" && !prefixHasSpeaker(line) {
			line = ensureSpeakerOnQuote(line, speaker)
		}
		lines[i] = line
		quote = rest
		joined = strings.Join(lines, "\n")
	}
	return lines, quote
}

func appendQuoteOverflowLines(lines []string, quote, speaker string, shot *ShotContext) ([]string, string) {
	if shot == nil || strings.TrimSpace(quote) == "" {
		return lines, quote
	}
	max := ShotMaxSeconds(shot.Duration)
	script := strings.Join(lines, "\n")
	for strings.TrimSpace(quote) != "" {
		if scriptAlreadyCoversQuote(script, quote) {
			break
		}
		if scriptTimelineEnd(script) >= max {
			break
		}
		slot := float64(max - scriptTimelineEnd(script))
		if slot > 4 {
			slot = 4
		}
		if slot < 1 {
			slot = 1
		}
		keep, rest := splitQuoteForBeat(quote, slot)
		if keep == "" {
			keep = quote
			rest = ""
		}
		body := "镜头：反应；"
		if speaker != "" {
			body += speaker + "说："
		}
		body += "「" + keep + "」"
		script = appendTimedBeat(script, body, max)
		quote = rest
	}
	return strings.Split(script, "\n"), strings.TrimSpace(quote)
}

// relocateTrailingQuoteToEarlierEmptyBeat moves a long line stuck on the last
// beat into the first earlier beat without 「」, so splits read forward in time.
func relocateTrailingQuoteToEarlierEmptyBeat(lines []string) []string {
	lastIdx := -1
	for i, line := range lines {
		if dialogueRE.MatchString(line) && beatHasSpokenQuote(line) {
			lastIdx = i
		}
	}
	if lastIdx <= 0 {
		return lines
	}
	firstEmpty := -1
	for i := 0; i < lastIdx; i++ {
		if strings.Contains(lines[i], "秒】") && !beatHasSpokenQuote(lines[i]) {
			firstEmpty = i
			break
		}
	}
	if firstEmpty < 0 {
		return lines
	}
	src := lines[lastIdx]
	m := dialogueRE.FindStringSubmatch(src)
	if len(m) < 2 || quoteIsEmpty(m[1]) {
		return lines
	}
	speaker := spokenSpeakerName(src)
	dst := lines[firstEmpty]
	dst = insertQuote(dst, m[1])
	if speaker != "" && !strings.Contains(dst, speaker+"说") {
		dst = ensureSpeakerOnQuote(dst, speaker)
	}
	lines[firstEmpty] = dst
	lines[lastIdx] = stripDialogueQuote(src, m[1])
	lines[lastIdx] = cleanupScript(lines[lastIdx])
	if strings.TrimSpace(beatHeaderRE.ReplaceAllString(lines[lastIdx], "")) == "" {
		lines[lastIdx] = beatHeaderRE.FindString(lines[lastIdx]) + "镜头：反应"
	}
	return lines
}

// placeDialogueOverflow fills beats after the line that was split, then appends
// a continuation beat. Never back-fills earlier beats or dialogue plays backwards.
func placeDialogueOverflow(shot *ShotContext, overflow, speaker string, afterLine int) {
	if shot == nil || strings.TrimSpace(overflow) == "" {
		return
	}
	if scriptAlreadyCoversQuote(shot.Script, overflow) {
		return
	}
	lines := strings.Split(shot.Script, "\n")
	start := afterLine + 1
	if start < 0 {
		start = 0
	}
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if !strings.Contains(line, "秒】") || beatHasSpokenQuote(line) {
			continue
		}
		if scriptAlreadyCoversQuote(strings.Join(lines, "\n"), overflow) {
			return
		}
		line = insertQuote(line, overflow)
		if speaker != "" && !strings.Contains(line, speaker+"说") {
			line = ensureSpeakerOnQuote(line, speaker)
		}
		lines[i] = line
		shot.Script = strings.Join(lines, "\n")
		return
	}
	body := "镜头：反应；"
	if speaker != "" {
		body += speaker + "说："
	}
	body += "「" + overflow + "」"
	max := ShotMaxSeconds(shot.Duration)
	if scriptTimelineEnd(shot.Script) >= max {
		shot.Script = mergeOverflowIntoLastBeat(shot.Script, overflow, speaker)
		return
	}
	shot.Script = appendTimedBeat(shot.Script, body, max)
}

// scriptAlreadyCoversQuote reports whether quote is already present (exact or
// as a substantive fragment/suffix of an existing 「」) in script.
func scriptAlreadyCoversQuote(script, quote string) bool {
	quote = strings.TrimSpace(quote)
	if quote == "" || speechRunes(quote) < 4 {
		return false
	}
	for _, q := range quotesInScript(script) {
		if quotesSubstantivelyDuplicate(q, quote) {
			return true
		}
		qk, tk := normalizeQuoteKey(q), normalizeQuoteKey(quote)
		if tk != "" && strings.HasSuffix(qk, tk) && speechRunes(quote) >= 6 {
			return true
		}
		if qk != "" && strings.HasSuffix(tk, qk) && speechRunes(q) >= 6 {
			return true
		}
	}
	return false
}

func beatHasSpokenQuote(line string) bool {
	for _, m := range dialogueRE.FindAllStringSubmatch(line, -1) {
		if len(m) > 1 && !quoteIsEmpty(m[1]) {
			return true
		}
	}
	return false
}

func spokenSpeakerName(line string) string {
	if prefix := speakerPrefixBefore(line); prefix != "" {
		return strings.TrimSuffix(strings.TrimSuffix(prefix, "："), "说")
	}
	return ""
}

func ensureSpeakerOnQuote(line, speaker string) string {
	speaker = strings.TrimSpace(speaker)
	if speaker == "" || prefixHasSpeaker(line) {
		return line
	}
	idx := strings.Index(line, "「")
	if idx < 0 {
		return line
	}
	return line[:idx] + speaker + "说：" + line[idx:]
}

func splitQuoteForBeat(quote string, secs float64) (keep, rest string) {
	maxN := maxSpeechRunes(secs)
	quote = strings.TrimSpace(quote)
	if speechRunes(quote) <= maxN {
		return quote, ""
	}
	runes := []rune(quote)
	bestCut := -1
	for i := 1; i <= len(runes); i++ {
		prefix := string(runes[:i])
		if speechRunes(prefix) > maxN {
			break
		}
		// Short vocatives such as「裴师傅，」「福公，」are valid semantic
		// pauses. Rejecting boundaries under four characters made the fallback
		// cut later at raw quota (交给/我), which is much worse.
		if isClauseBreakRune(runes[i-1]) && speechRunes(prefix) >= 2 {
			bestCut = i
		}
	}
	if bestCut > 0 && bestCut < len(runes) {
		keep = strings.TrimSpace(string(runes[:bestCut]))
		rest = strings.TrimSpace(string(runes[bestCut:]))
		if keep != "" && rest != "" && !quoteSplitMidWord(keep, rest) {
			return keep, rest
		}
	}
	// No clause boundary — only hard-cut at a non-Han boundary. Chinese has no
	// whitespace word delimiter, so Han/Han cuts such as 靠/谱人 are unsafe even
	// when neither side is a one-rune orphan.
	cut := 0
	for i := 1; i <= len(runes); i++ {
		if speechRunes(string(runes[:i])) > maxN {
			break
		}
		candidateKeep := strings.TrimSpace(string(runes[:i]))
		candidateRest := strings.TrimSpace(string(runes[i:]))
		if candidateRest != "" && !quoteSplitMidWord(candidateKeep, candidateRest) {
			cut = i
		}
	}
	if cut > 0 && cut < len(runes) {
		keep = strings.TrimSpace(string(runes[:cut]))
		rest = strings.TrimSpace(string(runes[cut:]))
		return keep, rest
	}
	// Beat too short for even one safe fragment — defer whole quote forward.
	return "", quote
}

func isClauseBreakRune(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '，', '、', ',', '.', '—', '–', '…':
		return true
	default:
		return false
	}
}

func quoteSplitMidWord(keep, rest string) bool {
	rest = strings.TrimLeft(rest, "…—–-. ")
	kr := []rune(strings.TrimRight(keep, "…—–-. "))
	rr := []rune(rest)
	if len(kr) == 0 || len(rr) == 0 {
		return false
	}
	if isClauseBreakRune(kr[len(kr)-1]) {
		return false
	}
	if !unicode.Is(unicode.Han, kr[len(kr)-1]) || !unicode.Is(unicode.Han, rr[0]) {
		return false
	}
	left, right := string(kr), string(rr)
	// For unpunctuated colloquial Chinese, permit only recognizable syntactic
	// boundaries. This is deliberately conservative: unknown Han/Han cuts stay
	// unsafe instead of trusting a raw character quota.
	for _, prefix := range []string{"我", "你", "他", "她", "它", "咱", "您", "别", "再", "还", "去", "来", "把", "被", "让", "给", "要", "会", "能", "该", "就", "才", "却", "但", "而", "若", "如果", "然后", "随后", "今日", "明日", "现在", "韩小灶"} {
		if strings.HasPrefix(right, prefix) {
			return false
		}
	}
	for _, suffix := range []string{"吧", "吗", "呢", "啊", "呀", "了", "着", "过", "人", "们", "师傅", "老板", "小灶"} {
		if strings.HasSuffix(left, suffix) {
			return false
		}
	}
	return true
}

func stripEmptyQuotes(shot *ShotContext) {
	if shot == nil {
		return
	}
	shot.Script = dialogueRE.ReplaceAllStringFunc(shot.Script, func(m string) string {
		sub := dialogueRE.FindStringSubmatch(m)
		if len(sub) > 1 && quoteIsEmpty(sub[1]) {
			return ""
		}
		return m
	})
	shot.Script = cleanupScript(shot.Script)
}

var danglingSpeakerTailRE = regexp.MustCompile(`；?\s*[\p{Han}A-Za-z0-9甲乙丙丁]{1,12}(?:（[^）]{1,12}）)?说：\s*$`)
var danglingInnerMonologueTailRE = regexp.MustCompile(`；?\s*[\p{Han}A-Za-z0-9甲乙丙丁]{1,12}(?:（[^）]{1,12}）)?内心独白：\s*$`)

func stripDialogueQuote(script, quote string) string {
	if strings.TrimSpace(quote) == "" {
		return script
	}
	var keep []string
	for _, line := range strings.Split(script, "\n") {
		for _, m := range dialogueRE.FindAllStringSubmatch(line, -1) {
			if len(m) < 2 {
				continue
			}
			if !quoteMatchesStripTarget(m[1], quote) {
				continue
			}
			line = strings.Replace(line, m[0], "", 1)
		}
		line = danglingSpeakerTailRE.ReplaceAllString(line, "")
		line = danglingInnerMonologueTailRE.ReplaceAllString(line, "")
		line = strings.TrimSpace(strings.Trim(line, "；"))
		rest := strings.TrimSpace(beatHeaderRE.ReplaceAllString(line, ""))
		if rest == "" {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

// stripLaterDuplicateQuotes keeps the first matching 「」 and removes later ones
// in the same script (within-shot exact / fragment repeats).
func stripLaterDuplicateQuotes(script, quote string) string {
	if strings.TrimSpace(quote) == "" {
		return script
	}
	seen := false
	var keep []string
	for _, line := range strings.Split(script, "\n") {
		for _, m := range dialogueRE.FindAllStringSubmatch(line, -1) {
			if len(m) < 2 || !quoteMatchesStripTarget(m[1], quote) {
				continue
			}
			if !seen {
				seen = true
				continue
			}
			line = strings.Replace(line, m[0], "", 1)
		}
		line = danglingSpeakerTailRE.ReplaceAllString(line, "")
		line = danglingInnerMonologueTailRE.ReplaceAllString(line, "")
		line = strings.TrimSpace(strings.Trim(line, "；"))
		rest := strings.TrimSpace(beatHeaderRE.ReplaceAllString(line, ""))
		if rest == "" {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

// stripWithinShotDuplicateQuotes walks 「」 in order, keeps the first, and removes
// later exact / substantive duplicates (e.g. full line then 「少惹姚三刀」tail).
func stripWithinShotDuplicateQuotes(script string) string {
	seen := make([]string, 0)
	var keep []string
	for _, line := range strings.Split(script, "\n") {
		for _, m := range dialogueRE.FindAllStringSubmatch(line, -1) {
			if len(m) < 2 {
				continue
			}
			q := strings.TrimSpace(m[1])
			if quoteIsEmpty(q) || speechRunes(q) < 4 {
				continue
			}
			dup := false
			for _, prev := range seen {
				if quotesSubstantivelyDuplicate(q, prev) {
					dup = true
					break
				}
			}
			if dup {
				line = strings.Replace(line, m[0], "", 1)
				continue
			}
			seen = append(seen, q)
		}
		line = danglingSpeakerTailRE.ReplaceAllString(line, "")
		line = danglingInnerMonologueTailRE.ReplaceAllString(line, "")
		line = strings.TrimSpace(strings.Trim(line, "；"))
		rest := strings.TrimSpace(beatHeaderRE.ReplaceAllString(line, ""))
		if rest == "" {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

func quoteMatchesStripTarget(got, want string) bool {
	ga, wb := normalizeQuoteKey(got), normalizeQuoteKey(want)
	if ga == "" || wb == "" {
		return false
	}
	if ga == wb {
		return true
	}
	// When asked to strip a short fragment, never delete the longer original line
	// that merely contains it (Dedupe passes the later short copy as want).
	if speechRunes(got) > speechRunes(want) {
		return false
	}
	if quotesSubstantivelyDuplicate(got, want) {
		return true
	}
	// Issue messages may carry a clipped prefix of the real 「」.
	if strings.HasPrefix(ga, wb) || strings.HasPrefix(wb, ga) {
		return speechRunes(got) >= 6 || speechRunes(want) >= 6
	}
	return false
}

// stripDuplicateDialogueQuote keeps the first occurrence of quote and removes later ones.
func stripDuplicateDialogueQuote(script, quote string) string {
	key := quoteKey(quote)
	if key == "" {
		return script
	}
	seen := false
	var keep []string
	for _, line := range strings.Split(script, "\n") {
		for _, m := range dialogueRE.FindAllStringSubmatch(line, -1) {
			if len(m) < 2 || quoteKey(m[1]) != key {
				continue
			}
			if !seen {
				seen = true
				continue
			}
			line = strings.Replace(line, m[0], "", 1)
		}
		line = danglingSpeakerTailRE.ReplaceAllString(line, "")
		line = danglingInnerMonologueTailRE.ReplaceAllString(line, "")
		line = strings.TrimSpace(strings.Trim(line, "；"))
		rest := strings.TrimSpace(beatHeaderRE.ReplaceAllString(line, ""))
		if rest == "" {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

func insertQuote(line, quote string) string {
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return line
	}
	if strings.Contains(line, "「") {
		return strings.TrimRight(line, "。； ") + "「" + quote + "」"
	}
	return strings.TrimRight(line, "。； ") + "；「" + quote + "」"
}

func insertSpeaker(line, speaker string) string {
	if loc := strings.Index(line, "镜头："); loc >= 0 {
		return line[:loc+len("镜头：")] + speaker + "说话，" + strings.TrimSpace(line[loc+len("镜头："):])
	}
	return line
}

// ensureOnscreenSpokenSpeakers rewrites each dialogue beat so the attributed
// speaker appears in the lens body (lip-sync safety for video models).
func ensureOnscreenSpokenSpeakers(shot *ShotContext) {
	if shot == nil {
		return
	}
	lines := strings.Split(shot.Script, "\n")
	changed := false
	for i, line := range lines {
		if !strings.Contains(line, "秒】") || !strings.Contains(line, "「") {
			continue
		}
		if strings.Contains(line, "内心独白：") && !strings.Contains(line, "说：") {
			continue
		}
		speaker := spokenSpeakerInBeat(line)
		if speaker == "" {
			continue
		}
		lens := lensBodyForSpeakerCheck(line)
		if speakerVisibleInLens(lens, speaker) {
			// Still reinforce speaking action if missing.
			if !strings.Contains(lens, "说话") && !strings.Contains(lens, "开口") && !strings.Contains(lens, "念") {
				fixed := reinforceSpeakerSpeaking(line, speaker)
				if fixed != line {
					lines[i] = fixed
					changed = true
				}
			}
			continue
		}
		fixed := insertSpeakerSpeaking(line, speaker)
		if fixed != line {
			lines[i] = fixed
			changed = true
		}
	}
	if changed {
		shot.Script = strings.Join(lines, "\n")
	}
}

func insertSpeakerSpeaking(line, speaker string) string {
	speaker = strings.TrimSpace(speaker)
	if speaker == "" {
		return line
	}
	hint := speaker + "说话，口型清晰，"
	for _, key := range []string{"镜头：", "镜头:"} {
		if loc := strings.Index(line, key); loc >= 0 {
			head := line[:loc+len(key)]
			rest := strings.TrimSpace(line[loc+len(key):])
			// Avoid double prefix.
			if strings.HasPrefix(rest, speaker+"说话") {
				return line
			}
			return head + hint + rest
		}
	}
	return line
}

func reinforceSpeakerSpeaking(line, speaker string) string {
	speaker = strings.TrimSpace(speaker)
	if speaker == "" || !strings.Contains(line, speaker) {
		return line
	}
	// Prefer attaching 说话 after the first speaker mention in the lens.
	key := "镜头："
	loc := strings.Index(line, key)
	if loc < 0 {
		key = "镜头:"
		loc = strings.Index(line, key)
		if loc < 0 {
			return line
		}
	}
	head := line[:loc+len(key)]
	rest := line[loc+len(key):]
	quoteAt := strings.Index(rest, "「")
	lensPart := rest
	tail := ""
	if quoteAt >= 0 {
		lensPart = rest[:quoteAt]
		tail = rest[quoteAt:]
	}
	idx := strings.Index(lensPart, speaker)
	if idx < 0 {
		return insertSpeakerSpeaking(line, speaker)
	}
	after := lensPart[idx+len(speaker):]
	if strings.HasPrefix(strings.TrimLeft(after, " (（"), "说话") || strings.HasPrefix(strings.TrimLeft(after, " (（"), "开口") {
		return line
	}
	// Insert after name / optional (grid) token.
	insertAt := idx + len(speaker)
	if paren := strings.Index(after, ")"); paren >= 0 && paren <= 12 {
		insertAt += paren + 1
	} else if paren := strings.Index(after, "）"); paren >= 0 && paren <= 12 {
		insertAt += paren + len("）")
	}
	lensPart = lensPart[:insertAt] + "说话" + lensPart[insertAt:]
	return head + lensPart + tail
}

func lensSpeakerHint(line string) string {
	loc := strings.Index(line, "镜头：")
	if loc < 0 {
		return ""
	}
	rest := line[loc+len("镜头："):]
	if q := strings.Index(rest, "「"); q >= 0 {
		rest = rest[:q]
	}
	rest = strings.Split(rest, "；")[0]
	rest = strings.TrimSpace(rest)
	runes := []rune(rest)
	if len(runes) > 8 {
		runes = runes[:8]
	}
	return strings.Trim(string(runes), "，。；、 ")
}

func removeFirstBeat(script string) string {
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		if strings.Contains(line, "秒】") {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return script
}

func dropOverlappingCharacterRefs(shot *ShotContext) {
	collapseCharacterIdentityRefs(shot, nil, 0)
}

func collapseCharacterIdentityRefs(shot *ShotContext, assets []AssetItem, keepID uint) {
	if shot == nil || len(shot.Refs) == 0 {
		return
	}
	groups := map[string][]ShotRefInfo{}
	order := make([]string, 0)
	other := make([]ShotRefInfo, 0, len(shot.Refs))
	for _, r := range shot.Refs {
		if r.Kind != "character" {
			other = append(other, r)
			continue
		}
		key := characterIdentityKey(r)
		if key == "" {
			key = fmt.Sprintf("#%d", r.ResourceID)
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], r)
	}
	out := append([]ShotRefInfo{}, other...)
	for _, key := range order {
		cands := groups[key]
		if len(cands) == 1 {
			out = append(out, cands[0])
			continue
		}
		out = append(out, pickCharacterRef(cands, shot.Script, assets, keepID))
	}
	shot.Refs = out
}

func pickCharacterRef(cands []ShotRefInfo, script string, assets []AssetItem, keepID uint) ShotRefInfo {
	if keepID > 0 {
		for _, r := range cands {
			if r.ResourceID == keepID {
				return r
			}
		}
	}
	puttingOn := puttingOnRE.MatchString(script)
	shirtless := shirtlessRE.MatchString(script)
	if puttingOn && !shirtless {
		for _, r := range cands {
			if !isDerivedRef(r) && !isShirtlessRef(r) {
				return r
			}
		}
		if parent := parentAssetFor(cands[0], assets); parent != nil {
			return refFromAsset(*parent)
		}
	}
	if shirtless && !puttingOn {
		for _, r := range cands {
			if isShirtlessRef(r) {
				return r
			}
		}
		for _, r := range cands {
			if isDerivedRef(r) {
				return r
			}
		}
	}
	for _, r := range cands {
		if isDerivedRef(r) {
			return r
		}
	}
	return cands[0]
}

func isDerivedRef(r ShotRefInfo) bool {
	return r.IsDerivative || r.ParentID > 0 || strings.TrimSpace(r.ParentName) != ""
}

func isOverlapR1(issue QCIssue) bool {
	blob := issue.Message + issue.Suggestion
	if strings.Contains(blob, "日常图和换装") || strings.Contains(blob, "同时绑了") {
		return true
	}
	if strings.Contains(blob, "重复") && (strings.Contains(blob, "衍生") || strings.Contains(blob, "绑定")) {
		return true
	}
	if strings.Contains(blob, "同镜") && strings.Contains(blob, "衍生") {
		return true
	}
	if strings.Contains(blob, "两个") && (strings.Contains(blob, "资源") || strings.Contains(blob, "refs") || strings.Contains(blob, "参考")) {
		return true
	}
	return keepResourceIDFromIssue(issue) > 0 || dropResourceIDFromIssue(issue) > 0
}

var (
	keepResourceIDRE = regexp.MustCompile(`(?:保留|只留|留下|改用|改绑)[^\d]{0,16}(\d{2,})`)
	dropResourceIDRE = regexp.MustCompile(`(?:删除|去掉|不要|解绑)[^\d]{0,16}(\d{2,})`)
)

func keepResourceIDFromIssue(issue QCIssue) uint {
	return parseFirstID(keepResourceIDRE, issue.Suggestion+" "+issue.Message)
}

func dropResourceIDFromIssue(issue QCIssue) uint {
	return parseFirstID(dropResourceIDRE, issue.Suggestion+" "+issue.Message)
}

func parseFirstID(re *regexp.Regexp, text string) uint {
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	var id uint
	fmt.Sscanf(m[1], "%d", &id)
	return id
}

func filterRefsExcept(refs []ShotRefInfo, dropID uint) []ShotRefInfo {
	if dropID == 0 {
		return refs
	}
	out := make([]ShotRefInfo, 0, len(refs))
	for _, r := range refs {
		if r.ResourceID == dropID {
			continue
		}
		out = append(out, r)
	}
	return out
}

func bindNamedProp(shot *ShotContext, assets []AssetItem, keywords ...string) {
	for _, a := range assets {
		if normalizeAssetType(a.Type) != "prop" || a.ResourceID == 0 {
			continue
		}
		blob := a.Name + a.Description + a.Prompt
		for _, kw := range keywords {
			if strings.Contains(blob, kw) {
				addRef(shot, refFromAsset(a))
				return
			}
		}
	}
}

func replaceShirtlessWithParent(shot *ShotContext, assets []AssetItem) {
	next := make([]ShotRefInfo, 0, len(shot.Refs))
	seen := map[uint]bool{}
	for _, r := range shot.Refs {
		if r.Kind == "character" && isShirtlessRef(r) {
			parent := parentAssetFor(r, assets)
			if parent != nil && !seen[parent.ResourceID] {
				next = append(next, refFromAsset(*parent))
				seen[parent.ResourceID] = true
			} else if r.ParentID > 0 && !seen[r.ParentID] {
				next = append(next, ShotRefInfo{
					Kind:       "character",
					Name:       firstNonEmpty(r.ParentName, r.Name),
					ResourceID: r.ParentID,
				})
				seen[r.ParentID] = true
			}
			continue
		}
		if r.ResourceID > 0 {
			seen[r.ResourceID] = true
		}
		next = append(next, r)
	}
	shot.Refs = next
}

func replaceParentWithShirtless(shot *ShotContext, assets []AssetItem) {
	next := make([]ShotRefInfo, 0, len(shot.Refs)+1)
	seen := map[uint]bool{}
	replaced := false
	for _, r := range shot.Refs {
		if r.Kind == "character" && !r.IsDerivative && r.ParentID == 0 {
			if child := shirtlessChildFor(r, assets); child != nil {
				if !seen[child.ResourceID] {
					next = append(next, refFromAsset(*child))
					seen[child.ResourceID] = true
				}
				replaced = true
				continue
			}
		}
		if r.ResourceID > 0 {
			seen[r.ResourceID] = true
		}
		next = append(next, r)
	}
	if !replaced {
		if child := firstShirtlessAsset(assets); child != nil && !seen[child.ResourceID] {
			next = append(next, refFromAsset(*child))
		}
	}
	shot.Refs = next
}

func unifyBGMAcrossShots(shots []ShotContext) {
	bed, last := "", ""
	for _, shot := range shots {
		phrases := scriptBGMPhrases(shot.Script)
		if len(phrases) > 0 {
			bed = phrases[0]
			break
		}
	}
	if bed == "" {
		bed = "低沉鼓点"
	}
	bedFam := bgmFamily(bed)
	last = bed
	for i := range shots {
		phrases := scriptBGMPhrases(shots[i].Script)
		if len(phrases) == 0 {
			shots[i].Script = prependBGMToAllBeats(shots[i].Script, last)
			continue
		}
		fam := bgmFamily(phrases[0])
		if !bgmFamiliesCompatible(bedFam, fam) {
			shots[i].Script = replaceBGMPhrases(shots[i].Script, last)
			continue
		}
		last = phrases[0]
	}
}

// prependBGMToAllBeats adds the bed to every beat line, not only the first 音效.
func prependBGMToAllBeats(script, bed string) string {
	script = strings.TrimSpace(script)
	if script == "" {
		return script
	}
	lines := strings.Split(script, "\n")
	changed := false
	for i, line := range lines {
		if !strings.Contains(line, "秒】") || beatIsPlotless(line) {
			continue
		}
		if strings.Contains(line, "音效：") || strings.Contains(line, "音效:") {
			if strings.Contains(scriptBGMInLine(line), bed) {
				continue
			}
			newLine := prependBGM(line, bed)
			if newLine != line {
				lines[i] = newLine
				changed = true
			}
			continue
		}
		lines[i] = strings.TrimRight(line, "； ") + "；音效：" + bed
		changed = true
	}
	if !changed {
		return prependBGM(script, bed)
	}
	return strings.Join(lines, "\n")
}

func scriptBGMInLine(line string) string {
	phrases := scriptBGMPhrases(line)
	if len(phrases) == 0 {
		return ""
	}
	return phrases[0]
}

func prependBGM(script, bed string) string {
	bed = strings.TrimSpace(bed)
	if bed == "" {
		return script
	}
	replaced := false
	out := sfxFieldRE.ReplaceAllStringFunc(script, func(field string) string {
		if replaced {
			return field
		}
		body := strings.TrimSpace(strings.TrimPrefix(field, "音效："))
		replaced = true
		if body == "" {
			return "音效：" + bed
		}
		return "音效：" + bed + "、" + body
	})
	if !replaced {
		if strings.TrimSpace(out) == "" {
			return "音效：" + bed
		}
		return strings.TrimRight(out, "\n") + "\n音效：" + bed
	}
	return out
}

func replaceBGMPhrases(script, bed string) string {
	bed = strings.TrimSpace(bed)
	if bed == "" {
		return script
	}
	return sfxFieldRE.ReplaceAllStringFunc(script, func(field string) string {
		body := strings.TrimPrefix(field, "音效：")
		parts := sfxSplitRE.Split(body, -1)
		kept := make([]string, 0, len(parts)+1)
		replaced := false
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if bgmRE.MatchString(p) {
				if !replaced {
					kept = append(kept, bed)
					replaced = true
				}
				continue
			}
			kept = append(kept, p)
		}
		if !replaced {
			kept = append([]string{bed}, kept...)
		}
		return "音效：" + strings.Join(kept, "、")
	})
}

func stripAppearanceFromScript(script string) string {
	out := appearanceRE.ReplaceAllString(script, "")
	out = wearingLookRE.ReplaceAllString(out, "")
	return cleanupScript(out)
}

func stripShirtlessPhrases(script string) string {
	out := shirtlessLeadRE.ReplaceAllString(script, "")
	out = shirtlessRE.ReplaceAllString(out, "")
	return cleanupScript(out)
}

func ensureLensLines(script string) string {
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if !strings.Contains(trim, "秒】") || strings.Contains(trim, "镜头") {
			continue
		}
		loc := beatHeaderRE.FindStringIndex(trim)
		if loc == nil {
			lines[i] = "镜头：" + trim
			continue
		}
		head, rest := trim[:loc[1]], strings.TrimSpace(trim[loc[1]:])
		lines[i] = head + "镜头：" + rest
	}
	return strings.Join(lines, "\n")
}

func cleanupScript(script string) string {
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		line = danglingDeRE.ReplaceAllString(line, "的")
		line = extraPunctRE.ReplaceAllString(line, "；")
		line = strings.ReplaceAll(line, "；；", "；")
		line = strings.ReplaceAll(line, "：；", "：")
		line = strings.ReplaceAll(line, "；。", "。")
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "；、，")
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func addRef(shot *ShotContext, ref ShotRefInfo) {
	if ref.ResourceID == 0 {
		return
	}
	for _, existing := range shot.Refs {
		if existing.ResourceID == ref.ResourceID {
			return
		}
		if ref.Kind == "character" && existing.Kind == "character" && characterIdentityKey(existing) == characterIdentityKey(ref) {
			return
		}
		if existing.Kind == ref.Kind && refNamesOverlap(existing, ref) {
			return
		}
	}
	if len(shot.Refs) >= maxShotFixRefs {
		return
	}
	shot.Refs = append(shot.Refs, ref)
}

func refNamesOverlap(a, b ShotRefInfo) bool {
	return compactNamesOverlap(
		firstNonEmpty(a.DisplayName, a.Name),
		firstNonEmpty(b.DisplayName, b.Name),
	)
}

func compactNamesOverlap(a, b string) bool {
	a, b = compactRefName(a), compactRefName(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	ar, br := []rune(a), []rune(b)
	shorter, longer := a, b
	if len(ar) > len(br) {
		shorter, longer = b, a
		ar = br
	}
	if len(ar) >= 2 && strings.Contains(longer, shorter) {
		return true
	}
	if len(ar) >= 4 {
		for i := 0; i <= len(ar)-4; i++ {
			if strings.Contains(longer, string(ar[i:i+4])) {
				return true
			}
		}
	}
	for _, tok := range []string{"绷带", "奖牌", "奖章", "奖杯", "水桶", "冰桶", "更衣室"} {
		if strings.Contains(a, tok) && strings.Contains(b, tok) {
			return true
		}
	}
	return false
}

func compactRefName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	repl := []string{" ", "", "·", "", "（", "", "）", "", "(", "", ")", ""}
	for i := 0; i+1 < len(repl); i += 2 {
		s = strings.ReplaceAll(s, repl[i], repl[i+1])
	}
	return s
}

func refFromAsset(a AssetItem) ShotRefInfo {
	kind := normalizeAssetType(a.Type)
	if kind == "" {
		kind = "character"
	}
	return ShotRefInfo{
		Kind:         kind,
		Name:         strings.TrimSpace(a.Name),
		DisplayName:  AssetDisplayName(a),
		ParentName:   strings.TrimSpace(a.ParentName),
		ParentID:     a.ParentID,
		ResourceID:   a.ResourceID,
		IsDerivative: a.IsDerivative || a.ParentID > 0,
	}
}

func assetByResourceID(assets []AssetItem, id uint) *AssetItem {
	if id == 0 {
		return nil
	}
	for i := range assets {
		if assets[i].ResourceID == id {
			return &assets[i]
		}
	}
	return nil
}

func assetByName(assets []AssetItem, name string) *AssetItem {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	for i := range assets {
		if AssetNameMatches(assets[i], name) && assets[i].ResourceID > 0 {
			return &assets[i]
		}
	}
	return nil
}

func pickSceneAsset(script string, assets []AssetItem) *AssetItem {
	names := make([]string, 0, len(assets))
	scenes := make([]AssetItem, 0)
	for _, a := range assets {
		if strings.TrimSpace(a.Name) != "" {
			names = append(names, a.Name)
		}
		if normalizeAssetType(a.Type) == "scene" && a.ResourceID > 0 {
			scenes = append(scenes, a)
		}
	}
	for i := range scenes {
		if sceneTokenMentioned(script, scenes[i].Name, names) {
			return &scenes[i]
		}
	}
	if len(scenes) > 0 {
		return &scenes[0]
	}
	return nil
}

func parentAssetFor(child ShotRefInfo, assets []AssetItem) *AssetItem {
	if child.ParentID > 0 {
		if a := assetByResourceID(assets, child.ParentID); a != nil {
			return a
		}
	}
	key := strings.ToLower(strings.TrimSpace(child.ParentName))
	if key == "" {
		key = characterIdentityKey(child)
	}
	for i := range assets {
		a := assets[i]
		if normalizeAssetType(a.Type) != "character" || a.ResourceID == 0 {
			continue
		}
		if a.IsDerivative || a.ParentID > 0 {
			continue
		}
		if strings.ToLower(strings.TrimSpace(a.Name)) == key {
			return &assets[i]
		}
	}
	return nil
}

func shirtlessChildFor(parent ShotRefInfo, assets []AssetItem) *AssetItem {
	for i := range assets {
		a := assets[i]
		if normalizeAssetType(a.Type) != "character" || a.ResourceID == 0 {
			continue
		}
		if !a.IsDerivative && a.ParentID == 0 {
			continue
		}
		sameParent := a.ParentID == parent.ResourceID || strings.EqualFold(strings.TrimSpace(a.ParentName), strings.TrimSpace(parent.Name))
		if !sameParent {
			continue
		}
		if shirtlessRE.MatchString(a.Name) || shirtlessRE.MatchString(AssetDisplayName(a)) {
			return &assets[i]
		}
	}
	return nil
}

func firstShirtlessAsset(assets []AssetItem) *AssetItem {
	for i := range assets {
		a := assets[i]
		if normalizeAssetType(a.Type) != "character" {
			continue
		}
		if shirtlessRE.MatchString(a.Name) || shirtlessRE.MatchString(AssetDisplayName(a)) {
			return &assets[i]
		}
	}
	return nil
}

func isShirtlessRef(r ShotRefInfo) bool {
	return shirtlessRE.MatchString(r.Name) || shirtlessRE.MatchString(r.DisplayName)
}

func hasKind(refs []ShotRefInfo, kind string) bool {
	for _, r := range refs {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

func filterRefsByKind(refs []ShotRefInfo, kind string, keep bool) []ShotRefInfo {
	out := make([]ShotRefInfo, 0, len(refs))
	for _, r := range refs {
		if (r.Kind == kind) == keep {
			out = append(out, r)
		}
	}
	return out
}

func quotedNames(text string) []string {
	matches := quotedNameRE.FindAllStringSubmatch(text, -1)
	out := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func shotIndexForIssue(shots []ShotContext, index map[uint]int, issue QCIssue) (int, bool) {
	if issue.ShotID > 0 {
		if i, ok := index[issue.ShotID]; ok {
			return i, true
		}
	}
	if issue.ShotIndex > 0 && issue.ShotIndex <= len(shots) {
		return issue.ShotIndex - 1, true
	}
	return 0, false
}

func cloneShotContexts(shots []ShotContext) []ShotContext {
	out := make([]ShotContext, len(shots))
	for i, shot := range shots {
		out[i] = shot
		if shot.Refs != nil {
			out[i].Refs = append([]ShotRefInfo{}, shot.Refs...)
		}
	}
	return out
}

const maxShotCaptionRunes = 24

func syncShotCaptions(shots []ShotContext, assets []AssetItem) {
	catalog := collectSceneCatalog(shots, assets)
	characters := collectCharacterNames(shots, assets)
	for i := range shots {
		oldLabel := strings.TrimSpace(shots[i].Label)
		oldNote := strings.TrimSpace(shots[i].Note)
		next := composeShotLabel(shots[i])
		if next == "" {
			continue
		}
		if captionStale(oldLabel, shots[i], catalog, characters) {
			shots[i].Label = next
		}
		if oldNote == "" {
			continue
		}
		if oldNote == oldLabel || utf8.RuneCountInString(oldNote) <= maxShotCaptionRunes && captionStale(oldNote, shots[i], catalog, characters) {
			shots[i].Note = shots[i].Label
			continue
		}
		if captionStale(oldNote, shots[i], catalog, characters) {
			shots[i].Note = replaceCaptionScene(oldNote, shots[i], catalog)
		}
	}
}

func captionStale(text string, shot ShotContext, catalog, characters []string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	bound := boundSceneNames(shot)
	mentioned := mentionedSceneNames(text, catalog)
	if len(mentioned) > 0 && !sceneNamesOverlap(bound, mentioned) {
		return true
	}
	boundChars := map[string]bool{}
	for _, r := range shot.Refs {
		if name := characterCaptionName(r); name != "" {
			boundChars[name] = true
		}
	}
	for _, name := range characters {
		if !standaloneMention(text, name, characters) {
			continue
		}
		if !boundChars[name] {
			return true
		}
	}
	return false
}

func composeShotLabel(shot ShotContext) string {
	parts := make([]string, 0, 4)
	if scene := shortSceneCaption(shot); scene != "" {
		parts = append(parts, scene)
	}
	seen := map[string]bool{}
	for _, r := range shot.Refs {
		name := characterCaptionName(r)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		parts = append(parts, name)
	}
	label := strings.Join(parts, "")
	return clipCaption(label, maxShotCaptionRunes)
}

func shortSceneCaption(shot ShotContext) string {
	names := boundSceneNames(shot)
	if len(names) == 0 {
		return ""
	}
	name := names[0]
	toks := uniqueSceneTokens(name, names)
	if len(toks) > 0 {
		return toks[0]
	}
	runes := []rune(name)
	if len(runes) > 6 {
		return string(runes[len(runes)-6:])
	}
	return name
}

func characterCaptionName(r ShotRefInfo) string {
	if r.Kind != "character" || captionSkipRef(r) {
		return ""
	}
	name := firstNonEmpty(r.ParentName, r.DisplayName, r.Name)
	if i := strings.Index(name, "（"); i > 0 {
		name = name[:i]
	}
	if i := strings.Index(name, "("); i > 0 {
		name = name[:i]
	}
	if i := strings.Index(name, "·"); i > 0 {
		name = strings.TrimSpace(name[:i])
	}
	name = strings.TrimSpace(name)
	if captionSkipName(name) {
		return ""
	}
	return name
}

func captionSkipRef(r ShotRefInfo) bool {
	return captionSkipName(r.Name) || captionSkipName(r.DisplayName)
}

func captionSkipName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	return strings.Contains(name, "站位") || strings.Contains(name, "尾帧") ||
		strings.Contains(name, "9帧") || strings.Contains(name, "9宫格") ||
		strings.Contains(name, "收势")
}

func collectCharacterNames(shots []ShotContext, assets []AssetItem) []string {
	out := make([]string, 0)
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || captionSkipName(name) || len([]rune(name)) < 2 {
			return
		}
		if i := strings.Index(name, "（"); i > 0 {
			name = strings.TrimSpace(name[:i])
		}
		if i := strings.Index(name, "·"); i > 0 {
			name = strings.TrimSpace(name[:i])
		}
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, a := range assets {
		if normalizeAssetType(a.Type) != "character" {
			continue
		}
		add(a.ParentName)
		add(a.Name)
	}
	for _, shot := range shots {
		for _, r := range shot.Refs {
			if r.Kind != "character" {
				continue
			}
			add(characterCaptionName(r))
			add(r.ParentName)
			add(r.DisplayName)
			add(r.Name)
		}
	}
	return out
}

func replaceCaptionScene(text string, shot ShotContext, catalog []string) string {
	next := shortSceneCaption(shot)
	if next == "" {
		return text
	}
	bound := boundSceneNames(shot)
	for _, name := range catalog {
		if sceneNamesOverlap(bound, []string{name}) {
			continue
		}
		if !sceneTokenMentioned(text, name, catalog) {
			continue
		}
		replaced := false
		for _, tok := range uniqueSceneTokens(name, catalog) {
			if strings.Contains(text, tok) {
				text = strings.ReplaceAll(text, tok, next)
				replaced = true
				break
			}
		}
		if !replaced && strings.Contains(text, name) {
			text = strings.ReplaceAll(text, name, next)
		}
	}
	return strings.TrimSpace(text)
}

func clipCaption(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
