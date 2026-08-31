package doubao

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

type VideoResult struct {
	VideoURL string  `json:"video_url"`
	CoverURL string  `json:"cover_url"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Duration float64 `json:"duration"`
}

type MediaFile struct {
	Data     []byte
	Filename string
}

type GenerateVideoOptions struct {
	Prompt           string
	Ratio            string
	RefImageKey      string   // deprecated: use RefImageKeys
	RefImageKeys     []string // multiple reference images for office mode
	RefImageFiles    []MediaFile
	RefAudioKey      string // voice / audio reference uri (or local-audio: id)
	RefAudioData     []byte
	RefAudioFilename string
	Timeout          time.Duration
	Duration         int64  // 视频时长（秒）：5/10/15，0 默认 10；其它值就近映射
	Model            string // fast / mini，默认 fast
	OnETA            func(text string, minutes int)
}

type ExtractedVideoContent struct {
	Prompt       string
	RefImageKeys []string
	RefAudioKey  string
}

func (c *Client) ExtractVideoContent(ctx context.Context, items []*model.CreateContentGenerationContentItem) (ExtractedVideoContent, error) {
	var out ExtractedVideoContent
	var textParts []string
	for _, item := range items {
		if item == nil {
			continue
		}
		switch item.Type {
		case model.ContentGenerationContentItemTypeText:
			if item.Text != nil {
				text := strings.TrimSpace(*item.Text)
				if text != "" {
					textParts = append(textParts, text)
				}
			}
		case model.ContentGenerationContentItemTypeImage:
			if item.ImageURL != nil && strings.TrimSpace(item.ImageURL.URL) != "" {
				key, resolveErr := c.resolveImageRef(ctx, item.ImageURL.URL)
				if resolveErr != nil {
					return ExtractedVideoContent{}, resolveErr
				}
				if key != "" {
					out.RefImageKeys = append(out.RefImageKeys, key)
				}
			}
		case model.ContentGenerationContentItemTypeAudio:
			if item.AudioURL != nil && strings.TrimSpace(item.AudioURL.Url) != "" {
				key, resolveErr := c.resolveMediaRef(ctx, item.AudioURL.Url)
				if resolveErr != nil {
					return ExtractedVideoContent{}, resolveErr
				}
				if key != "" {
					out.RefAudioKey = key
				}
			}
		default:
			// Some clients omit/mistype "type" but still send text — salvage it.
			if item.Text != nil {
				text := strings.TrimSpace(*item.Text)
				if text != "" {
					textParts = append(textParts, text)
				}
			}
		}
	}
	out.Prompt = strings.TrimSpace(strings.Join(textParts, "\n\n"))
	return out, nil
}

type videoPhase1 struct {
	TaskID              string
	AsyncTaskID         string
	PollID              string
	VideoStatus         int
	MessageID           string
	ReplyID             string
	ConversationID      string
	LocalConversationID string
}

func (c *Client) GenerateVideo(ctx context.Context, opts GenerateVideoOptions) ([]VideoResult, error) {
	if opts.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 25 * time.Minute
	}
	opts.Timeout = timeout
	if opts.RefImageKey != "" && len(opts.RefImageKeys) == 0 {
		opts.RefImageKeys = []string{opts.RefImageKey}
	}

	if c.generateVideoViaUI != nil {
		mode := "text-to-video"
		if len(opts.RefImageKeys) > 0 || opts.RefImageKey != "" {
			mode = "img2video"
		}
		if opts.RefAudioKey != "" {
			mode += "+audio"
		}
		log.Printf("generate_video: UI mode (%s)", mode)
		videos, err := c.generateVideoViaUI(ctx, opts)
		if err == nil && len(videos) > 0 {
			return videos, nil
		}
		if err != nil {
			return nil, fmt.Errorf("video UI generation failed: %w (keep Chrome on https://www.doubao.com/chat/ visible)", err)
		}
	}

	return nil, fmt.Errorf("video generation produced no result")
}

func (c *Client) submitVideoAndParsePhase1(ctx context.Context, opts GenerateVideoOptions, timeout time.Duration) ([]VideoResult, videoPhase1, error) {
	convID := c.resolveConversationID(ctx)
	payload, localConversationID := buildVideoSubmitPayload(opts, convID)

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, videoPhase1{}, err
	}

	raw, err := c.fetch(ctx, string(payloadJSON), minDuration(timeout, 120*time.Second))
	if err != nil {
		return nil, videoPhase1{}, err
	}

	videos, err := parseVideoResponse(raw)
	if err != nil {
		return nil, videoPhase1{}, err
	}
	if len(videos) > 0 {
		return videos, videoPhase1{}, nil
	}

	phase1 := c.enrichVideoPhase1(ctx, extractVideoPhase1(raw, localConversationID), raw)
	logVideoPhase1("generate_video phase1", phase1)

	if phase1.PollID == "" {
		if msg := extractTextFromSSE(raw); msg != "" {
			if strings.Contains(msg, "服务过载") || strings.Contains(msg, "重试") {
				return nil, videoPhase1{}, fmt.Errorf("video generation service overloaded, please retry later")
			}
			return nil, videoPhase1{}, fmt.Errorf("video generation failed: %s", truncate(msg, 500))
		}
		return nil, phase1, nil
	}

	if phase1.ConversationID == "" && phase1.VideoStatus == 1 {
		log.Printf("generate_video: no conversation_id in phase1, seeding via chat")
		if seeded, seedErr := c.seedConversation(ctx); seedErr != nil {
			log.Printf("generate_video: seed conversation failed: %v", seedErr)
		} else if seeded != "" {
			payload2, localConv2 := buildVideoSubmitPayload(opts, seeded)
			payloadJSON2, err := json.Marshal(payload2)
			if err == nil {
				raw2, err2 := c.fetch(ctx, string(payloadJSON2), minDuration(timeout, 120*time.Second))
				if err2 == nil {
					if v2, parseErr := parseVideoResponse(raw2); parseErr == nil && len(v2) > 0 {
						return v2, videoPhase1{}, nil
					}
					phase1 = c.enrichVideoPhase1(ctx, extractVideoPhase1(raw2, localConv2), raw2)
					logVideoPhase1("generate_video resubmit phase1", phase1)
				}
			}
		}
	}

	return nil, phase1, nil
}

func buildVideoSubmitPayload(opts GenerateVideoOptions, conversationID string) (map[string]any, string) {
	localConversationID := uuid.New().String()
	localMessageID := uuid.New().String()
	needCreate := conversationID == ""

	contentData := map[string]any{"text": opts.Prompt}
	if opts.Ratio != "" {
		contentData["ratio"] = opts.Ratio
	}

	message := map[string]any{
		"content":      mustJSON(contentData),
		"content_type": 2020,
		"attachments":  []any{},
		"references":   []any{},
		"skill": map[string]any{
			"skill_type":            17,
			"skill_type_no_default": 17,
			"skill_id":              "17",
			"skill_id_no_default":   "17",
		},
	}

	if opts.RefImageKey != "" {
		message["attachments"] = []any{
			map[string]any{
				"type": "image",
				"key":  opts.RefImageKey,
			},
		}
	}

	payload := map[string]any{
		"messages": []any{message},
		"completion_option": map[string]any{
			"is_regen":                 false,
			"with_suggest":             true,
			"need_create_conversation": needCreate,
			"launch_stage":             1,
			"is_replace":               false,
			"is_delete":                false,
			"is_ai_playground":         false,
			"memory_type":              2,
			"message_from":             0,
			"event_id":                 "0",
			"use_deep_think":           false,
			"use_auto_cot":             false,
			"resend_for_regen":         false,
			"enable_commerce_credit":   false,
			"action_bar_skill_id":      17,
		},
		"evaluate_option":       map[string]any{"web_ab_params": ""},
		"local_conversation_id": localConversationID,
		"local_message_id":      localMessageID,
	}
	if conversationID != "" {
		payload["conversation_id"] = conversationID
	} else {
		payload["conversation_id"] = "0"
	}
	return payload, localConversationID
}

func (c *Client) resolveConversationID(ctx context.Context) string {
	if c.getConversationID == nil {
		return ""
	}
	cid, err := c.getConversationID(ctx)
	if err != nil || cid == "" || cid == "0" {
		return ""
	}
	return cid
}

func (c *Client) enrichVideoPhase1(ctx context.Context, phase1 videoPhase1, raw string) videoPhase1 {
	if phase1.AsyncTaskID == "" {
		phase1.AsyncTaskID = extractAsyncTaskID(raw)
	}
	if phase1.TaskID == "" {
		phase1.TaskID = phase1.AsyncTaskID
	}
	if phase1.AsyncTaskID == "" {
		phase1.AsyncTaskID = phase1.TaskID
	}
	if phase1.PollID == "" {
		phase1.PollID = firstNonEmptyString(phase1.AsyncTaskID, phase1.TaskID, phase1.ReplyID, phase1.MessageID)
	}

	if phase1.ConversationID == "" {
		phase1.ConversationID = extractConversationIDFromSSE(raw)
	}
	if phase1.ConversationID == "" {
		phase1.ConversationID = captureConversationIDRegex(raw)
	}
	if phase1.ConversationID == "" {
		phase1.ConversationID = c.waitForConversationID(ctx, 15*time.Second)
	}
	return phase1
}

func (c *Client) waitForConversationID(ctx context.Context, timeout time.Duration) string {
	if c.getConversationID == nil {
		return ""
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cid, err := c.getConversationID(ctx)
		if err == nil && cid != "" && cid != "0" {
			return cid
		}
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(500 * time.Millisecond):
		}
	}
	return ""
}

func (c *Client) seedConversation(ctx context.Context) (string, error) {
	localConversationID := uuid.New().String()
	payload := map[string]any{
		"messages": []any{
			map[string]any{
				"content":      mustJSON(map[string]any{"text": "你好"}),
				"content_type": 2001,
				"attachments":  []any{},
				"references":   []any{},
			},
		},
		"completion_option": map[string]any{
			"is_regen":                 false,
			"with_suggest":             true,
			"need_create_conversation": true,
			"launch_stage":             1,
			"is_replace":               false,
			"is_delete":                false,
			"message_from":             0,
			"event_id":                 "0",
		},
		"conversation_id":       "0",
		"local_conversation_id": localConversationID,
		"local_message_id":      uuid.New().String(),
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	raw, err := c.fetch(ctx, string(payloadJSON), 60*time.Second)
	if err != nil {
		return "", err
	}

	cid := captureConversationIDRegex(raw)
	if cid == "" {
		cid = extractConversationIDFromSSE(raw)
	}
	if cid == "" {
		cid = c.waitForConversationID(ctx, 5*time.Second)
	}
	if cid == "" {
		return "", fmt.Errorf("seed conversation did not return conversation_id")
	}
	return cid, nil
}

func logVideoPhase1(prefix string, phase1 videoPhase1) {
	log.Printf(
		"%s: poll_id=%s async_task=%s task_id=%s conv=%s status=%d",
		prefix,
		truncate(phase1.PollID, 24),
		truncate(phase1.AsyncTaskID, 24),
		truncate(phase1.TaskID, 24),
		truncate(phase1.ConversationID, 16),
		phase1.VideoStatus,
	)
}

var conversationIDRegex = regexp.MustCompile(`"conversation_id"\s*:\s*"([^"]+)"`)

func captureConversationIDRegex(raw string) string {
	for _, match := range conversationIDRegex.FindAllStringSubmatch(raw, -1) {
		if len(match) < 2 {
			continue
		}
		cid := strings.TrimSpace(match[1])
		if cid != "" && cid != "0" {
			return cid
		}
	}
	return ""
}

const videoPollChunkWait = 25 * time.Second

func (c *Client) pollVideoUntilReady(ctx context.Context, phase1 videoPhase1, totalTimeout time.Duration) ([]VideoResult, error) {
	deadline := time.Now().Add(totalTimeout)
	candidates := videoPollCandidates(phase1)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no poll id from phase1")
	}

	var lastErr error
	for _, pollID := range candidates {
		var accumulated strings.Builder
		for time.Now().Before(deadline) {
			wait := videoPollChunkWait
			if remaining := time.Until(deadline); remaining < wait {
				wait = remaining
			}
			if wait <= 0 {
				break
			}

			pollPayload, err := json.Marshal(buildVideoPollBody(pollID, phase1))
			if err != nil {
				return nil, err
			}

			if accumulated.Len() == 0 {
				log.Printf("generate_video poll: task_id=%s conv=%s", truncate(pollID, 24), truncate(phase1.ConversationID, 16))
			}

			chunk, err := c.asyncFetch(ctx, string(pollPayload), wait)
			if chunk != "" {
				if accumulated.Len() > 0 {
					accumulated.WriteString("\n\n")
				}
				accumulated.WriteString(chunk)

				videos, parseErr := parseVideoResponse(accumulated.String())
				if parseErr != nil {
					return nil, parseErr
				}
				if len(videos) > 0 {
					return videos, nil
				}
				if !videoStillProcessing(accumulated.String()) && err == nil {
					break
				}
			}

			if err != nil {
				lastErr = err
				if isRetryableStreamError(err) && time.Until(deadline) > 5*time.Second {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(2 * time.Second):
					}
					continue
				}
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("video poll failed: %s", truncate(lastErr.Error(), 500))
	}
	return nil, fmt.Errorf("no videos generated within timeout")
}

func videoStillProcessing(raw string) bool {
	processing := false
	for _, ev := range ParseSamanthaSSE(raw) {
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
		if int(contentType) != 2021 {
			continue
		}
		content, ok := parseMessageContent(msg["content"])
		if !ok {
			continue
		}
		if status, ok := content["video_status"].(float64); ok && int(status) == 1 {
			processing = true
		} else {
			processing = false
		}
	}
	return processing
}

func isRetryableStreamError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "504") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "gateway time-out") ||
		strings.Contains(msg, "aborted")
}

func parseVideoResponse(raw string) ([]VideoResult, error) {
	var videos []VideoResult

	for _, ev := range ParseSamanthaSSE(raw) {
		if ev.EventType == 2005 {
			detail := string(ev.EventData)
			code := ErrorCodeFromDetail(detail)
			if code != "" {
				return nil, fmt.Errorf("generate_video error (code=%s): %s", code, truncate(detail, 500))
			}
			return nil, fmt.Errorf("generate_video error: %s", truncate(detail, 500))
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
		if int(contentType) != 2021 {
			continue
		}

		content, ok := parseMessageContent(msg["content"])
		if !ok {
			continue
		}

		if status, _ := content["video_status"].(float64); int(status) == 1 {
			continue
		}

		if rootURL := firstString(content["video_url"], content["url"]); rootURL != "" {
			videos = append(videos, VideoResult{
				VideoURL: rootURL,
				CoverURL: asString(content["cover_url"]),
				Width:    firstInt(content["width"]),
				Height:   firstInt(content["height"]),
				Duration: firstFloat(content["duration"]),
			})
			continue
		}

		dataItems, _ := content["data"].([]any)
		if len(dataItems) == 0 {
			dataItems = []any{content}
		}
		for _, item := range dataItems {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			videoURL := firstString(m["video_url"], m["url"])
			if videoURL == "" {
				videoURL = decodeVideoModelURL(m["video_model"])
			}
			if videoURL == "" {
				continue
			}

			coverURL := firstString(m["cover_url"])
			if coverURL == "" {
				if cover, ok := m["cover"].(map[string]any); ok {
					coverURL = firstString(cover["url"])
				}
			}

			videos = append(videos, VideoResult{
				VideoURL: videoURL,
				CoverURL: coverURL,
				Width:    firstInt(m["width"]),
				Height:   firstInt(m["height"]),
				Duration: firstFloat(m["duration"]),
			})
		}
	}

	return videos, nil
}

func extractAsyncTaskID(raw string) string {
	for _, ev := range ParseSamanthaSSE(raw) {
		if ev.EventType != 2001 {
			continue
		}
		var eventData map[string]any
		if err := unmarshalFlexible(ev.EventData, &eventData); err != nil {
			continue
		}
		finReason, ok := eventData["fin_reason"].(map[string]any)
		if !ok {
			continue
		}
		reason, _ := finReason["reason"].(float64)
		if int(reason) != 1 {
			continue
		}
		asyncTask, ok := finReason["async_task"].(map[string]any)
		if !ok {
			continue
		}
		if id := asString(asyncTask["id"]); id != "" {
			return id
		}
		if id := asString(asyncTask["task_id"]); id != "" {
			return id
		}
	}
	for _, ev := range ParseSamanthaSSE(raw) {
		var eventData map[string]any
		if err := unmarshalFlexible(ev.EventData, &eventData); err != nil {
			var wrapper map[string]any
			if err2 := json.Unmarshal([]byte(fmt.Sprintf(`{"event_data":%s}`, string(ev.EventData))), &wrapper); err2 == nil {
				if id := findAsyncTaskIDInObject(wrapper, 0); id != "" {
					return id
				}
			}
			continue
		}
		if id := findAsyncTaskIDInObject(eventData, 0); id != "" {
			return id
		}
	}
	return ""
}

func extractVideoPhase1(raw, localConversationID string) videoPhase1 {
	info := videoPhase1{LocalConversationID: localConversationID}

	for _, ev := range ParseSamanthaSSE(raw) {
		var eventData map[string]any
		if err := unmarshalFlexible(ev.EventData, &eventData); err != nil {
			continue
		}

		if cid := asString(eventData["conversation_id"]); cid != "" && cid != "0" {
			info.ConversationID = cid
		}

		switch ev.EventType {
		case 2002:
			if id := asString(eventData["message_id"]); id != "" {
				info.MessageID = id
			}
		case 2001:
			if id := findAsyncTaskIDInObject(eventData, 0); id != "" {
				info.AsyncTaskID = id
			}
			if id := asString(eventData["message_id"]); id != "" {
				info.MessageID = id
			}
			if id := asString(eventData["reply_id"]); id != "" {
				info.ReplyID = id
			}

			msgRaw, ok := eventData["message"]
			if !ok {
				break
			}
			msg, ok := msgRaw.(map[string]any)
			if !ok {
				b, err := json.Marshal(msgRaw)
				if err != nil {
					break
				}
				if err := json.Unmarshal(b, &msg); err != nil {
					break
				}
			}

			if id := asString(msg["id"]); id != "" && info.TaskID == "" {
				info.TaskID = id
			}

			contentType, _ := msg["content_type"].(float64)
			if int(contentType) != 2021 {
				break
			}

			if id := asString(msg["id"]); id != "" {
				info.TaskID = id
			}

			content, ok := parseMessageContent(msg["content"])
			if !ok {
				break
			}
			if status, ok := content["video_status"].(float64); ok {
				info.VideoStatus = int(status)
			}
		}
	}

	if info.AsyncTaskID != "" {
		info.PollID = info.AsyncTaskID
	} else if info.TaskID != "" {
		info.PollID = info.TaskID
	} else if info.ReplyID != "" {
		info.PollID = info.ReplyID
	} else if info.MessageID != "" {
		info.PollID = info.MessageID
	}

	if info.ConversationID == "" {
		info.ConversationID = extractConversationIDFromSSE(raw)
	}

	return info
}

func extractConversationIDFromSSE(raw string) string {
	for _, block := range strings.Split(raw, "\n\n") {
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var obj map[string]any
			if err := json.Unmarshal([]byte(dataStr), &obj); err != nil {
				continue
			}
			for _, key := range []string{"ack_client_meta", "client_meta"} {
				meta, ok := obj[key].(map[string]any)
				if !ok {
					continue
				}
				if cid := asString(meta["conversation_id"]); cid != "" && cid != "0" {
					return cid
				}
			}
		}
	}

	for _, ev := range ParseSamanthaSSE(raw) {
		var eventData map[string]any
		if err := unmarshalFlexible(ev.EventData, &eventData); err != nil {
			continue
		}
		if cid := asString(eventData["conversation_id"]); cid != "" && cid != "0" {
			return cid
		}
		for _, key := range []string{"ack_client_meta", "client_meta"} {
			meta, ok := eventData[key].(map[string]any)
			if !ok {
				continue
			}
			if cid := asString(meta["conversation_id"]); cid != "" && cid != "0" {
				return cid
			}
		}
	}
	return ""
}

func findAsyncTaskIDInObject(obj any, depth int) string {
	if depth > 10 || obj == nil {
		return ""
	}
	switch v := obj.(type) {
	case map[string]any:
		if asyncTask, ok := v["async_task"].(map[string]any); ok {
			if id := asString(asyncTask["id"]); id != "" {
				return id
			}
			if id := asString(asyncTask["task_id"]); id != "" {
				return id
			}
		}
		if finReason, ok := v["fin_reason"].(map[string]any); ok {
			if id := findAsyncTaskIDInObject(finReason, depth+1); id != "" {
				return id
			}
		}
		for _, key := range []string{"task_id", "async_task_id"} {
			if id := asString(v[key]); id != "" {
				return id
			}
		}
		for _, value := range v {
			if id := findAsyncTaskIDInObject(value, depth+1); id != "" {
				return id
			}
		}
	case []any:
		for _, item := range v {
			if id := findAsyncTaskIDInObject(item, depth+1); id != "" {
				return id
			}
		}
	}
	return ""
}

func videoPollCandidates(phase1 videoPhase1) []string {
	seen := make(map[string]struct{})
	var ordered []string
	add := func(val string) {
		val = strings.TrimSpace(val)
		if val == "" {
			return
		}
		if _, ok := seen[val]; ok {
			return
		}
		seen[val] = struct{}{}
		ordered = append(ordered, val)
	}
	add(phase1.AsyncTaskID)
	add(phase1.PollID)
	add(phase1.TaskID)
	add(phase1.ReplyID)
	add(phase1.MessageID)
	return ordered
}

func buildVideoPollBody(pollID string, phase1 videoPhase1) map[string]any {
	body := map[string]any{
		"task_id":  pollID,
		"event_id": 0,
	}
	if phase1.ConversationID != "" && phase1.ConversationID != "0" {
		body["conversation_id"] = phase1.ConversationID
	}
	if phase1.LocalConversationID != "" {
		body["local_conversation_id"] = phase1.LocalConversationID
	}
	return body
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func extractTextFromSSE(raw string) string {
	var parts []string
	for _, ev := range ParseSamanthaSSE(raw) {
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
		if int(contentType) != 2001 {
			continue
		}
		content, ok := parseMessageContent(msg["content"])
		if !ok {
			continue
		}
		if text := asString(content["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "")
}

func parseMessageContent(contentRaw any) (map[string]any, bool) {
	switch v := contentRaw.(type) {
	case string:
		var content map[string]any
		if err := json.Unmarshal([]byte(v), &content); err != nil {
			return nil, false
		}
		return content, true
	case map[string]any:
		return v, true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var content map[string]any
		if err := json.Unmarshal(b, &content); err != nil {
			return nil, false
		}
		return content, true
	}
}

func decodeVideoModelURL(vmRaw any) string {
	vmStr, ok := vmRaw.(string)
	if !ok || vmStr == "" {
		return ""
	}
	var vm map[string]any
	if err := json.Unmarshal([]byte(vmStr), &vm); err != nil {
		return ""
	}
	vlist, _ := vm["video_list"].(map[string]any)
	for _, vinfoRaw := range vlist {
		vinfo, ok := vinfoRaw.(map[string]any)
		if !ok {
			continue
		}
		mainB64 := asString(vinfo["main_url"])
		if mainB64 == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(mainB64)
		if err != nil {
			continue
		}
		if url := strings.TrimSpace(string(decoded)); url != "" {
			return url
		}
	}
	return ""
}

func firstFloat(values ...any) float64 {
	for _, v := range values {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return 0
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
