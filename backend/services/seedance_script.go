package services

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const VideoSpeechConstraint = "【对白朗读·最高优先级】必须生成包含对白人声的音轨。花括号{}里的句子只出声、只对口型，不是字幕、不是花字、不是对白条。说话人必须张嘴并同步口型，在指定时段整句说完，禁止吞字、跳句、改词或改成沉默。无{}的镜头不要凭空配音。音效用<>包裹，不要朗读音效。"

const VideoNoSpeechConstraint = "【无台词·最高优先级】本镜没有任何对白、旁白、画外音或人声。所有人物全程不说话、不发声、不做说话口型；禁止根据人物关系、动作或上下文自行编造台词。音轨只保留提示词中用<>标出的配乐与环境音效。"

const VideoNoSubtitleConstraint = "【禁止字幕·最高优先级】成片严禁字幕、自动字幕、花字、对白条、歌词条和水印；不得把对白转写成画面文字。{}里的台词只进入音轨并由人物出声朗读，不要烧进画面，禁止底部字幕、禁止人物身旁弹出对白文字。本条不禁止当前分镜明确指定的剧情内文字：剑身铭文、牌匾、书信等须按指定原字出现在对应物体表面，不得显示额外文字。"

var (
	seedanceBeatRE         = regexp.MustCompile(`【(\d+)\s*[-–—~到至]\s*(\d+)\s*秒】`)
	seedanceQuoteRE        = regexp.MustCompile(`「([^」]*)」`)
	seedanceSFXRE          = regexp.MustCompile(`音效：([^；\n「<{]*)`)
	seedanceSpeakerTailRE  = regexp.MustCompile(`[\p{Han}A-Za-z0-9甲乙丙丁]{1,12}(?:说|道|喊|问)?[：:]\s*$`)
	seedanceSFXSplitRE     = regexp.MustCompile(`[、，,]`)
	seedanceSubtitleLineRE = regexp.MustCompile(`(?m)^\s*(?:【字幕】|\[字幕\]|字幕\s*[：:]).*$`)
	seedanceSubtitleTagRE  = regexp.MustCompile(`(?:【字幕】|\[字幕\])[^\n；;。]*[；;。]?`)
)

func stripVideoSubtitleDirectives(script string) string {
	script = seedanceSubtitleLineRE.ReplaceAllString(script, "")
	script = seedanceSubtitleTagRE.ReplaceAllString(script, "")
	return strings.TrimSpace(script)
}

type seedanceSubject struct {
	tag   string
	names []string
}

func clipScriptToDuration(script string, duration int) string {
	if duration <= 0 {
		duration = 10
	}
	script = reflowSeedanceBeatTimes(script)
	var keep []string
	overflowing := false
	keptEnd := 0
	for _, line := range strings.Split(script, "\n") {
		trim := strings.TrimSpace(line)
		if m := seedanceBeatRE.FindStringSubmatch(trim); len(m) >= 3 {
			start, end := 0, 0
			fmt.Sscanf(m[1], "%d", &start)
			fmt.Sscanf(m[2], "%d", &end)
			// Keep 【9-13秒】-style straddling beats out of a 10s video prompt.
			if start >= duration || end > duration {
				overflowing = true
			}
		} else if trim != "" && keptEnd >= duration {
			overflowing = true
		}
		if overflowing {
			continue
		}
		keep = append(keep, line)
		if m := seedanceBeatRE.FindStringSubmatch(trim); len(m) >= 3 {
			end := 0
			fmt.Sscanf(m[2], "%d", &end)
			if end > keptEnd {
				keptEnd = end
			}
		}
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

func reflowSeedanceBeatTimes(script string) string {
	lines := strings.Split(script, "\n")
	cursor := 0
	for i, line := range lines {
		m := seedanceBeatRE.FindStringSubmatch(line)
		if len(m) < 3 {
			continue
		}
		start, end := 0, 0
		fmt.Sscanf(m[1], "%d", &start)
		fmt.Sscanf(m[2], "%d", &end)
		dur := end - start
		if dur <= 0 {
			dur = 3
		}
		if start < cursor {
			start = cursor
			end = start + dur
			lines[i] = seedanceBeatRE.ReplaceAllString(line, fmt.Sprintf("【%d-%d秒】", start, end))
		}
		if end > cursor {
			cursor = end
		}
	}
	return strings.Join(lines, "\n")
}

func rewriteScriptForSeedance(script string, refs []refImage, voices []CharacterVoice) string {
	script = strings.TrimSpace(script)
	if script == "" {
		return ""
	}
	subjects := seedanceSubjects(refs)
	beats := splitSeedanceBeats(script)
	if len(beats) == 0 {
		return script
	}
	out := make([]string, 0, len(beats))
	n := 0
	for _, beat := range beats {
		n++
		out = append(out, rewriteBeatForSeedance(n, beat, subjects, voices))
	}
	return strings.Join(out, "\n")
}

func splitSeedanceBeats(script string) []string {
	var beats []string
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "秒】") || seedanceQuoteRE.MatchString(line) {
			beats = append(beats, line)
			continue
		}
		if len(beats) == 0 {
			beats = append(beats, line)
			continue
		}
		beats[len(beats)-1] += " " + line
	}
	if len(beats) == 0 {
		return []string{script}
	}
	return beats
}

func rewriteBeatForSeedance(n int, beat string, subjects []seedanceSubject, voices []CharacterVoice) string {
	span := ""
	body := beat
	if loc := strings.Index(beat, "】"); loc >= 0 && strings.Contains(beat[:loc+len("】")], "秒") {
		if m := seedanceBeatRE.FindStringSubmatch(beat); len(m) >= 3 {
			span = m[1] + "-" + m[2] + "秒"
		}
		body = strings.TrimSpace(beat[loc+len("】"):])
	}
	body = strings.TrimPrefix(body, "镜头：")
	body = strings.TrimPrefix(body, "镜头:")
	body = strings.TrimSpace(body)

	body = seedanceSFXRE.ReplaceAllStringFunc(body, func(raw string) string {
		m := seedanceSFXRE.FindStringSubmatch(raw)
		if len(m) < 2 {
			return raw
		}
		return wrapSeedanceSFX(m[1])
	})

	idxs := seedanceQuoteRE.FindAllStringSubmatchIndex(body, -1)
	for i := len(idxs) - 1; i >= 0; i-- {
		inner := strings.TrimSpace(body[idxs[i][2]:idxs[i][3]])
		if seedanceQuoteEmpty(inner) {
			body = strings.TrimSpace(body[:idxs[i][0]] + body[idxs[i][1]:])
			continue
		}
		before := body[:idxs[i][0]]
		if isSeedanceDiegeticText(before) {
			// An inscription in corner quotes is visible content, not speech.
			continue
		}
		sub := inferSeedanceSpeaker(before, inner, subjects)
		repl := seedanceSpeechClause(sub, inner, voices)
		before = seedanceSpeakerTailRE.ReplaceAllString(strings.TrimRight(before, " \t"), "")
		before = strings.TrimRight(before, "；;，, ")
		if before != "" && !strings.HasSuffix(before, "。") {
			before += "。"
		}
		body = before + repl + body[idxs[i][1]:]
	}

	body = replaceSubjectsOutsideBraces(body, subjects)
	body = strings.Trim(body, "；;，, ")
	if span != "" {
		return fmt.Sprintf("镜头%d（%s）：%s", n, span, body)
	}
	return fmt.Sprintf("镜头%d：%s", n, body)
}

var seedanceDiegeticTextTailRE = regexp.MustCompile(`(?:显示|写着|写有|刻着|刻有|刻写|聚成|拼出|铭文|题字|文字|古字)[^。；;！!？?「」]{0,24}[：:]?\s*$`)
var seedanceExplicitSpeechTailRE = regexp.MustCompile(`(?:说|道|喊|问)[：:]\s*$`)

func isSeedanceDiegeticText(before string) bool {
	if seedanceExplicitSpeechTailRE.MatchString(before) {
		return false
	}
	return seedanceDiegeticTextTailRE.MatchString(before)
}

func seedanceQuoteEmpty(s string) bool {
	s = strings.TrimSpace(s)
	switch s {
	case "", "无", "无台词", "（无）", "(无)", "—", "–":
		return true
	default:
		return false
	}
}

func seedanceSpeechClause(sub seedanceSubject, quote string, voices []CharacterVoice) string {
	who := sub.tag
	if who == "" && len(sub.names) > 0 {
		who = sub.names[0]
	}
	if who == "" {
		who = "角色"
	}
	clause := who + " 说 {" + quote + "}"
	if v := voiceForSubject(sub, voices); v != "" {
		clause += "，音色：" + v
	}
	return clause
}

func inferSeedanceSpeaker(before, quote string, subjects []seedanceSubject) seedanceSubject {
	tail := strings.TrimSpace(before)
	if m := seedanceSpeakerTailRE.FindString(tail); m != "" {
		name := strings.TrimRight(m, "：:说道喊问 \t")
		if sub, ok := matchSeedanceSubject(name, subjects); ok {
			return sub
		}
	}
	if listener, ok := honorificListener(quote, subjects); ok {
		if other := firstSubjectIn(before, subjects, listener.tag); other.tag != "" {
			return other
		}
	}
	if sub := firstSubjectIn(before, subjects, ""); sub.tag != "" {
		return sub
	}
	if len(subjects) == 1 {
		return subjects[0]
	}
	return seedanceSubject{}
}

func honorificListener(quote string, subjects []seedanceSubject) (seedanceSubject, bool) {
	for _, sub := range subjects {
		for _, name := range sub.names {
			runes := []rune(name)
			if len(runes) == 0 {
				continue
			}
			last := string(runes[len(runes)-1])
			for _, hon := range []string{last + "哥", last + "姐", last + "叔", last + "哥儿", name} {
				if hon != name && strings.Contains(quote, hon) {
					return sub, true
				}
			}
		}
	}
	return seedanceSubject{}, false
}

func firstSubjectIn(text string, subjects []seedanceSubject, skipTag string) seedanceSubject {
	earliest := -1
	var found seedanceSubject
	for _, sub := range subjects {
		if skipTag != "" && sub.tag == skipTag {
			continue
		}
		for _, name := range sub.names {
			if name == "" {
				continue
			}
			i := strings.Index(text, name)
			if i < 0 {
				continue
			}
			if earliest < 0 || i < earliest {
				earliest = i
				found = sub
			}
		}
	}
	return found
}

func matchSeedanceSubject(name string, subjects []seedanceSubject) (seedanceSubject, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return seedanceSubject{}, false
	}
	for _, sub := range subjects {
		for _, n := range sub.names {
			if n == name || strings.Contains(n, name) || strings.Contains(name, n) {
				return sub, true
			}
		}
	}
	return seedanceSubject{}, false
}

func voiceForSubject(sub seedanceSubject, voices []CharacterVoice) string {
	for _, v := range voices {
		vn := strings.TrimSpace(v.Name)
		if vn == "" {
			continue
		}
		for _, n := range sub.names {
			if n == vn || strings.Contains(n, vn) || strings.Contains(vn, n) {
				return strings.TrimSpace(v.Prompt)
			}
		}
	}
	return ""
}

func wrapSeedanceSFX(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := seedanceSFXSplitRE.Split(raw, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "。；;，, ")
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "<") && strings.HasSuffix(p, ">") {
			out = append(out, p)
			continue
		}
		out = append(out, "<"+p+">")
	}
	return strings.Join(out, "，")
}

func replaceSubjectsOutsideBraces(s string, subjects []seedanceSubject) string {
	if len(subjects) == 0 || s == "" {
		return s
	}
	parts := strings.Split(s, "{")
	for i, part := range parts {
		if i == 0 {
			parts[i] = replaceSubjectNames(part, subjects)
			continue
		}
		close := strings.Index(part, "}")
		if close < 0 {
			parts[i] = replaceSubjectNames(part, subjects)
			continue
		}
		parts[i] = part[:close+1] + replaceSubjectNames(part[close+1:], subjects)
	}
	return strings.Join(parts, "{")
}

func replaceSubjectNames(s string, subjects []seedanceSubject) string {
	type pair struct {
		name string
		tag  string
	}
	pairs := make([]pair, 0)
	for _, sub := range subjects {
		for _, name := range sub.names {
			if name != "" {
				pairs = append(pairs, pair{name, sub.tag})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return len([]rune(pairs[i].name)) > len([]rune(pairs[j].name))
	})
	for _, p := range pairs {
		s = strings.ReplaceAll(s, p.name, p.tag)
	}
	return s
}

func seedanceSubjects(refs []refImage) []seedanceSubject {
	subjectN := 1
	out := make([]seedanceSubject, 0)
	for _, r := range refs {
		if r.Kind != "character" {
			continue
		}
		names := uniqueSeedanceNames(r.Label, r.Name, identityNameFromRef(r))
		if len(names) == 0 {
			continue
		}
		out = append(out, seedanceSubject{
			tag:   fmt.Sprintf("<主体%d>", subjectN),
			names: names,
		})
		subjectN++
	}
	return out
}

func identityNameFromRef(r refImage) string {
	for _, raw := range []string{r.Label, r.Name} {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		for _, sep := range []string{"（", "(", "，", ",", " · ", "·"} {
			if i := strings.Index(s, sep); i > 0 {
				s = strings.TrimSpace(s[:i])
			}
		}
		s = strings.TrimPrefix(s, "穿上衣服的")
		s = strings.TrimPrefix(s, "赤膊的")
		return strings.TrimSpace(s)
	}
	return ""
}

func uniqueSeedanceNames(vals ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
		for _, sep := range []string{"（", "(", "，", " · "} {
			if i := strings.Index(v, sep); i > 0 {
				head := strings.TrimSpace(v[:i])
				if head != "" && !seen[head] {
					seen[head] = true
					out = append(out, head)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return len([]rune(out[i])) > len([]rune(out[j]))
	})
	return out
}

func hasSeedanceSpeech(s string) bool {
	return strings.Contains(s, " 说 {") || strings.Contains(s, "说 {")
}
