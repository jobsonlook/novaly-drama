package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"novaly/backend/models"
)

const (
	TextAPIFormatOpenAI = "openai"
	TextAPIFormatClaude = "claude"
	TextAPIFormatGemini = "gemini"
)

func NormalizeTextAPIFormat(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", TextAPIFormatOpenAI, "openai-compatible", "openai_compatible":
		return TextAPIFormatOpenAI
	case TextAPIFormatClaude, "anthropic":
		return TextAPIFormatClaude
	case TextAPIFormatGemini, "google":
		return TextAPIFormatGemini
	default:
		return ""
	}
}

func (s *ArkService) chatByAPIFormat(provider models.AIProvider, body map[string]any) (string, error) {
	format := NormalizeTextAPIFormat(provider.APIFormat)
	if format == "" {
		return "", fmt.Errorf("不支持的文本 API 格式：%s", provider.APIFormat)
	}
	switch format {
	case TextAPIFormatClaude:
		return s.chatClaude(provider, body)
	case TextAPIFormatGemini:
		return s.chatGemini(provider, body)
	default:
		return s.chatOpenAI(provider, body)
	}
}

func (s *ArkService) chatOpenAI(provider models.AIProvider, body map[string]any) (string, error) {
	payload := prepareChatBody(provider, body)
	endpoint := textEndpoint(provider.BaseURL, TextAPIFormatOpenAI, stringValue(body["model"]))
	raw, err := s.postText(provider, endpoint, payload, TextAPIFormatOpenAI)
	if err != nil {
		return "", err
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	if len(decoded.Choices) == 0 {
		return "", errors.New("响应没有 choices")
	}
	return requireText(decodeChatContent(decoded.Choices[0].Message.Content))
}

func (s *ArkService) chatClaude(provider models.AIProvider, body map[string]any) (string, error) {
	payload := claudeChatBody(body)
	endpoint := textEndpoint(provider.BaseURL, TextAPIFormatClaude, stringValue(body["model"]))
	raw, err := s.postText(provider, endpoint, payload, TextAPIFormatClaude)
	if err != nil {
		return "", err
	}
	var decoded struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	var out strings.Builder
	for _, part := range decoded.Content {
		if part.Type == "" || part.Type == "text" {
			out.WriteString(part.Text)
		}
	}
	return requireText(out.String())
}

func (s *ArkService) chatGemini(provider models.AIProvider, body map[string]any) (string, error) {
	payload := geminiChatBody(body)
	endpoint := textEndpoint(provider.BaseURL, TextAPIFormatGemini, stringValue(body["model"]))
	raw, err := s.postText(provider, endpoint, payload, TextAPIFormatGemini)
	if err != nil {
		return "", err
	}
	var decoded struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	if len(decoded.Candidates) == 0 {
		return "", errors.New("响应没有 candidates")
	}
	var out strings.Builder
	for _, part := range decoded.Candidates[0].Content.Parts {
		out.WriteString(part.Text)
	}
	return requireText(out.String())
}

func (s *ArkService) postText(provider models.AIProvider, endpoint string, body map[string]any, format string) ([]byte, error) {
	logAPIRequest(provider, endpoint, body)
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	switch format {
	case TextAPIFormatClaude:
		if provider.APIKey != "" {
			req.Header.Set("x-api-key", provider.APIKey)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	case TextAPIFormatGemini:
		if provider.APIKey != "" {
			req.Header.Set("x-goog-api-key", provider.APIKey)
		}
	default:
		s.setAuth(req, provider)
	}
	resp, err := s.httpClient(provider).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	logAPIResponse(provider, http.MethodPost, endpoint, resp.StatusCode, raw)
	if resp.StatusCode >= 300 {
		return nil, parseArkError(resp.StatusCode, raw)
	}
	return raw, nil
}

func textEndpoint(base, format, model string) string {
	base = strings.TrimSuffix(strings.TrimSpace(base), "/")
	switch format {
	case TextAPIFormatClaude:
		if strings.HasSuffix(base, "/messages") {
			return base
		}
		return base + "/messages"
	case TextAPIFormatGemini:
		if strings.Contains(base, ":generateContent") {
			return base
		}
		return base + "/models/" + url.PathEscape(model) + ":generateContent"
	default:
		if strings.HasSuffix(base, "/chat/completions") {
			return base
		}
		return base + "/chat/completions"
	}
}

func claudeChatBody(body map[string]any) map[string]any {
	system, messages := splitMessages(body["messages"], "assistant")
	formatted := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		formatted = append(formatted, map[string]any{"role": message.Role, "content": []map[string]string{{"type": "text", "text": message.Text}}})
	}
	out := map[string]any{"model": body["model"], "max_tokens": intValue(body["max_tokens"], 4096), "messages": formatted}
	if system != "" {
		out["system"] = system
	}
	copyIfPresent(out, body, "temperature", "top_p", "stop_sequences")
	return out
}

func geminiChatBody(body map[string]any) map[string]any {
	system, messages := splitMessages(body["messages"], "model")
	formatted := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		formatted = append(formatted, map[string]any{"role": message.Role, "parts": []map[string]string{{"text": message.Text}}})
	}
	out := map[string]any{"contents": formatted}
	if system != "" {
		out["systemInstruction"] = map[string]any{"parts": []map[string]string{{"text": system}}}
	}
	config := map[string]any{"maxOutputTokens": intValue(body["max_tokens"], 4096)}
	if value, ok := body["temperature"]; ok {
		config["temperature"] = value
	}
	if value, ok := body["top_p"]; ok {
		config["topP"] = value
	}
	out["generationConfig"] = config
	return out
}

type textMessage struct{ Role, Text string }

func splitMessages(raw any, assistantRole string) (string, []textMessage) {
	b, _ := json.Marshal(raw)
	var input []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(b, &input)
	var systems []string
	messages := make([]textMessage, 0, len(input))
	for _, msg := range input {
		text := strings.TrimSpace(decodeChatContent(msg.Content))
		if text == "" {
			continue
		}
		if msg.Role == "system" {
			systems = append(systems, text)
			continue
		}
		role := "user"
		if msg.Role == "assistant" {
			role = assistantRole
		}
		if len(messages) > 0 && messages[len(messages)-1].Role == role {
			messages[len(messages)-1].Text += "\n\n" + text
			continue
		}
		messages = append(messages, textMessage{Role: role, Text: text})
	}
	return strings.Join(systems, "\n\n"), messages
}

func requireText(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("响应内容为空")
	}
	return value, nil
}

func stringValue(value any) string { valueString, _ := value.(string); return valueString }
func intValue(value any, fallback int) any {
	if value == nil {
		return fallback
	}
	return value
}
func copyIfPresent(dst, src map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}
