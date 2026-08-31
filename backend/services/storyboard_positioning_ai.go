package services

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"novaly/backend/models"
)

var (
	inlineRefLegendRe = regexp.MustCompile(`(?s)(.*?)([。.!！\s]*)(参考图[：:].+)\s*$`)
	lineRefLegendRe   = regexp.MustCompile(`(?m)^\s*参考图[：:].+\s*$`)
)

// AnalyzeShotPositioning asks the text model to write an editable scene-blocking
// (站位图) prompt from the current shot script plus prior shot context.
func (s *ArkService) AnalyzeShotPositioning(
	provider models.AIProvider,
	model models.AIModel,
	currentScript string,
	previousScripts []string,
	nextScripts []string,
	style string,
	refLabels []string,
) (string, error) {
	currentScript = strings.TrimSpace(currentScript)
	if currentScript == "" {
		return "", fmt.Errorf("当前分镜文案为空")
	}
	if ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return "", fmt.Errorf("文本模型服务商未配置 API Key")
	}

	var b strings.Builder
	b.WriteString(`你是专业电影分镜美术指导。请联合分析「当前分镜文案」及其前后相邻分镜，撰写一段用于 AI 图生图的「场景站位图」提示词。

目标：生成一张清晰的场景调度示意图（不是分镜四格，不是角色三视图），画面中人物位置、坐站姿态、朝向、前后景关系一目了然，方便后续视频生成。

写作要求：
1. 直接输出可交给图像模型的中文提示词正文，不要 JSON，不要 Markdown 标题，不要解释过程。
2. 明确环境（地点、时间、光影、氛围），以及文案里每个出场人物的九格站位、坐站姿态与朝向。格子只能写：左前、中前、右前、左中、中中、右中、左后、中后、右后；朝向用 3/4正面朝左/朝右、正面、背面等。
3. 【坐站姿态 · 最高优先级】必须按文案写出每人是坐着、站着、跪着、半起身还是正在起身/坐下。例：韩铮(中中)坐着3/4正面朝右；阿彪(右后)起身举杯3/4正面朝左。
   - 文案写坐着/落座/坐在桌边/围桌/喝酒聊天且未写起身→必须写「坐着」，禁止全员站立。
   - 只有文案明确写站着/起身/走近/门口站立的人才写站立或起身。
   - 禁止因为「站位图」三个字就默认站立；「站位」只表示位置调度，不是姿势。
   - 包厢/餐桌/酒局场景：未写起身时默认坐在桌边，前后景用坐姿高低区分，不要围桌全站。
4. 文案出场的人（含群演、次要角色）都要画进站位图并标名，每人只出现一次；没有单张角色参考图的人按文案外形画，禁止画成与焦点人物同脸的分身。群演放后排格子（左后/中后/右后）。
5. 【多镜联合推断 · 最高优先级】先在内部完成场次归并和站位追踪，再输出正文：
   - 只用与当前镜同一地点、同一连续时段的相邻镜头推断站位；不同地点或明显闪回/转场只参考风格，不得混入人物位置。
   - 综合至少当前镜及最近的 2～5 个同场镜头；离当前镜越近权重越高。前镜用于确定当前开场站位，后镜只用于校验当前站位是否能自然衔接，不得把后镜尚未发生的走位提前。
   - 为每个角色追踪「九格位置、坐站姿态、朝向、面对对象」。文案未写走近、退后、绕行、换位、起身、坐下等动作时，沿用最近同场镜头，禁止无动作跳格、左右互换或忽坐忽站。
   - 近景、特写、过肩、反打描述的是摄影机视角，不等于人物换位。屏幕左右若与场景九格冲突，以同场全景/中景建立的空间关系为准；反打只能改变朝向描述，不能凭空交换人物位置。
   - 多条信息冲突时按「当前镜明确动作 > 最近同场全景/中景 > 最近同场其他镜 > 更早镜头」裁决；无法确定时保持上一稳定站位，不自行发明移动。
6. 画面规格：16:9 横构图；质感跟项目画面风格走（写实就写实、插画就插画）；系统会另行强制「人脸马赛克 + 身上标名」，正文中不要要求露脸或禁止文字标注。
7. 正文控制在 180～420 字，具体可执行，避免空泛形容词堆砌。
8. 输出格式必须严格分为两段：
   - 第一段：站位画面描述正文（不要在正文里写「图1/图2」用途说明）。
   - 空一行后，单独一行写出参考图对应关系，格式固定为：
     参考图：图1为××，图2为××，图3为××
   - 「图N为谁」必须单独成行，不要跟在正文同一段里。
`)
	if style = strings.TrimSpace(style); style != "" {
		b.WriteString("\n项目画面质感：")
		b.WriteString(style)
		b.WriteString("\n")
	}
	if len(refLabels) > 0 {
		b.WriteString("\n参考图标签（按顺序，请据此写最后一行「参考图：…」）：\n")
		hasMap := false
		for i, label := range refLabels {
			b.WriteString(fmt.Sprintf("- 图%d：%s\n", i+1, label))
			if strings.Contains(label, "站位") || strings.Contains(label, "火柴人") || strings.Contains(label, "骨架") {
				hasMap = true
			}
		}
		if hasMap {
			b.WriteString("\n【站位参考图优先】参考图中已有站位示意图/骨架时：正文的九格站位、坐站、朝向、人数必须严格复述该图，不要按文案另发明一套站位。文案只用来补动作与台词语境。\n")
		}
	}
	writePositioningContinuityContext(&b, previousScripts, nextScripts)
	b.WriteString("\n当前分镜文案：\n")
	b.WriteString(currentScript)
	if cue := positioningPoseCueFromScript(currentScript); cue != "" {
		b.WriteString("\n\n【本镜姿态硬约束 · 必须写进正文】\n")
		b.WriteString(cue)
	}

	content, err := s.chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.4,
		"max_tokens":  2048,
		"messages": []map[string]string{
			{"role": "user", "content": b.String()},
		},
	})
	if err != nil {
		return "", err
	}
	prompt := strings.TrimSpace(content)
	prompt = strings.TrimPrefix(prompt, "```")
	prompt = strings.TrimPrefix(prompt, "markdown")
	prompt = strings.TrimPrefix(prompt, "text")
	prompt = strings.TrimSpace(strings.Trim(prompt, "`"))
	if prompt == "" {
		return "", fmt.Errorf("大模型未返回站位文案")
	}
	prompt = normalizePositioningPrompt(prompt, refLabels)
	return enforcePositioningPoseInPrompt(prompt, currentScript), nil
}

func writePositioningContinuityContext(b *strings.Builder, previousScripts, nextScripts []string) {
	if len(previousScripts) > 0 {
		b.WriteString("\n前面相邻分镜（从旧到新，含场景名；先筛选同场镜头，最多 12 镜）：\n")
		for i, script := range previousScripts {
			b.WriteString(fmt.Sprintf("—— 前镜 %d ——\n%s\n", i+1, strings.TrimSpace(script)))
		}
	}
	if len(nextScripts) > 0 {
		b.WriteString("\n后面相邻分镜（从近到远，仅用于校验连续性，不得提前后镜动作，最多 4 镜）：\n")
		for i, script := range nextScripts {
			b.WriteString(fmt.Sprintf("—— 后镜 %d ——\n%s\n", i+1, strings.TrimSpace(script)))
		}
	}
}

func positioningPoseCueFromScript(script string) string {
	script = strings.TrimSpace(script)
	if script == "" {
		return ""
	}
	hasSit := regexp.MustCompile(`坐着|落座|坐在|围坐|坐回|坐下`).MatchString(script)
	hasRise := regexp.MustCompile(`起身|站起|站起来|站着|站在|走近|门口`).MatchString(script)
	hasTable := regexp.MustCompile(`包厢|酒桌|餐桌|长桌|烧烤|举杯|敬酒|喝酒`).MatchString(script)
	switch {
	case hasSit && hasRise:
		return "文案同时有坐与起身：未写起身的人必须写「坐着」；只有明确起身/站立的人写起身或站着。禁止全员站立。"
	case hasSit:
		return "文案已写坐姿：出场人物默认「坐着」，禁止写成全员站立围桌。"
	case hasTable && !hasRise:
		return "酒局/包厢围桌且未写起身：出场人物默认「坐在桌边」，禁止全员站立；若有人举杯也先写坐着举杯，除非文案写起身。"
	case hasRise && !hasSit:
		return "文案以站立/起身为主：按文案写站着或起身即可。"
	default:
		return ""
	}
}

func enforcePositioningPoseInPrompt(prompt, script string) string {
	cue := positioningPoseCueFromScript(script)
	if cue == "" {
		return prompt
	}
	body, legend := splitPositioningPrompt(prompt)
	needSit := strings.Contains(cue, "坐") && !regexp.MustCompile(`坐着|坐在桌边|围坐`).MatchString(body)
	if !needSit {
		return prompt
	}
	inject := "人物姿态：未写起身者均坐在桌边椅凳上，禁止全员站立；仅文案起身者站立或半起身。"
	if body == "" {
		body = inject
	} else {
		body = inject + "\n" + body
	}
	return normalizePositioningPrompt(body+"\n\n"+legend, nil)
}

// normalizePositioningPrompt ensures the 参考图 legend sits on its own final line.
func normalizePositioningPrompt(prompt string, refLabels []string) string {
	body, legend := splitPositioningPrompt(prompt)
	if legend == "" && len(refLabels) > 0 {
		parts := make([]string, 0, len(refLabels))
		for i, label := range refLabels {
			label = strings.TrimSpace(label)
			label = strings.TrimPrefix(label, fmt.Sprintf("图%d：", i+1))
			label = strings.TrimPrefix(label, fmt.Sprintf("图%d:", i+1))
			label = strings.TrimSpace(label)
			if label == "" {
				continue
			}
			parts = append(parts, fmt.Sprintf("图%d为%s", i+1, label))
		}
		if len(parts) > 0 {
			legend = "参考图：" + strings.Join(parts, "，")
		}
	}
	body = strings.TrimSpace(body)
	legend = strings.TrimSpace(legend)
	if body == "" {
		return legend
	}
	if legend == "" {
		return body
	}
	return body + "\n\n" + legend
}

func splitPositioningPrompt(prompt string) (body, legend string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", ""
	}
	lines := strings.Split(prompt, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if lineRefLegendRe.MatchString(line) {
			legend = line
			body = strings.TrimSpace(strings.Join(lines[:i], "\n"))
			return body, legend
		}
		break
	}
	if m := inlineRefLegendRe.FindStringSubmatch(prompt); len(m) == 4 {
		body = strings.TrimSpace(m[1])
		legend = strings.TrimSpace(m[3])
		return body, legend
	}
	return prompt, ""
}

// PositioningFaceMosaicConstraint is always appended when generating 站位图.
// Faces stay fully mosaicked; names are schematic floating labels (not physical name tags).
const PositioningFaceMosaicConstraint = "【站位图固定要求】所有人物的面部必须打满马赛克，马赛克彻底完全遮住人脸，五官完全不可见；每个人物旁边用醒目、清晰、大号的中文名字做「示意图悬浮标注」（浮在人物身旁或上方，像分镜标注文字），不要做成衣服上的实体名牌、贴纸、号码布或缝在服装上的文字；不要水印、不要 logo、不要 UI 边框。"

const PositioningCastFromPrompt = "文案里的出场人物都要画进画面（含群演），每人只出现一次；禁止把次要角色画成焦点人物的分身或双胞胎。【坐站】严格按提示词的坐着/站着/起身执行：写坐着的人必须坐在椅凳或桌边，禁止改成全员站立；只有写起身/站着的人才站立。角色定妆参考图若是站姿全身，也只借鉴面容服装，姿势以提示词为准。"

const PositioningCastFromSkeleton = "人数必须与图1骨架一致：图1骨架上的每一个具名火柴人都必须一对一替换为同位置的一个真人，一个不能少、一个不能多，禁止只挑部分人物出画。每人只出现一次。图2起不得改变图1人数；角色定妆参考图若是站姿全身，只借鉴服装，禁止复制定妆图的站立姿势和白底构图。"

// PositioningPoseHint is appended when the shot script implies seated dining /
// lounge blocking so the image model does not default everyone to standing.
const PositioningPoseHint = "【姿态锁定】若上文写了「坐着」，对应人物必须坐姿出现在桌边/沙发/椅上；禁止把坐着的人画成站立围桌。起身举杯仅限文案明确起身的那一个人。"

// PositioningSkeletonConstraint is prepended on pass 2 so the photoreal 站位图
// follows the stick-figure layout instead of standing character sheets.
const PositioningSkeletonConstraint = "【骨架优先 · 最高优先级】图1是火柴人/线稿站位骨架。生成结果必须像把图1的火柴人换成真人：机位、桌子位置、谁在左/右/前/后、谁坐谁站、面朝哪边，全部按图1，不要按文字站位改图1。图2起只提供场景材质和人物五官服装。文字与图1冲突时以图1为准。"

var positioningMetaBlockRE = regexp.MustCompile(`(?m)^【(?:站位图固定要求|姿态锁定|骨架优先|骨架硬约束)】.*\n?`)
var figureNumberRE = regexp.MustCompile(`图(\d+)`)
var figure1ChunkRE = regexp.MustCompile(`图1为(.+?)(?:，图\d+为|$)`)

func withPositioningConstraints(prompt string) string {
	p := strings.TrimSpace(prompt)
	useSkeleton := positioningLooksLikeSkeletonGuide(p)
	mosaic := PositioningFaceMosaicConstraint
	if useSkeleton {
		mosaic += PositioningCastFromSkeleton
	} else {
		mosaic += PositioningCastFromPrompt
	}
	if p == "" {
		return mosaic
	}
	if strings.Contains(p, "【站位图固定要求】") {
		return p
	}
	out := p + "\n\n" + mosaic
	if !useSkeleton && positioningScriptImpliesSeated(p) && !strings.Contains(p, "【姿态锁定】") {
		out += "\n" + PositioningPoseHint
	}
	return out
}

var seatedPoseHintRE = regexp.MustCompile(`坐着|落座|坐在|围桌|餐桌|酒局|包厢.*酒|桌边`)

func positioningScriptImpliesSeated(prompt string) bool {
	return seatedPoseHintRE.MatchString(prompt)
}

// GeneratePositioningSkeletonCandidates draws a stick-figure blocking sketch.
// Optional referenceImages should be 1 scene plate / 9-grid cell (no character sheets).
func (s *ArkService) GeneratePositioningSkeletonCandidates(
	provider models.AIProvider,
	model models.AIModel,
	prompt string,
	referenceImages []string,
	count int,
	spec ImageGenSpec,
	onProgress ImageGenProgress,
) ([]string, string, error) {
	if ProviderRequiresAPIKey(provider) && provider.APIKey == "" {
		return nil, "", fmt.Errorf("请先在设置中心填写 API Key")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, "", fmt.Errorf("请填写站位图提示词")
	}
	if strings.TrimSpace(spec.Aspect) == "" {
		spec.Aspect = "16:9"
	}
	refs := referenceImages
	if len(refs) > 1 {
		refs = refs[:1]
	}
	skeletonPrompt := clampImagePrompt(buildPositioningStickFigurePrompt(prompt, spec.Aspect, len(refs) > 0), 1200)
	skeletonSpec := spec
	skeletonSpec.Resolution = "1k"
	count = normalizeImageCandidateCount(count)
	if count > 2 {
		count = 2
	}
	if onProgress != nil {
		msg := "正在生成火柴人站位骨架（不参考定妆图）…"
		if len(refs) > 0 {
			msg = "正在生成火柴人站位骨架（按场景底板描空间）…"
		}
		onProgress(0, count, msg)
	}
	urls, err := s.generateImageCandidates(provider, model, skeletonPrompt, refs, count, 2800, onProgress, skeletonSpec)
	if err != nil {
		return nil, skeletonPrompt, friendlyImageGenError(err)
	}
	return urls, skeletonPrompt, nil
}

// GeneratePositioningCandidates generates the photoreal 站位图. Callers should pass
// an approved stick-figure as 图1; this no longer auto-generates a skeleton.
func (s *ArkService) GeneratePositioningCandidates(
	provider models.AIProvider,
	model models.AIModel,
	prompt string,
	referenceImages []string,
	count int,
	spec ImageGenSpec,
	onProgress ImageGenProgress,
) ([]string, string, error) {
	if ProviderRequiresAPIKey(provider) && provider.APIKey == "" {
		return nil, "", fmt.Errorf("请先在设置中心填写 API Key")
	}
	prompt = withNoLogo(strings.TrimSpace(prompt))
	if prompt == "" {
		return nil, "", fmt.Errorf("请填写站位图提示词")
	}
	count = normalizeImageCandidateCount(count)
	if strings.TrimSpace(spec.Aspect) == "" {
		spec.Aspect = "16:9"
	}

	const maxPositioningRefs = 12
	refs := referenceImages
	if len(refs) > maxPositioningRefs {
		refs = refs[:maxPositioningRefs]
	}

	if positioningLooksLikeSkeletonGuide(prompt) && len(refs) > 0 {
		prompt = withPositioningSkeletonGuide(prompt)
		prompt, refs = prioritizePositioningReferenceInputs(prompt, refs)
	}

	prompt = clampImagePrompt(prompt, 1500)
	prompt = withPositioningConstraints(prompt)

	if onProgress != nil {
		onProgress(0, count, "按已确认的火柴人骨架生成正式站位图…")
	}
	urls, err := s.generateImageCandidates(provider, model, prompt, refs, count, 3000, onProgress, spec)
	if err != nil && isImageGenSoftFail(err) && len(refs) > 2 {
		trimmed := refs[:2]
		log.Printf("positioning gen soft-fail (%v), retrying with %d refs", err, len(trimmed))
		if onProgress != nil {
			onProgress(0, count, "参考图过多导致失败，正在减少参考图重试…")
		}
		urls, err = s.generateImageCandidates(provider, model, prompt, trimmed, count, 3100, onProgress, spec)
	}
	if err != nil && isImageGenSoftFail(err) && len(refs) > 0 {
		log.Printf("positioning gen soft-fail (%v), retrying text-only", err)
		if onProgress != nil {
			onProgress(0, count, "图生图失败，改为按文案直接生成站位图…")
		}
		fallback := prompt + "\n请仅根据以上文字描述绘制一张清晰的场景人物站位示意图，人物位置关系明确。"
		urls, err = s.generateImageCandidates(provider, model, fallback, nil, count, 3200, onProgress, spec)
	}
	if err != nil {
		return nil, prompt, friendlyImageGenError(err)
	}
	return urls, prompt, nil
}

var positioningExtraLegendRefRE = regexp.MustCompile(`，图(?:[3-9]|[1-9]\d+)为[^，。\n]*`)

func prioritizePositioningReferenceInputs(prompt string, refs []string) (string, []string) {
	// For a crowd layout, individual character sheets compete with the
	// skeleton and image models often render only a subset. Faces are fully
	// mosaicked here, so preserve geometry with 图1 skeleton + 图2 scene;
	// clothing remains controlled by the text prompt.
	body, _ := splitPositioningPrompt(prompt)
	if _, names := positioningSkeletonBlockingOnly(body); len(names) >= 3 && len(refs) > 2 {
		refs = refs[:2]
		prompt = positioningExtraLegendRefRE.ReplaceAllString(prompt, "")
	}
	return prompt, refs
}

func positioningLooksLikeSkeletonGuide(prompt string) bool {
	if strings.Contains(prompt, "【骨架优先】") {
		return true
	}
	_, legend := splitPositioningPrompt(prompt)
	if legendFigure1IsSkeleton(legend) {
		return true
	}
	return strings.Contains(prompt, "火柴人") && strings.Contains(prompt, "骨架")
}

func legendFigure1IsSkeleton(legend string) bool {
	m := figure1ChunkRE.FindStringSubmatch(legend)
	if len(m) < 2 {
		return false
	}
	label := m[1]
	return strings.Contains(label, "火柴人") || strings.Contains(label, "骨架") || strings.Contains(label, "线稿")
}

func withPositioningSkeletonGuide(prompt string) string {
	body, legend := splitPositioningPrompt(prompt)
	body = stripPositioningMetaBlocks(body)
	cast := ""
	if _, names := positioningSkeletonBlockingOnly(body); len(names) > 0 {
		cast = fmt.Sprintf("【最终人物清单】成图人物总数必须严格等于 %d 人，只能是：%s。图1中每个名字对应一个真人，禁止遗漏、合并、复制或增加人物。\n", len(names), strings.Join(names, "、"))
	}
	guided := PositioningSkeletonConstraint + "\n" + cast + body
	if strings.Contains(prompt, "【骨架优先】") || legendFigure1IsSkeleton(legend) {
		return normalizePositioningPrompt(guided+"\n\n"+legend, nil)
	}
	return normalizePositioningPrompt(guided+"\n\n"+prependSkeletonLegend(shiftFigureNumbers(legend, 1)), nil)
}

func buildPositioningStickFigurePrompt(prompt, aspect string, hasSceneRef bool) string {
	body, _ := splitPositioningPrompt(prompt)
	body = stripPositioningMetaBlocks(body)
	body = strings.TrimSpace(body)
	if body == "" {
		body = strings.TrimSpace(prompt)
	}
	aspect = strings.TrimSpace(aspect)
	if aspect == "" {
		aspect = "16:9"
	}
	blocking, names := positioningSkeletonBlockingOnly(body)
	if blocking == "" {
		blocking = body
	}
	castLock := "只画正文明确列出的具名人物，不画群演、不添加路人、不复制人物。"
	if len(names) > 0 {
		castLock = fmt.Sprintf("人物总数严格等于 %d 人，只能是：%s。每人恰好出现一次；禁止群演、厨役、路人和任何额外人形。", len(names), strings.Join(names, "、"))
	}
	if hasSceneRef {
		return fmt.Sprintf(`极简线稿，%s 构图，机位与透视必须对齐图1（不要擅自改成俯视鸟瞰，除非正文写了俯拍）。
图1是场景空间底板。只描主要结构和可行走地面：墙、门、窗、柱、灶台/桌案的大轮廓；省略食材、器皿、悬挂物、烟雾、纹理、光影和装饰细节，背景线必须细且淡。
先把可行走地面按透视映射为九宫格区域。人物只能落在地面或正文明确的椅凳上，绝不能站到、坐到或跨上灶台、案板、桌面、柜子、器皿等物体。九格“右中”表示右侧中景的可行走地面，不表示右边灶台表面。
画面人物只用统一比例的火柴人，黑色粗线；不要服装细节、不要五官、不要照片质感。每人旁边用清晰中文名标注。
%s
严格还原以下站位（位置、坐站、朝向、前后景），每人只出现一次：

%s

【骨架硬约束】空间布局按图1，人物脚底必须接触可行走地面；人数、坐站、左右严格按清单。所有动作冻结为站位关键帧：“走动”只画轻微迈步、双脚靠近地面，禁止奔跑、跳跃、腾空和夸张跨步。前景人物较大、后景人物较小。`, aspect, castLock, blocking)
	}
	return fmt.Sprintf(`纯白背景，极简线稿，%s 构图，平视机位（不要俯视鸟瞰，除非正文写了俯拍）。
画面只用火柴人/剪影轮廓表示人物，不要真实场景、不要服装细节、不要五官、不要马赛克、不要照片质感。
桌子只用简单矩形线框表示。每人旁边用清晰中文名标注。
%s
严格还原以下站位（位置、坐站、朝向、前后景），每人只出现一次：

%s

【骨架硬约束】人物脚底必须落在同一地面平面；“走动”冻结为轻微迈步，禁止奔跑、跳跃、腾空和夸张跨步。写坐着的人画坐姿，写站着/起身的人画成立姿；前景人物较大、后景人物较小。`, aspect, castLock, blocking)
}

var positioningBlockingPersonRE = regexp.MustCompile(`([\p{Han}A-Za-z0-9甲乙丙丁]{1,12})\s*[（(](左前|中前|右前|左中|中中|右中|左后|中后|右后)[）)]`)

func positioningSkeletonBlockingOnly(body string) (string, []string) {
	matches := positioningBlockingPersonRE.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return "", nil
	}
	lines := make([]string, 0, len(matches))
	names := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for i, match := range matches {
		if len(match) < 6 {
			continue
		}
		name := strings.TrimSpace(body[match[2]:match[3]])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
		poseEnd := len(body)
		if i+1 < len(matches) {
			poseEnd = matches[i+1][0]
		}
		pose := strings.Trim(body[match[1]:poseEnd], " ，,；;。\n\t")
		pose = strings.ReplaceAll(pose, "站着斜身走动", "站着，轻微迈步，双脚贴近地面")
		pose = strings.ReplaceAll(pose, "斜身走动", "站立，轻微迈步，双脚贴近地面")
		pose = strings.ReplaceAll(pose, "走动", "轻微迈步，双脚贴近地面")
		pose = strings.ReplaceAll(pose, "跑动", "站立预备姿态，双脚落地")
		slot := body[match[4]:match[5]]
		lines = append(lines, fmt.Sprintf("- %s(%s)%s。", name, slot, pose))
	}
	return strings.Join(lines, "\n"), names
}

func stripPositioningMetaBlocks(prompt string) string {
	out := positioningMetaBlockRE.ReplaceAllString(prompt, "")
	out = strings.ReplaceAll(out, NoLogoConstraint, "")
	return strings.TrimSpace(out)
}

func shiftFigureNumbers(s string, delta int) string {
	if strings.TrimSpace(s) == "" || delta == 0 {
		return s
	}
	return figureNumberRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := figureNumberRE.FindStringSubmatch(m)
		if len(sub) != 2 {
			return m
		}
		n, err := strconv.Atoi(sub[1])
		if err != nil {
			return m
		}
		return fmt.Sprintf("图%d", n+delta)
	})
}

func prependSkeletonLegend(legend string) string {
	rest := strings.TrimSpace(legend)
	rest = strings.TrimPrefix(rest, "参考图：")
	rest = strings.TrimPrefix(rest, "参考图:")
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "，"))
	if rest == "" {
		return "参考图：图1为火柴人站位骨架"
	}
	return "参考图：图1为火柴人站位骨架，" + rest
}

func clampImagePrompt(prompt string, maxRunes int) string {
	r := []rune(strings.TrimSpace(prompt))
	if maxRunes <= 0 || len(r) <= maxRunes {
		return string(r)
	}
	return string(r[:maxRunes]) + "…"
}

func isImageGenSoftFail(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "did not return an image") ||
		strings.Contains(msg, "revise the prompt") ||
		strings.Contains(msg, "模型未返回图片") ||
		(strings.Contains(msg, "content") && strings.Contains(msg, "polic")) ||
		strings.Contains(msg, "safety")
}

func friendlyImageGenError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if isImageGenSoftFail(err) {
		return fmt.Errorf("图像服务未返回图片（提示词可能过长、参考图过多或内容被拦截）。已自动尝试减少参考图/纯文生图仍失败，请精简站位文案或减少参考图后重试。原始错误：%s", msg)
	}
	return err
}
