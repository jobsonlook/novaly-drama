package crew

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"novaly/backend/models"
	"novaly/backend/services"
)

const (
	ChatNamePlanner    = "视频策划"
	ChatNameDirector   = "执行导演"
	ChatNameSupervisor = "监制"
	maxChatMessages    = 40
	maxChatMemory      = 12
)

type ChatMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"` // user | assistant
	Name      string `json:"name,omitempty"`
	Content   string `json:"content"`
	Action    string `json:"action,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

type ChatPlan struct {
	Action      string    `json:"action"`
	Reply       string    `json:"reply"`
	Replace     bool      `json:"replace"`
	ShotIDs     []uint    `json:"shotIds"`
	ShotIndexes []int     `json:"shotIndexes"`
	Issues      []QCIssue `json:"issues"`
}

func DecodeChat(raw string) []ChatMessage {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []ChatMessage{}
	}
	var out []ChatMessage
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []ChatMessage{}
	}
	return out
}

func EncodeChat(messages []ChatMessage) string {
	if len(messages) > maxChatMessages {
		messages = messages[len(messages)-maxChatMessages:]
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func AppendChat(messages []ChatMessage, extra ...ChatMessage) []ChatMessage {
	out := append(append([]ChatMessage{}, messages...), extra...)
	if len(out) > maxChatMessages {
		out = out[len(out)-maxChatMessages:]
	}
	return out
}

func NewChatMessage(role, name, content, action string) ChatMessage {
	return ChatMessage{
		ID:        fmt.Sprintf("%d-%d", time.Now().UnixMilli(), time.Now().UnixNano()%1000),
		Role:      role,
		Name:      name,
		Content:   strings.TrimSpace(content),
		Action:    strings.TrimSpace(action),
		CreatedAt: time.Now().UnixMilli(),
	}
}

func InferChatPlan(text string, shotCount int) ChatPlan {
	low := strings.TrimSpace(text)
	replace := strings.Contains(low, "替换") || strings.Contains(low, "重新拆") || strings.Contains(low, "重拆") || strings.Contains(low, "全部重做")
	switch {
	case strings.Contains(low, "质检") || strings.Contains(low, "审核") || strings.Contains(low, "复检") || strings.Contains(low, "再查"):
		return ChatPlan{Action: "qc", Reply: "对照当前分镜做质检。"}
	case strings.Contains(low, "按建议") || strings.Contains(low, "按上次") || strings.Contains(low, "改这些") || strings.Contains(low, "修好") || strings.Contains(low, "修改选中"):
		return ChatPlan{Action: "fix", Reply: "只改你确认过的项，其它镜头不动。"}
	case strings.Contains(low, "拆镜") || (strings.Contains(low, "分镜") && (strings.Contains(low, "开始") || strings.Contains(low, "自动") || replace)):
		if shotCount > 0 && !replace {
			return ChatPlan{Action: "reply", Reply: "本集已有分镜。要整集重拆并替换现有镜头，请明确说「替换分镜」或「重新拆镜」。只改某几镜请直接说镜号。"}
		}
		return ChatPlan{Action: "split", Replace: replace || shotCount == 0, Reply: "按当前剧本拆分镜。"}
	default:
		if shotCount == 0 && (strings.Contains(low, "开始") || strings.Contains(low, "制作")) {
			return ChatPlan{Action: "split", Replace: true, Reply: "本集还没有分镜，先按剧本拆。"}
		}
		return ChatPlan{Action: "reply"}
	}
}

func PlanChat(ark *services.ArkService, provider models.AIProvider, model models.AIModel, userText, script string, shots []ShotContext, assets []AssetItem, history []ChatMessage, lastIssues []QCIssue, thinkingLevel ...string) ChatPlan {
	fallback := InferChatPlan(userText, len(shots))
	if fallback.Action == "split" || fallback.Action == "qc" || fallback.Action == "fix" {
		return fallback
	}
	if ark == nil {
		return fallback
	}
	if services.ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return fallback
	}

	level := "off"
	if len(thinkingLevel) > 0 {
		level = strings.ToLower(strings.TrimSpace(thinkingLevel[0]))
	}
	memoryLimit := maxChatMemory
	analysisRule := "快速判断意图，避免扩展任务。"
	switch level {
	case "light":
		memoryLimit = 16
		analysisRule = "做一次轻量上下文核对后判断意图。"
	case "deep":
		memoryLimit = 28
		analysisRule = "深入核对近期对话、当前分镜和质检项之间的冲突后再判断。"
	case "extreme":
		memoryLimit = maxChatMessages
		analysisRule = "逐项交叉核对全部可用对话、分镜、剧本和质检信息，优先保证指令边界与连续性。"
	}
	mem := make([]string, 0, memoryLimit)
	start := 0
	if len(history) > memoryLimit {
		start = len(history) - memoryLimit
	}
	for _, m := range history[start:] {
		who := m.Name
		if who == "" {
			who = m.Role
		}
		mem = append(mem, fmt.Sprintf("%s：%s", who, clipRunes(m.Content, 240)))
	}
	shotLines := make([]string, 0, len(shots))
	for _, s := range shots {
		shotLines = append(shotLines, fmt.Sprintf("分镜%d id=%d %s refs=%d：%s", s.Index, s.ID, s.Label, len(s.Refs), clipRunes(s.Script, 180)))
	}
	issueLines := make([]string, 0, len(lastIssues))
	for i, issue := range lastIssues {
		if i >= 12 {
			break
		}
		issueLines = append(issueLines, fmt.Sprintf("%d. [%s] 分镜%d id=%d %s", i+1, firstNonEmpty(issue.Code, "QC"), issue.ShotIndex, issue.ShotID, issue.Message))
	}

	prompt := `你是视频策划，只调度分镜与质检，不改资产、不生图。对照【当前分镜快照】判断用户意图。
思考强度要求：` + analysisRule + `
规则：
- 当前分镜才是真相，不要用聊天里过期的旧分镜。
- 已有分镜时，禁止 split，除非用户明确要「替换 / 重新拆镜」。
- 改稿必须指定 shotIds 或沿用上次质检问题；禁止全片重写。
- qc = 只审核；fix = 用户已确认才改。
只输出 JSON：
{"action":"reply|split|qc|fix","reply":"给用户的一句话","replace":false,"shotIds":[],"shotIndexes":[],"issues":[{"severity":"high","code":"R2","shotId":0,"shotIndex":1,"message":"","suggestion":""}]}

用户：
` + clipRunes(userText, 800) + `

近期对话：
` + strings.Join(mem, "\n") + `

上次质检：
` + strings.Join(issueLines, "\n") + `

当前分镜（` + fmt.Sprintf("%d", len(shots)) + `）：
` + strings.Join(shotLines, "\n") + `

剧本草稿：
` + clipRunes(script, 4000) + `

资产数：` + fmt.Sprintf("%d", len(assets))

	content, err := ark.Chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.1,
		"max_tokens":  1200,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return fallback
	}
	var plan ChatPlan
	if err := unmarshalObject(content, &plan); err != nil {
		return fallback
	}
	plan.Action = strings.ToLower(strings.TrimSpace(plan.Action))
	switch plan.Action {
	case "reply", "split", "qc", "fix":
	default:
		plan.Action = fallback.Action
		if plan.Reply == "" {
			plan.Reply = fallback.Reply
		}
	}
	if plan.Action == "split" && len(shots) > 0 && !plan.Replace && !fallback.Replace {
		plan.Action = "reply"
		if strings.TrimSpace(plan.Reply) == "" {
			plan.Reply = fallback.Reply
		}
		if strings.TrimSpace(plan.Reply) == "" {
			plan.Reply = "本集已有分镜。要整集重拆请说「替换分镜」。"
		}
	}
	if plan.Action == "split" && len(shots) == 0 {
		plan.Replace = true
	}
	if strings.TrimSpace(plan.Reply) == "" {
		plan.Reply = fallback.Reply
	}
	return plan
}

func ResolvePlanIssues(plan ChatPlan, shots []ShotContext, lastIssues []QCIssue) []QCIssue {
	issues := append([]QCIssue{}, plan.Issues...)
	idSet := map[uint]bool{}
	idxSet := map[int]bool{}
	for _, id := range plan.ShotIDs {
		if id > 0 {
			idSet[id] = true
		}
	}
	for _, idx := range plan.ShotIndexes {
		if idx > 0 {
			idxSet[idx] = true
			if idx <= len(shots) {
				idSet[shots[idx-1].ID] = true
			}
		}
	}
	if len(issues) == 0 && (len(idSet) > 0 || plan.Action == "fix") {
		for _, issue := range lastIssues {
			if len(idSet) == 0 && len(idxSet) == 0 {
				issues = append(issues, issue)
				continue
			}
			if issue.ShotID > 0 && idSet[issue.ShotID] {
				issues = append(issues, issue)
				continue
			}
			if issue.ShotIndex > 0 && idxSet[issue.ShotIndex] {
				issues = append(issues, issue)
			}
		}
	}
	for i := range issues {
		if issues[i].ShotID == 0 && issues[i].ShotIndex > 0 && issues[i].ShotIndex <= len(shots) {
			issues[i].ShotID = shots[issues[i].ShotIndex-1].ID
		}
	}
	return issues
}

func FormatQCReport(report QCReport) string {
	var b strings.Builder
	score := strings.TrimSpace(report.Score)
	if score == "" {
		score = "-"
	}
	b.WriteString(fmt.Sprintf("质检 %s。%s\n", score, strings.TrimSpace(report.Summary)))
	if len(report.Issues) == 0 {
		b.WriteString("未发现需要处理的问题。")
		return b.String()
	}
	for i, issue := range report.Issues {
		if i >= 16 {
			b.WriteString(fmt.Sprintf("\n…其余 %d 项见质检报告。", len(report.Issues)-16))
			break
		}
		loc := fmt.Sprintf("分镜%d", issue.ShotIndex)
		if issue.ShotIndex <= 0 && issue.ShotID > 0 {
			loc = fmt.Sprintf("镜头#%d", issue.ShotID)
		}
		b.WriteString(fmt.Sprintf("\n%d. [%s] %s %s\n   %s", i+1, firstNonEmpty(issue.Code, "QC"), loc, issue.Message, issue.Suggestion))
	}
	b.WriteString("\n\n要改哪些请说「按建议修改」或指定镜号。监制不会直接改稿。")
	return b.String()
}
