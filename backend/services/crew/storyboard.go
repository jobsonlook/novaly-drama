package crew

import (
	"fmt"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"
)

func SplitStoryboard(ark *services.ArkService, provider models.AIProvider, model models.AIModel, script, plan string, project models.Project, assets []AssetItem) (StoryboardResult, error) {
	return SplitStoryboardContinuing(ark, provider, model, script, plan, project, assets, nil)
}

// SplitStoryboardContinuing splits only the remaining story after kept shots.
// kept shots are already locked (often with finished videos) and must not be rewritten.
func SplitStoryboardContinuing(
	ark *services.ArkService,
	provider models.AIProvider,
	model models.AIModel,
	script, plan string,
	project models.Project,
	assets []AssetItem,
	kept []KeptShot,
) (StoryboardResult, error) {
	script = strings.TrimSpace(script)
	if script == "" {
		return StoryboardResult{}, fmt.Errorf("剧本为空")
	}
	if services.ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return StoryboardResult{}, fmt.Errorf("文本模型服务商未配置 API Key")
	}
	remainingScript := continuationScriptAfterKept(script, kept)
	if strings.TrimSpace(remainingScript) == "" {
		return StoryboardResult{}, fmt.Errorf("续拆起点之后没有剩余剧本")
	}

	pace := NormalizeStoryboardPace(project.StoryboardPace)
	target := EstimateStoryboardCountForPace(remainingScript, pace)
	if target < 2 {
		target = 2
	}
	lo, hi := StoryboardCountRangeForPace(target, pace)
	prompt := storyboardPrompt(remainingScript, plan, project, assets, target, lo, hi, 0, pace, kept)

	content, err := ark.Chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.12,
		"max_tokens":  16384,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return StoryboardResult{}, err
	}
	result, err := parseStoryboardResultContinuing(content, remainingScript, nil)
	if err != nil {
		retryPrompt := storyboardJSONRetryPrompt(remainingScript, plan, project, assets, target, lo, hi, pace, kept, err)
		retryContent, retryErr := ark.Chat(provider, map[string]any{
			"model":       model.ModelID,
			"temperature": 0.05,
			"max_tokens":  16384,
			"messages": []map[string]string{
				{"role": "user", "content": retryPrompt},
			},
		})
		if retryErr == nil {
			if retried, pErr := parseStoryboardResultContinuing(retryContent, remainingScript, nil); pErr == nil && len(retried.Shots) > 0 {
				result, err = retried, nil
			}
		}
	}
	if err != nil {
		return StoryboardResult{}, err
	}
	n := len(result.Shots)
	if n < lo || n > hi {
		retry := storyboardPrompt(remainingScript, plan, project, assets, target, lo, hi, n, pace, kept)
		retryContent, retryErr := ark.Chat(provider, map[string]any{
			"model":       model.ModelID,
			"temperature": 0.05,
			"max_tokens":  16384,
			"messages": []map[string]string{
				{"role": "user", "content": retry},
			},
		})
		if retryErr == nil {
			if retried, pErr := parseStoryboardResultContinuing(retryContent, remainingScript, nil); pErr == nil && len(retried.Shots) > 0 {
				result = retried
			}
		}
	}
	return result, nil
}

// KeptShot is a locked preceding shot when continuing a split.
type KeptShot struct {
	Label  string
	Script string
}

// continuationScriptAfterKept advances through the locked screenplay dialogue
// exactly. A partial quote advances only by the spoken prefix, so clicking
// “从此续拆” after half of a long line resumes at its tail instead of marking
// the whole source line covered through fuzzy matching.
func continuationScriptAfterKept(script string, kept []KeptShot) string {
	script = strings.TrimSpace(script)
	if script == "" || len(kept) == 0 {
		return script
	}
	orig := ExtractSpokenLines(script)
	if len(orig) == 0 {
		return script
	}
	var sourceStream, coveredStream strings.Builder
	for _, line := range orig {
		sourceStream.WriteString(dialogueCoverageKey(line.Text))
	}
	for _, shot := range kept {
		for _, q := range quotesInScript(normalizeScriptForQC(shot.Script)) {
			coveredStream.WriteString(dialogueCoverageKey(q))
		}
	}
	sourceRunes, coveredRunes := []rune(sourceStream.String()), []rune(coveredStream.String())
	consumed := 0
	for consumed < len(sourceRunes) && consumed < len(coveredRunes) && sourceRunes[consumed] == coveredRunes[consumed] {
		consumed++
	}
	if consumed == 0 {
		return script
	}

	searchFrom, remaining := 0, consumed
	for _, line := range orig {
		keyLen := len([]rune(dialogueCoverageKey(line.Text)))
		textAt := strings.Index(script[searchFrom:], line.Text)
		if textAt < 0 {
			return script
		}
		textAt += searchFrom
		lineStart := strings.LastIndex(script[:textAt], "\n") + 1
		if remaining >= keyLen {
			remaining -= keyLen
			searchFrom = textAt + len(line.Text)
			if remaining == 0 {
				cut := searchFrom
				if nl := strings.Index(script[cut:], "\n"); nl >= 0 {
					cut += nl + 1
				}
				return prependContinuationSceneContext(script[:lineStart], script[cut:])
			}
			continue
		}
		tail := dialogueTailAfterSpeechRunes(line.Text, remaining)
		return prependContinuationSceneContext(script[:lineStart], script[lineStart:textAt]+tail+script[textAt+len(line.Text):])
	}
	return ""
}

func dialogueTailAfterSpeechRunes(text string, consumed int) string {
	if consumed <= 0 {
		return text
	}
	count := 0
	for i, r := range text {
		if isSpeechRune(r) {
			count++
		}
		if count == consumed {
			return text[i+len(string(r)):]
		}
	}
	return ""
}

func prependContinuationSceneContext(before, remainder string) string {
	lines := strings.Split(before, "\n")
	context := ""
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.Contains(line, "场景：") || strings.Contains(line, "场景:") {
			context = line
			break
		}
	}
	remainder = strings.TrimSpace(remainder)
	if context == "" || strings.HasPrefix(remainder, context) {
		return remainder
	}
	return "【续拆场景上下文】" + context + "\n" + remainder
}

func storyboardPrompt(script, plan string, project models.Project, assets []AssetItem, target, lo, hi, previous int, pace string, kept []KeptShot) string {
	countHint := fmt.Sprintf("本集必须拆成 %d 条分镜（允许 %d～%d 条，禁止差到一倍）。", target, lo, hi)
	if len(kept) > 0 {
		countHint = fmt.Sprintf("前面已锁定 %d 条分镜（有成片，禁止重写）。你只需再拆出后续 %d 条（允许 %d～%d 条）。", len(kept), target, lo, hi)
	}
	if previous > 0 {
		countHint = fmt.Sprintf("上一次拆成了 %d 条，不对。必须改成 %d 条（允许 %d～%d）。不要再自由发挥条数。", previous, target, lo, hi)
		if len(kept) > 0 {
			countHint = fmt.Sprintf("上一次后续拆成了 %d 条，不对。前面已锁定 %d 条仍禁止动。必须再拆出 %d 条后续（允许 %d～%d）。", previous, len(kept), target, lo, hi)
		}
	}
	prompt := `你是分镜 Agent。把定稿剧本拆成可生成视频的分镜列表。

【条数硬约束】
` + countHint + `
` + storyboardCutRules(pace) + `

` + DirectorCraftRules + `

` + DirectorBeatExample + `

【时长硬约束 · 不是 15 秒】
视频接口按「每次请求」计费，单次最长 10 秒（小红书那套「每段 15 秒」在本系统一律改成 10 秒）。因此：
- 每一镜 duration 必须是 10。禁止 5、6、9、13、15 或其它秒数。
- 时序只允许写【0-3秒】【3-7秒】【7-10秒】三行写满。禁止【3-6秒】【6-9秒】【10-13秒】【9-13秒】这类错拍或终点超过 10 秒的行；多出来的动作/台词必须拆到下一镜，从【0-3秒】重计。
- 严禁把三拍都写成【0-10秒】【0-10秒】【0-10秒】：那是整镜时长，不是镜内切点。多拍必须错开起止秒，只能是 0-3 / 3-7 / 7-10。
- 禁止把台词写成没有【秒】标题的游离行挂在文案末尾。
- 禁止为了少切镜把两场或闪回塞进同一条。
- 最后一条时序必须写到 10 秒（可延伸上一拍的目标性动作），不要停在 9 秒；禁止用无指向的反应或停顿凑时长。
- 禁止编造剧本没有的对白；禁止第四面墙、弹幕体、差评体吐槽。同一句「」不要在同一镜两拍里重复。
- 动作、视线和反应对象必须写具体角色姓名；禁止写“听者、对方、对面的人、另一人、其眼神/态度”。即使前文能推断，也必须明确写成“洛清霜盯住韩铮”“韩铮等待洛清霜回答”。
- 完整对白优先落在同一拍；只有超拍预算才拆。后半拍禁止再抄一遍已说完的提醒句（例如「少惹姚三刀」只出现一次）。
- 配乐床（鼓点/弦乐垫）写在「有画面、有动作」的拍的音效里即可；禁止单独开一拍只有「音效：…」、无镜头无台词，不要用 3 秒纯音效占剧情时长。

每镜 script 用中文时序格式，10 秒内 2～3 拍（起止秒必须不同、顺时衔接）：
【0-3秒】镜头：景别+运镜+核心动作；双人及以上必须写 人名(格子)朝向；音效：…（含配乐床）；角色说：「台词」
【3-7秒】镜头：正反打/动作延续（必须承接上一拍信息，写出人物为试探、拒绝、确认、隐瞒、等待答复等目标采取的具体行动；禁止只写「反应」占满 4 秒）；音效：…
若第三拍仍有台词或关键动作，再写【7-10秒】镜头：…；音效：…；禁止第三拍只留音效或空「反应」。
细切时一条只写一句「」；打包模式才允许【3-7秒】再写下一句。
不要英文引号。无台词不要写空「」。不要复制全局画风词到时序行。不要编造剧本没有的情节和台词。相邻两镜首尾动作/站位要能接上。
禁止持刀闯入、动刀、刺杀、踢人、要命等武装冲突与人身威胁；改成对峙、口角或商业纠纷，保持原情节走向。
站位硬约束（必须保留）：只要画面有人物，不论单人或多人，每个【秒】行里人物首次出现时必须写 人名(左前/右中等)朝向；单人也要写，例如「裴长河(右中)3/4正面朝左」。连续拍没写走位/转身就沿用原格子和朝向。只有空镜、纯物件、纯手部或脸部极特写可省略格子；人物极特写须写「承接上一拍人物位置不变」。

` + DialogueCraftRules + `
` + formatKeptShotsBlock(kept) + `
只输出 JSON（不要 markdown 代码块、不要解释）：
{"shots":[{"label":"简短标签","duration":10,"script":"时序文案","characterNames":["角色名"],"sceneName":"场景名","propNames":["道具名"]}]}
script 必须是 JSON 单行字符串：换行写 \\n，禁止真实换行；每镜 script 控制在 800 字以内，避免输出被截断。

characterNames/sceneName/propNames 必须来自下方资产名单；没有对应资产就留空数组/空字符串。duration 只能是 10。shots 数组长度必须落在条数硬约束里。
换装衍生的完整名是「父角色 · 状态」（如 韩铮 · 赤膊战损）。该外观出场时 characterNames 只写这一条，不要再并列父角色「韩铮」。script 里仍然写父角色人名（韩铮），不要把「赤膊战损」当成另一个人。

【换装/外观 · 对齐 Toonflow】
- 固有服装、发型、长相交给参考图，script 不要写「赤膊的韩铮」「蓝衬衫」当造型说明。
- 换装只写动作：接过衬衫、扣扣子、脱下外套。同一条 10 秒镜只允许一个方向，禁止又穿又脱。
- 赤膊开场 → 下一拍只能维持赤膊或开始穿上；已经扣扣子/穿上之后，下一镜禁止退回没穿上衣或改成脱衣服。
- 需要赤膊外观时 characterNames 用「韩铮 · 赤膊战损」；穿衣/扣扣子时只用「韩铮」。两张绝不同镜并列，也不同时出现在同一镜。
- 看奖牌、奖杯用道具参考，propNames 填对应道具。不要写「戴着奖牌」，除非剧本明确一直戴在脖子上。

【人数 · 对齐 Toonflow】
- 场景图是空镜底板：characterNames 里的人才进画，不要让场景自己长出路人。
- sceneName 写资产名单里的场景名。若同一场有多条时序、景别变化（特写/中景/全景/俯视），在对应【秒】行末写「机位：正面近景」这类机位名（与场景9宫格一致：正面全景/正面近景/侧面全景/侧面近景/背面全景/背面近景/俯视全景/俯视近景/斜向高位总览），系统会按机位挂对应格子图。
- 同一镜 characterNames 最多 5 人（只限制拆镜文案，选参考图时点名的人都要挂上）；且必须包含本镜口播台词的说话人。
- 同屏/同一条 10 秒里具名角色超过 5 人：按时间切开，每条只写当时在画面里的人（开场韩铮+阿彪对峙一条；后半小嘉小南进画再开一条，从【0-3秒】重计）。宁可多一条，也不要把 6 个具名角色写进同一条文案。
- 对白拍更严：有「X说：」的那一行，镜头具名角色尽量 ≤3，且 X 必须是画面主体并开口；不要把整场人塞进一句台词的画面里。
- 群演、杀手甲乙、路人、宾客不占这 5 个名额，不要每人挂一张角色图，改用一张站位图表示人数和左右站位。
- 同一场连续对话：细切时换说话人就切镜；打包时具名出场 ≤5 可仍收进一条 10 秒，但每一拍仍要让该拍说话人进画开口。
- 禁止为了热闹在镜里加剧本没有的群演特写；群演不要单独配台词。

` + services.SpatialBlockingRules + `
` + services.SpatialBlockingScriptHint + `
同场相邻镜：没写走位/转身，就不要换左右格子。

【配乐 · 写进音效，后期不加】
- 音效 = 配乐床 + 环境音 + 动作声。配乐必须写，不要只写环境音。
- 整集用同一条配乐床（乐器/节奏型一致）。相邻镜不要换曲风。
- 允许随情绪改强弱：低鼓点 → 紧张鼓点；不要从鼓点跳到钢琴或弦乐。
- 写具体听感（低沉鼓点、紧张鼓点），不要只写「BGM」两个字母。

资产名单：
` + formatAssetRoster(assets) + `
` + optionalProjectContext(project)
	prompt += formatLockedDialogue(script)
	if strings.TrimSpace(plan) != "" {
		prompt += "导演规划：\n" + clipRunes(plan, 2000) + "\n\n"
	}
	prompt += "定稿剧本：\n" + clipRunes(script, 14000)
	return services.ApplyDramaSkillGuidance(prompt, "write", "storyboard")
}

// storyboardJSONRetryPrompt is used when the model returned truncated/invalid JSON.
func storyboardJSONRetryPrompt(script, plan string, project models.Project, assets []AssetItem, target, lo, hi int, pace string, kept []KeptShot, parseErr error) string {
	b := strings.Builder{}
	b.WriteString("上次拆镜 JSON 解析失败（输出可能被截断或格式错误）。请重新拆镜。\n")
	b.WriteString("失败原因：" + clipRunes(parseErr.Error(), 120) + "\n\n")
	b.WriteString("【本次硬性要求】\n")
	b.WriteString(fmt.Sprintf("- 必须拆 %d 条（允许 %d～%d），每条 duration=10\n", target, lo, hi))
	b.WriteString("- 只输出一行紧凑 JSON，不要 markdown 代码块、不要任何解释文字\n")
	b.WriteString("- script 必须是 JSON 单行：换行写 \\\\n，禁止真实换行；每镜 script ≤600 字\n")
	b.WriteString("- label 不超过 12 字\n")
	b.WriteString("- 时序只用【0-3秒】【3-7秒】【7-10秒】，最后一拍写到 10 秒\n\n")
	b.WriteString(storyboardCutRules(pace) + "\n\n")
	b.WriteString(formatKeptShotsBlock(kept))
	b.WriteString("\n资产名单：\n" + formatAssetRoster(assets))
	b.WriteString("\n\n输出格式：\n")
	b.WriteString(`{"shots":[{"label":"标签","duration":10,"script":"【0-3秒】…\\n【3-7秒】…","characterNames":[],"sceneName":"","propNames":[]}]}` + "\n\n")
	if strings.TrimSpace(plan) != "" {
		b.WriteString("导演规划：\n" + clipRunes(plan, 1200) + "\n\n")
	}
	b.WriteString("定稿剧本：\n" + clipRunes(script, 12000))
	return b.String()
}

func storyboardCutRules(pace string) string {
	if NormalizeStoryboardPace(pace) == StoryboardPacePacked {
		return `先按场次/闪回/台词量估拍秒数，再按每条 10 秒打包。同一剧本多次拆镜，条数必须落在这个区间。

【切镜边界 · 打包】
- 同一场、同一批人物、连续发生的动作/对话，收进一条 10 秒镜，用镜内时序推进。
- 镜内可以换景别、推拉摇移、情绪起伏、对白来回，这些不算切镜。
- 必须切下一条的边界只有：换场/换空间、闪回进入或回到现在、时间跳跃、换装完成态切换、当前 10 秒按约 4 字/秒已经装不下。
- 禁止按「说话人乒乓 / 情绪微变 / 喝水三步骤」拆成碎镜。`
	}
	return `按「细切」拆：一条分镜只承载一个戏剧拍（一句对白，或一个关键动作）。同一剧本多次拆镜，条数必须落在这个区间。

【切镜边界 · 细切（对齐第1集）】
- 换说话人 → 新开一条。禁止把两句完整对白塞进同一条。
- 一句对白按 4 字/秒在【0-3秒】说不完（约 12 字）→ 拆到下一镜，不要塞进同一条的后半拍凑合。
- 每个关键动作行（△ 递物/起身/开门/闪回切换等）尽量单独成镜；不要把「递衬衫 + 对白 + 扣扣子」打进一条。
- 换场、闪回进出、换装完成态，必须切镜。
- duration 仍写 10：前 3～7 秒演完这一拍，余下写承接上下文的目标性行为（人物因上一信息作出选择、观察对方以求确认、等待明确答复等），不要用泛化反应/停顿凑时长，也不要为了填满 10 秒再塞下一句台词。
- 禁止为了条数好看把半场戏打成 4～5 条大镜。`
}

func formatKeptShotsBlock(kept []KeptShot) string {
	if len(kept) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n【续拆模式 · 按定稿剧本重拆后续 · 禁止沿用旧分镜】\n")
	b.WriteString("前面若干条分镜已有成片，只锁定「已拍完」的覆盖范围，禁止重写它们。\n")
	b.WriteString("你输出的 JSON 只能是它们之后的新分镜。\n")
	b.WriteString("硬性要求：\n")
	b.WriteString("1. 后续必须对照下方「定稿剧本」重新拆镜，按当前导演级规则与 10 秒时序重写。\n")
	b.WriteString("2. 禁止把旧分镜文案当模板：不要沿用旧切法、旧景别节奏、旧时序结构、旧机位写法去续写。\n")
	b.WriteString("3. 禁止再写已覆盖台词（见清单）；剧本里尚未覆盖的对白与情节，全部按剧本重新拆进新分镜。\n")
	b.WriteString("4. 第一条新分镜只需在站位/朝向上承接最后一镜结尾，便于成片衔接；剧情推进以剧本为准，不以旧分镜剧情顺序为准。\n")
	b.WriteString("5. 下方定稿剧本已经由系统裁到准确续拆游标；第一行就是尚未完成的剧情/台词，禁止跳过，也禁止回到更早剧情。\n")

	coveredQuotes := make([]string, 0)
	seen := map[string]bool{}
	for _, shot := range kept {
		for _, q := range quotesInScript(shot.Script) {
			key := normalizeQuoteKey(q)
			if key == "" || seen[key] || speechRunes(q) < 4 {
				continue
			}
			seen[key] = true
			coveredQuotes = append(coveredQuotes, q)
		}
	}
	if len(coveredQuotes) > 0 {
		b.WriteString("\n已覆盖台词（禁止再出现在新分镜「」里）：\n")
		for i, q := range coveredQuotes {
			b.WriteString(fmt.Sprintf("%d. 「%s」\n", i+1, clipRunes(q, 48)))
		}
	}

	last := kept[len(kept)-1]
	lastLabel := strings.TrimSpace(last.Label)
	if lastLabel == "" {
		lastLabel = fmt.Sprintf("前序%d", len(kept))
	}
	b.WriteString(fmt.Sprintf("\n最后一镜仅供站位衔接（禁止当拆镜模板）· %s：\n%s\n", lastLabel, lastBeatSnippet(last.Script)))
	start := len(kept) - 3
	if start < 0 {
		start = 0
	}
	b.WriteString("\n最近三镜剧情上下文（只用于理解人物状态、场景和因果，不得照抄旧切法）：\n")
	for i := start; i < len(kept); i++ {
		b.WriteString(fmt.Sprintf("- %s：%s\n", firstNonEmpty(strings.TrimSpace(kept[i].Label), fmt.Sprintf("前序%d", i+1)), clipRunes(strings.ReplaceAll(kept[i].Script, "\n", " "), 260)))
	}
	b.WriteString("\n")
	return b.String()
}

func lastBeatSnippet(script string) string {
	script = strings.TrimSpace(script)
	if script == "" {
		return "（无）"
	}
	lines := strings.Split(script, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trim := strings.TrimSpace(lines[i])
		if strings.Contains(trim, "秒】") {
			return clipRunes(trim, 180)
		}
	}
	return clipRunes(script, 180)
}

func parseStoryboardResult(content, script string) (StoryboardResult, error) {
	return parseStoryboardResultContinuing(content, script, nil)
}

func parseStoryboardResultContinuing(content, script string, kept []KeptShot) (StoryboardResult, error) {
	var result StoryboardResult
	if err := unmarshalObject(content, &result); err != nil {
		return StoryboardResult{}, fmt.Errorf("分镜解析失败: %w", err)
	}
	cleaned := make([]StoryboardShot, 0, len(result.Shots))
	for i, shot := range result.Shots {
		// Model retries sometimes double-escape newlines, producing a literal
		// two-character `\n` followed by a real newline. Canonicalize before
		// dialogue anchoring; doing it later leaves fake empty beats and tails.
		shot.Script = normalizeScriptForQC(shot.Script)
		if shot.Script == "" {
			continue
		}
		shot.Label = strings.TrimSpace(shot.Label)
		if shot.Label == "" {
			shot.Label = fmt.Sprintf("分镜%d", i+1)
		}
		shot.Duration = 10
		shot.SceneName = strings.TrimSpace(shot.SceneName)
		shot.CharacterNames = cleanNameList(shot.CharacterNames)
		shot.PropNames = cleanNameList(shot.PropNames)
		cleaned = append(cleaned, shot)
	}
	if len(cleaned) == 0 {
		return StoryboardResult{}, fmt.Errorf("分镜 Agent 未拆出镜头")
	}
	covered := make([]string, 0, len(kept))
	for _, k := range kept {
		covered = append(covered, k.Script)
	}
	restored := ScheduleStoryboardDialogue(cleaned, script, covered)
	if len(restored) == 0 {
		return StoryboardResult{}, fmt.Errorf("分镜 Agent 未拆出镜头")
	}
	// Dialogue has already been deterministically budgeted by
	// ScheduleStoryboardDialogue. Do not run fuzzy dialogue dedupe here: a short
	// continuation tail is meaningful and must never be removed as a duplicate.
	// Normalize timeline BEFORE pack. Otherwise three 【0-10秒】 lines normalize to
	// 【0-10】【10-20】【20-30】 and Pack splits them into three separate shots
	// each still labeled 【0-10秒】 — the bug users see after 重新拆镜.
	for i := range restored {
		restored[i].Duration = 10
		restored[i].Script = NormalizeShotTimelinePreservingDialogue(restored[i].Script, 10)
	}
	result.Shots = PackStoryboardShotsPreservingDialogue(restored)
	for i := range result.Shots {
		result.Shots[i].Duration = 10
		result.Shots[i].Script = FinalizeShotScriptPreservingDialogue(result.Shots[i].Script, 10)
	}
	// Hard postcondition: the object returned to the controller must still cover
	// every locked line. If packing exposed an edge case, rebuild once from the
	// finalized visual beats rather than handing incomplete dialogue to legacy QC.
	if !storyboardsCoverAllDialogue(result.Shots, script, covered) {
		result.Shots = ScheduleStoryboardDialogue(result.Shots, script, covered)
		for i := range result.Shots {
			result.Shots[i].Duration = 10
			result.Shots[i].Script = FinalizeShotScriptPreservingDialogue(result.Shots[i].Script, 10)
		}
	}
	result.Shots = dropEmptyDialogueStoryboards(result.Shots)
	return result, nil
}

func storyboardsCoverAllDialogue(shots []StoryboardShot, script string, covered []string) bool {
	contexts := make([]ShotContext, len(shots))
	for i, shot := range shots {
		contexts[i] = ShotContext{Label: shot.Label, Script: shot.Script, Duration: shot.Duration}
	}
	return shotContextsCoverAllDialogue(contexts, script, covered)
}

func dropEmptyDialogueStoryboards(shots []StoryboardShot) []StoryboardShot {
	out := make([]StoryboardShot, 0, len(shots))
	for _, shot := range shots {
		if strings.Contains(shot.Label, "对白") && len(quotesInScript(shot.Script)) == 0 {
			continue
		}
		out = append(out, shot)
	}
	return out
}

func cleanNameList(names []string) []string {
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		key := strings.ToLower(n)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, n)
	}
	return out
}
