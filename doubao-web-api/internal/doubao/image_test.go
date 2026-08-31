package doubao

import (
	"testing"
)

func TestSizeToRatio(t *testing.T) {
	tests := map[string]string{
		"":          "1:1",
		"1024x1024": "1:1",
		"1792x1024": "16:9",
		"1024x1792": "9:16",
		"16:9":      "16:9",
		"unknown":   "1:1",
	}
	for size, want := range tests {
		if got := SizeToRatio(size); got != want {
			t.Fatalf("SizeToRatio(%q) = %q, want %q", size, got, want)
		}
	}
}

func TestParseImageResponse(t *testing.T) {
	raw := `data: {"event_type":2001,"event_data":"{\"message\":{\"content_type\":2010,\"content\":\"{\\\"data\\\":[{\\\"key\\\":\\\"img1\\\",\\\"image_ori\\\":{\\\"url\\\":\\\"https://example.com/a.png\\\",\\\"width\\\":1024,\\\"height\\\":1024,\\\"format\\\":\\\"png\\\"}}]}\"}}"}

` + "\n\n" + `data: {"event_type":2003}`

	images, err := parseImageResponse(raw)
	if err != nil {
		t.Fatalf("parseImageResponse: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("got %d images, want 1", len(images))
	}
	if images[0].URL != "https://example.com/a.png" {
		t.Fatalf("url = %q", images[0].URL)
	}
	if images[0].Width != 1024 || images[0].Height != 1024 {
		t.Fatalf("size = %dx%d", images[0].Width, images[0].Height)
	}
}

func TestParseImageResponseError(t *testing.T) {
	raw := `data: {"event_type":2005,"event_data":"{\"code\":710022002,\"message\":\"rate limit\"}"}`
	_, err := parseImageResponse(raw)
	if err == nil {
		t.Fatal("expected error")
	}
}
