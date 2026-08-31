package crew

import (
	"fmt"
	"regexp"
	"strings"
)

type SpokenLine struct {
	Speaker string
	Text    string
}

var (
	skipSpokenLineRE  = regexp.MustCompile(`(?i)^\s*(?:\*{0,2})\s*(?:出场人物|场景|场次|时间|地点|音乐提示|音效|字幕|内景|外景)\s*[:：]`)
	sceneHeadSkipRE   = regexp.MustCompile(`(?i)^\s*(?:\*{0,2})\s*(?:场次\s*[一二三四五六七八九十百\d]+|\d+\s*[-–—]\s*\d+|第\s*\d+\s*场)`)
	episodeHeadSkipRE = regexp.MustCompile(`(?i)^\s*(?:#{1,6}\s*)?第\s*\d+\s*集\s*[：:]`)
	spokenAssignRE    = regexp.MustCompile(`^\s*(?:\*{0,2})\s*([\p{Han}A-Za-z0-9甲乙丙丁]{1,12})(?:\*{0,2})\s*(?:[（(][^）)]{0,40}[）)])?\s*(?:说(?:道|了)?)?\s*[：:]\s*(.*)$`)
	inlineSpokenRE    = regexp.MustCompile(`(?:^|[，。；、\s])([\p{Han}A-Za-z0-9甲乙丙丁]{1,12})(?:（[^）]{0,24}）)?(?:说(?:道|了)?|起哄|喊|问|道|残影音)[^：:「」\n]{0,12}[：:]\s*[「“]([^」”]+)[」”]`)
	generatedSpokenRE = regexp.MustCompile(`[\p{Han}A-Za-z0-9甲乙丙丁]{1,12}(?:（[^）]{0,24}）)?(?:说(?:道|了)?|喊|问|道)\s*[：:]?\s*「[^」]*」`)
)

func ExtractSpokenLines(script string) []SpokenLine {
	out := make([]SpokenLine, 0)
	for _, raw := range strings.Split(script, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if skipSpokenLineRE.MatchString(line) || sceneHeadSkipRE.MatchString(line) || episodeHeadSkipRE.MatchString(line) {
			continue
		}
		if strings.HasPrefix(line, "△") || strings.HasPrefix(line, "▲") {
			for _, m := range inlineSpokenRE.FindAllStringSubmatch(line, -1) {
				if len(m) >= 3 && strings.TrimSpace(m[1]) != "" && strings.TrimSpace(m[2]) != "" {
					out = append(out, SpokenLine{Speaker: strings.TrimSpace(m[1]), Text: strings.TrimSpace(m[2])})
				}
			}
			continue
		}
		m := spokenAssignRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		speaker := strings.TrimSpace(m[1])
		text := parseDialogueBody(m[2])
		if speaker == "" || text == "" {
			continue
		}
		out = append(out, SpokenLine{Speaker: speaker, Text: text})
	}
	return out
}

func parseDialogueBody(rest string) string {
	rest = strings.TrimSpace(rest)
	rest = stripStageDirection(rest)
	if rest == "" {
		return ""
	}
	if text, ok := unquoteDialogue(rest); ok {
		return text
	}
	return strings.TrimSpace(rest)
}

func unquoteDialogue(s string) (string, bool) {
	s = strings.TrimSpace(s)
	pairs := [][2]string{
		{"「", "」"},
		{"『", "』"},
		{"“", "”"},
		{"\"", "\""},
		{"'", "'"},
	}
	for _, p := range pairs {
		if strings.HasPrefix(s, p[0]) {
			body := s[len(p[0]):]
			if i := strings.Index(body, p[1]); i >= 0 {
				return strings.TrimSpace(body[:i]), true
			}
			return strings.TrimSpace(strings.TrimSuffix(body, p[1])), true
		}
	}
	return "", false
}

func formatLockedDialogue(script string) string {
	lines := ExtractSpokenLines(script)
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n【锁定台词 · 必须按序号原样写入「」，一字不改、不换说话人、不翻译成古白、不删 2024/穿越 等原词】\n")
	for i, line := range lines {
		b.WriteString(fmt.Sprintf("%d. %s说：「%s」\n", i+1, line.Speaker, line.Text))
	}
	b.WriteString("分镜里的「」必须能在上表找到原文。禁止改写成同义句。一句太长就整句拆到下一镜，禁止缩短。\n")
	return b.String()
}

// ScheduleStoryboardDialogue treats the model output as visual blocking only.
// Dialogue is removed and rebuilt deterministically from the locked screenplay:
// original order and speakers are preserved, and long lines create continuation
// shots instead of being truncated to whatever empty beats the model happened
// to leave behind.
func ScheduleStoryboardDialogue(shots []StoryboardShot, script string, coveredScripts []string) []StoryboardShot {
	lines := ExtractSpokenLines(script)
	if len(shots) == 0 || len(lines) == 0 {
		return shots
	}
	covered := make([]bool, len(lines))
	for i, line := range lines {
		covered[i] = dialogueCoveredByScripts(line.Text, coveredScripts)
	}

	anchors := make([]int, len(lines))
	prev := 0
	for i, line := range lines {
		bestIdx, bestScore := -1, 0
		for si, shot := range shots {
			if si < prev {
				continue
			}
			for _, q := range quotesInScript(shot.Script) {
				score := spokenTextMatchScore(q, line.Text)
				if score > 0 && strings.Contains(shot.Script, line.Speaker) {
					score += 10
				}
				if score > bestScore {
					bestIdx, bestScore = si, score
				}
			}
		}
		if bestIdx < 0 {
			for si := prev; si < len(shots); si++ {
				if strings.Contains(shots[si].Script, line.Speaker) || strings.Contains(shots[si].Label, line.Speaker) {
					bestIdx = si
					break
				}
			}
		}
		if bestIdx < 0 {
			bestIdx = prev
			if bestIdx >= len(shots) {
				bestIdx = len(shots) - 1
			}
		}
		anchors[i] = bestIdx
		prev = bestIdx
	}

	cleaned := make([]StoryboardShot, len(shots))
	copy(cleaned, shots)
	for i := range cleaned {
		cleaned[i].Script = stripGeneratedDialogue(cleaned[i].Script)
	}
	buckets := make(map[int][]SpokenLine)
	for i, line := range lines {
		if covered[i] {
			continue
		}
		anchor := anchors[i]
		buckets[anchor] = append(buckets[anchor], line)
	}
	out := make([]StoryboardShot, 0, len(cleaned)+len(lines))
	for i, shot := range cleaned {
		placed, overflow := placeDialogueOnStoryboardShot(shot, buckets[i])
		out = append(out, placed)
		out = append(out, overflow...)
	}
	return out
}

func placeDialogueOnStoryboardShot(shot StoryboardShot, spoken []SpokenLine) (StoryboardShot, []StoryboardShot) {
	if len(spoken) == 0 {
		return shot, nil
	}
	lines := strings.Split(shot.Script, "\n")
	beatCursor := 0
	overflow := make([]StoryboardShot, 0)
	for _, speech := range spoken {
		rest := strings.TrimSpace(speech.Text)
		for rest != "" {
			beatIdx := -1
			for beatCursor < len(lines) {
				idx := beatCursor
				beatCursor++
				if secs := beatSeconds(lines[idx]); secs > 0 && !dialogueRE.MatchString(lines[idx]) {
					beatIdx = idx
					break
				}
			}
			if beatIdx < 0 {
				overflow = append(overflow, dialogueStoryboardShots(SpokenLine{Speaker: speech.Speaker, Text: rest}, shot)...)
				break
			}
			budgetSeconds := float64(beatSeconds(lines[beatIdx]))
			if budgetSeconds > 4.5 {
				budgetSeconds = 4.5
			}
			keep, tail := splitQuoteForBeat(rest, budgetSeconds)
			if keep == "" {
				// No punctuation-safe split fits this beat. Move the complete line
				// forward instead of creating 靠/谱人 style fragments.
				overflow = append(overflow, dialogueStoryboardShots(SpokenLine{Speaker: speech.Speaker, Text: rest}, shot)...)
				break
			}
			lines[beatIdx] = strings.TrimRight(lines[beatIdx], "；;，, ") + "；" + speech.Speaker + "说：「" + keep + "」"
			rest = strings.TrimSpace(tail)
		}
	}
	shot.Script = strings.Join(lines, "\n")
	for _, speech := range spoken {
		shot.CharacterNames = cleanNameList(append(shot.CharacterNames, speech.Speaker))
	}
	return shot, overflow
}

func stripGeneratedDialogue(script string) string {
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		line = generatedSpokenRE.ReplaceAllString(line, "")
		// A model occasionally emits a bare quote without a speaker. It is still
		// generated dialogue and must not compete with the locked screenplay.
		line = dialogueRE.ReplaceAllString(line, "")
		lines[i] = cleanupScript(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func dialogueStoryboardShots(line SpokenLine, base StoryboardShot) []StoryboardShot {
	chunks := make([]string, 0, 4)
	rest := strings.TrimSpace(line.Text)
	for rest != "" {
		keep, tail := splitQuoteForBeat(rest, 3) // at 4 chars/sec: <=12 spoken runes
		if keep == "" {
			// No safe punctuation boundary: keep the sentence intact. A longer
			// delivery is preferable to corrupting a Chinese word at a rune quota.
			keep, tail = rest, ""
		}
		chunks = append(chunks, keep)
		rest = strings.TrimSpace(tail)
	}
	out := make([]StoryboardShot, 0, (len(chunks)+1)/2)
	for i := 0; i < len(chunks); i += 2 {
		label := line.Speaker + "对白"
		if i > 0 {
			label += " · 续"
		}
		listener := firstListenerName(line.Speaker, base.CharacterNames)
		parts := []string{fmt.Sprintf("【0-3秒】镜头：近景，%s(中中)3/4正面朝右开口，目光锁定听者以确认其态度；音效：配乐床、环境声；%s说：「%s」", line.Speaker, line.Speaker, chunks[i])}
		if i+1 < len(chunks) {
			parts = append(parts, fmt.Sprintf("【3-7秒】镜头：换景别，%s(中中)位置不变，继续盯住听者把关键信息说完；音效：配乐床、环境声；%s说：「%s」", line.Speaker, line.Speaker, chunks[i+1]))
		} else {
			parts = append(parts, fmt.Sprintf("【3-7秒】镜头：%s；音效：配乐床、环境声", listenerResponseBeat(line.Speaker, listener)))
		}
		parts = append(parts, fmt.Sprintf("【7-10秒】镜头：%s(中中)承接上一拍位置不变，盯住对方的眼神等待明确答复，维持对话压力；音效：配乐床、环境声", line.Speaker))
		out = append(out, StoryboardShot{
			Label: label, Duration: 10, Script: strings.Join(parts, "\n"),
			CharacterNames: cleanNameList(append([]string{line.Speaker}, base.CharacterNames...)),
			SceneName:      base.SceneName, PropNames: append([]string{}, base.PropNames...),
		})
	}
	return out
}

func firstListenerName(speaker string, names []string) string {
	speaker = strings.TrimSpace(speaker)
	for _, name := range cleanNameList(names) {
		name = strings.TrimSpace(name)
		if name != "" && name != speaker {
			return name
		}
	}
	return ""
}

func listenerResponseBeat(speaker, listener string) string {
	if listener != "" {
		return fmt.Sprintf("中近景，%s(右中)3/4正面朝左先看向%s所指之处，再抬眼与其对视，以核实话中信息但暂不表态", listener, speaker)
	}
	return fmt.Sprintf("过肩近景，画外听者沿%s所指方向核实话中信息；%s(中中)承接上一拍位置不变，观察对方视线以判断其态度", speaker, speaker)
}

func RestoreStoryboardDialogue(shots []StoryboardShot, script string) []StoryboardShot {
	return RestoreStoryboardDialogueSkippingCovered(shots, script, nil)
}

func RestoreStoryboardDialogueSkippingCovered(shots []StoryboardShot, script string, coveredScripts []string) []StoryboardShot {
	return restoreStoryboardDialogueSkippingCovered(shots, script, coveredScripts, true)
}

func restoreStoryboardDialogueSkippingCovered(shots []StoryboardShot, script string, coveredScripts []string, appendLeftover bool) []StoryboardShot {
	orig := ExtractSpokenLines(script)
	if len(orig) == 0 || len(shots) == 0 {
		return shots
	}
	type hit struct {
		shot  int
		line  int
		full  string
		quote string
	}
	hits := make([]hit, 0)
	for si, shot := range shots {
		for li, raw := range strings.Split(normalizeScriptForQC(shot.Script), "\n") {
			q := firstQuoteInLine(raw)
			if q == "" || quoteIsEmpty(q) {
				continue
			}
			hits = append(hits, hit{shot: si, line: li, full: raw, quote: q})
		}
	}
	used := make([]bool, len(orig))
	for j, line := range orig {
		used[j] = dialogueCoveredByScripts(line.Text, coveredScripts)
	}
	assign := make([]int, len(hits))
	for i := range assign {
		assign[i] = -1
	}
	// Prefer text match so paraphrased/wrong-speaker lines still lock to the right original.
	// Do NOT expand a short model tail into the full script line (that jams 【0-3秒】).
	for i, h := range hits {
		best, bestScore := -1, 0
		for j, line := range orig {
			if used[j] {
				continue
			}
			score := spokenTextMatchScore(h.quote, line.Text)
			if score > bestScore {
				best, bestScore = j, score
			}
		}
		if best >= 0 && bestScore >= 2 && !shouldSkipExpandingRestore(h.quote, orig[best].Text) {
			assign[i] = best
			used[best] = true
		}
	}
	// Fall back to remaining originals in order for unmatched hits.
	oi := 0
	for i := range hits {
		if assign[i] >= 0 {
			continue
		}
		for oi < len(orig) && used[oi] {
			oi++
		}
		if oi >= len(orig) {
			break
		}
		if shouldSkipExpandingRestore(hits[i].quote, orig[oi].Text) {
			// Leave this hit as the model wrote it; keep original for leftover / later beat.
			oi++
			continue
		}
		assign[i] = oi
		used[oi] = true
		oi++
	}
	for i, h := range hits {
		j := assign[i]
		if j < 0 {
			continue
		}
		lines := strings.Split(shots[h.shot].Script, "\n")
		if h.line < 0 || h.line >= len(lines) {
			continue
		}
		lines[h.line] = lockSpokenIntoLine(lines[h.line], orig[j])
		shots[h.shot].Script = strings.Join(lines, "\n")
	}
	leftover := make([]SpokenLine, 0)
	for j, line := range orig {
		if used[j] {
			continue
		}
		already := false
		for _, shot := range shots {
			for _, q := range quotesInScript(shot.Script) {
				if quotesSubstantivelyDuplicate(q, line.Text) {
					already = true
					break
				}
				// Original already appears as a suffix of a longer spoken beat — don't
				// append another leftover shot that only re-says the reminder tail.
				qk, lk := normalizeQuoteKey(q), normalizeQuoteKey(line.Text)
				if lk != "" && speechRunes(line.Text) >= 6 && strings.HasSuffix(qk, lk) {
					already = true
					break
				}
			}
			if already {
				break
			}
		}
		if !already {
			already = dialogueCoveredByScripts(line.Text, coveredScripts)
		}
		if !already {
			leftover = append(leftover, line)
		}
	}
	if len(leftover) > 0 && appendLeftover {
		shots = append(shots, leftoverSpokenShots(leftover)...)
	}
	return shots
}

func spokenTextMatchScore(got, want string) int {
	g := quoteKey(got)
	w := quoteKey(want)
	if g == "" || w == "" {
		return 0
	}
	if g == w {
		return 100
	}
	gN, wN := speechRunes(got), speechRunes(want)
	if strings.Contains(w, g) || strings.Contains(g, w) {
		shorter, longer := gN, wN
		if shorter > longer {
			shorter, longer = longer, shorter
		}
		if longer > 0 && shorter*10 >= longer*8 && longer >= 4 {
			return 50
		}
		gk, wk := normalizeQuoteKey(got), normalizeQuoteKey(want)
		// Model kept the head of the script line (duration trim) — allow lock.
		if gk != "" && wk != "" && strings.HasPrefix(wk, gk) && gN < wN {
			return 45
		}
		// Model only wrote a short tail (reminder repeat) — do not score as a lock.
		if gk != "" && wk != "" && strings.HasSuffix(wk, gk) && !strings.HasPrefix(wk, gk) && gN < wN {
			return 0
		}
		if longer >= 4 {
			return 3
		}
		return 0
	}
	// Shared head after stripping leading ellipsis — common when packing truncates.
	gt := strings.TrimLeft(g, ".…·・")
	wt := strings.TrimLeft(w, ".…·・")
	if gt != "" && wt != "" && (strings.HasPrefix(wt, gt) || strings.HasPrefix(gt, wt)) {
		return 40
	}
	return 0
}

// shouldSkipExpandingRestore is true only when locking want would replace a short
// model *tail* with a much longer script line (e.g. 「少惹姚三刀」→ full lecture).
// Prefix trims and paraphrases still restore so 零删改 holds.
func shouldSkipExpandingRestore(got, want string) bool {
	gN, wN := speechRunes(got), speechRunes(want)
	if wN <= gN {
		return false
	}
	if gN*10 >= wN*8 {
		return false
	}
	gk, wk := normalizeQuoteKey(got), normalizeQuoteKey(want)
	if gk == "" || wk == "" {
		return false
	}
	if strings.HasSuffix(wk, gk) && !strings.HasPrefix(wk, gk) {
		return true
	}
	return false
}

func lockSpokenIntoLine(line string, orig SpokenLine) string {
	replQuote := "「" + orig.Text + "」"
	speaker := strings.TrimSpace(orig.Speaker)
	if loc := dialogueRE.FindStringIndex(line); loc != nil {
		prefix := line[:loc[0]]
		suffix := line[loc[1]:]
		if speaker != "" {
			if m := speakerSayRE.FindAllStringSubmatchIndex(prefix, -1); len(m) > 0 {
				last := m[len(m)-1]
				prefix = prefix[:last[0]] + speaker + "说："
			} else {
				prefix = strings.TrimRight(prefix, "；;，,  ")
				if prefix != "" && !strings.HasSuffix(prefix, "；") {
					prefix += "；"
				}
				prefix += speaker + "说："
			}
		}
		return prefix + replQuote + suffix
	}
	trim := strings.TrimRight(line, "；;，, ")
	repl := speaker + "说：" + replQuote
	if trim == "" {
		return "【0-3秒】镜头：中景，" + speaker + "说话；音效：环境声；" + repl
	}
	return trim + "；" + repl
}

func leftoverSpokenShots(lines []SpokenLine) []StoryboardShot {
	out := make([]StoryboardShot, 0, len(lines))
	for _, line := range lines {
		speaker := line.Speaker
		out = append(out, StoryboardShot{
			Label:    speaker + "对白",
			Duration: 10,
			Script: fmt.Sprintf(
				"【0-3秒】镜头：中景，%s(中中)3/4正面朝右开口，目光锁定画外听者以确认其态度；音效：环境声；%s说：「%s」\n【3-7秒】镜头：过肩近景，画外听者沿%s所指方向核实话中信息；%s(中中)承接上一拍位置不变，观察对方视线以判断其态度；音效：环境声\n【7-10秒】镜头：%s(中中)承接上一拍位置不变，盯住对方的眼神等待明确答复，维持对话压力；音效：环境声",
				speaker, speaker, line.Text, speaker, speaker, speaker,
			),
			CharacterNames: []string{speaker},
		})
	}
	return out
}

func RestoreShotContextDialogue(shots []ShotContext, script string) []ShotContext {
	return RestoreShotContextDialogueSkippingCovered(shots, script, nil)
}

func RestoreShotContextDialogueSkippingCovered(shots []ShotContext, script string, coveredScripts []string) []ShotContext {
	return restoreShotContextDialogueSkippingCovered(shots, script, coveredScripts, true)
}

func restoreShotContextDialogueSkippingCovered(shots []ShotContext, script string, coveredScripts []string, appendLeftover bool) []ShotContext {
	if len(shots) == 0 || strings.TrimSpace(script) == "" {
		return shots
	}
	tmp := make([]StoryboardShot, len(shots))
	for i, s := range shots {
		tmp[i] = StoryboardShot{Label: s.Label, Duration: s.Duration, Script: s.Script}
	}
	restored := restoreStoryboardDialogueSkippingCovered(tmp, script, coveredScripts, appendLeftover)
	out := make([]ShotContext, 0, len(restored))
	for i, s := range restored {
		if i >= len(shots) {
			out = append(out, ShotContext{
				Label: s.Label, Script: s.Script, Duration: ShotMaxSeconds(s.Duration),
			})
			continue
		}
		sh := shots[i]
		sh.Script = s.Script
		out = append(out, sh)
	}
	return out
}
