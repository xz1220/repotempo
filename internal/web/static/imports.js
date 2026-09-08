(() => {
  "use strict";
  const panel = document.querySelector("[data-import-status]");
  if (!panel || panel.dataset.terminal === "true") return;
  let url;
  try { url = new URL(panel.dataset.pollUrl, window.location.href); } catch { return; }
  if (url.origin !== window.location.origin || !/^\/watch\/imports\/[A-Za-z0-9_-]{16,101}\/status$/.test(url.pathname)) return;
  let timer, stopped = false, failures = 0, attempts = 0, busy = false;
  const stage = panel.querySelector("[data-import-stage]");
  const message = panel.querySelector("[data-import-message]");
  function schedule(delay = 3000) {
    clearTimeout(timer);
    if (!stopped && !document.hidden) timer = setTimeout(poll, delay);
  }
  function pause() {
    stopped = true;
    clearTimeout(timer);
    if (message) message.textContent = panel.dataset.pausedLabel;
  }
  async function poll() {
    if (stopped || busy || document.hidden) return;
    if (++attempts > 120) { pause(); return; }
    busy = true;
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 10000);
    try {
      const response = await fetch(url.href, {credentials:"same-origin", cache:"no-store", signal:controller.signal, headers:{Accept:"application/json"}});
      if (!response.ok) throw new Error("status unavailable");
      const value = await response.json();
      if (stopped) return;
      if (typeof value.label !== "string" || typeof value.message !== "string" || typeof value.terminal !== "boolean" || !["queued","resolving","reading","classifying","done","partial","failed"].includes(value.stage)) throw new Error("invalid status");
      failures = 0;
      if (stage) stage.textContent = value.label;
      if (message) message.textContent = value.message;
      if (value.terminal) { stopped = true; window.location.reload(); return; }
    } catch {
      if (++failures >= 3) { pause(); return; }
    } finally { clearTimeout(timeout); busy = false; }
    schedule(failures ? 6000 : 3000);
  }
  document.addEventListener("visibilitychange", () => { if (document.hidden) clearTimeout(timer); else schedule(0); });
  window.addEventListener("pagehide", () => { stopped = true; clearTimeout(timer); });
  window.addEventListener("pageshow", event => { if (event.persisted && attempts < 120) { stopped = false; failures = 0; schedule(0); } });
  schedule();
})();
