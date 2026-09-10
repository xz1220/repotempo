(() => {
  "use strict";
  let modal;
  let pending = false;
  let active = true;
  let controller;
  const panel = () => modal?.open ? modal.querySelector("[data-agent-access-panel]") : document.querySelector("main [data-agent-access-panel]");
  function forgetSecret(root) {
    root?.querySelectorAll("[data-secret]").forEach(input => { input.value = ""; input.removeAttribute("value"); });
    root?.querySelectorAll("[data-new-secret]").forEach(section => section.remove());
  }
  function tab(root, name) {
    root.querySelectorAll("[data-api-panel]").forEach(section => { section.hidden = section.dataset.apiPanel !== name; section.classList.toggle("is-visible", !section.hidden); });
    root.querySelectorAll("[data-api-tab]").forEach(button => button.setAttribute("aria-pressed", String(button.dataset.apiTab === name)));
  }
  async function load(url, options) {
    const target = new URL(url, location.href);
    if (target.origin !== location.origin || !target.pathname.startsWith("/account/api")) throw new Error("Invalid account URL");
    controller = new AbortController();
    const response = await fetch(target, {credentials:"same-origin", cache:"no-store", signal:controller.signal, ...options});
    if (new URL(response.url).pathname === "/auth/login") { location.assign(response.url); return null; }
    const markup = await response.text();
    if (!active) return null;
    const documentResult = new DOMParser().parseFromString(markup, "text/html");
    const result = documentResult.querySelector("[data-agent-access-panel]");
    if (!result) throw new Error("Account view unavailable");
    return document.importNode(result, true);
  }
  document.addEventListener("click", async event => {
    const openLink = event.target.closest("a[data-agent-access]");
    if (openLink && !event.metaKey && !event.ctrlKey && !event.shiftKey && event.button === 0) {
      event.preventDefault();
      if (pending) return;
      pending = true;
      try {
        const content = await load(openLink.href);
        if (!content) return;
        if (!modal) {
          modal = document.createElement("dialog"); modal.className = "api-dialog"; modal.setAttribute("aria-labelledby", "api-access-title"); document.body.append(modal);
          modal.addEventListener("cancel", event => { if (pending) event.preventDefault(); });
          modal.addEventListener("close", () => { forgetSecret(modal); modal.replaceChildren(); openLink.focus(); });
        }
        forgetSecret(modal); modal.replaceChildren(content); modal.showModal();
        document.querySelector("[data-account-menu]")?.setAttribute("hidden", ""); document.querySelector("[data-account-trigger]")?.setAttribute("aria-expanded", "false");
      } catch { location.assign(openLink.href); } finally { pending = false; }
      return;
    }
    const root = event.target.closest("[data-agent-access-panel]");
    if (!root) return;
    const action = event.target.closest("button"); if (!action) return;
    if (action.hasAttribute("data-close-agent") && !pending) { forgetSecret(root); modal?.close(); }
    if (action.dataset.apiTab) tab(root, action.dataset.apiTab);
    if (action.hasAttribute("data-create-key")) { root.querySelector("[data-key-create]")?.classList.add("is-visible"); root.querySelector("#api-key-name")?.focus(); }
    if (action.hasAttribute("data-cancel-create")) root.querySelector("[data-key-create]")?.classList.remove("is-visible");
    if (action.hasAttribute("data-secret-saved")) { forgetSecret(root); tab(root, "connect"); }
    if (action.hasAttribute("data-reveal-secret")) {
      const input = root.querySelector("[data-secret]"); if (input) { input.type = input.type === "password" ? "text" : "password"; action.setAttribute("aria-pressed", String(input.type === "text")); }
    }
    if (action.dataset.copyTarget) {
      const input = root.querySelector(`#${CSS.escape(action.dataset.copyTarget)}`); const status = root.querySelector("[data-api-status]");
      try { await navigator.clipboard.writeText(input.value); status.textContent = status.dataset.copySuccess; }
      catch { input?.focus(); input?.select(); status.textContent = status.dataset.copyFailed; }
    }
  });
  document.addEventListener("submit", async event => {
    const form = event.target; const root = form.closest("[data-agent-access-panel]");
    if (!root || !(form instanceof HTMLFormElement) || pending) { if (root && pending) event.preventDefault(); return; }
    event.preventDefault(); pending = true;
    const status = root.querySelector("[data-api-status]");
    try {
      const body = new URLSearchParams(new FormData(form));
      const replacement = await load(form.action, {method:"POST", body});
      if (replacement && active && root.isConnected) { forgetSecret(root); root.replaceWith(replacement); }
    } catch { status.textContent = status.dataset.requestFailed; }
    finally { pending = false; }
  });
  window.addEventListener("pagehide", () => { active = false; controller?.abort(); forgetSecret(document); });
  window.addEventListener("pageshow", () => { active = true; });
})();
