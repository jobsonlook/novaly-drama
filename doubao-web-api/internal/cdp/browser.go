package cdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const (
	doubaoBaseURL = "https://www.doubao.com"
	chatURL       = "https://www.doubao.com/chat/"
)

type Browser struct {
	cdpURL        string
	videoUIMode   string // "skill" (default) or "office"
	allocCtx      context.Context
	allocCancel   context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc
	attachedTabID string
	connectedURL  string
	mu            sync.Mutex
	uiMu          sync.Mutex // serializes visible UI automation (one video task at a time)

	// CDP Network / Browser.download events for completed video media URLs.
	captureMu         sync.Mutex
	capturedVideoURLs []string
	captureListening  bool

	// History unwatermark: capture /im/chain/single response bodies (page fetch
	// hooks often miss them due to SPA timing / race with location.href).
	pendingChainIDs      map[network.RequestID]string
	finishedChainIDs     []network.RequestID
	capturedFallbackAPIs []string
	capturedIMBodies     int
}

type versionResponse struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type tabInfo struct {
	ID                   string `json:"id"`
	URL                  string `json:"url"`
	Type                 string `json:"type"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type fetchResult struct {
	OK      bool   `json:"ok"`
	Status  int    `json:"status"`
	Data    string `json:"data"`
	Error   string `json:"error"`
	Partial bool   `json:"partial,omitempty"`
}

type ImageUploadResult struct {
	URI    string
	URL    string
	Name   string
	Format string
}

type jsonFetchResult struct {
	OK     bool   `json:"ok"`
	Status int    `json:"status"`
	Body   string `json:"body"`
	Error  string `json:"error"`
}

func NewBrowser(cdpURL string) *Browser {
	return &Browser{cdpURL: cdpURL, videoUIMode: "skill"}
}

func (b *Browser) SetVideoUIMode(mode string) {
	if mode == "" {
		mode = "skill"
	}
	b.videoUIMode = mode
}

// filteredChromedpErrorLog drops known harmless chromedp/cdproto decode noise
// (e.g. newer Chrome sending IPAddressSpace=Loopback before cdproto supports it).
func filteredChromedpErrorLog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if strings.Contains(msg, "could not unmarshal event") && strings.Contains(msg, "IPAddressSpace") {
		return
	}
	log.Printf("ERROR: "+format, args...)
}

func browserContextOpts(extra ...chromedp.ContextOption) []chromedp.ContextOption {
	opts := []chromedp.ContextOption{chromedp.WithErrorf(filteredChromedpErrorLog)}
	return append(opts, extra...)
}

func evalReturnByValue(ctx context.Context, js string, out any) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		v, exp, evalErr := runtime.Evaluate(js).
			WithReturnByValue(true).
			Do(ctx)
		if evalErr != nil {
			return evalErr
		}
		if exp != nil {
			return exp
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(v.Value, out)
	}))
}

func (b *Browser) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Drop any previous remote allocator before reconnecting (account switch /
	// Chrome restart). Leaving the old one around causes flaky CDP sessions.
	b.closeSessionUnlocked()

	browserWS, err := resolveBrowserWebSocketURL(ctx, b.cdpURL)
	if err != nil {
		return err
	}
	b.allocCtx, b.allocCancel = chromedp.NewRemoteAllocator(context.Background(), browserWS)

	if tab := waitForTab(ctx, b.cdpURL, 20*time.Second, findUsablePageTab); tab != nil {
		log.Printf("cdp: chrome page tab ready: %s", tab.URL)
	}

	tabs := listTabs(ctx, b.cdpURL)
	if findDoubaoTab(tabs) == nil {
		if tab := waitForTab(ctx, b.cdpURL, 8*time.Second, findDoubaoTab); tab != nil {
			log.Printf("cdp: doubao tab appeared after wait: %s", tab.URL)
			tabs = listTabs(ctx, b.cdpURL)
		}
	}

	var attachTab *tabInfo
	if tab := findDoubaoTab(tabs); tab != nil {
		attachTab = tab
		log.Printf("cdp: attach existing doubao tab: %s", tab.URL)
	} else if tab := findUsablePageTab(tabs); tab != nil {
		attachTab = tab
		log.Printf("cdp: attach existing page tab: %s", tab.URL)
	} else {
		log.Printf("cdp: no page tab among %d targets, opening %s via CDP HTTP", len(tabs), chatURL)
		opened, err := openTabAtURL(ctx, b.cdpURL, chatURL)
		if err != nil {
			return fmt.Errorf("open doubao tab: %w", err)
		}
		attachTab = opened
	}

	href, err := b.connectToTab(ctx, attachTab)
	if err != nil {
		return fmt.Errorf("connect tab: %w", err)
	}

	if !isDoubaoChatURL(href) {
		if err := b.ensureOnDoubaoChat(ctx); err != nil {
			return fmt.Errorf("ensure doubao chat: %w", err)
		}
	}
	if err := b.verifyOnDoubaoChat(ctx); err != nil {
		return err
	}
	b.activateAttachedTab(ctx)
	if err := dismissDoubaoPopups(b.browserCtx); err != nil {
		return err
	}
	// Desktop-app promo often mounts a second or two after chat ready.
	go func() {
		for _, wait := range []time.Duration{1500 * time.Millisecond, 3500 * time.Millisecond} {
			select {
			case <-b.browserCtx.Done():
				return
			case <-time.After(wait):
			}
			_ = dismissDoubaoPopups(b.browserCtx)
		}
	}()
	log.Printf("cdp: doubao chat ready")
	return nil
}

func waitForTab(ctx context.Context, cdpURL string, timeout time.Duration, pick func([]tabInfo) *tabInfo) *tabInfo {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if tab := pick(listTabs(ctx, cdpURL)); tab != nil {
			return tab
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(400 * time.Millisecond):
		}
	}
	return nil
}

func refreshTabInfo(ctx context.Context, cdpURL string, tab *tabInfo) *tabInfo {
	if tab == nil {
		return nil
	}
	tabs := listTabs(ctx, cdpURL)
	for i := range tabs {
		if tabs[i].ID == tab.ID {
			return &tabs[i]
		}
	}
	return tab
}

func tabExists(ctx context.Context, cdpURL, tabID string) bool {
	tabs := listTabs(ctx, cdpURL)
	for i := range tabs {
		if tabs[i].ID == tabID {
			return true
		}
	}
	return false
}

func (b *Browser) verifyOnDoubaoChat(ctx context.Context) error {
	if isDoubaoChatURL(b.connectedURL) {
		return nil
	}
	href, err := b.currentPageURL(ctx)
	if err == nil && isDoubaoChatURL(href) {
		b.connectedURL = href
		return nil
	}
	return fmt.Errorf("verify doubao chat: not on %s (last url=%q err=%v)", chatURL, href, err)
}

func (b *Browser) resetBrowserSession(ctx context.Context) error {
	b.closeSessionUnlocked()
	browserWS, err := resolveBrowserWebSocketURL(ctx, b.cdpURL)
	if err != nil {
		return err
	}
	b.allocCtx, b.allocCancel = chromedp.NewRemoteAllocator(context.Background(), browserWS)
	return nil
}

func (b *Browser) pingTab() error {
	if b.browserCtx == nil {
		return fmt.Errorf("browser context not initialized")
	}
	var ok bool
	return evalReturnByValue(b.browserCtx, `(() => true)()`, &ok)
}

func (b *Browser) connectToTab(ctx context.Context, tab *tabInfo) (string, error) {
	if b.allocCtx == nil {
		return "", fmt.Errorf("cdp allocator not initialized")
	}
	tab = refreshTabInfo(ctx, b.cdpURL, tab)
	if tab == nil {
		return "", fmt.Errorf("nil tab")
	}
	if tab.ID == b.attachedTabID && b.browserCtx != nil && b.browserCtx.Err() == nil {
		if err := b.pingTab(); err == nil {
			return b.connectedURL, nil
		}
		log.Printf("cdp: session lost on tab %s, resetting browser session", shortTabID(tab.ID))
		if err := b.resetBrowserSession(ctx); err != nil {
			return "", err
		}
	} else if b.browserCtx != nil && tab.ID != b.attachedTabID {
		// Switch tabs without canceling old context (cancel would close the Chrome tab).
		b.browserCtx = nil
		b.browserCancel = nil
		b.attachedTabID = ""
		b.connectedURL = ""
	}

	newCtx, newCancel := chromedp.NewContext(
		b.allocCtx,
		browserContextOpts(chromedp.WithTargetID(target.ID(tab.ID)))...,
	)

	var href string
	if err := chromedp.Run(newCtx,
		chromedp.Location(&href),
		chromedp.Evaluate(`true`, nil),
	); err != nil {
		newCancel()
		return "", fmt.Errorf("connect tab %s: %w", shortTabID(tab.ID), err)
	}

	b.browserCtx = newCtx
	b.browserCancel = newCancel
	b.attachedTabID = tab.ID
	b.connectedURL = href
	b.captureListening = false
	b.installVideoNetworkCapture(newCtx)
	log.Printf("cdp: connected to tab %s url=%s", shortTabID(tab.ID), href)
	return href, nil
}

func (b *Browser) noteCapturedVideoURL(u string) {
	u = strings.TrimSpace(u)
	if u == "" || strings.HasPrefix(u, "blob:") || !isLikelyVideoMediaURL(u) {
		return
	}
	b.captureMu.Lock()
	defer b.captureMu.Unlock()
	for _, existing := range b.capturedVideoURLs {
		if existing == u {
			return
		}
	}
	b.capturedVideoURLs = append(b.capturedVideoURLs, u)
}

func (b *Browser) clearCapturedVideoURLs() {
	b.captureMu.Lock()
	b.capturedVideoURLs = nil
	b.captureMu.Unlock()
}

func (b *Browser) snapshotCapturedVideoItems() []VideoItem {
	b.captureMu.Lock()
	defer b.captureMu.Unlock()
	out := make([]VideoItem, 0, len(b.capturedVideoURLs))
	for _, u := range b.capturedVideoURLs {
		out = append(out, VideoItem{VideoURL: u})
	}
	return out
}

// installVideoNetworkCapture listens for media / download URLs that the page
// fetch hook often misses (blob players, direct CDN downloads, etc.).
func (b *Browser) installVideoNetworkCapture(ctx context.Context) {
	if ctx == nil || b.captureListening {
		return
	}
	b.captureListening = true
	b.captureMu.Lock()
	if b.pendingChainIDs == nil {
		b.pendingChainIDs = make(map[network.RequestID]string)
	}
	b.captureMu.Unlock()
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			if e.Request != nil {
				b.noteCapturedVideoURL(e.Request.URL)
				if strings.Contains(e.Request.URL, "/im/chain/single") {
					b.captureMu.Lock()
					b.pendingChainIDs[e.RequestID] = e.Request.URL
					b.captureMu.Unlock()
				}
			}
		case *network.EventResponseReceived:
			if e.Response != nil {
				b.noteCapturedVideoURL(e.Response.URL)
				if strings.Contains(e.Response.URL, "/im/chain/single") {
					b.captureMu.Lock()
					b.pendingChainIDs[e.RequestID] = e.Response.URL
					b.captureMu.Unlock()
				}
			}
		case *network.EventLoadingFinished:
			b.captureMu.Lock()
			if _, ok := b.pendingChainIDs[e.RequestID]; ok {
				b.finishedChainIDs = append(b.finishedChainIDs, e.RequestID)
				delete(b.pendingChainIDs, e.RequestID)
			}
			b.captureMu.Unlock()
		case *cdpbrowser.EventDownloadWillBegin:
			b.noteCapturedVideoURL(e.URL)
			if e.URL != "" {
				log.Printf("generate_video: captured download url (%s)", shortVideoURL(e.URL))
			}
		}
	})
	// Keep Chrome's normal Downloads folder so "Show in folder" still works.
	// Only enable download events for URL capture — do not redirect the path.
	if err := chromedp.Run(ctx,
		network.Enable(),
		cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorDefault).
			WithEventsEnabled(true),
	); err != nil {
		log.Printf("cdp: enable video network capture: %v", err)
	}
}

func (b *Browser) clearCapturedFallbackAPIs() {
	b.captureMu.Lock()
	b.capturedFallbackAPIs = nil
	b.finishedChainIDs = nil
	b.capturedIMBodies = 0
	if b.pendingChainIDs != nil {
		for k := range b.pendingChainIDs {
			delete(b.pendingChainIDs, k)
		}
	}
	b.captureMu.Unlock()
}

func (b *Browser) noteCapturedFallbackAPI(u string) {
	u = strings.TrimSpace(u)
	if u == "" || !strings.Contains(u, "/video/fplay/") {
		return
	}
	b.captureMu.Lock()
	defer b.captureMu.Unlock()
	for _, existing := range b.capturedFallbackAPIs {
		if existing == u {
			return
		}
	}
	b.capturedFallbackAPIs = append(b.capturedFallbackAPIs, u)
}

func (b *Browser) snapshotCapturedFallbackAPIs() []string {
	b.captureMu.Lock()
	defer b.captureMu.Unlock()
	out := make([]string, len(b.capturedFallbackAPIs))
	copy(out, b.capturedFallbackAPIs)
	return out
}

// harvestChainSingleBodies reads finished /im/chain/single response bodies via CDP
// Network.getResponseBody (must run outside the ListenTarget callback).
func (b *Browser) harvestChainSingleBodies(ctx context.Context) {
	if ctx == nil {
		return
	}
	b.captureMu.Lock()
	ids := append([]network.RequestID(nil), b.finishedChainIDs...)
	b.finishedChainIDs = nil
	b.captureMu.Unlock()
	for _, id := range ids {
		bodyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		var body []byte
		err := chromedp.Run(bodyCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			body, err = network.GetResponseBody(id).Do(ctx)
			return err
		}))
		cancel()
		if err != nil || len(body) == 0 {
			if err != nil && !strings.Contains(err.Error(), "No resource with given identifier") {
				log.Printf("scan_history: getResponseBody chain/single: %v", err)
			}
			continue
		}
		text := string(body)
		b.captureMu.Lock()
		b.capturedIMBodies++
		b.captureMu.Unlock()
		apis := ExtractFallbackAPIs(text)
		for _, api := range apis {
			b.noteCapturedFallbackAPI(api)
		}
		if len(apis) > 0 {
			log.Printf("scan_history: chain/single body len=%d fallback_api=%d", len(body), len(apis))
		} else {
			hasKey := strings.Contains(text, "fallback_api")
			hasFplay := strings.Contains(text, "fplay")
			log.Printf("scan_history: chain/single body len=%d fallback_api=0 hasKey=%v hasFplay=%v",
				len(body), hasKey, hasFplay)
		}
	}
}

func (b *Browser) ensureAttached(ctx context.Context) error {
	if b.browserCtx != nil && b.browserCtx.Err() == nil {
		if err := b.pingTab(); err == nil {
			return nil
		}
		log.Printf("cdp: tab session lost (%v), re-attaching", b.browserCtx.Err())
	}
	return b.reattachBestTab(ctx)
}

func (b *Browser) reattachBestTab(ctx context.Context) error {
	if b.allocCtx == nil {
		return fmt.Errorf("cdp allocator not initialized")
	}
	tabs := listTabs(ctx, b.cdpURL)
	tab := findDoubaoTab(tabs)
	if tab == nil {
		tab = waitForTab(ctx, b.cdpURL, 5*time.Second, findDoubaoTab)
	}
	if tab == nil {
		tab = findUsablePageTab(tabs)
	}
	if tab == nil {
		return fmt.Errorf("no usable chrome tab for doubao")
	}
	if _, err := b.connectToTab(ctx, tab); err != nil {
		return err
	}
	log.Printf("cdp: re-attached tab %s url=%s", shortTabID(tab.ID), tab.URL)
	return b.ensureOnDoubaoChat(ctx)
}

// dismissDoubaoPopups closes onboarding / promo overlays and the accidental
// 「编辑对话名称」dialog. It must stay conservative: page-wide "close" matching
// clicks dozens of chrome controls and can open the rename dialog.
func dismissDoubaoPopups(ctx context.Context) error {
	runCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	// CAPTCHA / safety challenges are never ordinary dismissible popups.
	// Leave the page untouched so the user can complete them manually.
	if present, reason := humanVerificationPresent(runCtx); present {
		log.Printf("cdp: human verification active (%s); popup dismissal skipped", reason)
		return nil
	}

	var result struct {
		Closed int      `json:"closed"`
		Hints  []string `json:"hints"`
	}
	const js = `(() => {
		// 「下次提醒我」= desktop-app promo; keep ahead of generic "关闭".
		const labels = ["下次提醒我", "下次再说", "关闭", "跳过", "我知道了", "不感兴趣", "稍后", "暂不需要", "不再提示", "知道了", "取消"];
		const maxClicks = 3;
		let closed = 0;
		const hints = [];
		const clicked = new Set();

		function isVisible(el) {
			if (!el || el.closest('[hidden], [aria-hidden="true"]')) return false;
			const st = window.getComputedStyle(el);
			if (st.display === 'none' || st.visibility === 'hidden' || parseFloat(st.opacity) === 0) return false;
			const r = el.getBoundingClientRect();
			return r.width > 4 && r.height > 4;
		}
		function tryClick(el, hint) {
			if (!el || !isVisible(el) || clicked.has(el) || closed >= maxClicks) return false;
			try {
				el.click();
				clicked.add(el);
				closed++;
				if (hint) hints.push(hint);
				return true;
			} catch (e) { return false; }
		}
		function clickTargets(root) {
			return root.querySelectorAll('button, [role="button"], a, [role="link"]');
		}
		function rootLooksModal(root) {
			if (!root || !isVisible(root)) return false;
			const r = root.getBoundingClientRect();
			// Real overlays cover a meaningful chunk of the viewport.
			if (r.width < 180 || r.height < 80) return false;
			const st = window.getComputedStyle(root);
			const zi = parseInt(st.zIndex || "0", 10);
			if (Number.isFinite(zi) && zi >= 100) return true;
			if (root.getAttribute("role") === "dialog" || root.tagName === "DIALOG") return true;
			const cls = String(root.className || "");
			return /modal|dialog|popup|overlay|mask|drawer/i.test(cls);
		}
		function dismissInside(root, preferCancel) {
			const text = (root.innerText || "").slice(0, 400);
			const isRename = /编辑对话名称|修改对话名称|重命名对话|Rename/.test(text);
			const isDesktopPromo = /下载电脑版|使用完整功能/.test(text);
			const prefer = isRename || preferCancel
				? ["取消", "Cancel", "关闭", "我知道了", "跳过", "下次提醒我", "下次再说"]
				: isDesktopPromo
					? ["下次提醒我", "下次再说", "关闭", "跳过", "我知道了", "稍后", "暂不需要"]
					: labels;
			for (const want of prefer) {
				for (const btn of clickTargets(root)) {
					if (!isVisible(btn)) continue;
					const t = (btn.textContent || "").trim().replace(/\s+/g, "");
					// Never click the primary CTA that opens the desktop download.
					if (/^下载电脑版/.test(t)) continue;
					if (t === want || t.startsWith(want)) {
						if (tryClick(btn, (isRename ? "rename:" : isDesktopPromo ? "desktop-promo:" : "modal:") + want)) return true;
					}
				}
			}
			for (const btn of root.querySelectorAll('[aria-label*="关闭"], [aria-label*="close"], [aria-label*="取消"], [aria-label*="Cancel"]')) {
				if (tryClick(btn, "modal:aria-close")) return true;
			}
			// Top-right circular X without aria-label (desktop download promo).
			if (isDesktopPromo) {
				const rr = root.getBoundingClientRect();
				for (const el of root.querySelectorAll('button, [role="button"], span, i, svg, div')) {
					if (!isVisible(el)) continue;
					const r = el.getBoundingClientRect();
					if (r.width < 12 || r.width > 48 || r.height < 12 || r.height > 48) continue;
					if (r.top < rr.top - 8 || r.top > rr.top + 72) continue;
					if (r.right < rr.right - 72 || r.right > rr.right + 8) continue;
					const t = (el.textContent || "").trim();
					if (t && t !== "×" && t !== "x" && t !== "X" && t !== "✕") continue;
					if (tryClick(el, "desktop-promo:x")) return true;
				}
			}
			return false;
		}

		// 1) Rename dialog first (opened by mis-clicking the chat title).
		for (const root of document.querySelectorAll('[role="dialog"], dialog, [class*="modal" i], [class*="dialog" i], [class*="popup" i], [class*="overlay" i], [class*="mask" i]')) {
			if (!rootLooksModal(root)) continue;
			const text = (root.innerText || "").slice(0, 120);
			if (!/编辑对话名称|修改对话名称|重命名对话|Rename/.test(text)) continue;
			dismissInside(root, true);
			return { closed, hints };
		}

		// 2) Desktop-app promo ("下载电脑版") — often a late overlay with link "下次提醒我".
		for (const root of document.querySelectorAll('[role="dialog"], dialog, [class*="modal" i], [class*="dialog" i], [class*="popup" i], [class*="overlay" i], [class*="mask" i]')) {
			if (closed >= maxClicks) break;
			if (!rootLooksModal(root)) continue;
			const text = (root.innerText || "").slice(0, 400);
			if (!/下载电脑版|使用完整功能/.test(text)) continue;
			dismissInside(root, false);
		}

		// 3) Other real modal overlays only — never scan the whole page for "关闭".
		for (const root of document.querySelectorAll('[role="dialog"], dialog, [class*="modal" i], [class*="dialog" i], [class*="popup" i], [class*="overlay" i], [class*="mask" i]')) {
			if (closed >= maxClicks) break;
			if (!rootLooksModal(root)) continue;
			const text = (root.innerText || "").slice(0, 400);
			if (/下载电脑版|使用完整功能/.test(text)) continue; // already tried above
			dismissInside(root, false);
		}
		return { closed, hints };
	})()`
	if err := evalReturnByValue(runCtx, js, &result); err != nil {
		return nil
	}
	if result.Closed > 0 {
		log.Printf("cdp: dismissed %d doubao popup(s) (%s)", result.Closed, strings.Join(result.Hints, ","))
		if err := chromedp.Run(runCtx, chromedp.Sleep(400*time.Millisecond)); err != nil {
			return err
		}
	}
	return nil
}

func (b *Browser) currentPageURL(ctx context.Context) (string, error) {
	runCtx, cancel := context.WithTimeout(b.browserCtx, 5*time.Second)
	defer cancel()
	var href string
	if err := evalReturnByValue(runCtx, `(() => location.href)()`, &href); err != nil {
		return "", err
	}
	return href, nil
}

func isDoubaoChatURL(href string) bool {
	href = strings.ToLower(strings.TrimSpace(href))
	return strings.Contains(href, "doubao.com") && strings.Contains(href, "/chat")
}

func isBlankBrowserURL(href string) bool {
	href = strings.ToLower(strings.TrimSpace(href))
	return href == "" ||
		href == "about:blank" ||
		strings.HasPrefix(href, "chrome://newtab") ||
		strings.HasPrefix(href, "chrome://new-tab-page")
}

func (b *Browser) ensureOnDoubaoChat(ctx context.Context) error {
	if isDoubaoChatURL(b.connectedURL) {
		if err := b.pingTab(); err == nil {
			return nil
		}
	}
	href, pageErr := b.currentPageURL(ctx)
	if pageErr == nil && isDoubaoChatURL(href) {
		return nil
	}
	if pageErr != nil {
		log.Printf("cdp: read page url failed: %v", pageErr)
	} else if isBlankBrowserURL(href) {
		log.Printf("cdp: current tab is not doubao (%q)", href)
	} else {
		log.Printf("cdp: current tab is not doubao chat (%q)", href)
	}

	tabs := listTabs(ctx, b.cdpURL)
	if tab := findDoubaoTab(tabs); tab != nil && tab.ID != b.attachedTabID {
		if err := b.switchToTab(ctx, tab); err != nil {
			log.Printf("cdp: switch to doubao tab failed: %v", err)
		} else {
			href, pageErr = b.currentPageURL(ctx)
			if pageErr == nil && isDoubaoChatURL(href) {
				return nil
			}
		}
	}

	if pageErr != nil {
		if err := b.attachToUsableTab(ctx, tabs); err != nil {
			return err
		}
		href, pageErr = b.currentPageURL(ctx)
		if pageErr == nil && isDoubaoChatURL(href) {
			return nil
		}
	}

	log.Printf("cdp: navigate current tab to doubao chat (was %q)", href)
	return b.navigateCurrentTabToChat(ctx, href)
}

func isRecoverableNavigateError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "target closed") ||
		strings.Contains(msg, "no such target")
}

func (b *Browser) navigateCurrentTabToChat(ctx context.Context, fromHref string) error {
	tabID := b.attachedTabID
	runCtx, cancel := context.WithTimeout(b.browserCtx, 25*time.Second)
	defer cancel()

	var navErr error
	if isBlankBrowserURL(fromHref) {
		urlJSON, _ := json.Marshal(chatURL)
		navErr = evalReturnByValue(runCtx, fmt.Sprintf(`(() => { location.assign(%s); return true; })()`, string(urlJSON)), new(bool))
	} else {
		navErr = chromedp.Run(runCtx, chromedp.Navigate(chatURL))
	}
	if navErr != nil && isRecoverableNavigateError(navErr) {
		return b.reattachAfterNavigate(ctx, tabID, navErr)
	}
	if navErr != nil {
		return fmt.Errorf("navigate to doubao chat: %w", navErr)
	}
	if err := b.waitForDoubaoChat(ctx, tabID, 20*time.Second); err != nil {
		return err
	}
	if err := chromedp.Run(b.browserCtx, chromedp.Sleep(2*time.Second)); err != nil {
		return err
	}
	return nil
}

func (b *Browser) waitForDoubaoChat(ctx context.Context, tabID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if href, err := b.currentPageURL(ctx); err == nil && isDoubaoChatURL(href) {
			return nil
		}
		tabs := listTabs(ctx, b.cdpURL)
		if tab := findDoubaoTab(tabs); tab != nil {
			if tab.ID != b.attachedTabID {
				if err := b.switchToTab(ctx, tab); err != nil {
					return err
				}
			}
			if href, err := b.currentPageURL(ctx); err == nil && isDoubaoChatURL(href) {
				return nil
			}
		}
		for i := range tabs {
			if tabs[i].ID == tabID && isDoubaoChatURL(tabs[i].URL) {
				if err := b.switchToTab(ctx, &tabs[i]); err != nil {
					return err
				}
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("timeout waiting for doubao chat page")
}

func (b *Browser) reattachAfterNavigate(ctx context.Context, tabID string, cause error) error {
	log.Printf("cdp: navigate interrupted (%v), re-attaching", cause)
	tabs := listTabs(ctx, b.cdpURL)
	if tab := findDoubaoTab(tabs); tab != nil {
		if err := b.switchToTab(ctx, tab); err != nil {
			return err
		}
		return b.waitForDoubaoChat(ctx, tab.ID, 20*time.Second)
	}
	for i := range tabs {
		if tabs[i].ID == tabID {
			if err := b.switchToTab(ctx, &tabs[i]); err != nil {
				return fmt.Errorf("navigate to doubao chat: %w", cause)
			}
			return b.waitForDoubaoChat(ctx, tabID, 15*time.Second)
		}
	}
	if tab := findUsablePageTab(tabs); tab != nil {
		if err := b.switchToTab(ctx, tab); err != nil {
			return fmt.Errorf("navigate to doubao chat: %w", cause)
		}
		return b.navigateCurrentTabToChat(ctx, tab.URL)
	}
	return fmt.Errorf("navigate to doubao chat: %w", cause)
}

func (b *Browser) attachToUsableTab(ctx context.Context, tabs []tabInfo) error {
	if tab := findDoubaoTab(tabs); tab != nil {
		return b.switchToTab(ctx, tab)
	}
	if tab := findUsablePageTab(tabs); tab != nil {
		return b.switchToTab(ctx, tab)
	}
	return fmt.Errorf("no usable page tab")
}

func openTabAtURL(ctx context.Context, cdpURL, url string) (*tabInfo, error) {
	cdpURL = strings.TrimRight(cdpURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, cdpURL+"/json/new?"+url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open tab status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tab tabInfo
	if err := json.Unmarshal(body, &tab); err != nil {
		return nil, err
	}
	if tab.ID == "" {
		return nil, fmt.Errorf("open tab returned empty id")
	}
	return &tab, nil
}

func activateTab(ctx context.Context, cdpURL, tabID string) error {
	if tabID == "" {
		return fmt.Errorf("empty tab id")
	}
	cdpURL = strings.TrimRight(cdpURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cdpURL+"/json/activate/"+tabID, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("activate tab status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (b *Browser) activateAttachedTab(ctx context.Context) {
	if b.attachedTabID == "" {
		return
	}
	if !tabExists(ctx, b.cdpURL, b.attachedTabID) {
		log.Printf("cdp: skip activate, tab %s no longer exists", shortTabID(b.attachedTabID))
		return
	}
	if err := activateTab(ctx, b.cdpURL, b.attachedTabID); err != nil {
		log.Printf("cdp: activate tab failed: %v", err)
		return
	}
	log.Printf("cdp: activated tab %s", shortTabID(b.attachedTabID))
}

func (b *Browser) switchToTab(ctx context.Context, tab *tabInfo) error {
	if tab == nil {
		return fmt.Errorf("nil tab")
	}
	if tab.ID == b.attachedTabID {
		return nil
	}
	_, err := b.connectToTab(ctx, tab)
	return err
}

func shortTabID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (b *Browser) GetConversationID(ctx context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	runCtx, cancel := context.WithTimeout(b.browserCtx, 5*time.Second)
	defer cancel()

	var cid string
	js := `(() => {
		const m = location.pathname.match(/\/chat\/(\d+)/);
		return m ? m[1] : '';
	})()`
	if err := evalReturnByValue(runCtx, js, &cid); err != nil {
		return "", fmt.Errorf("get conversation id: %w", err)
	}
	return strings.TrimSpace(cid), nil
}

func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeSessionUnlocked()
}

// closeSessionUnlocked detaches CDP without closing Chrome tabs.
// Caller must hold b.mu (except when already held by Start).
func (b *Browser) closeSessionUnlocked() {
	if b.allocCancel != nil {
		b.allocCancel()
		b.allocCancel = nil
	}
	b.allocCtx = nil
	b.browserCtx = nil
	b.browserCancel = nil
	b.attachedTabID = ""
	b.connectedURL = ""
	b.captureListening = false
}

func (b *Browser) IsLoggedIn(ctx context.Context) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.ensureAttached(ctx); err != nil {
		return false, err
	}

	runCtx, cancel := context.WithTimeout(b.browserCtx, 10*time.Second)
	defer cancel()

	var cookies []*network.Cookie
	err := chromedp.Run(runCtx,
		network.Enable(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().WithURLs([]string{doubaoBaseURL, "https://doubao.com"}).Do(ctx)
			return err
		}),
	)
	if err != nil {
		return false, err
	}
	for _, c := range cookies {
		if c.Name == "sessionid" && c.Value != "" {
			return true, nil
		}
	}
	return false, nil
}

func (b *Browser) FetchSamantha(ctx context.Context, payloadJSON string, timeout time.Duration) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.ensureReady(ctx); err != nil {
		return "", fmt.Errorf("ensure doubao page: %w", err)
	}

	runCtx, cancel := context.WithTimeout(b.browserCtx, timeout)
	defer cancel()

	payloadLiteral, err := json.Marshal(payloadJSON)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	// Must use IIFE — bare async () => {} returns a function object, not the fetch result.
	js := fmt.Sprintf(`(async () => {
		const payloadJson = %s;
		const params = new URLSearchParams({
			aid: "497858",
			device_platform: "web",
			language: "zh",
			pkg_type: "release_version",
			real_aid: "497858",
			region: "CN",
			samantha_web: "1",
			sys_region: "CN",
			use_olympus_account: "1",
			version_code: "20800",
		});
		const url = "/samantha/chat/completion?" + params.toString();
		try {
			const res = await fetch(url, {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
					Accept: "text/event-stream",
					Referer: %q,
					Origin: %q,
					"Agw-js-conv": "str",
				},
				body: payloadJson,
				credentials: "include",
			});
			if (!res.ok) {
				const errBody = await res.text();
				return { ok: false, status: res.status, error: errBody.slice(0, 500) };
			}
			const reader = res.body?.getReader();
			if (!reader) {
				return { ok: false, status: 500, error: "no response body" };
			}
			const decoder = new TextDecoder();
			let fullText = "";
			while (true) {
				const { done, value } = await reader.read();
				if (done) break;
				fullText += decoder.decode(value, { stream: true });
			}
			return { ok: true, data: fullText };
		} catch (e) {
			return { ok: false, status: 0, error: String(e && (e.message || e)) || "fetch failed" };
		}
	})()`, string(payloadLiteral), chatURL, doubaoBaseURL)

	var result fetchResult
	if err := chromedp.Run(runCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		v, exp, evalErr := runtime.Evaluate(js).
			WithAwaitPromise(true).
			WithReturnByValue(true).
			Do(ctx)
		if evalErr != nil {
			return evalErr
		}
		if exp != nil {
			return exp
		}
		if err := json.Unmarshal(v.Value, &result); err != nil {
			return fmt.Errorf("decode fetch result: %w (raw=%s)", err, string(v.Value))
		}
		return nil
	})); err != nil {
		return "", fmt.Errorf("cdp fetch: %w", err)
	}

	if !result.OK {
		log.Printf("cdp samantha fetch failed: status=%d error=%q", result.Status, result.Error)
		if result.Status == 401 {
			return "", fmt.Errorf("authentication failed: please login to doubao.com in Chrome (status 401)")
		}
		if result.Error != "" {
			return "", fmt.Errorf("samantha request failed (status %d): %s", result.Status, result.Error)
		}
		return "", fmt.Errorf("samantha request failed with status %d", result.Status)
	}
	return result.Data, nil
}

func (b *Browser) FetchSamanthaAsyncStream(ctx context.Context, payloadJSON string, maxWait time.Duration) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.ensureReady(ctx); err != nil {
		return "", fmt.Errorf("ensure doubao page: %w", err)
	}

	if maxWait <= 0 {
		maxWait = 25 * time.Second
	}
	runCtx, cancel := context.WithTimeout(b.browserCtx, maxWait+15*time.Second)
	defer cancel()

	payloadLiteral, err := json.Marshal(payloadJSON)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	maxWaitMs := int(maxWait.Milliseconds())

	js := fmt.Sprintf(`(async () => {
		const payloadJson = %s;
		const maxWaitMs = %d;
		const params = new URLSearchParams({
			aid: "497858",
			device_platform: "web",
			language: "zh",
			pkg_type: "release_version",
			real_aid: "497858",
			region: "CN",
			samantha_web: "1",
			sys_region: "CN",
			use_olympus_account: "1",
			version_code: "20800",
		});
		const url = "/samantha/chat/async/stream?" + params.toString();
		const controller = new AbortController();
		const timer = setTimeout(() => controller.abort(), maxWaitMs);
		let fullText = "";
		try {
			const res = await fetch(url, {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
					Accept: "text/event-stream",
					Referer: %q,
					Origin: %q,
					"Agw-js-conv": "str",
				},
				body: payloadJson,
				credentials: "include",
				signal: controller.signal,
			});
			if (!res.ok) {
				clearTimeout(timer);
				const errBody = await res.text();
				return { ok: false, status: res.status, error: errBody.slice(0, 500), data: fullText };
			}
			const reader = res.body?.getReader();
			if (!reader) {
				clearTimeout(timer);
				return { ok: false, status: 500, error: "no response body", data: fullText };
			}
			const decoder = new TextDecoder();
			while (true) {
				const { done, value } = await reader.read();
				if (done) break;
				fullText += decoder.decode(value, { stream: true });
			}
			clearTimeout(timer);
			return { ok: true, data: fullText };
		} catch (e) {
			clearTimeout(timer);
			if (fullText) {
				return { ok: true, data: fullText, partial: true };
			}
			return { ok: false, status: 0, error: String(e && (e.message || e)) || "fetch failed" };
		}
	})()`, string(payloadLiteral), maxWaitMs, chatURL, doubaoBaseURL)

	var result fetchResult
	if err := chromedp.Run(runCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		v, exp, evalErr := runtime.Evaluate(js).
			WithAwaitPromise(true).
			WithReturnByValue(true).
			Do(ctx)
		if evalErr != nil {
			return evalErr
		}
		if exp != nil {
			return exp
		}
		if err := json.Unmarshal(v.Value, &result); err != nil {
			return fmt.Errorf("decode fetch result: %w (raw=%s)", err, string(v.Value))
		}
		return nil
	})); err != nil {
		return "", fmt.Errorf("cdp async stream fetch: %w", err)
	}

	if !result.OK {
		if result.Data != "" {
			return result.Data, nil
		}
		log.Printf("cdp samantha async stream failed: status=%d error=%q", result.Status, result.Error)
		if result.Status == 401 {
			return "", fmt.Errorf("authentication failed: please login to doubao.com in Chrome (status 401)")
		}
		if result.Error != "" {
			return "", fmt.Errorf("samantha async stream failed (status %d): %s", result.Status, result.Error)
		}
		return "", fmt.Errorf("samantha async stream failed with status %d", result.Status)
	}
	return result.Data, nil
}

func (b *Browser) UploadImage(ctx context.Context, data []byte, filename string) (ImageUploadResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.ensureReady(ctx); err != nil {
		return ImageUploadResult{}, fmt.Errorf("ensure doubao page: %w", err)
	}

	if len(data) == 0 {
		return ImageUploadResult{}, fmt.Errorf("empty image data")
	}
	if filename == "" {
		filename = "image.png"
	}
	meta := resolveUploadImageMeta(data, filename)
	ext := meta.Ext
	uploadName := meta.UploadName
	mime := meta.MIME
	b64 := base64.StdEncoding.EncodeToString(data)

	runCtx, cancel := context.WithTimeout(b.browserCtx, 60*time.Second)
	defer cancel()

	uploadJS := fmt.Sprintf(`(async () => {
		const bytes = Uint8Array.from(atob(%q), c => c.charCodeAt(0));
		const params = new URLSearchParams({
			aid: "497858",
			device_platform: "web",
			language: "zh",
			pkg_type: "release_version",
			real_aid: "497858",
			region: "CN",
			samantha_web: "1",
			sys_region: "CN",
			use_olympus_account: "1",
			version_code: "20800",
		});
		const url = "/samantha/pages/upload_image?" + params.toString();
		try {
			const form = new FormData();
			form.append("data", new Blob([bytes], { type: %q }), %q);
			form.append("file_type", %q);
			const res = await fetch(url, {
				method: "POST",
				headers: { Referer: %q, Origin: %q },
				body: form,
				credentials: "include",
			});
			const body = await res.text();
			return { ok: res.ok, status: res.status, body: body, error: res.ok ? "" : body.slice(0, 500) };
		} catch (e) {
			return { ok: false, status: 0, body: "", error: String(e && (e.message || e)) || "upload failed" };
		}
	})()`, b64, mime, uploadName, ext, chatURL, doubaoBaseURL)

	var uploadResp jsonFetchResult
	if err := b.evaluateAsync(runCtx, uploadJS, &uploadResp); err != nil {
		return ImageUploadResult{}, fmt.Errorf("cdp upload: %w", err)
	}
	if !uploadResp.OK {
		return ImageUploadResult{}, fmt.Errorf("image upload failed (status %d): %s", uploadResp.Status, uploadResp.Error)
	}

	var uploadBody struct {
		Code int `json:"code"`
		Data struct {
			URI string `json:"uri"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(uploadResp.Body), &uploadBody); err != nil {
		return ImageUploadResult{}, fmt.Errorf("parse upload response: %w", err)
	}
	if uploadBody.Code != 0 {
		msg := firstNonEmpty(uploadBody.Msg, string(uploadResp.Body))
		if strings.Contains(msg, "params invalid") {
			return ImageUploadResult{}, fmt.Errorf("image upload error: %s (hint: check file extension matches actual image format, e.g. PNG must use .png)", msg)
		}
		return ImageUploadResult{}, fmt.Errorf("image upload error: %s", msg)
	}
	uri := uploadBody.Data.URI
	if uri == "" {
		return ImageUploadResult{}, fmt.Errorf("image upload returned empty uri")
	}

	fileURLJS := fmt.Sprintf(`(async () => {
		const params = new URLSearchParams({
			aid: "497858",
			device_platform: "web",
			language: "zh",
			pkg_type: "release_version",
			real_aid: "497858",
			region: "CN",
			samantha_web: "1",
			sys_region: "CN",
			use_olympus_account: "1",
			version_code: "20800",
		});
		const url = "/alice/message/get_file_url?" + params.toString();
		const payload = {
			uris: [%q],
			type: "image",
			format: %q,
			expire_second: 3600,
		};
		try {
			const res = await fetch(url, {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
					Referer: %q,
					Origin: %q,
				},
				body: JSON.stringify(payload),
				credentials: "include",
			});
			const body = await res.text();
			return { ok: res.ok, status: res.status, body: body, error: res.ok ? "" : body.slice(0, 500) };
		} catch (e) {
			return { ok: false, status: 0, body: "", error: String(e && (e.message || e)) || "get_file_url failed" };
		}
	})()`, uri, ext, chatURL, doubaoBaseURL)

	var fileURLResp jsonFetchResult
	if err := b.evaluateAsync(runCtx, fileURLJS, &fileURLResp); err != nil {
		return ImageUploadResult{}, fmt.Errorf("cdp get_file_url: %w", err)
	}
	if !fileURLResp.OK {
		return ImageUploadResult{}, fmt.Errorf("get_file_url failed (status %d): %s", fileURLResp.Status, fileURLResp.Error)
	}

	var fileBody struct {
		Code int `json:"code"`
		Data struct {
			FileURLs []struct {
				URI     string `json:"uri"`
				MainURL string `json:"main_url"`
			} `json:"file_urls"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(fileURLResp.Body), &fileBody); err != nil {
		return ImageUploadResult{}, fmt.Errorf("parse get_file_url response: %w", err)
	}
	if fileBody.Code != 0 {
		return ImageUploadResult{}, fmt.Errorf("get_file_url error: %s", firstNonEmpty(fileBody.Msg, string(fileURLResp.Body)))
	}
	if len(fileBody.Data.FileURLs) == 0 {
		return ImageUploadResult{}, fmt.Errorf("get_file_url returned no file_urls")
	}

	info := fileBody.Data.FileURLs[0]
	cdnURL := info.MainURL
	if info.URI != "" {
		uri = info.URI
	}
	return ImageUploadResult{
		URI:    uri,
		URL:    cdnURL,
		Name:   filename,
		Format: ext,
	}, nil
}

func (b *Browser) UploadMedia(ctx context.Context, data []byte, filename string) (ImageUploadResult, error) {
	ext := mediaExt(filename)
	if isAudioExt(ext) {
		return b.uploadResource(ctx, data, filename, "audio", ext, audioMIME(ext), "/samantha/pages/upload_file")
	}
	return b.UploadImage(ctx, data, filename)
}

func (b *Browser) uploadResource(ctx context.Context, data []byte, filename, fileKind, ext, mime, uploadPath string) (ImageUploadResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.ensureReady(ctx); err != nil {
		return ImageUploadResult{}, fmt.Errorf("ensure doubao page: %w", err)
	}

	if len(data) == 0 {
		return ImageUploadResult{}, fmt.Errorf("empty file data")
	}
	if filename == "" {
		filename = "file." + ext
	}
	b64 := base64.StdEncoding.EncodeToString(data)

	runCtx, cancel := context.WithTimeout(b.browserCtx, 90*time.Second)
	defer cancel()

	uploadJS := fmt.Sprintf(`(async () => {
		const bytes = Uint8Array.from(atob(%q), c => c.charCodeAt(0));
		const params = new URLSearchParams({
			aid: "497858", device_platform: "web", language: "zh", pkg_type: "release_version",
			real_aid: "497858", region: "CN", samantha_web: "1", sys_region: "CN",
			use_olympus_account: "1", version_code: "20800",
		});
		const paths = [%q, "/samantha/pages/upload_image"];
		let lastErr = "";
		for (const path of paths) {
			try {
				const url = path + "?" + params.toString();
				const form = new FormData();
				form.append("data", new Blob([bytes], { type: %q }), %q);
				form.append("file_type", %q);
				const res = await fetch(url, {
					method: "POST",
					headers: { Referer: %q, Origin: %q },
					body: form,
					credentials: "include",
				});
				const body = await res.text();
				if (res.ok) return { ok: true, status: res.status, body: body, error: "" };
				lastErr = body.slice(0, 500);
			} catch (e) {
				lastErr = String(e && (e.message || e)) || "upload failed";
			}
		}
		return { ok: false, status: 0, body: "", error: lastErr || "upload failed" };
	})()`, b64, uploadPath, mime, filename, ext, chatURL, doubaoBaseURL)

	var uploadResp jsonFetchResult
	if err := b.evaluateAsync(runCtx, uploadJS, &uploadResp); err != nil {
		return ImageUploadResult{}, fmt.Errorf("cdp upload: %w", err)
	}
	if !uploadResp.OK {
		return ImageUploadResult{}, fmt.Errorf("%s upload failed (status %d): %s", fileKind, uploadResp.Status, uploadResp.Error)
	}

	var uploadBody struct {
		Code int `json:"code"`
		Data struct {
			URI string `json:"uri"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(uploadResp.Body), &uploadBody); err != nil {
		return ImageUploadResult{}, fmt.Errorf("parse upload response: %w", err)
	}
	if uploadBody.Code != 0 {
		return ImageUploadResult{}, fmt.Errorf("%s upload error: %s", fileKind, firstNonEmpty(uploadBody.Msg, string(uploadResp.Body)))
	}
	uri := uploadBody.Data.URI
	if uri == "" {
		return ImageUploadResult{}, fmt.Errorf("%s upload returned empty uri", fileKind)
	}

	fileURLJS := fmt.Sprintf(`(async () => {
		const params = new URLSearchParams({
			aid: "497858", device_platform: "web", language: "zh", pkg_type: "release_version",
			real_aid: "497858", region: "CN", samantha_web: "1", sys_region: "CN",
			use_olympus_account: "1", version_code: "20800",
		});
		const url = "/alice/message/get_file_url?" + params.toString();
		const payload = { uris: [%q], type: %q, format: %q, expire_second: 3600 };
		const res = await fetch(url, {
			method: "POST",
			headers: { "Content-Type": "application/json", Referer: %q, Origin: %q },
			body: JSON.stringify(payload),
			credentials: "include",
		});
		const body = await res.text();
		return { ok: res.ok, status: res.status, body: body, error: res.ok ? "" : body.slice(0, 500) };
	})()`, uri, fileKind, ext, chatURL, doubaoBaseURL)

	var fileURLResp jsonFetchResult
	if err := b.evaluateAsync(runCtx, fileURLJS, &fileURLResp); err != nil {
		return ImageUploadResult{}, fmt.Errorf("cdp get_file_url: %w", err)
	}
	if !fileURLResp.OK {
		return ImageUploadResult{}, fmt.Errorf("get_file_url failed (status %d): %s", fileURLResp.Status, fileURLResp.Error)
	}

	var fileBody struct {
		Code int `json:"code"`
		Data struct {
			FileURLs []struct {
				URI     string `json:"uri"`
				MainURL string `json:"main_url"`
			} `json:"file_urls"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(fileURLResp.Body), &fileBody); err != nil {
		return ImageUploadResult{}, fmt.Errorf("parse get_file_url response: %w", err)
	}
	if fileBody.Code != 0 {
		return ImageUploadResult{}, fmt.Errorf("get_file_url error: %s", firstNonEmpty(fileBody.Msg, string(fileURLResp.Body)))
	}
	if len(fileBody.Data.FileURLs) == 0 {
		return ImageUploadResult{URI: uri, Name: filename, Format: ext}, nil
	}
	info := fileBody.Data.FileURLs[0]
	cdnURL := info.MainURL
	if info.URI != "" {
		uri = info.URI
	}
	return ImageUploadResult{URI: uri, URL: cdnURL, Name: filename, Format: ext}, nil
}

func mediaExt(filename string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if ext == "" {
		return "bin"
	}
	return ext
}

func isAudioExt(ext string) bool {
	switch ext {
	case "mp3", "wav", "m4a", "aac", "ogg":
		return true
	default:
		return false
	}
}

func audioMIME(ext string) string {
	switch ext {
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "m4a":
		return "audio/mp4"
	case "aac":
		return "audio/aac"
	case "ogg":
		return "audio/ogg"
	default:
		return "audio/mpeg"
	}
}

func (b *Browser) evaluateAsync(ctx context.Context, js string, out any) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		v, exp, evalErr := runtime.Evaluate(js).
			WithAwaitPromise(true).
			WithReturnByValue(true).
			Do(ctx)
		if evalErr != nil {
			return evalErr
		}
		if exp != nil {
			return exp
		}
		return json.Unmarshal(v.Value, out)
	}))
}

func imageExt(filename string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepathExt(filename)), ".")
	switch ext {
	case "jpg", "jpeg", "png", "webp", "gif", "bmp":
		if ext == "jpeg" {
			return "jpg"
		}
		return ext
	default:
		return "png"
	}
}

func imageMIMEForUpload(ext string) string {
	switch ext {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	case "bmp":
		return "image/bmp"
	default:
		return "image/png"
	}
}

type uploadImageMeta struct {
	Ext        string
	MIME       string
	UploadName string
}

func resolveUploadImageMeta(data []byte, filename string) uploadImageMeta {
	ext, mime := sniffImageFormat(data)
	nameExt := imageExt(filename)
	if ext == "" {
		ext = nameExt
		mime = imageMIMEForUpload(ext)
	} else if nameExt != ext {
		log.Printf("upload image: filename ext %q mismatches content %q, using detected format", nameExt, ext)
	}
	return uploadImageMeta{
		Ext:        ext,
		MIME:       mime,
		UploadName: "upload." + ext,
	}
}

func sniffImageFormat(data []byte) (ext, mime string) {
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "png", "image/png"
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "jpg", "image/jpeg"
	}
	if len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a") {
		return "gif", "image/gif"
	}
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "webp", "image/webp"
	}
	if len(data) >= 2 && data[0] == 0x42 && data[1] == 0x4D {
		return "bmp", "image/bmp"
	}
	return "", ""
}

func filepathExt(filename string) string {
	if i := strings.LastIndex(filename, "."); i >= 0 {
		return filename[i:]
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func resolveBrowserWebSocketURL(ctx context.Context, cdpURL string) (string, error) {
	cdpURL = strings.TrimRight(cdpURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cdpURL+"/json/version", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var v versionResponse
	if err := json.Unmarshal(body, &v); err != nil {
		return "", err
	}
	if v.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("empty webSocketDebuggerUrl from %s/json/version", cdpURL)
	}
	return v.WebSocketDebuggerURL, nil
}

func listTabs(ctx context.Context, cdpURL string) []tabInfo {
	cdpURL = strings.TrimRight(cdpURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cdpURL+"/json/list", nil)
	if err != nil {
		return nil
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var tabs []tabInfo
	if err := json.Unmarshal(body, &tabs); err != nil {
		return nil
	}
	return tabs
}

func findDoubaoTab(tabs []tabInfo) *tabInfo {
	var fallback *tabInfo
	for i := range tabs {
		t := &tabs[i]
		if t.Type != "page" || !strings.Contains(t.URL, "doubao.com") {
			continue
		}
		if strings.Contains(t.URL, "/chat") {
			return t
		}
		fallback = t
	}
	return fallback
}

func findUsablePageTab(tabs []tabInfo) *tabInfo {
	var blank *tabInfo
	var anyPage *tabInfo
	for i := range tabs {
		t := &tabs[i]
		if t.Type != "page" {
			continue
		}
		u := strings.ToLower(t.URL)
		if strings.HasPrefix(u, "devtools://") || strings.HasPrefix(u, "chrome-extension://") {
			continue
		}
		if anyPage == nil {
			anyPage = t
		}
		if isBlankBrowserURL(u) {
			if blank == nil {
				blank = t
			}
		}
	}
	if blank != nil {
		return blank
	}
	return anyPage
}
