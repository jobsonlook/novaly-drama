package crew

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

const (
	StoryboardPaceFine   = "fine"
	StoryboardPacePacked = "packed"
	finePackSeconds      = 3.5
	packedPackSeconds    = 10.0
)

var (
	sceneHeadRE  = regexp.MustCompile(`(?m)^\s*(?:\*{0,2})\s*(?:\d+\s*[-–—]\s*\d+|第\s*\d+\s*场|场\s*\d+)`)
	actionLineRE = regexp.MustCompile(`(?m)^\s*△`)
	spokenLineRE = regexp.MustCompile(`(?m)^\s*\*\*[^*]{1,20}\*\*\s*[：:]`)
	flashbackRE  = regexp.MustCompile(`闪回|回忆杀|倒叙`)
)

func NormalizeStoryboardPace(pace string) string {
	if strings.EqualFold(strings.TrimSpace(pace), StoryboardPacePacked) {
		return StoryboardPacePacked
	}
	return StoryboardPaceFine
}

func storyboardPackSeconds(pace string) float64 {
	if NormalizeStoryboardPace(pace) == StoryboardPacePacked {
		return packedPackSeconds
	}
	return finePackSeconds
}

// EstimateStoryboardCount uses the fine (ep.1-style) packing default.
func EstimateStoryboardCount(script string) int {
	return EstimateStoryboardCountForPace(script, StoryboardPaceFine)
}

// EstimateStoryboardCountForPace is a deterministic target:
// fine ≈ one shot per spoken line / action beat (~3.5s of story);
// packed ≈ one 10s shot per ~10s of dialogue+action.
func EstimateStoryboardCountForPace(script, pace string) int {
	script = strings.TrimSpace(script)
	if script == "" {
		return 2
	}
	pace = NormalizeStoryboardPace(pace)
	pack := storyboardPackSeconds(pace)
	scenes := splitScriptScenes(script)
	total := 0
	for _, scene := range scenes {
		total += estimateSceneShots(scene, pack, pace == StoryboardPaceFine)
	}
	if total < 2 {
		return 2
	}
	maxTotal := 24
	if pace == StoryboardPaceFine {
		maxTotal = 36
	}
	if total > maxTotal {
		return maxTotal
	}
	return total
}

func StoryboardCountRange(target int) (lo, hi int) {
	return StoryboardCountRangeForPace(target, StoryboardPaceFine)
}

func StoryboardCountRangeForPace(target int, pace string) (lo, hi int) {
	if target < 2 {
		target = 2
	}
	slack := 1
	if NormalizeStoryboardPace(pace) == StoryboardPaceFine {
		slack = 2
	}
	lo, hi = target-slack, target+slack
	if lo < 2 {
		lo = 2
	}
	if hi < lo {
		hi = lo
	}
	return lo, hi
}

func splitScriptScenes(script string) []string {
	idxs := sceneHeadRE.FindAllStringIndex(script, -1)
	if len(idxs) == 0 {
		return []string{script}
	}
	starts := make([]int, 0, len(idxs)+1)
	if idxs[0][0] > 0 {
		head := strings.TrimSpace(script[:idxs[0][0]])
		if head != "" {
			starts = append(starts, 0)
		}
	}
	for _, idx := range idxs {
		starts = append(starts, idx[0])
	}
	out := make([]string, 0, len(starts))
	for i, start := range starts {
		end := len(script)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		chunk := strings.TrimSpace(script[start:end])
		if chunk != "" {
			out = append(out, chunk)
		}
	}
	if len(out) == 0 {
		return []string{script}
	}
	return out
}

func estimateSceneShots(scene string, pack float64, fine bool) int {
	dSecs := float64(dialogueRunes(scene)) / 4.0
	actions := len(actionLineRE.FindAllString(scene, -1))
	spoken := 0
	for _, line := range strings.Split(scene, "\n") {
		if spokenLineRE.MatchString(line) {
			spoken++
		}
	}
	aSecs := float64(actions) * 2.0
	if actions == 0 && dSecs == 0 {
		aSecs = float64(visibleRunes(scene)) / 28.0
	}
	secs := dSecs + aSecs
	if flashbackRE.MatchString(scene) {
		secs += 8
	}
	if shirtlessRE.MatchString(scene) && puttingOnRE.MatchString(scene) {
		secs += 8
	}
	minSecs := pack
	if !fine {
		minSecs = packedPackSeconds
	}
	if secs < minSecs {
		secs = minSecs
	}
	n := int(math.Ceil(secs / pack))
	if fine {
		beats := spoken + actions
		if flashbackRE.MatchString(scene) {
			beats++
		}
		if beats > n {
			n = beats
		}
	}
	if n < 1 {
		return 1
	}
	maxScene := 8
	if fine {
		maxScene = 16
	}
	if n > maxScene {
		return maxScene
	}
	return n
}

func dialogueRunes(script string) int {
	n := 0
	for _, m := range dialogueRE.FindAllStringSubmatch(script, -1) {
		if len(m) > 1 {
			n += len([]rune(m[1]))
		}
	}
	for _, line := range strings.Split(script, "\n") {
		if !spokenLineRE.MatchString(line) {
			continue
		}
		_, rest, ok := strings.Cut(line, "：")
		if !ok {
			_, rest, ok = strings.Cut(line, ":")
		}
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if strings.Contains(rest, "「") {
			continue
		}
		n += len([]rune(stripStageDirection(rest)))
	}
	return n
}

func stripStageDirection(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "（") {
		if i := strings.Index(s, "）"); i >= 0 && i+len("）") < len(s) {
			s = strings.TrimSpace(s[i+len("）"):])
		}
	}
	if strings.HasPrefix(s, "(") {
		if i := strings.Index(s, ")"); i >= 0 && i+1 < len(s) {
			s = strings.TrimSpace(s[i+1:])
		}
	}
	return s
}

func visibleRunes(script string) int {
	n := 0
	for _, r := range script {
		if unicode.IsSpace(r) {
			continue
		}
		n++
	}
	return n
}
