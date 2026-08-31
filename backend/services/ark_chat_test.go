package services

import (
	"encoding/json"
	"testing"

	"novaly/backend/models"
)

func TestPrepareChatBodyDisablesDeepSeekThinking(t *testing.T) {
	body := map[string]any{"model": "deepseek-v4-pro", "max_tokens": 8}
	got := prepareChatBody(models.AIProvider{Slug: DeepSeekSlug}, body)
	thinking, ok := got["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("missing thinking: %#v", got["thinking"])
	}
	if thinking["type"] != "disabled" {
		t.Fatalf("thinking type = %v", thinking["type"])
	}
	if _, ok := body["thinking"]; ok {
		t.Fatal("original body should not be mutated")
	}
}

func TestPrepareChatBodyLeavesOtherProviders(t *testing.T) {
	body := map[string]any{"model": "doubao-seed-2-0-pro-260215"}
	got := prepareChatBody(models.AIProvider{Slug: "volcengine-ark"}, body)
	if _, ok := got["thinking"]; ok {
		t.Fatal("non-DeepSeek payload should stay unchanged")
	}
}

func TestDecodeChatContent(t *testing.T) {
	if got := decodeChatContent(json.RawMessage(`"hello"`)); got != "hello" {
		t.Fatalf("string content: %q", got)
	}
	if got := decodeChatContent(json.RawMessage(`[{"type":"text","text":"foo"},{"text":"bar"}]`)); got != "foobar" {
		t.Fatalf("parts content: %q", got)
	}
	if got := decodeChatContent(json.RawMessage(`null`)); got != "" {
		t.Fatalf("null content: %q", got)
	}
}
