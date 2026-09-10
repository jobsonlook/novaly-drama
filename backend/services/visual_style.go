package services

import "strings"

// UsesPhotorealPeople reports whether privacy masking intended for live-action
// faces should be applied. Unknown/empty styles keep the safer legacy behavior.
func UsesPhotorealPeople(style string) bool {
	s := strings.ToLower(strings.TrimSpace(style))
	if s == "" {
		return true
	}
	for _, token := range []string{
		"3d", "cg", "动漫", "动画", "二次元", "插画", "卡通", "漫画", "漫剧",
		"黏土", "粘土", "水墨", "手绘", "绘本", "像素", "cel shading", "toon",
	} {
		if strings.Contains(s, token) {
			return false
		}
	}
	for _, token := range []string{"真人", "写实", "实拍", "摄影", "live action", "photoreal"} {
		if strings.Contains(s, token) {
			return true
		}
	}
	return true
}
