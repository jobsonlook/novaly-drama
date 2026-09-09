package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestChatOpenAIFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer openai-key" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"openai ok"}}]}`)
	}))
	defer server.Close()
	s := NewArkService("", "")
	got, err := s.Chat(models.AIProvider{BaseURL: server.URL + "/v1", APIKey: "openai-key", APIFormat: "openai"}, chatTestBody())
	if err != nil || got != "openai ok" {
		t.Fatalf("got %q, err %v", got, err)
	}
}

func TestChatClaudeFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "claude-key" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["system"] != "system rule" {
			t.Errorf("system = %#v", body["system"])
		}
		if _, ok := body["parts"]; ok {
			t.Error("unexpected top-level parts")
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"claude ok"}]}`)
	}))
	defer server.Close()
	s := NewArkService("", "")
	got, err := s.Chat(models.AIProvider{BaseURL: server.URL + "/v1", APIKey: "claude-key", APIFormat: "claude"}, chatTestBody())
	if err != nil || got != "claude ok" {
		t.Fatalf("got %q, err %v", got, err)
	}
}

func TestChatGeminiFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1beta/models/gemini-2.5-pro:generateContent") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "gemini-key" {
			t.Errorf("x-goog-api-key = %q", r.Header.Get("x-goog-api-key"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["systemInstruction"]; !ok {
			t.Error("missing systemInstruction")
		}
		if _, ok := body["generationConfig"]; !ok {
			t.Error("missing generationConfig")
		}
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"gemini ok"}]}}]}`)
	}))
	defer server.Close()
	s := NewArkService("", "")
	got, err := s.Chat(models.AIProvider{BaseURL: server.URL + "/v1beta", APIKey: "gemini-key", APIFormat: "gemini"}, chatTestBody())
	if err != nil || got != "gemini ok" {
		t.Fatalf("got %q, err %v", got, err)
	}
}

func TestNormalizeTextAPIFormatDefaultsToOpenAI(t *testing.T) {
	if got := NormalizeTextAPIFormat(""); got != TextAPIFormatOpenAI {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeTextAPIFormat("anthropic"); got != TextAPIFormatClaude {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeTextAPIFormat("invalid"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func chatTestBody() map[string]any {
	return map[string]any{
		"model": "gemini-2.5-pro", "max_tokens": 128, "temperature": 0.2,
		"messages": []map[string]string{{"role": "system", "content": "system rule"}, {"role": "user", "content": "hello"}},
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
