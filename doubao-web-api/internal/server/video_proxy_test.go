package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mask/ai/doubao-web-api/internal/config"
)

func TestBuildVideoProxyURL(t *testing.T) {
	s := &Server{cfg: config.Config{Port: "8086"}}
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8086/api/v3/contents/generations/tasks/x", nil)
	got := s.buildVideoProxyURL(r, "https://v3-dy.ixigua.com/foo.mp4")
	if !strings.Contains(got, "/api/v3/videos/proxy?url=") {
		t.Fatalf("expected video proxy path, got %s", got)
	}
	if strings.Contains(got, "/images/proxy") {
		t.Fatalf("videos must not reuse the 60s image proxy: %s", got)
	}
}
