package services

import (
	"fmt"
	"strings"
)

// Platform violence / weapon phrasing that Seedance and similar video APIs reject.
// Longer phrases first so "持刀闯入" is not left as "闯入" after a short "持刀" replace.
var platformViolenceReplacements = [][2]string{
	{"持刀闯入", "闯进来"},
	{"持刀行凶", "动手"},
	{"拔刀相向", "对峙"},
	{"持刀威胁", "出言威胁"},
	{"人身威胁", "口头施压"},
	{"持枪威胁", "出言威胁"},
	{"持刀", "冲上来"},
	{"持枪", "冲上来"},
	{"亮刀", "逼近"},
	{"拔刀", "上前"},
	{"动刀", "搅局"},
	{"短刀", "东西"},
	{"砍刀", "东西"},
	{"尖刀", "东西"},
	{"匕首", "东西"},
	{"水果刀", "东西"},
	{"刀锋", "气势"},
	{"刀尖", "眼前"},
	{"手枪", "东西"},
	{"步枪", "东西"},
	{"枪支", "东西"},
	{"开枪", "动手"},
	{"枪击", "冲突"},
	{"枪杀", "算计"},
	{"刺杀", "算计"},
	{"暗杀", "暗中算计"},
	{"行刺", "算计"},
	{"砍人", "动手"},
	{"捅人", "动手"},
	{"踢人", "推开"},
	{"踹人", "推开"},
	{"杀人", "动手"},
	{"杀死", "让你完蛋"},
	{"杀掉", "让你完蛋"},
	{"杀手", "来人"},
	{"杀了你", "让你好看"},
	{"弄死你", "让你吃不了兜着走"},
	{"要你命", "给你好看"},
	{"弄死", "让你难看"},
	{"砍死", "让你完蛋"},
	{"捅死", "让你完蛋"},
	{"尸体", "倒下的人"},
	{"死尸", "倒下的人"},
	{"喷血", "受惊"},
	{"流血", "受伤"},
	{"血腥", "紧张"},
}

func HasPlatformViolence(script string) bool {
	script = strings.TrimSpace(script)
	if script == "" {
		return false
	}
	for _, pair := range platformViolenceReplacements {
		if strings.Contains(script, pair[0]) {
			return true
		}
	}
	return false
}

func SanitizePlatformViolence(script string) string {
	if script == "" {
		return script
	}
	out := script
	for _, pair := range platformViolenceReplacements {
		out = strings.ReplaceAll(out, pair[0], pair[1])
	}
	return out
}

// SanitizePlatformViolencePreserveDialogue rewrites platform-sensitive visual
// directions while keeping spoken lines verbatim. Dialogue fidelity is more
// important than silently changing what a character says; if the provider
// rejects the spoken line, the caller should surface that rejection instead.
// Both storyboard quotes and the braces used by the Seedance prompt are
// protected because BuildVideoPrompt passes through both forms.
func SanitizePlatformViolencePreserveDialogue(script string) string {
	if script == "" {
		return script
	}
	var out strings.Builder
	for len(script) > 0 {
		openAt := -1
		open, close := "", ""
		for _, pair := range [][2]string{{"「", "」"}, {"{", "}"}} {
			if i := strings.Index(script, pair[0]); i >= 0 && (openAt < 0 || i < openAt) {
				openAt, open, close = i, pair[0], pair[1]
			}
		}
		if openAt < 0 {
			out.WriteString(SanitizePlatformViolence(script))
			break
		}
		out.WriteString(SanitizePlatformViolence(script[:openAt]))
		rest := script[openAt+len(open):]
		closeAt := strings.Index(rest, close)
		if closeAt < 0 {
			// An unmatched opener is not reliable dialogue syntax; sanitize it.
			out.WriteString(SanitizePlatformViolence(script[openAt:]))
			break
		}
		out.WriteString(open)
		out.WriteString(rest[:closeAt])
		out.WriteString(close)
		script = rest[closeAt+len(close):]
	}
	return out.String()
}

func isPlatformContentReject(msg string) bool {
	msg = strings.ToLower(strings.TrimSpace(msg))
	if msg == "" {
		return false
	}
	for _, needle := range []string{
		"侵权", "违规", "无法返回该内容", "换个主题", "sensitive", "content_filter", "risk not",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func humanizeVideoError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !isPlatformContentReject(msg) {
		return err
	}
	explain := "平台安全审核拦截，额度通常未扣。请把分镜里的打斗改成对峙或推搡，角色名不要带「杀」，再生成。"
	if strings.Contains(msg, explain) {
		return err
	}
	return fmt.Errorf("%s\n%s", explain, msg)
}
