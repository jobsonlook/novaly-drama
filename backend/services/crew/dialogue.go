package crew

import (
	"strings"
	"unicode"
)

// DialogueCraftRules is the Toonflow storyboard-table dialogue contract:
// 零删改、分类语速、完整发言优先同镜、说话人必标、禁止空引号。
const DialogueCraftRules = `【台词铁律 · 对齐 Toonflow + DramaClaw 纪律】
1. 零删改：剧本里人物说过的话必须逐字进入「」，包括口语、数字、穿越梗。禁止精简、合并、改派说话人、禁止改成古白或同义句。分镜只设计画面，不二次创作台词。
2. 分类语速：普通对白 4.5–5 字/秒；快速斗嘴、逼问、喜剧抢话 5–6；萌宝日常 4–4.5；威胁、情绪爆发、重要揭晓 3.5–4.5；喘息、哭腔、边打边说 3–4。按汉字、数字和字母计数，标点不计，并为停顿与动作留余量。
3. 完整发言优先同镜：同一角色的一次完整发言优先留在同一条 10 秒分镜。单拍装不下时，先按自然语义停顿延续到本镜下一拍并换景别；不要因固定 3 秒/4 秒切点另开分镜。禁止切成 2～4 字碎行或复读已说完的内容。
4. 整镜容量：普通对白建议 40–45 字、上限 50；快速对白建议 45–52、极限 55；萌宝日常上限 45；威胁/爆发/揭晓上限 45；喘息/哭腔/边打边说上限 40。超过对应上限才优先在句号、问号、感叹号、分号或明确转折处拆到下一镜。
5. 单拍容量仍按实际秒数和分类语速计算，自动硬校验只按最快 6 字/秒：3 秒最多 18 字、4 秒最多 24 字。同一角色的同一次发言可以跨本镜连续时间拍；切换说话人时换拍。相邻拍禁止同义反复。
6. 必须标明说话人：写成 阿彪说：「……」。无台词不要写空「」，直接省略。无对白的拍用反应、出门、环境声填满，不要为填满时长再塞台词。
7. 口型对齐：口播「X说：」那一拍的镜头主体必须是 X（近景/特写/过肩看清嘴脸）；禁止台词是 A、画面却只写 B 看 C。听者可入画但不要抢主体。内心独白才允许画外无口型。
8. VO/旁白/内心OS 也按台词计时，原样写入「」，并在镜头里写清是内心/画外音。
9. 剧本可能是 角色名（动作）: "台词" 或 **角色**：台词，拆镜时都要收成 角色说：「原文」。`

func speechRunes(s string) int {
	n := 0
	for _, r := range s {
		if isSpeechRune(r) {
			n++
		}
	}
	return n
}

func isSpeechRune(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func maxSpeechRunes(secs float64) int {
	n := int(secs * 6)
	if n > 24 {
		n = 24
	}
	if n < 8 {
		n = 8
	}
	return n
}

func quoteIsEmpty(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "　", "")
	switch s {
	case "", "无", "无台词", "（无）", "(无)", "—", "–":
		return true
	default:
		return false
	}
}

func quoteKey(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), " ", "")
}

// normalizeQuoteKey strips ellipsis/dash/punct tails so near-copies still collide.
func normalizeQuoteKey(s string) string {
	k := quoteKey(s)
	k = strings.TrimRight(k, ".…·・—–-。，、；：！？!?")
	return k
}

// dialogueCoverageKey is punctuation-insensitive so a screenplay sentence
// split at a full stop/comma across two timed beats still counts as complete.
func dialogueCoverageKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if isSpeechRune(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func dialogueCoveredByScripts(text string, scripts []string) bool {
	key := dialogueCoverageKey(text)
	if key == "" {
		return false
	}
	var joined strings.Builder
	for _, script := range scripts {
		for _, q := range quotesInScript(normalizeScriptForQC(script)) {
			joined.WriteString(dialogueCoverageKey(q))
		}
	}
	return strings.Contains(joined.String(), key)
}

// quotesSubstantivelyDuplicate is true when two 「」 are the same line or one is a
// long fragment of the other (common after duration packing / mid-sentence splits).
func quotesSubstantivelyDuplicate(a, b string) bool {
	ka, kb := normalizeQuoteKey(a), normalizeQuoteKey(b)
	if ka == "" || kb == "" {
		return false
	}
	// Exact copies (e.g. 「少惹姚三刀」twice) — allow from 4 speech runes so short
	// reminders still collapse within a shot.
	if ka == kb {
		return speechRunes(a) >= 4 || speechRunes(b) >= 4
	}
	if speechRunes(a) < 6 || speechRunes(b) < 6 {
		return false
	}
	// Prefer the shorter as needle; require it cover most of itself inside the longer.
	shorter, longer := ka, kb
	if len([]rune(ka)) > len([]rune(kb)) {
		shorter, longer = kb, ka
	}
	if !strings.Contains(longer, shorter) {
		return false
	}
	shortN, longN := speechRunes(shorter), speechRunes(longer)
	// ≥8 字碎片：足够区分，整段包含即算重复。
	if shortN >= 8 {
		return true
	}
	// 6～7 字：只认「拆句后的头/尾」（如「记住—少惹姚三刀」挂在长句末尾又单独成拍），
	// 避免「记住」这类短词误伤中间任意匹配。
	if strings.HasPrefix(longer, shorter) || strings.HasSuffix(longer, shorter) {
		return true
	}
	return shortN*10 >= longN*6
}

func quotesInScript(script string) []string {
	out := make([]string, 0)
	for _, m := range dialogueRE.FindAllStringSubmatch(script, -1) {
		if len(m) < 2 {
			continue
		}
		text := strings.TrimSpace(m[1])
		if quoteIsEmpty(text) {
			continue
		}
		out = append(out, text)
	}
	return out
}

func firstQuoteInLine(line string) string {
	m := dialogueRE.FindStringSubmatch(line)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}
