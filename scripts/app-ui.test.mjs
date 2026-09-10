import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";

const script = await readFile(new URL("../internal/web/static/app.js", import.meta.url), "utf8");

class Classes {
  values = new Set();
  add(name) { this.values.add(name); }
  contains(name) { return this.values.has(name); }
  toggle(name, force) {
    if (force === undefined ? !this.values.has(name) : force) this.values.add(name);
    else this.values.delete(name);
  }
}

class Element extends EventTarget {
  attributes = new Map();
  classList = new Classes();
  dataset = {};
  hidden = false;
  textContent = "";
  value = "";
  disabled = false;
  focused = false;
  children = new Set();
  setAttribute(name, value) { this.attributes.set(name, String(value)); }
  getAttribute(name) { return this.attributes.get(name) ?? null; }
  querySelector() { return null; }
  querySelectorAll() { return []; }
  contains(target) { return target === this || this.children.has(target); }
  focus() { this.focused = true; }
  click() { this.dispatchEvent(new Event("click")); }
}

class Input extends Element {}
class Select extends Element {}

class Storage {
  values = new Map();
  getItem(key) { return this.values.get(key) ?? null; }
  setItem(key, value) { this.values.set(key, value); }
}

function keyboardEvent(key) {
  const event = new Event("keydown", { cancelable: true });
  Object.defineProperty(event, "key", { value: key });
  return event;
}

function page() {
  const filterCount = new Element();
  filterCount.hidden = true;
  const filterToggle = new Element();
  filterToggle.setAttribute("aria-expanded", "false");
  filterToggle.querySelector = (selector) => selector === "[data-filter-count]" ? filterCount : null;
  const period = new Select();
  period.value = "1d";
  const sort = new Select();
  sort.value = "stars";
  const filterPanel = new Element();
  filterPanel.dataset.collapsed = "true";
  filterPanel.querySelector = (selector) => ({
    "[data-library-search]": null,
    'select[name="period"]': period,
    'select[name="sort"]': sort,
  })[selector] ?? null;

  const list = new Element();
  const reading = new Element();
  reading.dataset.viewMode = "reading";
  const compact = new Element();
  compact.dataset.viewMode = "compact";

  const sidebar = new Element();
  sidebar.querySelectorAll = () => [];
  const sidebarToggle = new Element();
  const outside = new Element();

  const accountRoot = new Element();
  const accountTrigger = new Element();
  accountTrigger.setAttribute("aria-expanded", "false");
  const accountMenu = new Element();
  accountMenu.hidden = true;
  const menuItem = new Element();
  accountMenu.querySelector = () => menuItem;
  accountRoot.children.add(accountTrigger);
  accountRoot.children.add(accountMenu);
  accountRoot.querySelector = (selector) => ({
    "[data-account-trigger]": accountTrigger,
    "[data-account-menu]": accountMenu,
  })[selector] ?? null;

  const document = new EventTarget();
  document.documentElement = { classList: new Classes(), lang: "zh-CN" };
  document.querySelector = (selector) => ({
    ".watch-form": null,
    "[data-filter-toggle]": filterToggle,
    "[data-filter-panel]": filterPanel,
    "[data-library-search]": null,
    "[data-library-list]": list,
    "[data-page-size]": null,
    "#app-sidebar": sidebar,
    "[data-sidebar-toggle]": sidebarToggle,
    "[data-account-root]": accountRoot,
  })[selector] ?? null;
  document.querySelectorAll = (selector) => ({
    "[data-submit-on-change]": [],
    "[data-view-mode]": [reading, compact],
    '.library-views [role="tab"]': [],
    "[data-focus-form]": [],
  })[selector] ?? [];

  const window = new EventTarget();
  const storage = new Storage();
  window.localStorage = storage;
  window.location = { assign() {} };

  vm.runInNewContext(script, {
    document,
    window,
    HTMLInputElement: Input,
    HTMLSelectElement: Select,
    Event,
    EventTarget,
  });
  return { accountMenu, accountRoot, accountTrigger, compact, document, filterCount, filterPanel, filterToggle, list, menuItem, outside, reading, sidebar, sidebarToggle, storage };
}

test("filter disclosure is collapsed by default and toggles without clearing controls", () => {
  const current = page();
  assert.equal(current.filterPanel.dataset.collapsed, "true");
  assert.equal(current.filterToggle.getAttribute("aria-expanded"), "false");
  current.filterToggle.click();
  assert.equal(current.filterPanel.dataset.collapsed, "false");
  assert.equal(current.filterToggle.getAttribute("aria-expanded"), "true");
  current.filterToggle.click();
  assert.equal(current.filterPanel.dataset.collapsed, "true");
});

test("reading and compact views change only their dedicated preference", () => {
  const current = page();
  assert.equal(current.list.classList.contains("is-compact"), false);
  current.compact.click();
  assert.equal(current.list.classList.contains("is-compact"), true);
  assert.equal(current.compact.getAttribute("aria-pressed"), "true");
  assert.equal(current.reading.getAttribute("aria-pressed"), "false");
  assert.equal(current.storage.getItem("repotempo:view:v1"), "compact");
  assert.equal([...current.storage.values.keys()].some((key) => key.startsWith("repotempo:reading:")), false);
});

test("mobile sidebar and account menu close on outside click or Escape", () => {
  const current = page();
  current.sidebarToggle.click();
  assert.equal(current.sidebar.classList.contains("is-open"), true);
  current.document.dispatchEvent(new Event("click"));
  assert.equal(current.sidebar.classList.contains("is-open"), false);
  current.sidebarToggle.click();
  current.document.dispatchEvent(keyboardEvent("Escape"));
  assert.equal(current.sidebar.classList.contains("is-open"), false);
  assert.equal(current.sidebarToggle.focused, true);

  current.accountTrigger.click();
  assert.equal(current.accountMenu.hidden, false);
  assert.equal(current.menuItem.focused, true);
  current.accountRoot.dispatchEvent(keyboardEvent("Escape"));
  assert.equal(current.accountMenu.hidden, true);
  assert.equal(current.accountTrigger.focused, true);
});
