/* global DoubaoUnwatermark */
(function (root) {
  const QAAB_SALT_HEX =
    "4dd4c2e6b83162090e52b3c7a6733ba41cb2462b829ab58a196b39db57177524f49baf7f08e8d68d26a72e37c1a95a2f1f05a51892aef2949732b62a38aadd58";

  function hexToBytes(hex) {
    const b = new Uint8Array(hex.length / 2);
    for (let i = 0; i < b.length; i++) b[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
    return b;
  }

  function concatBytes(a, b) {
    const o = new Uint8Array(a.length + b.length);
    o.set(a, 0);
    o.set(b, a.length);
    return o;
  }

  function padBase64(t) {
    return t + "=".repeat((4 - (t.length % 4)) % 4);
  }

  function asciiUrlFromBytes(bytes) {
    if (!bytes || !bytes.length) return "";
    for (const x of bytes) if (x !== 9 && x !== 10 && x !== 13 && (x < 32 || x > 126)) return "";
    return new TextDecoder().decode(bytes);
  }

  function base64DecodeLoose(text) {
    const input = String(text || "").trim();
    const variants = [
      input,
      input.replace(/[$@#]/g, (c) => ({ $: "_", "@": "/", "#": "." }[c])),
      input.replace(/[$@#]/g, (c) => ({ $: "+", "@": "/", "#": "=" }[c])),
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
    if (!payload.length || payload.length % 16 !== 0) return "";
    try {
      const key = await crypto.subtle.importKey("raw", keyBytes, "AES-CBC", false, ["decrypt"]);
      const plain = new Uint8Array(await crypto.subtle.decrypt({ name: "AES-CBC", iv: ivBytes }, key, payload));
      const direct = asciiUrlFromBytes(plain);
      if (/^https?:\/\//i.test(direct)) return direct;
      const url = asciiUrlFromBytes(stripPkcs7(plain));
      return /^https?:\/\//i.test(url) ? url : "";
    } catch (_) {
      return "";
    }
  }

  async function decodeQaabToken(token, keySeed) {
    const data = base64DecodeLoose(token);
    const seed = base64DecodeLoose(keySeed);
    if (!data || !seed) return "";
    const digest1 = await crypto.subtle.digest("SHA-512", seed.slice(0, 32));
    const digest2 = new Uint8Array(
      await crypto.subtle.digest("SHA-512", concatBytes(new Uint8Array(digest1), hexToBytes(QAAB_SALT_HEX)))
    );
    const key = digest2.slice(0, 16);
    const iv = digest2.slice(16, 32);
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
    const entries =
      videoList && typeof videoList === "object" && Object.keys(videoList).length
        ? Object.values(videoList)
        : [data];
    let best = null;
    for (const entry of entries) {
      if (!entry || typeof entry !== "object") continue;
      const token = entry.main_url || entry.play_url || "";
      if (typeof token !== "string" || !token.trim()) continue;
      const score =
        Number(entry.bitrate || entry.real_bitrate || 0) +
        Number(entry.vwidth || entry.width || 0) * Number(entry.vheight || entry.height || 0);
      if (!best || score > best.score) best = { token: token.trim(), score };
    }
    return best ? best.token : "";
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
      let u = String(raw || "")
        .replace(/\\u0026/g, "&")
        .replace(/&amp;/g, "&")
        .replace(/\\\//g, "/")
        .replace(/[\\"',]+$/g, "")
        .trim();
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

  function preferFallbackAPIForVid(apis, vid) {
    if (!apis || !apis.length) return "";
    if (vid) {
      for (let i = apis.length - 1; i >= 0; i--) {
        if (apis[i].includes(vid)) return apis[i];
      }
    }
    return apis[apis.length - 1];
  }

  function vidFromFallbackAPI(api) {
    const m = String(api || "").match(/\/(v0[A-Za-z0-9_-]{8,})(?:\?|$)/);
    return m ? m[1] : "";
  }

  function isUnwatermarkedVideoURL(raw) {
    const u = String(raw || "").toLowerCase();
    return u.includes("lr=unwatermarked") || u.includes("logo_type=unwatermarked");
  }

  async function resolveUnwatermarkedViaFallback(fallbackApi) {
    const u = new URL(fallbackApi);
    u.searchParams.set("channel", "no");
    u.searchParams.set("codec_type", "8");
    u.searchParams.set("logo_type", "unwatermarked");
    const resp = await fetch(u.toString(), {
      method: "GET",
      credentials: "omit",
      headers: { accept: "application/json,text/plain,*/*" },
    });
    const text = await resp.text();
    let payload;
    try {
      payload = JSON.parse(text);
    } catch (e) {
      throw new Error("fplay json parse failed: " + String(e));
    }
    const videoInfo = payload.video_info || (payload.data && payload.data.video_info) || payload;
    const data = (videoInfo && videoInfo.data) || videoInfo || {};
    const dataObj = data && typeof data === "object" ? data : {};
    const token = pickMainUrlToken(dataObj);
    const keySeed = findKeySeedDeep(payload) || findKeySeedDeep(fallbackApi);
    const cleanUrl = token ? await decodeMainUrl(token, keySeed) : "";
    if (!cleanUrl) {
      throw new Error("empty clean url (token=" + (token ? token.slice(0, 24) : "") + ")");
    }
    return cleanUrl;
  }

  root.DoubaoUnwatermark = {
    extractFallbackAPIs,
    preferFallbackAPIForVid,
    vidFromFallbackAPI,
    isUnwatermarkedVideoURL,
    resolveUnwatermarkedViaFallback,
  };
})(typeof globalThis !== "undefined" ? globalThis : window);
