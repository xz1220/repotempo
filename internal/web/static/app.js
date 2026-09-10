(() => {
  "use strict";

  document.documentElement.classList.add("js");

  document.querySelectorAll("[data-submit-on-change]").forEach((control) => {
    control.addEventListener("change", () => control.form?.requestSubmit());
  });

  const watchForm = document.querySelector(".watch-form");
  const watchSubmit = watchForm?.querySelector('button[type="submit"]');
  if (watchForm && watchSubmit) {
    const label = watchSubmit.textContent;
    watchForm.addEventListener("submit", () => {
      watchSubmit.disabled = true;
      watchSubmit.textContent = document.documentElement.lang === "zh-CN"
        ? "正在提交…"
        : "Submitting…";
      watchForm.setAttribute("aria-busy", "true");
    });
    window.addEventListener("pageshow", () => {
      watchSubmit.disabled = false;
      watchSubmit.textContent = label;
      watchForm.removeAttribute("aria-busy");
    });
  }

  const filterToggle = document.querySelector("[data-filter-toggle]");
  const filterPanel = document.querySelector("[data-filter-panel]");
  if (filterToggle && filterPanel) {
    const filterCount = filterToggle.querySelector("[data-filter-count]");
    const updateFilterCount = () => {
      if (!filterCount) return;
      const search = filterPanel.querySelector("[data-library-search]")?.value.trim() || "";
      const period = filterPanel.querySelector('select[name="period"]')?.value || "1d";
      const sort = filterPanel.querySelector('select[name="sort"]')?.value || "stars";
      const legacyCount = ["topic", "source", "status"].filter((name) =>
        Boolean(filterPanel.querySelector(`input[name="${name}"]`)?.value),
      ).length;
      const count = Number(Boolean(search)) + Number(period !== "1d") + Number(sort !== "stars") + legacyCount;
      filterCount.textContent = count ? String(count) : "";
      filterCount.hidden = count === 0;
    };
    const setFilterOpen = (open) => {
      filterPanel.dataset.collapsed = String(!open);
      filterToggle.setAttribute("aria-expanded", String(open));
    };
    setFilterOpen(false);
    updateFilterCount();
    filterPanel.addEventListener("input", updateFilterCount);
    filterPanel.addEventListener("change", updateFilterCount);
    filterToggle.addEventListener("click", () => {
      setFilterOpen(filterToggle.getAttribute("aria-expanded") !== "true");
    });
  }

  const librarySearch = document.querySelector("[data-library-search]");
  if (librarySearch instanceof HTMLInputElement && librarySearch.form) {
    const exactTags = [...librarySearch.form.querySelectorAll("#repository-tags option")]
      .map((option) => option.value.trim().toLocaleLowerCase())
      .filter(Boolean);
    librarySearch.addEventListener("input", () => {
      librarySearch.name = "q";
    });
    librarySearch.form.addEventListener("submit", () => {
      const normalized = librarySearch.value.trim().toLocaleLowerCase();
      librarySearch.name = exactTags.includes(normalized) ? "tag" : "q";
    });
  }

  const list = document.querySelector("[data-library-list]");
  const viewButtons = [...document.querySelectorAll("[data-view-mode]")];
  if (list && viewButtons.length) {
    const storageKey = "repotempo:view:v1";
    let mode = "reading";
    try {
      if (window.localStorage.getItem(storageKey) === "compact") mode = "compact";
    } catch {
      mode = "reading";
    }
    const applyMode = (next, persist) => {
      mode = next === "compact" ? "compact" : "reading";
      list.classList.toggle("is-compact", mode === "compact");
      for (const button of viewButtons) {
        button.setAttribute("aria-pressed", String(button.dataset.viewMode === mode));
      }
      if (persist) {
        try {
          window.localStorage.setItem(storageKey, mode);
        } catch {
          // The selected view still applies for this document.
        }
      }
    };
    applyMode(mode, false);
    for (const button of viewButtons) {
      button.addEventListener("click", () => applyMode(button.dataset.viewMode, true));
    }
    window.addEventListener("storage", (event) => {
      if (event.key === storageKey) applyMode(event.newValue, false);
    });
  }

  const tabs = [...document.querySelectorAll('.library-views [role="tab"]')];
  tabs.forEach((tab, index) => {
    tab.addEventListener("keydown", (event) => {
      if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
      event.preventDefault();
      const targetIndex = event.key === "Home"
        ? 0
        : event.key === "End"
          ? tabs.length - 1
          : (index + (event.key === "ArrowRight" ? 1 : -1) + tabs.length) % tabs.length;
      window.location.assign(tabs[targetIndex].href);
    });
  });

  const pageSize = document.querySelector("[data-page-size]");
  if (pageSize instanceof HTMLSelectElement) {
    pageSize.addEventListener("change", () => {
      if (pageSize.value) window.location.assign(`${pageSize.value}#project-list`);
    });
  }

  const sidebar = document.querySelector("#app-sidebar");
  const sidebarToggle = document.querySelector("[data-sidebar-toggle]");
  if (sidebar && sidebarToggle) {
    const setSidebarOpen = (open, restoreFocus = false) => {
      sidebar.classList.toggle("is-open", open);
      sidebarToggle.setAttribute("aria-expanded", String(open));
      if (restoreFocus) sidebarToggle.focus();
    };
    sidebarToggle.addEventListener("click", () => {
      setSidebarOpen(!sidebar.classList.contains("is-open"));
    });
    sidebar.querySelectorAll(".primary-nav a").forEach((link) => {
      link.addEventListener("click", () => setSidebarOpen(false));
    });
    document.addEventListener("click", (event) => {
      if (!sidebar.classList.contains("is-open")) return;
      if (sidebar.contains(event.target) || sidebarToggle.contains(event.target)) return;
      setSidebarOpen(false);
    });
    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && sidebar.classList.contains("is-open")) {
        event.preventDefault();
        setSidebarOpen(false, true);
      }
    });
  }

  const accountRoot = document.querySelector("[data-account-root]");
  const accountTrigger = accountRoot?.querySelector("[data-account-trigger]");
  const accountMenu = accountRoot?.querySelector("[data-account-menu]");
  if (accountRoot && accountTrigger && accountMenu) {
    const closeAccountMenu = (restoreFocus = false) => {
      accountMenu.hidden = true;
      accountTrigger.setAttribute("aria-expanded", "false");
      if (restoreFocus) accountTrigger.focus();
    };
    accountTrigger.addEventListener("click", () => {
      const open = accountMenu.hidden;
      accountMenu.hidden = !open;
      accountTrigger.setAttribute("aria-expanded", String(open));
      if (open) accountMenu.querySelector("a, button:not(:disabled)")?.focus();
    });
    document.addEventListener("click", (event) => {
      if (!accountRoot.contains(event.target)) closeAccountMenu();
    });
    accountRoot.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && !accountMenu.hidden) {
        event.preventDefault();
        event.stopPropagation();
        closeAccountMenu(true);
      }
    });
  }

  const focusForms = [...document.querySelectorAll("[data-focus-form]")];
  focusForms.forEach((form) => {
    const submit = form.querySelector('button[type="submit"]');
    form.addEventListener("submit", () => {
      if (!submit) return;
      submit.disabled = true;
      form.setAttribute("aria-busy", "true");
    });
  });
  window.addEventListener("pageshow", () => {
    for (const form of focusForms) {
      form.removeAttribute("aria-busy");
      const submit = form.querySelector('button[type="submit"]');
      if (submit) submit.disabled = false;
    }
  });
})();
