package services

import (
	"fmt"
	"log"
	"strings"

	"novaly/backend/models"
)

// ScenePanoramaRefLegend explains reference roles when unwrapping a 9-grid into 360°.
const ScenePanoramaRefLegend = `参考图分工（按出现顺序）：
图1=场景正面底板（master：风格、材质、灯光、主活动区）。
其后优先用已切分的九宫格机位：正面全景格、背面全景格、俯视全景格、侧面全景格；
若有反打空镜，只补后半球结构。
若有二维建筑平面布局图，只锁门窗桌椅方位，禁止把全景画成白底黑线 CAD。
禁止把参考图的文字、标注、九宫格分隔线画进成片。`

// BuildScenePanoramaPrompt builds a 9-grid-first equirectangular prompt.
func BuildScenePanoramaPrompt(name, description string) string {
	subject := strings.TrimSpace(name)
	if subject == "" {
		subject = "当前场景"
	}
	desc := strings.TrimSpace(StripImageRefLegend(description))
	if desc == "" {
		desc = subject
	}
	return fmt.Sprintf(`把该场景已有的「九宫格机位」展开成一张精确 2:1 的 360° 等距柱状全景图（equirectangular panorama）。
不是普通广角，不是再画一张九宫格，不是俯视平面图，不是多面板拼贴。

【场景】%s
【空间描述】%s

【核心做法：参照九宫格展开】
- 九宫格已经锁定同一房间的材质、家具与机位关系。全景必须像「把格1~格6沿水平 yaw 摊开」，不是重新发明一个地点。
- 格1 正面全景 → 全景图 x≈25%% 正面中心（前半球视觉圣经）。
- 格5 背面全景 → 全景图 x≈75%% 背面中心（水平 yaw 180° 反打）。
- 格3/格4 侧面 → 填 x≈50%% 附近的左右接合带；侧面纵深要接得上正面与背面。
- 格7 俯视全景 → 只锁地面拓扑（桌/床/门/通道方位），禁止把整张全景画成俯视。
- 图1 正面底板补风格与近处细节；有反打空镜时只补后半球，不要当第二张正面贴旁边。

【等距柱状硬约束】
- 画布左右边缘必须无缝相接（x=0%% 与 x=100%% 是同一条缝，低细节侧缝）。
- 地平线水平；禁止鱼眼、立方体贴图六面贴、九宫格分隔线、多格分镜。
- 空镜无人；无文字、水印、logo、UI、箭头标注。

【一致性】
- 家具材质、灯光色温跟图1/格1一致。
- 禁止左右镜像复制独特装置（门、柜、灯、招牌）。
- 禁止在接缝处重复同一扇门/窗。
- 正面里远处的门，在背面格应对应靠近镜头或落到边缘。`, subject, truncateRunes(desc, 400))
}

func withScenePanoramaConstraint(prompt string) string {
	p := strings.TrimSpace(prompt)
	lock := "【全景硬锁】输出必须是单张 2:1 等距柱状 360° 全景。优先把九宫格机位展开成环视，禁止再输出九宫格、禁止俯视平面图、禁止多格拼图、禁止人物。"
	if strings.Contains(p, "【全景硬锁】") {
		return p
	}
	return lock + "\n" + p
}

// GenerateScenePanoramaCandidates generates a 2:1 equirectangular scene panorama
// from master + preferably 9-grid cells (front/back/overhead).
func (s *ArkService) GenerateScenePanoramaCandidates(
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
		prompt = BuildScenePanoramaPrompt("", "")
	}
	prompt = withScenePanoramaConstraint(prompt)
	prompt = StripImageRefLegend(prompt)
	if !strings.Contains(prompt, "参考图分工") && !strings.Contains(prompt, "INPUT IMAGE") {
		prompt = PrependImageRefLegend(prompt, ScenePanoramaRefLegend)
	}
	prompt = withNoLogo(prompt)
	prompt = withSceneEmptyConstraint(prompt)
	prompt = clampImagePrompt(prompt, 2200)

	count = normalizeImageCandidateCount(count)
	if count > 2 {
		count = 2
	}
	spec.Aspect = "2:1"
	if strings.TrimSpace(spec.Resolution) == "" {
		spec.Resolution = "2k"
	}
	refs := referenceImages
	if len(refs) == 0 {
		return nil, "", fmt.Errorf("请至少提供场景正面底板或九宫格机位作为参考")
	}
	const maxRefs = 8
	if len(refs) > maxRefs {
		refs = refs[:maxRefs]
	}
	if onProgress != nil {
		onProgress(0, count, "正在按九宫格机位展开 2:1 全景…")
	}
	urls, err := s.generateImageCandidates(provider, model, prompt, refs, count, 4600, onProgress, spec)
	if err != nil && isImageGenSoftFail(err) && len(refs) > 3 {
		trimmed := refs[:3]
		log.Printf("scene panorama soft-fail (%v), retrying with %d refs", err, len(trimmed))
		if onProgress != nil {
			onProgress(0, count, "参考图过多导致失败，正在减少参考图重试…")
		}
		urls, err = s.generateImageCandidates(provider, model, prompt, trimmed, count, 4700, onProgress, spec)
	}
	if err != nil && isImageGenSoftFail(err) && len(refs) > 1 {
		trimmed := refs[:1]
		log.Printf("scene panorama soft-fail (%v), retrying with 1 ref", err)
		if onProgress != nil {
			onProgress(0, count, "参考图仍失败，仅用主参考重试…")
		}
		urls, err = s.generateImageCandidates(provider, model, prompt, trimmed, count, 4800, onProgress, spec)
	}
	if err != nil {
		return nil, prompt, friendlyImageGenError(err)
	}
	return urls, prompt, nil
}
