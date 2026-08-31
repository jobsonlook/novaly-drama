package crew

import (
	"fmt"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"
)

const MinRecurringCharacterMentions = 2

func characterInAssets(assets []AssetItem, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, a := range assets {
		if normalizeAssetType(a.Type) != "character" {
			continue
		}
		if AssetNameMatches(a, name) {
			return true
		}
	}
	return false
}

// EnsureRecurringCharacters adds named characters mentioned minMentions+ times when the director skipped them.
func EnsureRecurringCharacters(result *DirectorResult, script string) {
	if result == nil {
		return
	}
	for _, name := range services.RecurringCharacterNames(script, MinRecurringCharacterMentions) {
		if characterInAssets(result.Characters, name) {
			continue
		}
		result.Characters = append(result.Characters, AssetItem{
			Name:     name,
			Type:     "character",
			Priority: 2,
		})
	}
}

// EnsureRecurringCharactersInAssets returns assets with missing recurring characters appended.
func EnsureRecurringCharactersInAssets(assets []AssetItem, script string) ([]AssetItem, []string) {
	added := make([]string, 0, 4)
	for _, name := range services.RecurringCharacterNames(script, MinRecurringCharacterMentions) {
		if characterInAssets(assets, name) {
			continue
		}
		assets = append(assets, AssetItem{
			Name:     name,
			Type:     "character",
			Priority: 2,
		})
		added = append(added, name)
	}
	return assets, added
}

// DescribeRecurringCharacters fills description and voicePrompt for auto-added recurring characters.
func DescribeRecurringCharacters(ark *services.ArkService, provider models.AIProvider, model models.AIModel, names []string, script string, project models.Project) (map[string]AssetItem, error) {
	out := map[string]AssetItem{}
	if len(names) == 0 {
		return out, nil
	}
	if services.ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return nil, fmt.Errorf("文本模型服务商未配置 API Key")
	}
	var b strings.Builder
	b.WriteString("你是短剧选角/美术助手。下列角色在剧本里被点名出现≥2次，但尚未有设定描述。请根据剧本推断可绘制的外观与固定音色。\n")
	b.WriteString("只输出 JSON，不要 Markdown：\n")
	b.WriteString(`{"characters":[{"name":"角色名","description":"40-90字视觉描述","voicePrompt":"30-50字固定音色"}]}`)
	b.WriteString("\n\n规则：\n")
	b.WriteString("1. name 必须与输入完全一致。\n")
	b.WriteString("2. description 视觉化：性别、年龄段、身份、体型、发型发色、五官/记忆点、常规着装、气质。\n")
	b.WriteString("3. voicePrompt：年龄段+性别+音高+质感+一句否定，供全剧视频共用。\n")
	b.WriteString("4. 同类配角（如太监甲/太监乙）外观要有区分度，不要复制粘贴。\n\n角色：\n")
	for _, name := range names {
		b.WriteString("- ")
		b.WriteString(name)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(optionalProjectContext(project))
	b.WriteString("剧本：\n")
	b.WriteString(clipRunes(script, 14000))

	content, err := ark.Chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.35,
		"max_tokens":  4096,
		"messages": []map[string]string{
			{"role": "user", "content": b.String()},
		},
	})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Characters []AssetItem `json:"characters"`
	}
	if err := unmarshalObject(content, &parsed); err != nil {
		return nil, fmt.Errorf("反复角色描述解析失败: %w", err)
	}
	for _, item := range parsed.Characters {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		out[name] = AssetItem{
			Name:        name,
			Type:        "character",
			Description: strings.TrimSpace(item.Description),
			VoicePrompt: strings.TrimSpace(item.VoicePrompt),
			Priority:    2,
		}
	}
	return out, nil
}

func ApplyRecurringCharacterDetails(assets []AssetItem, details map[string]AssetItem) {
	if len(details) == 0 {
		return
	}
	for i := range assets {
		if normalizeAssetType(assets[i].Type) != "character" {
			continue
		}
		d, ok := details[strings.TrimSpace(assets[i].Name)]
		if !ok {
			continue
		}
		if strings.TrimSpace(assets[i].Description) == "" {
			assets[i].Description = d.Description
		}
		if strings.TrimSpace(assets[i].VoicePrompt) == "" {
			assets[i].VoicePrompt = d.VoicePrompt
		}
	}
}

func CombineScripts(scripts ...string) string {
	var b strings.Builder
	for _, s := range scripts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s)
	}
	return b.String()
}
