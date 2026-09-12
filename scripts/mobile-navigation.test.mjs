import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";

const script = await readFile(new URL("../internal/web/static/app.js", import.meta.url), "utf8");

function page(mobile = true) {
  const document = new EventTarget();
  class Element extends EventTarget {
    attributes = new Map();
    children = [];
    hidden = false;
    inert = false;
    disabled = false;
    dataset = {};
    classes = new Set();
    classList = {
      add: (name) => this.classes.add(name),
      contains: (name) => this.classes.has(name),
      toggle: (name, force) => force ? this.classes.add(name) : this.classes.delete(name),
    };
    setAttribute(name, value) { this.attributes.set(name, String(value)); }
    getAttribute(name) { return this.attributes.get(name) ?? null; }
    removeAttribute(name) { this.attributes.delete(name); }
    querySelector() { return null; }
    querySelectorAll() { return []; }
    contains(target) { return target === this || this.children.some((child) => child.contains(target)); }
    getClientRects() { return this.hidden ? [] : [{}]; }
    focus() { document.activeElement = this; }
    click() { this.dispatchEvent(new Event("click")); }
  }
  const sidebar = new Element();
  const toggle = new Element();
  const brand = new Element();
  const trends = new Element();
  const projects = new Element();
  const account = new Element();
  const hiddenMenuItem = new Element();
  hiddenMenuItem.hidden = true;
  const source = new Element();
  sidebar.children = [brand, trends, projects, account, source, hiddenMenuItem];
  sidebar.querySelectorAll = (selector) => selector === ".primary-nav a" ? [trends, projects] : sidebar.children;
  sidebar.querySelector = (selector) => ({ ".primary-nav a": trends, "[data-account-trigger]": account })[selector] ?? null;
  const dialog = new Element();
  dialog.tagName = "DIALOG";
  dialog.open = false;
  const main = new Element();
  const skip = new Element();
  const exportButton = new Element();
  main.children = [toggle, exportButton];
  const body = new Element();
  body.children = [skip, sidebar, main];
  document.body = body;
  document.documentElement = new Element();
  document.documentElement.lang = "en";
  document.activeElement = body;
  document.querySelector = (selector) => ({
    "#app-sidebar": sidebar,
    "[data-sidebar-toggle]": toggle,
    ".app-main": main,
    ".skip-link": skip,
    "dialog[open]": dialog.open ? dialog : null,
  })[selector] ?? null;
  document.querySelectorAll = (selector) => selector === ".app-main, .skip-link" ? [main, skip] : [];
  const media = new EventTarget();
  media.matches = mobile;
  const window = new EventTarget();
  window.matchMedia = () => media;
  vm.runInNewContext(script, { document, window, HTMLInputElement: Element, HTMLSelectElement: Element });
  const key = (value, shiftKey = false) => {
    const event = new Event("keydown", { cancelable: true });
    Object.defineProperties(event, { key: { value }, shiftKey: { value: shiftKey } });
    document.dispatchEvent(event);
    return event;
  };
  const resize = (mobile) => { media.matches = mobile; media.dispatchEvent(new Event("change")); };
  return { document, sidebar, toggle, brand, trends, projects, account, hiddenMenuItem, source, main, skip, exportButton, dialog, key, resize };
}

test("closed mobile navigation is unavailable to keyboard and assistive technology", () => {
  const p = page();
  assert.equal(p.sidebar.inert, true);
  assert.equal(p.sidebar.getAttribute("aria-hidden"), "true");
  assert.equal(p.main.inert, false);
  assert.equal(p.toggle.getAttribute("aria-expanded"), "false");
});

test("opening mobile navigation focuses a destination and disables the obscured page", () => {
  const p = page();
  p.toggle.focus();
  p.toggle.click();
  assert.equal(p.document.activeElement, p.trends);
  assert.equal(p.sidebar.inert, false);
  assert.equal(p.main.inert, true);
  assert.equal(p.skip.inert, true);
  assert.equal(p.sidebar.getAttribute("aria-hidden"), null);
});

test("Tab wraps at both ends of the open menu and skips hidden account actions", () => {
  const p = page();
  p.toggle.click();
  p.source.focus();
  assert.equal(p.key("Tab").defaultPrevented, true);
  assert.equal(p.document.activeElement, p.brand);
  assert.equal(p.key("Tab", true).defaultPrevented, true);
  assert.equal(p.document.activeElement, p.source);
  p.projects.focus();
  assert.equal(p.key("Tab").defaultPrevented, false);
});

test("Escape and an outside click restore the trigger and background interaction", () => {
  const p = page();
  p.toggle.click();
  assert.equal(p.key("Escape").defaultPrevented, true);
  assert.equal(p.document.activeElement, p.toggle);
  assert.equal(p.sidebar.inert, true);
  assert.equal(p.main.inert, false);
  assert.equal(p.skip.inert, false);
  p.toggle.click();
  p.document.dispatchEvent(new Event("click"));
  assert.equal(p.document.activeElement, p.toggle);
  assert.equal(p.sidebar.classList.contains("is-open"), false);
});

test("a focus move outside an open mobile navigation returns to the menu", () => {
  const p = page();
  p.toggle.click();
  p.exportButton.focus();
  p.document.dispatchEvent(new Event("focusin"));
  assert.equal(p.document.activeElement, p.trends);
});

test("desktop resize restores both regions and does not trap desktop keyboard navigation", () => {
  const p = page();
  p.toggle.click();
  p.resize(false);
  assert.equal(p.sidebar.inert, false);
  assert.equal(p.sidebar.getAttribute("aria-hidden"), null);
  assert.equal(p.main.inert, false);
  assert.equal(p.skip.inert, false);
  assert.equal(p.sidebar.classList.contains("is-open"), false);
  p.source.focus();
  assert.equal(p.key("Tab").defaultPrevented, false);
  assert.equal(p.document.activeElement, p.source);
});

test("shrinking a focused desktop sidebar moves focus to the visible menu trigger", () => {
  const p = page(false);
  assert.equal(p.sidebar.inert, false);
  p.projects.focus();
  p.resize(true);
  assert.equal(p.sidebar.inert, true);
  assert.equal(p.document.activeElement, p.toggle);
});

test("an open Agent dialog owns focus and Escape until it closes", () => {
  const p = page();
  p.toggle.click();
  p.dialog.open = true;
  p.dialog.focus();
  p.document.dispatchEvent(new Event("focusin"));
  assert.equal(p.document.activeElement, p.dialog);
  assert.equal(p.key("Tab").defaultPrevented, false);
  assert.equal(p.key("Escape").defaultPrevented, false);
  p.document.dispatchEvent(new Event("click"));
  assert.equal(p.sidebar.classList.contains("is-open"), true);
  p.dialog.open = false;
  const close = new Event("close");
  Object.defineProperty(close, "target", { value: p.dialog });
  p.document.dispatchEvent(close);
  assert.equal(p.document.activeElement, p.account);
});
