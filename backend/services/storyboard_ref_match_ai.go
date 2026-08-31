package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"novaly/backend/models"
)

// RefMatchCandidate is a compact library item for LLM disambiguation.
type RefMatchCandidate struct {
	ID   uint   `json:"id"`
	Type string `json:"type"` // character|scene|prop|other
	Name string `json:"name"`
}

// RefMatchPick is one selected reference from the model.
type RefMatchPick struct {
	ID    uint   `json:"id"`
	Label string `json:"label"`
}

var (
	refMatchJSONArrayRe = regexp.MustCompile(`(?s)\[\s*{.*?}\s*\]`)
	refMatchIDLineRe    = regexp.MustCompile(`(?m)^\s*(\d+)\s*[,，]?\s*(.*)$`)
)

// MatchShotLibraryRefs asks the text model to pick which library resources belong
// in the current shot. Candidates are pre-filtered client-side to keep tokens low.
func (s *ArkService) MatchShotLibraryRefs(
	provider models.AIProvider,
	model models.AIModel,
	script string,
	candidates []RefMatchCandidate,
) ([]RefMatchPick, error) {
	script = strings.TrimSpace(script)
	if script == "" {
		return nil, fmt.Errorf("当前分镜文案为空")
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	if ProviderRequiresAPIKey(provider) && strings.TrimSpace(provider.APIKey) == "" {
		return nil, fmt.Errorf("文本模型服务商未配置 API Key")
	}

	// Soft cap to control prompt size
	if len(candidates) > 40 {
		candidates = candidates[:40]
	}
	script = truncateRunes(script, 1600)

	var b strings.Builder
	b.WriteString(`从候选资源中选出「当前分镜」视频/站位参考图。省字回答。

规则：
1. 文案出现角色/生物/道具本体时选本体，不要选以其命名的场景或空间（例：文案是「小七」飞起 → 选「小七」，不要选「小七识海」；「擂台」≠「擂台离场长廊」，除非场景行写明离场长廊）。
2. 「场景：」行优先对应场景类资源。文案里的地点词（包厢/更衣室/走廊等）也要对应场景名。
3. 有「场景名·正面近景」这类 9 宫格切分格时，按景别选 1～3 个机位格：中景/近景→正面近景，全景→正面全景，俯视→俯视格。不要只选场景整图而丢掉机位格。
4. 文案写成 人名(左前) 或 人名说 的角色都必须选上，不限人数，包括开场前排没有台词的人（小鹿(左前)、小南(右前)）。不要只选有台词的人。杀手/路人/群演不要选，留给站位图。
5. 场景若有 9 宫格切分格（名字含「·正面近景」等机位），优先选与文案景别/「机位：」匹配的格子，可多选 1～3 个；有格子时不要只选空镜主场景图。
5. 尺寸变体是同一实体：只选一个。「指甲盖大小的小七」与「拳头大小的小七」二选一（优先选与前镜/最近使用一致的那张）；「5米长的赤鳞蜈蚣」对应文案里的「赤鳞蜈蚣/蜈蚣」。
6. 角色宁多勿漏（点名的人都要有图）；场景/道具仍宁缺毋滥，不要重复近义项。
7. 只输出 JSON 数组，勿解释。格式：[{"id":数字,"label":"名称"}]
   label 用候选里的名称；id 必须来自候选。

文案：
`)
	b.WriteString(script)
	b.WriteString("\n\n候选(id|类型|名称)：\n")
	for _, c := range candidates {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		typ := strings.TrimSpace(c.Type)
		if typ == "" {
			typ = "other"
		}
		b.WriteString(fmt.Sprintf("%d|%s|%s\n", c.ID, typ, name))
	}

	content, err := s.chat(provider, map[string]any{
		"model":       model.ModelID,
		"temperature": 0.1,
		"max_tokens":  512,
		"messages": []map[string]string{
			{"role": "user", "content": b.String()},
		},
	})
	if err != nil {
		return nil, err
	}

	picks, err := parseRefMatchPicks(content, candidates)
	if err != nil {
		return nil, err
	}
	return EnsureMentionedCharacterPicks(picks, candidates, script), nil
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

func parseRefMatchPicks(raw string, candidates []RefMatchCandidate) ([]RefMatchPick, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	raw = strings.TrimSpace(raw)

	byID := make(map[uint]RefMatchCandidate, len(candidates))
	for _, c := range candidates {
		byID[c.ID] = c
	}

	var parsed []struct {
		ID    any    `json:"id"`
		Label string `json:"label"`
	}
	tryJSON := func(s string) bool {
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			return false
		}
		return len(parsed) > 0 || s == "[]"
	}
	if !tryJSON(raw) {
		if loc := refMatchJSONArrayRe.FindString(raw); loc != "" {
			_ = tryJSON(loc)
		}
	}

	out := make([]RefMatchPick, 0, 12)
	seen := map[uint]bool{}
	push := func(id uint, label string) {
		c, ok := byID[id]
		if !ok || seen[id] {
			return
		}
		seen[id] = true
		if strings.TrimSpace(label) == "" {
			label = c.Name
		}
		out = append(out, RefMatchPick{ID: id, Label: strings.TrimSpace(label)})
	}

	if len(parsed) > 0 {
		for _, p := range parsed {
			id, ok := anyToUint(p.ID)
			if !ok {
				continue
			}
			push(id, p.Label)
		}
		return out, nil
	}

	// Fallback: plain id lines
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := refMatchIDLineRe.FindStringSubmatch(line); len(m) >= 2 {
			id64, err := strconv.ParseUint(m[1], 10, 64)
			if err != nil {
				continue
			}
			push(uint(id64), strings.TrimSpace(m[2]))
		}
	}
	if len(out) == 0 && raw != "[]" {
		return nil, fmt.Errorf("无法解析模型返回的参考图匹配结果")
	}
	return out, nil
}

func anyToUint(v any) (uint, bool) {
	switch x := v.(type) {
	case float64:
		if x < 0 {
			return 0, false
		}
		return uint(x), true
	case int:
		if x < 0 {
			return 0, false
		}
		return uint(x), true
	case int64:
		if x < 0 {
			return 0, false
		}
		return uint(x), true
	case json.Number:
		i, err := x.Int64()
		if err != nil || i < 0 {
			return 0, false
		}
		return uint(i), true
	case string:
		i, err := strconv.ParseUint(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, false
		}
		return uint(i), true
	default:
		return 0, false
	}
}
