package cdp

import (
	"strings"
	"testing"
)

func TestExtractFallbackAPIs(t *testing.T) {
	raw := `{"fallback_api":"https://vas-lf-x.snssdk.com/video/fplay/1/abc/v0369cg10004d9fhjsa7dldaeu4q39gg?aid=1938&logo_type=video_gen_watermark_dyn&key_seed=x"}`
	apis := ExtractFallbackAPIs(raw)
	if len(apis) != 1 {
		t.Fatalf("want 1 api, got %#v", apis)
	}
	if !strings.Contains(apis[0], "/video/fplay/") {
		t.Fatalf("unexpected api: %s", apis[0])
	}
	if !strings.Contains(apis[0], "v0369cg10004d9fhjsa7dldaeu4q39gg") {
		t.Fatalf("vid missing in api: %s", apis[0])
	}
}

func TestExtractFallbackAPIsEscaped(t *testing.T) {
	raw := `fallback_api\":\"https:\/\/vas-lf-x.snssdk.com\/video\/fplay\/1\/hash\/v0123abcd4567efgh?aid=1938\u0026logo_type=video_gen_watermark_dyn\"`
	apis := ExtractFallbackAPIs(raw)
	if len(apis) != 1 {
		t.Fatalf("want 1 api, got %#v", apis)
	}
	if strings.Contains(apis[0], `\u0026`) || strings.Contains(apis[0], `\/`) {
		t.Fatalf("not decoded: %s", apis[0])
	}
	if !strings.Contains(apis[0], "logo_type=") {
		t.Fatalf("missing query: %s", apis[0])
	}
}

func TestExtractVids(t *testing.T) {
	raw := `{"vid":"v0369cg10004d9fhjsa7dldaeu4q39gg","video_id":"v0otherid12345678"}`
	vids := ExtractVids(raw)
	if len(vids) != 2 {
		t.Fatalf("want 2 vids, got %#v", vids)
	}
}

func TestPreferFallbackAPIForVid(t *testing.T) {
	apis := []string{
		"https://vas-lf-x.snssdk.com/video/fplay/1/a/v0oldxxxxxxxxxxxxxxxx",
		"https://vas-lf-x.snssdk.com/video/fplay/1/b/v0newyyyyyyyyyyyyyyyy",
	}
	got := PreferFallbackAPIForVid(apis, "v0newyyyyyyyyyyyyyyyy")
	if got != apis[1] {
		t.Fatalf("got %s", got)
	}
	got = PreferFallbackAPIForVid(apis, "missing")
	if got != apis[1] {
		t.Fatalf("fallback should be last, got %s", got)
	}
}

func TestIsUnwatermarkedVideoURL(t *testing.T) {
	if !IsUnwatermarkedVideoURL("https://v9-default.douyin.com/x/?lr=unwatermarked&mime_type=video_mp4") {
		t.Fatal("expected unwatermarked")
	}
	if IsUnwatermarkedVideoURL("https://v26-show.douyinvod.com/x/?lr=video_gen_watermark_dyn") {
		t.Fatal("watermarked should be false")
	}
}
