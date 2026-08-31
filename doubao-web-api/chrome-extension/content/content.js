(() => {
  if (window.__doubaoCleanDLContent) return;
  window.__doubaoCleanDLContent = true;

  const state = {
    apis: [],
    vids: [],
  };

  function mergeCapture(apis, vids) {
    for (const api of apis || []) {
      if (api && !state.apis.includes(api)) state.apis.push(api);
    }
    for (const vid of vids || []) {
      if (vid && !state.vids.includes(vid)) state.vids.push(vid);
    }
    updateBadge();
  }

  function scrapePageHTML() {
    try {
      const html = document.documentElement ? document.documentElement.innerHTML : "";
      const apis = DoubaoUnwatermark.extractFallbackAPIs(html);
      mergeCapture(apis, []);
    } catch (_) {}
  }

  // Inject page-world hook as early as possible.
  function injectHook() {
    const s = document.createElement("script");
    s.src = chrome.runtime.getURL("content/inject.js");
    s.async = false;
    (document.documentElement || document.head || document.body).appendChild(s);
    s.remove();
  }
  injectHook();

  window.addEventListener("message", (ev) => {
    if (!ev.data || ev.data.source !== "doubao-clean-dl") return;
    if (ev.data.type === "captured" || ev.data.type === "pong") {
      mergeCapture(ev.data.apis, ev.data.vids);
    }
  });

  // Ask inject for current cache after load.
  setTimeout(() => {
    window.postMessage({ source: "doubao-clean-dl-ext", type: "ping" }, "*");
    scrapePageHTML();
  }, 800);

  const root = document.createElement("div");
  root.id = "doubao-clean-dl-root";
  const btn = document.createElement("button");
  btn.id = "doubao-clean-dl-btn";
  btn.type = "button";
  btn.textContent = "无水印下载";
  const status = document.createElement("div");
  status.id = "doubao-clean-dl-status";
  root.appendChild(btn);
  root.appendChild(status);

  function mount() {
    if (!document.body) return;
    if (!document.getElementById("doubao-clean-dl-root")) {
      document.body.appendChild(root);
    }
  }
  if (document.body) mount();
  else document.addEventListener("DOMContentLoaded", mount);

  function setStatus(text, isErr) {
    status.textContent = text || "";
    status.classList.toggle("show", Boolean(text));
    status.classList.toggle("err", Boolean(isErr));
  }

  function updateBadge() {
    const n = state.apis.length;
    btn.textContent = n > 0 ? `无水印下载 (${n})` : "无水印下载";
  }

  function pickFallback() {
    scrapePageHTML();
    const vid = state.vids.length ? state.vids[state.vids.length - 1] : "";
    return DoubaoUnwatermark.preferFallbackAPIForVid(state.apis, vid);
  }

  function filenameFor(api, cleanUrl) {
    const vid = DoubaoUnwatermark.vidFromFallbackAPI(api) || "video";
    const chat = (location.pathname.match(/\/chat\/(\d+)/) || [])[1] || "doubao";
    return `doubao-clean-${chat}-${vid}.mp4`;
  }

  async function downloadViaBackground(url, filename) {
    return new Promise((resolve) => {
      chrome.runtime.sendMessage({ type: "download", url, filename, saveAs: false }, (resp) => {
        if (chrome.runtime.lastError) {
          resolve({ ok: false, error: chrome.runtime.lastError.message });
          return;
        }
        resolve(resp || { ok: false, error: "no response" });
      });
    });
  }

  async function downloadViaBlob(url, filename) {
    const resp = await fetch(url, { credentials: "omit" });
    if (!resp.ok) throw new Error("download http " + resp.status);
    const blob = await resp.blob();
    const obj = URL.createObjectURL(blob);
    try {
      const a = document.createElement("a");
      a.href = obj;
      a.download = filename;
      a.click();
    } finally {
      setTimeout(() => URL.revokeObjectURL(obj), 30_000);
    }
  }

  btn.addEventListener("click", async () => {
    if (btn.disabled) return;
    btn.disabled = true;
    setStatus("正在解析无水印地址…");
    try {
      window.postMessage({ source: "doubao-clean-dl-ext", type: "ping" }, "*");
      await new Promise((r) => setTimeout(r, 200));
      scrapePageHTML();

      let fallback = pickFallback();
      if (!fallback) {
        setStatus("未捕获到 fallback_api。请刷新页面并等视频出现后再点。", true);
        return;
      }

      const cleanUrl = await DoubaoUnwatermark.resolveUnwatermarkedViaFallback(fallback);
      if (!DoubaoUnwatermark.isUnwatermarkedVideoURL(cleanUrl) && !/unwatermarked/i.test(cleanUrl)) {
        // still allow if decrypt succeeded — some CDNs only mark in path differently
        console.warn("[doubao-clean-dl] clean url may lack unwatermarked marker", cleanUrl);
      }

      const filename = filenameFor(fallback, cleanUrl);
      setStatus("开始下载…");

      // Prefer chrome.downloads with direct CDN URL (no CORS blob needed).
      let result = await downloadViaBackground(cleanUrl, filename);
      if (!result.ok) {
        console.warn("[doubao-clean-dl] background download failed, try blob", result.error);
        await downloadViaBlob(cleanUrl, filename);
      }

      setStatus("已触发下载（无水印）。可上传到 Novaly「替换视频」。");
    } catch (err) {
      console.error(err);
      setStatus("失败：" + (err && err.message ? err.message : String(err)), true);
    } finally {
      btn.disabled = false;
      updateBadge();
    }
  });

  // Keep scraping occasionally — SPA navigations.
  let lastHref = location.href;
  setInterval(() => {
    if (location.href !== lastHref) {
      lastHref = location.href;
      state.apis = [];
      state.vids = [];
      updateBadge();
      setStatus("");
      window.postMessage({ source: "doubao-clean-dl-ext", type: "reset" }, "*");
      setTimeout(scrapePageHTML, 1000);
    }
  }, 1000);

  // Auto-dismiss intermittent 「下载电脑版」promo (下次提醒我 / X).
  function dismissDesktopPromo() {
    const roots = document.querySelectorAll(
      '[role="dialog"], dialog, [class*="modal" i], [class*="dialog" i], [class*="popup" i], [class*="overlay" i], [class*="mask" i]'
    );
    for (const root of roots) {
      const text = (root.innerText || "").slice(0, 400);
      if (!/下载电脑版|使用完整功能/.test(text)) continue;
      const prefer = ["下次提醒我", "下次再说"];
      for (const want of prefer) {
        for (const el of root.querySelectorAll('button, [role="button"], a, [role="link"]')) {
          const t = (el.textContent || "").trim().replace(/\s+/g, "");
          if (t === want || t.startsWith(want)) {
            try {
              el.click();
              return true;
            } catch (_) {}
          }
        }
      }
      for (const el of root.querySelectorAll('[aria-label*="关闭"], [aria-label*="close"]')) {
        try {
          el.click();
          return true;
        } catch (_) {}
      }
    }
    return false;
  }
  dismissDesktopPromo();
  let promoTimer = 0;
  const promoObserver = new MutationObserver(() => {
    if (promoTimer) return;
    promoTimer = setTimeout(() => {
      promoTimer = 0;
      dismissDesktopPromo();
    }, 300);
  });
  if (document.documentElement) {
    promoObserver.observe(document.documentElement, { childList: true, subtree: true });
  }
  setTimeout(() => {
    promoObserver.disconnect();
    if (promoTimer) clearTimeout(promoTimer);
  }, 60_000);
})();
