package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mask/ai/doubao-web-api/internal/account"
	"github.com/mask/ai/doubao-web-api/internal/doubao"
)

func (s *Server) historyMediaDir() string {
	dir := filepath.Join("data", "gen-history")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func (s *Server) currentAccountForHistory() (id int64, name string) {
	if s.accounts == nil {
		return 0, ""
	}
	active, err := s.accounts.Active()
	if err != nil {
		return 0, ""
	}
	return active.ID, active.Name
}

// accountForHistory resolves the account that actually executed a pooled job.
// Falling back to the active account preserves the non-pool/legacy behaviour,
// where no worker account ID is available.
func (s *Server) accountForHistory(accountID int64) (id int64, name string) {
	if s.accounts == nil {
		return 0, ""
	}
	if accountID > 0 {
		workerAccount, err := s.accounts.Get(accountID)
		if err == nil {
			return workerAccount.ID, workerAccount.Name
		}
		log.Printf("record generation history: resolve worker account %d: %v", accountID, err)
		return accountID, ""
	}
	return s.currentAccountForHistory()
}

func (s *Server) recordImageGeneration(r *http.Request, prompt, modelName string, images []doubao.ImageResult) {
	if s.accounts == nil || len(images) == 0 {
		return
	}
	urls := make([]string, 0, len(images))
	for _, img := range images {
		if img.URL == "" {
			continue
		}
		// Prefer durable local copies so admin history survives CDN expiry.
		if local := s.persistHistoryImage(r.Context(), img.URL, "out"); local != "" {
			urls = append(urls, local)
			continue
		}
		urls = append(urls, s.buildProxyURL(r, img.URL))
	}
	if len(urls) == 0 {
		return
	}
	accountID, accountName := s.currentAccountForHistory()
	if _, err := s.accounts.AddGeneration(account.GenerationInput{
		Kind:        account.GenerationKindImage,
		Prompt:      prompt,
		Model:       modelName,
		Status:      "succeeded",
		Images:      urls,
		AccountID:   accountID,
		AccountName: accountName,
	}); err != nil {
		log.Printf("record image generation history: %v", err)
	}
}

func (s *Server) recordVideoGeneration(taskID, prompt, modelName, resultURL string, imageFiles []doubao.MediaFile, workerAccountID int64) {
	if s.accounts == nil {
		return
	}
	images := make([]string, 0, len(imageFiles))
	for i, f := range imageFiles {
		if local := s.saveHistoryBytes(f.Data, f.Filename, fmt.Sprintf("ref-%d", i)); local != "" {
			images = append(images, local)
		}
	}
	accountID, accountName := s.accountForHistory(workerAccountID)
	if _, err := s.accounts.AddGeneration(account.GenerationInput{
		Kind:        account.GenerationKindVideo,
		Prompt:      prompt,
		Model:       modelName,
		Status:      "succeeded",
		Images:      images,
		ResultURL:   resultURL,
		TaskID:      taskID,
		AccountID:   accountID,
		AccountName: accountName,
	}); err != nil {
		log.Printf("record video generation history: %v", err)
	}
}

func (s *Server) persistHistoryImage(ctx context.Context, rawURL, prefix string) string {
	data, err := s.fetchMediaBytes(ctx, rawURL)
	if err != nil {
		log.Printf("history: fetch image for persist: %v", err)
		return ""
	}
	ext := ".png"
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, ".jpg"), strings.Contains(lower, ".jpeg"):
		ext = ".jpg"
	case strings.Contains(lower, ".webp"):
		ext = ".webp"
	case strings.Contains(lower, ".gif"):
		ext = ".gif"
	}
	return s.saveHistoryBytes(data, "image"+ext, prefix)
}

func (s *Server) saveHistoryBytes(data []byte, filename, prefix string) string {
	if len(data) == 0 {
		return ""
	}
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".bin"
	}
	name := fmt.Sprintf("%s-%d%s", prefix, time.Now().UnixNano(), strings.ToLower(ext))
	path := filepath.Join(s.historyMediaDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("history: write media %s: %v", path, err)
		return ""
	}
	return "/admin/history/media/" + name
}

func (s *Server) handleAdminHistoryMedia(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminAuth(w, r) {
		return
	}
	name := filepath.Base(r.PathValue("name"))
	if name == "." || name == ".." || name == "" {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.historyMediaDir(), name)
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".webp":
		w.Header().Set("Content-Type", "image/webp")
	case ".gif":
		w.Header().Set("Content-Type", "image/gif")
	case ".mp4":
		w.Header().Set("Content-Type", "video/mp4")
	default:
		w.Header().Set("Content-Type", "image/png")
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, path)
}
