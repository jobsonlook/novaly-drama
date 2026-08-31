package server

import (
	"path/filepath"
	"testing"

	"github.com/mask/ai/doubao-web-api/internal/account"
)

func TestRecordVideoGenerationUsesWorkerAccount(t *testing.T) {
	dir := t.TempDir()
	store, err := account.Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	active, err := store.Create("active", filepath.Join(dir, "active"), 5)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := store.Create("worker", filepath.Join(dir, "worker"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Select(active.ID); err != nil {
		t.Fatal(err)
	}

	s := &Server{accounts: store}
	s.recordVideoGeneration("task-1", "prompt", "mini", "https://example.test/video.mp4", nil, worker.ID)

	history, err := store.ListGenerations(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history count=%d want 1", len(history))
	}
	if history[0].AccountID != worker.ID || history[0].AccountName != worker.Name {
		t.Fatalf("history account=(%d, %q), want worker=(%d, %q)",
			history[0].AccountID, history[0].AccountName, worker.ID, worker.Name)
	}
}
