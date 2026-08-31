package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mask/ai/doubao-web-api/internal/cdp"
	"github.com/mask/ai/doubao-web-api/internal/config"
)

func main() {
	id := os.Args[1]
	out := os.Args[2]
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	b := cdp.NewBrowser(cfg.CDPURL)
	if err := b.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer b.Close()
	items, err := b.ScanConversation(ctx, id, true)
	if err != nil {
		log.Fatal(err)
	}
	if len(items) == 0 {
		log.Fatal("no items")
	}
	for _, hv := range items {
		fmt.Printf("vid=%s title=%q clean=%v err=%s\n", hv.Vid, hv.ChatTitle, hv.CleanURL != "", hv.Error)
		if hv.CleanURL == "" || !strings.Contains(strings.ToLower(hv.CleanURL), "unwatermarked") {
			continue
		}
		dest := filepath.Join(out, hv.Vid+".mp4")
		n, err := cdp.DownloadVideoURL(ctx, hv.CleanURL, dest)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("saved", n, dest)
	}
}
