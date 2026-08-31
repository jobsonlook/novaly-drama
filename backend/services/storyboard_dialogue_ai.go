package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"novaly/backend/models"
)

type StoryboardDialogueCharacter struct {
	Name      string `json:"name"`
	VoiceHint string `json:"voice_hint"`
}

type StoryboardDialogueLine struct {
	Time         string `json:"time"`
	Type         string `json:"type"`
	Speaker      string `json:"speaker"`
	Text         string `json:"text"`
	Tone         string `json:"tone"`
	Emotion      string `json:"emotion"`
	EmotionHint  string `json:"emotion_hint"`
	Pitch        int    `json:"pitch"`
	SpeechRate   int    `json:"speech_rate"`
	LoudnessRate int    `json:"loudness_rate"`
	NeedsReview  bool   `json:"needs_review"`
}

type StoryboardDialogueAnalysis struct {
	Characters []StoryboardDialogueCharacter `json:"characters"`
	Lines      []StoryboardDialogueLine      `json:"lines"`
}

// AnalyzeStoryboardDialogue asks the configured text model to turn one
// storyboard prompt into strict, editable TTS metadata.
func (s *ArkService) AnalyzeStoryboardDialogue(
	provider models.AIProvider,
	model models.AIModel,
	script string,
) (StoryboardDialogueAnalysis, error) {
	if strings.TrimSpace(script) == "" {
		return StoryboardDialogueAnalysis{}, errors.New("分镜文案为空")
	}
	if ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return StoryboardDialogueAnalysis{}, errors.New("文本模型服务商未配置 API Key")
	}

	prompt := `你是专业短剧配音导演。请从下面一条分镜文案中提取所有真正需要配音的台词，并输出严格 JSON。

规则：
1. 只提取明确的“台词、对白、内心戏、旁白、画外音、系统播报”。不要把镜头、动作、场景、音效、角色编号、参考素材、数字、道具名当成说话人。
2. speaker 必须是实际角色名。结合“角色”行、镜头中正在说话的人、语气和上下文判断；确实无法判断才 needs_review=true。
3. text 只保留实际朗读正文，去掉引号和“台词：”“内心戏：”等标签；“（无）”不要输出。
4. time 使用原文时段，例如“3-6秒”；type 只能是“台词”“内心戏”“旁白”“画外音”“系统”之一。
5. tone 保留原文简短语气，例如“清晰可辨，决绝”。
6. emotion 必须写成有场景、有身体反应、有情绪强度的中文导演指令。不要使用“轻微、适度、克制、平稳”等泛化弱指令，除非剧情明确要求压抑。
   参考：“像被逼入绝境后面对仇人彻底爆发；胸腔强力发声，呼吸猛烈，关键词狠狠咬住；语势持续推高，句尾彻底炸开，允许自然破音”
   指令必须突出当前分镜的危险、喜剧、悲伤、压迫或爆发氛围，不能只是复述 tone。
7. emotion_hint 只能是 neutral/angry/fearful/sad/happy/cold/narration。
8. pitch 范围 -12~12；speech_rate 范围 -50~100；loudness_rate 范围 -50~100。根据氛围拉开差异：爆发/惊恐可提高语速和音量，虚弱/悲伤可明显降低；pitch 通常控制在 -3~3。
9. characters 仅列出本分镜实际有台词的角色，并给出简短“年龄+性别+声线气质”的 voice_hint。
10. 不要解释，不要 Markdown，只输出此结构：
{"characters":[{"name":"角色名","voice_hint":"青年男声，清爽硬朗"}],"lines":[{"time":"0-3秒","type":"台词","speaker":"角色名","text":"正文","tone":"语气","emotion":"完整导演指令","emotion_hint":"neutral","pitch":0,"speech_rate":0,"loudness_rate":0,"needs_review":false}]}

分镜文案：
` + script

	content, err := s.chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.1,
		"max_tokens":  4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return StoryboardDialogueAnalysis{}, err
	}
	raw := strings.TrimSpace(content)
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return StoryboardDialogueAnalysis{}, fmt.Errorf("大模型未返回 JSON：%s", truncate(raw, 240))
	}
	var result StoryboardDialogueAnalysis
	if err := json.Unmarshal([]byte(raw[start:end+1]), &result); err != nil {
		return StoryboardDialogueAnalysis{}, fmt.Errorf("解析大模型台词结果失败: %w", err)
	}
	if result.Characters == nil {
		result.Characters = []StoryboardDialogueCharacter{}
	}
	if result.Lines == nil {
		result.Lines = []StoryboardDialogueLine{}
	}
	return result, nil
}
