package crew

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

func extractJSONObject(raw string) (string, error) {
	s := stripThink(stripFence(raw))
	if chunk, ok := findJSONObject(s); ok {
		return chunk, nil
	}
	return "", fmt.Errorf("JSON 无法解析")
}

// findJSONObject locates the best JSON object in model output, preferring
// payloads that contain a top-level "shots" array (storyboard/QC).
func findJSONObject(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	bestStart := strings.Index(s, "{")
	if bestStart < 0 {
		return "", false
	}
	// Prefer the object that carries storyboard/QC payloads.
	for idx := 0; idx < len(s); {
		start := strings.Index(s[idx:], "{")
		if start < 0 {
			break
		}
		start += idx
		head := s[start:]
		if len(head) > 240 {
			head = head[:240]
		}
		if strings.Contains(head, `"shots"`) || strings.Contains(head, `"issues"`) {
			if chunk, ok := tryNormalizeJSONObject(s[start:]); ok {
				return chunk, true
			}
		}
		idx = start + 1
	}
	return tryNormalizeJSONObject(s[bestStart:])
}

func tryNormalizeJSONObject(rest string) (string, bool) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	if end := strings.LastIndex(rest, "}"); end > 0 {
		if chunk, ok := normalizeJSONObject(rest[:end+1]); ok {
			return chunk, true
		}
	}
	return normalizeJSONObject(rest)
}

func unmarshalObject[T any](raw string, dest *T) error {
	chunk, err := extractJSONObject(raw)
	if err != nil {
		preview := strings.TrimSpace(raw)
		if len([]rune(preview)) > 180 {
			preview = string([]rune(preview)[:180]) + "…"
		}
		return fmt.Errorf("%w：%s", err, preview)
	}
	if err := json.Unmarshal([]byte(chunk), dest); err != nil {
		return fmt.Errorf("解析 JSON 失败: %w", err)
	}
	return nil
}

func normalizeJSONObject(chunk string) (string, bool) {
	candidates := []string{
		chunk,
		repairJSONObject(chunk),
		repairJSONObject(closeTruncatedJSON(chunk)),
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if json.Valid([]byte(c)) {
			return c, true
		}
	}
	return "", false
}

func repairJSONObject(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = stripJSONComments(s)
	s = escapeBareControlInStrings(s)
	s = escapeInteriorQuotes(s)
	s = removeTrailingCommas(s)
	return strings.TrimSpace(s)
}

func stripJSONComments(s string) string {
	var b strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			b.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			b.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			if i < len(s) {
				b.WriteByte('\n')
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func escapeBareControlInStrings(s string) string {
	var b strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inString {
			if c == '"' {
				inString = true
			}
			b.WriteByte(c)
			continue
		}
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' {
			b.WriteByte(c)
			escaped = true
			continue
		}
		if c == '"' {
			inString = false
			b.WriteByte(c)
			continue
		}
		if c == '\n' {
			b.WriteString(`\n`)
			continue
		}
		if c == '\t' {
			b.WriteString(`\t`)
			continue
		}
		if c < 0x20 {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func escapeInteriorQuotes(s string) string {
	var b strings.Builder
	inString, escaped := false, false
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if inString && r == '\\' {
			b.WriteRune(r)
			escaped = true
			continue
		}
		if r != '"' {
			b.WriteRune(r)
			continue
		}
		if !inString {
			inString = true
			b.WriteRune(r)
			continue
		}
		rest := strings.TrimLeftFunc(string(runes[i+1:]), unicode.IsSpace)
		if rest == "" || strings.HasPrefix(rest, ",") || strings.HasPrefix(rest, "}") || strings.HasPrefix(rest, "]") || strings.HasPrefix(rest, ":") {
			inString = false
			b.WriteRune(r)
			continue
		}
		b.WriteString(`\"`)
	}
	return b.String()
}

func removeTrailingCommas(s string) string {
	var b strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			b.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\n' || s[j] == '\t' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func closeTruncatedJSON(s string) string {
	s = strings.TrimRight(s, " \n\t\r,")
	inString, escaped := false, false
	stack := make([]byte, 0, 8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, c)
		case '}':
			if n := len(stack); n > 0 && stack[n-1] == '{' {
				stack = stack[:n-1]
			}
		case ']':
			if n := len(stack); n > 0 && stack[n-1] == '[' {
				stack = stack[:n-1]
			}
		}
	}
	if inString {
		s = fixTrailingBackslashBeforeClose(s)
		s += `"`
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			s += "}"
		} else {
			s += "]"
		}
	}
	return s
}

// fixTrailingBackslashBeforeClose ensures an open JSON string does not end with
// a lone `\` — otherwise appending `"` creates an invalid escape and json.Valid fails.
func fixTrailingBackslashBeforeClose(s string) string {
	n := 0
	for n < len(s) && s[len(s)-1-n] == '\\' {
		n++
	}
	if n%2 == 1 {
		s += `\`
	}
	return s
}

func stripFence(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func clipRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func normalizeAssetType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "character", "role", "角色":
		return "character"
	case "scene", "场景":
		return "scene"
	case "prop", "tool", "道具":
		return "prop"
	default:
		return ""
	}
}

func mergeAssets(characters, scenes, props []AssetItem) []AssetItem {
	out := make([]AssetItem, 0, len(characters)+len(scenes)+len(props))
	seen := map[string]bool{}
	add := func(item AssetItem, fallbackType string) {
		item.Name = strings.TrimSpace(item.Name)
		if item.Name == "" {
			return
		}
		item.Type = normalizeAssetType(item.Type)
		if item.Type == "" {
			item.Type = fallbackType
		}
		item.Description = strings.TrimSpace(item.Description)
		item.VoicePrompt = strings.TrimSpace(item.VoicePrompt)
		item.Prompt = strings.TrimSpace(item.Prompt)
		key := item.Type + ":" + strings.ToLower(item.Name)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, item)
	}
	for _, a := range characters {
		add(a, "character")
	}
	for _, a := range scenes {
		add(a, "scene")
	}
	for _, a := range props {
		add(a, "prop")
	}
	return out
}
