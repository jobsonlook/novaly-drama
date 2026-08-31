package crew

import (
	"encoding/json"
	"fmt"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"
)

type deriveResult struct {
	Items []AssetItem `json:"items"`
}

func AnalyzeDerivatives(ark *services.ArkService, provider models.AIProvider, model models.AIModel, script string, bases []AssetItem, project models.Project) ([]AssetItem, error) {
	script = strings.TrimSpace(script)
	if script == "" || len(bases) == 0 {
		return nil, nil
	}
	if services.ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return nil, fmt.Errorf("文本模型服务商未配置 API Key")
	}

	compact := make([]map[string]string, 0, len(bases))
	for _, a := range bases {
		if a.IsDerivative || a.ParentID > 0 {
			continue
		}
		typ := normalizeAssetType(a.Type)
		if typ != "character" && typ != "scene" {
			continue
		}
		compact = append(compact, map[string]string{
			"name":        a.Name,
			"type":        typ,
			"description": clipRunes(a.Description, 180),
		})
	}
	if len(compact) == 0 {
		return nil, nil
	}
	raw, _ := json.Marshal(compact)

	prompt := `你是衍生资产分析助手。父资产已经是角色/场景的「日常默认态」。只提取父资产的稳定视觉状态变体，不是新角色、也不是镜头特写。

只输出 JSON：
{"items":[{"parentName":"与父资产同名","type":"character|scene","name":"2到6字状态名","description":"与默认态的差异 · 视觉特征"}]}

提取范围：
- 角色：仅变身状态。①整身换装（校服→战斗服、礼服、拳台装）②变身特效外观 ③变形（兽化/巨大化）。三类可并列。全程一套日常装则不要衍生。
- 场景：仅时间变体（日景→夜景/黄昏/清晨）。角度、天候、破坏不要。
- 道具：一律不提取。

不要提取：瞬时表情、局部特写、单镜头情绪钩子、可由分镜提示词表达的内容。
每个父资产最多 5 条，宁缺勿滥。name 体现外观变化，不要写成「韩铮夜」这种角色名。description 格式必须是「与默认态的差异 · 视觉特征」。
无需要衍生时输出 {"items":[]}。

` + optionalProjectContext(project) + `父资产列表：
` + string(raw) + `

剧本：
` + clipRunes(script, 14000)

	content, err := ark.Chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.25,
		"max_tokens":  3072,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return nil, err
	}
	var parsed deriveResult
	if err := unmarshalObject(content, &parsed); err != nil {
		return nil, fmt.Errorf("衍生资产解析失败: %w", err)
	}

	byKey := map[string]AssetItem{}
	for _, b := range bases {
		typ := normalizeAssetType(b.Type)
		if typ == "" {
			continue
		}
		byKey[typ+":"+strings.ToLower(strings.TrimSpace(b.Name))] = b
	}

	out := make([]AssetItem, 0, len(parsed.Items))
	seen := map[string]bool{}
	perParent := map[string]int{}
	for _, item := range parsed.Items {
		item.Type = normalizeAssetType(item.Type)
		item.Name = strings.TrimSpace(item.Name)
		item.ParentName = strings.TrimSpace(item.ParentName)
		item.Description = strings.TrimSpace(item.Description)
		if item.Name == "" || item.ParentName == "" {
			continue
		}
		if item.Type != "character" && item.Type != "scene" {
			continue
		}
		parent, ok := byKey[item.Type+":"+strings.ToLower(item.ParentName)]
		if !ok {
			continue
		}
		pk := item.Type + ":" + strings.ToLower(parent.Name)
		if perParent[pk] >= 5 {
			continue
		}
		dk := pk + ":" + strings.ToLower(item.Name)
		if seen[dk] {
			continue
		}
		seen[dk] = true
		perParent[pk]++
		item.IsDerivative = true
		item.ParentID = parent.ResourceID
		item.ParentDescription = parent.Description
		if item.ParentID == 0 {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}
