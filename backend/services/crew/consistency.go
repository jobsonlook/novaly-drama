package crew

import (
	"fmt"
	"strings"
	"sync"

	"novaly/backend/models"
	"novaly/backend/services"
)

func PolishVisualPrompts(ark *services.ArkService, provider models.AIProvider, model models.AIModel, assets []AssetItem, project models.Project) ([]AssetItem, error) {
	if len(assets) == 0 {
		return []AssetItem{}, nil
	}
	if services.ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return nil, fmt.Errorf("文本模型服务商未配置 API Key")
	}

	out := make([]AssetItem, len(assets))
	copy(out, assets)
	style := resolveVisualStyle(project)

	var mu sync.Mutex
	var firstErr error
	done := 0
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			prompt, err := polishOneAsset(ark, provider, model, out[i], project, style)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				if strings.TrimSpace(out[i].Prompt) == "" {
					out[i].Prompt = strings.TrimSpace(out[i].Description)
				}
				return
			}
			out[i].Prompt = prompt
			done++
		}(i)
	}
	wg.Wait()
	if done == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func polishOneAsset(ark *services.ArkService, provider models.AIProvider, model models.AIModel, item AssetItem, project models.Project, style visualStyle) (string, error) {
	item.Type = normalizeAssetType(item.Type)
	if item.Type == "" {
		item.Type = "character"
	}
	content, err := ark.Chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.45,
		"max_tokens":  2048,
		"messages": []map[string]string{
			{"role": "system", "content": artSystemPrompt(item.Type, style, item.IsDerivative || item.ParentID > 0)},
			{"role": "user", "content": visualUserPrompt(item, project)},
		},
	})
	if err != nil {
		return "", err
	}
	prompt := parsePromptOutput(content)
	if prompt == "" {
		return "", fmt.Errorf("%s %s 未返回提示词", item.Type, item.Name)
	}
	return prompt, nil
}
