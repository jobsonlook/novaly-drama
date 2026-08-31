package doubao

import (
	"encoding/json"
	"fmt"
	"strings"
)

type SamanthaEvent struct {
	EventType int             `json:"event_type"`
	EventData json.RawMessage `json:"event_data"`
}

func ParseSamanthaSSE(raw string) []SamanthaEvent {
	var events []SamanthaEvent
	for _, block := range strings.Split(raw, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var dataStr string
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data:") {
				dataStr = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if dataStr == "" {
			continue
		}
		var ev SamanthaEvent
		if err := json.Unmarshal([]byte(dataStr), &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events
}

func ErrorCodeFromDetail(detail string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(detail), &m); err != nil {
		return ""
	}
	switch v := m["code"].(type) {
	case float64:
		return fmt.Sprintf("%.0f", v)
	case string:
		return v
	default:
		return ""
	}
}
