package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"novaly/backend/models"
)

// SceneGridAngles is the fixed 3×3 camera matrix for scene 9-grids (row-major, 1..9).
var SceneGridAngles = []string{
	"正面全景", "正面近景", "侧面全景",
	"侧面近景", "背面全景", "背面近景",
	"俯视全景", "俯视近景", "斜向高位总览",
}

var (
	optimizeAnglesTailRE = regexp.MustCompile(`(?is)(?:["']\s*)?,\s*["']angles["']\s*:`)
	optimizeScriptHeadRE = regexp.MustCompile(`(?is)^\s*\{\s*["']script["']\s*:\s*["']`)
)

// SceneAngleCandidate is one selectable camera-angle cell from a scene 9-grid.
type SceneAngleCandidate struct {
	ID        uint   `json:"id"`
	SceneName string `json:"sceneName"`
	Angle     string `json:"angle"`
	Cell      int    `json:"cell"`
}

// OptimizeShotAnglePick is a model-selected scene angle for the optimized script.
type OptimizeShotAnglePick struct {
	ID    uint   `json:"id"`
	Label string `json:"label"`
	Beats string `json:"beats,omitempty"` // e.g. "0-3秒、3-6秒"
}

// OptimizeShotResult is the optimized script plus optional scene-angle refs.
type OptimizeShotResult struct {
	Script string
	Angles []OptimizeShotAnglePick
}

// OptimizeShotScript re-directs one shot using the same production standards as
// storyboard generation while preserving its locked dialogue and story facts.
// Neighboring shots (prev up to 10 + next 2) are context. When scene 9-grid
// angle cells are available, the model also picks perspectives.
func (s *ArkService) OptimizeShotScript(
	provider models.AIProvider,
	model models.AIModel,
	currentScript string,
	previousScripts []string,
	nextScripts []string,
	style string,
	prevRefsSummary string,
	currentRefsSummary string,
	angles []SceneAngleCandidate,
	duration int,
) (OptimizeShotResult, error) {
	// Repeated optimization must be idempotent: remove malformed JSON tails from
	// previous model responses before feeding them back as story content.
	currentScript = cleanOptimizeShotPlain(currentScript)
	currentScript = cleanOptimizeShotRepetition(currentScript, style)
	for i := range previousScripts {
		previousScripts[i] = cleanOptimizeShotRepetition(cleanOptimizeShotPlain(previousScripts[i]), style)
	}
	for i := range nextScripts {
		nextScripts[i] = cleanOptimizeShotRepetition(cleanOptimizeShotPlain(nextScripts[i]), style)
	}
	if currentScript == "" {
		return OptimizeShotResult{}, fmt.Errorf("当前分镜文案为空")
	}
	if ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return OptimizeShotResult{}, fmt.Errorf("文本模型服务商未配置 API Key")
	}
	if duration <= 0 {
		duration = 10
	}

	wantAngles := len(angles) > 0

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`你是影视分镜导演。点击「AI优化文案」代表按制作分镜时的完整标准，重新导演「当前分镜」，不是只做局部润色或挑错。

【重做边界】
- 保留当前镜的剧情事实、人物、场景、道具、台词原文和说话人；禁止加戏、删剧情、改派说话人、编造新对白。
- 可以重排本镜内部的景别、运镜、动作、表情、站位、朝向、音效与节奏，让它成为可直接生成视频的确定稿。
- 前后分镜只用于承接动作、人物关系、位置和情绪；不得把前镜已完成内容重复进来，也不得提前后镜剧情。
- 当前参考图决定本镜能出现的人物、场景、道具和站位；不得写出参考图之外的新角色或新资产。

本镜时长上限：%d 秒（本系统固定按 10 秒成片，不是小红书模板里的 15 秒）。时序只允许 0 到 %d 秒，标准写成【0-3秒】【3-7秒】【7-10秒】三行写满。禁止【3-6秒】【6-9秒】【10-13秒】【9-13秒】这类错拍或终点超过上限的行；禁止两段写成同一起止秒（例如两个【7-10秒】）；严禁三行都写成【0-10秒】（那是整镜总长，不是镜内切点）。多出的拍不要写进本镜，也不要写成无【秒】标题的游离台词挂在文案末尾。站位规则保留：双人及以上已有的 人名(格子)朝向 不要删。

【制作分镜标准 · 每项都要执行】
R1 文案能认出的角色/场景/道具，本镜参考图里要有；同一人不要日常图和换装图同镜。文案地点必须对上绑定的场景图。
R2 台词：写成 阿彪说：「……」。禁止空「」。同一句「」不要出现在两镜，也不要在同一镜两拍里重复。按 4 字/秒，3 秒拍大约 12 字；单拍超过 20 字拆到下一拍并换景别，禁止精简原文。一句一拍，换说话人就换拍。零删改：引号内对白必须逐字保留；仅 R9 平台违禁（动刀/刺杀/踢人等）必须中性替换。禁止编造剧本没有的对白，禁止第四面墙/弹幕体/差评体吐槽。
R3 人物目标与因果：每句台词和每个动作必须承接上一拍的具体信息，服务于人物当下明确目标（试探、逼问、说服、隐瞒、拒绝、确认、争取、保护等），并给下一拍留下回应点。通过动作、微表情、视线和说话方式呈现，不要把「目标：」写成字幕。动作、视线和反应对象必须写具体角色姓名，禁止“听者、对方、对面的人、另一人、其眼神/态度”；即使上下文能推断也必须点名。禁止输出“接受、质疑或拒绝”这类待选择说明，必须结合上下文选定一个可拍的具体反应；不得用停顿、陷入沉思凑时长。承接上一镜结尾动作；已穿衣/扣扣子后不要退回赤膊。
R4 衍生态绑衍生图，穿衣绑日常父角色。
R5 音效必须带配乐床（鼓点等）+ 环境/动作声；相邻镜沿用同一曲风。
R6 固有服装发型交给参考图，文案只写动作（接衬衫、扣扣子、皱眉），不要写「身着××」。
R7 奖牌/奖杯是道具，不要画进脖子当项链。
R8 总时长不超过本镜秒数，时序不得重叠；终点秒数不得大于本镜上限；不要停在【6-9秒】就结束。
R9 平台安全：禁止持刀/动刀/刺杀/踢人/要命。改成对峙、口角、商业纠纷，不改情节走向。
人数：不要在文案里加路人特写；具名角色尽量 ≤5，点名的人都要能挂参考图；同屏超过 5 人就按时间拆镜。场景是空镜底板，人物只来自角色参考图。
`+SpatialBlockingRules+`
`+SpatialBlockingScriptHint+`
同场相邻镜没写走位就不要换左右格子。已有九格站位必须保留。
站位补写：只要画面有人物，不论单人或多人，每个【秒】行中人物首次出现时都写 人名(格子)朝向。连续拍没写走位/转身就沿用原格子和朝向。只有空镜、纯物件、纯手部或脸部极特写可省略；人物极特写须写「承接上一拍人物位置不变」。当前参考图若已挂「站位示意图/站位图」，人物左右前后以该图为准，文案九格只复述图，冲突改文案不改图。
禁止在画面里安排字幕、花字、对白条、图N 编号。图N 只用于理解参考图，不要写进成片画面。

【每拍必备信息】
每个【秒】行都要形成确定、可拍的画面，并包含：景别与运镜；人物位置与朝向（适用时）；承接上下文的具体动作、微表情和目标；配乐床、环境声及动作声；本拍台词（如有）。禁止把规则、备选项或解释文字写进文案。
每拍自检：承接了什么信息 → 人物想得到什么 → 做了什么可见动作 → 下一拍需要回应什么。任何一项不明确就重写。

【台词铁律】
1. 零删改：禁止精简、合并、改派说话人。禁止新编对白、禁止现代网络梗/弹幕吐槽。
2. 4字/秒：按汉字（不含标点）计时。超时拆拍，不要指望视频模型加速念完。
3. 超20字强制拆拍并换景别。
4. 一句一拍；切换说话人时换拍。同一句不要抄两遍。
5. 必须标明说话人。无台词不要写空「」。
6. VO/旁白/内心OS 也按台词计时，原样写入「」，并标注内心独白无口型。

【输出】
- 无论原文是否已有时序，都按上述标准重新组织为【0-3秒】【3-7秒】【7-10秒】三行确定稿；不是点评，也不是在原句上小修。
- 格式：【0-3秒】镜头：景别+运镜+站位朝向+目标性动作；音效：配乐床+环境声+动作声；角色说：「台词」
- 无小说叙述就不要编第一段，直接输出时序正文。有小说叙述则原样保留在时序之前。
- 不要复制全局画风词到时序行。不要编造台词。不要把换装状态名写成另一个人。
- 全文中文；台词用「」；不要 Markdown。
`, duration, duration))
	if style = strings.TrimSpace(style); style != "" {
		b.WriteString("\n项目画面质感（仅用于理解整体视觉，不得复制到各时序行；视频生成阶段会另行全局附加）：")
		b.WriteString(style)
		b.WriteString("\n禁止在【0-3秒】等时序行中重复 UE5、电影质感、胶片颗粒、4K、光影、调色、真人实拍等全局画质词；每行只写本段独有的镜头、动作、音效、台词和机位。\n")
	}
	if len(previousScripts) > 0 {
		b.WriteString("\n前面分镜（从旧到新，仅作连续性参考，最多 10 镜）：\n")
		for i, script := range previousScripts {
			b.WriteString(fmt.Sprintf("—— 前镜 %d ——\n%s\n", i+1, strings.TrimSpace(script)))
		}
	}
	if prevRefs := strings.TrimSpace(prevRefsSummary); prevRefs != "" {
		b.WriteString("\n前面分镜已选参考图（尤其场景机位，请优先延续同场景视角或做合理机位切换）：\n")
		b.WriteString(prevRefs)
		b.WriteString("\n")
	}
	if currentRefs := strings.TrimSpace(currentRefsSummary); currentRefs != "" {
		b.WriteString("\n当前镜参考图（只用来认人/对服装，不要把「图N」写进时序正文或画面）：\n")
		b.WriteString(currentRefs)
		b.WriteString("\n")
	}
	if len(nextScripts) > 0 {
		b.WriteString("\n后面分镜（从近到远，仅作衔接铺垫参考，最多 2 镜，勿剧透进当前成片）：\n")
		for i, script := range nextScripts {
			b.WriteString(fmt.Sprintf("—— 后镜 %d ——\n%s\n", i+1, strings.TrimSpace(script)))
		}
	}

	if wantAngles {
		b.WriteString(`
【七、场景机位（9宫格视角）】
当前可用「场景9宫格切分格」如下，请为当前分镜挑选 1～3 个最合适的视角（可多个），并在对应时序行里写清楚「机位：机位名」。
规则：
1. id 必须来自下列候选；label 写「场景名·机位名」（如废弃洞府·正面近景）。
2. 每个【起止秒数】行末尾补「机位：机位名」（该秒数段所用视角）；多秒数共用同一机位可重复写。机位名用下方候选的机位列（如正面近景）。
3. 同场景优先延续前镜已用机位；需要推进空间/动作时再切换到近景/侧面/俯视等。
4. 不要编造候选外的机位名；若无需场景机位则 angles 为空数组。

候选机位（id|场景|格号|机位）：
`)
		for _, a := range angles {
			name := strings.TrimSpace(a.SceneName)
			angle := strings.TrimSpace(a.Angle)
			if angle == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("%d|%s|格%d|%s\n", a.ID, name, a.Cell, angle))
		}
		b.WriteString(`
【输出要求】只输出一个 JSON 对象，不要 Markdown，不要解释：
{"script":"核对后的分镜文案（含机位标注）","angles":[{"id":数字,"label":"场景名·机位名","beats":"0-3秒"}]}
angles 为本次选用的机位；beats 写清用在哪些秒数段。
`)
	} else {
		b.WriteString("\n【输出要求】直接输出按制作分镜标准重做后的文案正文，不要 JSON，不要 Markdown（禁用 **、__、# 等标记），不要解释过程，不要前言后语。\n")
	}

	b.WriteString("\n当前分镜原文（锁定剧情、人物、场景、道具、台词与说话人；镜头表达按标准重做）：\n")
	b.WriteString(currentScript)
	prompt := ApplyDramaSkillGuidance(b.String(), "write", "storyboard")

	maxTokens := 4096
	if wantAngles {
		maxTokens = 4600
	}
	content, err := s.chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.15,
		"max_tokens":  maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return OptimizeShotResult{}, err
	}

	if wantAngles {
		result, err := parseOptimizeShotJSON(content, angles)
		result.Script = cleanOptimizeShotRepetition(result.Script, style)
		return result, err
	}
	out := cleanOptimizeShotRepetition(cleanOptimizeShotPlain(content), style)
	if out == "" {
		return OptimizeShotResult{}, fmt.Errorf("大模型未返回优化文案")
	}
	return OptimizeShotResult{Script: out}, nil
}

func cleanOptimizeShotPlain(content string) string {
	out := strings.TrimSpace(content)
	out = strings.TrimPrefix(out, "```")
	out = strings.TrimPrefix(out, "markdown")
	out = strings.TrimPrefix(out, "text")
	out = strings.TrimPrefix(out, "json")
	out = strings.TrimSpace(strings.Trim(out, "`"))
	if i := strings.Index(out, "```"); i >= 0 {
		out = strings.TrimSpace(out[:i])
	}
	// Strip markdown bold / heading markers the model likes to scatter into the script.
	out = strings.ReplaceAll(out, "**", "")
	out = strings.ReplaceAll(out, "__", "")
	// Some models emit invalid JSON containing literal newlines in "script".
	// Recover that field instead of leaking the trailing angles JSON into the script.
	if script := extractLooseOptimizeScript(out); script != "" {
		out = script
	}
	// Last-resort cleanup for standard JSON and single-quoted pseudo JSON:
	// `","angles":[...]}` / `','angles':[...]}`.
	if loc := optimizeAnglesTailRE.FindStringIndex(out); loc != nil {
		out = strings.TrimSpace(out[:loc[0]])
	}
	out = optimizeScriptHeadRE.ReplaceAllString(out, "")
	out = strings.TrimSuffix(strings.TrimSpace(out), `"}`)
	out = strings.TrimSuffix(strings.TrimSpace(out), `'}`)
	return strings.TrimSpace(out)
}

// CleanOptimizedShotScript exposes the defensive sanitizer for migrations and
// endpoints that return an existing shot without running optimization.
func CleanOptimizedShotScript(content string) string {
	return cleanOptimizeShotPlain(content)
}

// cleanOptimizeShotRepetition removes global project style copied into every
// timing beat. BuildVideoPrompt already appends project style once globally.
func cleanOptimizeShotRepetition(script, style string) string {
	out := strings.TrimSpace(script)
	if global := strings.TrimSpace(style); global != "" {
		out = strings.ReplaceAll(out, global, "")
		// The model often copies only a subset of the global style into one beat
		// (usually the final beat), so exact whole-string removal is insufficient.
		// Remove comma-delimited clauses that already exist in project style.
		styleClauses := map[string]bool{}
		for _, clause := range strings.FieldsFunc(global, func(r rune) bool {
			return r == '，' || r == ',' || r == '。' || r == '；' || r == ';' || r == '\n'
		}) {
			if key := normalizeStyleClause(clause); len([]rune(key)) >= 2 {
				styleClauses[key] = true
			}
		}
		lines := strings.Split(out, "\n")
		for i, line := range lines {
			if !strings.Contains(line, "秒】") {
				continue
			}
			audio := strings.Index(line, "；音效")
			before, after := line, ""
			if audio >= 0 {
				before, after = line[:audio], line[audio:]
			}
			parts := strings.Split(before, "，")
			kept := parts[:0]
			for _, part := range parts {
				if styleClauses[normalizeStyleClause(part)] {
					continue
				}
				kept = append(kept, part)
			}
			lines[i] = strings.TrimRight(strings.Join(kept, "，"), "，； ") + after
		}
		out = strings.Join(lines, "\n")
	}

	lines := strings.Split(out, "\n")
	type beat struct {
		line int
		head string
	}
	beats := make([]beat, 0, 4)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "【") || !strings.Contains(trimmed, "秒】") {
			continue
		}
		audio := strings.Index(line, "；音效")
		if audio < 0 {
			continue
		}
		beats = append(beats, beat{line: i, head: strings.TrimSpace(line[:audio])})
	}
	if len(beats) >= 2 {
		parts := make([][]string, len(beats))
		for i, b := range beats {
			parts[i] = strings.Split(b.head, "，")
		}
		common := make([]string, 0, 12)
		for offset := 1; ; offset++ {
			if len(parts[0]) < offset {
				break
			}
			token := strings.TrimSpace(parts[0][len(parts[0])-offset])
			if token == "" {
				break
			}
			same := true
			for i := 1; i < len(parts); i++ {
				if len(parts[i]) < offset || strings.TrimSpace(parts[i][len(parts[i])-offset]) != token {
					same = false
					break
				}
			}
			if !same {
				break
			}
			common = append([]string{token}, common...)
		}
		suffix := strings.Join(common, "，")
		// Only remove a substantial shared suffix; short common action wording is
		// legitimate and should remain.
		if len([]rune(suffix)) >= 24 {
			for _, b := range beats {
				audio := strings.Index(lines[b.line], "；音效")
				before, after := lines[b.line][:audio], lines[b.line][audio:]
				before = strings.TrimSuffix(strings.TrimSpace(before), "，"+suffix)
				before = strings.TrimSuffix(strings.TrimSpace(before), suffix)
				lines[b.line] = strings.TrimRight(before, "，； ") + after
			}
		}
	}

	out = strings.Join(lines, "\n")
	replacer := strings.NewReplacer("，，", "，", "，；", "；", "；；", "；")
	return strings.TrimSpace(replacer.Replace(out))
}

func normalizeStyleClause(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "，,。；; ")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\t", "")
	return s
}

// CleanOptimizeShotRepetition exposes timing-beat cleanup for startup backfills.
func CleanOptimizeShotRepetition(script, style string) string {
	return cleanOptimizeShotRepetition(script, style)
}

// extractLooseOptimizeScript reads the script string from model-produced JSON even
// when literal newlines make it invalid JSON. It scans for the quote immediately
// before the `angles` field, respecting escaped quotes inside dialogue.
func extractLooseOptimizeScript(raw string) string {
	startKey := strings.Index(raw, `"script"`)
	if startKey < 0 {
		return ""
	}
	colon := strings.Index(raw[startKey+len(`"script"`):], ":")
	if colon < 0 {
		return ""
	}
	start := startKey + len(`"script"`) + colon + 1
	for start < len(raw) && (raw[start] == ' ' || raw[start] == '\n' || raw[start] == '\r' || raw[start] == '\t') {
		start++
	}
	if start >= len(raw) || raw[start] != '"' {
		return ""
	}
	start++
	end := -1
	escaped := false
	for i := start; i < len(raw); i++ {
		switch {
		case escaped:
			escaped = false
		case raw[i] == '\\':
			escaped = true
		case raw[i] == '"':
			rest := strings.TrimLeft(raw[i+1:], " \t\r\n")
			if strings.HasPrefix(rest, `,"angles"`) || strings.HasPrefix(rest, `, "angles"`) {
				end = i
				i = len(raw)
			}
		}
	}
	if end < start {
		return ""
	}
	encoded := raw[start:end]
	// strconv.Unquote requires valid JSON-style newlines.
	encoded = strings.ReplaceAll(encoded, "\r\n", `\n`)
	encoded = strings.ReplaceAll(encoded, "\n", `\n`)
	if decoded, err := strconv.Unquote(`"` + encoded + `"`); err == nil {
		return strings.TrimSpace(decoded)
	}
	return strings.TrimSpace(strings.ReplaceAll(encoded, `\"`, `"`))
}

func parseOptimizeShotJSON(raw string, angles []SceneAngleCandidate) (OptimizeShotResult, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	raw = strings.TrimSpace(raw)

	byID := make(map[uint]SceneAngleCandidate, len(angles))
	for _, a := range angles {
		byID[a.ID] = a
	}

	var parsed struct {
		Script string `json:"script"`
		Angles []struct {
			ID    any    `json:"id"`
			Label string `json:"label"`
			Beats string `json:"beats"`
		} `json:"angles"`
	}
	try := func(s string) bool {
		return json.Unmarshal([]byte(s), &parsed) == nil && strings.TrimSpace(parsed.Script) != ""
	}
	if !try(raw) {
		if loc := refMatchJSONArrayRe.FindString(raw); loc != "" {
			// not an array — try object extract
		}
		// Try first {...} block
		if i := strings.Index(raw, "{"); i >= 0 {
			if j := strings.LastIndex(raw, "}"); j > i {
				_ = try(raw[i : j+1])
			}
		}
	}

	script := strings.TrimSpace(parsed.Script)
	if script == "" {
		// Recover malformed JSON (usually literal newlines inside script) before
		// falling back to plain text.
		script = extractLooseOptimizeScript(raw)
	}
	if script == "" {
		// Model ignored JSON — treat whole reply as script.
		script = cleanOptimizeShotPlain(raw)
		if script == "" {
			return OptimizeShotResult{}, fmt.Errorf("大模型未返回优化文案")
		}
		return OptimizeShotResult{Script: script}, nil
	}
	script = cleanOptimizeShotPlain(script)

	picks := make([]OptimizeShotAnglePick, 0, 3)
	seen := map[uint]bool{}
	for _, p := range parsed.Angles {
		id, ok := anyToUint(p.ID)
		if !ok || seen[id] {
			continue
		}
		c, ok := byID[id]
		if !ok {
			continue
		}
		seen[id] = true
		label := strings.TrimSpace(p.Label)
		if label == "" {
			label = c.Angle
		}
		picks = append(picks, OptimizeShotAnglePick{
			ID:    id,
			Label: label, // 期望「场景名·机位名」；前端按资源覆盖为 canonical 标签
			Beats: strings.TrimSpace(p.Beats),
		})
		if len(picks) >= 3 {
			break
		}
	}
	return OptimizeShotResult{Script: script, Angles: picks}, nil
}

// SceneAngleLabel returns the fixed angle name for cell index 1..9.
func SceneAngleLabel(cell int) string {
	if cell < 1 || cell > len(SceneGridAngles) {
		return ""
	}
	return SceneGridAngles[cell-1]
}
