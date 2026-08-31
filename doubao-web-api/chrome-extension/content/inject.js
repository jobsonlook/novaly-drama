(() => {
  if (window.__doubaoCleanDLInjected) return;
  window.__doubaoCleanDLInjected = true;

  const store = {
    apis: [],
    vids: [],
  };

  function decodeValue(value) {
    return String(value || "")
      .replace(/\\u0026/gi, "&")
      .replace(/&amp;/g, "&")
      .replace(/\\\//g, "/")
      .replace(/\\"/g, '"')
      .trim();
  }

  function interestingURL(url) {
    return /\/im\/chain\/single|\/video\/fplay\/|fallback_api|get_play_info|samantha\/media/i.test(
      String(url || "")
    );
  }

  function extractFallbackAPIs(text) {
    if (!text) return [];
    const decoded = String(text)
      .replace(/\\u0026/g, "&")
      .replace(/&amp;/g, "&")
      .replace(/\\\//g, "/");
    const out = [];
    const seen = new Set();
    const add = (raw) => {
      let u = decodeValue(raw).replace(/[\\"',]+$/g, "").trim();
      if (!u.startsWith("http") || !u.includes("/video/fplay/")) return;
      if (seen.has(u)) return;
      seen.add(u);
      out.push(u);
    };
    const urlRe = /https:\/\/(?:vas-lf-x\.snssdk\.com|[^"'\\\s<>]+)\/video\/fplay\/[^\s"'<>\\]+/g;
    let m;
    while ((m = urlRe.exec(decoded))) add(m[0]);
    const keyRe = /"fallback_api"\s*:\s*"((?:\\.|[^"\\])+)"/g;
    while ((m = keyRe.exec(text))) add(m[1]);
    const escRe = /fallback_api\\":\\"(.*?)\\"/g;
    while ((m = escRe.exec(text))) add(m[1]);
    return out;
  }

  function extractVids(text) {
    if (!text) return [];
    const out = [];
    const seen = new Set();
    const re = /"(?:vid|video_id)"\s*:\s*"(v0[A-Za-z0-9_-]{8,})"/g;
    let m;
    while ((m = re.exec(text))) {
      if (seen.has(m[1])) continue;
      seen.add(m[1]);
      out.push(m[1]);
    }
    return out;
  }

  function publish() {
    window.postMessage(
      {
        source: "doubao-clean-dl",
        type: "captured",
        apis: store.apis.slice(),
        vids: store.vids.slice(),
      },
      "*"
    );
  }

  function ingest(text) {
    if (!text || typeof text !== "string") return;
    if (!text.includes("fplay") && !text.includes("fallback_api") && !/v0[A-Za-z0-9_-]{8,}/.test(text)) {
      return;
    }
    const apis = extractFallbackAPIs(text);
    const vids = extractVids(text);
    let changed = false;
    for (const api of apis) {
      if (!store.apis.includes(api)) {
        store.apis.push(api);
        changed = true;
      }
    }
    for (const vid of vids) {
      if (!store.vids.includes(vid)) {
        store.vids.push(vid);
        changed = true;
      }
    }
    if (changed) publish();
  }

  const origFetch = window.fetch;
  window.fetch = async function (...args) {
    const res = await origFetch.apply(this, args);
    try {
      const url = typeof args[0] === "string" ? args[0] : (args[0] && args[0].url) || "";
      if (interestingURL(url)) {
        const clone = res.clone();
        clone
          .text()
          .then((t) => ingest(t))
          .catch(() => {});
      }
    } catch (_) {}
    return res;
  };

  const OrigXHR = window.XMLHttpRequest;
  function PatchedXHR() {
    const xhr = new OrigXHR();
    const open = xhr.open;
    xhr.open = function (method, url, ...rest) {
      this.__doubaoCleanURL = String(url || "");
      return open.call(this, method, url, ...rest);
    };
    xhr.addEventListener("load", function () {
      try {
        if (interestingURL(this.__doubaoCleanURL) && typeof this.responseText === "string") {
          ingest(this.responseText);
        }
      } catch (_) {}
    });
    return xhr;
  }
  window.XMLHttpRequest = PatchedXHR;

  window.addEventListener("message", (ev) => {
    if (!ev.data || ev.data.source !== "doubao-clean-dl-ext") return;
    if (ev.data.type === "ping") {
      window.postMessage({ source: "doubao-clean-dl", type: "pong", apis: store.apis, vids: store.vids }, "*");
    }
    if (ev.data.type === "reset") {
      store.apis = [];
      store.vids = [];
      try {
        ingest(document.documentElement ? document.documentElement.innerHTML : "");
      } catch (_) {}
      publish();
    }
  });

  // Initial scrape of current HTML (history pages often embed escaped JSON).
  try {
    ingest(document.documentElement ? document.documentElement.innerHTML : "");
  } catch (_) {}
})();
