package services

import (
	"regexp"
	"sort"
	"strings"
)

// SpatialGridSlots is Toonflow's 3×3 blocking vocabulary
// (left/center/right × front/mid/back).
var SpatialGridSlots = []string{
	"左前", "中前", "右前",
	"左中", "中中", "右中",
	"左后", "中后", "右后",
}

// SpatialBlockingRules is the compact Toonflow blocking language shared by
// storyboard, 站位图, script optimize, QC, and motion-grid prompts.
const SpatialBlockingRules = `【站位 · 对齐 Toonflow 3×3】
画面分「左/中/右」×「前/中/后」九格。前=靠近镜头，后=远离镜头（也可表高低：跪者中前、站者中后）。格子只能写：左前、中前、右前、左中、中中、右中、左后、中后、右后。
朝向只能写：正面、3/4正面朝左、3/4正面朝右、面朝左、面朝右、正侧面朝左、正侧面朝右、3/4背面朝左、3/4背面朝右、背面；可加微低头/微仰头。
多人写法：人名(格子)朝向。例：韩铮(左前)3/4正面朝右；阿彪(右中)3/4正面朝左。群演写后排格子（左后/中后/右后），不要每人一张脸。
同场锁轴：同一人左右位置锁死；对话左侧的人面朝右、右侧的人面朝左。换位/转身必须写衔接动作，禁止凭空跳轴。换场才可重置站位。
坐站要写清：围桌喝酒默认坐着；只有起身/站立/走近才写站。禁止把「站位」理解成全员站立。
【何时写 / 何时不写】
- 只要画面有人物，不论单人或多人，每个【秒】行中人物首次出现时都必须写九格站位+朝向；单人例：裴长河(右中)3/4正面朝左。后续拍沿用上一拍，没写走位/转身不得换格子或朝向。
- 只有空镜、纯物件、纯手部或脸部极特写可以不写格子；人物极特写必须明确写「承接上一拍人物位置不变」，禁止借特写让人物左右漂移，也不要编造第二个人。
- 参考图里已有「站位示意图/站位图」时：左右前后、人数、坐站以该图为准；文案九格只用来复述图上的站位，与图冲突时改文案对齐图，不要按文案改图。`

// SpatialBlockingScriptHint is the one-line example fused into Novaly's timing beats.
const SpatialBlockingScriptHint = `每个有人的【秒】行在人物首次出现时写清 人名(格子)朝向，例如单人「裴长河(右中)3/4正面朝左」，双人「韩铮(左前)3/4正面朝右对峙，阿彪(右中)3/4正面朝左」。连续拍没写走位就沿用原格子。只有空镜、纯物件、纯手部或脸部极特写可省略格子；人物极特写须写「承接上一拍人物位置不变」。有站位参考图时文案格子对齐该图。`

// MaxNamedCharacterRefs is the per-shot named-cast cap when splitting storyboard
// scripts. Reference picking and video generation do not use this cap.
const MaxNamedCharacterRefs = 5

// VideoSpatialBlockingConstraint hangs on multi-subject or 站位图 shots so the
// video model follows the 3×3 grid in the script, not only a schematic image.
const VideoSpatialBlockingConstraint = "【站位网格】文案里的「左前/中中/右后」是 3×3 站位（左/中/右 × 前/中/后）。没有站位示意图时，必须按文案格子出画：谁在哪一格、面朝哪边。同场禁止无走位左右换位。"

// VideoSpatialBlockingFromMapConstraint hangs when a 站位图 is attached:
// blocking follows the reference image first.
const VideoSpatialBlockingFromMapConstraint = "【站位网格】有站位示意图时，人物位置、朝向、前后景与人数必须严格按站位参考图出画，与文案九格冲突时以站位图为准。没有站位示意图时，才按文案「左前/中中/右后」九格。同场禁止无走位左右换位。"

var (
	spatialSlotRE = regexp.MustCompile(`左前|中前|右前|左中|中中|右中|左后|中后|右后|画面左侧|画面右侧|画面左|画面右`)
	beatHeaderRE  = regexp.MustCompile(`【\d+\s*[-–—~到至]\s*\d+\s*秒】`)
	crowdNameRE   = regexp.MustCompile(`杀手|路人|群演|群众|宾客|保镖|手下`)
	gridNameRE    = regexp.MustCompile(`([\p{Han}]{2,12})[（(](?:左前|中前|右前|左中|中中|右中|左后|中后|右后)[）)]`)
	speakerNameRE = regexp.MustCompile(`([\p{Han}]{2,12})\s*说[：:「]`)
)

// ScriptHasSpatialSlot reports whether a shot script already uses the 3×3
// grid (or an equivalent 画面左/右 cue).
func ScriptHasSpatialSlot(script string) bool {
	return spatialSlotRE.MatchString(script)
}

// CharacterFocus describes why a named person should keep a face reference
// when a shot has more than MaxNamedCharacterRefs identities.
type CharacterFocus struct {
	Speaker   bool
	FirstBeat bool
	FocusGrid bool // 前/中 row of the 3×3
	BackGrid  bool // 后 row: extras
	Crowd     bool
	Mentions  int
}

// AnalyzeCharacterFocus scores how "focus" a name is in a timed shot script.
func AnalyzeCharacterFocus(name, script string) CharacterFocus {
	name = strings.TrimSpace(name)
	out := CharacterFocus{}
	if name == "" || script == "" {
		return out
	}
	out.Crowd = crowdNameRE.MatchString(name)
	out.Mentions = strings.Count(script, name)
	out.Speaker = strings.Contains(script, name+"说") || strings.Contains(script, name+" 说")
	first := firstBeatBody(script)
	out.FirstBeat = first != "" && strings.Contains(first, name)
	slot := characterGridSlot(name, script)
	if slot != "" {
		if strings.HasSuffix(slot, "后") {
			out.BackGrid = true
		} else {
			out.FocusGrid = true
		}
	}
	return out
}

// KeepCharacterFocus picks up to max identities. Opening-beat front/mid grid
// people are locked first so a two-shot like 韩铮(左中)/阿彪(右中) is not
// dropped in favor of later speakers.
func KeepCharacterFocus(items []CharacterFocus, max int) []bool {
	n := len(items)
	keep := make([]bool, n)
	if max <= 0 || n == 0 {
		return keep
	}
	taken := 0
	pick := func(ok func(CharacterFocus) bool) {
		for i, it := range items {
			if taken >= max || keep[i] || !ok(it) {
				continue
			}
			keep[i] = true
			taken++
		}
	}
	pick(func(it CharacterFocus) bool { return !it.Crowd && it.FirstBeat && it.FocusGrid })
	pick(func(it CharacterFocus) bool { return !it.Crowd && it.Speaker })
	pick(func(it CharacterFocus) bool { return !it.Crowd && it.FirstBeat })
	pick(func(it CharacterFocus) bool { return !it.Crowd && it.FocusGrid })
	pick(func(it CharacterFocus) bool { return !it.Crowd })
	return keep
}

// CharacterLooksLikeCrowd reports extras that should not take a face-sheet slot.
func CharacterLooksLikeCrowd(name string) bool {
	return crowdNameRE.MatchString(name)
}

func firstBeatBody(script string) string {
	locs := beatHeaderRE.FindAllStringIndex(script, -1)
	if len(locs) == 0 {
		return script
	}
	start := locs[0][1]
	end := len(script)
	if len(locs) > 1 {
		end = locs[1][0]
	}
	if start >= end {
		return ""
	}
	return script[start:end]
}

func characterGridSlot(name, script string) string {
	if name == "" {
		return ""
	}
	for _, slot := range SpatialGridSlots {
		for _, pat := range []string{
			name + "(" + slot + ")",
			name + "（" + slot + "）",
			name + " (" + slot + ")",
		} {
			if strings.Contains(script, pat) {
				return slot
			}
		}
	}
	return ""
}

// CountCharacterMentions counts how often each named character appears as 人名(九格) or 人名说.
func CountCharacterMentions(script string) map[string]int {
	counts := map[string]int{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || CharacterLooksLikeCrowd(name) {
			return
		}
		counts[name]++
	}
	for _, m := range gridNameRE.FindAllStringSubmatch(script, -1) {
		if len(m) > 1 {
			add(m[1])
		}
	}
	for _, m := range speakerNameRE.FindAllStringSubmatch(script, -1) {
		if len(m) > 1 {
			add(m[1])
		}
	}
	return counts
}

// RecurringCharacterNames returns names mentioned at least minMentions times.
func RecurringCharacterNames(script string, minMentions int) []string {
	if minMentions < 1 {
		minMentions = 1
	}
	counts := CountCharacterMentions(script)
	out := make([]string, 0, len(counts))
	for name, n := range counts {
		if n >= minMentions {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// MentionedCharacterNames extracts names written as 人名(左前) or 人名说.
func MentionedCharacterNames(script string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || CharacterLooksLikeCrowd(name) || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, m := range gridNameRE.FindAllStringSubmatch(script, -1) {
		if len(m) > 1 {
			add(m[1])
		}
	}
	for _, m := range speakerNameRE.FindAllStringSubmatch(script, -1) {
		if len(m) > 1 {
			add(m[1])
		}
	}
	return out
}

func candidateNameMatchesMention(candName, mention string) bool {
	candName = strings.TrimSpace(candName)
	mention = strings.TrimSpace(mention)
	if candName == "" || mention == "" {
		return false
	}
	if candName == mention {
		return true
	}
	if strings.HasPrefix(candName, mention+"·") || strings.HasPrefix(candName, mention+" ") {
		return true
	}
	return false
}

// EnsureMentionedCharacterPicks adds character candidates named in the script
// (九格站位 / 说) when the model skipped them.
func EnsureMentionedCharacterPicks(picks []RefMatchPick, candidates []RefMatchCandidate, script string) []RefMatchPick {
	names := MentionedCharacterNames(script)
	if len(names) == 0 {
		return picks
	}
	haveID := map[uint]bool{}
	for _, p := range picks {
		haveID[p.ID] = true
	}
	for _, c := range candidates {
		if haveID[c.ID] {
			continue
		}
		if c.Type != "character" && c.Type != "role" {
			continue
		}
		if CharacterLooksLikeCrowd(c.Name) {
			continue
		}
		hit := false
		for _, n := range names {
			if candidateNameMatchesMention(c.Name, n) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		picks = append(picks, RefMatchPick{ID: c.ID, Label: c.Name})
		haveID[c.ID] = true
	}
	return picks
}
