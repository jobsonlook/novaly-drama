package crew

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

const DefaultShotSeconds = 10

func ShotMaxSeconds(duration int) int {
	if duration <= 0 {
		return DefaultShotSeconds
	}
	if duration > 30 {
		return 30
	}
	return duration
}

func ScriptEndSeconds(script string) int {
	return scriptTimelineEnd(script)
}

// scriptTimelineEnd returns the latest end second from every 【起止秒】 header,
// including multiple headers jammed onto one physical line.
func scriptTimelineEnd(script string) int {
	end := 0
	for _, line := range strings.Split(script, "\n") {
		for _, m := range beatRangeRE.FindAllStringSubmatch(line, -1) {
			if len(m) < 3 {
				continue
			}
			e := 0
			fmt.Sscanf(m[2], "%d", &e)
			if e > end {
				end = e
			}
		}
	}
	return end
}

func SplitScriptOverflow(script string, max int) (keep, overflow string) {
	max = ShotMaxSeconds(max)
	script = normalizeBeatTimeline(script)
	var keepLines, overLines []string
	overflowing := false
	keptEnd := 0
	for _, line := range strings.Split(script, "\n") {
		trim := strings.TrimSpace(line)
		if start, end, ok := beatRange(trim); ok {
			// 【9-13秒】 starts before 10 but ends past it — old logic kept the whole beat.
			if start >= max || end > max {
				overflowing = true
			}
		} else if trim != "" && keptEnd >= max {
			// Orphan dialogue after a filled timeline (no 【秒】 header) must move too.
			overflowing = true
		}
		if overflowing {
			if trim != "" {
				overLines = append(overLines, trim)
			}
			continue
		}
		keepLines = append(keepLines, line)
		if _, end, ok := beatRange(trim); ok && end > keptEnd {
			keptEnd = end
		}
	}
	keep = strings.TrimSpace(strings.Join(keepLines, "\n"))
	overflow = remapScriptToZero(strings.TrimSpace(strings.Join(overLines, "\n")))
	overflow = dropQuotesAlreadyPresent(keep, overflow)
	return keep, overflow
}

// FinalizeShotScript hard-clamps a single shot to its duration budget:
// strip meta junk, fix repeated 【0-10秒】 / overlapping beats, drop beats past
// max, then stretch the last in-budget beat to fill max so we don't leave a "9秒" ending.
func FinalizeShotScript(script string, max int) string {
	max = ShotMaxSeconds(max)
	script = NormalizeShotTimeline(script, max)
	script = collapsePlotlessBeats(script, max)
	if needsContentRetime(script, max) {
		script = retimeBeatsByContent(script, max)
	}
	keep, _ := SplitScriptOverflow(script, max)
	deduped := DedupeDialogueAcrossShots([]ShotContext{{Script: keep, Duration: max}})
	if len(deduped) > 0 {
		keep = strings.TrimSpace(deduped[0].Script)
	}
	return fillShotScriptToDuration(keep, max)
}

// FinalizeShotScriptPreservingDialogue is for dialogue rebuilt from the locked
// screenplay. It keeps faithful modern/meta wording and short continuation
// tails; only timeline, overflow and duration normalization are applied.
func FinalizeShotScriptPreservingDialogue(script string, max int) string {
	max = ShotMaxSeconds(max)
	script = NormalizeShotTimelinePreservingDialogue(script, max)
	script = collapsePlotlessBeats(script, max)
	if needsContentRetime(script, max) {
		script = retimeBeatsByContent(script, max)
	}
	keep, _ := SplitScriptOverflow(script, max)
	return fillShotScriptToDuration(keep, max)
}

// PolishSavedShotScript normalizes hand-edited shot scripts without episode-wide
// Pack or content retime — only expands literal \n and splits embedded beats.
func PolishSavedShotScript(script string, max int) string {
	_ = ShotMaxSeconds(max)
	return strings.TrimSpace(normalizeScriptForQC(script))
}

// NormalizeShotTimeline fixes beat headers and overlaps in-place without clipping overflow.
// Use before Pack so 【10-13秒】 can move to the next shot.
func NormalizeShotTimeline(script string, max int) string {
	max = ShotMaxSeconds(max)
	script = normalizeScriptBeatStructure(script)
	script = stripMetaJunkDialogue(script)
	return retimeOverlappingOrFullSpanBeats(script, max)
}

func NormalizeShotTimelinePreservingDialogue(script string, max int) string {
	max = ShotMaxSeconds(max)
	script = normalizeScriptBeatStructure(script)
	return retimeOverlappingOrFullSpanBeats(script, max)
}

// normalizeScriptBeatStructure splits multiple 【秒】 headers on one line onto
// separate rows and drops dangling 「X说：」 tails that would swallow the next beat.
func normalizeScriptBeatStructure(script string) string {
	script = splitEmbeddedBeatLines(script)
	return repairDanglingSpeakerBeats(script)
}

func splitEmbeddedBeatLines(script string) string {
	script = strings.TrimSpace(script)
	if script == "" {
		return script
	}
	var out []string
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		locs := beatRangeRE.FindAllStringIndex(line, -1)
		if len(locs) <= 1 {
			out = append(out, line)
			continue
		}
		for i, loc := range locs {
			end := len(line)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			seg := strings.TrimSpace(line[loc[0]:end])
			if seg != "" {
				out = append(out, seg)
			}
		}
	}
	return strings.Join(out, "\n")
}

func repairDanglingSpeakerBeats(script string) string {
	lines := strings.Split(script, "\n")
	for i := range lines {
		trim := strings.TrimSpace(lines[i])
		if !strings.Contains(trim, "秒】") || strings.Contains(trim, "「") {
			continue
		}
		if !strings.Contains(trim, "说：") && !strings.Contains(trim, "内心独白：") {
			continue
		}
		// Trailing 「角色说：」 / 「角色内心独白：」 with no 「…」 on this beat.
		lines[i] = danglingSpeakerTailRE.ReplaceAllString(trim, "")
		lines[i] = danglingInnerMonologueTailRE.ReplaceAllString(lines[i], "")
		lines[i] = cleanupScript(lines[i])
	}
	return strings.Join(lines, "\n")
}

// collapsePlotlessBeats merges timed rows that only carry BGM bed / empty reaction
// into the previous beat so 3 seconds are not wasted on 「音效：紧张鼓点」 alone.
func collapsePlotlessBeats(script string, max int) string {
	_ = max
	script = strings.TrimSpace(script)
	if script == "" {
		return script
	}
	lines := strings.Split(script, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if !strings.Contains(trim, "秒】") {
			if trim != "" {
				out = append(out, line)
			}
			continue
		}
		if beatIsPlotless(trim) && len(out) > 0 && strings.Contains(out[len(out)-1], "秒】") {
			out[len(out)-1] = mergePlotlessBeatInto(out[len(out)-1], trim)
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func beatIsPlotless(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.Contains(line, "秒】") {
		return false
	}
	if beatHasSpokenQuote(line) {
		return false
	}
	body := strings.TrimSpace(beatHeaderRE.ReplaceAllString(line, ""))
	body = strings.TrimSpace(sfxFieldRE.ReplaceAllString(body, ""))
	body = strings.Trim(body, "；;")
	if body == "" {
		return true
	}
	lens := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(body, "镜头:"), "镜头："))
	lens = strings.Trim(lens, "；;")
	return lensIsPlotless(lens)
}

func lensIsPlotless(lens string) bool {
	lens = strings.TrimSpace(lens)
	if lens == "" {
		return true
	}
	for _, filler := range []string{
		"反应", "余韵", "停顿", "停", "环境", "听者反应", "维持机位不变",
		"过渡", "延续", "停顿或环境", "环境声", "定镜",
	} {
		if lens == filler || strings.HasPrefix(lens, filler+"；") || strings.HasPrefix(lens, filler+"，") {
			return true
		}
	}
	return false
}

func mergePlotlessBeatInto(dest, src string) string {
	srcBody := strings.TrimSpace(beatHeaderRE.ReplaceAllString(src, ""))
	if srcBody == "" {
		return dest
	}
	srcSfx := ""
	if m := sfxFieldRE.FindStringSubmatch(srcBody); len(m) >= 2 {
		srcSfx = strings.TrimSpace(m[1])
	}
	if srcSfx != "" {
		dest = mergeSFXPartsIntoLine(dest, srcSfx)
	}
	// If the tail beat had substantive lens (rare), append it to the previous 镜头.
	srcLens := extractLensBody(srcBody)
	if srcLens != "" && !lensIsPlotless(srcLens) {
		dest = appendLensTail(dest, srcLens)
	}
	return dest
}

func extractLensBody(body string) string {
	body = strings.TrimSpace(sfxFieldRE.ReplaceAllString(body, ""))
	body = strings.Trim(body, "；;")
	if !strings.HasPrefix(body, "镜头：") && !strings.HasPrefix(body, "镜头:") {
		return ""
	}
	lens := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(body, "镜头："), "镜头:"))
	return strings.Trim(lens, "；;")
}

func appendLensTail(line, tail string) string {
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return line
	}
	if loc := strings.Index(line, "镜头："); loc >= 0 {
		rest := line[loc+len("镜头："):]
		cut := len(rest)
		for _, sep := range []string{"；音效：", "；音效:", "；"} {
			if i := strings.Index(rest, sep); i >= 0 && i < cut {
				cut = i
			}
		}
		return line[:loc+len("镜头：")] + strings.TrimSpace(rest[:cut]) + "，" + tail + rest[cut:]
	}
	return line
}

func mergeSFXPartsIntoLine(line, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return line
	}
	if !strings.Contains(line, "音效：") && !strings.Contains(line, "音效:") {
		return strings.TrimRight(line, "； ") + "；音效：" + extra
	}
	return sfxFieldRE.ReplaceAllStringFunc(line, func(field string) string {
		body := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(field, "音效："), "音效:"))
		seen := map[string]bool{}
		parts := make([]string, 0, 4)
		add := func(p string) {
			p = strings.TrimSpace(p)
			if p == "" || seen[p] {
				return
			}
			seen[p] = true
			parts = append(parts, p)
		}
		for _, p := range sfxSplitRE.Split(body+"、"+extra, -1) {
			add(p)
		}
		return "音效：" + strings.Join(parts, "、")
	})
}

// stripMetaJunkDialogue removes fourth-wall / review / danmaku lines the model
// sometimes invents (e.g. 「……差评。」).
func stripMetaJunkDialogue(script string) string {
	script = strings.TrimSpace(script)
	if script == "" {
		return script
	}
	var keep []string
	for _, line := range strings.Split(script, "\n") {
		q := firstQuoteInLine(line)
		if q != "" && isMetaJunkDialogue(q) {
			line = stripDialogueQuote(line, q)
			line = strings.TrimSpace(strings.Trim(line, "；;"))
			rest := strings.TrimSpace(beatHeaderRE.ReplaceAllString(line, ""))
			if rest == "" {
				continue
			}
		}
		if strings.TrimSpace(line) != "" {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}

func isMetaJunkDialogue(q string) bool {
	k := normalizeQuoteKey(q)
	if k == "" {
		return false
	}
	junk := []string{"差评", "好评", "弹幕", "第四面墙", "ai写的", "模型胡编", "本分镜", "观众朋友"}
	lower := strings.ToLower(k)
	for _, j := range junk {
		if strings.Contains(lower, j) {
			return true
		}
	}
	return false
}

// retimeOverlappingOrFullSpanBeats fixes the common model mistake of writing
// three lines all labeled 【0-10秒】 (or overlapping ranges like 【3-10秒】 after
// 【3-7秒】) instead of 【0-3秒】【3-7秒】【7-10秒】. Bodies are kept; only headers change.
func retimeOverlappingOrFullSpanBeats(script string, max int) string {
	max = ShotMaxSeconds(max)
	script = strings.TrimSpace(script)
	if script == "" {
		return script
	}
	lines := strings.Split(script, "\n")
	type beatLine struct {
		idx        int
		start, end int
		line       string
	}
	beats := make([]beatLine, 0, 4)
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		start, end, ok := beatRange(trim)
		if !ok {
			continue
		}
		beats = append(beats, beatLine{idx: i, start: start, end: end, line: line})
	}
	if len(beats) < 2 {
		return script
	}
	fullSpan := 0
	sameRange := true
	allStartZero := true
	anyNearFull := false
	for i, b := range beats {
		if b.start == 0 && b.end >= max {
			fullSpan++
		}
		if b.end-b.start >= max-1 {
			anyNearFull = true
		}
		if b.start != 0 {
			allStartZero = false
		}
		if i > 0 && (b.start != beats[0].start || b.end != beats[0].end) {
			sameRange = false
		}
	}
	// Retimes when: multiple full/near-full spans, all start at 0, identical ranges,
	// or overlapping ranges (e.g. 【3-10秒】 after 【3-7秒】).
	// Do NOT retime solely because a beat is past max — SplitScriptOverflow
	// must keep 【10-13秒】 so Pack can move it to the next shot.
	need := fullSpan >= 2 || (anyNearFull && len(beats) >= 2) || sameRange || allStartZero || scriptHasOverlappingBeats(script)
	if !need {
		return script
	}
	limit := len(beats)
	if max == DefaultShotSeconds && limit > 3 {
		limit = 3
	}
	windows := standardBeatWindows(max, limit)
	for i := 0; i < limit; i++ {
		b := beats[i]
		w := windows[i]
		lines[b.idx] = beatRangeRE.ReplaceAllString(b.line, fmt.Sprintf("【%d-%d秒】", w[0], w[1]))
	}
	cursor := max
	for i := limit; i < len(beats); i++ {
		b := beats[i]
		dur := b.end - b.start
		if dur <= 0 {
			dur = 3
		}
		lines[b.idx] = beatRangeRE.ReplaceAllString(b.line, fmt.Sprintf("【%d-%d秒】", cursor, cursor+dur))
		cursor += dur
	}
	return strings.Join(lines, "\n")
}

func standardBeatWindows(max, n int) [][2]int {
	if n <= 0 {
		return nil
	}
	if n == 1 {
		return [][2]int{{0, max}}
	}
	if max == DefaultShotSeconds && n == 3 {
		return [][2]int{{0, 3}, {3, 7}, {7, 10}}
	}
	if max == DefaultShotSeconds && n == 2 {
		return [][2]int{{0, 4}, {4, 10}}
	}
	out := make([][2]int, n)
	for i := 0; i < n; i++ {
		start := max * i / n
		end := max * (i + 1) / n
		if end <= start {
			end = start + 1
		}
		if i == n-1 {
			end = max
		}
		out[i] = [2]int{start, end}
	}
	return out
}

// needsContentRetime is true when a beat carries far more seconds than its
// dialogue and action require (common: 【3-10秒】 with a 5-word line).
func needsContentRetime(script string, max int) bool {
	max = ShotMaxSeconds(max)
	script = strings.TrimSpace(script)
	if script == "" {
		return false
	}
	type beat struct {
		start, end int
		line       string
	}
	var beats []beat
	for _, line := range strings.Split(script, "\n") {
		start, end, ok := beatRange(strings.TrimSpace(line))
		if !ok {
			continue
		}
		beats = append(beats, beat{start: start, end: end, line: line})
	}
	if len(beats) < 2 {
		return false
	}
	cursor := 0
	for _, b := range beats {
		if b.start != cursor {
			return true
		}
		cursor = b.end
	}
	for _, b := range beats {
		dur := b.end - b.start
		if dur <= 0 {
			continue
		}
		need := minBeatDurationForLine(b.line)
		if dur > need+1 {
			return true
		}
	}
	return false
}

// scriptTimelineGap returns the first uncovered interval between timed beats.
// It also treats a first beat that starts after zero as a leading gap.
func scriptTimelineGap(script string) (int, int, bool) {
	cursor := 0
	for _, beat := range scriptBeats(script) {
		start, end, ok := beatRange(beat)
		if !ok {
			continue
		}
		if start > cursor {
			return cursor, start, true
		}
		if end > cursor {
			cursor = end
		}
	}
	return 0, 0, false
}

func beatSpeechRunes(line string) int {
	n := 0
	for _, q := range quotesInScript(line) {
		if !quoteIsEmpty(q) {
			n += speechRunes(q)
		}
	}
	return n
}

func minBeatDurationForLine(line string) int {
	min := 2
	speech := beatSpeechRunes(line)
	if speech > 0 {
		need := int(math.Ceil(float64(speech) / 4.0))
		if need < 2 {
			need = 2
		}
		if need > min {
			min = need
		}
	}
	if lens := extractLensBody(strings.TrimSpace(beatHeaderRE.ReplaceAllString(line, ""))); lens != "" && !lensIsPlotless(lens) {
		min++
		if estimateLensSeconds(lens) > 3 {
			min++
		}
	}
	if min > DefaultShotSeconds {
		min = DefaultShotSeconds
	}
	return min
}

func beatContentWeight(line string) float64 {
	w := float64(minBeatDurationForLine(line))
	if speech := beatSpeechRunes(line); speech > 0 {
		w += float64(speech) * 0.15
	}
	if lens := extractLensBody(strings.TrimSpace(beatHeaderRE.ReplaceAllString(line, ""))); lens != "" {
		w += estimateLensSeconds(lens) * 0.5
	}
	if w < 2 {
		w = 2
	}
	return w
}

func estimateLensSeconds(lens string) float64 {
	lens = strings.TrimSpace(lens)
	if lens == "" || lensIsPlotless(lens) {
		return 1.5
	}
	complexity := 2.0
	for _, kw := range []string{"全景", "缓拉", "缓推", "走", "跑", "踢", "砸", "推镜", "摇镜"} {
		if strings.Contains(lens, kw) {
			complexity++
			break
		}
	}
	if strings.Count(lens, "，") >= 2 || utf8.RuneCountInString(lens) > 36 {
		complexity++
	}
	if complexity > 5 {
		complexity = 5
	}
	return complexity
}

// retimeBeatsByContent reallocates 【起止秒】 by dialogue length and lens
// complexity so a 5-word line is not left on a 7-second beat.
func retimeBeatsByContent(script string, max int) string {
	max = ShotMaxSeconds(max)
	script = strings.TrimSpace(script)
	if script == "" {
		return script
	}
	lines := strings.Split(script, "\n")
	type beatLine struct {
		idx        int
		start, end int
		line       string
	}
	beats := make([]beatLine, 0, 4)
	for i, line := range lines {
		start, end, ok := beatRange(strings.TrimSpace(line))
		if !ok {
			continue
		}
		beats = append(beats, beatLine{idx: i, start: start, end: end, line: line})
	}
	if len(beats) < 2 {
		return script
	}
	mins := make([]int, len(beats))
	weights := make([]float64, len(beats))
	sumMin := 0
	totalW := 0.0
	for i, b := range beats {
		mins[i] = minBeatDurationForLine(b.line)
		weights[i] = beatContentWeight(b.line)
		sumMin += mins[i]
		totalW += weights[i]
	}
	durs := allocateBeatDurations(mins, weights, max, sumMin, totalW)
	cursor := 0
	for i, b := range beats {
		end := cursor + durs[i]
		if i == len(beats)-1 {
			end = max
		}
		lines[b.idx] = beatRangeRE.ReplaceAllString(b.line, fmt.Sprintf("【%d-%d秒】", cursor, end))
		cursor = end
	}
	return strings.Join(lines, "\n")
}

func allocateBeatDurations(mins []int, weights []float64, max, sumMin int, totalW float64) []int {
	n := len(mins)
	if n == 0 {
		return nil
	}
	if n == 1 {
		return []int{max}
	}
	if sumMin > max || totalW <= 0 {
		windows := standardBeatWindows(max, n)
		out := make([]int, n)
		for i, w := range windows {
			out[i] = w[1] - w[0]
		}
		return out
	}
	slack := max - sumMin
	durs := make([]int, n)
	for i := 0; i < n; i++ {
		extra := 0
		if slack > 0 {
			extra = int(math.Round(float64(slack) * weights[i] / totalW))
		}
		durs[i] = mins[i] + extra
	}
	fix := max - sumInts(durs)
	durs[n-1] += fix
	for durs[n-1] < mins[n-1] && n > 1 {
		for j := 0; j < n-1 && durs[n-1] < mins[n-1]; j++ {
			if durs[j] > mins[j]+1 {
				durs[j]--
				durs[n-1]++
			}
		}
		if durs[n-1] < mins[n-1] {
			break
		}
	}
	return durs
}

func sumInts(v []int) int {
	s := 0
	for _, n := range v {
		s += n
	}
	return s
}

func fillShotScriptToDuration(script string, max int) string {
	max = ShotMaxSeconds(max)
	script = strings.TrimSpace(script)
	if script == "" {
		return script
	}
	end := ScriptEndSeconds(script)
	if end <= 0 || end >= max {
		return script
	}
	lines := strings.Split(script, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		start, e, ok := beatRange(lines[i])
		if !ok || e != end {
			continue
		}
		lines[i] = beatRangeRE.ReplaceAllString(lines[i], fmt.Sprintf("【%d-%d秒】", start, max))
		return strings.Join(lines, "\n")
	}
	return script
}

func normalizeBeatTimeline(script string) string {
	lines := strings.Split(script, "\n")
	cursor := 0
	for i, line := range lines {
		start, end, ok := beatRange(line)
		if !ok {
			continue
		}
		dur := end - start
		if dur <= 0 {
			dur = 3
		}
		if start < cursor {
			start = cursor
			end = start + dur
			lines[i] = beatRangeRE.ReplaceAllString(line, fmt.Sprintf("【%d-%d秒】", start, end))
		}
		if end > cursor {
			cursor = end
		}
	}
	return strings.Join(lines, "\n")
}

func appendTimedBeat(script, body string, max int) string {
	script = strings.TrimRight(script, "\n")
	body = strings.TrimSpace(body)
	if body == "" {
		return script
	}
	max = ShotMaxSeconds(max)
	start := scriptTimelineEnd(script)
	if start < 0 {
		start = 0
	}
	if start >= max {
		return mergeOverflowIntoLastBeat(script, extractQuoteFromBeatBody(body), extractSpeakerFromBeatBody(body))
	}
	end := start + 3
	if end > max {
		end = max
	}
	if end <= start {
		return script
	}
	if script == "" {
		return fmt.Sprintf("【%d-%d秒】%s", start, end, body)
	}
	return script + fmt.Sprintf("\n【%d-%d秒】%s", start, end, body)
}

func extractQuoteFromBeatBody(body string) string {
	if m := dialogueRE.FindStringSubmatch(body); len(m) >= 2 {
		return m[1]
	}
	return ""
}

func extractSpeakerFromBeatBody(body string) string {
	if m := speakerSayRE.FindStringSubmatch(body); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func mergeOverflowIntoLastBeat(script, quote, speaker string) string {
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return script
	}
	if scriptAlreadyCoversQuote(script, quote) {
		return script
	}
	lines := strings.Split(strings.TrimRight(script, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.Contains(lines[i], "秒】") {
			continue
		}
		line := lines[i]
		if strings.Contains(line, "「") {
			continue
		}
		line = insertQuote(line, quote)
		if speaker != "" && !strings.Contains(line, speaker+"说") {
			line = ensureSpeakerOnQuote(line, speaker)
		}
		lines[i] = line
		return strings.Join(lines, "\n")
	}
	return script
}

func scriptHasOverlappingBeats(script string) bool {
	lastEnd := -1
	for _, beat := range scriptBeats(script) {
		start, end, ok := beatRange(beat)
		if !ok {
			continue
		}
		if lastEnd >= 0 && start < lastEnd {
			return true
		}
		if end > lastEnd {
			lastEnd = end
		}
	}
	return false
}

func PolishShotForQC(shot ShotContext, assets []AssetItem) []ShotContext {
	packed := PackShotContexts([]ShotContext{shot})
	issues := detectDeterministicQC(packed, assets, "")
	if len(issues) == 0 {
		return packed
	}
	return ApplyQCFixes(packed, assets, issues)
}

// NormalizeShotDialogues splits overlong 「」 into later empty beats (without
// re-copying tails already spoken) then drops cross-beat / cross-shot duplicates.
// Shared by storyboard parse and QC prepare so 重新拆镜 is not left messy until QC.
func NormalizeShotDialogues(shots []ShotContext) []ShotContext {
	return NormalizeShotDialoguesScoped(shots, nil)
}

func NormalizeShotDialoguesScoped(shots []ShotContext, only map[uint]bool) []ShotContext {
	if len(shots) == 0 {
		return shots
	}
	out := cloneShotContexts(shots)
	scopeAll := len(only) == 0
	for i := range out {
		if !scopeAll && !only[out[i].ID] {
			continue
		}
		var next *ShotContext
		if i+1 < len(out) && (scopeAll || only[out[i+1].ID]) {
			next = &out[i+1]
		}
		splitOverlongDialogue(&out[i], next)
	}
	return DedupeDialogueAcrossShots(out)
}

func RestoreShotDialoguesScoped(shots []ShotContext, script string, coveredScripts []string, only map[uint]bool) []ShotContext {
	if len(shots) == 0 || len(only) == 0 {
		return shots
	}
	subset := make([]ShotContext, 0, len(only))
	slot := make([]int, 0, len(only))
	for i, s := range shots {
		if only[s.ID] {
			slot = append(slot, i)
			subset = append(subset, s)
		}
	}
	restored := restoreShotContextDialogueSkippingCovered(subset, script, coveredScripts, false)
	out := cloneShotContexts(shots)
	for j := 0; j < len(restored) && j < len(slot); j++ {
		out[slot[j]] = restored[j]
	}
	return out
}

// NormalizeStoryboardShots runs the same dialogue normalize on storyboard shots.
func NormalizeStoryboardShots(shots []StoryboardShot) []StoryboardShot {
	if len(shots) == 0 {
		return shots
	}
	ctxs := make([]ShotContext, len(shots))
	for i, s := range shots {
		ctxs[i] = ShotContext{Label: s.Label, Script: s.Script, Duration: ShotMaxSeconds(s.Duration)}
	}
	ctxs = NormalizeShotDialogues(ctxs)
	out := make([]StoryboardShot, 0, len(ctxs))
	for i, c := range ctxs {
		item := StoryboardShot{
			Label:    c.Label,
			Duration: ShotMaxSeconds(c.Duration),
			Script:   c.Script,
		}
		if i < len(shots) {
			item.Label = firstNonEmpty(c.Label, shots[i].Label)
			item.CharacterNames = shots[i].CharacterNames
			item.SceneName = shots[i].SceneName
			item.PropNames = shots[i].PropNames
			if item.Duration <= 0 {
				item.Duration = ShotMaxSeconds(shots[i].Duration)
			}
		}
		if strings.TrimSpace(item.Script) == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

// PrepareShotsForQC applies the checks that can be fixed without editorial
// judgement before the supervisor runs. Dialogue is restored first, then
// split to fit its beat; restoring it last would put the full long sentence
// back into a three-second beat and make every re-check report it again.
func PrepareShotsForQC(shots []ShotContext, assets []AssetItem, script string) []ShotContext {
	return PrepareShotsAfterFix(shots, assets, script, nil, nil)
}

func PrepareShotsForQCCovered(shots []ShotContext, assets []AssetItem, script string, coveredScripts []string) []ShotContext {
	return PrepareShotsAfterFix(shots, assets, script, coveredScripts, nil)
}

// PrepareShotsAfterFix runs the post-fix normalize pipeline. User-confirmed fixes
// only touch R2 shots for dialogue restore/split; never re-scan the whole episode.
func PrepareShotsAfterFix(shots []ShotContext, assets []AssetItem, script string, coveredScripts []string, selectedIssues []QCIssue) []ShotContext {
	out := cloneShotContexts(shots)
	// JSON retry output can contain a literal two-character `\n`. Expand it
	// before coverage checks and before any deterministic QC/fix pass.
	for i := range out {
		out[i].Script = normalizeScriptForQC(out[i].Script)
	}
	lockedDialogueComplete := shotContextsCoverAllDialogue(out, script, coveredScripts)
	lockedSchedule := lockedDialogueComplete || hasDeterministicDialogueSchedule(out)
	userSelected := len(selectedIssues) > 0
	dialogueIDs := dialoguePipelineShotIDs(out, selectedIssues)
	// A deterministic schedule protects already complete dialogue, but must not
	// suppress a user-confirmed R2 "source dialogue missing" repair. That was
	// leaving the same issue present on every re-check.
	missingDialogueSelected := false
	for _, issue := range selectedIssues {
		if strings.EqualFold(strings.TrimSpace(issue.Code), "R2") && r2Kind(issue.Message) == "missing" {
			missingDialogueSelected = true
			break
		}
	}
	runDialoguePipeline := (!userSelected || len(dialogueIDs) > 0) && (!lockedSchedule || missingDialogueSelected)

	if runDialoguePipeline {
		if userSelected {
			out = seedMissingDialoguePlaceholders(out, selectedIssues)
			out = RestoreShotDialoguesScoped(out, script, coveredScripts, dialogueIDs)
		} else {
			out = RestoreShotContextDialogueSkippingCovered(out, script, coveredScripts)
		}
	}

	if !userSelected {
		for pass := 0; pass < 4; pass++ {
			issues := detectDeterministicQC(out, assets, script)
			if len(issues) == 0 {
				break
			}
			var next []ShotContext
			if lockedSchedule {
				next = ApplyQCRefFixesPreservingDialogue(out, assets, issues)
			} else {
				next = ApplyQCFixes(out, assets, issues)
			}
			if !shotContextsChanged(out, next) {
				break
			}
			out = next
		}
	}

	if runDialoguePipeline {
		if userSelected {
			// Missing source dialogue is a zero-deletion repair. Keep the complete
			// locked line even when adjacent shots have no empty beat; the normal
			// capacity splitter can otherwise silently discard the tail. Duration
			// pressure may be reported separately, but source text must survive.
			if !missingDialogueSelected {
				out = NormalizeShotDialoguesScoped(out, dialogueIDs)
			}
		} else {
			out = NormalizeShotDialogues(out)
		}
	}
	for i := range out {
		if lockedSchedule {
			out[i].Script = FinalizeShotScriptPreservingDialogue(out[i].Script, out[i].Duration)
		} else {
			out[i].Script = FinalizeShotScript(out[i].Script, out[i].Duration)
		}
		ensureOnscreenSpokenSpeakers(&out[i])
	}
	return out
}

var missingDialogueIssueRE = regexp.MustCompile(`剧本\s+(.+?)\s+的台词未进分镜：「([^」]+)」`)

// seedMissingDialoguePlaceholders gives the scoped restore an exact target when
// a shot contains no quote at all. The clipped issue quote is only a locator;
// RestoreShotDialoguesScoped replaces it with the complete source-script line.
func seedMissingDialoguePlaceholders(shots []ShotContext, issues []QCIssue) []ShotContext {
	out := cloneShotContexts(shots)
	index := make(map[uint]int, len(out))
	for i, shot := range out {
		index[shot.ID] = i
	}
	for _, issue := range issues {
		if !strings.EqualFold(strings.TrimSpace(issue.Code), "R2") || r2Kind(issue.Message) != "missing" {
			continue
		}
		m := missingDialogueIssueRE.FindStringSubmatch(issue.Message)
		if len(m) < 3 {
			continue
		}
		locator := missingDialogueLocatorText(m[2])
		i, ok := index[issue.ShotID]
		for candidate := range out {
			if locator != "" && strings.Contains(out[candidate].Script, locator) {
				i, ok = candidate, true
				break
			}
		}
		if !ok && issue.ShotIndex > 0 && issue.ShotIndex <= len(out) {
			i, ok = issue.ShotIndex-1, true
		}
		if !ok {
			continue
		}
		lines := strings.Split(out[i].Script, "\n")
		lineIndex := len(lines) - 1
		for lineIndex >= 0 && strings.TrimSpace(lines[lineIndex]) == "" {
			lineIndex--
		}
		if lineIndex < 0 {
			lines = []string{"【0-10秒】镜头：中景固定；音效：环境声"}
			lineIndex = 0
		}
		lines[lineIndex] = lockSpokenIntoLine(lines[lineIndex], SpokenLine{Speaker: strings.TrimSpace(m[1]), Text: locator})
		out[i].Script = strings.Join(lines, "\n")
	}
	return out
}

func hasDeterministicDialogueSchedule(shots []ShotContext) bool {
	for _, shot := range shots {
		if strings.Contains(shot.Label, "对白") || strings.Contains(shot.Script, "配乐床、环境声") {
			return true
		}
	}
	return false
}

func shotContextsCoverAllDialogue(shots []ShotContext, script string, coveredScripts []string) bool {
	orig := ExtractSpokenLines(script)
	if len(orig) == 0 {
		return true
	}
	var joined strings.Builder
	for _, prior := range coveredScripts {
		for _, q := range quotesInScript(normalizeScriptForQC(prior)) {
			joined.WriteString(dialogueCoverageKey(q))
		}
	}
	for _, shot := range shots {
		for _, q := range quotesInScript(normalizeScriptForQC(shot.Script)) {
			joined.WriteString(dialogueCoverageKey(q))
		}
	}
	all := joined.String()
	for _, line := range orig {
		key := dialogueCoverageKey(line.Text)
		if key != "" && !strings.Contains(all, key) {
			return false
		}
	}
	return true
}

func shotContextsChanged(before, after []ShotContext) bool {
	if len(before) != len(after) {
		return true
	}
	for i := range before {
		if before[i].ID != after[i].ID || before[i].Script != after[i].Script || !sameShotRefs(before[i].Refs, after[i].Refs) {
			return true
		}
	}
	return false
}

func sameShotRefs(a, b []ShotRefInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || a[i].ResourceID != b[i].ResourceID {
			return false
		}
	}
	return true
}

// FlattenPackedScript puts continuation shots back onto the first script's
// timeline so packEpisodeOverflow can move overflow onto the real next shot.
func FlattenPackedScript(packed []ShotContext) string {
	if len(packed) == 0 {
		return ""
	}
	script := packed[0].Script
	cursor := ShotMaxSeconds(packed[0].Duration)
	for _, extra := range packed[1:] {
		body := strings.TrimSpace(extra.Script)
		if body == "" {
			continue
		}
		script = strings.TrimRight(script, "\n") + "\n" + shiftScriptBeats(body, cursor)
		cursor += ShotMaxSeconds(extra.Duration)
	}
	return strings.TrimSpace(script)
}

func ShotScriptsChanged(before, after []ShotContext) bool {
	if len(before) != len(after) {
		return true
	}
	for i := range before {
		if before[i].ID != after[i].ID || strings.TrimSpace(before[i].Script) != strings.TrimSpace(after[i].Script) {
			return true
		}
	}
	return false
}

func PackShotContexts(shots []ShotContext) []ShotContext {
	if len(shots) == 0 {
		return shots
	}
	out := make([]ShotContext, 0, len(shots)+2)
	pending := ""
	var pendingRefs []ShotRefInfo
	pendingLabel := ""
	for _, shot := range shots {
		script := NormalizeShotTimeline(shot.Script, shot.Duration)
		if pending != "" {
			script = joinOverflowOntoNext(pending, script)
			pending = ""
		}
		keep, overflow := SplitScriptOverflow(script, shot.Duration)
		if _, _, hasGap := scriptTimelineGap(keep); hasGap {
			keep = retimeBeatsByContent(keep, shot.Duration)
		}
		shot.Script = keep
		out = append(out, shot)
		if overflow != "" {
			pending = overflow
			pendingRefs = append([]ShotRefInfo{}, shot.Refs...)
			pendingLabel = strings.TrimSpace(shot.Label)
		}
	}
	for pending != "" {
		keep, overflow := SplitScriptOverflow(pending, DefaultShotSeconds)
		label := pendingLabel
		if label == "" {
			label = "续"
		} else if !strings.Contains(label, "续") {
			label += " · 续"
		}
		out = append(out, ShotContext{
			Label:    label,
			Script:   keep,
			Refs:     pendingRefs,
			Duration: DefaultShotSeconds,
		})
		pending = overflow
	}
	return DedupeDialogueAcrossShots(out)
}

func PackStoryboardShots(shots []StoryboardShot) []StoryboardShot {
	return packStoryboardShots(shots, true)
}

func PackStoryboardShotsPreservingDialogue(shots []StoryboardShot) []StoryboardShot {
	return packStoryboardShots(shots, false)
}

func packStoryboardShots(shots []StoryboardShot, dedupeDialogue bool) []StoryboardShot {
	if len(shots) == 0 {
		return shots
	}
	out := make([]StoryboardShot, 0, len(shots)+2)
	pending := ""
	var pendingChars, pendingProps []string
	pendingScene := ""
	pendingLabel := ""
	for _, shot := range shots {
		script := NormalizeShotTimeline(shot.Script, shot.Duration)
		if !dedupeDialogue {
			script = NormalizeShotTimelinePreservingDialogue(shot.Script, shot.Duration)
		}
		if pending != "" {
			script = joinOverflowOntoNext(pending, script)
			pending = ""
		}
		keep, overflow := SplitScriptOverflow(script, shot.Duration)
		shot.Script = keep
		if shot.Duration != DefaultShotSeconds {
			shot.Duration = DefaultShotSeconds
		}
		out = append(out, shot)
		if overflow != "" {
			pending = overflow
			pendingChars = append([]string{}, shot.CharacterNames...)
			pendingProps = append([]string{}, shot.PropNames...)
			pendingScene = shot.SceneName
			pendingLabel = strings.TrimSpace(shot.Label)
		}
	}
	for pending != "" {
		keep, overflow := SplitScriptOverflow(pending, DefaultShotSeconds)
		label := pendingLabel
		if label == "" {
			label = "续"
		} else if !strings.Contains(label, "续") {
			label += " · 续"
		}
		out = append(out, StoryboardShot{
			Label:          label,
			Duration:       DefaultShotSeconds,
			Script:         keep,
			CharacterNames: pendingChars,
			SceneName:      pendingScene,
			PropNames:      pendingProps,
		})
		pending = overflow
	}
	if dedupeDialogue {
		contexts := make([]ShotContext, len(out))
		for i, s := range out {
			contexts[i] = ShotContext{Label: s.Label, Script: s.Script, Duration: s.Duration}
		}
		contexts = DedupeDialogueAcrossShots(contexts)
		for i := range out {
			if i < len(contexts) {
				out[i].Script = contexts[i].Script
			}
		}
	}
	return out
}

func beatRange(line string) (start, end int, ok bool) {
	m := beatRangeRE.FindStringSubmatch(line)
	if len(m) < 3 {
		return 0, 0, false
	}
	fmt.Sscanf(m[1], "%d", &start)
	fmt.Sscanf(m[2], "%d", &end)
	return start, end, true
}

func remapScriptToZero(script string) string {
	for _, beat := range scriptBeats(script) {
		start, _, ok := beatRange(beat)
		if !ok || start == 0 {
			return script
		}
		return shiftScriptBeats(script, -start)
	}
	return script
}

func lastBeatEnd(script string) int {
	return scriptTimelineEnd(script)
}

func shiftScriptBeats(script string, delta int) string {
	if delta == 0 || strings.TrimSpace(script) == "" {
		return script
	}
	return beatRangeRE.ReplaceAllStringFunc(script, func(raw string) string {
		start, end, ok := beatRange(raw)
		if !ok {
			return raw
		}
		start += delta
		end += delta
		if start < 0 {
			start = 0
		}
		if end < 0 {
			end = 0
		}
		return fmt.Sprintf("【%d-%d秒】", start, end)
	})
}

func joinOverflowOntoNext(overflow, next string) string {
	overflow = strings.TrimSpace(overflow)
	next = strings.TrimSpace(next)
	if overflow == "" {
		return next
	}
	overflow = dropQuotesAlreadyPresent(next, overflow)
	if overflow == "" {
		return next
	}
	if next == "" {
		return overflow
	}
	shift := lastBeatEnd(overflow)
	if shift <= 0 {
		shift = DefaultShotSeconds
	}
	return overflow + "\n" + shiftScriptBeats(next, shift)
}

func dropQuotesAlreadyPresent(keep, overflow string) string {
	overflow = strings.TrimSpace(overflow)
	if overflow == "" {
		return overflow
	}
	kept := quotesInScript(keep)
	if len(kept) == 0 {
		return overflow
	}
	var keepLines []string
	for _, line := range strings.Split(overflow, "\n") {
		q := firstQuoteInLine(line)
		if q != "" && speechRunes(q) >= 6 {
			dup := false
			for _, prev := range kept {
				if quotesSubstantivelyDuplicate(q, prev) {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
		}
		keepLines = append(keepLines, line)
	}
	return strings.TrimSpace(strings.Join(keepLines, "\n"))
}
