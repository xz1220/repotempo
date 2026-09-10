(() => {
  "use strict";
  if (document.body.dataset.authEnabled !== "true") return;
  // A cached privileged document must re-check its session after logout or
  // expiry; the server is always authoritative, not this UI hint.
  window.addEventListener("pageshow", event => { if (event.persisted) window.location.reload(); });
})();
