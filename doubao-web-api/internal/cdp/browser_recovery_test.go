package cdp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenTabAtURLCreatesDoubaoPage(t *testing.T) {
	t.Helper()

	var gotMethod, gotURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotURL = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(tabInfo{
			ID:                   "new-tab",
			URL:                  chatURL,
			Type:                 "page",
			WebSocketDebuggerURL: "ws://example/new-tab",
		})
	}))
	defer server.Close()

	tab, err := openTabAtURL(context.Background(), server.URL, chatURL)
	if err != nil {
		t.Fatalf("openTabAtURL: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method=%s want PUT", gotMethod)
	}
	if gotURL != chatURL {
		t.Fatalf("url=%q want %q", gotURL, chatURL)
	}
	if tab.ID != "new-tab" || tab.URL != chatURL {
		t.Fatalf("tab=%+v", tab)
	}
}
