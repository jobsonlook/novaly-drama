package chrome

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeBrowser struct {
	starts int
}

func (b *fakeBrowser) Close() {}

func (b *fakeBrowser) Start(context.Context) error {
	b.starts++
	return nil
}

func TestCDPReady(t *testing.T) {
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer okServer.Close()

	if !cdpReady(context.Background(), okServer.URL) {
		t.Fatal("expected test CDP endpoint to be ready")
	}

	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer badServer.Close()
	if cdpReady(context.Background(), badServer.URL) {
		t.Fatal("503 endpoint must not be ready")
	}
}

func TestEnsureStartedUsesExistingCDP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	browser := &fakeBrowser{}
	manager := NewManager(server.URL, 9222, "/path/that/must/not/run", browser)
	if err := manager.EnsureStarted(context.Background()); err != nil {
		t.Fatalf("EnsureStarted: %v", err)
	}
	if browser.starts != 1 {
		t.Fatalf("browser starts = %d, want 1", browser.starts)
	}
}
