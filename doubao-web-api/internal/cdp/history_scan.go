package cdp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	_ "modernc.org/sqlite"
)

var reChatConvID = regexp.MustCompile(`doubao\.com/chat/(\d{10,})`)

// HistoryVideo is one video discovered while scanning Doubao chat history.
type HistoryVideo struct {
	ConversationID string  `json:"conversation_id"`
	ChatTitle      string  `json:"chat_title,omitempty"`
	Vid            string  `json:"vid,omitempty"`
	FallbackAPI    string  `json:"fallback_api,omitempty"`
	VideoURL       string  `json:"video_url,omitempty"`
	CleanURL       string  `json:"clean_url,omitempty"`
	CoverURL       string  `json:"cover_url,omitempty"`
	Duration       float64 `json:"duration,omitempty"`
	Width          int     `json:"width,omitempty"`
	Height         int     `json:"height,omitempty"`
	LocalPath      string  `json:"local_path,omitempty"`
	Bytes          int64   `json:"bytes,omitempty"`
	Error          string  `json:"error,omitempty"`
}

// ListConversationIDsFromChromeHistory reads Chrome History (copy) for doubao /chat/<id> URLs.
func ListConversationIDsFromChromeHistory(sessionDir string) ([]string, error) {
	return ListConversationIDsFromChromeHistoryBetween(sessionDir, time.Time{}, time.Time{})
}

// ListConversationIDsFromChromeHistoryBetween filters by Chrome last_visit_time.
// since/until are inclusive/exclusive in the caller's timezone (converted to UTC for WebKit epoch).
// Zero times mean no bound.
func ListConversationIDsFromChromeHistoryBetween(sessionDir string, since, until time.Time) ([]string, error) {
	histPath := findChromeHistoryDB(sessionDir)
	if histPath == "" {
		return nil, fmt.Errorf("chrome history not found under %s", sessionDir)
	}
	if _, err := os.Stat(histPath); err != nil {
		return nil, fmt.Errorf("chrome history not found: %w", err)
	}
	tmp, err := os.CreateTemp("", "chrome-history-*.db")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	src, err := os.Open(histPath)
	if err != nil {
		return nil, err
	}
	dst, err := os.Create(tmpPath)
	if err != nil {
		src.Close()
		return nil, err
	}
	if _, err := io.Copy(dst, src); err != nil {
		src.Close()
		dst.Close()
		return nil, err
	}
	src.Close()
	dst.Close()

	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Chrome WebKit epoch: microseconds since 1601-01-01 UTC.
	// Do NOT use time.Duration — spans >~290y overflow int64 nanoseconds.
	const chromeEpochOffsetSec int64 = 11644473600 // 1601-01-01 → 1970-01-01
	q := `SELECT url FROM urls WHERE url LIKE '%doubao.com/chat/%'`
	args := []any{}
	if !since.IsZero() {
		q += ` AND last_visit_time >= ?`
		args = append(args, since.UTC().UnixMicro()+chromeEpochOffsetSec*1_000_000)
	}
	if !until.IsZero() {
		q += ` AND last_visit_time < ?`
		args = append(args, until.UTC().UnixMicro()+chromeEpochOffsetSec*1_000_000)
	}
	q += ` ORDER BY last_visit_time DESC`

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[string]struct{}{}
	var ids []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		m := reChatConvID.FindStringSubmatch(u)
		if len(m) < 2 {
			continue
		}
		id := m[1]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ProbeConversation opens a past chat and reports whether fallback_api was captured.
func (b *Browser) ProbeConversation(ctx context.Context, conversationID string) (HistoryVideo, error) {
	items, err := b.ScanConversation(ctx, conversationID, true)
	if err != nil {
		return HistoryVideo{}, err
	}
	diag := b.pageScanDiagnostics(ctx)
	log.Printf("scan_history: probe diag %s: %s", conversationID, diag)
	if len(items) == 0 {
		return HistoryVideo{
			ConversationID: conversationID,
			Error:          "no video found on page: " + diag,
		}, nil
	}
	hv := items[0]
	if hv.Error == "" && hv.FallbackAPI == "" && hv.CleanURL == "" {
		hv.Error = "no fallback_api: " + diag
	}
	return hv, nil
}

func (b *Browser) pageScanDiagnostics(ctx context.Context) string {
	b.mu.Lock()
	runCtx := b.browserCtx
	b.mu.Unlock()
	if runCtx == nil {
		return "no browser ctx"
	}
	const js = `(() => {
	  const cap = window.__doubaoVideoCapture || {};
	  const videos = document.querySelectorAll('video').length;
	  const text = (document.body && document.body.innerText || '').replace(/\s+/g, ' ').slice(0, 240);
	  const perf = [];
	  try {
	    for (const e of performance.getEntriesByType('resource')) {
	      const n = e.name || '';
	      if (/im\/chain|fplay|fallback|thread_message|video\/tos|mime_type=video_mp4/i.test(n)) perf.push(n.slice(0, 160));
	    }
	  } catch (_) {}
	  const html = document.documentElement.innerHTML || '';
	  const chunkPreview = (cap.chunks || []).slice(0, 3).map(c => {
	    const s = String(c || '');
	    return { len: s.length, head: s.slice(0, 120), hasFplay: /fplay|fallback_api|video_url|v0[A-Za-z0-9_-]{8,}/.test(s) };
	  });
	  return {
	    href: location.href,
	    title: document.title || '',
	    videos,
	    iframes: document.querySelectorAll('iframe').length,
	    fallbackApis: (cap.fallbackApis || []).length,
	    vids: (cap.vids || []).length,
	    videoURLs: (cap.videoURLs || []).length,
	    chunks: (cap.chunks || []).length,
	    chunkPreview,
	    htmlFplay: /fplay|fallback_api/.test(html),
	    htmlVid: /"vid"\s*:\s*"v0/.test(html),
	    htmlLen: html.length,
	    perf: perf.slice(0, 15),
	    text
	  };
	})()`
	var out struct {
		Href         string `json:"href"`
		Title        string `json:"title"`
		Videos       int    `json:"videos"`
		Iframes      int    `json:"iframes"`
		FallbackApis int    `json:"fallbackApis"`
		Vids         int    `json:"vids"`
		VideoURLs    int    `json:"videoURLs"`
		Chunks       int    `json:"chunks"`
		ChunkPreview []struct {
			Len     int    `json:"len"`
			Head    string `json:"head"`
			HasFplay bool  `json:"hasFplay"`
		} `json:"chunkPreview"`
		HTMLFplay bool     `json:"htmlFplay"`
		HTMLVid   bool     `json:"htmlVid"`
		HTMLLen   int      `json:"htmlLen"`
		Perf      []string `json:"perf"`
		Text      string   `json:"text"`
	}
	if err := evalReturnByValue(runCtx, js, &out); err != nil {
		return "diag err: " + err.Error()
	}
	preview, _ := json.Marshal(out.ChunkPreview)
	perfPreview, _ := json.Marshal(out.Perf)
	return fmt.Sprintf("href=%s title=%q videos=%d iframes=%d fb=%d vids=%d urls=%d chunks=%d htmlFplay=%v htmlVid=%v htmlLen=%d perf=%s chunkPreview=%s text=%q",
		out.Href, out.Title, out.Videos, out.Iframes, out.FallbackApis, out.Vids, out.VideoURLs, out.Chunks, out.HTMLFplay, out.HTMLVid, out.HTMLLen, string(perfPreview), string(preview), out.Text)
}

// ScanConversation navigates to a chat the same way the Chrome extension does:
// open chat → capture /im/chain/single for vid + fallback_api → resolve clean URL.
// If resolveClean is true: prefer fallback_api+logo_type=unwatermarked, else get_play_info(vid).
func (b *Browser) ScanConversation(ctx context.Context, conversationID string, resolveClean bool) ([]HistoryVideo, error) {
	if conversationID == "" || conversationID == "0" || strings.HasPrefix(conversationID, "local_") {
		return nil, fmt.Errorf("invalid conversation id: %s", conversationID)
	}
	if err := b.ensureAttached(ctx); err != nil {
		return nil, err
	}

	b.mu.Lock()
	runCtx := b.browserCtx
	b.mu.Unlock()
	if runCtx == nil {
		return nil, fmt.Errorf("browser not connected")
	}

	navCtx, cancel := context.WithTimeout(runCtx, 120*time.Second)
	defer cancel()

	if err := installCaptureHookOnNewDocument(navCtx); err != nil {
		log.Printf("scan_history: AddScriptToEvaluateOnNewDocument: %v", err)
	}
	b.clearCapturedFallbackAPIs()
	b.clearCapturedVideoURLs()

	// Warm SPA on /chat/ WITH hooks, then open target via sidebar click so
	// /im/chain/single fires while fetch/XHR hooks are already installed
	// (same timing as the Chrome extension content script).
	if err := chromedp.Run(navCtx,
		chromedp.Navigate(doubaoBaseURL+"/chat/"),
		chromedp.Sleep(2*time.Second),
	); err != nil {
		return nil, fmt.Errorf("navigate chat home: %w", err)
	}
	_ = installVideoCaptureHook(navCtx)
	_ = dismissDoubaoPopups(navCtx)
	b.harvestChainSingleBodies(navCtx)

	// Clear capture buffers so we only keep this conversation's payloads.
	_ = chromedp.Run(navCtx, chromedp.Evaluate(`(() => {
		const c = window.__doubaoVideoCapture || (window.__doubaoVideoCapture = {});
		c.chunks = []; c.videoURLs = []; c.fallbackApis = []; c.vids = [];
		return true;
	})()`, nil))
	b.clearCapturedFallbackAPIs()

	var clicked string
	_ = evalReturnByValue(navCtx, fmt.Sprintf(`(() => {
		const id = %q;
		const a = document.querySelector('a[href*="/chat/' + id + '"]');
		if (a) { a.click(); return 'href:' + (a.getAttribute('href') || ''); }
		for (const el of document.querySelectorAll('[href*="/chat/' + id + '"]')) {
			try { el.click(); return 'attr'; } catch (_) {}
		}
		// Fallback: hard navigate (hooks already on document via OnNewDocument for next load)
		location.href = '/chat/' + id;
		return 'location';
	})()`, conversationID), &clicked)
	log.Printf("scan_history: open %s via %q", conversationID, clicked)
	_ = chromedp.Run(navCtx, chromedp.Sleep(5*time.Second))
	_ = installVideoCaptureHook(navCtx)
	_ = dismissDoubaoPopups(navCtx)
	b.harvestChainSingleBodies(navCtx)

	// If still not on the conversation, force navigate once (hooks re-applied after).
	var pathNow string
	_ = evalReturnByValue(navCtx, `location.pathname || ""`, &pathNow)
	if !strings.Contains(pathNow, conversationID) {
		targetURL := fmt.Sprintf("%s/chat/%s", doubaoBaseURL, conversationID)
		_ = chromedp.Run(navCtx, chromedp.Navigate(targetURL), chromedp.Sleep(5*time.Second))
		_ = installVideoCaptureHook(navCtx)
		b.harvestChainSingleBodies(navCtx)
	}

	_ = chromedp.Run(navCtx, chromedp.Evaluate(`(() => {
		window.scrollTo(0, document.body.scrollHeight);
		const main = document.querySelector('main') || document.body;
		if (main) main.scrollTop = main.scrollHeight;
		for (const v of document.querySelectorAll('video')) {
			try { v.muted = true; v.play().catch(() => {}); } catch (_) {}
		}
		return true;
	})()`, nil))

	var title string
	_ = evalReturnByValue(navCtx, `document.title || ""`, &title)

	// Wait specifically for fallback_api (true unwatermark path). Do not stop early on vid-only.
	deadline := time.Now().Add(50 * time.Second)
	emptyDeadline := time.Now().Add(14 * time.Second) // non-video chats bail sooner
	var pageItems []VideoItem
	sawVidOrVideo := false
	htmlTick := 0
	for time.Now().Before(deadline) {
		b.harvestChainSingleBodies(navCtx)
		items, _ := b.ExtractVideosFromPage(ctx)
		apisTmp, vidTmp, _ := b.collectCapturedFallbackAPIs(navCtx)
		apisTmp = uniqueStrings(append(apisTmp, b.snapshotCapturedFallbackAPIs()...))
		allVids := collectCapturedVids(navCtx)
		if vidTmp != "" {
			allVids = append(allVids, vidTmp)
		}
		// Full HTML scrape is expensive over CDP — only every ~5s.
		htmlTick++
		if htmlTick == 1 || htmlTick%5 == 0 {
			var htmlBlob string
			_ = evalReturnByValue(navCtx, `document.documentElement.innerHTML || ""`, &htmlBlob)
			apisTmp = uniqueStrings(append(apisTmp, ExtractFallbackAPIs(htmlBlob)...))
			htmlVids := ExtractVids(htmlBlob)
			if len(htmlVids) > 0 {
				sawVidOrVideo = true
			}
		}
		if len(items) > 0 || len(allVids) > 0 {
			sawVidOrVideo = true
		}
		if len(items) > 0 {
			pageItems = items
		}
		if len(apisTmp) > 0 {
			log.Printf("scan_history: captured fallback_api count=%d for %s", len(apisTmp), conversationID)
			break
		}
		if !sawVidOrVideo && time.Now().After(emptyDeadline) {
			log.Printf("scan_history: no video signals in 14s — skip long wait for %s", conversationID)
			break
		}
		// Keep waiting while video/vid appears — extension often gets fallback_api slightly later.
		_ = chromedp.Run(navCtx, chromedp.Evaluate(`window.scrollBy(0, 200)`, nil))
		time.Sleep(1 * time.Second)
		_ = allVids
	}

	b.harvestChainSingleBodies(navCtx)
	apis, lastVid, err := b.collectCapturedFallbackAPIs(navCtx)
	if err != nil {
		log.Printf("scan_history: collect fallback_api %s: %v", conversationID, err)
	}
	apis = uniqueStrings(append(apis, b.snapshotCapturedFallbackAPIs()...))
	// Extension also scrapes fallback_api from page HTML (often escaped in message JSON).
	var htmlBlob string
	_ = evalReturnByValue(navCtx, `document.documentElement.innerHTML || ""`, &htmlBlob)
	apis = uniqueStrings(append(apis, ExtractFallbackAPIs(htmlBlob)...))
	vids := uniqueStrings(append(append(collectCapturedVids(navCtx), lastVid), ExtractVids(htmlBlob)...))
	metaDurations := collectVideoDurations(navCtx)
	b.captureMu.Lock()
	imBodies := b.capturedIMBodies
	b.captureMu.Unlock()
	log.Printf("scan_history: %s apis=%d vids=%v pageVideos=%d imBodies=%d",
		conversationID, len(apis), vids, len(pageItems), imBodies)

	// Build candidate items from: page videos, fallback_apis, and intercepted vids.
	candidates := make([]VideoItem, 0)
	seen := map[string]struct{}{}
	addCand := func(item VideoItem) {
		key := item.Vid
		if key == "" {
			key = item.FallbackAPI
		}
		if key == "" {
			key = item.VideoURL
		}
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, item)
	}

	for _, item := range pageItems {
		if item.Vid == "" {
			item.Vid = PreferFallbackAPIForVid(apis, "") // no-op if empty
			item.Vid = vidFromFallbackAPI(PreferFallbackAPIForVid(apis, item.Vid))
		}
		if item.FallbackAPI == "" {
			item.FallbackAPI = PreferFallbackAPIForVid(apis, item.Vid)
		}
		if item.Vid == "" && len(vids) > 0 {
			item.Vid = vids[len(vids)-1]
		}
		addCand(item)
	}
	for _, fb := range apis {
		addCand(VideoItem{FallbackAPI: fb, Vid: vidFromFallbackAPI(fb)})
	}
	for _, v := range vids {
		addCand(VideoItem{Vid: v, FallbackAPI: PreferFallbackAPIForVid(apis, v)})
	}

	out := make([]HistoryVideo, 0, len(candidates))
	for _, item := range candidates {
		hv := b.historyFromItem(navCtx, conversationID, title, item, resolveClean, metaDurations)
		out = append(out, hv)
	}
	return out, nil
}

func (b *Browser) historyFromItem(ctx context.Context, convID, title string, item VideoItem, resolveClean bool, durations []float64) HistoryVideo {
	hv := HistoryVideo{
		ConversationID: convID,
		ChatTitle:      title,
		Vid:            item.Vid,
		FallbackAPI:    item.FallbackAPI,
		VideoURL:       item.VideoURL,
		CoverURL:       item.CoverURL,
		Duration:       item.Duration,
		Width:          item.Width,
		Height:         item.Height,
	}
	if hv.Duration <= 0 && len(durations) > 0 {
		hv.Duration = durations[0]
	}
	if !resolveClean {
		return hv
	}
	if item.VideoURL != "" && IsUnwatermarkedVideoURL(item.VideoURL) {
		hv.CleanURL = item.VideoURL
		return hv
	}

	// Only true clean path (Chrome extension strategy 0): fallback_api → logo_type=unwatermarked.
	// get_play_info still returns lr=video_gen_watermark_dyn (watermarked "豆包AI生成").
	upgraded := b.UpgradeVideoToUnwatermarked(ctx, item)
	hv.FallbackAPI = upgraded.FallbackAPI
	if upgraded.Vid != "" {
		hv.Vid = upgraded.Vid
	}
	if IsUnwatermarkedVideoURL(upgraded.VideoURL) {
		hv.CleanURL = upgraded.VideoURL
		hv.VideoURL = upgraded.VideoURL
		return hv
	}
	fb := upgraded.FallbackAPI
	if fb == "" {
		fb = item.FallbackAPI
	}
	if fb != "" {
		clean, err := b.resolveUnwatermarkedViaFallback(ctx, fb)
		if err == nil && clean != "" && IsUnwatermarkedVideoURL(clean) {
			hv.CleanURL = clean
			hv.FallbackAPI = fb
			return hv
		}
		if err == nil && clean != "" {
			// Accept resolved URL even if lr= query is missing; prefer size later.
			hv.CleanURL = clean
			hv.FallbackAPI = fb
			return hv
		}
		if err != nil {
			hv.Error = err.Error()
			log.Printf("scan_history: fallback_api resolve: %v", err)
		}
	}

	vid := hv.Vid
	if vid == "" {
		vid = item.Vid
	}
	if vid != "" && fb == "" {
		hv.Error = "watermarked_only: have vid but no fallback_api (get_play_info keeps 豆包AI生成 watermark)"
		return hv
	}
	if hv.Error == "" {
		hv.Error = "no fallback_api captured"
	}
	return hv
}

// PlayInfo is the result of POST /samantha/media/get_play_info (Chrome extension path).
type PlayInfo struct {
	DownURL  string
	CoverURL string
	Duration float64
	Width    int
	Height   int
}

// FetchPlayInfo calls doubao get_play_info inside the page context (session cookies).
func (b *Browser) FetchPlayInfo(ctx context.Context, vid string) (PlayInfo, error) {
	vid = strings.TrimSpace(vid)
	if vid == "" {
		return PlayInfo{}, fmt.Errorf("empty vid")
	}
	runCtx := ctx
	if runCtx == nil {
		b.mu.Lock()
		runCtx = b.browserCtx
		b.mu.Unlock()
	}
	if runCtx == nil {
		return PlayInfo{}, fmt.Errorf("browser not connected")
	}
	js := fmt.Sprintf(`(async () => {
	  try {
	    const vid = %q;
	    const res = await fetch(
	      'https://www.doubao.com/samantha/media/get_play_info?aid=497858&device_platform=web&language=zh-CN',
	      {
	        method: 'POST',
	        credentials: 'include',
	        headers: {
	          'Content-Type': 'application/json',
	          'accept-language': 'zh-CN,zh;q=0.9',
	          'origin': 'https://www.doubao.com',
	          'referer': 'https://www.doubao.com/'
	        },
	        body: JSON.stringify({ key: vid })
	      }
	    );
	    const json = await res.json();
	    if (json.code !== 0 || !json.data) {
	      return { ok: false, error: json.msg || ('code=' + json.code) };
	    }
	    const data = json.data;
	    const original = data.original_media_info || {};
	    const preview = (data.media_info && data.media_info[0]) || {};
	    const downurl = original.main_url || preview.main_url || '';
	    if (!downurl) return { ok: false, error: 'empty main_url' };
	    const meta = original.meta || preview.meta || {};
	    return {
	      ok: true,
	      downurl,
	      cover_url: data.poster_url || '',
	      duration: parseFloat(meta.duration) || 0,
	      width: parseInt(meta.width, 10) || 0,
	      height: parseInt(meta.height, 10) || 0
	    };
	  } catch (e) {
	    return { ok: false, error: String(e && (e.message || e)) };
	  }
	})()`, vid)

	var out struct {
		OK       bool    `json:"ok"`
		DownURL  string  `json:"downurl"`
		CoverURL string  `json:"cover_url"`
		Duration float64 `json:"duration"`
		Width    int     `json:"width"`
		Height   int     `json:"height"`
		Error    string  `json:"error"`
	}
	if err := b.evaluateAsync(runCtx, js, &out); err != nil {
		return PlayInfo{}, fmt.Errorf("get_play_info cdp: %w", err)
	}
	if !out.OK || out.DownURL == "" {
		if out.Error != "" {
			return PlayInfo{}, fmt.Errorf("get_play_info: %s", out.Error)
		}
		return PlayInfo{}, fmt.Errorf("get_play_info failed")
	}
	return PlayInfo{
		DownURL:  out.DownURL,
		CoverURL: out.CoverURL,
		Duration: out.Duration,
		Width:    out.Width,
		Height:   out.Height,
	}, nil
}

func collectCapturedVids(ctx context.Context) []string {
	const js = `(() => {
	  const cap = window.__doubaoVideoCapture || {};
	  const out = [];
	  const seen = new Set();
	  function add(v) {
	    if (typeof v === 'string' && /^v0[A-Za-z0-9_-]{8,}$/.test(v) && !seen.has(v)) {
	      seen.add(v); out.push(v);
	    }
	  }
	  if (Array.isArray(cap.vids)) cap.vids.forEach(add);
	  if (Array.isArray(cap.chunks)) {
	    for (const c of cap.chunks) {
	      if (!c || typeof c !== 'string') continue;
	      for (const m of c.matchAll(/"(?:vid|video_id)"\s*:\s*"(v0[^"]+)"/g)) add(m[1]);
	    }
	  }
	  try {
	    const html = document.documentElement.innerHTML || '';
	    for (const m of html.matchAll(/"(?:vid|video_id)"\s*:\s*"(v0[^"]+)"/g)) add(m[1]);
	  } catch (_) {}
	  for (const v of document.querySelectorAll('video[data-doubao-vid]')) {
	    add(v.getAttribute('data-doubao-vid') || '');
	  }
	  return out;
	})()`
	var out []string
	_ = evalReturnByValue(ctx, js, &out)
	return out
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func collectVideoDurations(ctx context.Context) []float64 {
	const js = `(() => {
	  const out = [];
	  for (const v of document.querySelectorAll('video')) {
	    const d = Number(v.duration);
	    if (d && isFinite(d) && d > 0) out.push(d);
	  }
	  return out;
	})()`
	var out []float64
	_ = evalReturnByValue(ctx, js, &out)
	return out
}

func vidFromFallbackAPI(fb string) string {
	fb = strings.TrimSpace(fb)
	if fb == "" {
		return ""
	}
	u, err := url.Parse(fb)
	if err != nil {
		return ""
	}
	base := filepath.Base(u.Path)
	if strings.HasPrefix(base, "v0") {
		return base
	}
	parts := strings.Split(u.Path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.HasPrefix(parts[i], "v0") && len(parts[i]) > 8 {
			return parts[i]
		}
	}
	return ""
}

// DownloadVideoURL downloads a play URL to destPath (mp4).
func DownloadVideoURL(ctx context.Context, videoURL, destPath string) (int64, error) {
	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		return 0, fmt.Errorf("empty video url")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Referer", doubaoBaseURL+"/")
	req.Header.Set("Accept", "*/*")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("download status %d", resp.StatusCode)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(resp.Body, 120<<20))
	if err != nil {
		return n, err
	}
	return n, nil
}

func findChromeHistoryDB(sessionDir string) string {
	candidates := []string{
		filepath.Join(sessionDir, "Default", "History"),
		filepath.Join(sessionDir, "History"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p
		}
	}
	return ""
}

// DiscoverSessionDirs returns chrome user-data dirs under root that look like Doubao sessions.
func DiscoverSessionDirs(root string) []string {
	var out []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	// Prefer named account sessions over bare Default when both exist.
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "Default" || name == "Safe Browsing" || strings.HasPrefix(name, ".") {
			continue
		}
		// Skip Chrome component / cache dirs accidentally living under session/.
		if strings.Contains(name, " ") || name == "BrowserMetrics" || name == "GrShaderCache" ||
			name == "GraphiteDawnCache" || name == "ShaderCache" || name == "component_crx_cache" ||
			strings.HasSuffix(name, "Cache") {
			continue
		}
		dir := filepath.Join(root, name)
		if findChromeHistoryDB(dir) != "" {
			out = append(out, dir)
		}
	}
	if len(out) == 0 {
		if bare := filepath.Join(root); findChromeHistoryDB(bare) != "" {
			out = append(out, bare)
		}
	}
	sort.Strings(out)
	return out
}
