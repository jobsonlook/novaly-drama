package crew

import (
	"fmt"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"
)

func PlanAndExtract(ark *services.ArkService, provider models.AIProvider, model models.AIModel, script string, project models.Project) (DirectorResult, error) {
	script = strings.TrimSpace(script)
	if script == "" {
		return DirectorResult{}, fmt.Errorf("剧本为空")
	}
	if services.ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return DirectorResult{}, fmt.Errorf("文本模型服务商未配置 API Key")
	}

	prompt := `你是短剧导演 Agent。阅读定稿剧本，给出拍摄规划，并提取本集需要入库的基础资产。

只输出 JSON，不要 Markdown：
{
  "plan": "拍摄规划（节奏、场次顺序、重点镜头、资产生成优先级，200-500字）",
  "characters": [{"name":"角色名","description":"剧情中的身份/年龄/关系/关键外观线索","voicePrompt":"30岁左右男性，声线低沉沙哑带磁性，不是少年音","priority":1}],
  "scenes": [{"name":"场景名","description":"时空、结构、光线、氛围","priority":1}],
  "props": [{"name":"道具名","description":"外形与剧情用途","priority":2}]
}

规则：
1. 只提取剧本里真正出镜、需要单独绘图的资产。有具体称呼且在本集出现≥2次（含分镜式「人名(左前)」「人名说」写法）的配角也要提取，例如太监甲、太监乙；纯无名群众/只出现1次的一次性路人不要。
2. name 用剧本里的称呼，简短稳定，不要别名堆砌。
3. priority 1=必须先画，2=重要，3=可后补。
4. description 必须视觉化，40-90字，供后续画设定图；少写人物关系，多写别人能看见的东西。
   - 角色：性别、年龄段、身份、体型、发型发色、五官/疤痕等记忆点、常规着装、气质。不要只写「谁的弟弟/会所陪侍」这种剧情句。常规着装不要写冠军奖牌、奖杯——那是道具。
   - 场景：室内外、时代、结构与物件、光线时间、氛围。
   - 道具：材质、颜色、尺度、标志性细节。
5. 道具只保留反复出现或叙事关键的物件。奖牌、奖杯、绷带等出镜物件必须进 props，不要画进角色脖子。
6. 只提取日常默认态。换装、变身、夜景版不要当成新角色或新场景——后续会单独做衍生资产。
7. 每个角色必须给 voicePrompt：固定一句音色，供后续所有分镜视频共用，禁止每镜改写。
   格式：年龄段+性别+音高（中低/中高）+质感（沙哑/清亮/磁性等）+一句明确否定（如「不是清脆少女音」）。30-50字。不要写台词或情绪。

` + optionalProjectContext(project) + `定稿剧本：
` + clipRunes(script, 16000)
	prompt = services.ApplyDramaSkillGuidance(prompt, "develop", "assets")

	content, err := ark.Chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.3,
		"max_tokens":  4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return DirectorResult{}, err
	}
	var result DirectorResult
	if err := unmarshalObject(content, &result); err != nil {
		return DirectorResult{}, fmt.Errorf("导演规划解析失败: %w", err)
	}
	EnsureRecurringCharacters(&result, script)
	result.Plan = strings.TrimSpace(result.Plan)
	result.Characters = mergeAssets(result.Characters, nil, nil)
	result.Scenes = mergeAssets(nil, result.Scenes, nil)
	result.Props = mergeAssets(nil, nil, result.Props)
	if result.Plan == "" && len(result.Characters)+len(result.Scenes)+len(result.Props) == 0 {
		return DirectorResult{}, fmt.Errorf("导演未提取到规划或资产")
	}
	return result, nil
}

func MergeExported(plan DirectorResult) []AssetItem {
	return mergeAssets(plan.Characters, plan.Scenes, plan.Props)
}
