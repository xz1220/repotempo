(() => {
  "use strict";

  const storageKey = "repotempo:reading-position:v1";
  const maxAge = 8 * 60 * 60 * 1000;
  const maxRecords = 40;
  const list = document.querySelector("[data-list-url]");

  function listTarget(raw) {
    try {
      const url = new URL(raw, window.location.href);
      if (url.origin !== window.location.origin || url.pathname !== "/repositories" || url.username || url.password || url.href.length > 8192) return null;
      if (url.hash && !/^#project-[1-9][0-9]{0,18}$/.test(url.hash)) return null;
      url.searchParams.sort();
      return { key: url.pathname + url.search + url.hash, anchor: url.hash.slice(1) };
    } catch { return null; }
  }

  function readState() {
    try {
      const raw = JSON.parse(window.sessionStorage.getItem(storageKey) || "{}");
      const now = Date.now();
      const records = Array.isArray(raw.records) ? raw.records.filter(record =>
        record && typeof record.key === "string" && record.key.length <= 8192 &&
        /^project-[1-9][0-9]{0,18}$/.test(record.anchor || "") &&
        Number.isFinite(record.offset) && Math.abs(record.offset) <= 1e7 &&
        Number.isFinite(record.at) && record.at <= now && now - record.at < maxAge &&
        Array.isArray(record.expanded) && record.expanded.length <= 100 &&
        record.expanded.every(id => /^[1-9][0-9]{0,18}$/.test(id))
      ).slice(-maxRecords) : [];
      const pending = raw.pending && typeof raw.pending.key === "string" &&
        Number.isFinite(raw.pending.at) && raw.pending.at <= now && now - raw.pending.at < 10 * 60 * 1000 ? raw.pending : null;
      return { records, pending };
    } catch { return { records: [], pending: null }; }
  }

  function saveState(state) {
    try { window.sessionStorage.setItem(storageKey, JSON.stringify(state)); }
    catch { /* The server-provided cursor and fragment still work without storage. */ }
  }

  function remember(link) {
    if (!list) return;
    let destination;
    try { destination = new URL(link.href, window.location.href); } catch { return; }
    if (destination.origin !== window.location.origin || !/^\/repositories\/[1-9][0-9]*$/.test(destination.pathname)) return;
    const target = listTarget(destination.searchParams.get("return_to"));
    const current = listTarget(list.dataset.listUrl);
    if (!target?.anchor || !current || target.key.split("#")[0] !== current.key) return;
    const anchor = document.getElementById(target.anchor);
    if (!anchor) return;
    const offset = anchor.getBoundingClientRect().top;
    if (!Number.isFinite(offset) || Math.abs(offset) > 1e7) return;
    const expanded = [...document.querySelectorAll(".project-more-tags[open]")]
      .map(detail => detail.closest("[data-repository-id]")?.dataset.repositoryId)
      .filter(id => /^[1-9][0-9]{0,18}$/.test(id || "")).slice(0, 100);
    const state = readState();
    state.records = state.records.filter(record => record.key !== target.key);
    state.records.push({ key: target.key, anchor: target.anchor, offset, expanded, at: Date.now() });
    state.records = state.records.slice(-maxRecords);
    state.pending = null;
    saveState(state);
  }

  for (const link of document.querySelectorAll("[data-repository-detail]")) {
    link.addEventListener("click", () => remember(link));
    link.addEventListener("auxclick", () => remember(link));
  }
  for (const link of document.querySelectorAll("[data-library-return]")) {
    const markReturn = () => {
      const target = listTarget(link.href);
      if (!target?.anchor) return;
      const state = readState();
      state.pending = { key: target.key, at: Date.now() };
      saveState(state);
    };
    link.addEventListener("click", markReturn);
    link.addEventListener("auxclick", markReturn);
  }

  if (!list) return;
  window.addEventListener("pageshow", event => {
    // Native Back/Forward and bfcache own their scroll positions. Explicit
    // on-page return links use the separately saved position below.
    const navigation = window.performance?.getEntriesByType?.("navigation")?.[0];
    if (event.persisted || navigation?.type === "back_forward" || navigation?.type === "reload") return;
    const target = listTarget(list.dataset.listUrl + window.location.hash);
    if (!target?.anchor) return;
    const state = readState();
    if (state.pending?.key !== target.key) return;
    state.pending = null;
    saveState(state);
    const record = state.records.find(item => item.key === target.key && item.anchor === target.anchor);
    if (!record || !document.getElementById(record.anchor)) return;
    let interrupted = false;
    const interrupt = () => { interrupted = true; };
    const inputs = ["wheel", "touchstart", "pointerdown", "keydown"];
    inputs.forEach(type => window.addEventListener(type, interrupt, { once: true, passive: true }));
    window.requestAnimationFrame(() => window.requestAnimationFrame(() => {
      inputs.forEach(type => window.removeEventListener(type, interrupt));
      if (interrupted) return;
      for (const detail of document.querySelectorAll(".project-more-tags")) {
        const id = detail.closest("[data-repository-id]")?.dataset.repositoryId;
        detail.open = record.expanded.includes(id);
      }
      const anchor = document.getElementById(record.anchor);
      if (!anchor) return;
      const top = anchor.getBoundingClientRect().top + window.scrollY - record.offset;
      if (!Number.isFinite(top)) return;
      window.scrollTo({ top: Math.max(0, top), left: 0, behavior: "instant" });
      // Remove only the consumed anchor, preserving history.state and every
      // query parameter. A later refresh must not jump to an old reading mark.
      try { window.history.replaceState(window.history.state, "", window.location.pathname + window.location.search); }
      catch { /* Keeping the valid fragment is an acceptable fallback. */ }
    }));
  });
})();
