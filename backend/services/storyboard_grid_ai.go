package services

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"novaly/backend/models"
)

// sceneGridTemplate is the fixed 9-angle camera-matrix template for scene grids.
// Slot: %s = 空间主体（场景名 + 描述）。Angle labels must stay in SceneGridAngles order.
const sceneGridTemplate = `输出一张 3×3 九宫格拼图：3行3列共9格，细窄深色分隔线，整体画面 16:9，空镜无人。
【空间主体】
%s
【九格机位必须一眼能分开】参考图/图1只提供这个房间的材质、家具、酒食和光线，禁止九格都复刻参考图的取景。只有格2允许接近参考图构图。
格1 第一行左 正面全景：平视、站得远。必须同时入画整张长桌/床/柜台、围合座位、门口。比参考图更远。
格2 第一行中 正面近景：平视推近主活动区桌面。这是唯一允许接近参考图构图的一格。
格3 第一行右 侧面全景：平视贴侧墙。桌子变成纵深一条线，尽头是另一面墙，不要正对门拍成格1。
格4 第二行左 侧面近景：平视侧向中近景，拍座位侧面和侧墙。
格5 第二行中 背面全景：平视反打，站到格1对面拍回来。格1里远处的门在这一格要靠近镜头或落到画面边缘。
格6 第二行右 背面近景：反打中近景，靠近背面的门或墙。
格7 第三行左 俯视全景：真实摄影机在天花板高度垂直往下拍的写实空镜。必须看见地板材质与家具立体顶面，桌子是俯视矩形，瓶口朝镜头。禁止平视。禁止白底黑线 CAD、建筑平面图、线稿示意图。
格8 第三行中 俯视近景：真实摄影机更近的写实俯拍，只拍桌面/地面局部，盘子是圆的。禁止 CAD 平面图、禁止线稿。
格9 第三行右 斜向高位总览：约45度斜上方写实空镜，同时看见桌面和一面墙。禁止平面图。
硬性：第三行（格7格8格9）必须是写实摄影俯视/高位机位，镜头高度明显高于前两行；若第三行出现白底黑线平面图、CAD、线稿布局图，或仍是平视桌面，就算失败。九格家具材质相同，但机位、远近、朝向必须不同。
画面内不要文字、标注、水印或logo。严禁出现人物、人影或剪影。`

// BuildSceneGridPrompt renders the default (editable) scene 9-grid prompt.
func BuildSceneGridPrompt(name, description, style string) string {
	subject := strings.TrimSpace(name)
	if d := strings.TrimSpace(description); d != "" {
		if subject != "" {
			subject += "："
		}
		subject += d
	}
	if subject == "" {
		subject = "（待补充场景名称与描述）"
	}
	prompt := fmt.Sprintf(sceneGridTemplate, subject)
	if style = strings.TrimSpace(style); style != "" {
		prompt += "\n【整体画面质感】\n" + style
	}
	return prompt
}

var (
	sceneGridSubjectRE = regexp.MustCompile(`(?s)【(?:建筑|空间)主体】\s*(.*?)(?:\n【|\z)`)
	sceneGridStyleRE   = regexp.MustCompile(`(?s)【整体画面质感】\s*(.*)`)
)

// LooksLikeLegacySceneGridPrompt is the old architectural-exterior 9-grid template.
func LooksLikeLegacySceneGridPrompt(prompt string) bool {
	return strings.Contains(prompt, "同一建筑连续摄影") ||
		strings.Contains(prompt, "ArchViz") ||
		strings.Contains(prompt, "屋顶结构") ||
		strings.Contains(prompt, "山体结构") ||
		strings.Contains(prompt, "【建筑主体】")
}

// NeedsSceneGridPromptRefresh is true for facade templates and the previous
// interior-orbit text that Seedream still treated as nine copies of the ref.
func NeedsSceneGridPromptRefresh(prompt string) bool {
	if LooksLikeLegacySceneGridPrompt(prompt) {
		return true
	}
	// Current matrix already bans CAD in overhead cells.
	if strings.Contains(prompt, "瓶口朝镜头") && strings.Contains(prompt, "格7") &&
		(strings.Contains(prompt, "禁止白底黑线") || strings.Contains(prompt, "禁止 CAD") || strings.Contains(prompt, "建筑平面图")) {
		return false
	}
	// Older matrix had 格7 but still let Seedream paste the floor-plan CAD into overhead cells.
	if strings.Contains(prompt, "瓶口朝镜头") && strings.Contains(prompt, "格7") {
		return true
	}
	return strings.Contains(prompt, "【九宫格摄影机矩阵】") ||
		strings.Contains(prompt, "房间内部绕主活动区") ||
		strings.Contains(prompt, "同一空间连续摄影")
}

// NormalizeSceneGridPrompt rewrites stale 9-grid templates into the current camera matrix,
// keeping the scene subject and project style.
func NormalizeSceneGridPrompt(prompt, name, style string) string {
	prompt = strings.TrimSpace(prompt)
	name = strings.TrimSpace(name)
	style = strings.TrimSpace(style)
	if prompt == "" {
		return BuildSceneGridPrompt(name, "", style)
	}
	if !NeedsSceneGridPromptRefresh(prompt) {
		return prompt
	}

	subject := name
	if m := sceneGridSubjectRE.FindStringSubmatch(prompt); len(m) > 1 {
		raw := strings.TrimSpace(m[1])
		for _, marker := range []string{"同一建筑连续摄影", "同一空间连续摄影"} {
			if i := strings.Index(raw, marker); i >= 0 {
				raw = strings.TrimSpace(raw[:i])
			}
		}
		if raw != "" {
			subject = raw
		}
	}

	sceneName := name
	desc := ""
	if i := strings.Index(subject, "："); i > 0 {
		left := strings.TrimSpace(subject[:i])
		right := strings.TrimSpace(subject[i+len("："):])
		if sceneName == "" {
			sceneName = left
		}
		desc = right
	} else if sceneName == "" {
		sceneName = subject
	} else if subject != sceneName {
		if strings.HasPrefix(subject, sceneName) {
			desc = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(subject, sceneName), "："))
		} else {
			desc = subject
		}
	}
	if style == "" {
		if m := sceneGridStyleRE.FindStringSubmatch(prompt); len(m) > 1 {
			style = strings.TrimSpace(m[1])
			if i := strings.Index(style, "【"); i >= 0 {
				style = strings.TrimSpace(style[:i])
			}
		}
	}
	return BuildSceneGridPrompt(sceneName, desc, style)
}

// MotionGridLockConstraint is always appended to 9-frame motion-grid prompts so all
// 9 cells stay mutually consistent; text annotations are banned to keep cells usable
// as video references.
const MotionGridLockConstraint = "【连续镜头锁定】本图为一张 3×3 网格拼图，9个格子是【同一个镜头】按时间先后排列的连续画面（从左到右、从上到下依次为第1帧至第9帧），格与格之间用细窄深色分隔线，每格画面比例 16:9，整体画面比例 16:9。全部9格中：人物的面容、发型、服装、体型、武器与人数完全相同，场景、光照、天气、时间段完全相同；禁止更换人物，禁止改变服装与道具，禁止改变场景布局与建筑风格；禁止路人、群演、同款分身或双胞胎；仅允许时间推进、动作连续发展与摄影机连续运动；格间动作必须连贯、无跳切。格子内不要任何文字、时间码、水印、logo 或 UI 边框。"

// SceneGridFloorPlanRefConstraint is appended when the user confirmed a CAD floor plan
// as a spatial lock ref. Seedream otherwise pastes the line drawing into 格7/8.
const SceneGridFloorPlanRefConstraint = "【平面图仅锁方位·禁止成片】若参考图含白底黑线二维建筑平面布局图/CAD，它只提供门、墙、家具、通道的平面相对位置，禁止把该线稿样式画进九宫格任何一格。格7、格8必须是真实摄影机俯拍写实空镜（可见地板材质与家具立体顶面），格9为斜向高位写实；严禁 CAD、平面图、线稿示意图出现在任何一格。"

func withSceneGridFloorPlanRefConstraint(prompt string) string {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return p
	}
	if !strings.Contains(p, "二维建筑平面布局图") && !strings.Contains(p, "【二维平面布局图约束】") && !strings.Contains(p, "俯视布局线稿") {
		return p
	}
	if strings.Contains(p, "【平面图仅锁方位·禁止成片】") {
		return p
	}
	return p + "\n" + SceneGridFloorPlanRefConstraint
}

func withMotionGridConstraints(prompt string) string {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return MotionGridLockConstraint
	}
	if strings.Contains(p, "【连续镜头锁定】") {
		return p
	}
	return p + "\n\n" + MotionGridLockConstraint
}

// VideoMotionGridConstraint tells the video model how to read a 9-frame motion-grid reference.
const VideoMotionGridConstraint = "【最高优先级·9帧时序网格】参考图中若出现 3×3 网格图，它是本镜头的「9帧连续分镜」（从左上到右下按时间先后依次为格1至格9）。视频必须严格按该网格的时序、人物站位、动作节拍、机位方向与构图推进：把格1作为开场画面、格9作为收尾画面，中间格依次衔接；不得跳格、倒序、自由发挥。人物面容、服装、人数、场景布局须与网格保持完全一致，禁止路人、分身；网格分隔线不得出现在成片。若其它参考图与网格冲突，以网格为准。"

// HasMotionGridVideoRef reports whether any video ref is a 9-frame motion grid.
func HasMotionGridVideoRef(input VideoInput) bool {
	for _, r := range input.Refs {
		if strings.EqualFold(strings.TrimSpace(r.Resource.GenType), "motion_grid") {
			return true
		}
	}
	return false
}

// AnalyzeMotionGrid asks the text model to write an editable 9-frame (3×3 temporal grid)
// prompt for one shot, anchored to the previous shot's outro pose when provided.
func (s *ArkService) AnalyzeMotionGrid(
	provider models.AIProvider,
	model models.AIModel,
	currentScript string,
	previousScripts []string,
	style string,
	refLabels []string,
	prevOutroHint string,
) (string, error) {
	currentScript = strings.TrimSpace(currentScript)
	if currentScript == "" {
		return "", fmt.Errorf("当前分镜文案为空")
	}
	if ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return "", fmt.Errorf("文本模型服务商未配置 API Key")
	}

	var b strings.Builder
	b.WriteString(`你是专业电影分镜导演兼动作指导（武指）。请根据「当前分镜文案」以及「前面分镜」的连续性，撰写一段用于 AI 图生图的「9帧连续画面网格」提示词，适用于武打、多人调度等对连贯性要求极高的镜头。

目标：生成一张 3×3 网格图，9个格子是【同一个镜头】从开场到结尾按时间先后排列的9个关键画面（从左到右、从上到下依次为格1至格9）。

写作要求：
1. 直接输出可交给图像模型的中文提示词正文，不要 JSON，不要 Markdown 标题，不要解释过程。
2. 正文必须包含以下两个部分，顺序固定：
   - 【9帧时序】逐格描述，每格一行，格式为「格N（节拍名）：画面描述」：
     · 格1（起势）：开场态势，用九格站位写清每个人物的位置与朝向（左前/右中等，3/4正面朝左/朝右）
     · 格2～格8：按文案的时间轴/动作节拍推进，覆盖逼近、交锋、格挡、位移、反击、高潮等关键动作瞬间；相邻格的动作必须连续、可信，无跳切；人物左右格子同场锁死，换位须有走位
     · 格9（收势）：本镜结束时的定格姿态，人物九格位置与朝向要明确稳定（它将作为下一镜头的开场）
   - 【摄影机】本镜头采用单一连续运镜（从固定、缓慢推进、环绕、跟拍、升降中选一种并写明方向与幅度），统一焦段、统一曝光、统一白平衡、统一色彩风格。
3. 环境、光影、氛围与每个出场人物的外观，须与前面分镜保持一致；只描述本镜需要的动作与调度变化。
4. 正文控制在 450～800 字，具体可执行，避免空泛形容词堆砌。
5. 空一行后，单独一行写出参考图对应关系，格式固定为：
   参考图：图1为××，图2为××，图3为××
6. 系统会另行强制「连续镜头锁定」与「禁止文字标注」约束，正文中不要重复这些要求，也不要要求露脸特写或文字标注。
`)
	if style = strings.TrimSpace(style); style != "" {
		b.WriteString("\n项目画面质感：")
		b.WriteString(style)
		b.WriteString("\n")
	}
	if prevOutroHint = strings.TrimSpace(prevOutroHint); prevOutroHint != "" {
		b.WriteString("\n跨镜衔接要求（最高优先级）：")
		b.WriteString(prevOutroHint)
		b.WriteString("\n格1（起势）必须严格承接该收势姿态：相同的人物位置、朝向、姿态与场面状态，仅时间继续推进。\n")
	}
	if len(refLabels) > 0 {
		b.WriteString("\n参考图标签（按顺序，请据此写最后一行「参考图：…」）：\n")
		for i, label := range refLabels {
			b.WriteString(fmt.Sprintf("- 图%d：%s\n", i+1, label))
		}
	}
	if len(previousScripts) > 0 {
		b.WriteString("\n前面分镜（从旧到新，含场景名，仅作连续性参考，最多 10 镜）：\n")
		for i, script := range previousScripts {
			b.WriteString(fmt.Sprintf("—— 前镜 %d ——\n%s\n", i+1, strings.TrimSpace(script)))
		}
	}
	b.WriteString("\n当前分镜文案：\n")
	b.WriteString(currentScript)

	content, err := s.chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.4,
		"max_tokens":  3000,
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
		return "", fmt.Errorf("大模型未返回9帧文案")
	}
	return normalizePositioningPrompt(prompt, refLabels), nil
}

// GenerateSceneGridCandidates generates a scene 9-grid (camera matrix) from an editable prompt + refs.
func (s *ArkService) GenerateSceneGridCandidates(
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
		return nil, "", fmt.Errorf("请填写9宫格提示词")
	}
	prompt = NormalizeSceneGridPrompt(prompt, "", "")
	prompt = withNoLogo(prompt)
	prompt = withSceneEmptyConstraint(prompt)
	prompt = withSceneGridFloorPlanRefConstraint(prompt)
	prompt = clampImagePrompt(prompt, 2600)
	count = normalizeImageCandidateCount(count)
	spec.Aspect = "16:9"

	const maxRefs = 8
	refs := referenceImages
	if len(refs) > maxRefs {
		refs = refs[:maxRefs]
	}

	urls, err := s.generateImageCandidates(provider, model, prompt, refs, count, 4000, onProgress, spec)
	if err != nil && isImageGenSoftFail(err) && len(refs) > 1 {
		log.Printf("scene grid gen soft-fail (%v), retrying with 1 ref", err)
		if onProgress != nil {
			onProgress(0, count, "参考图过多导致失败，正在减少参考图重试…")
		}
		urls, err = s.generateImageCandidates(provider, model, prompt, refs[:1], count, 4100, onProgress, spec)
	}
	if err != nil && isImageGenSoftFail(err) && len(refs) > 0 {
		log.Printf("scene grid gen soft-fail (%v), retrying text-only", err)
		if onProgress != nil {
			onProgress(0, count, "图生图失败，改为按文案直接生成9宫格…")
		}
		urls, err = s.generateImageCandidates(provider, model, prompt, nil, count, 4200, onProgress, spec)
	}
	if err != nil {
		return nil, prompt, friendlyImageGenError(err)
	}
	return urls, prompt, nil
}

// GenerateMotionGridCandidates generates a 9-frame continuous-motion grid for one shot.
func (s *ArkService) GenerateMotionGridCandidates(
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
		return nil, "", fmt.Errorf("请填写9帧图提示词")
	}
	// Keep room for the fixed lock constraint at the end.
	prompt = clampImagePrompt(prompt, 2200)
	prompt = withMotionGridConstraints(prompt)
	count = normalizeImageCandidateCount(count)
	spec.Aspect = "16:9"

	// Ref 1 is expected to be the previous shot's outro frame when present.
	const maxRefs = 12
	refs := referenceImages
	if len(refs) > maxRefs {
		refs = refs[:maxRefs]
	}

	urls, err := s.generateImageCandidates(provider, model, prompt, refs, count, 5000, onProgress, spec)
	if err != nil && isImageGenSoftFail(err) && len(refs) > 2 {
		trimmed := refs[:2]
		log.Printf("motion grid gen soft-fail (%v), retrying with %d refs", err, len(trimmed))
		if onProgress != nil {
			onProgress(0, count, "参考图过多导致失败，正在减少参考图重试…")
		}
		urls, err = s.generateImageCandidates(provider, model, prompt, trimmed, count, 5100, onProgress, spec)
	}
	if err != nil && isImageGenSoftFail(err) && len(refs) > 0 {
		log.Printf("motion grid gen soft-fail (%v), retrying text-only", err)
		if onProgress != nil {
			onProgress(0, count, "图生图失败，改为按文案直接生成9帧图…")
		}
		fallback := prompt + "\n请仅根据以上文字描述绘制一张 3×3 的9帧连续画面网格，同一镜头、动作连贯。"
		urls, err = s.generateImageCandidates(provider, model, fallback, nil, count, 5200, onProgress, spec)
	}
	if err != nil {
		return nil, prompt, friendlyImageGenError(err)
	}
	return urls, prompt, nil
}

// AnalyzeSceneGridShapeLegend asks the text model to draft a per-scene CAD plan
// shape legend from the scene name and description (furniture, piles, fixtures, etc.).
func (s *ArkService) AnalyzeSceneGridShapeLegend(
	provider models.AIProvider,
	model models.AIModel,
	sceneName string,
	sceneDescription string,
) (string, error) {
	sceneName = strings.TrimSpace(sceneName)
	sceneDescription = strings.TrimSpace(sceneDescription)
	if sceneName == "" && sceneDescription == "" {
		return "", fmt.Errorf("场景名称与描述均为空")
	}
	if ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return "", fmt.Errorf("文本模型服务商未配置 API Key")
	}

	var b strings.Builder
	b.WriteString(`你是建筑制图与影视场景美术指导。请根据「场景名称」和「场景描述」，为一张 CAD 风格正交俯视二维建筑平面布局图撰写「图形语义对照表」。

用途：图像模型会先画平面图（只有顶视轮廓符号），后续 9 宫格机位必须按此对照表解读图2中每个形状代表什么物体。

写作要求：
1. 直接输出对照表正文，不要 JSON，不要 Markdown 标题，不要解释过程。
2. 第一行固定为：图2为人工确认二维建筑平面布局图，图片本身无任何文字。
3. 第二行固定为：请按以下形状语义解读图2：
4. 从第三行起输出编号列表，每条格式「N. 形状符号描述 = 对应物体/区域（补充说明）」。
5. 必须包含以下通用结构项（编号靠前）：
   - 外框粗黑线 = 房间/空间墙体边界
   - 墙体缺口 = 门洞开口
   - 门洞旁扇形弧线 = 门扇开启方向
   - 留白窄带区域 = 人行通道
6. 根据场景描述，补充该场景特有的家具、陈设、堆放物、固定装置（如灶台、货架、柴堆、沙发、床、柜台、水池、柱子等）。只写描述里出现或合理推断存在的物体，不要套用无关模板。
7. 所有物体只描述「从上往下看的顶面轮廓符号」，不写立面、不写体积、不写透视；不规则块状/交叉短线可表示堆放物。
8. 同类物体可合并一条（如「矩形/圆角矩形 = 木桌、矮柜顶面」），但不同场景要用不同具体名称（御膳房写灶台/案板，不是泛写木桌除非描述如此）。
9. 控制在 6～14 条，中文，简洁可执行。
`)
	if sceneName != "" {
		b.WriteString("\n场景名称：\n")
		b.WriteString(sceneName)
		b.WriteString("\n")
	}
	if sceneDescription != "" {
		b.WriteString("\n场景描述：\n")
		b.WriteString(sceneDescription)
		b.WriteString("\n")
	}

	content, err := s.chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.35,
		"max_tokens":  1200,
		"messages": []map[string]string{
			{"role": "user", "content": b.String()},
		},
	})
	if err != nil {
		return "", err
	}
	legend := normalizeSceneGridShapeLegend(content)
	if legend == "" {
		return "", fmt.Errorf("大模型未返回图形语义对照表")
	}
	return legend, nil
}

func normalizeSceneGridShapeLegend(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimPrefix(raw, "markdown")
	raw = strings.TrimPrefix(raw, "text")
	raw = strings.TrimSpace(strings.Trim(raw, "`"))
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "图2为人工确认") {
		raw = "图2为人工确认二维建筑平面布局图，图片本身无任何文字。\n请按以下形状语义解读图2：\n" + raw
	}
	return strings.TrimSpace(raw)
}
