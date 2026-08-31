package doubao

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ImageResult struct {
	Key    string `json:"key"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
}

type GenerateImageOptions struct {
	Prompt       string
	Ratio        string
	RefImageKey  string
	Timeout      time.Duration
}

type Client struct {
	fetch             func(ctx context.Context, payloadJSON string, timeout time.Duration) (string, error)
	asyncFetch        func(ctx context.Context, payloadJSON string, timeout time.Duration) (string, error)
	upload            func(ctx context.Context, data []byte, filename string) (UploadResult, error)
	getConversationID  func(ctx context.Context) (string, error)
	generateVideoViaUI func(ctx context.Context, opts GenerateVideoOptions) ([]VideoResult, error)
	waitForVideos      func(ctx context.Context, conversationID string, timeout time.Duration) ([]VideoResult, error)
}

func NewClient(
	fetch func(ctx context.Context, payloadJSON string, timeout time.Duration) (string, error),
	asyncFetch func(ctx context.Context, payloadJSON string, timeout time.Duration) (string, error),
	upload func(ctx context.Context, data []byte, filename string) (UploadResult, error),
	getConversationID func(ctx context.Context) (string, error),
	generateVideoViaUI func(ctx context.Context, opts GenerateVideoOptions) ([]VideoResult, error),
	waitForVideos func(ctx context.Context, conversationID string, timeout time.Duration) ([]VideoResult, error),
) *Client {
	return &Client{
		fetch:              fetch,
		asyncFetch:         asyncFetch,
		upload:             upload,
		getConversationID:  getConversationID,
		generateVideoViaUI: generateVideoViaUI,
		waitForVideos:      waitForVideos,
	}
}

func SizeToRatio(size string) string {
	if size == "" {
		return "1:1"
	}
	sizeMap := map[string]string{
		"1024x1024": "1:1",
		"1792x1024": "16:9",
		"1024x1792": "9:16",
		"1024x768":  "4:3",
		"768x1024":  "3:4",
	}
	if ratio, ok := sizeMap[size]; ok {
		return ratio
	}
	if stringsContainsColon(size) {
		return size
	}
	return "1:1"
}

func stringsContainsColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
	}
	return false
}

func (c *Client) GenerateImage(ctx context.Context, opts GenerateImageOptions) ([]ImageResult, error) {
	if opts.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}

	contentData := map[string]any{"text": opts.Prompt}
	if opts.Ratio != "" {
		contentData["ratio"] = opts.Ratio
	}

	message := map[string]any{
		"content":      mustJSON(contentData),
		"content_type": 2009,
		"attachments":  []any{},
		"references":   []any{},
		"skill": map[string]any{
			"skill_type":             3,
			"skill_type_no_default":  3,
			"skill_id":               "3",
			"skill_id_no_default":    "3",
		},
	}

	if opts.RefImageKey != "" {
		message["attachments"] = []any{
			map[string]any{
				"type":  "image",
				"key":   opts.RefImageKey,
				"extra": map[string]any{"refer_types": "overall"},
			},
		}
	}

	payload := map[string]any{
		"messages": []any{message},
		"completion_option": map[string]any{
			"is_regen":                 false,
			"with_suggest":             true,
			"need_create_conversation": true,
			"launch_stage":             1,
			"is_replace":               false,
			"is_delete":                false,
			"is_ai_playground":         false,
			"memory_type":              2,
			"message_from":             0,
			"use_deep_think":           false,
			"use_auto_cot":             false,
			"resend_for_regen":         false,
			"enable_commerce_credit":   false,
			"action_bar_skill_id":      3,
		},
		"evaluate_option":         map[string]any{"web_ab_params": ""},
		"local_conversation_id":   uuid.New().String(),
		"local_message_id":        uuid.New().String(),
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	raw, err := c.fetch(ctx, string(payloadJSON), timeout)
	if err != nil {
		return nil, err
	}

	return parseImageResponse(raw)
}

func parseImageResponse(raw string) ([]ImageResult, error) {
	var images []ImageResult

	for _, ev := range ParseSamanthaSSE(raw) {
		if ev.EventType == 2005 {
			detail := string(ev.EventData)
			code := ErrorCodeFromDetail(detail)
			if code != "" {
				return nil, fmt.Errorf("generate_image error (code=%s): %s", code, truncate(detail, 500))
			}
			return nil, fmt.Errorf("generate_image error: %s", truncate(detail, 500))
		}
		if ev.EventType != 2001 {
			continue
		}

		var eventData map[string]any
		if err := unmarshalFlexible(ev.EventData, &eventData); err != nil {
			continue
		}

		msgRaw, ok := eventData["message"]
		if !ok {
			continue
		}
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			b, err := json.Marshal(msgRaw)
			if err != nil {
				continue
			}
			if err := json.Unmarshal(b, &msg); err != nil {
				continue
			}
		}

		contentType, _ := msg["content_type"].(float64)
		if int(contentType) != 2010 {
			continue
		}

		contentRaw, ok := msg["content"]
		if !ok {
			continue
		}

		var content map[string]any
		switch v := contentRaw.(type) {
		case string:
			if err := json.Unmarshal([]byte(v), &content); err != nil {
				continue
			}
		case map[string]any:
			content = v
		default:
			b, err := json.Marshal(v)
			if err != nil {
				continue
			}
			if err := json.Unmarshal(b, &content); err != nil {
				continue
			}
		}

		dataItems, _ := content["data"].([]any)
		for _, item := range dataItems {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			ori, _ := m["image_ori"].(map[string]any)
			rawImg, _ := m["image_raw"].(map[string]any)
			thumb, _ := m["image_thumb"].(map[string]any)

			url := firstString(ori["url"], rawImg["url"], thumb["url"])
			if url == "" {
				continue
			}

			images = append(images, ImageResult{
				Key:    asString(m["key"]),
				URL:    url,
				Width:  firstInt(ori["width"], thumb["width"]),
				Height: firstInt(ori["height"], thumb["height"]),
				Format: firstString(ori["format"], thumb["format"]),
			})
		}
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("no images generated")
	}
	return images, nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func firstString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func firstInt(values ...any) int {
	for _, v := range values {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

func unmarshalFlexible(raw json.RawMessage, out any) error {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return json.Unmarshal([]byte(s), out)
	}
	return json.Unmarshal(raw, out)
}
