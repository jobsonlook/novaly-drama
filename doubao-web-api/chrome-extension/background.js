chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (!msg || msg.type !== "download") return false;
  const url = String(msg.url || "").trim();
  const filename = String(msg.filename || "doubao-clean.mp4").replace(/[\\/:*?"<>|]+/g, "_");
  if (!url) {
    sendResponse({ ok: false, error: "empty url" });
    return false;
  }
  chrome.downloads
    .download({
      url,
      filename,
      saveAs: Boolean(msg.saveAs),
      conflictAction: "uniquify",
    })
    .then((id) => sendResponse({ ok: true, id }))
    .catch((err) => sendResponse({ ok: false, error: String(err && err.message ? err.message : err) }));
  return true;
});
