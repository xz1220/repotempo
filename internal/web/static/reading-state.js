(() => {
  "use strict";
  if (document.body?.dataset.readingEnabled === "false") return;

  // One key per immutable repository ID avoids overwriting another tab's marks
  // for other projects. These marks never leave this browser and origin.
  const user = document.body?.dataset.readingUser;
  const prefix = /^[1-9][0-9]*$/.test(user || "")
    ? `repotempo:reading:v2:user:${user}:`
    : "repotempo:reading:v1:";
  const groups = new Map();

  function render(id, read) {
    for (const entry of groups.get(id) || []) {
      entry.read = read;
      entry.card.classList.toggle("is-read", read);
      entry.card.dataset.readingState = read ? "read" : "unread";
      entry.button.setAttribute("aria-pressed", String(read));
      entry.button.textContent = read
        ? entry.button.dataset.readingMarkUnread
        : entry.button.dataset.readingMarkRead;
      entry.note.textContent = "";
      entry.note.hidden = true;
      delete entry.note.dataset.readingError;
    }
  }

  function showError(id, kind) {
    for (const entry of groups.get(id) || []) {
      // Keep the last known state when saving fails; do not claim the toggle
      // persisted or silently replace an unread/read value with a fallback.
      entry.note.textContent = entry.button.dataset[kind];
      entry.note.hidden = false;
      entry.note.dataset.readingError = "true";
    }
  }

  function refresh(id) {
    try {
      render(id, window.localStorage.getItem(prefix + id) === "read");
    } catch {
      showError(id, "readingLoadError");
    }
  }

  function toggle(id, read) {
    try {
      const storage = window.localStorage;
      if (read) {
        storage.setItem(prefix + id, "read");
      } else {
        storage.removeItem(prefix + id);
      }
      render(id, read);
    } catch {
      showError(id, "readingSaveError");
    }
  }

  for (const card of document.querySelectorAll("[data-reading-repository]")) {
    const id = card.dataset.readingRepository;
    // Keep IDs as strings: GitHub IDs need not fit JavaScript's safe integer.
    if (!/^[1-9][0-9]*$/.test(id || "")) continue;
    const button = card.querySelector("[data-reading-toggle]");
    const note = card.querySelector("[data-reading-note]");
    if (!button || !note) continue;
    const entry = { card, button, note, read: false };
    if (!groups.has(id)) groups.set(id, []);
    groups.get(id).push(entry);
    button.hidden = false;
    // Honor the action currently shown to the user even if another tab wrote
    // the same key just before its storage event reached this tab.
    button.addEventListener("click", () => toggle(id, !entry.read));
  }

  if (groups.size === 0) return;
  const help = document.querySelector("[data-reading-help]");
  if (help) help.hidden = false;

  function refreshAll() {
    for (const id of groups.keys()) refresh(id);
  }

  refreshAll();
  // Returning from another page may restore a cached DOM without rerunning JS.
  window.addEventListener("pageshow", refreshAll);
  window.addEventListener("storage", (event) => {
    try {
      if (event.storageArea && event.storageArea !== window.localStorage) return;
    } catch {
      for (const id of groups.keys()) showError(id, "readingLoadError");
      return;
    }
    if (event.key === null) {
      refreshAll();
    } else if (typeof event.key === "string" && event.key.startsWith(prefix)) {
      const id = event.key.slice(prefix.length);
      if (groups.has(id)) refresh(id);
    }
  });
})();
