package cdp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildUnwatermarkInstallScript(t *testing.T) {
	dir := t.TempDir()
	content := filepath.Join(dir, "content")
	if err := os.MkdirAll(content, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"panel.css":      "#doubao-clean-dl-btn{color:red}",
		"inject.js":      "window.__injectMarker = 1;",
		"unwatermark.js": "window.DoubaoUnwatermark = {};",
		"content.js":     "window.__contentMarker = 1;",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(content, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src, err := buildUnwatermarkInstallScript(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"doubao-clean-dl-style",
		"#doubao-clean-dl-btn{color:red}",
		"window.__injectMarker = 1;",
		"window.DoubaoUnwatermark = {};",
		"window.__contentMarker = 1;",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("script missing %q", want)
		}
	}
}
