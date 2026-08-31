package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

var (
	reFallbackAPI = regexp.MustCompile(`https://(?:vas-lf-x\.snssdk\.com|[^"'\\\s<>]+)/video/fplay/[^\s"'<>\\]+`)
	// Chrome extension patterns for nested / double-escaped JSON strings.
	reFallbackAPIKey    = regexp.MustCompile(`"fallback_api"\s*:\s*"((?:\\.|[^"\\])+)"`)
	reFallbackAPIEscKey = regexp.MustCompile(`fallback_api\\":\\"(.*?)\\"`)
	reVidField          = regexp.MustCompile(`"(?:vid|video_id)"\s*:\s*"(v0[A-Za-z0-9_-]{8,})"`)
)

func decodeFallbackAPIValue(value string) string {
	text := value
	for i := 0; i < 3; i++ {
		var decoded string
		if err := json.Unmarshal([]byte(`"`+text+`"`), &decoded); err != nil || decoded == text {
			break
		}
		text = decoded
	}
	text = strings.ReplaceAll(text, `\u0026`, "&")
	text = strings.ReplaceAll(text, `&amp;`, "&")
	text = strings.ReplaceAll(text, `\/`, "/")
	return strings.TrimSpace(text)
}

// ExtractFallbackAPIs finds Doubao VOD fallback_api URLs in raw response text / HTML.
func ExtractFallbackAPIs(text string) []string {
	if text == "" {
		return nil
	}
	decoded := strings.ReplaceAll(text, `\u0026`, "&")
	decoded = strings.ReplaceAll(decoded, `&amp;`, "&")
	decoded = strings.ReplaceAll(decoded, `\/`, "/")
	seen := map[string]struct{}{}
	var out []string
	add := func(u string) {
		u = decodeFallbackAPIValue(u)
		u = strings.TrimRight(u, `\",'`)
		u = strings.TrimSpace(u)
		if !strings.HasPrefix(u, "http") || !strings.Contains(u, "/video/fplay/") {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	for _, m := range reFallbackAPI.FindAllString(decoded, -1) {
		add(m)
	}
	for _, m := range reFallbackAPIKey.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	for _, m := range reFallbackAPIEscKey.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	return out
}

// ExtractVids finds Doubao video ids (v0...) in raw response text.
func ExtractVids(text string) []string {
	if text == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, m := range reVidField.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		vid := m[1]
		if _, ok := seen[vid]; ok {
			continue
		}
		seen[vid] = struct{}{}
		out = append(out, vid)
	}
	return out
}

// PreferFallbackAPIForVid picks the fallback_api that embeds vid, else the last one.
func PreferFallbackAPIForVid(apis []string, vid string) string {
	if len(apis) == 0 {
		return ""
	}
	if vid != "" {
		for i := len(apis) - 1; i >= 0; i-- {
			if strings.Contains(apis[i], vid) {
				return apis[i]
			}
		}
	}
	return apis[len(apis)-1]
}

// IsUnwatermarkedVideoURL reports whether a play URL looks like a clean (no-logo) stream.
func IsUnwatermarkedVideoURL(raw string) bool {
	u := strings.ToLower(raw)
	return strings.Contains(u, "lr=unwatermarked") ||
		strings.Contains(u, "logo_type=unwatermarked")
}

// resolveUnwatermarkedViaFallback fetches fallback_api with logo_type=unwatermarked
// inside the Doubao page context and decrypts the returned main_url token.
func (b *Browser) resolveUnwatermarkedViaFallback(ctx context.Context, fallbackAPI string) (string, error) {
	fallbackAPI = strings.TrimSpace(fallbackAPI)
	if fallbackAPI == "" {
		return "", fmt.Errorf("empty fallback_api")
	}
	if _, err := url.Parse(fallbackAPI); err != nil {
		return "", fmt.Errorf("invalid fallback_api: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	js := fmt.Sprintf(`(async () => {
	  try {
	    const fallbackApi = %q;
	    const QAAB_SALT_HEX = "4dd4c2e6b83162090e52b3c7a6733ba41cb2462b829ab58a196b39db57177524f49baf7f08e8d68d26a72e37c1a95a2f1f05a51892aef2949732b62a38aadd58";
	    function hexToBytes(hex) {
	      const b = new Uint8Array(hex.length / 2);
	      for (let i = 0; i < b.length; i++) b[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
	      return b;
	    }
	    function concatBytes(a, b) {
	      const o = new Uint8Array(a.length + b.length);
	      o.set(a, 0); o.set(b, a.length);
	      return o;
	    }
	    function padBase64(t) { return t + "=".repeat((4 - (t.length %% 4)) %% 4); }
	    function asciiUrlFromBytes(bytes) {
	      if (!bytes || !bytes.length) return "";
	      for (const x of bytes) if (x !== 9 && x !== 10 && x !== 13 && (x < 32 || x > 126)) return "";
	      return new TextDecoder().decode(bytes);
	    }
	    function base64DecodeLoose(text) {
	      const input = String(text || "").trim();
	      const variants = [
	        input,
	        input.replace(/[$@#]/g, c => ({ "$": "_", "@": "/", "#": "." }[c])),
	        input.replace(/[$@#]/g, c => ({ "$": "+", "@": "/", "#": "=" }[c]))
	      ];
	      const seen = new Set();
	      for (const candidate of variants) {
	        if (!candidate || seen.has(candidate)) continue;
	        seen.add(candidate);
	        try {
	          const normalized = padBase64(candidate).replace(/-/g, "+").replace(/_/g, "/");
	          const binary = atob(normalized);
	          const bytes = new Uint8Array(binary.length);
	          for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
	          return bytes;
	        } catch (_) {}
	      }
	      return null;
	    }
	    function stripPkcs7(bytes) {
	      if (!bytes || !bytes.length) return new Uint8Array();
	      const pad = bytes[bytes.length - 1];
	      if (pad < 1 || pad > 16 || pad > bytes.length) return bytes;
	      for (let i = bytes.length - pad; i < bytes.length; i++) if (bytes[i] !== pad) return bytes;
	      return bytes.slice(0, bytes.length - pad);
	    }
	    async function decryptAesCbcUrl(payload, keyBytes, ivBytes) {
	      if (!payload.length || payload.length %% 16 !== 0) return "";
	      try {
	        const key = await crypto.subtle.importKey("raw", keyBytes, "AES-CBC", false, ["decrypt"]);
	        const plain = new Uint8Array(await crypto.subtle.decrypt({ name: "AES-CBC", iv: ivBytes }, key, payload));
	        const direct = asciiUrlFromBytes(plain);
	        if (/^https?:\/\//i.test(direct)) return direct;
	        const url = asciiUrlFromBytes(stripPkcs7(plain));
	        return /^https?:\/\//i.test(url) ? url : "";
	      } catch (_) { return ""; }
	    }
	    async function decodeQaabToken(token, keySeed) {
	      const data = base64DecodeLoose(token);
	      const seed = base64DecodeLoose(keySeed);
	      if (!data || !seed) return "";
	      const digest1 = await crypto.subtle.digest("SHA-512", seed.slice(0, 32));
	      const digest2 = new Uint8Array(await crypto.subtle.digest("SHA-512", concatBytes(new Uint8Array(digest1), hexToBytes(QAAB_SALT_HEX))));
	      const key = digest2.slice(0, 16), iv = digest2.slice(16, 32);
	      const attempts = [];
	      if (data.length >= 4 && data[0] === 0xa8 && data[1] === 0x00 && data[2] === 0x01 && data[3] === 0x00) {
	        attempts.push({ payload: data.slice(4), key, iv });
	        attempts.push({ payload: data.slice(4), key: iv, iv: key });
	        if (data.length > 36) {
	          attempts.push({ payload: data.slice(36), key, iv: data.slice(20, 36) });
	          attempts.push({ payload: data.slice(36), key, iv });
	        }
	      } else {
	        attempts.push({ payload: data, key, iv });
	      }
	      for (const a of attempts) {
	        const url = await decryptAesCbcUrl(a.payload, a.key, a.iv);
	        if (url) return url;
	      }
	      return "";
	    }
	    function tryDecodeBase64Url(token) {
	      const bytes = base64DecodeLoose(token);
	      if (!bytes) return "";
	      const text = asciiUrlFromBytes(bytes);
	      return /^https?:\/\//i.test(text) ? text : "";
	    }
	    async function decodeMainUrl(token, keySeed) {
	      if (/^https?:\/\//i.test(token)) return token;
	      const plain = tryDecodeBase64Url(token);
	      if (plain) return plain;
	      if (token.startsWith("qAAB") && keySeed) return await decodeQaabToken(token, keySeed);
	      return "";
	    }
	    function findKeySeedDeep(value, depth = 0) {
	      if (depth > 10 || value == null) return "";
	      if (typeof value === "string") {
	        let m = value.match(/(?:^|[?&])key_seed=([^&"'<>\s]+)/i);
	        if (m) return decodeURIComponent(m[1]);
	        m = value.match(/["']key_seed["']\s*:\s*["']([^"']+)/i);
	        return m ? decodeURIComponent(m[1]) : "";
	      }
	      if (typeof value !== "object") return "";
	      if (typeof value.key_seed === "string" && value.key_seed.trim()) return value.key_seed.trim();
	      for (const item of Object.values(value)) {
	        const hit = findKeySeedDeep(item, depth + 1);
	        if (hit) return hit;
	      }
	      return "";
	    }
	    function pickMainUrlToken(data) {
	      const videoList = data && data.video_list;
	      const entries = (videoList && typeof videoList === "object" && Object.keys(videoList).length)
	        ? Object.values(videoList) : [data];
	      let best = null;
	      for (const entry of entries) {
	        if (!entry || typeof entry !== "object") continue;
	        const token = entry.main_url || entry.play_url || "";
	        if (typeof token !== "string" || !token.trim()) continue;
	        const score = Number(entry.bitrate || entry.real_bitrate || 0)
	          + Number(entry.vwidth || entry.width || 0) * Number(entry.vheight || entry.height || 0);
	        if (!best || score > best.score) best = { token: token.trim(), score };
	      }
	      return best ? best.token : "";
	    }

	    const u = new URL(fallbackApi);
	    u.searchParams.set("channel", "no");
	    u.searchParams.set("codec_type", "8");
	    u.searchParams.set("logo_type", "unwatermarked");
	    const resp = await fetch(u.toString(), {
	      method: "GET",
	      credentials: "omit",
	      headers: { "accept": "application/json,text/plain,*/*" }
	    });
	    const text = await resp.text();
	    let payload;
	    try { payload = JSON.parse(text); }
	    catch (e) {
	      return { ok: false, status: resp.status, error: "json parse: " + String(e), preview: text.slice(0, 200) };
	    }
	    const videoInfo = payload.video_info || (payload.data && payload.data.video_info) || payload;
	    const data = (videoInfo && videoInfo.data) || videoInfo || {};
	    const dataObj = (data && typeof data === "object") ? data : {};
	    const token = pickMainUrlToken(dataObj);
	    const keySeed = findKeySeedDeep(payload) || findKeySeedDeep(fallbackApi);
	    const cleanUrl = token ? await decodeMainUrl(token, keySeed) : "";
	    if (!cleanUrl) {
	      return { ok: false, status: resp.status, error: "empty clean url", tokenPrefix: token ? token.slice(0, 24) : "", keySeedFound: !!keySeed };
	    }
	    return { ok: true, status: resp.status, url: cleanUrl };
	  } catch (e) {
	    return { ok: false, error: String(e && (e.message || e)) };
	  }
	})()`, fallbackAPI)

	var out struct {
		OK     bool   `json:"ok"`
		URL    string `json:"url"`
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	if err := b.evaluateAsync(runCtx, js, &out); err != nil {
		return "", fmt.Errorf("cdp resolve unwatermarked: %w", err)
	}
	if !out.OK || strings.TrimSpace(out.URL) == "" {
		if out.Error != "" {
			return "", fmt.Errorf("resolve unwatermarked failed: %s", out.Error)
		}
		return "", fmt.Errorf("resolve unwatermarked failed (status %d)", out.Status)
	}
	return strings.TrimSpace(out.URL), nil
}

// collectCapturedFallbackAPIs reads fallback_api URLs captured by the page hook / DOM.
func (b *Browser) collectCapturedFallbackAPIs(ctx context.Context) ([]string, string, error) {
	const js = `(() => {
	  const apis = [];
	  const vids = [];
	  const seenA = new Set();
	  const seenV = new Set();
	  function addApi(u) {
	    if (typeof u !== "string" || !u.includes("/video/fplay/")) return;
	    u = u.replace(/\\u0026/g, "&").replace(/&amp;/g, "&").replace(/\\\//g, "/").trim();
	    if (!/^https?:\/\//.test(u) || seenA.has(u)) return;
	    seenA.add(u);
	    apis.push(u);
	  }
	  function addVid(v) {
	    if (typeof v === "string" && /^v0[A-Za-z0-9_-]{8,}$/.test(v) && !seenV.has(v)) {
	      seenV.add(v); vids.push(v);
	    }
	  }
	  function scanText(text) {
	    if (!text || typeof text !== "string") return;
	    for (const m of text.matchAll(/https?:\/\/[^"'\\\s<>]+\/video\/fplay\/[^"'\\\s<>]+/g)) {
	      addApi(m[0]);
	    }
	    for (const m of text.matchAll(/"fallback_api"\s*:\s*"((?:\\.|[^"\\])+)"/g)) {
	      try {
	        addApi(JSON.parse('"' + m[1].replace(/"/g, '\\"') + '"'));
	      } catch (_) { addApi(m[1]); }
	    }
	    for (const m of text.matchAll(/fallback_api\\":\\"(.*?)\\"/g)) {
	      try {
	        addApi(JSON.parse('"' + m[1].replace(/"/g, '\\"') + '"'));
	      } catch (_) { addApi(m[1]); }
	    }
	    for (const m of text.matchAll(/"(?:vid|video_id)"\s*:\s*"(v0[^"]+)"/g)) addVid(m[1]);
	  }
	  const cap = window.__doubaoVideoCapture || {};
	  if (Array.isArray(cap.fallbackApis)) cap.fallbackApis.forEach(addApi);
	  if (Array.isArray(cap.vids)) cap.vids.forEach(addVid);
	  if (Array.isArray(cap.chunks)) {
	    for (const c of cap.chunks) scanText(c);
	  }
	  try { scanText(document.documentElement.innerHTML); } catch (_) {}
	  return { apis, vids };
	})()`
	var out struct {
		Apis []string `json:"apis"`
		Vids []string `json:"vids"`
	}
	if err := evalReturnByValue(ctx, js, &out); err != nil {
		return nil, "", err
	}
	vid := ""
	if len(out.Vids) > 0 {
		vid = out.Vids[len(out.Vids)-1]
	}
	return out.Apis, vid, nil
}

// collectAllFallbackAPIs merges page-hook captures, CDP /im/chain bodies, and HTML scrape.
func (b *Browser) collectAllFallbackAPIs(ctx context.Context) (apis []string, vid string) {
	b.harvestChainSingleBodies(ctx)
	pageApis, pageVid, err := b.collectCapturedFallbackAPIs(ctx)
	if err != nil {
		log.Printf("generate_video: collect fallback_api: %v", err)
	}
	apis = uniqueStrings(append(pageApis, b.snapshotCapturedFallbackAPIs()...))
	vid = pageVid
	return apis, vid
}

// waitForFallbackAPI polls like the Chrome extension: fallback_api often arrives a few
// seconds after the watermarked player URL (via /im/chain/single or hydrated HTML).
func (b *Browser) waitForFallbackAPI(ctx context.Context, preferVid string, timeout time.Duration) (apis []string, vid string) {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	deadline := time.Now().Add(timeout)
	attempt := 0
	for {
		attempt++
		apis, vid = b.collectAllFallbackAPIs(ctx)
		if preferVid != "" {
			if fb := PreferFallbackAPIForVid(apis, preferVid); fb != "" {
				if attempt > 1 {
					log.Printf("generate_video: fallback_api ready after %ds (n=%d vid=%s)", attempt-1, len(apis), preferVid)
				}
				return apis, preferVid
			}
		} else if len(apis) > 0 {
			if attempt > 1 {
				log.Printf("generate_video: fallback_api ready after %ds (n=%d)", attempt-1, len(apis))
			}
			return apis, vid
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return apis, vid
		}
		// Nudge SPA hydration / message chain fetch (same idea as history scan).
		_ = chromedp.Run(ctx, chromedp.Evaluate(`window.scrollBy(0, 80)`, nil))
		select {
		case <-ctx.Done():
			return apis, vid
		case <-time.After(1 * time.Second):
		}
	}
}

// UpgradeVideoToUnwatermarked replaces item.VideoURL with a clean stream when possible.
// On failure it returns the original item unchanged.
func (b *Browser) UpgradeVideoToUnwatermarked(ctx context.Context, item VideoItem) VideoItem {
	if item.VideoURL != "" && IsUnwatermarkedVideoURL(item.VideoURL) {
		return item
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = b.browserCtx
	}
	if runCtx == nil {
		return item
	}

	preferVid := item.Vid
	apis, vid := b.waitForFallbackAPI(runCtx, preferVid, 22*time.Second)
	if preferVid == "" {
		preferVid = vid
	}
	fb := PreferFallbackAPIForVid(apis, preferVid)
	if fb == "" && item.FallbackAPI != "" {
		fb = item.FallbackAPI
	}
	if fb == "" {
		if item.VideoURL != "" {
			log.Printf("generate_video: no fallback_api captured after wait, keeping watermarked url (%s)", shortVideoURL(item.VideoURL))
		}
		return item
	}

	clean, err := b.resolveUnwatermarkedViaFallback(runCtx, fb)
	if err != nil {
		log.Printf("generate_video: unwatermark resolve failed: %v (keep original)", err)
		return item
	}
	log.Printf("generate_video: upgraded to unwatermarked url (%s)", shortVideoURL(clean))
	item.VideoURL = clean
	item.FallbackAPI = fb
	if item.Vid == "" {
		item.Vid = preferVid
	}
	return item
}
