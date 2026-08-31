package crew

import (
	"fmt"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"
)

func FillCharacterVoices(ark *services.ArkService, provider models.AIProvider, model models.AIModel, assets []AssetItem, script string) ([]AssetItem, error) {
	out := make([]AssetItem, len(assets))
	copy(out, assets)
	missing := make([]AssetItem, 0, len(out))
	indexes := make([]int, 0, len(out))
	for i := range out {
		if normalizeAssetType(out[i].Type) != "character" {
			continue
		}
		if out[i].IsDerivative || out[i].ParentID > 0 {
			continue
		}
		if strings.TrimSpace(out[i].VoicePrompt) != "" {
			continue
		}
		missing = append(missing, out[i])
		indexes = append(indexes, i)
	}
	if len(missing) == 0 {
		inheritParentVoices(out)
		return out, nil
	}
	if services.ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return out, fmt.Errorf("文本模型服务商未配置 API Key")
	}

	var b strings.Builder
	b.WriteString("你为短剧角色写固定音色提示词。同一角色在所有分镜视频里必须用完全相同的一句，才能跨账号保持声线一致。\n")
	b.WriteString("只输出 JSON，不要 Markdown：\n")
	b.WriteString(`{"voices":[{"name":"角色名","voicePrompt":"30岁左右男性，声线低沉沙哑带磁性，不是少年音"}]}`)
	b.WriteString("\n\n规则：\n")
	b.WriteString("1. 每个角色一句 30-50 字：年龄段+性别+音高（中低/中高）+质感（沙哑/清亮/磁性等）+一句明确否定（如「不是清脆少女音」）。\n")
	b.WriteString("2. 不要写台词、口音表演或情绪变化；那些会随分镜变，音色本身必须锁死。\n")
	b.WriteString("3. name 必须与输入完全一致。\n")
	b.WriteString("4. 群众/无名角色也给一句可用的默认音色。\n\n角色：\n")
	for _, item := range missing {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(item.Name))
		if d := strings.TrimSpace(item.Description); d != "" {
			b.WriteString("：")
			b.WriteString(clipRunes(d, 120))
		}
		b.WriteString("\n")
	}
	if s := strings.TrimSpace(script); s != "" {
		b.WriteString("\n剧本节选：\n")
		b.WriteString(clipRunes(s, 4000))
	}

	content, err := ark.Chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.3,
		"max_tokens":  2048,
		"messages": []map[string]string{
			{"role": "user", "content": b.String()},
		},
	})
	if err != nil {
		return out, err
	}
	var parsed struct {
		Voices []struct {
			Name        string `json:"name"`
			VoicePrompt string `json:"voicePrompt"`
		} `json:"voices"`
	}
	if err := unmarshalObject(content, &parsed); err != nil {
		return out, fmt.Errorf("音色解析失败: %w", err)
	}
	byName := map[string]string{}
	for _, v := range parsed.Voices {
		name := strings.ToLower(strings.TrimSpace(v.Name))
		prompt := strings.TrimSpace(v.VoicePrompt)
		if name == "" || prompt == "" {
			continue
		}
		byName[name] = prompt
	}
	for _, i := range indexes {
		key := strings.ToLower(strings.TrimSpace(out[i].Name))
		if p := byName[key]; p != "" {
			out[i].VoicePrompt = p
		}
	}
	inheritParentVoices(out)
	return out, nil
}

func inheritParentVoices(assets []AssetItem) {
	byName := map[string]string{}
	for _, a := range assets {
		if normalizeAssetType(a.Type) != "character" || a.IsDerivative || a.ParentID > 0 {
			continue
		}
		if p := strings.TrimSpace(a.VoicePrompt); p != "" {
			byName[strings.ToLower(strings.TrimSpace(a.Name))] = p
		}
	}
	for i := range assets {
		if normalizeAssetType(assets[i].Type) != "character" {
			continue
		}
		if strings.TrimSpace(assets[i].VoicePrompt) != "" {
			continue
		}
		parent := strings.TrimSpace(assets[i].ParentName)
		if parent == "" && assets[i].ParentID == 0 && !assets[i].IsDerivative {
			continue
		}
		if p := byName[strings.ToLower(parent)]; p != "" {
			assets[i].VoicePrompt = p
		}
	}
}
