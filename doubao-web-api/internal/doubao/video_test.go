package doubao

import (
	"encoding/base64"
	"testing"
)

func TestParseVideoResponse(t *testing.T) {
	raw := `data: {"event_type":2001,"event_data":"{\"message\":{\"content_type\":2021,\"content\":\"{\\\"video_url\\\":\\\"https://example.com/a.mp4\\\",\\\"cover_url\\\":\\\"https://example.com/cover.jpg\\\",\\\"width\\\":1280,\\\"height\\\":720,\\\"duration\\\":5.0}\"}}"}

` + "\n\n" + `data: {"event_type":2003}`

	videos, err := parseVideoResponse(raw)
	if err != nil {
		t.Fatalf("parseVideoResponse: %v", err)
	}
	if len(videos) != 1 {
		t.Fatalf("got %d videos, want 1", len(videos))
	}
	if videos[0].VideoURL != "https://example.com/a.mp4" {
		t.Fatalf("video_url = %q", videos[0].VideoURL)
	}
	if videos[0].CoverURL != "https://example.com/cover.jpg" {
		t.Fatalf("cover_url = %q", videos[0].CoverURL)
	}
}

func TestParseVideoResponseFromVideoModel(t *testing.T) {
	videoURL := "https://example.com/model.mp4"
	encoded := base64.StdEncoding.EncodeToString([]byte(videoURL))
	raw := `data: {"event_type":2001,"event_data":"{\"message\":{\"content_type\":2021,\"content\":\"{\\\"data\\\":[{\\\"video_model\\\":\\\"{\\\\\\\"video_list\\\\\\\":{\\\\\\\"720p\\\\\\\":{\\\\\\\"main_url\\\\\\\":\\\\\\\"` + encoded + `\\\\\\\"}}}\\\"}]}\"}}"}

` + "\n\n" + `data: {"event_type":2003}`

	videos, err := parseVideoResponse(raw)
	if err != nil {
		t.Fatalf("parseVideoResponse: %v", err)
	}
	if len(videos) != 1 {
		t.Fatalf("got %d videos, want 1", len(videos))
	}
	if videos[0].VideoURL != videoURL {
		t.Fatalf("video_url = %q", videos[0].VideoURL)
	}
}

func TestExtractVideoPhase1(t *testing.T) {
	raw := `data: {"event_type":2001,"event_data":"{\"conversation_id\":\"conv-123\",\"message\":{\"id\":\"msg-uuid-456\",\"content_type\":2021,\"content\":\"{\\\"video_status\\\":1}\"}}"}`
	phase1 := extractVideoPhase1(raw, "local-conv-789")
	if phase1.TaskID != "msg-uuid-456" {
		t.Fatalf("TaskID = %q, want msg-uuid-456", phase1.TaskID)
	}
	if phase1.ConversationID != "conv-123" {
		t.Fatalf("ConversationID = %q", phase1.ConversationID)
	}
	if phase1.VideoStatus != 1 {
		t.Fatalf("VideoStatus = %d, want 1", phase1.VideoStatus)
	}
	if phase1.PollID != "msg-uuid-456" {
		t.Fatalf("PollID = %q", phase1.PollID)
	}
}

func TestExtractAsyncTaskID(t *testing.T) {
	raw := `data: {"event_type":2001,"event_data":"{\"fin_reason\":{\"reason\":1,\"async_task\":{\"id\":\"task-abc-123\"}}}"}`
	if got := extractAsyncTaskID(raw); got != "task-abc-123" {
		t.Fatalf("extractAsyncTaskID = %q", got)
	}
}

func TestParseVideoResponseError(t *testing.T) {
	raw := `data: {"event_type":2005,"event_data":"{\"code\":710022002,\"message\":\"rate limit\"}"}`
	_, err := parseVideoResponse(raw)
	if err == nil {
		t.Fatal("expected error")
	}
}
