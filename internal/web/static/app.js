(() => {
  "use strict";

  document.documentElement.classList.add("js");

  const toggle = document.querySelector("[data-nav-toggle]");
  const navigation = document.querySelector("[data-navigation]");

  if (toggle && navigation) {
    const desktop = window.matchMedia("(min-width: 861px)");
    const navigationLinks = navigation.querySelectorAll("a[href]");

    const setNavigation = (open, { restoreFocus = false } = {}) => {
      navigation.classList.toggle("is-open", open);
      toggle.setAttribute("aria-expanded", String(open));
      navigation.toggleAttribute("inert", !desktop.matches && !open);

      if (restoreFocus) {
        toggle.focus();
      }
    };

    const closeNavigation = (restoreFocus = false) => {
      const focusWasInside = navigation.contains(document.activeElement);
      setNavigation(false, { restoreFocus: restoreFocus || focusWasInside });
    };

    toggle.addEventListener("click", () => {
      const expanded = toggle.getAttribute("aria-expanded") === "true";
      setNavigation(!expanded);

      if (!expanded) {
        navigationLinks[0]?.focus();
      }
    });

    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && toggle.getAttribute("aria-expanded") === "true") {
        closeNavigation(true);
      }
    });

    document.addEventListener("click", (event) => {
      if (
        toggle.getAttribute("aria-expanded") === "true" &&
        !toggle.contains(event.target) &&
        !navigation.contains(event.target)
      ) {
        closeNavigation();
      }
    });

    navigationLinks.forEach((link) => {
      link.addEventListener("click", () => closeNavigation());
    });

    desktop.addEventListener("change", () => closeNavigation());
    closeNavigation();
  }

  document.querySelectorAll("[data-submit-on-change]").forEach((control) => {
    control.addEventListener("change", () => {
      if (control.form) {
        control.form.requestSubmit();
      }
    });
  });
})();
