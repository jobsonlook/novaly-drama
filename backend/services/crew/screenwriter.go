package crew

import (
	"fmt"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"
)

func PolishScript(ark *services.ArkService, provider models.AIProvider, model models.AIModel, script string, project models.Project) (string, error) {
	script = strings.TrimSpace(script)
	if script == "" {
		return "", fmt.Errorf("剧本为空")
	}
	if services.ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return "", fmt.Errorf("文本模型服务商未配置 API Key")
	}

	prompt := `你是短剧编剧 Agent。把用户提供的分集草稿改写成可拍摄的标准剧本，不要改情节走向、人物关系和结局。

格式要求：
1. 开头一行：**出场人物：角色A，角色B**
2. 每场用加粗场头： **集号-场号 场景名 [内/外] [日/夜]**
3. 画面/动作行以「△ 」开头，必须可视化，禁止心理描写。
4. 对白格式：**角色名**：(情绪/动作) 台词
5. 重要音效用【音效】，字幕用【字幕】。
6. 大段独白可拆成多行对白，但引号内/冒号后的原词必须逐字保留（含 2024、穿越、口语、外语）。禁止改成古白、禁止同义替换、禁止删关键句。
7. 只输出剧本正文，不要解释，不要 Markdown 代码块。

` + optionalProjectContext(project) + `原始草稿：
` + clipRunes(script, 18000)

	content, err := ark.Chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.4,
		"max_tokens":  8192,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(stripFence(content))
	if out == "" {
		return "", fmt.Errorf("编剧未返回剧本")
	}
	var parsed struct {
		Script string `json:"script"`
	}
	if unmarshalObject(content, &parsed) == nil && strings.TrimSpace(parsed.Script) != "" {
		out = strings.TrimSpace(parsed.Script)
	}
	return out, nil
}

func optionalProjectContext(project models.Project) string {
	var b strings.Builder
	if project.Kind == "novel" {
		b.WriteString("项目类型：基于小说改编。\n")
	} else if strings.TrimSpace(project.Kind) != "" {
		b.WriteString("项目类型：基于剧本。\n")
	}
	if g := strings.TrimSpace(project.Genre); g != "" {
		b.WriteString("题材类型：")
		b.WriteString(g)
		b.WriteString("\n")
	}
	if s := strings.TrimSpace(project.Synopsis); s != "" {
		b.WriteString("故事简介：")
		b.WriteString(clipRunes(s, 800))
		b.WriteString("\n")
	}
	if d := DirectorBrief(project.DirectorManual); d != "" {
		b.WriteString(d)
		b.WriteString("\n")
	}
	style := strings.TrimSpace(project.Style)
	if style == "" {
		style = StylePrompt(project.VisualManual)
	}
	if style != "" {
		b.WriteString("项目画风（仅作氛围参考，不要写进剧本格式）：")
		b.WriteString(clipRunes(style, 400))
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String() + "\n"
}
