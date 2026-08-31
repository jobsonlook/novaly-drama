package crew

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"regexp"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"
)

func ReviewQuality(ark *services.ArkService, provider models.AIProvider, model models.AIModel, script string, assets []AssetItem, shots []ShotContext) (QCReport, error) {
	return ReviewQualityAgainst(ark, provider, model, script, assets, shots, nil)
}

func ReviewQualityAgainst(ark *services.ArkService, provider models.AIProvider, model models.AIModel, script string, assets []AssetItem, shots []ShotContext, known []QCIssue) (QCReport, error) {
	if services.ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return QCReport{}, fmt.Errorf("文本模型服务商未配置 API Key")
	}

	payload, _ := json.Marshal(map[string]any{
		"script": clipRunes(script, 12000),
		"assets": assets,
		"shots":  shots,
	})

	scope := ""
	if len(known) > 0 {
		lines := make([]string, 0, len(known))
		for i, issue := range known {
			lines = append(lines, fmt.Sprintf("%d. [%s] 分镜%d id=%d：%s", i+1, firstNonEmpty(issue.Code, "QC"), issue.ShotIndex, issue.ShotID, issue.Message))
		}
		scope = `
复检约束：只判断下列已有问题是否还在。不要新增未列出的镜头、不要另开新规则。修好的项不要写入 issues。
` + strings.Join(lines, "\n") + "\n"
	}

	prompt := `你是质检 Agent（监制），对齐 Toonflow 监督层：只审核、不修改。对照剧本与分镜，找出不能拍/会穿帮的问题。用户确认后由执行层改稿。
` + scope + `
红线（命中即为 high）：
R1 资产引用合法：文案里能认出来的焦点角色/场景/道具，若资产名单已有，必须出现在该镜 refs。文案点名的具名角色都应挂参考图，不因人数裁掉。群演/杀手甲乙用站位图，不要求每人一张角色图。同一父角色禁止日常图和换装衍生同镜。有场景资产时每镜都要绑场景图。文案地点必须对上绑定的场景图（更衣室文案不要绑会所包厢）。资产名单没有的角色不要当问题（缺资产不审核、不建议新增）。
R2 台词忠实与时长：分镜「」内台词须对照剧本原句与说话人；允许删旁白，不允许改人物说过的话、不改派说话人、不新增剧本没有的情节。例：剧本韩铮说「舌头？你也穿越了？」绝不能写成姚三刀说。同一句「」禁止出现在两镜。按 4 字/秒计时（不计标点），3 秒拍大约 12 字；单拍超过 20 字必须拆到下一拍并换景别。空「」、未写说话人（应写成 阿彪说：「……」）也是 R2。口播「X说：」那一拍镜头必须让 X 进画并开口，禁止台词是 A、画面只写 B 盯 C（会导致视频模型对错口型）；内心独白除外。超时说不完会导致视频模型整句不念。
R3 上下文因果、人物目标与连续性：每句台词、每个动作须承接前一拍的信息，并服务于人物当下可辨认的目标（如试探、逼问、说服、隐瞒、拒绝、确认、争取、保护），其结果还应给下一拍留下回应点。只有「开口、听者反应、停顿、看向对方、陷入沉思」而无法判断人物想得到什么，或动作与前后文无因果关系，均报 R3；建议须指出应承接的具体信息和应呈现的目标，不得编造新剧情。相邻镜时空/道具也不得无过渡突变。服装动作单向可接：赤膊→接衬衫/扣扣子可以；已经穿衣/扣扣子后不能退回赤膊或改成脱衣服。
R4 父子外观匹配：衍生态（赤膊/战损/夜景）须绑衍生图；穿衣/扣衬衫须绑日常父角色。同一角色日常图与换装图的脖子配饰必须一致（奖牌/项链不能一张有一张没有）。
R5 配乐要衔接：音效必须带配乐床（鼓点/弦乐垫等），后期不加。整集同一曲风/节奏型，相邻镜不要换乐器。允许随情绪改强弱（低鼓点→紧张鼓点）。不要建议删配乐。
R6 外观交给参考图：不要写「身着蓝衬衫、高束发」这种固有造型；只写动作/表情/当下状态（汗湿、青筋、接过衬衫）。
R7 奖牌/奖杯是道具：不要画进人物脖子当项链。文案「看奖牌」须绑道具参考图；角色设定里若写了奖牌，各镜成片会时有时无。
R8 单镜不超过所选秒数：默认 10 秒。禁止【10-13秒】这类超出上限的行，多出的拍必须放到下一镜。
另查（medium/low）：心理旁白须转成可见动作或「」台词；环境/动作声要具体到声源；在场人物未离场应有视觉痕迹；相邻镜景别尽量错开；分镜文案同屏具名超过 5 人应拆镜（选参考图不裁人）；单人和多人镜的每个【秒】行在人物首次出现时都须写 人名(左前)3/4正面朝右 这种九格站位+朝向，同场相邻拍禁止无走位跳轴。只有空镜、纯物件、纯手部或脸部极特写可省略；人物极特写必须注明承接上一拍人物位置不变。

只输出 JSON：
{"score":"A|B|C|D","summary":"一句话总评","issues":[{"severity":"high|medium|low","code":"R1","shotId":0,"shotIndex":1,"resourceId":0,"message":"具体问题","suggestion":"可执行建议"}]}

输出约束：只输出这一个 JSON 对象，不要 Markdown、不要解释。issues 最多 12 条。message 和 suggestion 各不超过 80 字，字符串里不要用英文双引号，改用「」。

评分：A 无严重且中等≤2；B 无严重且中等≤5；C 有 1-2 个严重；D 严重≥3。
通过的项不要写入 issues。shotId 必须来自输入 shots；对不上就填 0 并用 shotIndex（从 1 计）。
不要建议新增基础资产名单之外的新角色/场景。

审核材料：
` + string(payload)
	prompt = services.ApplyDramaSkillGuidance(prompt, "review")

	content, err := ark.Chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.1,
		"max_tokens":  8192,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return QCReport{}, err
	}
	var report QCReport
	if err := unmarshalObject(content, &report); err != nil {
		log.Printf("qc report json failed: %v", err)
		report = QCReport{Summary: "模型报告格式异常，已改用规则质检。"}
	}
	report.Score = strings.ToUpper(strings.TrimSpace(report.Score))
	if report.Issues == nil {
		report.Issues = []QCIssue{}
	}
	for i := range report.Issues {
		sev := strings.ToLower(strings.TrimSpace(report.Issues[i].Severity))
		if sev != "high" && sev != "medium" && sev != "low" {
			report.Issues[i].Severity = "medium"
		} else {
			report.Issues[i].Severity = sev
		}
		report.Issues[i].Message = strings.TrimSpace(report.Issues[i].Message)
		report.Issues[i].Suggestion = strings.TrimSpace(report.Issues[i].Suggestion)
		report.Issues[i].Code = strings.TrimSpace(report.Issues[i].Code)
	}
	llm := report.Issues
	det := detectDeterministicQC(shots, assets, script)
	report.Issues = dedupeQCIssues(append(det, dropLLMGhostIssues(llm, det)...))
	report.Score = scoreQCIssues(report.Issues)
	report.Summary = summarizeQCIssues(report.Issues, len(known) > 0)
	return report, nil
}

var (
	shirtlessRE       = regexp.MustCompile(`赤膊|裸上身|没穿上衣|未穿上衣|不穿上衣|光着上身|不着上衣`)
	puttingOnRE       = regexp.MustCompile(`穿上|套上|接衬衫|接过衬衫|扣衬衫|扣扣子|披上|穿衣`)
	takingOffRE       = regexp.MustCompile(`脱下上衣|脱掉上衣|脱衣服|扯开衣|解开衬衫|扒开衣`)
	bgmRE             = regexp.MustCompile(`(?i)BGM|配乐|背景音乐|主题曲|插曲|氛围音乐|旋律烘托|乐器烘托|情绪音乐|氛围鼓|(?:低|紧|紧张|沉|缓|快)?鼓点`)
	appearanceRE      = regexp.MustCompile(`身着|一袭[^，。；\n]{0,8}(袍|衫|裙|甲)|高束发|浓妆|金线绣`)
	wearingLookRE     = regexp.MustCompile(`穿着[^，。；\n]{0,10}(长袍|战甲|礼服|校服|西装|旗袍)`)
	dialogueRE        = regexp.MustCompile(`「([^」]*)」`)
	speakerPrefixRE   = regexp.MustCompile(`(?:说(?:道|了)?|道|喊|问)\s*[：:]?\s*$`)
	nameColonPrefixRE = regexp.MustCompile(`[\p{Han}A-Za-z0-9甲乙丙丁]{1,12}[：:]\s*$`)
	beatRangeRE       = regexp.MustCompile(`【(\d+)\s*[-–—~到至]\s*(\d+)\s*秒】`)
	lookAtPropRE      = regexp.MustCompile(`看(?:向|着|着那)?[^，。；\n]{0,6}(?:奖牌|奖杯|奖章)|(?:奖牌|奖杯)冷光|(?:奖牌|奖杯)特写`)
	neckPropRE        = regexp.MustCompile(`奖牌|奖章|项链|绶带`)
	noNeckPropRE      = regexp.MustCompile(`无(?:任何)?(?:颈部|脖子)?配饰|不戴(?:任何)?(?:奖牌|奖章|项链|绶带)|(?:颈部|脖子)(?:保持)?(?:干净|无配饰)|没有(?:佩戴)?(?:奖牌|奖章|项链|绶带)`)
	metaSpeakerRE     = regexp.MustCompile(`第\s*\d+\s*集(?:说|内心独白|道|问|喊)`)
	literalNewlineRE  = regexp.MustCompile(`\\n`)
)

func detectDeterministicQC(shots []ShotContext, assets []AssetItem, script string) []QCIssue {
	out := detectRefAndCostumeIssues(shots)
	out = append(out, detectMissingAssetRefs(shots, assets)...)
	out = append(out, detectAudioBeatAndLookIssues(shots)...)
	out = append(out, detectBGMContinuity(shots)...)
	out = append(out, detectAccessoryContinuity(shots, assets)...)
	out = append(out, detectDurationOverflow(shots)...)
	out = append(out, detectDuplicateDialogue(shots)...)
	out = append(out, detectMisattributedSpeakers(shots, script)...)
	out = append(out, detectOffscreenSpokenSpeakers(shots)...)
	out = append(out, detectSceneRefMismatch(shots, assets)...)
	out = append(out, detectCrowdCharacterRefs(shots)...)
	out = append(out, detectMissingSpatialBlocking(shots)...)
	out = append(out, detectAmbiguousInteractionTargets(shots)...)
	out = append(out, detectNarrativeGoalLeak(shots)...)
	out = append(out, detectScriptFormatIssuesAgainst(shots, script)...)
	out = append(out, detectMissingScriptDialogue(shots, script)...)
	return out
}

var narrativeGoalLeakRE = regexp.MustCompile(`意图(?:是|为|在于|点破|制造|营造|突出|强化)?|打脸爽感|爽感|逗趣师傅|接受、质疑或拒绝|目标[：:]`)
var ambiguousInteractionTargetRE = regexp.MustCompile(`听者|对方|对面(?:的人|角色)?|另一(?:人|角色)|其(?:眼神|反应|态度|回应)`)

// detectAmbiguousInteractionTargets prevents a video model from having to
// invent who an eyeline, reaction, or interaction is aimed at. Spoken dialogue
// is excluded because a character may legitimately say “对方” in a line.
func detectAmbiguousInteractionTargets(shots []ShotContext) []QCIssue {
	out := make([]QCIssue, 0)
	for i, shot := range shots {
		if !ambiguousInteractionTargetRE.MatchString(stripQuotedDialogue(shot.Script)) {
			continue
		}
		out = append(out, QCIssue{
			Severity:   "high",
			Code:       "R3",
			ShotID:     shot.ID,
			ShotIndex:  shotIndexOf(shot, i),
			Message:    "动作使用了“听者/对方/对面的人”等模糊指代，视频模型无法确定互动对象",
			Suggestion: "把模糊指代改成当前镜中具体角色姓名，并写明该角色的九格站位与朝向；例如“洛清霜盯住韩铮的眼睛”。",
		})
	}
	return out
}

// detectNarrativeGoalLeak rejects writers-room analysis accidentally emitted as
// a visible action. Goals must be dramatized through concrete behavior instead.
func detectNarrativeGoalLeak(shots []ShotContext) []QCIssue {
	out := make([]QCIssue, 0)
	for i, shot := range shots {
		if !narrativeGoalLeakRE.MatchString(shot.Script) {
			continue
		}
		out = append(out, QCIssue{
			Severity:   "high",
			Code:       "R3",
			ShotID:     shot.ID,
			ShotIndex:  shotIndexOf(shot, i),
			Message:    "分镜把「意图/爽感/逗趣」等创作分析标签直接写进动作，人物目标没有被转成可拍行为",
			Suggestion: "结合前后文选定人物此刻要达成的具体目标，用视线、手势、距离变化和对方回应呈现；删除所有分析标签和备选措辞。",
		})
	}
	return out
}

var spokenShotPairRE = regexp.MustCompile(`([\p{Han}A-Za-z0-9甲乙丙丁]{1,12})(?:（[^）]{0,24}）)?说：\s*「([^」]*)」`)

func detectMisattributedSpeakers(shots []ShotContext, script string) []QCIssue {
	orig := ExtractSpokenLines(script)
	if len(orig) == 0 || len(shots) == 0 {
		return nil
	}
	out := make([]QCIssue, 0)
	flagged := 0
	for i, shot := range shots {
		for _, m := range spokenShotPairRE.FindAllStringSubmatch(shot.Script, -1) {
			if len(m) < 3 {
				continue
			}
			gotSpeaker := strings.TrimSpace(m[1])
			quote := strings.TrimSpace(m[2])
			if quoteIsEmpty(quote) || gotSpeaker == "" {
				continue
			}
			wantSpeaker, ok := matchSpokenSpeaker(orig, quote)
			if !ok || wantSpeaker == "" || wantSpeaker == gotSpeaker {
				continue
			}
			idx := shotIndexOf(shot, i)
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R2",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    fmt.Sprintf("台词改派说话人：剧本是%s说「%s」，分镜写成了%s说", wantSpeaker, clipRunes(quote, 24), gotSpeaker),
				Suggestion: fmt.Sprintf("改回 %s说：「%s」；禁止把别人的台词挂到旁边角色名下。", wantSpeaker, clipRunes(quote, 40)),
			})
			flagged++
			if flagged >= 8 {
				return out
			}
		}
	}
	return out
}

// detectOffscreenSpokenSpeakers flags lip-sync traps: dialogue attributed to A
// while the same beat's lens only stages B/C.
func detectOffscreenSpokenSpeakers(shots []ShotContext) []QCIssue {
	out := make([]QCIssue, 0)
	for i, shot := range shots {
		for _, beat := range scriptBeats(shot.Script) {
			if strings.Contains(beat, "内心独白：") && !strings.Contains(beat, "说：") {
				continue
			}
			speaker := spokenSpeakerInBeat(beat)
			if speaker == "" {
				continue
			}
			lens := lensBodyForSpeakerCheck(beat)
			if lens == "" {
				continue
			}
			if speakerVisibleInLens(lens, speaker) {
				continue
			}
			idx := shotIndexOf(shot, i)
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R2",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    fmt.Sprintf("说话人未进画面：台词是%s说，但镜头未写%s，视频模型容易对错口型", speaker, speaker),
				Suggestion: fmt.Sprintf("本拍镜头主体改成%s说话（近景/过肩看清嘴脸）；其他人只写听/反应，或另切%s专属镜。", speaker, speaker),
			})
		}
	}
	return out
}

func spokenSpeakerInBeat(beat string) string {
	// Prefer explicit 「X说：」 over 内心独白.
	if m := speakerSayRE.FindStringSubmatch(beat); len(m) >= 2 {
		name := strings.TrimSpace(m[1])
		if i := strings.Index(name, "（"); i > 0 {
			name = strings.TrimSpace(name[:i])
		}
		return name
	}
	return ""
}

func lensBodyForSpeakerCheck(beat string) string {
	loc := strings.Index(beat, "镜头：")
	if loc < 0 {
		loc = strings.Index(beat, "镜头:")
		if loc < 0 {
			return ""
		}
		loc += len("镜头:")
	} else {
		loc += len("镜头：")
	}
	rest := beat[loc:]
	for _, sep := range []string{"；音效", ";音效", "音效：", "音效:", "「"} {
		if i := strings.Index(rest, sep); i >= 0 {
			rest = rest[:i]
		}
	}
	return strings.TrimSpace(rest)
}

func speakerVisibleInLens(lens, speaker string) bool {
	speaker = strings.TrimSpace(speaker)
	if speaker == "" || lens == "" {
		return false
	}
	if strings.Contains(lens, speaker) {
		return true
	}
	// 「X说话」 prefix without full grid notation still counts.
	if strings.Contains(lens, speaker+"说话") || strings.Contains(lens, speaker+"开口") {
		return true
	}
	return false
}

func matchSpokenSpeaker(orig []SpokenLine, quote string) (string, bool) {
	bestSpeaker := ""
	bestScore := 0
	for _, line := range orig {
		score := spokenTextMatchScore(quote, line.Text)
		if score > bestScore {
			bestScore = score
			bestSpeaker = line.Speaker
		}
	}
	if bestScore < 2 {
		return "", false
	}
	return bestSpeaker, true
}

func detectDurationOverflow(shots []ShotContext) []QCIssue {
	out := make([]QCIssue, 0)
	for i, shot := range shots {
		max := ShotMaxSeconds(shot.Duration)
		end := ScriptEndSeconds(shot.Script)
		if scriptHasOverlappingBeats(shot.Script) {
			idx := shotIndexOf(shot, i)
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R8",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    "同一镜里有两拍时序重叠（例如两段都写【7-10秒】）",
				Suggestion: "后一段应接到上一拍结束之后；超出本镜秒数的拍挪到下一镜。重叠时序会让视频模型把两句台词挤进同一时间。",
			})
			continue
		}
		if end > max {
			idx := shotIndexOf(shot, i)
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R8",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    fmt.Sprintf("时序写到了 %d 秒，超过本镜 %d 秒上限", end, max),
				Suggestion: "超出的拍挪到下一镜，本镜只保留 0 到上限的时序。生成视频按本镜秒数计费，多写的拍不会被拍进成片。",
			})
			continue
		}
		if gapStart, gapEnd, hasGap := scriptTimelineGap(shot.Script); hasGap {
			idx := shotIndexOf(shot, i)
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R8",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    fmt.Sprintf("时序在 %d-%d 秒之间断档，视频有 %d 秒没有画面动作定义", gapStart, gapEnd, gapEnd-gapStart),
				Suggestion: "重新连续分配本镜时序，上一拍结束秒必须等于下一拍开始秒；缺失区间写承接上下文的目标性动作，禁止用空反应凑时长。",
			})
		}
	}
	return out
}

func detectDuplicateDialogue(shots []ShotContext) []QCIssue {
	out := make([]QCIssue, 0)
	type seenQuote struct {
		text  string
		index int
		shot  ShotContext
	}
	seen := make([]seenQuote, 0)
	for i, shot := range shots {
		idx := shotIndexOf(shot, i)
		local := make([]string, 0)
		for _, q := range quotesInScript(shot.Script) {
			if speechRunes(q) < 6 {
				continue
			}
			dupLocal := false
			for _, prev := range local {
				if quotesSubstantivelyDuplicate(q, prev) {
					out = append(out, QCIssue{
						Severity:   "high",
						Code:       "R2",
						ShotID:     shot.ID,
						ShotIndex:  idx,
						Message:    fmt.Sprintf("同一镜内重复同一句台词：「%s」", q),
						Suggestion: "删掉本镜重复的那拍，一句「」只保留一次。",
					})
					dupLocal = true
					break
				}
			}
			if dupLocal {
				continue
			}
			local = append(local, q)

			dupCross := false
			for _, prev := range seen {
				if !quotesSubstantivelyDuplicate(q, prev.text) {
					continue
				}
				prevIdx := shotIndexOf(prev.shot, prev.index)
				out = append(out, QCIssue{
					Severity:   "high",
					Code:       "R2",
					ShotID:     shot.ID,
					ShotIndex:  idx,
					Message:    fmt.Sprintf("与分镜%d重复同一句台词：「%s」", prevIdx, q),
					Suggestion: "删掉本镜这句，只保留先出现的那镜。半句碎片、省略号尾巴也算重复。",
				})
				dupCross = true
				break
			}
			if dupCross {
				continue
			}
			seen = append(seen, seenQuote{text: q, index: i, shot: shot})
		}
	}
	return out
}

func detectSceneRefMismatch(shots []ShotContext, assets []AssetItem) []QCIssue {
	catalog := collectSceneCatalog(shots, assets)
	if len(catalog) < 2 {
		return nil
	}
	out := make([]QCIssue, 0)
	for i, shot := range shots {
		bound := boundSceneNames(shot)
		if len(bound) == 0 {
			continue
		}
		mentioned := mentionedSceneNames(shot.Script, catalog)
		if len(mentioned) == 0 {
			continue
		}
		if sceneNamesOverlap(bound, mentioned) {
			continue
		}
		idx := shotIndexOf(shot, i)
		out = append(out, QCIssue{
			Severity:   "high",
			Code:       "R1",
			ShotID:     shot.ID,
			ShotIndex:  idx,
			Message:    fmt.Sprintf("文案地点是「%s」，但参考图绑的是「%s」", mentioned[0], bound[0]),
			Suggestion: "把本镜场景图换成文案所在的那张，不要把更衣室戏绑到会所包厢上。",
		})
	}
	return out
}

func collectSceneCatalog(shots []ShotContext, assets []AssetItem) []string {
	out := make([]string, 0)
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, a := range assets {
		if normalizeAssetType(a.Type) == "scene" {
			add(a.Name)
		}
	}
	for _, shot := range shots {
		for _, name := range boundSceneNames(shot) {
			add(name)
		}
	}
	return out
}

func boundSceneNames(shot ShotContext) []string {
	out := make([]string, 0)
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, r := range shot.Refs {
		if r.Kind != "scene" {
			continue
		}
		add(r.DisplayName)
		add(r.Name)
	}
	return out
}

func mentionedSceneNames(script string, catalog []string) []string {
	out := make([]string, 0)
	for _, name := range catalog {
		if sceneTokenMentioned(script, name, catalog) {
			out = append(out, name)
		}
	}
	return out
}

func sceneNamesOverlap(bound, mentioned []string) bool {
	for _, b := range bound {
		for _, m := range mentioned {
			if b == m || strings.Contains(b, m) || strings.Contains(m, b) {
				return true
			}
		}
	}
	return false
}

func sceneTokenMentioned(script, sceneName string, catalog []string) bool {
	if standaloneMention(script, sceneName, catalog) {
		return true
	}
	for _, tok := range uniqueSceneTokens(sceneName, catalog) {
		if strings.Contains(script, tok) {
			return true
		}
	}
	return false
}

func uniqueSceneTokens(name string, catalog []string) []string {
	runes := []rune(strings.TrimSpace(name))
	out := make([]string, 0)
	for n := 3; n < len(runes); n++ {
		suf := string(runes[len(runes)-n:])
		unique := true
		for _, other := range catalog {
			other = strings.TrimSpace(other)
			if other == "" || other == strings.TrimSpace(name) {
				continue
			}
			if strings.Contains(other, suf) {
				unique = false
				break
			}
		}
		if unique {
			out = append(out, suf)
		}
	}
	return out
}

func detectRefAndCostumeIssues(shots []ShotContext) []QCIssue {
	out := make([]QCIssue, 0)
	for i, shot := range shots {
		idx := shot.Index
		if idx <= 0 {
			idx = i + 1
		}
		parentSeen := map[string]ShotRefInfo{}
		childSeen := map[string][]ShotRefInfo{}
		shirtlessRef := false
		baseRef := false
		for _, r := range shot.Refs {
			if r.Kind != "character" {
				continue
			}
			key := characterIdentityKey(r)
			if r.IsDerivative || r.ParentID > 0 || strings.TrimSpace(r.ParentName) != "" {
				childSeen[key] = append(childSeen[key], r)
				if shirtlessRE.MatchString(r.Name) || shirtlessRE.MatchString(r.DisplayName) {
					shirtlessRef = true
				}
			} else {
				parentSeen[key] = r
				baseRef = true
			}
		}
		for key, kids := range childSeen {
			if len(kids) > 1 {
				ids := make([]string, 0, len(kids))
				for _, k := range kids {
					if k.ResourceID > 0 {
						ids = append(ids, fmt.Sprintf("%d", k.ResourceID))
					}
				}
				out = append(out, QCIssue{
					Severity:   "high",
					Code:       "R1",
					ShotID:     shot.ID,
					ShotIndex:  idx,
					ResourceID: kids[len(kids)-1].ResourceID,
					Message:    fmt.Sprintf("同一父角色在本镜重复绑定了 %d 张衍生图（%s）", len(kids), strings.Join(ids, "、")),
					Suggestion: fmt.Sprintf("同一镜只留一张当前外观。删除 %d，保留 %d。", kids[0].ResourceID, kids[len(kids)-1].ResourceID),
				})
			}
			child := kids[0]
			if _, ok := parentSeen[key]; !ok {
				continue
			}
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R1",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				ResourceID: child.ResourceID,
				Message:    fmt.Sprintf("同一角色同时绑了日常图和换装图（%s）", firstNonEmpty(child.DisplayName, child.Name)),
				Suggestion: "本镜只留当前外观一张。赤膊段落只留换装衍生，穿衣/扣扣子段落只留日常父角色。",
			})
		}

		shirtless := shirtlessRE.MatchString(shot.Script)
		puttingOn := puttingOnRE.MatchString(shot.Script)
		takingOff := takingOffRE.MatchString(shot.Script)
		if shirtless && takingOff {
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R3",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    "文案里已经赤膊/没穿上衣，又写脱衣服，视频会把穿脱来回演",
				Suggestion: "赤膊开场只写动作（浸冰水、接过衬衫），不要再写脱衣；穿衣过程用「接衬衫、扣扣子」。",
			})
		}
		if shirtless && puttingOn && !takingOff {
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R3",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    "同一镜先写赤膊/没穿上衣，后又接衬衫、扣扣子，视频会把穿脱演反",
				Suggestion: "赤膊外观只靠换装参考图。文案从动作写：坐着浸冰水、伸手接过衬衫、扣扣子，不要再写「赤膊」。穿衣完成后的下一镜改绑日常父角色。",
			})
		}
		if puttingOn && takingOff {
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R3",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    "同一镜又穿衣又脱衣，外观方向冲突",
				Suggestion: "一条 10 秒镜只保留一个换装方向：要么穿上，要么脱下。",
			})
		}
		if puttingOn && !takingOff && shirtlessRef && !shirtless {
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R4",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    "文案在穿衣/扣衬衫，参考图却仍是赤膊衍生",
				Suggestion: "扣扣子、穿上衬衫的镜头改绑日常「韩铮」，不要再挂「赤膊战损」。",
			})
		}
		if shirtless && baseRef && !shirtlessRef && !puttingOn {
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R4",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    "文案是赤膊/没穿上衣，却只绑了日常父角色",
				Suggestion: "赤膊段落应绑「父角色 · 赤膊战损」这一张，不要日常着装图。",
			})
		}
	}

	for i := 1; i < len(shots); i++ {
		prevEnd := lastScriptBeat(shots[i-1].Script)
		nextStart := firstScriptBeat(shots[i].Script)
		if prevEnd == "" || nextStart == "" {
			continue
		}
		idx := shots[i].Index
		if idx <= 0 {
			idx = i + 1
		}
		prevPutting := puttingOnRE.MatchString(prevEnd)
		nextShirtless := shirtlessRE.MatchString(nextStart)
		nextTakingOff := takingOffRE.MatchString(nextStart)
		prevShirtlessEnd := shirtlessRE.MatchString(prevEnd) && !puttingOnRE.MatchString(prevEnd)
		if (prevPutting || (!shirtlessRE.MatchString(prevEnd) && puttingOnRE.MatchString(shots[i-1].Script))) && nextShirtless {
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R3",
				ShotID:     shots[i].ID,
				ShotIndex:  idx,
				Message:    "上一镜已经在穿衣/扣扣子，这一镜又变成没穿上衣",
				Suggestion: "接着写扣扣子、整理衣领或起身离开，不要退回赤膊。",
			})
		}
		if prevShirtlessEnd && nextTakingOff {
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R3",
				ShotID:     shots[i].ID,
				ShotIndex:  idx,
				Message:    "上一镜已经赤膊，这一镜又写脱衣服",
				Suggestion: "赤膊之后只能维持赤膊或开始穿上，不能再脱一次。",
			})
		}
	}
	return out
}

func detectMissingAssetRefs(shots []ShotContext, assets []AssetItem) []QCIssue {
	if len(assets) == 0 {
		return nil
	}
	hasSceneAsset := false
	characters := make([]AssetItem, 0)
	props := make([]AssetItem, 0)
	allNames := make([]string, 0, len(assets))
	for _, a := range assets {
		name := strings.TrimSpace(a.Name)
		if name != "" {
			allNames = append(allNames, name)
		}
		switch normalizeAssetType(a.Type) {
		case "scene":
			hasSceneAsset = true
		case "character":
			if a.IsDerivative || a.ParentID > 0 {
				continue
			}
			characters = append(characters, a)
		case "prop":
			props = append(props, a)
		}
	}
	out := make([]QCIssue, 0)
	for i, shot := range shots {
		idx := shotIndexOf(shot, i)
		hasSceneRef := false
		bound := map[string]bool{}
		for _, r := range shot.Refs {
			if r.Kind == "scene" {
				hasSceneRef = true
			}
			if key := characterIdentityKey(r); key != "" {
				bound[key] = true
			}
			if n := strings.ToLower(strings.TrimSpace(r.Name)); n != "" {
				bound[n] = true
			}
			if n := strings.ToLower(strings.TrimSpace(r.DisplayName)); n != "" {
				bound[n] = true
			}
		}
		if hasSceneAsset && !hasSceneRef {
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R1",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    "本镜没有绑定场景参考图",
				Suggestion: "有场景资产时每镜都要挂场景图，Seedance 才能锁空间。",
			})
		}
		for _, ch := range characters {
			name := strings.TrimSpace(ch.Name)
			if !standaloneMention(stripQuotedDialogue(shot.Script), name, allNames) {
				continue
			}
			if bound[strings.ToLower(name)] {
				continue
			}
			if services.CharacterLooksLikeCrowd(name) {
				continue
			}
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R1",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				ResourceID: ch.ResourceID,
				Message:    fmt.Sprintf("文案出现「%s」，但本镜 refs 未绑定该角色", name),
				Suggestion: fmt.Sprintf("把「%s」的参考图加入本镜；换装段落绑对应衍生，不要只写名字。", name),
			})
		}
		for _, p := range props {
			name := strings.TrimSpace(p.Name)
			if !standaloneMention(shot.Script, name, allNames) {
				continue
			}
			if bound[strings.ToLower(name)] {
				continue
			}
			out = append(out, QCIssue{
				Severity:   "medium",
				Code:       "R1",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				ResourceID: p.ResourceID,
				Message:    fmt.Sprintf("文案出现道具「%s」，但本镜 refs 未绑定", name),
				Suggestion: "画面里能看清的道具应挂上对应参考图。",
			})
		}
	}
	return out
}

func stripQuotedDialogue(script string) string {
	return dialogueRE.ReplaceAllString(script, "")
}

func detectAudioBeatAndLookIssues(shots []ShotContext) []QCIssue {
	out := make([]QCIssue, 0)
	for i, shot := range shots {
		idx := shotIndexOf(shot, i)
		if appearanceRE.MatchString(shot.Script) || wearingLookRE.MatchString(shot.Script) {
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R6",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    "文案写了固有服装/发型造型，会和参考图打架",
				Suggestion: "服装发型交给参考图。文案只写动作：接过衬衫、扣扣子、皱眉，不要写「身着××」。",
			})
		}
		for _, beat := range scriptBeats(shot.Script) {
			if !strings.Contains(beat, "镜头") {
				out = append(out, QCIssue{
					Severity:   "medium",
					Code:       "R3",
					ShotID:     shot.ID,
					ShotIndex:  idx,
					Message:    "时序行缺少「镜头：景别+运镜+动作」",
					Suggestion: "每段写成【起止秒】镜头：…；音效：…；「台词」。",
				})
			}
			secs := beatSeconds(beat)
			quotes := dialogueRE.FindAllStringSubmatch(beat, -1)
			nonEmpty := 0
			for _, m := range quotes {
				if len(m) < 2 {
					continue
				}
				text := strings.TrimSpace(m[1])
				if quoteIsEmpty(text) {
					out = append(out, QCIssue{
						Severity:   "medium",
						Code:       "R2",
						ShotID:     shot.ID,
						ShotIndex:  idx,
						Message:    "出现空「」，视频模型会当成有台词却不念",
						Suggestion: "无台词就删掉「」，不要留空引号。",
					})
					continue
				}
				nonEmpty++
				if !quoteHasSpeaker(beat, m[0]) {
					out = append(out, QCIssue{
						Severity:   "medium",
						Code:       "R2",
						ShotID:     shot.ID,
						ShotIndex:  idx,
						Message:    "台词未标明说话人",
						Suggestion: "写成 阿彪说：「……」，方便视频模型对上口型和音色。",
					})
				}
				if secs <= 0 {
					continue
				}
				n := speechRunes(text)
				maxN := maxSpeechRunes(secs)
				if n > maxN {
					out = append(out, QCIssue{
						Severity:   "high",
						Code:       "R2",
						ShotID:     shot.ID,
						ShotIndex:  idx,
						Message:    fmt.Sprintf("这段约 %.0f 秒，台词 %d 字，按 4 字/秒会说不完（上限 %d 字）", secs, n, maxN),
						Suggestion: "按语义停顿拆到下一拍或下一镜并换景别；禁止精简原文。超时模型常整句不念。",
					})
				}
			}
			if nonEmpty >= 2 {
				out = append(out, QCIssue{
					Severity:   "high",
					Code:       "R2",
					ShotID:     shot.ID,
					ShotIndex:  idx,
					Message:    "两句台词挤在同一拍",
					Suggestion: "拆成两拍，每句写成 角色说：「……」。",
				})
			}
		}
	}
	return out
}

func detectBGMContinuity(shots []ShotContext) []QCIssue {
	if len(shots) == 0 {
		return nil
	}
	phrases := make([][]string, len(shots))
	families := make([]string, len(shots))
	anyBGM := false
	for i, shot := range shots {
		phrases[i] = scriptBGMPhrases(shot.Script)
		if len(phrases[i]) > 0 {
			families[i] = bgmFamily(phrases[i][0])
			anyBGM = true
		}
	}
	out := make([]QCIssue, 0)
	if !anyBGM {
		idx := shotIndexOf(shots[0], 0)
		out = append(out, QCIssue{
			Severity:   "medium",
			Code:       "R5",
			ShotID:     shots[0].ID,
			ShotIndex:  idx,
			Message:    "本集音效没有配乐床，各镜生成视频时配乐会对不上",
			Suggestion: "每镜音效都写同一条配乐床，例如「低沉鼓点」；情绪升高可写成「紧张鼓点」，不要换乐器。",
		})
		return out
	}
	lastFam, lastPhrase := "", ""
	for i, shot := range shots {
		idx := shotIndexOf(shot, i)
		if len(phrases[i]) == 0 {
			if lastFam != "" {
				out = append(out, QCIssue{
					Severity:   "medium",
					Code:       "R5",
					ShotID:     shot.ID,
					ShotIndex:  idx,
					Message:    fmt.Sprintf("上一镜配乐是「%s」，这一镜音效里断了", lastPhrase),
					Suggestion: fmt.Sprintf("本镜音效续上「%s」，可随情绪改强弱，不要换成别的乐器。", lastPhrase),
				})
			}
			continue
		}
		fam := families[i]
		if lastFam != "" && !bgmFamiliesCompatible(lastFam, fam) {
			out = append(out, QCIssue{
				Severity:   "medium",
				Code:       "R5",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    fmt.Sprintf("配乐从「%s」换成「%s」，相邻镜会接不上", lastPhrase, phrases[i][0]),
				Suggestion: fmt.Sprintf("沿用「%s」这条配乐床，只改强弱，不要换曲风。", lastPhrase),
			})
		}
		lastFam, lastPhrase = fam, phrases[i][0]
	}
	return out
}

func detectAccessoryContinuity(shots []ShotContext, assets []AssetItem) []QCIssue {
	out := make([]QCIssue, 0)
	byID := map[uint]AssetItem{}
	families := map[string][]AssetItem{}
	usedResourceIDs := map[uint]bool{}
	propMedal := false
	for _, shot := range shots {
		for _, r := range shot.Refs {
			if r.Kind == "character" && r.ResourceID > 0 {
				usedResourceIDs[r.ResourceID] = true
			}
		}
	}
	for _, a := range assets {
		if a.ResourceID > 0 {
			byID[a.ResourceID] = a
		}
		switch normalizeAssetType(a.Type) {
		case "character":
			key := characterAssetIdentity(a)
			if key != "" {
				families[key] = append(families[key], a)
			}
		case "prop":
			if neckPropRE.MatchString(a.Name) || neckPropRE.MatchString(a.Description) {
				propMedal = true
			}
		}
	}

	firstShotOf := func(resourceID uint, identity string) (uint, int) {
		for i, shot := range shots {
			for _, r := range shot.Refs {
				if r.Kind != "character" {
					continue
				}
				if resourceID > 0 && r.ResourceID == resourceID {
					return shot.ID, shotIndexOf(shot, i)
				}
				if identity != "" && characterIdentityKey(r) == identity {
					return shot.ID, shotIndexOf(shot, i)
				}
			}
		}
		if len(shots) > 0 {
			return shots[0].ID, shotIndexOf(shots[0], 0)
		}
		return 0, 1
	}

	flaggedIdentity := map[string]bool{}
	for identity, members := range families {
		var parent AssetItem
		children := make([]AssetItem, 0, len(members))
		for _, a := range members {
			if a.IsDerivative || a.ParentID > 0 {
				children = append(children, a)
			} else {
				parent = a
			}
		}
		if parent.Name == "" || len(children) == 0 {
			continue
		}
		for _, child := range children {
			// Catalog variants that are not used by the current storyboard cannot
			// create an on-screen continuity error. Also, an omitted accessory in a
			// prompt is unknown rather than proof that the character is not wearing it.
			if !usedResourceIDs[parent.ResourceID] || !usedResourceIDs[child.ResourceID] {
				continue
			}
			parentSet, parentKnown := neckPropClaim(characterLookText(parent))
			childSet, childKnown := neckPropClaim(characterLookText(child))
			if !parentKnown || !childKnown {
				continue
			}
			if sameStringSet(parentSet, childSet) {
				continue
			}
			sid, idx := firstShotOf(parent.ResourceID, identity)
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R4",
				ShotID:     sid,
				ShotIndex:  idx,
				ResourceID: parent.ResourceID,
				Message:    fmt.Sprintf("「%s」日常设定含%s，换装「%s」含%s。视频会一镜脖子有奖牌、一镜没有", parent.Name, formatPropSet(parentSet), firstNonEmpty(child.Name, "衍生"), formatPropSet(childSet)),
				Suggestion: "奖牌/奖杯做成道具图，角色设定去掉脖子上的奖牌；或让所有外观戴同一枚同款奖牌。改完后重出相关分镜视频。",
			})
			flaggedIdentity[identity] = true
			break
		}
	}

	type boundLook struct {
		shot  ShotContext
		index int
		ref   ShotRefInfo
		props map[string]bool
		known bool
	}
	byIdentity := map[string][]boundLook{}
	for i, shot := range shots {
		for _, r := range shot.Refs {
			if r.Kind != "character" {
				continue
			}
			key := characterIdentityKey(r)
			if key == "" {
				continue
			}
			text := strings.TrimSpace(r.Prompt)
			if text == "" {
				if a, ok := byID[r.ResourceID]; ok {
					text = characterLookText(a)
				}
			}
			props, known := neckPropClaim(text)
			byIdentity[key] = append(byIdentity[key], boundLook{
				shot: shot, index: shotIndexOf(shot, i), ref: r, props: props, known: known,
			})
		}
	}
	for identity, looks := range byIdentity {
		if flaggedIdentity[identity] || len(looks) < 2 {
			continue
		}
		base := looks[0]
		for _, look := range looks[1:] {
			if !base.known || !look.known || sameStringSet(base.props, look.props) {
				continue
			}
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R4",
				ShotID:     look.shot.ID,
				ShotIndex:  look.index,
				ResourceID: look.ref.ResourceID,
				Message:    fmt.Sprintf("同一角色「%s」各镜参考图配饰不一致（有的有%s，有的是%s），成片脖子会时有时无", identity, formatPropSet(base.props), formatPropSet(look.props)),
				Suggestion: "统一角色设定：奖牌不要画进人物图。看奖牌的镜头绑道具参考图，再重出视频。",
			})
			break
		}
	}

	scriptHasMedal := false
	scriptWearMedal := false
	for _, shot := range shots {
		if lookAtPropRE.MatchString(shot.Script) || strings.Contains(shot.Script, "奖牌") || strings.Contains(shot.Script, "奖杯") {
			scriptHasMedal = true
		}
		if strings.Contains(shot.Script, "戴着奖牌") || strings.Contains(shot.Script, "戴奖牌") ||
			(strings.Contains(shot.Script, "脖子上") && neckPropRE.MatchString(shot.Script)) {
			scriptWearMedal = true
		}
	}
	if scriptHasMedal && !scriptWearMedal {
		for _, a := range assets {
			if normalizeAssetType(a.Type) != "character" {
				continue
			}
			if flaggedIdentity[characterAssetIdentity(a)] {
				continue
			}
			if !neckPropSet(characterLookText(a))["奖牌"] && !neckPropSet(characterLookText(a))["奖章"] {
				continue
			}
			sid, idx := firstShotOf(a.ResourceID, characterAssetIdentity(a))
			msg := fmt.Sprintf("文案是看奖牌/奖牌出镜，但角色「%s」设定把奖牌画成了项链，各镜参考图会不一致", a.Name)
			if propMedal {
				msg += "（资产里已有奖牌/奖杯道具）"
			}
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R7",
				ShotID:     sid,
				ShotIndex:  idx,
				ResourceID: a.ResourceID,
				Message:    msg,
				Suggestion: "重出该角色图，去掉脖子上的奖牌；奖牌用道具参考图。分镜里看奖牌时绑道具，不要绑成人物常戴项链。",
			})
			break
		}
	}
	return out
}

func characterLookText(a AssetItem) string {
	return strings.TrimSpace(a.Prompt + "\n" + a.Description)
}

func characterAssetIdentity(a AssetItem) string {
	if p := strings.ToLower(strings.TrimSpace(a.ParentName)); p != "" {
		return p
	}
	return strings.ToLower(strings.TrimSpace(a.Name))
}

func neckPropSet(text string) map[string]bool {
	set, _ := neckPropClaim(text)
	return set
}

func neckPropClaim(text string) (map[string]bool, bool) {
	t := lookAtPropRE.ReplaceAllString(text, "")
	explicitNone := noNeckPropRE.MatchString(t)
	t = noNeckPropRE.ReplaceAllString(t, "")
	out := map[string]bool{}
	if strings.Contains(t, "奖牌") || strings.Contains(t, "奖章") || strings.Contains(t, "金牌") || strings.Contains(t, "银牌") || strings.Contains(t, "铜牌") {
		out["奖牌"] = true
	}
	if strings.Contains(t, "项链") {
		out["项链"] = true
	}
	if strings.Contains(t, "绶带") {
		out["绶带"] = true
	}
	return out, len(out) > 0 || explicitNone
}

func formatPropSet(set map[string]bool) string {
	if len(set) == 0 {
		return "无奖牌/项链"
	}
	parts := make([]string, 0, len(set))
	for _, k := range []string{"奖牌", "项链", "绶带"} {
		if set[k] {
			parts = append(parts, k)
		}
	}
	return strings.Join(parts, "、")
}

func sameStringSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func scriptBGMPhrases(script string) []string {
	out := make([]string, 0)
	for _, m := range sfxFieldRE.FindAllStringSubmatch(script, -1) {
		if len(m) < 2 {
			continue
		}
		for _, p := range sfxSplitRE.Split(m[1], -1) {
			p = strings.TrimSpace(p)
			if p != "" && bgmRE.MatchString(p) {
				out = append(out, p)
			}
		}
	}
	return out
}

func bgmFamily(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "鼓"):
		return "drums"
	case strings.Contains(s, "钢琴"):
		return "piano"
	case strings.Contains(s, "弦") || strings.Contains(s, "提琴"):
		return "strings"
	case strings.Contains(s, "古筝") || strings.Contains(s, "琵琶") || strings.Contains(s, "古琴") || strings.Contains(s, "笛") || strings.Contains(s, "箫"):
		return "folk"
	case strings.Contains(s, "合成") || strings.Contains(s, "电子"):
		return "synth"
	case bgmRE.MatchString(s):
		return "generic"
	default:
		return ""
	}
}

func bgmFamiliesCompatible(a, b string) bool {
	if a == "" || b == "" || a == "generic" || b == "generic" {
		return true
	}
	return a == b
}

func shotIndexOf(shot ShotContext, i int) int {
	if shot.Index > 0 {
		return shot.Index
	}
	return i + 1
}

func standaloneMention(script, name string, allNames []string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) < 2 || !strings.Contains(script, name) {
		return false
	}
	longer := make([]string, 0)
	for _, other := range allNames {
		if other != name && strings.Contains(other, name) && len(other) > len(name) {
			longer = append(longer, other)
		}
	}
	from := 0
	for {
		rel := strings.Index(script[from:], name)
		if rel < 0 {
			return false
		}
		i := from + rel
		inside := false
		for _, L := range longer {
			pos := strings.Index(L, name)
			if pos < 0 {
				continue
			}
			start := i - pos
			if start >= 0 && start+len(L) <= len(script) && script[start:start+len(L)] == L {
				inside = true
				break
			}
		}
		if !inside {
			return true
		}
		from = i + len(name)
	}
}

func beatSeconds(line string) float64 {
	m := beatRangeRE.FindStringSubmatch(line)
	if len(m) < 3 {
		return 0
	}
	start, end := 0, 0
	fmt.Sscanf(m[1], "%d", &start)
	fmt.Sscanf(m[2], "%d", &end)
	if end <= start {
		return 0
	}
	return float64(end - start)
}

func detectCrowdCharacterRefs(shots []ShotContext) []QCIssue {
	out := make([]QCIssue, 0)
	for i, shot := range shots {
		n := len(services.MentionedCharacterNames(shot.Script))
		if n <= services.MaxNamedCharacterRefs {
			continue
		}
		idx := shotIndexOf(shot, i)
		out = append(out, QCIssue{
			Severity:   "high",
			Code:       "R1",
			ShotID:     shot.ID,
			ShotIndex:  idx,
			Message:    fmt.Sprintf("本镜文案点名了 %d 个有名有姓的人，超过 %d 人。10 秒镜拆得过密，Seedance 容易糊成群像", n, services.MaxNamedCharacterRefs),
			Suggestion: fmt.Sprintf("改写分镜脚本：本镜最多点名 %d 个焦点角色。其余人写成杀手/路人/群演，用站位图出人数，不要再点名。挂参考图不限人数。", services.MaxNamedCharacterRefs),
		})
	}
	return out
}

func detectMissingSpatialBlocking(shots []ShotContext) []QCIssue {
	out := make([]QCIssue, 0)
	flagged := 0
	for i, shot := range shots {
		n := 0
		seen := map[string]bool{}
		for _, r := range shot.Refs {
			if r.Kind != "character" {
				continue
			}
			key := characterIdentityKey(r)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			n++
		}
		// Also count names written in the timed script (九格 / 说), so QC
		// still fires before refs are hung.
		for _, name := range services.MentionedCharacterNames(shot.Script) {
			key := strings.ToLower(strings.TrimSpace(name))
			if key == "" || seen[key] || services.CharacterLooksLikeCrowd(name) {
				continue
			}
			seen[key] = true
			n++
		}
		if n < 1 {
			continue
		}
		if services.ScriptHasSpatialSlot(shot.Script) {
			continue
		}
		idx := shotIndexOf(shot, i)
		out = append(out, QCIssue{
			Severity:   "low",
			Code:       "R3",
			ShotID:     shot.ID,
			ShotIndex:  idx,
			Message:    "人物镜未写九格站位（左前/右中等）与朝向，视频容易漂移、跳轴或换位",
			Suggestion: "单人也写成 裴长河(右中)3/4正面朝左；多人逐一标注。同场没写走位就沿用原格子；极特写注明承接上一拍位置不变。",
		})
		flagged++
		if flagged >= 12 {
			break
		}
	}
	return out
}

// normalizeScriptForQC expands literal \n from JSON storyboard output and splits
// embedded 【秒】 rows so per-beat QC actually runs.
func normalizeScriptForQC(script string) string {
	if script == "" {
		return script
	}
	if strings.Contains(script, `\n`) {
		script = strings.ReplaceAll(script, `\n`, "\n")
	}
	return normalizeScriptBeatStructure(script)
}

func detectScriptFormatIssues(shots []ShotContext) []QCIssue {
	return detectScriptFormatIssuesAgainst(shots, "")
}

func detectScriptFormatIssuesAgainst(shots []ShotContext, sourceScript string) []QCIssue {
	out := make([]QCIssue, 0)
	flagged := 0
	for i, shot := range shots {
		raw := shot.Script
		idx := shotIndexOf(shot, i)
		if literalNewlineRE.MatchString(raw) {
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R2",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    "分镜文案含字面量 \\n，时序没有正确分行",
				Suggestion: "每拍单独一行；JSON 里换行写 \\n 后系统应展开。请重新拆镜或手动改成真换行。",
			})
			flagged++
		}
		script := normalizeScriptForQC(raw)
		if metaSpeakerRE.MatchString(script) {
			out = append(out, QCIssue{
				Severity:   "high",
				Code:       "R2",
				ShotID:     shot.ID,
				ShotIndex:  idx,
				Message:    "出现「第N集说」等非角色说话人",
				Suggestion: "删掉集数/meta 标签，台词必须写成 韩铮说：「……」这类真实角色名。",
			})
			flagged++
		}
		beats := scriptBeats(script)
		for bi, beat := range beats {
			if danglingSpeakerTailRE.MatchString(beat) || danglingInnerMonologueTailRE.MatchString(beat) {
				out = append(out, QCIssue{
					Severity:   "high",
					Code:       "R2",
					ShotID:     shot.ID,
					ShotIndex:  idx,
					Message:    "时序行有「角色说：」但没有「台词」",
					Suggestion: "补全「」或删掉空说话人标签，避免下一拍台词错位。",
				})
				flagged++
			}
			if beatIsPlotless(beat) {
				out = append(out, QCIssue{
					Severity:   "medium",
					Code:       "R2",
					ShotID:     shot.ID,
					ShotIndex:  idx,
					Message:    "有一拍只有音效/反应，没有镜头动作或台词",
					Suggestion: "合并进上一拍，或补上镜头动作/对白；不要单独占 3 秒纯鼓点。",
				})
				flagged++
			}
			for _, m := range dialogueRE.FindAllStringSubmatch(beat, -1) {
				if len(m) < 2 {
					continue
				}
				q := strings.TrimSpace(m[1])
				if (isMetaJunkDialogue(q) || isQCMetaDialogue(q)) && !dialogueAppearsInSource(q, sourceScript) {
					out = append(out, QCIssue{
						Severity:   "high",
						Code:       "R2",
						ShotID:     shot.ID,
						ShotIndex:  idx,
						Message:    fmt.Sprintf("台词含第四面墙/meta：「%s」", clipRunes(q, 20)),
						Suggestion: "删掉差评、维基百科、主线任务等现代吐槽，只保留剧本原句。",
					})
					flagged++
				}
				if quoteEndsMidClause(q) && bi+1 < len(beats) {
					nextQ := firstQuoteInLine(beats[bi+1])
					if nextQ != "" && quoteSplitMidWord(q, nextQ) {
						out = append(out, QCIssue{
							Severity:   "high",
							Code:       "R2",
							ShotID:     shot.ID,
							ShotIndex:  idx,
							Message:    fmt.Sprintf("台词在词中间被切开：「%s」/「%s」", clipRunes(q, 12), clipRunes(nextQ, 12)),
							Suggestion: "整句移到下一拍，或只在标点处拆分；禁止备/料、顾爷爷/姚爷这类断句。",
						})
						flagged++
					}
				}
			}
			secs := beatSeconds(beat)
			if secs <= 0 {
				continue
			}
			for _, m := range dialogueRE.FindAllStringSubmatch(beat, -1) {
				if len(m) < 2 || quoteIsEmpty(m[1]) {
					continue
				}
				n := speechRunes(m[1])
				need := int(math.Ceil(float64(n) / 4.0))
				if need < 2 {
					need = 2
				}
				dur := int(secs)
				if dur >= 6 && n > 0 && n <= 8 && dur > need+2 {
					out = append(out, QCIssue{
						Severity:   "medium",
						Code:       "R2",
						ShotID:     shot.ID,
						ShotIndex:  idx,
						Message:    fmt.Sprintf("短台词 %d 字却给了 %.0f 秒", n, secs),
						Suggestion: "把秒数让给动作/对白更多的上一拍；短句 2～4 秒足够。",
					})
					flagged++
					break
				}
			}
		}
		if flagged >= 16 {
			return out
		}
	}
	return out
}

func dialogueAppearsInSource(quote, script string) bool {
	if strings.TrimSpace(script) == "" {
		return false
	}
	for _, line := range ExtractSpokenLines(script) {
		qk, lk := normalizeQuoteKey(quote), normalizeQuoteKey(line.Text)
		if qk != "" && lk != "" && strings.Contains(lk, qk) {
			return true
		}
		if spokenTextMatchScore(quote, line.Text) > 0 {
			return true
		}
	}
	return false
}

func isQCMetaDialogue(q string) bool {
	k := normalizeQuoteKey(q)
	if k == "" {
		return false
	}
	for _, junk := range []string{"维基百科", "主线任务", "刘博", "丁——", "丁—", "第3集", "第2集", "第1集"} {
		if strings.Contains(k, junk) {
			return true
		}
	}
	return false
}

func quoteEndsMidClause(q string) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return false
	}
	runes := []rune(q)
	if len(runes) == 0 {
		return false
	}
	last := runes[len(runes)-1]
	return !isClauseBreakRune(last)
}

func detectMissingScriptDialogue(shots []ShotContext, script string) []QCIssue {
	orig := ExtractSpokenLines(script)
	if len(orig) == 0 || len(shots) == 0 {
		return nil
	}
	joined := strings.Builder{}
	for _, shot := range shots {
		for _, q := range quotesInScript(normalizeScriptForQC(shot.Script)) {
			if !quoteIsEmpty(q) {
				joined.WriteString(dialogueCoverageKey(q))
			}
		}
	}
	all := joined.String()
	out := make([]QCIssue, 0)
	flagged := 0
	for _, line := range orig {
		if speechRunes(line.Text) < 8 {
			continue
		}
		key := dialogueCoverageKey(line.Text)
		if key == "" {
			continue
		}
		if scriptLineCoveredInShots(key, all) {
			continue
		}
		shotID, shotIdx := bestShotForMissingDialogue(shots, line)
		out = append(out, QCIssue{
			Severity:   "high",
			Code:       "R2",
			ShotID:     shotID,
			ShotIndex:  shotIdx,
			Message:    fmt.Sprintf("剧本 %s 的台词未进分镜：「%s」", line.Speaker, clipRunes(line.Text, 28)),
			Suggestion: "按剧本原句补回该角色台词，不要只留尾巴或改写成别的说法。",
		})
		flagged++
		if flagged >= 6 {
			break
		}
	}
	return out
}

func bestShotForMissingDialogue(shots []ShotContext, line SpokenLine) (uint, int) {
	best, bestScore := -1, 0
	for i, shot := range shots {
		for _, quote := range quotesInScript(normalizeScriptForQC(shot.Script)) {
			if score := spokenTextMatchScore(quote, line.Text); score > bestScore {
				best, bestScore = i, score
			}
		}
	}
	if best >= 0 {
		return shots[best].ID, shotIndexOf(shots[best], best)
	}
	return firstShotForSpeaker(shots, line.Speaker)
}

func scriptLineCoveredInShots(key, allQuotes string) bool {
	if key == "" || allQuotes == "" {
		return false
	}
	if strings.Contains(allQuotes, key) {
		return true
	}
	return false
}

func firstShotForSpeaker(shots []ShotContext, speaker string) (uint, int) {
	speaker = strings.TrimSpace(speaker)
	for i, shot := range shots {
		if strings.Contains(shot.Script, speaker) {
			return shot.ID, shotIndexOf(shot, i)
		}
	}
	if len(shots) > 0 {
		return shots[0].ID, shotIndexOf(shots[0], 0)
	}
	return 0, 1
}

func characterIdentityKey(r ShotRefInfo) string {
	if p := strings.ToLower(strings.TrimSpace(r.ParentName)); p != "" {
		return p
	}
	name := strings.ToLower(strings.TrimSpace(r.DisplayName))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(r.Name))
	}
	if i := strings.Index(name, "·"); i > 0 {
		return strings.TrimSpace(name[:i])
	}
	if i := strings.Index(name, "（"); i > 0 {
		return strings.TrimSpace(name[:i])
	}
	return name
}

func scriptBeats(script string) []string {
	script = normalizeScriptForQC(script)
	var beats []string
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "秒】") {
			beats = append(beats, line)
		}
	}
	return beats
}

func firstScriptBeat(script string) string {
	beats := scriptBeats(script)
	if len(beats) == 0 {
		return strings.TrimSpace(script)
	}
	return beats[0]
}

func lastScriptBeat(script string) string {
	beats := scriptBeats(script)
	if len(beats) == 0 {
		return strings.TrimSpace(script)
	}
	return beats[len(beats)-1]
}

func MergeQCAfterFix(fresh QCReport, previous, leftover []QCIssue) QCReport {
	verified := fresh.Issues
	scope := append(append([]QCIssue{}, previous...), leftover...)
	out := make([]QCIssue, 0, len(verified)+len(leftover))
	seen := map[string]bool{}
	add := func(issue QCIssue) {
		key := qcIssueLocationKey(issue)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, issue)
	}
	for _, issue := range verified {
		if matchesKnownIssue(issue, scope) {
			add(issue)
		}
	}
	for _, issue := range leftover {
		if mechanicalQCIssue(issue) {
			continue
		}
		add(issue)
	}
	fresh.Issues = out
	fresh.Score = scoreQCIssues(out)
	fresh.Summary = summarizeQCIssues(out, true)
	return fresh
}

func dropLLMGhostIssues(issues, det []QCIssue) []QCIssue {
	out := make([]QCIssue, 0, len(issues))
	for _, issue := range issues {
		code := strings.ToUpper(strings.TrimSpace(issue.Code))
		switch code {
		case "R1", "R3", "R4", "R5", "R6", "R7", "R8":
			if matchesKnownIssue(issue, det) {
				out = append(out, issue)
			}
		case "R9":
			// Platform compatibility is handled at video submission time, not
			// inside the creative QC/fix loop.
			continue
		case "R2":
			kind := r2Kind(issue.Message)
			if kind != "other" {
				continue
			}
			out = append(out, issue)
		default:
			out = append(out, issue)
		}
	}
	return out
}

func r2Kind(message string) string {
	switch {
	case strings.Contains(message, "未标明说话人") || strings.Contains(message, "未写说话人") ||
		(strings.Contains(message, "格式") && strings.Contains(message, "说：")):
		return "speaker"
	case strings.Contains(message, "说话人未进画面") || strings.Contains(message, "对错口型"):
		return "onscreen"
	case strings.Contains(message, "会说不完") || strings.Contains(message, "上限"):
		return "overlong"
	case strings.Contains(message, "空「") || strings.Contains(message, "空引号"):
		return "empty"
	case strings.Contains(message, "挤在同一拍") || strings.Contains(message, "两句台词"):
		return "merged"
	case strings.Contains(message, "重复同一句") || strings.Contains(message, "两镜台词重复"):
		return "duplicate"
	case strings.Contains(message, "字面量") || strings.Contains(message, "\\n"):
		return "format"
	case strings.Contains(message, "第N集说") || strings.Contains(message, "非角色说话人"):
		return "meta"
	case strings.Contains(message, "第四面墙") || strings.Contains(message, "meta"):
		return "meta"
	case strings.Contains(message, "词中间被切开") || strings.Contains(message, "断句"):
		return "split"
	case strings.Contains(message, "剧本") && strings.Contains(message, "未进分镜"):
		return "missing"
	case strings.Contains(message, "只有音效") || strings.Contains(message, "纯鼓点"):
		return "plotless"
	case strings.Contains(message, "短台词") && strings.Contains(message, "却给了"):
		return "timing"
	case strings.Contains(message, "没有「台词」") || strings.Contains(message, "空说话人"):
		return "speaker"
	default:
		return "other"
	}
}

func mechanicalQCIssue(issue QCIssue) bool {
	code := strings.ToUpper(strings.TrimSpace(issue.Code))
	switch code {
	case "R3":
		// Ambiguous interaction targets require scene/character context and are
		// handled by the editorial rewrite. The deterministic R3 fixer cannot
		// safely infer which named character "对方" refers to.
		return !r3NeedsEditorialRewrite(issue)
	case "R1", "R4", "R5", "R6", "R7", "R8", "R9":
		return true
	case "R2":
		return r2Kind(issue.Message) != "other"
	default:
		return false
	}
}

func prefixHasSpeaker(prefix string) bool {
	p := strings.TrimRight(prefix, " \t")
	if speakerPrefixRE.MatchString(p) {
		return true
	}
	if !nameColonPrefixRE.MatchString(p) {
		return false
	}
	for _, blocked := range []string{"镜头：", "镜头:", "音效：", "音效:", "画面：", "画面:", "旁白：", "旁白:"} {
		if strings.HasSuffix(p, blocked) {
			return false
		}
	}
	return true
}

func quoteHasSpeaker(beat, quote string) bool {
	idx := strings.Index(beat, quote)
	if idx < 0 {
		return prefixHasSpeaker(beat)
	}
	return prefixHasSpeaker(beat[:idx])
}

func summarizeQCIssues(issues []QCIssue, recheck bool) string {
	if len(issues) == 0 {
		if recheck {
			return "复检通过：对照当前分镜，先前指出的格式与台词问题已不在。"
		}
		return "质检完成，未发现需要处理的问题。"
	}
	high := 0
	for _, issue := range issues {
		if issue.Severity == "high" {
			high++
		}
	}
	prefix := "质检发现"
	if recheck {
		prefix = "复检后仍有"
	}
	if high > 0 {
		return fmt.Sprintf("%s %d 项问题，其中 %d 项为高优先级。", prefix, len(issues), high)
	}
	return fmt.Sprintf("%s %d 项问题。", prefix, len(issues))
}

func hasR2Kind(known []QCIssue, issue QCIssue, kind string) bool {
	for _, item := range known {
		if strings.ToUpper(strings.TrimSpace(item.Code)) != "R2" {
			continue
		}
		if r2Kind(item.Message) != kind {
			continue
		}
		if issue.ShotID > 0 && item.ShotID > 0 && issue.ShotID == item.ShotID {
			return true
		}
		if issue.ShotIndex > 0 && item.ShotIndex > 0 && issue.ShotIndex == item.ShotIndex {
			return true
		}
	}
	return false
}

func matchesKnownIssue(issue QCIssue, known []QCIssue) bool {
	code := strings.ToUpper(strings.TrimSpace(issue.Code))
	for _, item := range known {
		if strings.ToUpper(strings.TrimSpace(item.Code)) != code {
			continue
		}
		if issue.ShotID > 0 && item.ShotID > 0 && issue.ShotID == item.ShotID {
			return true
		}
		if issue.ShotIndex > 0 && item.ShotIndex > 0 && issue.ShotIndex == item.ShotIndex {
			return true
		}
	}
	return false
}

func qcIssueLocationKey(issue QCIssue) string {
	code := strings.ToUpper(strings.TrimSpace(issue.Code))
	detail := ""
	if code == "R2" {
		detail = "|" + r2Kind(issue.Message)
		if r2Kind(issue.Message) == "missing" {
			detail += "|" + normalizeQuoteKey(issue.Message)
		}
	}
	if issue.ShotID > 0 {
		return fmt.Sprintf("%s|id|%d%s", code, issue.ShotID, detail)
	}
	return fmt.Sprintf("%s|idx|%d%s", code, issue.ShotIndex, detail)
}

func dedupeQCIssues(issues []QCIssue) []QCIssue {
	rank := func(sev string) int {
		switch sev {
		case "high":
			return 3
		case "medium":
			return 2
		default:
			return 1
		}
	}
	best := map[string]QCIssue{}
	order := make([]string, 0, len(issues))
	for _, issue := range issues {
		key := qcIssueLocationKey(issue)
		prev, ok := best[key]
		if !ok {
			best[key] = issue
			order = append(order, key)
			continue
		}
		if rank(issue.Severity) > rank(prev.Severity) {
			best[key] = issue
		}
	}
	out := make([]QCIssue, 0, len(order))
	for _, key := range order {
		out = append(out, best[key])
	}
	return out
}

func LeftoverQCIssues(previous []QCIssue, selected []QCIssue) []QCIssue {
	skip := map[string]bool{}
	for _, issue := range selected {
		skip[qcIssueDedupeKey(issue)] = true
	}
	out := make([]QCIssue, 0, len(previous))
	for _, issue := range previous {
		if skip[qcIssueDedupeKey(issue)] {
			continue
		}
		out = append(out, issue)
	}
	return out
}

func ChangedShotIDs(before, after []ShotContext) map[uint]bool {
	prev := map[uint]ShotContext{}
	for _, shot := range before {
		prev[shot.ID] = shot
	}
	out := map[uint]bool{}
	for _, shot := range after {
		old, ok := prev[shot.ID]
		if !ok || old.Script != shot.Script || refsFingerprint(old.Refs) != refsFingerprint(shot.Refs) {
			out[shot.ID] = true
		}
	}
	return out
}

func qcIssueDedupeKey(issue QCIssue) string {
	return fmt.Sprintf("%s|%d|%d", strings.ToUpper(strings.TrimSpace(issue.Code)), issue.ShotID, issue.ResourceID)
}

func refsFingerprint(refs []ShotRefInfo) string {
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, fmt.Sprintf("%s:%d", ref.Kind, ref.ResourceID))
	}
	return strings.Join(parts, ",")
}

func scoreQCIssues(issues []QCIssue) string {
	high, med := 0, 0
	for _, issue := range issues {
		switch issue.Severity {
		case "high":
			high++
		case "medium":
			med++
		}
	}
	switch {
	case high >= 3:
		return "D"
	case high >= 1:
		return "C"
	case med > 5:
		return "C"
	case med > 2:
		return "B"
	default:
		return "A"
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
