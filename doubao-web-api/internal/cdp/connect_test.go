package cdp

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

func TestConnectExistingDoubaoTab(t *testing.T) {
	cdpURL := os.Getenv("DOUBAO_CDP_URL")
	if cdpURL == "" {
		cdpURL = "http://127.0.0.1:9222"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tabs := listTabs(ctx, cdpURL)
	tab := findDoubaoTab(tabs)
	if tab == nil {
		t.Skip("no doubao tab")
	}

	hostWS, err := resolveBrowserWebSocketURL(ctx, cdpURL)
	if err != nil {
		t.Fatalf("resolve browser ws: %v", err)
	}
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), hostWS)
	defer allocCancel()

	runCtx, runCancel := chromedp.NewContext(
		allocCtx,
		chromedp.WithTargetID(target.ID(tab.ID)),
	)
	defer runCancel()

	deadline, cancelDeadline := context.WithTimeout(runCtx, 10*time.Second)
	defer cancelDeadline()

	var href string
	if err := chromedp.Run(deadline, chromedp.Location(&href)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Simulate a second Run after connectToTab returns (must stay alive).
	var ok bool
	if err := chromedp.Run(runCtx, chromedp.Evaluate(`(() => true)()`, &ok)); err != nil {
		t.Fatalf("second run after connect: %v", err)
	}
	if !isDoubaoChatURL(href) {
		t.Fatalf("unexpected url: %s", href)
	}
}
