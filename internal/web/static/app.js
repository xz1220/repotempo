(() => {
  "use strict";

  document.documentElement.classList.add("js");
  document.querySelectorAll("[data-submit-on-change]").forEach((control) => {
    control.addEventListener("change", () => control.form?.requestSubmit());
  });

  const form = document.querySelector(".watch-form");
  const submit = form?.querySelector('button[type="submit"]');
  if (form && submit) {
    const label = submit.textContent;
    form.addEventListener("submit", () => {
      submit.disabled = true;
      submit.textContent = document.documentElement.lang === "zh-CN"
        ? "正在提交…"
        : "Submitting…";
      form.setAttribute("aria-busy", "true");
    });
    window.addEventListener("pageshow", () => {
      submit.disabled = false;
      submit.textContent = label;
      form.removeAttribute("aria-busy");
    });
  }
})();
