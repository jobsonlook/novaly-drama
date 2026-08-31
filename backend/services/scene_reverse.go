package services

import (
	"fmt"
	"strings"

	"novaly/backend/models"
)

func sceneReverseCleanSubject(name, description, cameraToken string) string {
	subject := strings.TrimSpace(name)
	d := StripImageRefLegend(strings.TrimSpace(description))
	if d == "" || strings.Contains(d, cameraToken) || strings.Contains(d, "参考图：") || strings.Contains(d, "【") || strings.Contains(d, "图1") || strings.Contains(d, "图2") {
		if subject == "" {
			return "当前场景"
		}
		return subject
	}
	runes := []rune(d)
	if len(runes) > 80 {
		if subject == "" {
			return "当前场景"
		}
		return subject
	}
	if subject != "" {
		return subject + "：" + d
	}
	return d
}

// BuildSceneReverseSkeletonPrompt draws the reverse shot itself as a stick-figure frame.
func BuildSceneReverseSkeletonPrompt(name, description string) string {
	subject := sceneReverseCleanSubject(name, description, "这张线稿就是反打")
	return fmt.Sprintf(`【这张线稿就是反打镜头】输出必须是反打机位「正在拍摄到的画面」：平视分镜线稿，16:9，白底黑线火柴人。禁止俯视平面图，禁止鸟瞰，禁止在一张图里画两个机位、轴线箭头、A正打/B反打图标。
【线稿硬锁 · 最高优先级】禁止照片、禁止真实皮肤五官、禁止服装花纹、禁止灯光材质。画成实拍或俯视示意图视为失败。
图1是原镜头（正打）。先看图1谁在近处、谁在远处、谁正对镜头、门在哪，然后把摄影机挪到桌子对面那一端（图1里远处、正对镜头的人背后），回头看向原镜头。
若还有图2俯视格或9宫格：只用来确认桌子、沙发、门在房间哪一端，线稿仍必须是平视反打画面。禁止画成俯视，禁止画成九宫格拼图。
【必须四角换位】摄影机水平旋转180度后，图1近处、背对镜头的人，线稿里必须变成远处、正对镜头；图1远处、正对镜头的人，必须变成近处、背影或过肩。屏幕方位也必须交叉：图1「近左→远右、近右→远左、远左→近右、远右→近左」。这是四角换位，不是照抄图1，也不是只做水平镜像。
【必须画人】图1有几个人就画几个火柴人，坐着画坐姿，站着/举杯画立姿。逐人跟踪身份，按上述四角规则移位；能辨认的人用中文名标注，不得把姓名留在图1的原位置。
图1里靠近镜头的门/门框，线稿里应画在远处背景，且屏幕左右也随180度反打变换。
房间用简单线框：桌椅卡座、门、窗。不要照片人物、不要定妆照。
【骨架硬约束】输出前逐人核对：图1远处正脸者现在必须在前景背影/过肩；图1近处背影者现在必须在对面正脸；四人场景必须四个角全部换位。任何人仍留在图1的前后/左右原位，都视为失败。
【空间】%s`, subject)
}

// SceneReverseRefLegend is prepended for reverse photoreal jobs.
const SceneReverseRefLegend = "参考图：图1为反打镜头线稿（唯一的构图、人物位置和姓名标注依据，把火柴人换成真人），图2为原镜头/人物（只锁房间、五官服装；禁止抄图2机位，禁止复制图2的姓名文字和马赛克位置），其后若有俯视格只锁桌子/沙发/门的平面位置（禁止画成俯视），若有反打一侧空镜（背面全景等）只锁那一侧房间结构，其余为角色定妆。\n按图号引用上方参考图，不要弄混。"

// SceneReverseAnnotationConstraint makes the reverse plate usable as a blocking/reference image,
// matching the face privacy and cast labeling rules used by positioning images.
const SceneReverseAnnotationConstraint = "【反打图固定要求 · 姓名只认图1】所有人物的面部必须打满马赛克，马赛克彻底完全遮住人脸，五官完全不可见。人物身份、姓名以及姓名所在的人物位置，只能读取图1反打线稿；图2中已有的姓名文字、马赛克及其位置全部忽略，禁止照抄。每个人只出现一次姓名，姓名紧贴该人物头顶或肩旁；禁止同一姓名出现两次，禁止一个人旁边出现两个姓名，禁止把远处人物姓名放到近处人物身上。输出前逐人核对：人物数=马赛克数=姓名数，且每个姓名唯一并与图1对应。姓名做醒目、清晰、大号的中文「示意图悬浮标注」，不要做成衣服上的实体名牌、贴纸、号码布或缝在服装上的文字；除这组人物姓名外，不要其他文字、水印、logo 或 UI 边框。"

// BuildSceneReversePrompt turns the reverse line drawing into a photoreal reverse shot.
func BuildSceneReversePrompt(name, description string) string {
	subject := sceneReverseCleanSubject(name, description, "把图1的火柴人换成")
	return fmt.Sprintf(`【骨架优先 · 最高优先级】图1是反打镜头的火柴人线稿，这就是成片要拍到的画面。生成结果必须像把图1的火柴人换成真人：机位、透视、谁在前景/后景、谁正脸/背影/过肩、门在近还是远，全部按图1，不要按图2构图。
【参考图】图1=反打线稿（唯一的构图、人物位置、姓名依据），图2=原镜头/人物（只锁房间、五官服装，禁止复制图2里的姓名文字和马赛克位置），俯视格只锁平面布局（禁止抄俯视），反打一侧空镜只锁对面看到的房间（门、沙发、墙），禁止按空镜里的无人构图摆人。其余=角色定妆。
禁止再拍成图2那个机位。如果成片里仍是图2那种「门口往里拍、近处背影、远处正脸」，视为失败。
按图1每个火柴人旁的姓名，把它替换成对应人物定妆参考中的真人；若图2的旧姓名位置与图1冲突，绝对以图1为准。禁止换人、加人、少人。房间材质优先按反打一侧空镜，空镜没有的细节再按图2。
%s
遵守轴线：不要左右翻转图1。
【空间】%s`, SceneReverseAnnotationConstraint, subject)
}

func withSceneReverseSkeletonLineArt(prompt string) string {
	p := strings.TrimSpace(prompt)
	lock := "【线稿硬锁 · 最高优先级】必须输出平视的反打镜头线稿：白底黑线火柴人。禁止俯视平面图，禁止画两个机位，禁止照片。这张图就是反打机位看到的画面。若有俯视格或9宫格，只用来看桌子沙发门的平面位置，禁止把线稿画成俯视或九宫格。"
	if strings.Contains(p, "【线稿硬锁") {
		return p
	}
	return lock + "\n" + p
}

func withSceneReverseSkeletonGuide(prompt string) string {
	p := strings.TrimSpace(prompt)
	guide := "【反打优先】图1是反打镜头线稿。把图1火柴人换成图2里的真人，机位完全按图1；图2只提供五官服装，禁止抄图2构图。俯视格只锁平面；反打一侧空镜只锁对面房间结构，禁止按空镜摆人。"
	if strings.Contains(p, "【反打优先】") {
		return p
	}
	return guide + "\n" + p
}

// OppositeSceneGridCell maps a 9-grid camera cell to the reverse-side cell.
// 正面↔背面; 侧面 defaults to 背面全景.
func OppositeSceneGridCell(cell int) int {
	switch cell {
	case 1:
		return 5
	case 2:
		return 6
	case 3, 4:
		return 5
	case 5:
		return 1
	case 6:
		return 2
	default:
		return 5
	}
}

func sceneReverseSkeletonNeedsRebuild(prompt string) bool {
	p := strings.TrimSpace(prompt)
	if !strings.Contains(p, "这张线稿就是反打") {
		return true
	}
	if !strings.Contains(p, "不要照片人物") {
		return true
	}
	if !strings.Contains(p, "俯视格") {
		return true
	}
	if !strings.Contains(p, "必须四角换位") {
		return true
	}
	if strings.Contains(p, "俯视平面图") {
		return false
	}
	if strings.Contains(p, "俯视") && strings.Contains(p, "机位A") {
		return true
	}
	return false
}

func sceneReversePhotorealNeedsRebuild(prompt string) bool {
	p := strings.TrimSpace(prompt)
	if strings.Contains(p, "空镜无人") {
		return true
	}
	if !strings.Contains(p, "把图1") || !strings.Contains(p, "火柴人换成") {
		return true
	}
	if !strings.Contains(p, "骨架优先") {
		return true
	}
	if !strings.Contains(p, "俯视格") {
		return true
	}
	if !strings.Contains(p, "反打一侧") {
		return true
	}
	if strings.Contains(p, "俯视线稿") {
		return true
	}
	if !strings.Contains(p, "禁止换人") {
		return true
	}
	if !strings.Contains(p, "【反打图固定要求") {
		return true
	}
	if !strings.Contains(p, "姓名只认图1") {
		return true
	}
	if strings.Contains(p, "参考图：图1为") && !strings.Contains(p, "反打镜头线稿") && !strings.Contains(p, "反打空间线稿") {
		return true
	}
	return false
}

// GenerateSceneReverseSkeletonCandidates draws a reverse-angle stick-figure frame.
// referenceImages[0] should be the original scene plate; [1] may be a 9-grid overhead cell.
func (s *ArkService) GenerateSceneReverseSkeletonCandidates(
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
		return nil, "", fmt.Errorf("请填写反打线稿提示词")
	}
	if sceneReverseSkeletonNeedsRebuild(prompt) {
		prompt = BuildSceneReverseSkeletonPrompt("", prompt)
	}
	prompt = withSceneReverseSkeletonLineArt(prompt)
	refs := referenceImages
	if len(refs) == 0 {
		return nil, "", fmt.Errorf("请先选择场景原图作为参考，线稿必须按原图描反打机位")
	}
	if len(refs) > 2 {
		refs = refs[:2]
	}
	spec.Aspect = "16:9"
	if strings.TrimSpace(spec.Resolution) == "" {
		spec.Resolution = "1k"
	}
	count = normalizeImageCandidateCount(count)
	if count > 2 {
		count = 2
	}
	prompt = clampImagePrompt(prompt, 1400)
	if onProgress != nil {
		onProgress(0, count, "正在生成反打线稿（平视，对调前后景）…")
	}
	urls, err := s.generateImageCandidates(provider, model, prompt, refs, count, 4300, onProgress, spec)
	if err != nil {
		return nil, prompt, friendlyImageGenError(err)
	}
	return urls, prompt, nil
}

// GenerateSceneReverseCandidates draws the photoreal reverse shot from the reverse line drawing.
// Callers should pass the approved reverse line drawing as 图1 and the original scene as 图2.
// A 9-grid overhead cell may follow so the model can lock table/door layout without copying the original camera.
func (s *ArkService) GenerateSceneReverseCandidates(
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
		return nil, "", fmt.Errorf("请填写反打图提示词")
	}
	if !strings.Contains(prompt, "【反打优先】") {
		prompt = withSceneReverseSkeletonGuide(prompt)
	}
	if sceneReversePhotorealNeedsRebuild(prompt) {
		prompt = withSceneReverseSkeletonGuide(BuildSceneReversePrompt("", StripImageRefLegend(prompt)))
	}
	prompt = StripImageRefLegend(prompt)
	if !strings.Contains(prompt, "反打镜头线稿") && !strings.Contains(prompt, "反打空间线稿") {
		prompt = PrependImageRefLegend(prompt, SceneReverseRefLegend)
	}
	prompt = withNoLogo(prompt)
	prompt = clampImagePrompt(prompt, 1600)
	count = normalizeImageCandidateCount(count)
	if strings.TrimSpace(spec.Aspect) == "" {
		spec.Aspect = "16:9"
	}
	refs := referenceImages
	if len(refs) < 2 {
		return nil, "", fmt.Errorf("请同时提供反打线稿（图1）和场景/人物原图（图2）")
	}
	if len(refs) > 8 {
		refs = refs[:8]
	}
	if onProgress != nil {
		onProgress(0, count, "正在按反打线稿生成成片…")
	}
	urls, err := s.generateImageCandidates(provider, model, prompt, refs, count, 4400, onProgress, spec)
	if err != nil {
		return nil, prompt, friendlyImageGenError(err)
	}
	return urls, prompt, nil
}
