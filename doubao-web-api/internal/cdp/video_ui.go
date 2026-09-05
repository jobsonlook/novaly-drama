package cdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type VideoItem struct {
	VideoURL     string  `json:"video_url"`
	CoverURL     string  `json:"cover_url"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	Duration     float64 `json:"duration"`
	FromVideoTag bool    `json:"from_video_tag,omitempty"`
	Vid          string  `json:"vid,omitempty"`
	FallbackAPI  string  `json:"fallback_api,omitempty"`
}

type LocalMediaFile struct {
	Data     []byte
	Filename string
}

type VideoETA struct {
	Text    string // e.g. "预计等待 15 分钟"
	Minutes int    // upper bound when Doubao quotes a range
}

type VideoUIOptions struct {
	Prompt           string
	Ratio            string
	RefImageKeys     []string
	RefImageFiles    []LocalMediaFile
	RefAudioKey      string
	RefAudioData     []byte
	RefAudioFilename string
	Timeout          time.Duration
	Duration         int64  // 视频时长（秒）：5/10/15，0 或未传时默认 10；其它值就近映射
	Model            string // fast / mini，默认 fast
	OnETA            func(VideoETA)
}

const (
	DefaultVideoDurationSec = 10
	DefaultVideoUIModel     = "fast"
)

// Allowed Doubao Seedance UI durations (chip labels: 5s / 10s / 15s).
var allowedVideoDurationsSec = []int{5, 10, 15}

func normalizeVideoDurationSec(sec int) int {
	if sec <= 0 {
		return DefaultVideoDurationSec
	}
	// Generate enough source material for exact local trimming. Mapping 7s to
	// the nearest 5s can never produce a 7s final video; use the next supported
	// Seedance duration instead.
	for _, d := range allowedVideoDurationsSec {
		if sec <= d {
			return d
		}
	}
	return allowedVideoDurationsSec[len(allowedVideoDurationsSec)-1]
}

// NormalizeVideoDurationSec rounds API duration up to a Doubao-supported
// 5 / 10 / 15 second source duration so callers can trim to the exact request.
func NormalizeVideoDurationSec(sec int) int {
	return normalizeVideoDurationSec(sec)
}

func videoPromptPrefix(durationSec int) string {
	durationSec = normalizeVideoDurationSec(durationSec)
	return fmt.Sprintf("帮我严格按照下面要求生成%d秒的视频，无需二次确认，请直接开始生成", durationSec)
}

var (
	reVideoConfirmPending = regexp.MustCompile(`请确认以下视频生成参数|请核对以下视频生成参数|确认后我再开始生成|请确认后我再开始生成|确认参数后生成视频|确认以下.*参数|核对以下.*参数|确认无误后请回复|整理好视频生成参数|请确认后.*开始生成`)
	// Doubao ack copy drifts: older ETA banners plus newer short lines like
	// 「收到，即将为您生成视频。」
	// 「正在为您生成10秒的仙侠玄幻视频」— 「生成」与「视频」之间常夹时长/题材。
	reVideoGenerating = regexp.MustCompile(`视频生成已提交|收到[，,]?\s*即将为您生成视频|即将为您生成视频|正在为您生成.{0,40}?视频|本次使用|大约需要|视频生成好后，我会主动发送给你|预计等待`)
	// Do NOT match 「无水印下载」alone — the Chrome extension injects that button permanently.
	reVideoComplete  = regexp.MustCompile(`你的视频生成好了|视频已生成完成|视频生成完成|视频生成好了|已为你生成.*视频`)
	reVideoETARange  = regexp.MustCompile(`(?:预计等待|大约需要)\s*(\d+)\s*[-～~—到至]+\s*(\d+)\s*分钟`)
	reVideoETASingle = regexp.MustCompile(`(?:预计等待|大约需要)\s*(\d+)\s*分钟`)
)

func textNeedsVideoConfirm(text string) bool {
	if textIndicatesVideoGenerating(text) || textIndicatesVideoComplete(text) {
		// Already generating / finished — never treat as "please confirm params".
		return false
	}
	return reVideoConfirmPending.MatchString(text)
}

func textIndicatesVideoGenerating(text string) bool {
	if textIndicatesVideoComplete(text) {
		return false
	}
	if _, _, ok := matchVideoFailureMessage(text); ok {
		// Policy / hard failures must not look like "still generating".
		return false
	}
	return reVideoGenerating.MatchString(text)
}

func textIndicatesVideoComplete(text string) bool {
	return reVideoComplete.MatchString(text)
}

// parseVideoETA extracts Doubao's quoted wait time from ack copy such as
// 「预计等待 15 分钟」or older 「大约需要 1-3 分钟」.
func parseVideoETA(text string) (VideoETA, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return VideoETA{}, false
	}
	if m := reVideoETARange.FindStringSubmatch(text); len(m) == 3 {
		a, errA := strconv.Atoi(m[1])
		b, errB := strconv.Atoi(m[2])
		if errA != nil || errB != nil || a <= 0 || b <= 0 {
			return VideoETA{}, false
		}
		if a > b {
			a, b = b, a
		}
		label := fmt.Sprintf("预计等待 %d～%d 分钟", a, b)
		return VideoETA{Text: label, Minutes: b}, true
	}
	if m := reVideoETASingle.FindStringSubmatch(text); len(m) == 2 {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			return VideoETA{}, false
		}
		return VideoETA{Text: fmt.Sprintf("预计等待 %d 分钟", n), Minutes: n}, true
	}
	return VideoETA{}, false
}

func hasVideoPromptPrefix(prompt string) bool {
	return strings.Contains(prompt, "无需二次确认，请直接开始生成")
}

func withVideoPromptPrefix(prompt string, durationSec int) string {
	prompt = strings.TrimSpace(prompt)
	prefix := videoPromptPrefix(durationSec)
	if prompt == "" {
		return prefix
	}
	if hasVideoPromptPrefix(prompt) {
		return prompt
	}
	return prefix + "\n\n" + prompt
}

func buildVideoUIPrompt(prompt, ratio string, imageCount int, durationSec int) string {
	var body string
	if imageCount > 0 {
		if strings.Contains(prompt, "图1") || strings.Contains(prompt, "图2") {
			if ratio != "" {
				body = fmt.Sprintf("请生成视频（画面比例 %s）：\n%s", ratio, prompt)
			} else {
				body = "请生成视频：\n" + prompt
			}
		} else {
			header := "请根据上传的图片生成视频"
			if ratio != "" {
				header += "，画面比例 " + ratio
			}
			body = header + "：\n" + prompt
		}
	} else if ratio != "" {
		body = fmt.Sprintf("%s（画面比例 %s）", prompt, ratio)
	} else {
		body = prompt
	}
	return withVideoPromptPrefix(body, durationSec)
}

func buildOfficeVideoUIPrompt(prompt, ratio string, imageCount int, hasAudio bool, durationSec int) string {
	var body string
	// User prompt already labels 图1/图2/音频1 — keep wrapper short.
	if strings.Contains(prompt, "图1") || strings.Contains(prompt, "图2") {
		if ratio != "" {
			body = fmt.Sprintf("请生成视频（画面比例 %s）：\n%s", ratio, prompt)
		} else {
			body = "请生成视频：\n" + prompt
		}
	} else {
		header := "请帮我生成一段视频"
		switch {
		case imageCount > 0 && hasAudio:
			header = "请根据上传的图片和音频参考生成视频（图1起依次为上传顺序，音频为音色参考）"
		case imageCount > 0:
			header = "请根据上传的图片生成视频"
		case hasAudio:
			header = "请根据上传的音频参考生成视频（音频为音色/配音参考）"
		}
		if ratio != "" {
			header += "，画面比例 " + ratio
		}
		body = header + "：\n" + prompt
	}
	return withVideoPromptPrefix(body, durationSec)
}

func clickFirstLabel(ctx context.Context, labels ...string) error {
	var lastErr error
	for _, label := range labels {
		if err := clickByLabel(ctx, label, false); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no matching label found")
}

// fileInputHelperJS is injected into attach*ViaUI scripts to locate hidden <input type="file">.
const fileInputHelperJS = `
function findFileInput() {
	const inputs = [...document.querySelectorAll('input[type="file"]')];
	return inputs.find(i => !i.disabled) || inputs[0] || null;
}
async function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }
async function revealFileInput(officeMode) {
	let input = findFileInput();
	if (input) return input;

	async function tryClick(el) {
		if (!el) return null;
		try { el.click(); } catch (e) {}
		await sleep(500);
		return findFileInput();
	}

	for (const sel of ['[aria-label*="上传"]', '[aria-label*="添加"]', '[aria-label*="附件"]', '[title*="上传"]', '[title*="添加"]']) {
		for (const el of document.querySelectorAll(sel)) {
			if (!el.offsetParent) continue;
			input = await tryClick(el);
			if (input) return input;
		}
	}

	const composer = [...document.querySelectorAll('textarea, [contenteditable]:not([contenteditable="false"]), [role="textbox"]')].find(e => {
		if (!e) return false;
		const r = e.getBoundingClientRect();
		return r.width > 80 && r.height > 8;
	});
	let scope = document;
	if (composer) {
		scope = composer.closest('footer, [class*="footer"], [class*="composer"], [class*="input"], [class*="chat"]') || composer.parentElement?.parentElement || document;
	}
	for (const el of scope.querySelectorAll('button, [role="button"], div, span, a, svg')) {
		if (!el.offsetParent) continue;
		const t = (el.textContent || '').trim();
		const aria = (el.getAttribute('aria-label') || el.getAttribute('title') || '').trim();
		if (t === '+' || t === '＋' || aria.includes('上传') || aria.includes('添加') || aria.includes('附件')) {
			input = await tryClick(el);
			if (input) return input;
		}
	}

	const labels = officeMode
		? ["本地电脑", "上传图片", "上传文件", "上传", "附件", "图片", "本地文件"]
		: ["本地电脑", "上传图片", "上传文件", "上传", "附件", "图片", "本地文件"];
	for (const label of labels) {
		for (const el of document.querySelectorAll('button, [role="button"], div, span, a, li')) {
			if (!el.offsetParent) continue;
			const t = (el.textContent || '').trim();
			if (!t || t.length > 24) continue;
			if (t === label || t.includes(label)) {
				input = await tryClick(el);
				if (input) return input;
			}
		}
	}
	return findFileInput();
}`

func ensureUploadEntryVisible(ctx context.Context) error {
	const js = `(async () => {
` + fileInputHelperJS + `
		await revealFileInput(false);
		return { ok: !!findFileInput() };
	})()`
	var result struct {
		OK bool `json:"ok"`
	}
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		v, exp, evalErr := runtime.Evaluate(js).WithAwaitPromise(true).WithReturnByValue(true).Do(ctx)
		if evalErr != nil {
			return evalErr
		}
		if exp != nil {
			return exp
		}
		return json.Unmarshal(v.Value, &result)
	})); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("upload entry not visible")
	}
	return nil
}

func (b *Browser) EnsureChatPage(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ensureReady(ctx)
}

func (b *Browser) ensureReady(ctx context.Context) error {
	if err := b.ensureAttached(ctx); err != nil {
		return err
	}
	if err := b.ensureOnDoubaoChat(ctx); err != nil {
		return err
	}
	return dismissDoubaoPopups(b.browserCtx)
}

type clickPoint struct {
	Found bool    `json:"found"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Text  string  `json:"text"`
	Error string  `json:"error"`
}

func clickByLabel(ctx context.Context, label string, exact bool) error {
	if _, err := waitForHumanVerification(ctx); err != nil {
		return err
	}
	_ = dismissDoubaoPopups(ctx)
	var pt clickPoint
	js := fmt.Sprintf(`(() => {
		const target = %s;
		const exact = %t;
		const vw = window.innerWidth;
		function isVisible(el) {
			if (!el || el.closest('[hidden], [aria-hidden="true"]')) return false;
			const st = window.getComputedStyle(el);
			if (st.display === 'none' || st.visibility === 'hidden' || parseFloat(st.opacity) === 0) return false;
			const r = el.getBoundingClientRect();
			return r.width > 4 && r.height > 4;
		}
		function rejectNoise(el) {
			if (el.closest('[role="dialog"], dialog, [class*="modal" i], [class*="dialog" i]')) {
				const root = el.closest('[role="dialog"], dialog, [class*="modal" i], [class*="dialog" i]');
				const title = ((root && root.innerText) || "").slice(0, 80);
				if (/编辑对话名称|修改对话名称|重命名对话/.test(title)) return true;
			}
			const r = el.getBoundingClientRect();
			// Header conversation title sits top-center; never treat it as an action chip.
			if (r.top < 72 && r.left > vw * 0.2 && r.right < vw * 0.85 && r.height < 48) return true;
			return false;
		}
		const candidates = [];
		for (const el of document.querySelectorAll('button, [role="button"], div, span, a, li')) {
			if (!isVisible(el)) continue;
			if (rejectNoise(el)) continue;
			const text = (el.textContent || '').trim();
			if (!text) continue;
			if (exact ? text === target : text.includes(target)) {
				candidates.push(el);
			}
		}
		if (!candidates.length) return { found: false, error: "not found: " + target };
		const el = candidates.sort((a, b) => (a.textContent || '').length - (b.textContent || '').length)[0];
		el.scrollIntoView({ block: 'center', inline: 'center' });
		const r = el.getBoundingClientRect();
		return { found: true, x: r.left + r.width / 2, y: r.top + r.height / 2, text: (el.textContent || '').trim().slice(0, 40) };
	})()`, jsonString(label), exact)
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return err
	}
	if !pt.Found {
		if pt.Error != "" {
			return fmt.Errorf("%s", pt.Error)
		}
		return fmt.Errorf("element not found: %s", label)
	}
	return chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(300*time.Millisecond),
	)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func installVideoCaptureHook(ctx context.Context) error {
	const js = `(() => {
		if (!window.__doubaoVideoCapture) {
			window.__doubaoVideoCapture = { chunks: [], submitCount: 0, videoURLs: [], fallbackApis: [], vids: [] };
		}
		window.__doubaoVideoCapture.chunks = [];
		window.__doubaoVideoCapture.videoURLs = [];
		window.__doubaoVideoCapture.fallbackApis = [];
		window.__doubaoVideoCapture.vids = [];
		window.__doubaoVideoCapture.chunkBaseline = 0;
		window.__doubaoVideoCapture.videoURLBaseline = 0;

		// LauZzL/doubao-downloader style: force chat_ability duration (ability_type=17).
		function forceVideoDurationInValue(value, depth) {
			const want = Number(window.__doubaoForceVideoDuration) || 0;
			if (!want || want <= 0 || value == null || depth > 8) return false;
			let changed = false;
			if (typeof value === "object") {
				if (!Array.isArray(value)) {
					const ca = value.chat_ability;
					if (ca && Number(ca.ability_type) === 17) {
						try {
							let p = ca.ability_param;
							if (typeof p === "string") {
								p = JSON.parse(p || "{}");
								p.duration = want;
								ca.ability_param = JSON.stringify(p);
							} else if (p && typeof p === "object") {
								p.duration = want;
							} else {
								ca.ability_param = JSON.stringify({ duration: want });
							}
							changed = true;
							window.__doubaoForcedDurationApplied = want;
						} catch (_) {}
					}
					for (const k of Object.keys(value)) {
						if (forceVideoDurationInValue(value[k], (depth || 0) + 1)) changed = true;
					}
				} else {
					for (const item of value) {
						if (forceVideoDurationInValue(item, (depth || 0) + 1)) changed = true;
					}
				}
			}
			return changed;
		}
		function forceVideoDurationInText(text) {
			const want = Number(window.__doubaoForceVideoDuration) || 0;
			if (!want || want <= 0 || typeof text !== "string") return text;
			if (!/ability_type|ability_param|chat_ability/i.test(text)) return text;
			try {
				const obj = JSON.parse(text);
				if (forceVideoDurationInValue(obj, 0)) return JSON.stringify(obj);
			} catch (_) {}
			return text;
		}
		window.__doubaoForceVideoDurationInText = forceVideoDurationInText;
		window.__doubaoForceVideoDurationInValue = forceVideoDurationInValue;

		if (!window.__doubaoStringifyHooked) {
			const _stringify = JSON.stringify.bind(JSON);
			JSON.stringify = function(value, replacer, space) {
				try { forceVideoDurationInValue(value, 0); } catch (_) {}
				return _stringify(value, replacer, space);
			};
			window.__doubaoStringifyHooked = true;
		}

		function noteVideoURL(u) {
			if (!u || typeof u !== "string") return;
			u = u.replace(/\\u0026/g, "&").replace(/&amp;/g, "&").trim();
			if (!u || u.startsWith("blob:")) return;
			const path = u.split(/[?#]/)[0].toLowerCase();
			if (/\.(png|jpe?g|webp|gif|bmp)$/i.test(path)) return;
			if (!/\.(mp4|m3u8|webm|mov)(\?|#|$)/i.test(u) &&
				!/douyinvod|mime_type=video_mp4|video_mp4|\/video\/tos\//i.test(u) &&
				!/(?:^|\/\/)[^/]*douyin\.com\/[^"'\\s]*\/video\//i.test(u)) return;
			const cap = window.__doubaoVideoCapture;
			if (!cap.videoURLs) cap.videoURLs = [];
			if (cap.videoURLs.includes(u)) return;
			cap.videoURLs.push(u);
		}

		function noteFallbackAPI(u) {
			if (!u || typeof u !== "string" || !u.includes("/video/fplay/")) return;
			u = u.replace(/\\u0026/g, "&").replace(/&amp;/g, "&").replace(/\\\//g, "/").trim();
			if (!/^https?:\/\//.test(u)) return;
			const cap = window.__doubaoVideoCapture;
			if (!cap.fallbackApis) cap.fallbackApis = [];
			if (cap.fallbackApis.includes(u)) return;
			cap.fallbackApis.push(u);
		}

		function noteVid(v) {
			if (typeof v !== "string" || !/^v0[A-Za-z0-9_-]{8,}$/.test(v)) return;
			const cap = window.__doubaoVideoCapture;
			if (!cap.vids) cap.vids = [];
			if (cap.vids.includes(v)) return;
			cap.vids.push(v);
		}

		function noteVideoURLs(text) {
			if (!text || typeof text !== "string") return;
			if (/fallback_api|\/video\/fplay\//i.test(text)) {
				for (const m of text.matchAll(/https?:\/\/[^"'\\s<>]+\/video\/fplay\/[^"'\\s<>]+/g)) {
					noteFallbackAPI(m[0]);
				}
				for (const m of text.matchAll(/"fallback_api"\s*:\s*"((?:\\.|[^"\\])+)"/g)) {
					try { noteFallbackAPI(JSON.parse('"' + m[1].replace(/"/g, '\\"') + '"')); }
					catch (_) { noteFallbackAPI(m[1]); }
				}
			}
			if (/"vid"|"video_id"|video_url|douyinvod|mime_type=video_mp4|\/video\/tos\//i.test(text)) {
				for (const m of text.matchAll(/"(?:vid|video_id)"\s*:\s*"(v0[^"]+)"/g)) noteVid(m[1]);
			}
			if (!/video_url|douyinvod|mime_type=video_mp4|\/video\/tos\//i.test(text)) return;
			for (const m of text.matchAll(/https?:\/\/[^"'\\s<>]+/g)) {
				noteVideoURL(m[0]);
			}
			for (const m of text.matchAll(/"video_url"\s*:\s*"((?:\\.|[^"\\])+)"/g)) {
				noteVideoURL(m[1].replace(/\\"/g, '"'));
			}
		}

		function scanPerformance() {
			try {
				for (const e of performance.getEntriesByType("resource")) {
					noteVideoURL(e.name || "");
				}
			} catch (err) {}
		}

		function noteSubmit(url) {
			const u = String(url || "");
			if (u.includes("samantha") && (u.includes("completion") || u.includes("chat"))) {
				window.__doubaoVideoCapture.submitCount = (window.__doubaoVideoCapture.submitCount || 0) + 1;
			}
		}

		if (!window.__doubaoFetchHooked) {
			const origFetch = window.fetch.bind(window);
			window.fetch = async (...args) => {
				let input = args[0];
				let init = args[1];
				try {
					if (init && typeof init.body === "string" && /ability_param|chat_ability/i.test(init.body)) {
						const nextBody = forceVideoDurationInText(init.body);
						if (nextBody !== init.body) {
							init = Object.assign({}, init, { body: nextBody });
							args = [input, init];
						}
					}
				} catch (_) {}
				const url = String(input?.url || input || "");
				noteSubmit(url);
				noteVideoURL(url);
				const res = await origFetch(...args);
				try {
					// Include /im/chain history APIs — past chats load videos there, not via samantha.
					if ((url.includes("samantha") || url.includes("/im/") || /video|media|vod|thread_message|fallback/i.test(url)) && res.ok) {
						const ct = (res.headers && res.headers.get("content-type")) || "";
						if (/json|text|event-stream|javascript|octet/i.test(ct) || url.includes("samantha") || url.includes("/im/") || ct === "") {
							res.clone().text().then(body => {
								if (body && body.trim()) {
									if (url.includes("samantha") || url.includes("/im/")) {
										window.__doubaoVideoCapture.chunks.push(body);
									}
									noteVideoURLs(body);
								}
							}).catch(() => {});
						}
					}
				} catch (e) {}
				return res;
			};

			const origOpen = XMLHttpRequest.prototype.open;
			XMLHttpRequest.prototype.open = function(method, url, ...rest) {
				this.__doubaoUrl = String(url || "");
				return origOpen.call(this, method, url, ...rest);
			};
			const origSend = XMLHttpRequest.prototype.send;
			XMLHttpRequest.prototype.send = function(...args) {
				try {
					if (typeof args[0] === "string" && /ability_param|chat_ability/i.test(args[0])) {
						args[0] = forceVideoDurationInText(args[0]);
					}
				} catch (_) {}
				noteSubmit(this.__doubaoUrl);
				noteVideoURL(this.__doubaoUrl);
				this.addEventListener("load", () => {
					try {
						const url = this.__doubaoUrl || "";
						if (!this.responseText) return;
						// History videos: /im/chain/single carries fallback_api (Chrome extension path).
						if (url.includes("samantha") || url.includes("/im/") || /video|media|vod|thread_message/i.test(url)) {
							window.__doubaoVideoCapture.chunks.push(this.responseText);
						}
						noteVideoURLs(this.responseText);
					} catch (e) {}
				});
				return origSend.apply(this, args);
			};

			try {
				const po = new PerformanceObserver((list) => {
					for (const e of list.getEntries()) noteVideoURL(e.name || "");
				});
				po.observe({ type: "resource", buffered: true });
			} catch (err) {}

			window.__doubaoFetchHooked = true;
		}

		scanPerformance();
		return { ok: true };
	})()`
	var out map[string]any
	return evalReturnByValue(ctx, js, &out)
}

// setForcedVideoDuration installs / updates the LauZzL-style duration override
// (chat_ability.ability_type=17 → ability_param.duration).
func setForcedVideoDuration(ctx context.Context, durationSec int) error {
	durationSec = normalizeVideoDurationSec(durationSec)
	js := fmt.Sprintf(`(() => {
		window.__doubaoForceVideoDuration = %d;
		window.__doubaoForcedDurationApplied = 0;
		if (!window.__doubaoStringifyHooked) {
			const _stringify = JSON.stringify.bind(JSON);
			function forceVideoDurationInValue(value, depth) {
				const want = Number(window.__doubaoForceVideoDuration) || 0;
				if (!want || want <= 0 || value == null || depth > 8) return false;
				let changed = false;
				if (typeof value === "object") {
					if (!Array.isArray(value)) {
						const ca = value.chat_ability;
						if (ca && Number(ca.ability_type) === 17) {
							try {
								let p = ca.ability_param;
								if (typeof p === "string") {
									p = JSON.parse(p || "{}");
									p.duration = want;
									ca.ability_param = JSON.stringify(p);
								} else if (p && typeof p === "object") {
									p.duration = want;
								} else {
									ca.ability_param = JSON.stringify({ duration: want });
								}
								changed = true;
								window.__doubaoForcedDurationApplied = want;
							} catch (_) {}
						}
						for (const k of Object.keys(value)) {
							if (forceVideoDurationInValue(value[k], (depth || 0) + 1)) changed = true;
						}
					} else {
						for (const item of value) {
							if (forceVideoDurationInValue(item, (depth || 0) + 1)) changed = true;
						}
					}
				}
				return changed;
			}
			JSON.stringify = function(value, replacer, space) {
				try { forceVideoDurationInValue(value, 0); } catch (_) {}
				return _stringify(value, replacer, space);
			};
			window.__doubaoForceVideoDurationInValue = forceVideoDurationInValue;
			window.__doubaoStringifyHooked = true;
		}
		return { ok: true, duration: window.__doubaoForceVideoDuration };
	})()`, durationSec)
	var out struct {
		OK       bool `json:"ok"`
		Duration int  `json:"duration"`
	}
	if err := evalReturnByValue(ctx, js, &out); err != nil {
		return err
	}
	log.Printf("generate_video: force request duration=%ds (ability_param hook)", durationSec)
	return nil
}

// installCaptureHookOnNewDocument injects the video capture hook before any page script runs.
func installCaptureHookOnNewDocument(ctx context.Context) error {
	// Reuse the same IIFE as installVideoCaptureHook by evaluating a thin wrapper that
	// registers itself for future documents via a flag, then runs once.
	const src = `(() => {
		if (window.__doubaoHistoryHookQueued) return;
		window.__doubaoHistoryHookQueued = true;
		const boot = () => {
			try {
				if (!window.__doubaoVideoCapture) {
					window.__doubaoVideoCapture = { chunks: [], submitCount: 0, videoURLs: [], fallbackApis: [], vids: [] };
				}
				function noteFallbackAPI(u) {
					if (!u || typeof u !== "string" || !u.includes("/video/fplay/")) return;
					u = u.replace(/\\u0026/g, "&").replace(/&amp;/g, "&").replace(/\\\//g, "/").trim();
					if (!/^https?:\/\//.test(u)) return;
					const cap = window.__doubaoVideoCapture;
					if (!cap.fallbackApis) cap.fallbackApis = [];
					if (!cap.fallbackApis.includes(u)) cap.fallbackApis.push(u);
				}
				function noteVid(v) {
					if (typeof v !== "string" || !/^v0[A-Za-z0-9_-]{8,}$/.test(v)) return;
					const cap = window.__doubaoVideoCapture;
					if (!cap.vids) cap.vids = [];
					if (!cap.vids.includes(v)) cap.vids.push(v);
				}
				function noteVideoURL(u) {
					if (!u || typeof u !== "string") return;
					u = u.replace(/\\u0026/g, "&").replace(/&amp;/g, "&").trim();
					if (!u || u.startsWith("blob:")) return;
					if (!/\.(mp4|m3u8|webm|mov)(\?|#|$)/i.test(u) &&
						!/douyinvod|mime_type=video_mp4|video_mp4|\/video\/tos\//i.test(u)) return;
					const cap = window.__doubaoVideoCapture;
					if (!cap.videoURLs) cap.videoURLs = [];
					if (!cap.videoURLs.includes(u)) cap.videoURLs.push(u);
				}
				function noteVideoURLs(text) {
					if (!text || typeof text !== "string") return;
					for (const m of text.matchAll(/https?:\/\/[^"'\\\s<>]+\/video\/fplay\/[^"'\\\s<>]+/g)) noteFallbackAPI(m[0]);
					for (const m of text.matchAll(/"fallback_api"\s*:\s*"((?:\\.|[^"\\])+)"/g)) {
						try { noteFallbackAPI(JSON.parse('"' + m[1].replace(/"/g, '\\"') + '"')); }
						catch (_) { noteFallbackAPI(m[1]); }
					}
					for (const m of text.matchAll(/"(?:vid|video_id)"\s*:\s*"(v0[^"]+)"/g)) noteVid(m[1]);
					for (const m of text.matchAll(/https?:\/\/[^"'\\\s<>]+/g)) noteVideoURL(m[0]);
				}
				if (!window.__doubaoFetchHooked) {
					const origFetch = window.fetch.bind(window);
					window.fetch = async (...args) => {
						const url = String(args[0]?.url || args[0] || "");
						noteVideoURL(url);
						const res = await origFetch(...args);
						try {
							if ((url.includes("samantha") || url.includes("/im/") || /video|media|vod|thread_message/i.test(url)) && res.ok) {
								res.clone().text().then(body => {
									if (body && body.trim()) {
										window.__doubaoVideoCapture.chunks.push(body);
										noteVideoURLs(body);
									}
								}).catch(() => {});
							}
						} catch (_) {}
						return res;
					};
					const origOpen = XMLHttpRequest.prototype.open;
					XMLHttpRequest.prototype.open = function(method, url, ...rest) {
						this.__doubaoUrl = String(url || "");
						return origOpen.call(this, method, url, ...rest);
					};
					const origSend = XMLHttpRequest.prototype.send;
					XMLHttpRequest.prototype.send = function(...args) {
						this.addEventListener("load", () => {
							try {
								const url = this.__doubaoUrl || "";
								if (this.responseText) {
									if (url.includes("samantha") || url.includes("/im/") || /video|media|vod/i.test(url)) {
										window.__doubaoVideoCapture.chunks.push(this.responseText);
									}
									noteVideoURLs(this.responseText);
								}
							} catch (_) {}
						});
						return origSend.apply(this, args);
					};
					window.__doubaoFetchHooked = true;
				}
				// Also hook WebSocket text frames — history messages may arrive via maigc WS.
				if (!window.__doubaoWSHooked && window.WebSocket) {
					const OrigWS = window.WebSocket;
					window.WebSocket = function(url, protocols) {
						const ws = protocols !== undefined ? new OrigWS(url, protocols) : new OrigWS(url);
						ws.addEventListener('message', (ev) => {
							try {
								const data = ev && ev.data;
								if (typeof data === 'string' && data.length) {
									window.__doubaoVideoCapture.chunks.push(data);
									noteVideoURLs(data);
								} else if (data && data.byteLength && data.byteLength < 2_000_000) {
									try {
										const text = new TextDecoder().decode(data);
										if (text && /fplay|fallback_api|video_url|"vid"/.test(text)) {
											window.__doubaoVideoCapture.chunks.push(text);
											noteVideoURLs(text);
										}
									} catch (_) {}
								}
							} catch (_) {}
						});
						return ws;
					};
					window.WebSocket.prototype = OrigWS.prototype;
					window.__doubaoWSHooked = true;
				}
			} catch (_) {}
		};
		boot();
		document.addEventListener("DOMContentLoaded", boot);
	})();`
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(src).Do(ctx)
		return err
	}))
}

func officeTaskModeActive(ctx context.Context) bool {
	var active bool
	_ = evalReturnByValue(ctx, `(() => {
		for (const el of document.querySelectorAll('button, [role="button"], div, span')) {
			if (!el.offsetParent) continue;
			const t = (el.textContent || '').trim();
			if (!/办公任务|办公模式/.test(t)) continue;
			const cls = String(el.className || '') + String(el.parentElement?.className || '');
			if (/active|selected|checked|current|turbo/i.test(cls)) return true;
		}
		const ph = document.querySelector('textarea, [contenteditable]:not([contenteditable="false"]), [role="textbox"]');
		const hint = (ph?.getAttribute('placeholder') || ph?.getAttribute('data-placeholder') || '').trim();
		return /办公|发消息或按住空格说话/.test(hint);
	})()`, &active)
	return active
}

func ensureOfficeTaskMode(ctx context.Context) error {
	if officeTaskModeActive(ctx) {
		return nil
	}
	if err := clickFirstLabel(ctx, "办公任务 Turbo", "办公任务", "办公模式"); err != nil {
		return fmt.Errorf("enable office task mode: %w", err)
	}
	return chromedp.Run(ctx, chromedp.Sleep(800*time.Millisecond))
}

func ensureVideoSkillMode(ctx context.Context) error {
	// After a completed video conversation, Doubao SPA often leaves suggestion
	// cards / sticky skill UI that steal 「视频生成」clicks. Prefer precise
	// toolbar click, then "/" skill menu, then hard refresh.
	const maxAttempts = 5
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if _, err := waitForHumanVerification(ctx); err != nil {
			return err
		}
		if err := waitForModelChipButton(ctx, 2*time.Second); err == nil {
			return nil
		}
		switch {
		case attempt == 1:
			// already on fresh chat from ensureNewSession
		case attempt == 2:
			log.Printf("generate_video: settle then retry skill entry (attempt %d)", attempt)
			if _, err := waitForHumanVerification(ctx); err != nil {
				return err
			}
			_ = dismissDoubaoPopups(ctx)
			if err := chromedp.Run(ctx, chromedp.Sleep(1500*time.Millisecond)); err != nil {
				return err
			}
		case attempt == 3:
			log.Printf("generate_video: hard refresh after %d failures, then retry", attempt-1)
			if err := hardRefreshChatPage(ctx); err != nil {
				log.Printf("generate_video: hard refresh: %v", err)
			}
			if err := settleFreshChatBeforeVideoSkill(ctx); err != nil {
				return err
			}
		default:
			log.Printf("generate_video: reset chat page before video skill retry %d", attempt)
			if err := resetToFreshChat(ctx); err != nil {
				log.Printf("generate_video: reset chat page: %v", err)
			}
			if err := settleFreshChatBeforeVideoSkill(ctx); err != nil {
				return err
			}
		}
		_ = focusChatEditor(ctx)
		if err := chromedp.Run(ctx, chromedp.Sleep(600*time.Millisecond)); err != nil {
			return err
		}

		if err := clickVideoSkillButton(ctx); err != nil {
			log.Printf("generate_video: video skill chip click attempt %d: %v", attempt, err)
			lastErr = fmt.Errorf("enable video skill: %w", err)
		} else {
			log.Printf("generate_video: clicked 视频生成 (attempt %d)", attempt)
			if err := chromedp.Run(ctx, chromedp.Sleep(1200*time.Millisecond)); err != nil {
				return err
			}
			if err := waitForModelChipButton(ctx, 8*time.Second); err == nil {
				return nil
			}
			lastErr = err
			log.Printf("generate_video: model chip not ready after 视频生成 (attempt %d)", attempt)
			logVideoToolbarDiagnostics(ctx, fmt.Sprintf("after chip click attempt %d", attempt))
		}

		// Chip bar may be empty (placeholder: 输入“/”选择技能) or click hit noise.
		if err := enterVideoSkillViaSlash(ctx); err != nil {
			log.Printf("generate_video: / skill entry attempt %d: %v", attempt, err)
			if lastErr == nil {
				lastErr = err
			}
		} else if err := chromedp.Run(ctx, chromedp.Sleep(1200*time.Millisecond)); err != nil {
			return err
		} else if err := waitForModelChipButton(ctx, 10*time.Second); err == nil {
			return nil
		} else {
			lastErr = err
			log.Printf("generate_video: model chip not ready after / skill (attempt %d)", attempt)
			logVideoToolbarDiagnostics(ctx, fmt.Sprintf("after slash attempt %d", attempt))
		}

		if attempt < maxAttempts {
			if _, err := waitForHumanVerification(ctx); err != nil {
				return err
			}
			_ = dismissDoubaoPopups(ctx)
			if err := chromedp.Run(ctx, chromedp.Sleep(800*time.Millisecond)); err != nil {
				return err
			}
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("model chip button not found in input toolbar")
}

// settleFreshChatBeforeVideoSkill waits for the blank-chat skill bar to become
// interactive. Needed after leaving a finished video conversation — immediate
// clicks often no-op until the SPA finishes tearing down the previous skill UI.
func settleFreshChatBeforeVideoSkill(ctx context.Context) error {
	log.Printf("generate_video: settle fresh chat before 视频生成")
	if _, err := waitForHumanVerification(ctx); err != nil {
		return err
	}
	_ = dismissDoubaoPopups(ctx)
	if err := chromedp.Run(ctx, chromedp.Sleep(2*time.Second)); err != nil {
		return err
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var ready bool
		js := videoToolbarJSShared + `(() => {
			const editors = [...document.querySelectorAll('textarea, [contenteditable]:not([contenteditable="false"]), [role="textbox"]')].filter(isVisible);
			const ed = editors.length ? editors[editors.length - 1] : null;
			const ph = ed ? (ed.getAttribute('placeholder') || ed.getAttribute('data-placeholder') || '') : '';
			// Slash skill picker UI is also a valid ready state.
			if (/输入.*\/.*技能|选择技能/.test(ph)) return true;
			const vh = window.innerHeight;
			for (const el of document.querySelectorAll('button, [role="button"], div, span')) {
				if (!isVisible(el)) continue;
				const t = (el.textContent || '').trim().replace(/\s+/g, ' ');
				if (t !== '视频生成' && t !== '快速视频生成') continue;
				const r = el.getBoundingClientRect();
				if (r.top > vh * 0.45 && r.width >= 40 && r.width <= 220) return true;
			}
			return !!ed;
		})()`
		if err := evalReturnByValue(ctx, js, &ready); err != nil {
			return err
		}
		if ready {
			return nil
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(400*time.Millisecond)); err != nil {
			return err
		}
	}
	log.Printf("generate_video: composer not stable yet, continuing anyway")
	return nil
}

func logVideoToolbarDiagnostics(ctx context.Context, where string) {
	var out struct {
		URL         string   `json:"url"`
		Placeholder string   `json:"placeholder"`
		ExactSkills []string `json:"exactSkills"`
		Chips       []string `json:"chips"`
		SkillActive bool     `json:"skillActive"`
	}
	js := videoToolbarJSShared + `(() => {
		const editors = [...document.querySelectorAll('textarea, [contenteditable]:not([contenteditable="false"]), [role="textbox"]')].filter(isVisible);
		const ed = editors.length ? editors[editors.length - 1] : null;
		const ph = ed ? (ed.getAttribute('placeholder') || ed.getAttribute('data-placeholder') || '') : '';
		const vh = window.innerHeight;
		const exactSkills = [];
		const chips = [];
		for (const el of document.querySelectorAll('button, [role="button"], div, span, a')) {
			if (!isVisible(el)) continue;
			const r = el.getBoundingClientRect();
			if (r.top < vh * 0.4) continue;
			const t = (el.textContent || '').trim().replace(/\s+/g, ' ');
			if (!t || t.length > 48) continue;
			if (/^(快速)?视频生成$/.test(t)) {
				exactSkills.push(t + "@" + Math.round(r.left) + "," + Math.round(r.top) + " " + Math.round(r.width) + "x" + Math.round(r.height));
			}
			if (/模型|视频生成|比例|\d+s|Fast|Mini|Seedance|选择技能|自动/i.test(t) && t.length <= 40) {
				chips.push(t.slice(0, 40));
			}
			if (exactSkills.length >= 8 && chips.length >= 10) break;
		}
		let skillActive = !!findModelChipButton();
		if (!skillActive) {
			for (const el of document.querySelectorAll('button, [role="button"], div, span')) {
				if (!isVisible(el)) continue;
				const t = (el.textContent || '').trim().replace(/\s+/g, ' ');
				if (!/^(快速)?视频生成$/.test(t)) continue;
				const cls = String(el.className || '') + ' ' + String(el.parentElement && el.parentElement.className || '');
				if (/active|selected|checked|current/i.test(cls)) { skillActive = true; break; }
			}
		}
		return { url: location.href, placeholder: ph.slice(0, 60), exactSkills, chips: chips.slice(0, 10), skillActive };
	})()`
	if err := evalReturnByValue(ctx, js, &out); err != nil {
		log.Printf("generate_video: toolbar diag (%s): %v", where, err)
		return
	}
	log.Printf("generate_video: toolbar diag (%s): url=%s skillActive=%v placeholder=%q exact=%v chips=%v",
		where, out.URL, out.SkillActive, out.Placeholder, out.ExactSkills, out.Chips)
}

// hardRefreshChatPage force-reloads the chat page to clear stuck SPA UI state,
// then ensures a fresh chat and reinstalls the video capture hook.
func hardRefreshChatPage(ctx context.Context) error {
	if err := chromedp.Run(ctx,
		chromedp.Reload(),
		chromedp.Sleep(1500*time.Millisecond),
	); err != nil {
		log.Printf("generate_video: Reload failed (%v), navigate to fresh chat", err)
		if err := resetToFreshChat(ctx); err != nil {
			return err
		}
	} else if id := currentChatConversationID(ctx); id != "" {
		log.Printf("generate_video: still on conversation %s after reload, navigating to fresh chat", id)
		if err := resetToFreshChat(ctx); err != nil {
			return err
		}
	} else if err := waitForNewChatReady(ctx, 20*time.Second); err != nil {
		log.Printf("generate_video: chat not ready after reload (%v), navigating to fresh chat", err)
		if err := resetToFreshChat(ctx); err != nil {
			return err
		}
	}
	if err := installVideoCaptureHook(ctx); err != nil {
		log.Printf("generate_video: reinstall capture hook after refresh: %v", err)
	}
	return dismissDoubaoPopups(ctx)
}

func clickVideoSkillButton(ctx context.Context) error {
	var pt clickPoint
	js := videoToolbarJSShared + `(() => {
		const vh = window.innerHeight;
		const skillRe = /^(快速)?视频生成$/;
		function labelOf(el) {
			return (el.textContent || '').trim().replace(/\s+/g, ' ');
		}
		function nearEditor(el) {
			const editors = [...document.querySelectorAll('textarea, [contenteditable]:not([contenteditable="false"]), [role="textbox"]')].filter(isVisible);
			const ed = editors.length ? editors[editors.length - 1] : null;
			if (!ed) return true;
			const er = ed.getBoundingClientRect();
			const r = el.getBoundingClientRect();
			// Skill chips sit in the composer row around the editor, not in history cards.
			if (Math.abs(r.bottom - er.top) > 220 && Math.abs(r.top - er.bottom) > 120) return false;
			if (r.bottom < er.top - 80) return false;
			return true;
		}
		function rejectNoise(el) {
			let n = el;
			for (let i = 0; i < 8 && n; i++) {
				const cls = String(n.className || '');
				const t = labelOf(n);
				// Conversation / suggestion cards often embed the words 视频生成.
				if (/sidebar|history|conversation|suggest|recommend|recent|session/i.test(cls)) return true;
				if (t.length > 16 && t !== '视频生成' && t !== '快速视频生成' && /视频生成/.test(t)) return true;
				n = n.parentElement;
			}
			return false;
		}
		function clickTarget(el) {
			// Prefer the tightest ancestor that still has the exact skill label.
			let best = el;
			let node = el;
			for (let i = 0; i < 5 && node; i++) {
				const t = labelOf(node);
				if (!skillRe.test(t)) break;
				best = node;
				if (node.tagName === 'BUTTON' || node.getAttribute('role') === 'button') break;
				node = node.parentElement;
			}
			return best;
		}
		const candidates = [];
		const scopes = [];
		const root = videoToolbarRoot();
		if (root) scopes.push(root);
		for (const list of document.querySelectorAll('[class*="overflow-list"]')) {
			if (!isVisible(list)) continue;
			if (list.getBoundingClientRect().top < vh * 0.5) continue;
			scopes.push(list);
		}
		scopes.push(document);
		const seen = new Set();
		for (const scope of scopes) {
			for (const el of scope.querySelectorAll('button, [role="button"], div, span, a')) {
				if (!isVisible(el)) continue;
				const t = labelOf(el);
				if (!skillRe.test(t)) continue;
				if (rejectNoise(el)) continue;
				const pick = clickTarget(el);
				if (seen.has(pick)) continue;
				const r = pick.getBoundingClientRect();
				if (r.top < vh * 0.45) continue;
				if (r.width < 40 || r.width > 220 || r.height < 18 || r.height > 64) continue;
				if (!nearEditor(pick)) continue;
				seen.add(pick);
				// Prefer tight exact chips near the bottom editor.
				const score = (800 - r.top) + (200 - Math.abs(r.width - 90)) + (t === '视频生成' ? 50 : 0);
				candidates.push({ el: pick, text: t, score, w: r.width, h: r.height });
			}
			if (candidates.length) break;
		}
		if (!candidates.length) return { found: false, error: "视频生成 button not found in input toolbar" };
		candidates.sort((a, b) => b.score - a.score);
		const pick = candidates[0].el;
		pick.scrollIntoView({ block: 'nearest', inline: 'center' });
		try { pick.click(); } catch (e) {}
		const r = pick.getBoundingClientRect();
		return {
			found: true,
			x: r.left + r.width / 2,
			y: r.top + r.height / 2,
			text: labelOf(pick),
			error: candidates.length > 1 ? ("alts=" + candidates.slice(0, 3).map(c => c.text + "@" + Math.round(c.w) + "x" + Math.round(c.h)).join(",")) : ""
		};
	})()`
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return err
	}
	if !pt.Found {
		if pt.Error != "" {
			return errors.New(pt.Error)
		}
		return fmt.Errorf("视频生成 button not found")
	}
	log.Printf("generate_video: js-click 视频生成 %q at (%.0f, %.0f) %s", pt.Text, pt.X, pt.Y, pt.Error)
	// JS click above; mouse click as fallback for stubborn React handlers.
	_ = chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(400*time.Millisecond),
	)
	return nil
}

// enterVideoSkillViaSlash uses the "/" skill picker (shown when chip bar is empty
// or placeholder says 输入“/”选择技能).
func enterVideoSkillViaSlash(ctx context.Context) error {
	log.Printf("generate_video: try enter video skill via / menu")
	if err := focusChatEditor(ctx); err != nil {
		return err
	}
	if err := typeIntoFocused(ctx, "/"); err != nil {
		return err
	}
	if err := chromedp.Run(ctx, chromedp.Sleep(800*time.Millisecond)); err != nil {
		return err
	}
	var pt clickPoint
	js := videoToolbarJSShared + `(() => {
		const skillRe = /^(快速)?视频生成$/;
		const candidates = [];
		for (const el of document.querySelectorAll('[role="option"], [role="menuitem"], li, button, [role="button"], div, span')) {
			if (!isVisible(el)) continue;
			const t = (el.textContent || '').trim().replace(/\s+/g, ' ');
			if (!skillRe.test(t) && t !== '视频生成') continue;
			// Menu rows can be wider than toolbar chips.
			const r = el.getBoundingClientRect();
			if (r.width < 40 || r.height < 18 || r.height > 80) continue;
			if (t.length > 12 && !skillRe.test(t)) continue;
			candidates.push({ el, t, top: r.top, w: r.width });
		}
		// Also allow rows that start with 视频生成 (icon + label).
		if (!candidates.length) {
			for (const el of document.querySelectorAll('[role="option"], [role="menuitem"], li, button, div')) {
				if (!isVisible(el)) continue;
				const t = (el.textContent || '').trim().replace(/\s+/g, ' ');
				if (!/^视频生成\b/.test(t) && !/^快速视频生成\b/.test(t)) continue;
				if (t.length > 24) continue;
				const r = el.getBoundingClientRect();
				if (r.width < 40 || r.height < 18) continue;
				candidates.push({ el, t: t.slice(0, 20), top: r.top, w: r.width });
			}
		}
		if (!candidates.length) return { found: false, error: "/ skill menu item 视频生成 not found" };
		candidates.sort((a, b) => a.t.length - b.t.length || a.top - b.top);
		const pick = candidates[0].el;
		try { pick.click(); } catch (e) {}
		const r = pick.getBoundingClientRect();
		return { found: true, x: r.left + r.width / 2, y: r.top + r.height / 2, text: candidates[0].t };
	})()`
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return err
	}
	if !pt.Found {
		// Clear the "/" so we don't pollute the next attempt.
		_ = chromedp.Run(ctx, chromedp.KeyEvent("\u001b")) // Escape
		if pt.Error != "" {
			return errors.New(pt.Error)
		}
		return fmt.Errorf("/ skill menu item not found")
	}
	log.Printf("generate_video: js-click / skill %q at (%.0f, %.0f)", pt.Text, pt.X, pt.Y)
	_ = chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(500*time.Millisecond),
	)
	return nil
}

// normalizeVideoUIModel maps API model names to doubao UI variants: fast | mini.
func normalizeVideoUIModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return DefaultVideoUIModel
	}
	if strings.Contains(m, "mini") {
		return "mini"
	}
	return "fast"
}

func videoModelUILabel(variant string) string {
	if variant == "mini" {
		return "Seedance 2.0 Mini"
	}
	return "Seedance 2.0 Fast"
}

// videoToolbarJSShared contains helpers scoped to the bottom video input chip bar.
// Doubao chip label drift (2026-07): 「模型 2.0 Fast」→「模型 Seedance 2.0 Fast」,
// duration chip often 「自动 · 10s」 instead of bare 「10s」.
const videoToolbarJSShared = `
	function isVisible(el) {
		// Do not treat ancestor aria-hidden as invisible: slash/upload popovers
		// often mark the composer tree that way while it is still on screen.
		if (!el || el.hasAttribute('hidden') || el.getAttribute('aria-hidden') === 'true') return false;
		if (el.closest('[hidden]')) return false;
		const st = window.getComputedStyle(el);
		if (st.display === 'none' || st.visibility === 'hidden' || parseFloat(st.opacity) === 0) return false;
		const r = el.getBoundingClientRect();
		return r.width > 4 && r.height > 4;
	}
	// Accept both legacy 「模型 2.0 Fast」 and current 「模型 Seedance 2.0 Fast」.
	// Use var (not const): chromedp Runtime.evaluate reuses the page lexical
	// environment, so top-level const throws "already been declared" on retry.
	var modelChipRe = /^模型\s*(?:Seedance\s+)?2\.0\s*(Fast|Mini)\b/i;
	var durationChipRe = /^(?:自动\s*[^\d\s]{0,3}\s*)?(\d+)\s*s$/i;
	function videoToolbarRoot() {
		const editors = [...document.querySelectorAll('textarea, [contenteditable]:not([contenteditable="false"]), [role="textbox"]')].filter(isVisible);
		const editor = editors[editors.length - 1];
		let node = editor;
		while (node && node !== document.body) {
			if (node.querySelector) {
				if (node.querySelector('[class*="overflow-list"]')) return node;
				for (const btn of node.querySelectorAll('button, [role="button"]')) {
					const t = (btn.textContent || '').trim().replace(/\s+/g, ' ');
					if (modelChipRe.test(t)) return node;
				}
			}
			node = node.parentElement;
		}
		for (const list of document.querySelectorAll('[class*="overflow-list"]')) {
			if (!isVisible(list)) continue;
			const r = list.getBoundingClientRect();
			if (r.top > window.innerHeight * 0.55) {
				return list.closest('[class*="input-content"], [class*="input-guidance"], [class*="input-container"]') || list;
			}
		}
		return null;
	}
	function findModelChipButton() {
		const scopes = [];
		const root = videoToolbarRoot();
		if (root) scopes.push(root);
		scopes.push(document);
		const vh = window.innerHeight;
		let best = null;
		for (const scope of scopes) {
			for (const el of scope.querySelectorAll('button, [role="button"], div, span')) {
				if (!isVisible(el)) continue;
				const r = el.getBoundingClientRect();
				if (r.top < vh * 0.45) continue;
				// Seedance label is wider than legacy 「模型 2.0 Fast」.
				if (r.width < 60 || r.width > 360 || r.height > 56) continue;
				const t = (el.textContent || '').trim().replace(/\s+/g, ' ');
				if (!modelChipRe.test(t)) continue;
				// Prefer the tightest clickable ancestor that still matches.
				let pick = el;
				if (el.tagName !== 'BUTTON' && el.getAttribute('role') !== 'button') {
					let n = el.parentElement;
					for (let i = 0; i < 4 && n; i++) {
						const nt = (n.textContent || '').trim().replace(/\s+/g, ' ');
						if (!modelChipRe.test(nt)) break;
						pick = n;
						if (n.tagName === 'BUTTON' || n.getAttribute('role') === 'button') break;
						n = n.parentElement;
					}
				}
				const pr = pick.getBoundingClientRect();
				if (pr.width > 360 || pr.height > 56) continue;
				if (!best || t.length <= best.len) best = { el: pick, len: t.length, text: t };
			}
			if (best) break;
		}
		return best;
	}
	function isToolbarChipText(t) {
		return modelChipRe.test(t) || durationChipRe.test(t) || /^\d+\s*秒$/.test(t) || /^比例$/i.test(t) || t === '视频生成';
	}
	function isModelMenuOpen() {
		for (const el of document.querySelectorAll('div, li, button, span, [role="menuitem"], [role="option"]')) {
			if (!isVisible(el)) continue;
			const t = (el.innerText || '').replace(/\s+/g, ' ').trim();
			if (!t || t.length > 200 || isToolbarChipText(t)) continue;
			if (!/2\.0\s*(Fast|Mini)|Seedance\s*2\.0/i.test(t)) continue;
			const r = el.getBoundingClientRect();
			if (r.width > 50 && r.height > 20) return true;
		}
		return false;
	}
	// Bottom composer editor rect. Marker can vanish after React re-renders
	// (image attach / long prompt); fall back to the visible bottom textarea.
	function composerEditorRect() {
		const marked = document.querySelector('[data-doubao-chat-editor="1"]');
		if (marked && isVisible(marked)) return marked.getBoundingClientRect();
		const vh = window.innerHeight;
		const sels = [
			'textarea.semi-input-textarea',
			'textarea[placeholder*="发消息"]',
			'textarea[placeholder*="描述"]',
			'.tiptap.ProseMirror',
			'.ProseMirror[contenteditable]',
			'[data-placeholder*="发消息"]',
			'[contenteditable="true"]',
			'[contenteditable="plaintext-only"]',
			'textarea',
			'[role="textbox"]',
		];
		for (const sel of sels) {
			for (const el of document.querySelectorAll(sel)) {
				if (!isVisible(el)) continue;
				const r = el.getBoundingClientRect();
				if (r.width < 160 || r.height < 12) continue;
				if (r.bottom < vh * 0.38) continue;
				if (el.tagName === 'P') continue;
				return r;
			}
		}
		return null;
	}
	function isVoiceWaveformPath(pathD) {
		// Empty-composer mic control uses 3 vertical bars; never treat as send.
		return /M15\.064[\s\S]*M8\.937[\s\S]*M2\.809|v16\.026[\s\S]*v9\.636[\s\S]*v6\.052/.test(pathD || "");
	}
	function isSendArrowPath(pathD) {
		// Enabled send uses an upward chevron/arrow (see Doubao 2026-08 UI).
		return /M4\.93934|10\.2598|2\.84763|L10\.2304|13\.7675/.test(pathD || "");
	}
	function isHighlightSendButton(el) {
		const cls = String(el.className || "");
		if (/bg-dbx-text-highlight|bg-dbx-fill-highlight/.test(cls)) return true;
		const bg = window.getComputedStyle(el).backgroundColor || "";
		const m = bg.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/);
		if (!m) return false;
		const r = +m[1], g = +m[2], b = +m[3];
		// Brand blue send, e.g. rgb(0, 102, 255).
		return b >= 180 && b > r + 40 && b > g + 40;
	}
	// Score the bottom-composer send control. Must never pick the Chrome
	// extension 「无水印下载」button (fixed bottom-right) — when the real send
	// control is disabled (images still uploading), position-only scoring used
	// to prefer that extension button and leave the composer filled.
	function scoreComposerSubmitButton(el, editorRect, root) {
		const vh = window.innerHeight;
		const vw = window.innerWidth;
		const aria = (el.getAttribute("aria-label") || "") + " " + (el.getAttribute("title") || "");
		const text = (el.textContent || "").trim().replace(/\s+/g, " ");
		const r = el.getBoundingClientRect();
		if (r.width <= 0 || r.height <= 0) return -1;
		if (el.id === "doubao-clean-dl-btn" || el.closest("#doubao-clean-dl-root")) return -1;
		if (/无水印|下载电脑版|开通加强|开通豆包/.test(text) || /无水印|下载电脑版/.test(aria)) return -1;
		if (isToolbarChipText(text) || /^(快速|帮我写作|PPT\s*生成|图像生成|解题答疑|音乐生成|深入研究|录音转写|翻译|更多)$/.test(text)) return -1;
		// Ignore header / floating chrome — submit lives in the bottom composer bar.
		if (r.top < vh * 0.55) return -1;
		if (r.bottom < vh - 220) return -1;
		// Disabled send must not compete with other bottom-right controls.
		if (el.disabled || el.getAttribute("aria-disabled") === "true") return -1;

		const pathD = [...el.querySelectorAll("path")].map(p => p.getAttribute("d") || "").join(" ");
		if (isVoiceWaveformPath(pathD)) return -1;

		const sendLike = /发送|Send|提交/i.test(aria) || text === "发送" || text === "Send" ||
			isSendArrowPath(pathD) || isHighlightSendButton(el);
		let s = 0;
		if (/发送|Send|提交/i.test(aria)) s += 200;
		if (text === "发送" || text === "Send") s += 180;
		if (isSendArrowPath(pathD)) s += 220;
		if (isHighlightSendButton(el)) s += 200;
		if (el.querySelector("svg")) s += 40;
		if (r.width >= 28 && r.width <= 72 && r.height >= 28 && r.height <= 72) s += 30;
		if (editorRect && r.left >= editorRect.right - 120) s += 100;
		if (r.left > vw * 0.72) s += 60;
		if (r.bottom > vh - 90) s += 50;
		if (root && root.contains(el)) s += 80;
		if (!sendLike) {
			// Icon-only send next to the editor is OK; pure position guesses are not.
			const iconNearEditor = !!el.querySelector("svg") && !!editorRect &&
				r.left >= editorRect.right - 80 && r.width <= 56 && r.height <= 56;
			if (!iconNearEditor) return -1;
			s += 20;
		}
		return s;
	}
`

// readCurrentVideoUIModel reads the model selector button only (not chat history).
func readCurrentVideoUIModel(ctx context.Context) (string, error) {
	var out struct {
		Found   bool   `json:"found"`
		Variant string `json:"variant"`
	}
	const js = videoToolbarJSShared + `(() => {
		const chip = findModelChipButton();
		if (!chip) return { found: false, variant: "" };
		const m = chip.text.match(modelChipRe);
		if (!m) return { found: false, variant: "" };
		return { found: true, variant: m[1].toLowerCase() === "mini" ? "mini" : "fast" };
	})()`
	if err := evalReturnByValue(ctx, js, &out); err != nil {
		return "", err
	}
	if !out.Found {
		return "", nil
	}
	return out.Variant, nil
}

func waitForModelChipButton(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var out struct {
			Found bool   `json:"found"`
			Text  string `json:"text"`
		}
		js := videoToolbarJSShared + `(() => {
			const chip = findModelChipButton();
			if (!chip) return { found: false, text: "" };
			return { found: true, text: chip.text };
		})()`
		if err := evalReturnByValue(ctx, js, &out); err != nil {
			return err
		}
		if out.Found {
			log.Printf("generate_video: model chip ready: %q", out.Text)
			return nil
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(400*time.Millisecond)); err != nil {
			return err
		}
	}
	return fmt.Errorf("model chip button not found in input toolbar")
}

func ensureVideoModel(ctx context.Context, model string) error {
	want := normalizeVideoUIModel(model)
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if _, err := waitForHumanVerification(ctx); err != nil {
			return err
		}
		if err := waitForModelChipButton(ctx, 8*time.Second); err != nil {
			lastErr = err
			log.Printf("generate_video: wait model chip attempt %d: %v", attempt, err)
			if attempt < maxAttempts {
				if err := ensureVideoSkillMode(ctx); err != nil {
					log.Printf("generate_video: re-enter video skill: %v", err)
				}
			}
			continue
		}
		current, err := readCurrentVideoUIModel(ctx)
		if err != nil {
			log.Printf("generate_video: read current model: %v", err)
		}
		if current == want {
			log.Printf("generate_video: model already %s", want)
			return nil
		}
		if current != "" {
			log.Printf("generate_video: switch model %s -> %s", current, want)
		} else {
			log.Printf("generate_video: select model %s", want)
		}
		if err := selectVideoModelOnce(ctx, want); err != nil {
			lastErr = err
			log.Printf("generate_video: select model attempt %d: %v", attempt, err)
			if attempt < maxAttempts {
				_ = dismissDoubaoPopups(ctx)
				if err := chromedp.Run(ctx, chromedp.Sleep(800*time.Millisecond)); err != nil {
					return err
				}
			}
			continue
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("model option not found: %s", videoModelUILabel(want))
}

func selectVideoModelOnce(ctx context.Context, want string) error {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := waitForHumanVerification(ctx); err != nil {
			return err
		}
		if err := clickModelSelector(ctx); err != nil {
			return fmt.Errorf("open model menu: %w", err)
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(1300*time.Millisecond)); err != nil {
			return err
		}
		if _, err := waitForHumanVerification(ctx); err != nil {
			return err
		}
		open, err := isVideoModelMenuOpen(ctx)
		if err != nil {
			log.Printf("generate_video: check model menu: %v", err)
		}
		if !open {
			lastErr = fmt.Errorf("model menu did not open")
			log.Printf("generate_video: model menu not open after click (attempt %d)", attempt+1)
			continue
		}
		if err := clickVideoModelOption(ctx, want); err != nil {
			lastErr = err
			log.Printf("generate_video: pick model option attempt %d: %v", attempt+1, err)
			continue
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(700*time.Millisecond)); err != nil {
			return err
		}
		after, err := readCurrentVideoUIModel(ctx)
		if err != nil {
			log.Printf("generate_video: read model after select: %v", err)
		}
		if after == want {
			log.Printf("generate_video: model set to %s", want)
			return nil
		}
		lastErr = fmt.Errorf("model still %q, want %q", after, want)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("model option not found: %s", videoModelUILabel(want))
}

// ensureVideoDuration selects the toolbar duration chip (5s/10s/15s) and keeps
// the LauZzL-style request hook in sync.
func ensureVideoDuration(ctx context.Context, durationSec int) error {
	want := normalizeVideoDurationSec(durationSec)
	if err := setForcedVideoDuration(ctx, want); err != nil {
		log.Printf("generate_video: set force duration hook: %v", err)
	}
	current, err := readCurrentVideoDuration(ctx)
	if err != nil {
		log.Printf("generate_video: read duration chip: %v", err)
	}
	if current == want {
		log.Printf("generate_video: duration chip already %ds", want)
		return nil
	}
	if current > 0 {
		log.Printf("generate_video: switch duration %ds -> %ds", current, want)
	} else {
		log.Printf("generate_video: select duration %ds", want)
	}
	if err := selectVideoDurationOnce(ctx, want); err != nil {
		// Chip may be missing on some layouts; request hook still forces duration.
		log.Printf("generate_video: duration chip select: %v (hook still forces %ds)", err, want)
		return nil
	}
	after, _ := readCurrentVideoDuration(ctx)
	if after == want {
		log.Printf("generate_video: duration set to %ds", want)
	} else if after > 0 {
		log.Printf("generate_video: duration chip now %ds (want %ds; hook still forces)", after, want)
	}
	return nil
}

func readCurrentVideoDuration(ctx context.Context) (int, error) {
	var out struct {
		Found bool `json:"found"`
		Sec   int  `json:"sec"`
	}
	const js = videoToolbarJSShared + `(() => {
		const scopes = [];
		const root = videoToolbarRoot();
		if (root) scopes.push(root);
		scopes.push(document);
		const vh = window.innerHeight;
		for (const scope of scopes) {
			for (const el of scope.querySelectorAll('button, [role="button"]')) {
				if (!isVisible(el)) continue;
				const r = el.getBoundingClientRect();
				if (r.top < vh * 0.45) continue;
				const t = (el.textContent || '').trim().replace(/\s+/g, ' ');
				const m = t.match(durationChipRe) || t.match(/^(\d+)\s*秒$/);
				if (!m) continue;
				return { found: true, sec: parseInt(m[1], 10) };
			}
		}
		return { found: false, sec: 0 };
	})()`
	if err := evalReturnByValue(ctx, js, &out); err != nil {
		return 0, err
	}
	if !out.Found {
		return 0, nil
	}
	return out.Sec, nil
}

func selectVideoDurationOnce(ctx context.Context, wantSec int) error {
	want := normalizeVideoDurationSec(wantSec)
	// Click duration chip.
	var chip clickPoint
	jsChip := videoToolbarJSShared + `(() => {
		const scopes = [];
		const root = videoToolbarRoot();
		if (root) scopes.push(root);
		scopes.push(document);
		const vh = window.innerHeight;
		for (const scope of scopes) {
			for (const el of scope.querySelectorAll('button, [role="button"]')) {
				if (!isVisible(el)) continue;
				const r = el.getBoundingClientRect();
				if (r.top < vh * 0.45) continue;
				const t = (el.textContent || '').trim().replace(/\s+/g, ' ');
				if (!durationChipRe.test(t) && !/^(\d+)\s*秒$/.test(t)) continue;
				el.scrollIntoView({ block: 'center', inline: 'center' });
				const rr = el.getBoundingClientRect();
				return { found: true, x: rr.left + rr.width / 2, y: rr.top + rr.height / 2, text: t };
			}
		}
		return { found: false, error: "duration chip not found" };
	})()`
	if err := evalReturnByValue(ctx, jsChip, &chip); err != nil {
		return err
	}
	if !chip.Found {
		if chip.Error != "" {
			return errors.New(chip.Error)
		}
		return errors.New("duration chip not found")
	}
	log.Printf("generate_video: click duration chip %q at (%.0f, %.0f)", chip.Text, chip.X, chip.Y)
	if err := chromedp.Run(ctx,
		chromedp.MouseClickXY(chip.X, chip.Y, chromedp.ButtonLeft),
		chromedp.Sleep(900*time.Millisecond),
	); err != nil {
		return err
	}

	var opt clickPoint
	jsOpt := videoToolbarJSShared + fmt.Sprintf(`(() => {
		const want = %d;
		const labelRe = new RegExp('^' + want + '\\\\s*(s|秒)$', 'i');
		const softRe = new RegExp('\\\\b' + want + '\\\\s*(s|秒)\\\\b', 'i');
		const candidates = [];
		for (const el of document.querySelectorAll('div, li, button, span, [role="menuitem"], [role="option"]')) {
			if (!isVisible(el)) continue;
			const t = (el.innerText || el.textContent || '').replace(/\\s+/g, ' ').trim();
			if (!t || t.length > 40) continue;
			if (isToolbarChipText(t) && !labelRe.test(t)) continue;
			if (!labelRe.test(t) && !softRe.test(t)) continue;
			const r = el.getBoundingClientRect();
			if (r.width < 20 || r.height < 16) continue;
			let score = labelRe.test(t) ? 0 : 2;
			if (el.closest('[class*="popover"], [class*="dropdown"], [class*="popup"], [role="menu"], [role="listbox"]')) score -= 1;
			candidates.push({ score, len: t.length, x: r.left + r.width / 2, y: r.top + r.height / 2, text: t });
		}
		if (!candidates.length) return { found: false, error: "duration option " + want + "s not found" };
		candidates.sort((a, b) => a.score - b.score || a.len - b.len);
		const best = candidates[0];
		return { found: true, x: best.x, y: best.y, text: best.text };
	})()`, want)
	if err := evalReturnByValue(ctx, jsOpt, &opt); err != nil {
		return err
	}
	if !opt.Found {
		if opt.Error != "" {
			return errors.New(opt.Error)
		}
		return fmt.Errorf("duration option %ds not found", want)
	}
	log.Printf("generate_video: pick duration option %q at (%.0f, %.0f)", opt.Text, opt.X, opt.Y)
	return chromedp.Run(ctx,
		chromedp.MouseClickXY(opt.X, opt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(500*time.Millisecond),
	)
}

func isVideoModelMenuOpen(ctx context.Context) (bool, error) {
	var out struct {
		Open bool `json:"open"`
	}
	js := videoToolbarJSShared + `(() => ({ open: isModelMenuOpen() }))()`
	if err := evalReturnByValue(ctx, js, &out); err != nil {
		return false, err
	}
	return out.Open, nil
}

func clickVideoModelOption(ctx context.Context, want string) error {
	var pt clickPoint
	js := videoToolbarJSShared + fmt.Sprintf(`(() => {
		const want = %s;
		const wantLineRe = want === "mini" ? /^2\.0\s*Mini\b/i : /^2\.0\s*Fast\b/i;
		const wantFullRe = want === "mini" ? /2\.0\s*Mini/i : /2\.0\s*Fast/i;
		const chip = findModelChipButton();
		const chipTop = chip ? chip.el.getBoundingClientRect().top : window.innerHeight;
		const rows = [];
		for (const el of document.querySelectorAll('div, li, button, span, p, [role="menuitem"], [role="option"]')) {
			if (!isVisible(el)) continue;
			const full = (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim();
			if (!full || full.length > 200 || isToolbarChipText(full)) continue;
			const lines = (el.innerText || '').split('\n').map(s => s.trim()).filter(Boolean);
			const first = lines[0] || '';
			if (/^Seedance\s*2\.0\s*$/i.test(first)) continue;
			if (/Seedance\s*2\.0/i.test(full) && !/Fast|Mini/i.test(full)) continue;
			if (want === "fast" && /2\.0\s*Mini/i.test(full) && !/2\.0\s*Fast/i.test(full)) continue;
			if (want === "mini" && /2\.0\s*Fast/i.test(full) && !/2\.0\s*Mini/i.test(full)) continue;
			const lineMatch = wantLineRe.test(first) || new RegExp("Seedance\\s*2\\.0\\s*" + (want === "mini" ? "Mini" : "Fast"), "i").test(first);
			const fullMatch = wantFullRe.test(full) || new RegExp("Seedance\\s*2\\.0\\s*" + (want === "mini" ? "Mini" : "Fast"), "i").test(full);
			if (!lineMatch && !fullMatch) continue;
			const r = el.getBoundingClientRect();
			const inMenu = el.closest('[class*="popover"], [class*="dropdown"], [class*="popup"], [role="menu"], [role="listbox"]');
			if (!inMenu && r.top > chipTop + 8) continue;
			let score = wantLineRe.test(first) ? 0 : (/Seedance/i.test(first) ? 1 : 2);
			rows.push({ el, score, len: full.length });
		}
		if (!rows.length) {
			const debug = [];
			for (const el of document.querySelectorAll('div, li, span, p, button')) {
				if (!isVisible(el)) continue;
				const t = (el.innerText || '').replace(/\s+/g, ' ').trim();
				if (/2\.0|Fast|Mini|Seedance|模型/.test(t) && t.length < 100 && !isToolbarChipText(t)) debug.push(t.slice(0, 60));
			}
			const uniq = [...new Set(debug)].slice(0, 6);
			return { found: false, error: "model option not found" + (uniq.length ? " (menu: " + uniq.join(" | ") + ")" : "") };
		}
		rows.sort((a, b) => a.score - b.score || a.len - b.len);
		let el = rows[0].el;
		while (el && el.tagName !== 'BUTTON' && el.getAttribute('role') !== 'button' && el.getAttribute('role') !== 'menuitem' && el.getAttribute('role') !== 'option') {
			if (!el.parentElement || el.parentElement === document.body) break;
			const pt = (el.parentElement.innerText || '').replace(/\s+/g, ' ').trim();
			if (pt.length > 200 || isToolbarChipText(pt)) break;
			el = el.parentElement;
		}
		el.scrollIntoView({ block: 'center', inline: 'center' });
		const r = el.getBoundingClientRect();
		return { found: true, x: r.left + r.width / 2, y: r.top + r.height / 2 };
	})()`, jsonString(want))
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return err
	}
	if pt.Found {
		return chromedp.Run(ctx,
			chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
			chromedp.Sleep(300*time.Millisecond),
		)
	}
	if pt.Error != "" {
		return errors.New(pt.Error)
	}
	return fmt.Errorf("model option not found: %s", videoModelUILabel(want))
}

func clickModelSelector(ctx context.Context) error {
	var pt clickPoint
	js := videoToolbarJSShared + `(() => {
		const chip = findModelChipButton();
		if (!chip) return { found: false, error: "model chip button not found in input toolbar" };
		chip.el.scrollIntoView({ block: 'center', inline: 'center' });
		const r = chip.el.getBoundingClientRect();
		return { found: true, x: r.left + r.width / 2, y: r.top + r.height / 2, text: chip.text };
	})()`
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return err
	}
	if !pt.Found {
		if pt.Error != "" {
			return errors.New(pt.Error)
		}
		return fmt.Errorf("model selector not found")
	}
	log.Printf("generate_video: click model chip %q at (%.0f, %.0f)", pt.Text, pt.X, pt.Y)
	return chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(500*time.Millisecond),
	)
}

func videoSkillActive(ctx context.Context) bool {
	var active bool
	_ = evalReturnByValue(ctx, videoToolbarJSShared+`(() => {
		if (findModelChipButton()) return true;
		for (const el of document.querySelectorAll('button, [role="button"], div, span')) {
			if (!isVisible(el)) continue;
			const t = (el.textContent || '').trim().replace(/\s+/g, ' ');
			if (t !== '视频生成' && t !== '快速视频生成') continue;
			const cls = String(el.className || '') + String(el.parentElement && el.parentElement.className || '');
			if (/active|selected|checked|current/i.test(cls)) return true;
		}
		const editors = [...document.querySelectorAll('textarea, [contenteditable]:not([contenteditable="false"]), [role="textbox"]')].filter(isVisible);
		const ph = editors.length ? editors[editors.length - 1] : null;
		const hint = (ph && (ph.getAttribute('placeholder') || ph.getAttribute('data-placeholder') || '') || '').trim();
		return /视频|描述你想/.test(hint);
	})()`, &active)
	return active
}

func submitAcceptedInDOM(ctx context.Context) (bool, string) {
	var result struct {
		OK          bool   `json:"ok"`
		Reason      string `json:"reason"`
		EditorEmpty bool   `json:"editorEmpty"`
		BodyTail    string `json:"bodyTail"`
	}
	js := `(() => {` + chatEditorQueryJS + `
		const text = document.body.innerText || "";
		if (/本月办公任务免费额度已用完|办公任务免费额度已用完|今日免费生视频额度已用完|今日视频生成免费次数用完了|视频生成免费次数用完了|专业能力暂不可用|暂时无法使用专业版功能|专业版加强套餐专属能力|开通加强套餐，我就能继续|这是专业版加强套餐|开通加强套餐/.test(text)) {
			return { ok: false, reason: "quota_exceeded", editorEmpty: false, bodyTail: text.slice(-2500) };
		}

		function readVal(el) {
			if (!el) return null;
			const val = ('value' in el) ? (el.value || '') : (el.innerText || el.textContent || '');
			return String(val);
		}

		// Marker can vanish while the chat re-renders after send; fall back to the
		// visible bottom composer so we still notice a cleared input.
		let el = document.querySelector('[data-doubao-chat-editor="1"]');
		let val = (el && paintedChatEl(el)) ? readVal(el) : null;
		if (val === null) {
			const pick = pickChatEditor();
			if (pick) {
				el = pick.el;
				val = readVal(el);
			}
		}

		const editorEmpty = val === null || !String(val).trim();
		// Cleared composer after send is enough — do not require /chat/<id> yet;
		// new chats often stay on /chat until the first reply lands.
		if (editorEmpty && /\/chat/.test(location.pathname || "")) {
			return { ok: true, reason: "input_cleared", editorEmpty: true, bodyTail: text.slice(-2500) };
		}
		return { ok: false, editorEmpty: editorEmpty, bodyTail: text.slice(-2500) };
	})()`
	if err := evalReturnByValue(ctx, js, &result); err != nil {
		return false, ""
	}
	if result.Reason == "quota_exceeded" {
		return false, result.Reason
	}
	if result.OK {
		return true, result.Reason
	}
	// Prefer Go-side ack matchers so new Doubao copy stays in one place.
	if textIndicatesVideoGenerating(result.BodyTail) || textIndicatesVideoComplete(result.BodyTail) {
		return true, "assistant_ack"
	}
	return false, ""
}

func renameDialogOpen(ctx context.Context) bool {
	var open bool
	_ = evalReturnByValue(ctx, `(() => {
		const text = (document.body && document.body.innerText) || "";
		if (!/编辑对话名称|修改对话名称|重命名对话/.test(text)) return false;
		for (const root of document.querySelectorAll('[role="dialog"], dialog, [class*="modal" i], [class*="dialog" i], [class*="popup" i]')) {
			if (!root.offsetParent) continue;
			const t = (root.innerText || "").slice(0, 120);
			if (/编辑对话名称|修改对话名称|重命名对话/.test(t)) return true;
		}
		return false;
	})()`, &open)
	return open
}

// chatEditorQueryJS locates Doubao's bottom composer. Kept inside IIFEs so
// Runtime.evaluate lexical reuse cannot redeclare these helpers.
// 2026-08 UI: blank chat uses textarea.semi-input-textarea; video-skill mode
// switches to Tiptap (.tiptap.ProseMirror). Slash/upload popovers often set
// aria-hidden on ancestors while the editor is still painted.
const chatEditorQueryJS = `
	function paintedChatEl(el) {
		if (!el) return false;
		// aria-hidden is an accessibility hint, not a paint signal. Doubao's
		// composer can retain it while an upload/skill popover is transitioning.
		if (el.hasAttribute('hidden')) return false;
		if (el.closest('[hidden]')) return false;
		const st = window.getComputedStyle(el);
		if (st.display === 'none' || st.visibility === 'hidden') return false;
		const r = el.getBoundingClientRect();
		if (r.width > 4 && r.height > 4) return true;
		// An empty ProseMirror/contenteditable can temporarily collapse to a
		// zero-height caret node even though its composer wrapper is visible.
		let p = el.parentElement;
		for (let i = 0; p && i < 6; i++, p = p.parentElement) {
			const ps = window.getComputedStyle(p);
			const pr = p.getBoundingClientRect();
			if (ps.display !== 'none' && ps.visibility !== 'hidden' && pr.width > 160 && pr.height > 12
				&& pr.height < Math.max(180, window.innerHeight * 0.48)) return true;
		}
		return false;
	}
	function chatEditorRect(el) {
		let r = el.getBoundingClientRect();
		if (r.width > 80 && r.height > 8) return r;
		let p = el.parentElement;
		for (let i = 0; p && i < 6; i++, p = p.parentElement) {
			const pr = p.getBoundingClientRect();
			if (pr.width > 160 && pr.height > 12
				&& pr.height < Math.max(180, window.innerHeight * 0.48)) return pr;
		}
		return r;
	}
	function isRenameDialogEl(el) {
		const root = el.closest('[role="dialog"], dialog');
		if (!root) return false;
		return /编辑对话名称|修改对话名称|重命名对话/.test((root.innerText || '').slice(0, 120));
	}
	function collectChatEditors() {
		const vh = window.innerHeight, vw = window.innerWidth;
		const selectors = [
			'textarea.semi-input-textarea',
			'textarea[placeholder*="发消息"]',
			'textarea[placeholder*="描述"]',
			'.tiptap.ProseMirror',
			'.ProseMirror[contenteditable]',
			'[data-placeholder*="发消息"]',
			'[contenteditable="true"]',
			'[contenteditable="plaintext-only"]',
			'[role="textbox"]',
			'textarea',
		];
		const seen = new Set();
		const out = [];
		// Clicking the composer may expose the real editor through focus even
		// while React has not yet restored its normal class names/attributes.
		let active = document.activeElement;
		while (active && active.shadowRoot && active.shadowRoot.activeElement) active = active.shadowRoot.activeElement;
		if (active && (active.isContentEditable || active.tagName === 'TEXTAREA' || active.getAttribute('role') === 'textbox')) {
			const ar = chatEditorRect(active);
			const activeMinWidth = (active.isContentEditable || active.getAttribute('role') === 'textbox') ? 48 : 160;
			if (paintedChatEl(active) && ar.width >= activeMinWidth && ar.height >= 12 && ar.right >= vw * 0.22 && ar.bottom >= vh * 0.38) {
				seen.add(active);
				out.push({ el: active, r: ar, score: 10000, placeholder: (active.getAttribute('placeholder') || '').trim() });
			}
		}
		for (const sel of selectors) {
			for (const el of document.querySelectorAll(sel)) {
				if (seen.has(el) || !paintedChatEl(el)) continue;
				seen.add(el);
				if (isRenameDialogEl(el)) continue;
				const r = chatEditorRect(el);
				// 2026-08 Doubao sometimes shrink-wraps the visible contenteditable
				// to its placeholder (e.g. 106x32 "描述你想要的视频") instead of
				// stretching it across the composer. It is still the real editor.
				const minWidth = (el.isContentEditable || el.getAttribute('role') === 'textbox') ? 48 : 160;
				if (r.width < minWidth || r.height < 12) continue;
				if (r.right < vw * 0.22) continue;
				if (r.bottom < vh * 0.38) continue;
				if (r.top < 40 && r.height < 48) continue;
				const placeholder = (el.getAttribute('placeholder') || el.getAttribute('data-placeholder') || '').trim();
				const cls = String(el.className || '');
				let score = (r.bottom / vh) * 1000 + r.width / 20;
				if (/发消息|描述|空格说话|输入/.test(placeholder)) score += 2000;
				if (/semi-input-textarea|tiptap|ProseMirror/.test(cls)) score += 1500;
				if (el.tagName === 'TEXTAREA') score += 1000;
				if (el.isContentEditable || el.getAttribute('role') === 'textbox') score += 800;
				if (el.tagName === 'P') score -= 600;
				if (r.bottom > vh - 240) score += 800;
				if (r.top > vh * 0.50) score += 400;
				out.push({ el: el, r: r, score: score, placeholder: placeholder });
			}
		}
		out.sort(function(a, b) { return b.score - a.score; });
		return out;
	}
	function pickChatEditor() {
		const all = collectChatEditors();
		if (!all.length) return null;
		let el = all[0].el;
		if (el.tagName === 'P') {
			const host = el.closest('[contenteditable], .ProseMirror, .tiptap');
			if (host) el = host;
		}
		return { el: el, placeholder: all[0].placeholder };
	}
	function chatEditorDebug() {
		const raw = Array.from(document.querySelectorAll('textarea, [contenteditable], [role="textbox"]')).slice(0, 8).map(function(el) {
			const r = el.getBoundingClientRect();
			const p = el.parentElement ? el.parentElement.getBoundingClientRect() : { width: 0, height: 0, bottom: 0 };
			const st = window.getComputedStyle(el);
			return el.tagName + ':' + Math.round(r.width) + 'x' + Math.round(r.height) + '@' + Math.round(r.bottom)
				+ '/p' + Math.round(p.width) + 'x' + Math.round(p.height) + '@' + Math.round(p.bottom)
				+ '/' + st.display + '/' + st.visibility + '/ce=' + (el.getAttribute('contenteditable') || '')
				+ '/role=' + (el.getAttribute('role') || '') + '/ah=' + (el.getAttribute('aria-hidden') || '');
		}).join(',');
		return "textarea=" + document.querySelectorAll('textarea').length
			+ " ce=" + document.querySelectorAll('[contenteditable]').length
			+ " tb=" + document.querySelectorAll('[role="textbox"]').length
			+ " painted=" + collectChatEditors().length
			+ " vh=" + window.innerHeight + " raw=[" + raw + "]";
	}
`

func focusChatEditor(ctx context.Context) error {
	// Never type into an accidental 「编辑对话名称」dialog.
	_ = dismissDoubaoPopups(ctx)
	var lastErr error
	for attempt := 1; attempt <= 12; attempt++ {
		if attempt > 1 {
			_ = chromedp.Run(ctx,
				chromedp.KeyEvent("\u001b"),
				chromedp.Sleep(500*time.Millisecond),
			)
			_ = dismissDoubaoPopups(ctx)
		}
		// When the DOM candidates are in a transient zero-size state, activate
		// the visibly painted lower composer first and let activeElement reveal
		// the editor on the next pass. Keep this away from the bottom toolbar.
		if attempt == 4 || attempt == 8 {
			_ = activateBottomComposer(ctx)
		}
		err := focusChatEditorOnce(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		log.Printf("generate_video: focus editor attempt %d: %v", attempt, err)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("chat editor not found")
}

func activateBottomComposer(ctx context.Context) error {
	var pt clickPoint
	if err := evalReturnByValue(ctx, `(() => {
		const vw = window.innerWidth, vh = window.innerHeight;
		if (vw < 500 || vh < 400) return { found: false };
		function visible(el) {
			const st = window.getComputedStyle(el);
			const r = el.getBoundingClientRect();
			return st.display !== 'none' && st.visibility !== 'hidden' && r.width > 40 && r.height > 12;
		}
		// In the collapsed composer Doubao leaves the real role=textbox at the
		// exact input-line coordinates but marks it visibility:hidden; a visible
		// placeholder overlay receives the click at the same point. Use that
		// geometry before guessing from copy or a fixed viewport offset.
		const editorRects = [];
		for (const el of document.querySelectorAll('[role="textbox"], [contenteditable="true"], [contenteditable="plaintext-only"]')) {
			const r = el.getBoundingClientRect();
			if (r.width < 160 || r.height < 12 || r.height > 120) continue;
			if (r.bottom < vh * 0.50 || r.top >= vh || r.right < vw * 0.22) continue;
			const st = window.getComputedStyle(el);
			editorRects.push({ r, score: r.width + r.bottom, state: st.visibility + '/' + (el.getAttribute('role') || '') });
		}
		if (editorRects.length) {
			editorRects.sort((a, b) => b.score - a.score);
			const pick = editorRects[0];
			// The collapsed composer only binds its activation handler around the
			// left-side placeholder/caret area; clicking the wide rect centre can
			// land on an inert layer.
			const x = pick.r.left + Math.min(36, Math.max(12, pick.r.width * 0.04));
			const y = pick.r.top + pick.r.height / 2;
			const hit = document.elementFromPoint(x, y);
			const hitText = hit ? ((hit.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 24) || hit.tagName) : 'none';
			return { found: true, x, y, text: 'editor-left:' + pick.state + '/hit=' + hitText };
		}
		const promptRe = /^(描述你想要的视频|描述你想生成的视频|发消息或按住空格说话|发消息|输入消息)$/;
		const candidates = [];
		for (const el of document.querySelectorAll('[contenteditable], [role="textbox"], [data-placeholder], [placeholder], div, span, p')) {
			if (!visible(el)) continue;
			const r = el.getBoundingClientRect();
			if (r.bottom < vh * 0.50 || r.right < vw * 0.22 || r.height > 90 || r.width > 520) continue;
			const text = (el.textContent || '').trim().replace(/\s+/g, ' ');
			const hint = (el.getAttribute('data-placeholder') || el.getAttribute('placeholder') || el.getAttribute('aria-label') || '').trim();
			if (!promptRe.test(text) && !promptRe.test(hint)) continue;
			candidates.push({ el, r, score: r.bottom + (promptRe.test(hint) ? 1000 : 0) - text.length });
		}
		if (candidates.length) {
			candidates.sort((a, b) => b.score - a.score);
			const pick = candidates[0];
			return { found: true, x: pick.r.left + pick.r.width / 2, y: pick.r.top + pick.r.height / 2, text: 'placeholder:' + ((pick.el.textContent || '').trim() || pick.el.getAttribute('data-placeholder') || '') };
		}
		// Last resort for UI variants without recognizable placeholder copy.
		return { found: true, x: Math.max(vw * 0.58, 420), y: Math.max(vh - 125, vh * 0.62), text: 'fixed-probe' };
	})()`, &pt); err != nil {
		return err
	}
	if !pt.Found {
		return fmt.Errorf("composer probe point unavailable")
	}
	log.Printf("generate_video: activate bottom composer via %s at (%.0f, %.0f)", pt.Text, pt.X, pt.Y)
	return chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(350*time.Millisecond),
	)
}

func focusChatEditorOnce(ctx context.Context) error {
	js := `(() => {` + chatEditorQueryJS + `
		let pick = pickChatEditor();
		let revived = false;
		if (!pick) {
			// Slash-menu selection occasionally leaves the real video composer
			// mounted at the correct bottom geometry but visibility:hidden while a
			// stale "/" overlay remains clickable. Revive only the strict textbox
			// shape; never expose arbitrary hidden contenteditables.
			const vh = window.innerHeight, vw = window.innerWidth;
			const dormant = [];
			for (const el of document.querySelectorAll('[role="textbox"][contenteditable="true"], [role="textbox"][contenteditable="plaintext-only"]')) {
				if (isRenameDialogEl(el) || el.hasAttribute('hidden') || el.closest('[hidden]')) continue;
				const r = el.getBoundingClientRect();
				if (r.width < 160 || r.height < 12 || r.height > 120) continue;
				if (r.bottom < vh * 0.50 || r.top >= vh || r.right < vw * 0.22) continue;
				const st = window.getComputedStyle(el);
				if (st.display === 'none') continue;
				dormant.push({ el, r, score: r.bottom + r.width / 20 });
			}
			if (dormant.length) {
				dormant.sort((a, b) => b.score - a.score);
				const el = dormant[0].el;
				el.style.setProperty('visibility', 'visible', 'important');
				el.style.setProperty('opacity', '1', 'important');
				el.style.setProperty('pointer-events', 'auto', 'important');
				pick = { el, placeholder: el.getAttribute('data-placeholder') || el.getAttribute('placeholder') || 'revived-textbox' };
				revived = true;
			}
		}
		if (!pick) {
			return { found: false, error: "chat editor not found (" + chatEditorDebug() + ")" };
		}
		const el = pick.el;
		for (const old of document.querySelectorAll('[data-doubao-chat-editor="1"]')) {
			old.removeAttribute('data-doubao-chat-editor');
		}
		el.setAttribute('data-doubao-chat-editor', '1');
		el.scrollIntoView({ block: 'nearest', inline: 'nearest' });
		try { el.focus(); } catch (e) {}
		const own = el.getBoundingClientRect();
		const r = chatEditorRect(el);
		// With many reference images an empty contenteditable may have a 0px
		// rect. Clicking the wrapper centre can hit a thumbnail and immediately
		// steal the focus we just established. In that state JS focus is enough
		// for CDP key events; use a negative point to suppress the mouse click.
		const needsMouse = own.width > 4 && own.height > 4;
		return {
			found: true,
			x: needsMouse ? (revived ? own.left + Math.min(36, own.width * 0.04) : own.left + own.width / 2) : -1,
			y: needsMouse ? own.top + own.height / 2 : -1,
			text: (revived ? "revived:" : needsMouse ? "mouse:" : "js-focus:") + (pick.placeholder || el.tagName),
		};
	})()`
	var pt clickPoint
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return err
	}
	if !pt.Found {
		if pt.Error != "" {
			return fmt.Errorf("%s", pt.Error)
		}
		return fmt.Errorf("chat editor not found")
	}
	if strings.HasPrefix(pt.Text, "revived:") {
		log.Printf("generate_video: revived dormant video textbox (%s)", pt.Text)
	}
	if pt.X < 0 || pt.Y < 0 {
		return chromedp.Run(ctx, chromedp.Sleep(200*time.Millisecond))
	}
	return chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(200*time.Millisecond),
	)
}

// clearFocusedEditor sends real keyboard events so React-controlled editor state
// is cleared together with the visible DOM value.
func clearFocusedEditor(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		const keyA = 65
		const keyBackspace = 8
		if err := input.DispatchKeyEvent(input.KeyDown).
			WithModifiers(input.ModifierMeta).
			WithKey("a").
			WithCode("KeyA").
			WithWindowsVirtualKeyCode(keyA).
			WithCommands([]string{"selectAll"}).
			Do(ctx); err != nil {
			return err
		}
		if err := input.DispatchKeyEvent(input.KeyUp).
			WithModifiers(input.ModifierMeta).
			WithKey("a").
			WithCode("KeyA").
			WithWindowsVirtualKeyCode(keyA).
			Do(ctx); err != nil {
			return err
		}
		if err := input.DispatchKeyEvent(input.KeyDown).
			WithKey("Backspace").
			WithCode("Backspace").
			WithWindowsVirtualKeyCode(keyBackspace).
			Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).
			WithKey("Backspace").
			WithCode("Backspace").
			WithWindowsVirtualKeyCode(keyBackspace).
			Do(ctx)
	}))
}

// typeIntoFocused inserts text into the focused editor.
// Long prompts must NOT use per-keystroke DispatchKeyEvent (drops most CJK chars).
// Input.insertText is a browser editing command, so React receives the trusted
// beforeinput/input sequence. Directly assigning textarea.value is forbidden here:
// it only changes the DOM while Doubao keeps stale state (typically the "/" used
// to open the skill menu), causing an images-only submission.
func typeIntoFocused(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	// Short tokens (e.g. "/" skill menu) can use key events; bulk text must not.
	if utf8RuneCount(text) <= 4 {
		return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			for _, r := range text {
				if err := input.DispatchKeyEvent(input.KeyDown).WithText(string(r)).Do(ctx); err != nil {
					return err
				}
				if err := input.DispatchKeyEvent(input.KeyUp).WithText(string(r)).Do(ctx); err != nil {
					return err
				}
			}
			return nil
		}))
	}
	if err := insertTextBulk(ctx, text); err == nil {
		log.Printf("generate_video: inserted prompt via Input.insertText (%d runes)", utf8RuneCount(text))
		return nil
	} else {
		return fmt.Errorf("Input.insertText: %w; refusing unsafe DOM-only fallback", err)
	}
}

func insertTextBulk(ctx context.Context, text string) error {
	return chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			const el = document.querySelector('[data-doubao-chat-editor="1"]');
			if (!el) return false;
			try { el.focus(); } catch (_) {}
			return document.activeElement === el || el.contains(document.activeElement);
		})()`, nil),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.InsertText(text).Do(ctx)
		}),
	)
}

func utf8RuneCount(s string) int {
	return len([]rune(s))
}

// normalizePromptText collapses all whitespace so Doubao editor formatting
// (extra blank lines between paragraphs) does not fail prompt checks.
func normalizePromptText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		lastSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// promptEnteredOK reports whether the composer captured enough of the intended prompt.
// Exact equality is too brittle (whitespace / NBSP normalization); length is the signal
// that per-keystroke typing failed (e.g. 2308 → 1). Extra blank lines from the Doubao
// editor (874 typed → 909 read) must still pass.
func promptEnteredOK(want, got string) bool {
	want = normalizePromptText(want)
	got = normalizePromptText(got)
	if want == "" {
		return false
	}
	wantN := utf8RuneCount(want)
	gotN := utf8RuneCount(got)
	if wantN <= 8 {
		return gotN >= wantN || strings.Contains(got, want)
	}
	minOK := wantN * 80 / 100
	if minOK < 32 {
		minOK = wantN
	}
	if gotN < minOK {
		return false
	}
	// Editor may inject a little formatting, but should not grow unboundedly
	// unless unrelated leftover text is mixed in.
	if gotN > wantN*3/2+64 && !strings.Contains(got, want) {
		head := want
		if utf8RuneCount(head) > 48 {
			head = string([]rune(want)[:48])
		}
		if !strings.Contains(got, head) {
			return false
		}
	}
	needle := promptNeedle(want)
	if needle == "" {
		return true
	}
	if strings.Contains(got, needle) {
		return true
	}
	// Also accept when head + tail anchors are present (newline reformatting
	// can split an unlucky mid-slice needle).
	runes := []rune(want)
	head := string(runes[:min(32, len(runes))])
	tail := string(runes[max(0, len(runes)-32):])
	return strings.Contains(got, head) && strings.Contains(got, tail)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// promptNeedle returns a distinctive mid-slice used to verify the prompt survived submit.
func promptNeedle(prompt string) string {
	prompt = normalizePromptText(prompt)
	runes := []rune(prompt)
	n := len(runes)
	if n == 0 {
		return ""
	}
	if n <= 16 {
		return string(runes)
	}
	start := n / 3
	end := start + 16
	if end > n {
		end = n
		start = n - 16
		if start < 0 {
			start = 0
		}
	}
	return string(runes[start:end])
}

func pageContainsPromptNeedle(ctx context.Context, prompt string) bool {
	anchors := promptAnchors(prompt)
	if len(anchors) == 0 {
		return false
	}
	payload, err := json.Marshal(anchors)
	if err != nil {
		return false
	}
	js := fmt.Sprintf(`(() => {
		const anchors = %s;
		const editor = document.querySelector('[data-doubao-chat-editor="1"]');
		let editorText = "";
		if (editor && editor.offsetParent) {
			editorText = String(('value' in editor) ? (editor.value || '') : (editor.innerText || editor.textContent || '')).trim();
		}
		const norm = (s) => String(s || '').replace(/\s+/g, ' ').trim();
		const editorN = norm(editorText);
		const body = norm(document.body.innerText || "");
		// Composer still holding a long prompt means it was NOT sent yet.
		if (editorN.length > 80) {
			for (const a of anchors) {
				const n = norm(a);
				if (n && editorN.includes(n)) return false;
			}
		}
		for (const a of anchors) {
			const n = norm(a);
			if (n && body.includes(n)) return true;
		}
		return false;
	})()`, string(payload))
	var ok bool
	if err := evalReturnByValue(ctx, js, &ok); err != nil {
		return false
	}
	return ok
}

// promptAnchors returns short distinctive fragments for post-submit verification.
// Doubao often collapses long user prompts in the bubble, so a single mid-slice
// needle is frequently missing from the DOM even when the send succeeded.
func promptAnchors(prompt string) []string {
	prompt = normalizePromptText(prompt)
	if prompt == "" {
		return nil
	}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, existing := range out {
			if existing == s {
				return
			}
		}
		out = append(out, s)
	}
	runes := []rune(prompt)
	if len(runes) <= 24 {
		add(prompt)
		return out
	}
	add(string(runes[:min(24, len(runes))]))
	if n := promptNeedle(prompt); n != "" {
		add(n)
	}
	add(string(runes[max(0, len(runes)-24):]))
	// Stable markers from our UI wrapper.
	if strings.Contains(prompt, "无需二次确认，请直接开始生成") {
		add("无需二次确认，请直接开始生成")
	}
	if strings.Contains(prompt, "帮我严格按照下面要求生成") {
		add("帮我严格按照下面要求生成")
	}
	return out
}

// ensurePromptSubmittedInChat verifies the prompt actually landed in the chat after send.
// DOM-only inserts can look fine in the composer while React still submits images only.
// Long prompts may be collapsed in the UI, so generation acknowledgements also count.
func ensurePromptSubmittedInChat(ctx context.Context, prompt string) error {
	anchors := promptAnchors(prompt)
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		paused, err := waitForHumanVerification(ctx)
		if err != nil {
			return err
		}
		deadline = deadline.Add(paused)
		if pageContainsPromptNeedle(ctx, prompt) {
			log.Printf("generate_video: submitted prompt verified on page (anchors=%d)", len(anchors))
			return nil
		}
		// A generation acknowledgement in this freshly-created job conversation
		// proves the current submit landed. Do not require editorHasText=false:
		// after submit the editor finder can briefly read the detached old node,
		// producing a false "images-only send" even while Doubao visibly says
		// 「视频生成已提交」 and shows an ETA.
		if videoGenerationAcknowledged(ctx) || videoGenerationComplete(ctx) {
			log.Printf("generate_video: submitted prompt verified via generation acknowledgement")
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if videoGenerationAcknowledged(ctx) || videoGenerationComplete(ctx) {
		log.Printf("generate_video: submitted prompt verified via generation acknowledgement (late)")
		return nil
	}
	preview := ""
	if len(anchors) > 0 {
		preview = truncateRunes(anchors[0], 24)
	}
	return fmt.Errorf("refuse video generation: prompt missing from chat after submit (anchor=%q) — images-only send", preview)
}

func readSubmitCount(ctx context.Context) (int, error) {
	var n int
	if err := evalReturnByValue(ctx, `(() => (window.__doubaoVideoCapture && window.__doubaoVideoCapture.submitCount) || 0)()`, &n); err != nil {
		return 0, err
	}
	return n, nil
}

func markVideoCaptureSubmitBaseline(ctx context.Context) error {
	const js = `(() => {
		const cap = window.__doubaoVideoCapture;
		if (!cap) return { ok: false };
		cap.chunkBaseline = (cap.chunks || []).length;
		cap.videoURLBaseline = (cap.videoURLs || []).length;
		return { ok: true };
	})()`
	var out map[string]any
	return evalReturnByValue(ctx, js, &out)
}

func findSubmitButtonPoint(ctx context.Context) (clickPoint, error) {
	const js = videoToolbarJSShared + `(() => {
		const root = videoToolbarRoot();
		const editorRect = composerEditorRect();

		const scopes = root ? [root, document] : [document];
		const candidates = [];
		for (const scope of scopes) {
			for (const el of scope.querySelectorAll('button, [role="button"]')) {
				if (!isVisible(el)) continue;
				const s = scoreComposerSubmitButton(el, editorRect, root);
				if (s < 20) continue;
				const r = el.getBoundingClientRect();
				const label = (el.textContent || "").trim() || (el.getAttribute("aria-label") || "").trim() ||
					(isHighlightSendButton(el) ? "send-highlight" : "send-icon");
				candidates.push({ s, x: r.left + r.width / 2, y: r.top + r.height / 2, text: label });
			}
			if (candidates.length) break;
		}
		candidates.sort((a, b) => b.s - a.s);
		if (!candidates.length) return { found: false, error: "submit button not found in input toolbar" };
		return { found: true, x: candidates[0].x, y: candidates[0].y, text: candidates[0].text };
	})()`
	var pt clickPoint
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return pt, err
	}
	return pt, nil
}

func clickSubmitButtonJS(ctx context.Context) error {
	const js = videoToolbarJSShared + `(() => {
		const root = videoToolbarRoot();
		const editorRect = composerEditorRect();

		const scopes = root ? [root, document] : [document];
		const candidates = [];
		for (const scope of scopes) {
			for (const el of scope.querySelectorAll('button, [role="button"]')) {
				if (!isVisible(el)) continue;
				const s = scoreComposerSubmitButton(el, editorRect, root);
				if (s < 20) continue;
				candidates.push({ el, s });
			}
			if (candidates.length) break;
		}
		if (!candidates.length) return { found: false, error: "submit button not found in input toolbar" };
		candidates.sort((a, b) => b.s - a.s);
		const pick = candidates[0].el;
		pick.scrollIntoView({ block: "center", inline: "center" });
		pick.click();
		const r = pick.getBoundingClientRect();
		const label = (pick.textContent || "").trim() || (pick.getAttribute("aria-label") || "").trim() ||
			(isHighlightSendButton(pick) ? "send-highlight" : "send-icon");
		return { found: true, x: r.left + r.width / 2, y: r.top + r.height / 2, text: label };
	})()`
	var pt clickPoint
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return err
	}
	if !pt.Found {
		if pt.Error != "" {
			return fmt.Errorf("%s", pt.Error)
		}
		return fmt.Errorf("send button not found")
	}
	log.Printf("generate_video: js-click submit %q at (%.0f, %.0f)", pt.Text, pt.X, pt.Y)
	return nil
}

func readEditorText(ctx context.Context) string {
	var text string
	_ = evalReturnByValue(ctx, `(() => {`+chatEditorQueryJS+`
		function readVal(el) {
			if (!el) return "";
			const val = ('value' in el) ? (el.value || '') : (el.innerText || el.textContent || '');
			return String(val).trim();
		}
		const marked = document.querySelector('[data-doubao-chat-editor="1"]');
		if (marked && paintedChatEl(marked)) {
			return readVal(marked);
		}
		const pick = pickChatEditor();
		return pick ? readVal(pick.el) : "";
	})()`, &text)
	return text
}

func editorHasText(ctx context.Context) bool {
	return readEditorText(ctx) != ""
}

func isRetryableComposerError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, marker := range []string{
		"chat editor not found",
		"focus editor",
		"re-focus editor",
		"prompt mismatch in editor",
		"input.inserttext",
		"clear editor left stale text",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// ensurePromptInEditor types the prompt and refuses to continue if the composer
// does not contain substantially the same text (image-only / truncated submits
// produce videos that ignore the shot list).
func ensurePromptInEditor(ctx context.Context, prompt string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("refuse video generation: text prompt is empty")
	}
	wantN := utf8RuneCount(prompt)

	typeOnce := func() error {
		_ = dismissDoubaoPopups(ctx)
		if err := focusChatEditor(ctx); err != nil {
			return fmt.Errorf("focus editor: %w", err)
		}
		if renameDialogOpen(ctx) {
			_ = dismissDoubaoPopups(ctx)
			return fmt.Errorf("refuse typing: conversation rename dialog is open")
		}
		// Backspacing an already-empty ProseMirror can make React replace the
		// node, losing focus immediately before Input.insertText.
		if current := readEditorText(ctx); current != "" {
			if err := clearFocusedEditor(ctx); err != nil {
				return fmt.Errorf("clear editor: %w", err)
			}
			if err := chromedp.Run(ctx, chromedp.Sleep(200*time.Millisecond)); err != nil {
				return err
			}
			if stale := readEditorText(ctx); stale != "" {
				return fmt.Errorf("clear editor left stale text %q", truncateRunes(stale, 24))
			}
		}
		// Upload completion or clearing may replace the editor. Mark and focus
		// the current node again immediately before the editing command.
		if err := focusChatEditor(ctx); err != nil {
			return fmt.Errorf("re-focus editor before typing: %w", err)
		}
		if err := typeIntoFocused(ctx, prompt); err != nil {
			return fmt.Errorf("type prompt: %w", err)
		}
		return chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond))
	}

	if err := typeOnce(); err != nil {
		return err
	}
	got := readEditorText(ctx)
	if promptEnteredOK(prompt, got) {
		log.Printf("generate_video: editor has prompt (%d/%d runes)", utf8RuneCount(got), wantN)
		return nil
	}
	log.Printf("generate_video: editor prompt mismatch after type (got %d/%d runes, preview=%q), retry",
		utf8RuneCount(got), wantN, truncateRunes(got, 40))

	if err := typeOnce(); err != nil {
		return err
	}
	got = readEditorText(ctx)
	if !promptEnteredOK(prompt, got) {
		return fmt.Errorf("refuse video generation: prompt mismatch in editor (got %d/%d runes, preview=%q)",
			utf8RuneCount(got), wantN, truncateRunes(got, 48))
	}
	log.Printf("generate_video: editor has prompt after retry (%d/%d runes)", utf8RuneCount(got), wantN)
	return nil
}

func truncateRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

func clickSendButton(ctx context.Context) error {
	// Multi-image uploads often leave the send control disabled for 1–3s; wait
	// briefly so we don't miss it or click the wrong bottom-right control.
	deadline := time.Now().Add(4 * time.Second)
	var pt clickPoint
	var findErr error
	for {
		pt, findErr = findSubmitButtonPoint(ctx)
		if findErr == nil && pt.Found {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}

	if findErr != nil {
		return findErr
	}
	if !pt.Found {
		if pt.Error != "" {
			return fmt.Errorf("%s", pt.Error)
		}
		return fmt.Errorf("send button not found")
	}
	// Use a trusted CDP mouse event first. Doubao may ignore HTMLElement.click()
	// for the video submit control; a real manual click then works and opens the
	// material-safety dialog, which made the old JS-first path look successful
	// even though no submit event was dispatched.
	log.Printf("generate_video: mouse-click submit %q at (%.0f, %.0f)", pt.Text, pt.X, pt.Y)
	if err := chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(300*time.Millisecond),
	); err == nil {
		return nil
	} else {
		log.Printf("generate_video: mouse submit click: %v (fallback js)", err)
	}
	return clickSubmitButtonJS(ctx)
}

func keyboardSubmit(ctx context.Context) error {
	for _, mods := range []input.Modifier{input.ModifierMeta, input.ModifierCtrl, 0} {
		if err := chromedp.Run(ctx, chromedp.KeyEvent("\n", chromedp.KeyModifiers(mods))); err != nil {
			return err
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(400*time.Millisecond)); err != nil {
			return err
		}
	}
	return nil
}

func materialSafetyConfirmationText(text string) bool {
	text = strings.TrimSpace(text)
	return strings.Contains(text, "安全确认") &&
		strings.Contains(text, "充分授权") &&
		(strings.Contains(text, "侵权违法风险") || strings.Contains(text, "相关责任需由你自行承担"))
}

func materialSafetyConfirmationPresent(ctx context.Context) bool {
	var present bool
	_ = evalReturnByValue(ctx, `(() => {
		function visible(el) {
			if (!el || el.closest('[hidden], [aria-hidden="true"]')) return false;
			const st = window.getComputedStyle(el);
			const r = el.getBoundingClientRect();
			return st.display !== 'none' && st.visibility !== 'hidden' && r.width > 0 && r.height > 0;
		}
		for (const button of document.querySelectorAll('button, [role="button"]')) {
			if (!visible(button)) continue;
			const label = (button.innerText || button.textContent || '').replace(/\s+/g, '').trim();
			if (label !== '确认') continue;
			let root = button;
			for (let depth = 0; root && depth < 12; depth++, root = root.parentElement) {
				const text = (root.innerText || '').replace(/\s+/g, ' ').trim();
				const safety = text.includes('安全确认') && text.includes('充分授权') &&
					(text.includes('侵权违法风险') || text.includes('相关责任需由你自行承担'));
				if (!safety) continue;
				const hasReject = Array.from(root.querySelectorAll('button, [role="button"]')).some(el =>
					visible(el) && (el.innerText || el.textContent || '').replace(/\s+/g, '').trim() === '拒绝');
				if (hasReject) return true;
			}
		}
		return false;
	})()`, &present)
	return present
}

func clickMaterialSafetyConfirmation(ctx context.Context) error {
	const js = `(() => {
		function visible(el) {
			if (!el || el.closest('[hidden], [aria-hidden="true"]')) return false;
			const st = window.getComputedStyle(el);
			const r = el.getBoundingClientRect();
			return st.display !== 'none' && st.visibility !== 'hidden' && r.width > 0 && r.height > 0;
		}
		for (const button of document.querySelectorAll('button, [role="button"]')) {
			if (!visible(button)) continue;
			const label = (button.innerText || button.textContent || '').replace(/\s+/g, '').trim();
			if (label !== '确认') continue;
			let root = button;
			for (let depth = 0; root && depth < 12; depth++, root = root.parentElement) {
				const text = (root.innerText || '').replace(/\s+/g, ' ').trim();
				const safety = text.includes('安全确认') && text.includes('充分授权') &&
					(text.includes('侵权违法风险') || text.includes('相关责任需由你自行承担'));
				if (!safety) continue;
				const hasReject = Array.from(root.querySelectorAll('button, [role="button"]')).some(el =>
					visible(el) && (el.innerText || el.textContent || '').replace(/\s+/g, '').trim() === '拒绝');
				if (!hasReject) continue;
				const r = button.getBoundingClientRect();
				return { found: true, x: r.left + r.width / 2, y: r.top + r.height / 2, text: label };
			}
		}
		return { found: false, error: 'material safety 确认 button not found' };
	})()`
	var pt clickPoint
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return err
	}
	if !pt.Found {
		return fmt.Errorf("%s", pt.Error)
	}
	log.Printf("generate_video: mouse-click material safety confirmation at (%.0f, %.0f)", pt.X, pt.Y)
	return chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(300*time.Millisecond),
	)
}

// The API owner explicitly opted into accepting the material-rights dialog.
// Only click the exact 确认 button inside the matching safety dialog, then keep
// the same request alive until the modal has actually closed.
func waitForMaterialSafetyConfirmation(ctx context.Context) (time.Duration, error) {
	if !materialSafetyConfirmationPresent(ctx) {
		return 0, nil
	}
	started := time.Now()
	log.Printf("generate_video: material safety confirmation is open; accepting automatically")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	nextClick := time.Time{}
	for {
		if !materialSafetyConfirmationPresent(ctx) {
			paused := time.Since(started)
			log.Printf("generate_video: material safety confirmation accepted; resuming submit detection after %s", paused.Round(time.Second))
			return paused, nil
		}
		if time.Now().After(nextClick) {
			if err := clickMaterialSafetyConfirmation(ctx); err != nil {
				log.Printf("generate_video: material safety confirm click failed: %v", err)
			}
			nextClick = time.Now().Add(2 * time.Second)
		}
		select {
		case <-ctx.Done():
			return time.Since(started), ctx.Err()
		case <-ticker.C:
		}
	}
}

func trySubmitUI(ctx context.Context, beforeCount int) error {
	submitDetected := func() (bool, error) {
		if ok, reason := submitAcceptedInDOM(ctx); ok {
			log.Printf("generate_video: submit accepted via dom (%s)", reason)
			return true, nil
		} else if reason == "quota_exceeded" {
			if err := checkVideoQuotaExceeded(ctx); err != nil {
				return false, err
			}
			return false, &VideoQuotaExceededError{}
		}
		if n, _ := readSubmitCount(ctx); n > beforeCount {
			log.Printf("generate_video: submit accepted via network hook")
			return true, nil
		}
		return false, nil
	}
	resumeAfterSafetyConfirmation := func() error {
		for attempt := 0; attempt < 3; attempt++ {
			paused, err := waitForMaterialSafetyConfirmation(ctx)
			if err != nil {
				return err
			}
			if paused == 0 {
				return nil
			}
			// Doubao has two behaviours after accepting: some versions continue
			// the original submit, while others only close the modal. Detect the
			// former before looking for a send button that no longer exists.
			if ok, err := submitDetected(); err != nil {
				return err
			} else if ok {
				log.Printf("generate_video: safety confirmation continued original submit")
				return nil
			}
			if !editorHasText(ctx) {
				log.Printf("generate_video: safety confirmation cleared composer; original submit accepted")
				return nil
			}

			// The prompt is still present, so this Doubao version requires an
			// explicit second send after the overlay has gone away.
			log.Printf("generate_video: safety confirmation closed; re-submit prompt")
			if err := clickSendButton(ctx); err != nil {
				// UI replacement can race the button lookup. Re-check actual submit
				// state before turning a successful request into a false failure.
				if ok, detectErr := submitDetected(); detectErr != nil {
					return detectErr
				} else if ok || !editorHasText(ctx) {
					log.Printf("generate_video: submit accepted while safety re-submit button disappeared")
					return nil
				}
				return fmt.Errorf("submit after safety confirmation: %w", err)
			}
			time.Sleep(800 * time.Millisecond)
		}
		return fmt.Errorf("material safety confirmation repeated after 3 accepts")
	}

	if _, err := waitForHumanVerification(ctx); err != nil {
		return err
	}
	if err := clickSendButton(ctx); err != nil {
		log.Printf("generate_video: send button click failed: %v", err)
	}

	time.Sleep(800 * time.Millisecond)
	if _, err := waitForHumanVerification(ctx); err != nil {
		return err
	}
	if err := resumeAfterSafetyConfirmation(); err != nil {
		return err
	}
	if ok, err := submitDetected(); err != nil {
		return err
	} else if ok {
		return nil
	}

	// Composer already cleared: send almost certainly landed. Poll briefly for
	// ack / network hook instead of treating this as failure (Doubao may show
	// 「收到，即将为您生成视频」a second later, and the editor marker can flicker).
	if !editorHasText(ctx) {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := waitForHumanVerification(ctx); err != nil {
				return err
			}
			if ok, err := submitDetected(); err != nil {
				return err
			} else if ok {
				return nil
			}
			if videoGenerationAcknowledged(ctx) || videoGenerationComplete(ctx) {
				log.Printf("generate_video: submit accepted via generation acknowledgement")
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(400 * time.Millisecond):
			}
		}
		if !editorHasText(ctx) {
			log.Printf("generate_video: submit accepted via empty composer after wait")
			return nil
		}
	}

	// Retry once — long prompts / image previews can delay enabling the send button.
	if editorHasText(ctx) {
		if _, err := waitForHumanVerification(ctx); err != nil {
			return err
		}
		log.Printf("generate_video: input still filled, retry submit click")
		_ = focusChatEditor(ctx)
		time.Sleep(300 * time.Millisecond)
		if err := clickSendButton(ctx); err != nil {
			log.Printf("generate_video: retry send button click failed: %v", err)
		}
		time.Sleep(800 * time.Millisecond)
		if _, err := waitForHumanVerification(ctx); err != nil {
			return err
		}
		if err := resumeAfterSafetyConfirmation(); err != nil {
			return err
		}
		if ok, err := submitDetected(); err != nil {
			return err
		} else if ok {
			return nil
		}

		if _, err := waitForHumanVerification(ctx); err != nil {
			return err
		}
		log.Printf("generate_video: input still filled, try keyboard submit")
		if err := keyboardSubmit(ctx); err != nil {
			return fmt.Errorf("keyboard submit: %w", err)
		}
		time.Sleep(800 * time.Millisecond)
		if err := resumeAfterSafetyConfirmation(); err != nil {
			return err
		}
		if ok, err := submitDetected(); err != nil {
			return err
		} else if ok {
			return nil
		}
		if !editorHasText(ctx) {
			log.Printf("generate_video: submit accepted via empty composer after keyboard")
			return nil
		}
	}

	// Prefer the real Doubao reason (quota / policy) over a generic submit miss —
	// users otherwise think the confirm/send button is missing.
	if err := checkVideoUIErrors(ctx); err != nil {
		return err
	}
	return fmt.Errorf("submit not detected — composer still has text or no generation ack")
}

func currentChatConversationID(ctx context.Context) string {
	var id string
	_ = evalReturnByValue(ctx, `(() => {
		const m = location.pathname.match(/\/chat\/(\d+)/);
		return m ? m[1] : "";
	})()`, &id)
	return id
}

func ensureFreshConversation(ctx context.Context, labels ...string) error {
	before := currentChatConversationID(ctx)
	if before != "" {
		// Soft-navigating away from a finished video chat often leaves the skill
		// bar half-initialized: 「视频生成」clicks succeed but model chips never
		// mount. Hard refresh clears that residue before the next skill entry.
		log.Printf("generate_video: leaving conversation %s for fresh chat (hard refresh)", before)
		if err := hardRefreshChatPage(ctx); err != nil {
			log.Printf("generate_video: hard refresh after leave failed (%v), soft reset", err)
			if err := resetToFreshChat(ctx, labels...); err != nil {
				return err
			}
		}
		return nil
	}
	// We are already on a fresh, fully mounted landing page. Clicking 新对话
	// again is not harmless: Doubao's SPA occasionally replaces the centre with
	// a blank shell while leaving the bottom composer mounted.
	if newChatLandingMounted(ctx) {
		log.Printf("generate_video: already on complete new-chat landing; reuse without clicking 新对话")
		return dismissDoubaoPopups(ctx)
	}

	if err := clickNewChatControl(ctx, labels...); err != nil {
		log.Printf("generate_video: new conversation click: %v (try navigate)", err)
		return resetToFreshChat(ctx, labels...)
	}

	if err := chromedp.Run(ctx, chromedp.Sleep(800*time.Millisecond)); err != nil {
		return err
	}

	if after := currentChatConversationID(ctx); after != "" {
		log.Printf("generate_video: still on conversation %s, navigating to fresh chat", after)
		return resetToFreshChat(ctx, labels...)
	}

	return waitForNewChatReady(ctx, 20*time.Second, labels...)
}

func newChatLandingMounted(ctx context.Context) bool {
	var ready bool
	_ = evalReturnByValue(ctx, `(() => {
		if ((location.pathname.match(/\/chat\/(\d+)/) || [])[1]) return false;
		const text = (document.body && document.body.innerText) || '';
		const landing = /有什么我能帮你的|有什么可以帮你|我能帮你做什么|为你推荐|How can I help|What can I help/i.test(text);
		if (!landing) return false;
		for (const el of document.querySelectorAll('textarea, [contenteditable]:not([contenteditable="false"]), [role="textbox"]')) {
			const st = window.getComputedStyle(el);
			const r = el.getBoundingClientRect();
			if (st.display !== 'none' && st.visibility !== 'hidden' && r.width > 160 && r.bottom > window.innerHeight * 0.5) return true;
		}
		return false;
	})()`, &ready)
	return ready
}

func resetToFreshChat(ctx context.Context, labels ...string) error {
	if len(labels) == 0 {
		labels = []string{"新对话", "New Chat", "新办公任务"}
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(chatURL),
		chromedp.Sleep(1200*time.Millisecond),
	); err != nil {
		return err
	}
	_ = dismissDoubaoPopups(ctx)

	// Doubao SPA often redirects /chat/ back to the last conversation.
	// Keep forcing "新对话" until we land on a blank chat (or timeout).
	for attempt := 1; attempt <= 4; attempt++ {
		if id := currentChatConversationID(ctx); id == "" {
			break
		} else {
			log.Printf("generate_video: still on conversation %s after navigate (attempt %d), click 新对话", id, attempt)
		}
		if err := clickNewChatControl(ctx, labels...); err != nil {
			log.Printf("generate_video: 新对话 click attempt %d: %v", attempt, err)
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(1000*time.Millisecond)); err != nil {
			return err
		}
		_ = dismissDoubaoPopups(ctx)
	}
	return waitForNewChatReady(ctx, 25*time.Second, labels...)
}

func clickNewChatControl(ctx context.Context, labels ...string) error {
	if err := clickFirstLabel(ctx, labels...); err == nil {
		return nil
	}
	// Icon / aria-label based sidebar "新对话" control.
	var pt clickPoint
	js := `(() => {
		function isVisible(el) {
			if (!el || el.closest('[hidden], [aria-hidden="true"]')) return false;
			const st = window.getComputedStyle(el);
			if (st.display === 'none' || st.visibility === 'hidden' || parseFloat(st.opacity) === 0) return false;
			const r = el.getBoundingClientRect();
			return r.width > 4 && r.height > 4;
		}
		const needles = ["新对话", "New Chat", "新办公任务", "新建对话", "new chat"];
		const candidates = [];
		for (const el of document.querySelectorAll('button, [role="button"], a, div, span')) {
			if (!isVisible(el)) continue;
			const text = (el.textContent || "").trim().replace(/\s+/g, " ");
			const aria = (el.getAttribute("aria-label") || "") + " " + (el.getAttribute("title") || "");
			const hay = (text + " " + aria).toLowerCase();
			let hit = false;
			for (const n of needles) {
				if (hay.includes(n.toLowerCase())) { hit = true; break; }
			}
			if (!hit) continue;
			// Prefer compact sidebar controls over large page banners.
			const r = el.getBoundingClientRect();
			if (r.width > 420 || r.height > 120) continue;
			candidates.push({ el, area: r.width * r.height, left: r.left, top: r.top });
		}
		if (!candidates.length) return { found: false, error: "new chat control not found" };
		candidates.sort((a, b) => a.left - b.left || a.area - b.area || a.top - b.top);
		const el = candidates[0].el;
		el.scrollIntoView({ block: "center", inline: "center" });
		const r = el.getBoundingClientRect();
		return { found: true, x: r.left + r.width / 2, y: r.top + r.height / 2, text: (el.textContent || "").trim().slice(0, 40) };
	})()`
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return err
	}
	if !pt.Found {
		if pt.Error != "" {
			return errors.New(pt.Error)
		}
		return fmt.Errorf("new chat control not found")
	}
	log.Printf("generate_video: click new-chat control (%s)", pt.Text)
	return chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(400*time.Millisecond),
	)
}

func waitForNewChatReady(ctx context.Context, timeout time.Duration, labels ...string) error {
	if len(labels) == 0 {
		labels = []string{"新对话", "New Chat", "新办公任务"}
	}
	deadline := time.Now().Add(timeout)
	var lastHint, lastURL string
	lastClick := time.Time{}
	blankSince := time.Time{}
	reloadedBlankShell := false
	stableReady := 0
	for time.Now().Before(deadline) {
		var out struct {
			Ready        bool   `json:"ready"`
			HasEditor    bool   `json:"hasEditor"`
			LandingReady bool   `json:"landingReady"`
			Hint         string `json:"hint"`
			URL          string `json:"url"`
			ConvID       string `json:"convId"`
		}
		js := videoToolbarJSShared + `(() => {
			const convID = (location.pathname.match(/\/chat\/(\d+)/) || [])[1] || "";
			const editors = [...document.querySelectorAll('textarea, [contenteditable]:not([contenteditable="false"]), [role="textbox"]')].filter(isVisible);
			const ph = editors.length ? (editors[editors.length - 1].getAttribute('placeholder') || editors[editors.length - 1].getAttribute('data-placeholder') || '') : '';
			const vh = window.innerHeight;
			let skillBtn = false;
			for (const el of document.querySelectorAll('button, [role="button"], div, span')) {
				if (!isVisible(el)) continue;
				if ((el.textContent || '').trim() !== '视频生成') continue;
				if (el.getBoundingClientRect().top > vh * 0.45) { skillBtn = true; break; }
			}
			if (convID) {
				return { ready: false, hint: "conversation:" + convID, url: location.href, convId: convID };
			}
			const hasEditor = editors.length > 0;
			const placeholderOK = /有什么我能帮|发消息或按住|描述你想|输入|问问/.test(ph) || ph === '';
			const bodyText = (document.body && document.body.innerText) || '';
			// The broken SPA shell also paints the bottom editor, so editor presence
			// alone is not a readiness signal. Wait for the actual new-chat landing
			// content before switching skills or attaching reference files.
			const landingReady = /有什么我能帮你的|有什么可以帮你|我能帮你做什么|为你推荐|How can I help|What can I help/i.test(bodyText);
			const ready = hasEditor && landingReady && (skillBtn || placeholderOK);
			return {
				ready,
				hasEditor,
				landingReady,
				hint: ready ? (ph.slice(0, 40) || "landing") : (hasEditor ? "blank_shell" : "waiting_editor"),
				url: location.href,
				convId: "",
			};
		})()`
		if err := evalReturnByValue(ctx, js, &out); err != nil {
			return err
		}
		lastHint, lastURL = out.Hint, out.URL
		if out.Ready {
			stableReady++
			if stableReady >= 2 {
				log.Printf("generate_video: new chat landing ready and stable (%s)", out.Hint)
				return dismissDoubaoPopups(ctx)
			}
		} else {
			stableReady = 0
		}
		if out.ConvID == "" && out.HasEditor && !out.LandingReady {
			if blankSince.IsZero() {
				blankSince = time.Now()
			}
			if !reloadedBlankShell && time.Since(blankSince) >= 4*time.Second {
				log.Printf("generate_video: new-chat editor mounted but landing is blank; hard reload /chat")
				if err := chromedp.Run(ctx,
					chromedp.Navigate(chatURL),
					chromedp.Sleep(1200*time.Millisecond),
				); err != nil {
					return err
				}
				reloadedBlankShell = true
				blankSince = time.Time{}
				_ = dismissDoubaoPopups(ctx)
				continue
			}
		} else {
			blankSince = time.Time{}
		}
		// Keep escaping sticky last-conversation redirects.
		if out.ConvID != "" && time.Since(lastClick) > 2*time.Second {
			log.Printf("generate_video: waiting on conversation %s, retry 新对话", out.ConvID)
			_ = clickNewChatControl(ctx, labels...)
			lastClick = time.Now()
			_ = dismissDoubaoPopups(ctx)
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond)); err != nil {
			return err
		}
	}
	return fmt.Errorf("new chat page not ready (hint=%s url=%s)", lastHint, lastURL)
}

func ensureNewChat(ctx context.Context) error {
	leftConvo := currentChatConversationID(ctx) != ""
	if err := ensureFreshConversation(ctx, "新对话", "New Chat"); err != nil {
		return err
	}
	if leftConvo {
		return settleFreshChatBeforeVideoSkill(ctx)
	}
	return nil
}

func ensureNewOfficeTask(ctx context.Context) error {
	return ensureFreshConversation(ctx, "新办公任务", "新对话")
}

func ensureNewSession(ctx context.Context, officeMode bool) error {
	if officeMode {
		return ensureNewOfficeTask(ctx)
	}
	return ensureNewChat(ctx)
}

func attachRefFileViaUI(ctx context.Context, refKey, mediaType string, officeMode bool) error {
	refKeyLiteral, err := json.Marshal(refKey)
	if err != nil {
		return err
	}
	fileType := "image"
	fileFormat := "png"
	defaultName := "ref.png"
	defaultMIME := "image/png"
	if mediaType == "audio" {
		fileType = "audio"
		fileFormat = "mp3"
		defaultName = "ref.mp3"
		defaultMIME = "audio/mpeg"
	}
	attachJS := fmt.Sprintf(`(async () => {
		const refKey = %s;
		const officeMode = %t;
		const fileType = %s;
		const fileFormat = %s;
		const defaultName = %s;
		const defaultMIME = %s;
%s

		const params = new URLSearchParams({
			aid: "497858", device_platform: "web", language: "zh", pkg_type: "release_version",
			real_aid: "497858", region: "CN", samantha_web: "1", sys_region: "CN",
			use_olympus_account: "1", version_code: "20800",
		});
		const res = await fetch("/alice/message/get_file_url?" + params.toString(), {
			method: "POST",
			headers: { "Content-Type": "application/json", Referer: %q, Origin: %q },
			body: JSON.stringify({ uris: [refKey], type: fileType, format: fileFormat, expire_second: 3600 }),
			credentials: "include",
		});
		const body = await res.json();
		const fileUrl = (body?.data?.file_urls || [])[0]?.main_url || "";
		if (!fileUrl) return { ok: false, error: "resolve ref file url failed" };

		const input = await revealFileInput(officeMode);
		if (!input) return { ok: false, error: "file input not found" };

		const resp = await fetch(fileUrl, { credentials: "include" });
		const blob = await resp.blob();
		const file = new File([blob], defaultName, { type: blob.type || defaultMIME });
		const dt = new DataTransfer();
		dt.items.add(file);
		input.files = dt.files;
		input.dispatchEvent(new Event("change", { bubbles: true }));
		input.dispatchEvent(new Event("input", { bubbles: true }));
		return { ok: true };
	})()`,
		string(refKeyLiteral),
		officeMode,
		jsonString(fileType),
		jsonString(fileFormat),
		jsonString(defaultName),
		jsonString(defaultMIME),
		fileInputHelperJS,
		chatURL,
		doubaoBaseURL,
	)

	type attachResult struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	var attached attachResult
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		v, exp, evalErr := runtime.Evaluate(attachJS).WithAwaitPromise(true).WithReturnByValue(true).Do(ctx)
		if evalErr != nil {
			return evalErr
		}
		if exp != nil {
			return exp
		}
		return json.Unmarshal(v.Value, &attached)
	})); err != nil {
		return fmt.Errorf("attach ref %s: %w", mediaType, err)
	}
	if !attached.OK {
		return fmt.Errorf("attach ref %s: %s", mediaType, attached.Error)
	}
	log.Printf("generate_video: attached ref %s via UI", mediaType)
	return nil
}

func attachLocalFileViaUI(ctx context.Context, data []byte, filename, mediaType string, officeMode bool) error {
	if len(data) == 0 {
		return fmt.Errorf("attach local %s: empty data", mediaType)
	}
	if filename == "" {
		filename = "ref.bin"
	}
	defaultMIME := "application/octet-stream"
	defaultName := filename
	if mediaType == "audio" {
		defaultMIME = audioMIME(mediaExt(filename))
		if !strings.Contains(filename, ".") {
			defaultName = filename + ".mp3"
		}
	} else if mediaType == "image" {
		meta := resolveUploadImageMeta(data, filename)
		defaultMIME = meta.MIME
		defaultName = meta.UploadName
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	attachJS := fmt.Sprintf(`(async () => {
		const officeMode = %t;
		const defaultName = %s;
		const defaultMIME = %s;
		const bytes = Uint8Array.from(atob(%q), c => c.charCodeAt(0));
%s

		const input = await revealFileInput(officeMode);
		if (!input) return { ok: false, error: "file input not found" };

		const blob = new Blob([bytes], { type: defaultMIME });
		const file = new File([blob], defaultName, { type: blob.type || defaultMIME });
		const dt = new DataTransfer();
		dt.items.add(file);
		input.files = dt.files;
		input.dispatchEvent(new Event("change", { bubbles: true }));
		input.dispatchEvent(new Event("input", { bubbles: true }));
		return { ok: true };
	})()`, officeMode, jsonString(defaultName), jsonString(defaultMIME), b64, fileInputHelperJS)

	type attachResult struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	var attached attachResult
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		v, exp, evalErr := runtime.Evaluate(attachJS).WithAwaitPromise(true).WithReturnByValue(true).Do(ctx)
		if evalErr != nil {
			return evalErr
		}
		if exp != nil {
			return exp
		}
		return json.Unmarshal(v.Value, &attached)
	})); err != nil {
		return fmt.Errorf("attach local %s: %w", mediaType, err)
	}
	if !attached.OK {
		return fmt.Errorf("attach local %s: %s", mediaType, attached.Error)
	}
	log.Printf("generate_video: attached local %s via UI (%s, %d bytes)", mediaType, defaultName, len(data))
	return nil
}

func imageMIME(ext string) string {
	switch ext {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

func videoGenerationPending(ctx context.Context, submittedAt time.Time) bool {
	if videoGenerateActionButtonPending(ctx) {
		// Assistant showed the in-message 「生成视频」button — not started yet.
		return false
	}
	// Hard failures end the job — never keep the 90s "pending" window alive.
	if checkVideoGenerationFailed(ctx) != nil {
		return false
	}

	// Page-level completion wins over a stale 「预计等待 / 本次使用」bubble.
	// Doubao keeps the ETA ack visible after 「你的视频生成好了」, and
	// latestAssistantMessageJS may still land on the older ack node.
	var bodyTail string
	_ = evalReturnByValue(ctx, `(() => (document.body.innerText || "").slice(-3500))()`, &bodyTail)
	if textIndicatesVideoComplete(bodyTail) {
		return false
	}

	var latest string
	if err := evalReturnByValue(ctx, latestAssistantMessageJS, &latest); err == nil {
		if textIndicatesVideoComplete(latest) {
			return false
		}
		if textIndicatesVideoGenerating(latest) {
			return true
		}
	}
	if textIndicatesVideoGenerating(bodyTail) {
		return true
	}
	// No clear assistant cue yet: keep pending shortly after submit so stale page/SSE
	// videos from other conversations are not mistaken for this job.
	if !submittedAt.IsZero() && time.Since(submittedAt) < 90*time.Second {
		return true
	}
	return false
}

func videoGenerationComplete(ctx context.Context) bool {
	var bodyTail string
	if err := evalReturnByValue(ctx, `(() => (document.body.innerText || "").slice(-3500))()`, &bodyTail); err == nil {
		if textIndicatesVideoComplete(bodyTail) {
			return true
		}
	}
	var latest string
	if err := evalReturnByValue(ctx, latestAssistantMessageJS, &latest); err == nil {
		if textIndicatesVideoComplete(latest) {
			return true
		}
	}
	return false
}

func tryActivateVideoPlayer(ctx context.Context) bool {
	var pt clickPoint
	const js = `(() => {
		const vh = window.innerHeight, vw = window.innerWidth;
		const candidates = [];
		function inComposer(r) {
			// Bottom input / attachment strip — never click ref-image thumbs here.
			return r.bottom > vh - 200 || r.top > vh * 0.78;
		}
		function inSidebar(r) {
			return r.right < vw * 0.28;
		}
		function nearComplete(r) {
			const body = document.body.innerText || "";
			if (!/你的视频生成好了|视频已生成完成|视频生成完成|视频生成好了/.test(body)) return 0;
			// Prefer mid-chat region where the completion bubble usually sits.
			if (r.top > vh * 0.12 && r.bottom < vh * 0.78) return 50000;
			return 0;
		}
		function addCandidate(el) {
			if (!el || !el.offsetParent) return;
			const r = el.getBoundingClientRect();
			if (r.width < 80 || r.height < 60) return;
			if (inComposer(r) || inSidebar(r)) return;
			// Tiny square chips / icons are not the generated cover.
			if (r.width < 120 && r.height < 120) return;
			const area = r.width * r.height;
			const score = area + nearComplete(r);
			candidates.push({ el, score, x: r.left + r.width / 2, y: r.top + r.height / 2 });
		}
		for (const sel of [
			'[class*="thumb-video"] img',
			'[class*="block-video"] img',
			'[class*="block-video"]',
			'[class*="video-card"] img',
			'[class*="video-card"]',
			'[class*="md-video"] img',
			'[class*="md-video"]',
			'video',
			'[class*="xgplayer"]',
		]) {
			for (const el of document.querySelectorAll(sel)) addCandidate(el);
		}
		// Fallback: large images in the message list (not composer), often the cover poster.
		if (!candidates.length) {
			for (const el of document.querySelectorAll('img')) {
				if (!el.offsetParent) continue;
				const r = el.getBoundingClientRect();
				if (inComposer(r) || inSidebar(r)) continue;
				if (r.width < 160 || r.height < 90) continue;
				const src = (el.currentSrc || el.src || "").toLowerCase();
				if (!src || src.startsWith("data:")) continue;
				candidates.push({
					el,
					score: r.width * r.height + nearComplete(r),
					x: r.left + r.width / 2,
					y: r.top + r.height / 2,
				});
			}
		}
		if (!candidates.length) return { found: false };
		candidates.sort((a, b) => b.score - a.score);
		const best = candidates[0];
		best.el.scrollIntoView({ block: 'center', inline: 'center' });
		const r2 = best.el.getBoundingClientRect();
		return { found: true, x: r2.left + r2.width / 2, y: r2.top + r2.height / 2 };
	})()`
	if err := evalReturnByValue(ctx, js, &pt); err != nil || !pt.Found {
		return false
	}
	log.Printf("generate_video: click video cover to load player at (%.0f, %.0f)", pt.X, pt.Y)
	if err := chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(2*time.Second),
	); err != nil {
		return false
	}
	return true
}

// tryClickVideoDownload clicks the in-player / message 「下载」control so Chrome
// emits a downloadWillBegin / media request we can capture as the video URL.
func tryClickVideoDownload(ctx context.Context) bool {
	var pt clickPoint
	const js = `(() => {
		const vw = window.innerWidth, vh = window.innerHeight;
		const candidates = [];
		function score(el, label) {
			const r = el.getBoundingClientRect();
			if (r.width < 12 || r.height < 12) return -1;
			if (r.top < 40 || r.top > vh - 80) return -1;
			// Prefer controls near the main canvas / message video, not sidebar.
			if (r.right < vw * 0.28) return -1;
			let s = 50;
			if (label === '下载' || label === '下载视频') s += 80;
			if (/download/i.test(el.getAttribute('aria-label') || '') ||
				/download/i.test(el.getAttribute('title') || '')) s += 70;
			if (el.closest('[class*="video"], [class*="xgplayer"], [class*="block-video"], [class*="canvas"]')) s += 60;
			if (r.top > vh * 0.2 && r.top < vh * 0.85) s += 20;
			return s;
		}
		for (const el of document.querySelectorAll('button, [role="button"], a, div, span')) {
			if (!el.offsetParent) continue;
			const aria = ((el.getAttribute('aria-label') || '') + ' ' + (el.getAttribute('title') || '')).trim();
			const text = (el.textContent || '').trim().replace(/\s+/g, ' ');
			const label = text.length <= 8 ? text : '';
			const looksDownload = label === '下载' || label === '下载视频' ||
				/^下载/.test(label) || /download/i.test(aria);
			if (!looksDownload) continue;
			const s = score(el, label || 'download');
			if (s < 40) continue;
			const r = el.getBoundingClientRect();
			candidates.push({ s, x: r.left + r.width / 2, y: r.top + r.height / 2, text: label || aria.slice(0, 24) });
		}
		if (!candidates.length) return { found: false };
		candidates.sort((a, b) => b.s - a.s);
		const best = candidates[0];
		return { found: true, x: best.x, y: best.y, text: best.text };
	})()`
	if err := evalReturnByValue(ctx, js, &pt); err != nil || !pt.Found {
		return false
	}
	log.Printf("generate_video: click download control %q at (%.0f, %.0f)", pt.Text, pt.X, pt.Y)
	if err := chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(2*time.Second),
	); err != nil {
		return false
	}
	return true
}

type videoURLRecoverState struct {
	PlayerActivated bool
	DownloadClicked bool
	ChainReloaded   bool
	LastDiagAt      time.Time
	LastPlayerAt    time.Time
	LastFallbackAt  time.Time
}

func logVideoExtractDiagnostics(ctx context.Context, where string, cdpCount int) {
	var diag struct {
		VideoTags   int      `json:"videoTags"`
		BlobTags    int      `json:"blobTags"`
		HTTPTags    int      `json:"httpTags"`
		PerfURLs    int      `json:"perfURLs"`
		CapURLs     int      `json:"capURLs"`
		CapFBs      int      `json:"capFBs"`
		CapVids     int      `json:"capVids"`
		Chunks      int      `json:"chunks"`
		ChunkLens   []int    `json:"chunkLens"`
		ChunkHint   string   `json:"chunkHint"`
		SampleURLs  []string `json:"sampleURLs"`
		HasComplete bool     `json:"hasComplete"`
		HTMLFplay   bool     `json:"htmlFplay"`
	}
	const js = `(() => {
		function isPlayable(u) {
			if (!u || u.startsWith("blob:")) return false;
			return /\.(mp4|m3u8|webm|mov)(\?|$)/i.test(u) ||
				/douyinvod|mime_type=video_mp4|\/video\/tos\//i.test(u);
		}
		let videoTags = 0, blobTags = 0, httpTags = 0;
		const samples = [];
		for (const v of document.querySelectorAll("video")) {
			if (!v.offsetParent) continue;
			videoTags++;
			const srcs = [v.src, v.currentSrc].filter(Boolean);
			for (const s of srcs) {
				if (s.startsWith("blob:")) blobTags++;
				else if (isPlayable(s)) { httpTags++; if (samples.length < 3) samples.push(s.slice(0, 120)); }
			}
		}
		let perfURLs = 0;
		try {
			for (const e of performance.getEntriesByType("resource")) {
				if (isPlayable(e.name || "")) {
					perfURLs++;
					if (samples.length < 3) samples.push(String(e.name).slice(0, 120));
				}
			}
		} catch (err) {}
		const cap = window.__doubaoVideoCapture || {};
		const chunks = cap.chunks || [];
		const chunkLens = chunks.slice(-3).map(c => (c || "").length);
		let chunkHint = "";
		for (let i = chunks.length - 1; i >= 0 && !chunkHint; i--) {
			const c = String(chunks[i] || "");
			if (/fallback_api|fplay|video_url|"vid"|v0[A-Za-z0-9_-]{8,}/.test(c)) {
				chunkHint = c.slice(0, 160).replace(/\s+/g, " ");
			}
		}
		const body = (document.body.innerText || "").slice(-2000);
		let html = "";
		try { html = document.documentElement.innerHTML || ""; } catch (e) {}
		return {
			videoTags, blobTags, httpTags, perfURLs,
			capURLs: (cap.videoURLs || []).length,
			capFBs: (cap.fallbackApis || []).length,
			capVids: (cap.vids || []).length,
			chunks: chunks.length,
			chunkLens,
			chunkHint,
			sampleURLs: samples,
			hasComplete: /你的视频生成好了|视频已生成完成|视频生成完成|视频生成好了/.test(body),
			htmlFplay: /fplay|fallback_api/.test(html),
		};
	})()`
	if err := evalReturnByValue(ctx, js, &diag); err != nil {
		log.Printf("generate_video: extract diag (%s): %v", where, err)
		return
	}
	log.Printf("generate_video: extract diag (%s): tags=%d blob=%d http=%d perf=%d cap=%d cdp=%d chunks=%d fb=%d vids=%d htmlFplay=%v complete=%v lens=%v hint=%q samples=%v",
		where, diag.VideoTags, diag.BlobTags, diag.HTTPTags, diag.PerfURLs, diag.CapURLs, cdpCount, diag.Chunks,
		diag.CapFBs, diag.CapVids, diag.HTMLFplay, diag.HasComplete, diag.ChunkLens, diag.ChunkHint, diag.SampleURLs)
}

// latestAssistantMessageJS returns text from the newest assistant/chat bubble (not full page history).
const latestAssistantMessageJS = `(() => {
	function latestAssistantBlock() {
		const selectors = [
			'[class*="assistant"]',
			'[class*="message-content"]',
			'[class*="Message"]',
			'main [class*="item"]',
		];
		for (const sel of selectors) {
			const nodes = [...document.querySelectorAll(sel)].filter(el => el.offsetParent);
			if (nodes.length) {
				const t = (nodes[nodes.length - 1].innerText || nodes[nodes.length - 1].textContent || '').trim();
				if (t.length >= 20) return t;
			}
		}
		const body = document.body.innerText || "";
		return body.slice(-1200);
	}
	return latestAssistantBlock();
})()`

func videoGenerationConfirmPending(ctx context.Context) bool {
	// Generation already started — do not spam 「确认」or switch chats.
	if videoGenerationAcknowledged(ctx) && !videoGenerateActionButtonPending(ctx) {
		return false
	}
	if videoGenerateActionButtonPending(ctx) {
		return true
	}
	var latest string
	if err := evalReturnByValue(ctx, latestAssistantMessageJS, &latest); err != nil {
		return false
	}
	return textNeedsVideoConfirm(latest)
}

func videoGenerationAcknowledged(ctx context.Context) bool {
	var latest string
	if err := evalReturnByValue(ctx, latestAssistantMessageJS, &latest); err == nil {
		if textIndicatesVideoGenerating(latest) {
			return true
		}
	}
	var bodyTail string
	_ = evalReturnByValue(ctx, `(() => (document.body.innerText || "").slice(-2500))()`, &bodyTail)
	return textIndicatesVideoGenerating(bodyTail)
}

func readVideoETA(ctx context.Context) (VideoETA, bool) {
	var latest string
	if err := evalReturnByValue(ctx, latestAssistantMessageJS, &latest); err == nil {
		if eta, ok := parseVideoETA(latest); ok {
			return eta, true
		}
	}
	var bodyTail string
	_ = evalReturnByValue(ctx, `(() => (document.body.innerText || "").slice(-2500))()`, &bodyTail)
	return parseVideoETA(bodyTail)
}

// findVideoGenerateActionButton locates the in-message 「生成视频」CTA
// (not the bottom toolbar skill chip 「视频生成」, and not sidebar chat titles).
func findVideoGenerateActionButton(ctx context.Context) (clickPoint, error) {
	const js = `(() => {
		const vh = window.innerHeight;
		const vw = window.innerWidth;
		function inSidebar(el) {
			let n = el;
			for (let i = 0; i < 8 && n; i++, n = n.parentElement) {
				const cls = String(n.className || '');
				const role = n.getAttribute('role') || '';
				if (/sidebar|side-bar|history|conversation-list|nav|menu|aside/i.test(cls + ' ' + role)) return true;
				if (n.tagName === 'ASIDE' || n.tagName === 'NAV') return true;
			}
			const r = el.getBoundingClientRect();
			// Left history rail.
			if (r.right < vw * 0.28 && r.width < vw * 0.32) return true;
			return false;
		}
		const candidates = [];
		for (const el of document.querySelectorAll('button, [role="button"]')) {
			if (!el.offsetParent) continue;
			if (inSidebar(el)) continue;
			const t = (el.textContent || '').replace(/\s+/g, '').trim();
			// Exact label on the confirm card. Skill chip is 「视频生成」 (different order).
			if (t !== '生成视频') continue;
			const r = el.getBoundingClientRect();
			if (r.width < 48 || r.height < 24) continue;
			if (r.width > 280) continue;
			// Prefer mid-page message actions; skip bottom composer (~last 140px).
			if (r.top > vh - 140) continue;
			if (r.top < 56) continue;
			candidates.push({
				x: r.left + r.width / 2,
				y: r.top + r.height / 2,
				area: r.width * r.height,
				yScore: r.top,
				text: t,
			});
		}
		if (!candidates.length) return { found: false };
		candidates.sort((a, b) => b.yScore - a.yScore || b.area - a.area);
		const best = candidates[0];
		return { found: true, x: best.x, y: best.y, text: best.text };
	})()`
	var pt clickPoint
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return pt, err
	}
	return pt, nil
}

func videoGenerateActionButtonPending(ctx context.Context) bool {
	pt, err := findVideoGenerateActionButton(ctx)
	return err == nil && pt.Found
}

func clickVideoGenerateActionButton(ctx context.Context) error {
	pt, err := findVideoGenerateActionButton(ctx)
	if err != nil {
		return err
	}
	if !pt.Found {
		return fmt.Errorf("生成视频 action button not found")
	}
	log.Printf("generate_video: click in-message 生成视频 at (%.0f, %.0f)", pt.X, pt.Y)
	return chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(1200*time.Millisecond),
	)
}

func confirmAcceptedAfterReply(ctx context.Context, beforeCount int) bool {
	if videoGenerateActionButtonPending(ctx) {
		return false
	}
	if videoGenerationAcknowledged(ctx) {
		return true
	}
	if n, err := readSubmitCount(ctx); err == nil && n > beforeCount && !videoGenerationConfirmPending(ctx) {
		return true
	}
	return false
}

func confirmViaChatReply(ctx context.Context, beforeCount int) (bool, error) {
	for _, reply := range []string{"确认", "开始生成"} {
		if _, err := waitForHumanVerification(ctx); err != nil {
			return false, err
		}
		if err := focusChatEditor(ctx); err != nil {
			return false, err
		}
		if err := typeIntoFocused(ctx, reply); err != nil {
			return false, err
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(400*time.Millisecond)); err != nil {
			return false, err
		}
		submitBefore := beforeCount
		if n, err := readSubmitCount(ctx); err == nil {
			submitBefore = n
		}
		if err := trySubmitUI(ctx, submitBefore); err != nil {
			log.Printf("generate_video: confirm chat submit (%q): %v (try keyboard)", reply, err)
			if err := keyboardSubmit(ctx); err != nil {
				return false, err
			}
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(1500*time.Millisecond)); err != nil {
			return false, err
		}
		if confirmAcceptedAfterReply(ctx, beforeCount) {
			log.Printf("generate_video: confirmed via chat reply %q", reply)
			return true, nil
		}
	}
	return false, nil
}

func tryConfirmVideoGeneration(ctx context.Context, beforeCount int) (bool, error) {
	if _, err := waitForHumanVerification(ctx); err != nil {
		return false, err
	}
	// Already generating — never type 「确认」or click sidebar-looking targets.
	if videoGenerationAcknowledged(ctx) && !videoGenerateActionButtonPending(ctx) {
		return false, nil
	}

	// New Doubao card: images + ETA text + 「生成视频」button (no "请确认参数" copy).
	if videoGenerateActionButtonPending(ctx) {
		if err := clickVideoGenerateActionButton(ctx); err != nil {
			log.Printf("generate_video: click 生成视频 action: %v", err)
			return false, nil
		}
		if !videoGenerateActionButtonPending(ctx) || confirmAcceptedAfterReply(ctx, beforeCount) {
			log.Printf("generate_video: confirmed via in-message 生成视频 button")
			return true, nil
		}
		// Do NOT fall through to typing 「确认」— that spams other chats.
		return false, nil
	}

	var latest string
	_ = evalReturnByValue(ctx, latestAssistantMessageJS, &latest)
	if !textNeedsVideoConfirm(latest) {
		return false, nil
	}

	// Office / parameter-confirm mode only: type "确认" or "开始生成".
	if ok, err := confirmViaChatReply(ctx, beforeCount); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}

	labels := []string{
		"确认参数后生成视频",
		"确认并生成视频",
		"确认生成视频",
		"确认并生成",
		"确认生成",
		"开始生成",
	}
	for _, label := range labels {
		if err := clickByLabel(ctx, label, true); err == nil {
			if err := chromedp.Run(ctx, chromedp.Sleep(800*time.Millisecond)); err != nil {
				return false, err
			}
			if confirmAcceptedAfterReply(ctx, beforeCount) {
				log.Printf("generate_video: confirmed via button %q", label)
				return true, nil
			}
		}
	}

	var pt clickPoint
	const js = `(() => {
		const targets = ["确认参数后生成视频", "确认并生成", "确认生成", "开始生成"];
		const vh = window.innerHeight;
		const vw = window.innerWidth;
		const candidates = [];
		for (const target of targets) {
			for (const el of document.querySelectorAll('button, [role="button"]')) {
				if (!el.offsetParent) continue;
				const t = (el.textContent || '').trim();
				if (!t || t.length > 48) continue;
				if (t !== target && !t.includes(target)) continue;
				const r = el.getBoundingClientRect();
				if (r.right < vw * 0.28) continue; // skip sidebar
				if (r.top > vh - 140) continue;
				candidates.push({ el, t, len: t.length, target });
			}
		}
		if (!candidates.length) return { found: false, error: "confirm button not found" };
		candidates.sort((a, b) => a.len - b.len);
		const el = candidates[0].el;
		el.scrollIntoView({ block: 'center', inline: 'center' });
		const r = el.getBoundingClientRect();
		return { found: true, x: r.left + r.width / 2, y: r.top + r.height / 2 };
	})()`
	if err := evalReturnByValue(ctx, js, &pt); err != nil {
		return false, err
	}
	if !pt.Found {
		return false, nil
	}
	if err := chromedp.Run(ctx,
		chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonLeft),
		chromedp.Sleep(1500*time.Millisecond),
	); err != nil {
		return false, err
	}
	if confirmAcceptedAfterReply(ctx, beforeCount) {
		log.Printf("generate_video: confirmed via clickable element")
		return true, nil
	}
	return false, nil
}

func waitAndConfirmVideoGeneration(ctx context.Context, beforeCount int, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		paused, err := waitForHumanVerification(ctx)
		if err != nil {
			return false
		}
		deadline = deadline.Add(paused)
		// Soft refusals / policy rejects appear instead of a confirm card.
		if checkVideoUIErrors(ctx) != nil {
			return false
		}
		if !videoGenerationConfirmPending(ctx) {
			return false
		}
		if ok, err := tryConfirmVideoGeneration(ctx, beforeCount); err == nil && ok {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(1500 * time.Millisecond):
		}
	}
	return false
}

// VideoQuotaExceededError is returned when Doubao web UI shows daily video quota is used up.
type VideoQuotaExceededError struct {
	Message string
}

func (e *VideoQuotaExceededError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "今日视频生成免费次数已用完，请开通豆包专业版加强套餐或明日再试"
}

func (e *VideoQuotaExceededError) Is(target error) bool {
	_, ok := target.(*VideoQuotaExceededError)
	return ok
}

func matchVideoQuotaMessage(text string) (string, bool) {
	checks := []struct {
		substr string
		msg    string
	}{
		{"本月办公任务免费额度已用完", "本月办公任务免费额度已用完，请开通豆包专业版标准套餐或下月再试"},
		{"办公任务免费额度已用完", "办公任务免费额度已用完，请开通豆包专业版标准套餐或下月再试"},
		{"今日免费生视频额度已用完", "今日免费生视频额度已用完，请开通豆包专业版加强套餐或明日再试"},
		{"今日视频生成免费次数用完了", "今日视频生成免费次数已用完，请开通豆包专业版加强套餐或明日再试"},
		{"视频生成免费次数用完了", "视频生成免费次数已用完，请开通豆包专业版加强套餐或明日再试"},
		{"专业能力暂不可用", "豆包专业能力暂不可用（今日免费生视频额度可能已用完），请开通加强套餐或明日再试"},
		{"暂时无法使用专业版功能", "豆包专业版视频生成功能暂不可用（可能已达额度上限），请开通加强套餐或明日再试"},
		// 15s / Seedance features gated behind Plus (seen when forcing duration=15).
		{"专业版加强套餐专属能力", "该能力（如 15 秒视频）需豆包专业版加强套餐，当前账号未开通"},
		{"开通加强套餐，我就能继续", "该能力需豆包专业版加强套餐，当前账号未开通"},
		{"这是专业版加强套餐", "该能力需豆包专业版加强套餐，当前账号未开通"},
		{"开通豆包专业版加强套餐", "视频生成需要豆包专业版加强套餐，当前账号额度不足"},
		{"开通豆包专业版标准套餐", "办公任务需要豆包专业版标准套餐，当前账号额度不足"},
		{"开通加强套餐", "视频生成需要豆包专业版加强套餐，当前账号未开通或额度不足"},
	}
	for _, c := range checks {
		if strings.Contains(text, c.substr) {
			return c.msg, true
		}
	}
	return "", false
}

func checkVideoQuotaExceeded(ctx context.Context) error {
	var text string
	if err := evalReturnByValue(ctx, `(() => document.body.innerText || "")()`, &text); err != nil {
		return nil
	}
	if msg, ok := matchVideoQuotaMessage(text); ok {
		return &VideoQuotaExceededError{Message: msg}
	}
	return nil
}

// VideoGenerationFailedError is returned when Doubao web UI shows the task cannot complete.
type VideoGenerationFailedError struct {
	Message string
	Code    string
}

func (e *VideoGenerationFailedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "视频生成失败，请查看豆包页面了解详情"
}

func (e *VideoGenerationFailedError) Is(target error) bool {
	_, ok := target.(*VideoGenerationFailedError)
	return ok
}

func matchVideoFailureMessage(text string) (string, string, bool) {
	checks := []struct {
		substr string
		code   string
	}{
		{"暂不支持上传真实人脸素材", "content_policy_violation"},
		{"出于肖像保护考虑", "content_policy_violation"},
		{"肖像保护", "content_policy_violation"},
		{"真实人脸素材", "content_policy_violation"},
		{"换张参考图或者文生视频", "content_policy_violation"},
		{"换张参考图", "content_policy_violation"},
		{"疑似包含侵权", "content_policy_violation"},
		{"侵权 / 违规", "content_policy_violation"},
		{"侵权/违规", "content_policy_violation"},
		{"无法返回该内容", "content_policy_violation"},
		{"换个主题再试", "content_policy_violation"},
		{"生成额度未扣除", "content_policy_violation"},
		// Soft content refusal (no confirm card / no ETA) — previously left jobs
		// stuck in "generating" until VIDEO_TIMEOUT.
		{"抱歉，我无法生成你要求的内容", "content_policy_violation"},
		{"我无法生成你要求的内容", "content_policy_violation"},
		{"无法生成你要求的内容", "content_policy_violation"},
		{"视频生成失败", "generation_failed"},
		{"无法生成视频", "generation_failed"},
		{"生成未成功", "generation_failed"},
		{"不符合平台规范", "content_policy_violation"},
		{"内容不符合", "content_policy_violation"},
	}
	for _, c := range checks {
		if !strings.Contains(text, c.substr) {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, c.substr) && len([]rune(line)) >= 8 {
				return line, c.code, true
			}
		}
		return "视频生成失败：" + c.substr, c.code, true
	}
	return "", "", false
}

// pageVideoFailureTextJS finds policy / generation failure copy anywhere in the
// visible chat column. body.innerText.slice(-N) is unreliable because the DOM
// tail is often the sidebar / composer chrome, not the latest assistant reply.
const pageVideoFailureTextJS = `(() => {
	const needles = [
		"暂不支持上传真实人脸素材",
		"出于肖像保护考虑",
		"肖像保护",
		"真实人脸素材",
		"换张参考图或者文生视频",
		"换张参考图",
		"疑似包含侵权",
		"侵权 / 违规",
		"侵权/违规",
		"无法返回该内容",
		"换个主题再试",
		"生成额度未扣除",
		"抱歉，我无法生成你要求的内容",
		"我无法生成你要求的内容",
		"无法生成你要求的内容",
		"视频生成失败",
		"无法生成视频",
		"生成未成功",
		"不符合平台规范",
		"内容不符合",
	];
	function hit(text) {
		const t = String(text || "").trim();
		if (!t) return "";
		for (const n of needles) {
			if (t.includes(n)) return t.length > 400 ? t.slice(0, 400) : t;
		}
		return "";
	}
	const vw = window.innerWidth;
	const candidates = [];
	const nodes = document.querySelectorAll('main p, main div, main span, [class*="message"] p, [class*="message"] div, [class*="assistant"] p, [class*="assistant"] div, [class*="markdown"] p, [class*="markdown"] div, [class*="content"] p, [class*="content"] div');
	for (const el of nodes) {
		if (!el.offsetParent) continue;
		const r = el.getBoundingClientRect();
		if (r.width < 40 || r.height < 12) continue;
		// Skip left history rail.
		if (r.right < vw * 0.28) continue;
		const raw = (el.innerText || el.textContent || "").trim();
		if (raw.length < 8 || raw.length > 800) continue;
		const m = hit(raw);
		if (!m) continue;
		candidates.push({ y: r.top, text: m });
	}
	if (candidates.length) {
		candidates.sort((a, b) => b.y - a.y);
		return candidates[0].text;
	}
	// Fallback: full-page search (not just the tail).
	const body = document.body ? (document.body.innerText || "") : "";
	for (const n of needles) {
		const i = body.indexOf(n);
		if (i < 0) continue;
		const start = Math.max(0, body.lastIndexOf("\n", i) + 1);
		let end = body.indexOf("\n", i);
		if (end < 0) end = Math.min(body.length, i + 200);
		const line = body.slice(start, end).trim();
		if (line) return line.length > 400 ? line.slice(0, 400) : line;
	}
	return "";
})()`

func checkVideoGenerationFailed(ctx context.Context) error {
	// Scan failures first. Stale 「你的视频生成好了」in chat history must not
	// mask a newer portrait-policy / generation reject on the same page.
	var pageFail string
	if err := evalReturnByValue(ctx, pageVideoFailureTextJS, &pageFail); err == nil {
		if msg, code, ok := matchVideoFailureMessage(pageFail); ok {
			return &VideoGenerationFailedError{Message: msg, Code: code}
		}
	}
	var latest string
	if err := evalReturnByValue(ctx, latestAssistantMessageJS, &latest); err == nil {
		if msg, code, ok := matchVideoFailureMessage(latest); ok {
			return &VideoGenerationFailedError{Message: msg, Code: code}
		}
	}
	// Full body — sidebar-heavy pages often push the failure out of slice(-2500).
	var body string
	_ = evalReturnByValue(ctx, `(() => (document.body && document.body.innerText) || "")()`, &body)
	if msg, code, ok := matchVideoFailureMessage(body); ok {
		return &VideoGenerationFailedError{Message: msg, Code: code}
	}
	return nil
}

func checkVideoUIErrors(ctx context.Context) error {
	if err := checkVideoQuotaExceeded(ctx); err != nil {
		return err
	}
	return checkVideoGenerationFailed(ctx)
}

func matchHumanVerificationText(text string) (string, bool) {
	normalized := strings.ToLower(strings.Join(strings.Fields(text), ""))
	signals := []string{
		"请选择所有不包含ai的图片",
		"请在评论区留下相应的序号",
		"请完成人机验证",
		"人机验证",
		"请完成安全验证",
		"安全验证",
		"拖动滑块完成拼图",
		"拖动滑块完成验证",
		"请依次点击",
		"请选出下图中",
		"点击进行验证",
		"验证码",
		"captcha",
	}
	for _, signal := range signals {
		if strings.Contains(normalized, signal) {
			return signal, true
		}
	}
	return "", false
}

func humanVerificationPresent(ctx context.Context) (bool, string) {
	var result struct {
		Text      string `json:"text"`
		DOMReason string `json:"dom_reason"`
	}
	const js = `(() => {
		const visible = el => {
			if (!el || !el.offsetParent) return false;
			const r = el.getBoundingClientRect();
			return r.width > 40 && r.height > 30;
		};
		const selectors = [
			'iframe[src*="captcha" i]',
			'iframe[src*="verify" i]',
			'iframe[src*="challenge" i]',
			'[class*="captcha" i]',
			'[id*="captcha" i]',
			'[class*="geetest" i]',
			'[id*="geetest" i]',
			'[class*="secsdk-captcha" i]',
			'[class*="slider-verify" i]',
		];
		for (const sel of selectors) {
			for (const el of document.querySelectorAll(sel)) {
				if (visible(el)) {
					return {
						text: document.body.innerText || "",
						dom_reason: sel,
					};
				}
			}
		}
		return { text: document.body.innerText || "", dom_reason: "" };
	})()`
	if err := evalReturnByValue(ctx, js, &result); err != nil {
		return false, ""
	}
	if signal, ok := matchHumanVerificationText(result.Text); ok {
		return true, signal
	}
	if result.DOMReason != "" {
		return true, result.DOMReason
	}
	return false, ""
}

// waitForHumanVerification performs read-only polling while a CAPTCHA is visible.
// It must be called before every cluster of UI interactions. The user completes
// the challenge manually; automation resumes only after the challenge disappears.
func waitForHumanVerification(ctx context.Context) (time.Duration, error) {
	present, reason := humanVerificationPresent(ctx)
	if !present {
		return 0, nil
	}

	started := time.Now()
	log.Printf("generate_video: human verification detected (%s); pausing all UI operations for manual completion", reason)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return time.Since(started), ctx.Err()
		case <-ticker.C:
			present, _ := humanVerificationPresent(ctx)
			if present {
				continue
			}
			paused := time.Since(started)
			log.Printf("generate_video: human verification completed manually; resuming after %s", paused.Round(time.Second))
			select {
			case <-ctx.Done():
				return paused, ctx.Err()
			case <-time.After(time.Second):
				return paused, nil
			}
		}
	}
}

func (b *Browser) GenerateVideoViaUI(ctx context.Context, opts VideoUIOptions) ([]VideoItem, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		// Seedance 2.0 Fast often quotes ~15 minutes; leave headroom for queue + URL recovery.
		timeout = 25 * time.Minute
	}

	b.uiMu.Lock()
	defer b.uiMu.Unlock()

	if err := b.EnsureChatPage(ctx); err != nil {
		return nil, fmt.Errorf("open doubao chat: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.activateAttachedTab(ctx)

	// Manual CAPTCHA completion must not consume the normal generation timeout.
	runCtx, cancel := context.WithTimeout(b.browserCtx, timeout+15*time.Minute)
	defer cancel()
	stopCancelPropagation := context.AfterFunc(ctx, cancel)
	defer stopCancelPropagation()
	waitForManualVerification := func() error {
		_, err := waitForHumanVerification(runCtx)
		return err
	}

	officeMode := b.videoUIMode != "skill"
	if officeMode {
		log.Printf("generate_video: office task mode (Turbo)")
	} else {
		log.Printf("generate_video: video skill mode")
	}

	log.Printf("generate_video: start new session")
	if err := waitForManualVerification(); err != nil {
		return nil, err
	}
	if err := ensureNewSession(runCtx, officeMode); err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}
	if err := waitForManualVerification(); err != nil {
		return nil, err
	}
	if err := dismissDoubaoPopups(runCtx); err != nil {
		log.Printf("generate_video: dismiss popups: %v", err)
	}

	hasAudio := opts.RefAudioKey != "" || len(opts.RefAudioData) > 0
	durationSec := normalizeVideoDurationSec(int(opts.Duration))
	if opts.Duration > 0 && int(opts.Duration) != durationSec {
		log.Printf("generate_video: duration %d remapped to %ds (allowed: 5/10/15)", opts.Duration, durationSec)
	}
	var uiPrompt string
	if officeMode {
		uiPrompt = buildOfficeVideoUIPrompt(opts.Prompt, opts.Ratio, len(opts.RefImageFiles)+len(opts.RefImageKeys), hasAudio, durationSec)
	} else {
		uiPrompt = buildVideoUIPrompt(opts.Prompt, opts.Ratio, len(opts.RefImageFiles)+len(opts.RefImageKeys), durationSec)
	}
	if strings.TrimSpace(opts.Prompt) == "" {
		return nil, fmt.Errorf("refuse video generation: text prompt is empty")
	}

	if err := installVideoCaptureHook(runCtx); err != nil {
		return nil, fmt.Errorf("install capture hook: %w", err)
	}
	if err := setForcedVideoDuration(runCtx, durationSec); err != nil {
		log.Printf("generate_video: force duration hook: %v", err)
	}
	beforeCount, _ := readSubmitCount(runCtx)

	if officeMode {
		log.Printf("generate_video: enable 办公任务 Turbo")
		if err := waitForManualVerification(); err != nil {
			return nil, err
		}
		if err := ensureOfficeTaskMode(runCtx); err != nil {
			log.Printf("generate_video: office mode: %v (continuing anyway)", err)
		}
	} else {
		log.Printf("generate_video: click 视频生成 on visible tab")
		if err := waitForManualVerification(); err != nil {
			return nil, err
		}
		if err := ensureVideoSkillMode(runCtx); err != nil {
			return nil, fmt.Errorf("enter video skill: %w", err)
		}
		if err := waitForManualVerification(); err != nil {
			return nil, err
		}
		if err := ensureVideoModel(runCtx, opts.Model); err != nil {
			return nil, fmt.Errorf("select video model: %w", err)
		}
		if err := waitForManualVerification(); err != nil {
			return nil, err
		}
		if err := ensureVideoDuration(runCtx, durationSec); err != nil {
			log.Printf("generate_video: ensure duration %ds: %v", durationSec, err)
		}
	}

	if err := waitForManualVerification(); err != nil {
		return nil, err
	}
	if err := focusChatEditor(runCtx); err != nil {
		log.Printf("generate_video: focus editor before attach: %v", err)
	} else if err := chromedp.Run(runCtx, chromedp.Sleep(400*time.Millisecond)); err != nil {
		return nil, err
	}
	if err := ensureUploadEntryVisible(runCtx); err != nil {
		log.Printf("generate_video: reveal upload entry: %v", err)
	}

	attachImages := func() error {
		if len(opts.RefImageFiles) > 0 {
			for i, f := range opts.RefImageFiles {
				if err := waitForManualVerification(); err != nil {
					return err
				}
				log.Printf("generate_video: attach local image %d/%d", i+1, len(opts.RefImageFiles))
				if err := attachLocalFileViaUI(runCtx, f.Data, f.Filename, "image", officeMode); err != nil {
					return err
				}
				if err := chromedp.Run(runCtx, chromedp.Sleep(900*time.Millisecond)); err != nil {
					return err
				}
			}
			return nil
		}
		if len(opts.RefImageKeys) > 0 {
			for i, refKey := range opts.RefImageKeys {
				if err := waitForManualVerification(); err != nil {
					return err
				}
				log.Printf("generate_video: attach ref image %d/%d", i+1, len(opts.RefImageKeys))
				if err := attachRefFileViaUI(runCtx, refKey, "image", officeMode); err != nil {
					return err
				}
				if err := chromedp.Run(runCtx, chromedp.Sleep(900*time.Millisecond)); err != nil {
					return err
				}
			}
			return nil
		}
		return nil
	}
	if err := attachImages(); err != nil {
		return nil, err
	}
	attachAudio := func() error {
		if opts.RefAudioKey == "" && len(opts.RefAudioData) == 0 {
			return nil
		}
		if err := waitForManualVerification(); err != nil {
			return err
		}
		log.Printf("generate_video: attach ref audio")
		var err error
		if len(opts.RefAudioData) > 0 {
			err = attachLocalFileViaUI(runCtx, opts.RefAudioData, opts.RefAudioFilename, "audio", officeMode)
		} else {
			err = attachRefFileViaUI(runCtx, opts.RefAudioKey, "audio", officeMode)
		}
		if err != nil {
			return err
		}
		return chromedp.Run(runCtx, chromedp.Sleep(900*time.Millisecond))
	}
	if err := attachAudio(); err != nil {
		return nil, err
	}

	if err := waitForManualVerification(); err != nil {
		return nil, err
	}
	// Close leftover "/" skill menu or "+" upload popover so the composer
	// is not aria-hidden / covered when we type the prompt.
	_ = chromedp.Run(runCtx,
		chromedp.KeyEvent("\u001b"),
		chromedp.Sleep(250*time.Millisecond),
	)
	_ = dismissDoubaoPopups(runCtx)
	log.Printf("generate_video: type prompt (%d chars)", len(uiPrompt))
	if err := ensurePromptInEditor(runCtx, uiPrompt); err != nil {
		if !isRetryableComposerError(err) {
			return nil, err
		}
		// The SPA occasionally leaves a half-mounted composer after uploads.
		// Rebuild the whole pre-submit state once so callers do not have to retry
		// the API manually. Nothing has been submitted at this point.
		log.Printf("generate_video: transient composer failure (%v); rebuilding fresh session once", err)
		if resetErr := resetToFreshChat(runCtx); resetErr != nil {
			return nil, fmt.Errorf("%v; composer recovery reset: %w", err, resetErr)
		}
		if hookErr := installVideoCaptureHook(runCtx); hookErr != nil {
			return nil, fmt.Errorf("composer recovery capture hook: %w", hookErr)
		}
		_ = setForcedVideoDuration(runCtx, durationSec)
		if officeMode {
			if modeErr := ensureOfficeTaskMode(runCtx); modeErr != nil {
				log.Printf("generate_video: composer recovery office mode: %v", modeErr)
			}
		} else {
			if modeErr := ensureVideoSkillMode(runCtx); modeErr != nil {
				return nil, fmt.Errorf("composer recovery enter video skill: %w", modeErr)
			}
			if modelErr := ensureVideoModel(runCtx, opts.Model); modelErr != nil {
				return nil, fmt.Errorf("composer recovery select model: %w", modelErr)
			}
			if durationErr := ensureVideoDuration(runCtx, durationSec); durationErr != nil {
				log.Printf("generate_video: composer recovery duration: %v", durationErr)
			}
		}
		beforeCount, _ = readSubmitCount(runCtx)
		_ = ensureUploadEntryVisible(runCtx)
		if attachErr := attachImages(); attachErr != nil {
			return nil, fmt.Errorf("composer recovery images: %w", attachErr)
		}
		if attachErr := attachAudio(); attachErr != nil {
			return nil, fmt.Errorf("composer recovery audio: %w", attachErr)
		}
		_ = chromedp.Run(runCtx, chromedp.KeyEvent("\u001b"), chromedp.Sleep(300*time.Millisecond))
		_ = dismissDoubaoPopups(runCtx)
		if retryErr := ensurePromptInEditor(runCtx, uiPrompt); retryErr != nil {
			return nil, fmt.Errorf("composer recovery failed: %w (initial: %v)", retryErr, err)
		}
	}

	if err := waitForManualVerification(); err != nil {
		return nil, err
	}
	// Re-mark the composer right before submit — React often replaces the
	// textarea after image attach / long prompt, dropping data-doubao-chat-editor.
	if err := focusChatEditor(runCtx); err != nil {
		log.Printf("generate_video: re-focus editor before submit: %v", err)
	}
	if err := trySubmitUI(runCtx, beforeCount); err != nil {
		return nil, fmt.Errorf("ui submit: %w", err)
	}
	if err := ensurePromptSubmittedInChat(runCtx, uiPrompt); err != nil {
		return nil, err
	}
	submittedAt := time.Now()
	jobConvID := currentChatConversationID(runCtx)
	if jobConvID != "" {
		log.Printf("generate_video: pinned to conversation %s for this job", jobConvID)
	}
	lastETAText := ""
	reportETA := func() {
		if opts.OnETA == nil {
			return
		}
		eta, ok := readVideoETA(runCtx)
		if !ok || eta.Text == "" || eta.Text == lastETAText {
			return
		}
		lastETAText = eta.Text
		log.Printf("generate_video: doubao eta %q", eta.Text)
		opts.OnETA(eta)
	}
	if err := markVideoCaptureSubmitBaseline(runCtx); err != nil {
		log.Printf("generate_video: mark capture baseline: %v", err)
	}
	if err := chromedp.Run(runCtx, chromedp.Sleep(1500*time.Millisecond)); err != nil {
		return nil, err
	}
	if err := checkVideoUIErrors(runCtx); err != nil {
		log.Printf("generate_video: %v", err)
		return nil, err
	}
	reportETA()

	if waitAndConfirmVideoGeneration(runCtx, beforeCount, 25*time.Second) {
		if err := chromedp.Run(runCtx, chromedp.Sleep(1500*time.Millisecond)); err != nil {
			return nil, err
		}
		beforeCount, _ = readSubmitCount(runCtx)
		reportETA()
	}

	baseline, _ := b.snapshotVideoURLs(runCtx)
	if baseline == nil {
		baseline = make(map[string]struct{})
	}
	for _, item := range b.snapshotCapturedVideoItems() {
		if item.VideoURL != "" {
			baseline[item.VideoURL] = struct{}{}
		}
	}
	log.Printf("generate_video: UI submitted (images=%d, audio=%v), baseline videos=%d, waiting for completion",
		len(opts.RefImageFiles)+len(opts.RefImageKeys), hasAudio, len(baseline))

	if err := chromedp.Run(runCtx, chromedp.Sleep(2*time.Second)); err != nil {
		return nil, err
	}
	if err := checkVideoUIErrors(runCtx); err != nil {
		log.Printf("generate_video: %v", err)
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	confirmAttempted := false
	var recover videoURLRecoverState
	var lastPendingLog time.Time
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		paused, err := waitForHumanVerification(runCtx)
		if err != nil {
			return nil, err
		}
		deadline = deadline.Add(paused)

		if jobConvID != "" {
			if cur := currentChatConversationID(runCtx); cur != "" && cur != jobConvID {
				log.Printf("generate_video: left job conversation %s -> %s, navigating back", jobConvID, cur)
				targetURL := fmt.Sprintf("%s/chat/%s", doubaoBaseURL, jobConvID)
				if err := chromedp.Run(runCtx,
					chromedp.Navigate(targetURL),
					chromedp.Sleep(800*time.Millisecond),
				); err != nil {
					log.Printf("generate_video: navigate back to job chat: %v", err)
				} else {
					_ = installVideoCaptureHook(runCtx)
				}
			}
		}

		if err := checkVideoUIErrors(runCtx); err != nil {
			log.Printf("generate_video: %v", err)
			return nil, err
		}

		if videoGenerationAcknowledged(runCtx) && !videoGenerateActionButtonPending(runCtx) {
			confirmAttempted = true
		}

		if videoGenerationConfirmPending(runCtx) && !confirmAttempted {
			log.Printf("generate_video: parameter confirmation pending, auto-replying...")
			confirmCount, _ := readSubmitCount(runCtx)
			if ok, _ := tryConfirmVideoGeneration(runCtx, confirmCount); ok {
				confirmAttempted = true
				log.Printf("generate_video: confirmation sent, waiting for generation to start")
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(2 * time.Second):
				}
			} else if videoGenerationAcknowledged(runCtx) {
				confirmAttempted = true
			}
		}

		if items, err := b.pollUIVideoResults(runCtx, baseline, &recover, submittedAt); err == nil && len(items) > 0 {
			best := pickLatestVideoItem(items)
			log.Printf("generate_video: got completed video url (%s)", shortVideoURL(best.VideoURL))
			// Resolve fallback_api → logo_type=unwatermarked before leaving the chat,
			// so COS / proxy downloads get a clean (no 「豆包AI生成」) stream.
			best = b.UpgradeVideoToUnwatermarked(runCtx, best)
			// Leave the finished video conversation so the next request does not
			// inherit a sticky skill-bar state that blocks 「视频生成」.
			log.Printf("generate_video: reset to fresh chat after success")
			if err := resetToFreshChat(runCtx); err != nil {
				log.Printf("generate_video: post-success fresh chat: %v", err)
			}
			return []VideoItem{best}, nil
		}

		if videoGenerationComplete(runCtx) {
			if lastPendingLog.IsZero() || time.Since(lastPendingLog) > 30*time.Second {
				log.Printf("generate_video: video complete on page, extracting url...")
				lastPendingLog = time.Now()
			}
		} else if videoGenerationPending(runCtx, submittedAt) {
			reportETA()
			if lastPendingLog.IsZero() || time.Since(lastPendingLog) > 30*time.Second {
				log.Printf("generate_video: generation acknowledged, keep polling...")
				lastPendingLog = time.Now()
			}
		}

		// Yield Browser.mu during the poll sleep so image/upload on the same
		// Chrome are not blocked for the entire Seedance wait (~15m).
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			b.mu.Lock()
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
		b.mu.Lock()
	}
	return nil, fmt.Errorf("video UI mode timed out — check Chrome tab for errors or rate limit")
}

func (b *Browser) pollUIVideoResults(ctx context.Context, baseline map[string]struct{}, recover *videoURLRecoverState, submittedAt time.Time) ([]VideoItem, error) {
	type pollResult struct {
		Videos []VideoItem `json:"videos"`
		Chunks []string    `json:"chunks"`
	}
	var result pollResult
	if err := evalReturnByValue(ctx, extractVideosFromPageJS, &result); err != nil {
		return nil, err
	}

	confirmPending := videoGenerationConfirmPending(ctx)
	if confirmPending {
		return nil, nil
	}
	complete := videoGenerationComplete(ctx)
	pending := videoGenerationPending(ctx, submittedAt)
	// Stale ETA ack can look "pending" while the page already shows completion.
	// Prefer completion so we enter URL recovery instead of spinning forever.
	if pending && !complete {
		return nil, nil
	}

	tryExtract := func() ([]VideoItem, bool) {
		if chunkItems := parseVideosFromCapturedChunks(result.Chunks); len(chunkItems) > 0 {
			if resolved := b.resolveFallbackOnlyItems(ctx, chunkItems); len(resolved) > 0 {
				if fresh := diffVideoItems(baseline, resolved); len(fresh) > 0 {
					return fresh, true
				}
			}
		}
		for _, chunk := range result.Chunks {
			if items := parseVideosFromSSE(chunk); len(items) > 0 {
				if fresh := diffVideoItems(baseline, filterSSEVideoItems(items)); len(fresh) > 0 {
					return fresh, true
				}
			}
		}
		if fresh := diffVideoItems(baseline, filterDOMVideoItems(result.Videos)); len(fresh) > 0 {
			return fresh, true
		}
		if fresh := diffVideoItems(baseline, filterSSEVideoItems(b.snapshotCapturedVideoItems())); len(fresh) > 0 {
			return fresh, true
		}
		return nil, false
	}

	if fresh, ok := tryExtract(); ok && complete {
		return fresh, nil
	}

	if complete {
		if recover == nil {
			recover = &videoURLRecoverState{}
		}
		// Soft-reload / player clicking mid-Seedance can wipe the waiting UI.
		// Only do heavy recovery after the job has had time to finish.
		early := !submittedAt.IsZero() && time.Since(submittedAt) < 2*time.Minute

		if recover.LastDiagAt.IsZero() || time.Since(recover.LastDiagAt) > 15*time.Second {
			logVideoExtractDiagnostics(ctx, "complete-no-url", len(b.snapshotCapturedVideoItems()))
			if n := len(result.Videos); n > 0 {
				sample := result.Videos[0].VideoURL
				filtered := filterDOMVideoItems(result.Videos)
				_, inBaseline := baseline[sample]
				log.Printf("generate_video: raw extract videos=%d filtered=%d sample=%s cover=%v likely=%v inBaseline=%v",
					n, len(filtered), shortVideoURL(sample), isCoverImageURL(sample), isLikelyVideoMediaURL(sample), inBaseline)
			}
			recover.LastDiagAt = time.Now()
		}

		// Prefer fallback_api → playable URL (cover/player may never expose <video src>).
		if recover.LastFallbackAt.IsZero() || time.Since(recover.LastFallbackAt) > 12*time.Second {
			recover.LastFallbackAt = time.Now()
			if item, ok := b.tryResolveCompletedVideoViaFallback(ctx); ok {
				if _, seen := baseline[item.VideoURL]; !seen {
					log.Printf("generate_video: resolved via fallback_api (%s)", shortVideoURL(item.VideoURL))
					return []VideoItem{item}, nil
				}
			}
		}

		if early {
			return nil, nil
		}

		shouldRetryPlayer := !recover.PlayerActivated ||
			(time.Since(recover.LastPlayerAt) > 25*time.Second)
		if shouldRetryPlayer {
			_ = scrollVideoCompletionIntoView(ctx)
			if tryActivateVideoPlayer(ctx) {
				recover.PlayerActivated = true
				recover.LastPlayerAt = time.Now()
				recover.DownloadClicked = false // allow download after a fresh player open
				if err := chromedp.Run(ctx, chromedp.Sleep(3*time.Second)); err != nil {
					return nil, err
				}
				if err := evalReturnByValue(ctx, extractVideosFromPageJS, &result); err == nil {
					if fresh, ok := tryExtract(); ok {
						return fresh, nil
					}
				}
				if item, ok := b.tryResolveCompletedVideoViaFallback(ctx); ok {
					if _, seen := baseline[item.VideoURL]; !seen {
						return []VideoItem{item}, nil
					}
				}
			} else if !recover.PlayerActivated {
				recover.LastPlayerAt = time.Now()
			}
		} else if !recover.DownloadClicked {
			if tryClickVideoDownload(ctx) {
				recover.DownloadClicked = true
				if err := chromedp.Run(ctx, chromedp.Sleep(2*time.Second)); err != nil {
					return nil, err
				}
				if err := evalReturnByValue(ctx, extractVideosFromPageJS, &result); err == nil {
					if fresh, ok := tryExtract(); ok {
						return fresh, nil
					}
				}
				if fresh, ok := tryExtract(); ok {
					return fresh, nil
				}
			} else {
				recover.DownloadClicked = true
			}
		} else if !recover.ChainReloaded {
			// Soft-reload the job chat so /im/chain/single re-fires with fallback_api.
			convID := currentChatConversationID(ctx)
			if convID != "" {
				log.Printf("generate_video: soft-reload conversation %s to capture fallback_api", convID)
				targetURL := fmt.Sprintf("%s/chat/%s", doubaoBaseURL, convID)
				if err := chromedp.Run(ctx,
					chromedp.Navigate(targetURL),
					chromedp.Sleep(4*time.Second),
				); err != nil {
					log.Printf("generate_video: soft-reload navigate: %v", err)
				} else {
					_ = installVideoCaptureHook(ctx)
					_ = dismissDoubaoPopups(ctx)
					b.harvestChainSingleBodies(ctx)
					if err := evalReturnByValue(ctx, extractVideosFromPageJS, &result); err == nil {
						if fresh, ok := tryExtract(); ok {
							return fresh, nil
						}
					}
					if item, ok := b.tryResolveCompletedVideoViaFallback(ctx); ok {
						if _, seen := baseline[item.VideoURL]; !seen {
							return []VideoItem{item}, nil
						}
					}
				}
			}
			recover.ChainReloaded = true
			recover.PlayerActivated = false
			recover.DownloadClicked = false
			recover.LastPlayerAt = time.Time{}
		}
	}

	return nil, nil
}

// tryResolveCompletedVideoViaFallback turns captured fallback_api / HTML scrape into a playable URL.
func (b *Browser) tryResolveCompletedVideoViaFallback(ctx context.Context) (VideoItem, bool) {
	apis, vid := b.collectAllFallbackAPIs(ctx)
	fb := PreferFallbackAPIForVid(apis, vid)
	if fb == "" {
		return VideoItem{}, false
	}
	clean, err := b.resolveUnwatermarkedViaFallback(ctx, fb)
	if err != nil || strings.TrimSpace(clean) == "" {
		if err != nil {
			log.Printf("generate_video: fallback_api resolve while complete: %v", err)
		}
		return VideoItem{}, false
	}
	return VideoItem{VideoURL: clean, FallbackAPI: fb, Vid: vid}, true
}

// resolveFallbackOnlyItems fills VideoURL for chunk items that only have fallback_api.
func (b *Browser) resolveFallbackOnlyItems(ctx context.Context, items []VideoItem) []VideoItem {
	out := make([]VideoItem, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.VideoURL) != "" {
			out = append(out, item)
			continue
		}
		fb := strings.TrimSpace(item.FallbackAPI)
		if fb == "" {
			continue
		}
		clean, err := b.resolveUnwatermarkedViaFallback(ctx, fb)
		if err != nil || strings.TrimSpace(clean) == "" {
			continue
		}
		item.VideoURL = clean
		out = append(out, item)
	}
	return out
}

func scrollVideoCompletionIntoView(ctx context.Context) error {
	const js = `(() => {
		const re = /你的视频生成好了|视频已生成完成|视频生成完成|视频生成好了/;
		const nodes = [...document.querySelectorAll('div, section, article, p, span')];
		for (let i = nodes.length - 1; i >= 0; i--) {
			const el = nodes[i];
			if (!el.offsetParent) continue;
			const t = (el.innerText || "").trim();
			if (t.length < 8 || t.length > 400) continue;
			if (!re.test(t)) continue;
			el.scrollIntoView({ block: 'center', inline: 'nearest' });
			return true;
		}
		window.scrollBy(0, -200);
		return false;
	})()`
	var ok bool
	_ = evalReturnByValue(ctx, js, &ok)
	return nil
}

func parseVideosFromCapturedChunks(chunks []string) []VideoItem {
	reURL := regexp.MustCompile(`"video_url"\s*:\s*"((?:\\.|[^"\\])*)"`)
	reDouyin := regexp.MustCompile(`https://[^\s"'\\]+(?:douyinvod|douyin\.com)[^\s"'\\]+`)
	seen := make(map[string]struct{})
	var items []VideoItem
	add := func(url, fb, vid string) {
		url = strings.ReplaceAll(url, `\u0026`, "&")
		url = strings.ReplaceAll(url, `\/`, "/")
		url = strings.TrimSpace(url)
		if url == "" || isCoverImageURL(url) {
			return
		}
		if !isLikelyVideoMediaURL(url) {
			return
		}
		if _, ok := seen[url]; ok {
			return
		}
		seen[url] = struct{}{}
		items = append(items, VideoItem{VideoURL: url, FallbackAPI: fb, Vid: vid})
	}
	for _, chunk := range chunks {
		apis := ExtractFallbackAPIs(chunk)
		vids := ExtractVids(chunk)
		fb, vid := "", ""
		if len(vids) > 0 {
			vid = vids[len(vids)-1]
		}
		if len(apis) > 0 {
			fb = PreferFallbackAPIForVid(apis, vid)
		}
		before := len(items)
		if strings.Contains(chunk, "event_type") {
			for _, item := range parseVideosFromSSE(chunk) {
				if item.FallbackAPI == "" {
					item.FallbackAPI = fb
				}
				if item.Vid == "" {
					item.Vid = vid
				}
				add(item.VideoURL, item.FallbackAPI, item.Vid)
			}
		} else {
			for _, m := range reURL.FindAllStringSubmatch(chunk, -1) {
				if len(m) > 1 {
					add(m[1], fb, vid)
				}
			}
			for _, m := range reDouyin.FindAllString(chunk, -1) {
				add(m, fb, vid)
			}
		}
		// Completion payloads sometimes carry only fallback_api / vid (no direct CDN url).
		if len(items) == before && fb != "" {
			key := "fb:" + fb
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				items = append(items, VideoItem{FallbackAPI: fb, Vid: vid})
			}
		}
	}
	return filterSSEVideoItems(items)
}

func parseVideosFromSSE(raw string) []VideoItem {
	var items []VideoItem
	for _, block := range strings.Split(raw, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var dataStr string
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data:") {
				dataStr = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if dataStr == "" {
			continue
		}
		var root map[string]any
		if json.Unmarshal([]byte(dataStr), &root) != nil {
			continue
		}
		if root["event_type"] != float64(2001) {
			continue
		}
		eventDataRaw, ok := root["event_data"]
		if !ok {
			continue
		}
		var eventData map[string]any
		switch v := eventDataRaw.(type) {
		case string:
			if json.Unmarshal([]byte(v), &eventData) != nil {
				continue
			}
		case map[string]any:
			eventData = v
		default:
			continue
		}
		msg, ok := eventData["message"].(map[string]any)
		if !ok {
			continue
		}
		if ct, _ := msg["content_type"].(float64); int(ct) != 2021 {
			continue
		}
		contentRaw := msg["content"]
		var content map[string]any
		switch v := contentRaw.(type) {
		case string:
			if json.Unmarshal([]byte(v), &content) != nil {
				continue
			}
		case map[string]any:
			content = v
		default:
			continue
		}
		if status, ok := content["video_status"].(float64); ok && int(status) == 1 {
			continue
		}
		if url, _ := content["video_url"].(string); url != "" {
			cover, _ := content["cover_url"].(string)
			items = append(items, VideoItem{VideoURL: url, CoverURL: cover})
		}
	}
	return filterSSEVideoItems(items)
}

func (b *Browser) NavigateToConversation(ctx context.Context, conversationID string) error {
	if conversationID == "" || conversationID == "0" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	targetURL := fmt.Sprintf("%s/chat/%s", doubaoBaseURL, conversationID)
	runCtx, cancel := context.WithTimeout(b.browserCtx, 30*time.Second)
	defer cancel()

	return chromedp.Run(runCtx,
		chromedp.Navigate(targetURL),
		chromedp.Sleep(2*time.Second),
	)
}

func (b *Browser) snapshotVideoURLs(ctx context.Context) (map[string]struct{}, error) {
	items, err := b.extractVideosFromPageUnlocked(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.VideoURL != "" {
			seen[item.VideoURL] = struct{}{}
		}
	}
	return seen, nil
}

func (b *Browser) extractVideosFromPageUnlocked(ctx context.Context) ([]VideoItem, error) {
	type pollResult struct {
		Videos []VideoItem `json:"videos"`
	}
	var result pollResult
	if err := evalReturnByValue(ctx, extractVideosFromPageJS, &result); err != nil {
		return nil, err
	}
	return filterDOMVideoItems(result.Videos), nil
}

// extractVideosFromPageJS collects playable video URLs from every visible
// player, performance resource timings, and the in-page capture hook.
// Doubao often serves v3-default.douyin.com/.../video/tos/... (not *douyinvod*).
const extractVideosFromPageJS = `(() => {
	const videos = [];
	const seen = new Set();
	function add(url, cover, fromVideoTag) {
		if (!url || typeof url !== "string" || seen.has(url) || url.startsWith("blob:")) return;
		seen.add(url);
		videos.push({
			video_url: url,
			cover_url: cover || "",
			width: 0,
			height: 0,
			duration: 0,
			from_video_tag: !!fromVideoTag,
		});
	}
	function isPlayableURL(u) {
		if (!u || u.startsWith("blob:")) return false;
		return /\.(mp4|m3u8|webm|mov)(\?|#|$)/i.test(u) ||
			/douyinvod|mime_type=video_mp4|video_mp4|\/video\/tos\//i.test(u) ||
			/(?:^|\/\/)[^/]*douyin\.com\/[^"'\\s]*\/video\//i.test(u);
	}
	// Scan ALL visible video players (not only the completion-message subtree).
	for (const v of document.querySelectorAll("video")) {
		if (!v.offsetParent) continue;
		const poster = v.poster || "";
		const sources = [v.src, v.currentSrc, ...Array.from(v.querySelectorAll("source")).map(s => s.src)];
		for (const src of sources) {
			if (src && !src.startsWith("blob:")) add(src, poster, true);
		}
		let p = v;
		for (let i = 0; i < 8 && p; i++, p = p.parentElement) {
			const cfg = (p.__player && p.__player.config) ||
				(p.player && p.player.config) ||
				(p.__xgPlayer__ && p.__xgPlayer__.config) || null;
			if (!cfg || typeof cfg !== "object") continue;
			if (typeof cfg.url === "string") add(cfg.url, poster, true);
			if (typeof cfg.src === "string") add(cfg.src, poster, true);
			if (Array.isArray(cfg.urls)) {
				for (const item of cfg.urls) {
					if (typeof item === "string") add(item, poster, true);
					else if (item && typeof item.src === "string") add(item.src, poster, true);
					else if (item && typeof item.url === "string") add(item.url, poster, true);
				}
			}
		}
	}
	for (const el of document.querySelectorAll("[src], [href], source, [data-src], [data-url], [data-video-url]")) {
		if (!el.offsetParent && el.tagName !== "SOURCE") continue;
		const u = el.src || el.href || el.getAttribute("data-src") ||
			el.getAttribute("data-url") || el.getAttribute("data-video-url") || "";
		if (!isPlayableURL(u)) continue;
		add(u, "", false);
	}
	try {
		for (const e of performance.getEntriesByType("resource")) {
			const u = e.name || "";
			if (isPlayableURL(u)) add(u, "", false);
		}
	} catch (err) {}
	const cap = window.__doubaoVideoCapture;
	const urlStart = (cap && cap.videoURLBaseline) || 0;
	if (cap && cap.videoURLs) {
		for (const u of cap.videoURLs.slice(urlStart)) add(u, "", false);
		// When generation is complete, also accept any captured URL (baseline can
		// race with late performance entries from the same job).
		for (const u of cap.videoURLs) add(u, "", false);
	}
	const chunkStart = (cap && cap.chunkBaseline) || 0;
	const chunks = (cap && cap.chunks) ? cap.chunks.slice(chunkStart) : [];
	return { videos, chunks };
})()`

func diffVideoItems(baseline map[string]struct{}, items []VideoItem) []VideoItem {
	var out []VideoItem
	for _, item := range items {
		if _, ok := baseline[item.VideoURL]; ok {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (b *Browser) ExtractVideosFromPage(ctx context.Context) ([]VideoItem, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	runCtx, cancel := context.WithTimeout(b.browserCtx, 15*time.Second)
	defer cancel()
	return b.extractVideosFromPageUnlocked(runCtx)
}

func filterDOMVideoItems(items []VideoItem) []VideoItem {
	var out []VideoItem
	seen := make(map[string]struct{})
	for _, item := range items {
		url := strings.TrimSpace(item.VideoURL)
		if url == "" || strings.Contains(url, "blob:") {
			continue
		}
		if isCoverImageURL(url) {
			continue
		}
		if !isLikelyVideoMediaURL(url) && !item.FromVideoTag {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		item.VideoURL = url
		out = append(out, item)
	}
	return out
}

func filterSSEVideoItems(items []VideoItem) []VideoItem {
	var out []VideoItem
	seen := make(map[string]struct{})
	for _, item := range items {
		url := strings.TrimSpace(item.VideoURL)
		if url == "" || isCoverImageURL(url) {
			if isCoverImageURL(url) && item.CoverURL == "" {
				item.CoverURL = url
			}
			// Keep fallback_api-only rows so callers can resolve a playable URL.
			if url == "" && strings.TrimSpace(item.FallbackAPI) != "" {
				key := "fb:" + item.FallbackAPI
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, item)
			}
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		item.VideoURL = url
		out = append(out, item)
	}
	return out
}

func filterVideoItems(items []VideoItem) []VideoItem {
	return filterDOMVideoItems(items)
}

// urlPathOnly strips query/hash so cover heuristics don't false-positive on
// signed CDN query params that mention poster/cover/.jpg thumbnails.
func urlPathOnly(u string) string {
	lower := strings.ToLower(strings.TrimSpace(u))
	if i := strings.IndexAny(lower, "?#"); i >= 0 {
		return lower[:i]
	}
	return lower
}

func isCoverImageURL(u string) bool {
	lower := strings.ToLower(strings.TrimSpace(u))
	if lower == "" {
		return false
	}
	// Playable video CDN paths — never classify as cover even if the signed
	// query string embeds poster=.jpg / cover=... (common on douyin.com).
	if looksLikeVideoMediaURL(lower) {
		return false
	}
	path := urlPathOnly(lower)
	if strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") ||
		strings.HasSuffix(path, ".jpeg") || strings.HasSuffix(path, ".webp") ||
		strings.HasSuffix(path, ".gif") || strings.HasSuffix(path, ".bmp") {
		return true
	}
	// Path-segment heuristics only (avoid matching random query keys).
	for _, seg := range []string{"/cover/", "/poster/", "/thumbnail/", "/thumb/", "/watermark/"} {
		if strings.Contains(path, seg) {
			return true
		}
	}
	if strings.Contains(path, "cover") && !strings.Contains(path, "/video/") {
		return true
	}
	if strings.Contains(path, "poster") && !strings.Contains(path, "/video/") {
		return true
	}
	if strings.Contains(path, "thumbnail") || strings.Contains(path, "watermark") {
		return true
	}
	return false
}

func looksLikeVideoMediaURL(lower string) bool {
	path := urlPathOnly(lower)
	if strings.HasSuffix(path, ".mp4") || strings.HasSuffix(path, ".m3u8") ||
		strings.HasSuffix(path, ".webm") || strings.HasSuffix(path, ".mov") {
		return true
	}
	if strings.Contains(lower, "mime_type=video_mp4") || strings.Contains(lower, "video_mp4") {
		return true
	}
	if strings.Contains(lower, "douyinvod") || strings.Contains(path, "/video/tos/") {
		return true
	}
	// New Doubao player hosts (not *douyinvod*): v3-default.douyin.com/.../video/tos/...
	if strings.Contains(path, "douyin.com") && strings.Contains(path, "/video/") {
		return true
	}
	if strings.Contains(path, "bytevcloud") && strings.Contains(path, "/video/") {
		return true
	}
	return false
}

func isLikelyVideoMediaURL(u string) bool {
	if isCoverImageURL(u) {
		return false
	}
	return looksLikeVideoMediaURL(strings.ToLower(strings.TrimSpace(u)))
}

func videoURLScore(u string) int {
	lower := strings.ToLower(u)
	score := 0
	if strings.Contains(lower, ".mp4") {
		score += 100
	}
	if strings.Contains(lower, ".m3u8") {
		score += 80
	}
	if strings.Contains(lower, ".webm") || strings.Contains(lower, ".mov") {
		score += 70
	}
	if strings.Contains(lower, "lr=unwatermarked") || strings.Contains(lower, "logo_type=unwatermarked") {
		score += 200
	}
	if strings.Contains(lower, "watermark") || strings.Contains(lower, "cover") {
		score -= 50
	}
	if strings.Contains(lower, ".png") || strings.Contains(lower, ".jpg") {
		score -= 100
	}
	return score
}

func pickLatestVideoItem(items []VideoItem) VideoItem {
	if len(items) == 0 {
		return VideoItem{}
	}
	return items[len(items)-1]
}

func pickBestVideoItem(items []VideoItem) VideoItem {
	return pickLatestVideoItem(items)
}

func shortVideoURL(u string) string {
	if len(u) <= 80 {
		return u
	}
	return u[:40] + "..." + u[len(u)-30:]
}

func (b *Browser) WaitForVideos(ctx context.Context, conversationID string, timeout time.Duration) ([]VideoItem, error) {
	if timeout <= 0 {
		timeout = 25 * time.Minute
	}

	b.mu.Lock()
	runCtx, cancel := context.WithTimeout(b.browserCtx, timeout+30*time.Second)
	baseline, _ := b.snapshotVideoURLs(runCtx)
	if baseline == nil {
		baseline = make(map[string]struct{})
	}
	b.mu.Unlock()
	defer cancel()

	if conversationID != "" && conversationID != "0" {
		if err := b.NavigateToConversation(ctx, conversationID); err != nil {
			return nil, fmt.Errorf("navigate to conversation: %w", err)
		}
		b.mu.Lock()
		baseline2, _ := b.snapshotVideoURLs(runCtx)
		b.mu.Unlock()
		for k := range baseline2 {
			baseline[k] = struct{}{}
		}
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		items, err := b.ExtractVideosFromPage(ctx)
		if err == nil {
			if fresh := diffVideoItems(baseline, items); len(fresh) > 0 {
				best := pickBestVideoItem(fresh)
				best = b.UpgradeVideoToUnwatermarked(runCtx, best)
				return []VideoItem{best}, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return nil, fmt.Errorf("no video appeared in conversation within timeout")
}
