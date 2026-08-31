package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/mask/ai/doubao-web-api/internal/cdp"
)

func main() {
	id := "38434745307950082"
	if len(os.Args) > 1 {
		id = os.Args[1]
	}
	ctx := context.Background()
	b := cdp.NewBrowser("http://127.0.0.1:9222")
	if err := b.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer b.Close()
	logged, _ := b.IsLoggedIn(ctx)
	fmt.Println("logged_in=", logged)

	if err := b.NavigateToConversation(ctx, id); err != nil {
		log.Fatal(err)
	}
	time.Sleep(6 * time.Second)

	// Need evaluate - use ScanConversation internals via public Extract
	items, err := b.ExtractVideosFromPage(ctx)
	fmt.Printf("videos=%d err=%v\n", len(items), err)
	b2, _ := json.MarshalIndent(items, "", "  ")
	fmt.Println(string(b2))

	// Dump diagnostics with chromedp on same browser is hard without exported evaluate.
	// Re-navigate and dump via temporary exported helper — use chromedp allocator attach.
	allocCtx, cancel := chromedp.NewRemoteAllocator(context.Background(), "ws://127.0.0.1:9222/devtools/browser")
	_ = allocCtx
	_ = cancel
}
