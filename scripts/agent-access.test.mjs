import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";

const script = await readFile(new URL("../internal/web/static/agent-access.js", import.meta.url), "utf8");
const origin = "https://repotempo.example";
const fixtureSecret = "fixture-only-secret-no-authority";

class Events {
  listeners = new Map();
  addEventListener(type, callback) {
    this.listeners.set(type, [...(this.listeners.get(type) || []), callback]);
  }
  emit(type, properties = {}) {
    const event = { type, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; }, ...properties };
    const done = Promise.all((this.listeners.get(type) || []).map(callback => callback(event)));
    return { event, done };
  }
}

// Minimal native-control behavior needed by the credential lifecycle. In a
// textarea defaultValue reflects child text; an input reflects its value
// attribute. Assigning .value makes either control dirty without changing its
// default value, which is why checking only .value would miss retained secrets.
class Element extends Events {
  constructor(tagName = "div", attributes = {}, text = "") {
    super();
    this.tagName = tagName.toLowerCase();
    this.attributes = new Map(Object.entries(attributes));
    this.children = [];
    this.parent = null;
    this.dirtyValue = null;
    this.content = text;
    this.type = attributes.type || "text";
    this.focused = false;
    this.selected = false;
    this.open = false;
  }
  get dataset() {
    return Object.fromEntries([...this.attributes].filter(([key]) => key.startsWith("data-")).map(([key, value]) => [key.slice(5).replace(/-([a-z])/g, (_, character) => character.toUpperCase()), value]));
  }
  get value() { return this.dirtyValue ?? this.defaultValue; }
  set value(value) { this.dirtyValue = value; }
  get defaultValue() { return this.tagName === "textarea" ? this.content : this.attributes.get("value") || ""; }
  set defaultValue(value) { if (this.tagName === "textarea") this.content = value; else this.setAttribute("value", value); }
  get textContent() { return this.content; }
  set textContent(value) { this.content = value; this.replaceChildren(); }
  get isConnected() { return this.parent?.isConnected || false; }
  get href() { return new URL(this.attributes.get("href") || "", origin).href; }
  get action() { return new URL(this.attributes.get("action") || "", origin).href; }
  hasAttribute(name) { return this.attributes.has(name); }
  getAttribute(name) { return this.attributes.get(name) ?? null; }
  setAttribute(name, value) { this.attributes.set(name, String(value)); }
  removeAttribute(name) { this.attributes.delete(name); }
  append(...children) { for (const child of children) { child.remove(); child.parent = this; this.children.push(child); } }
  remove() {
    if (this.parent) this.parent.children = this.parent.children.filter(child => child !== this);
    this.parent = null;
  }
  replaceChildren(...children) { for (const child of [...this.children]) child.remove(); this.append(...children); }
  replaceWith(replacement) {
    const parent = this.parent;
    const index = parent.children.indexOf(this);
    this.remove(); replacement.remove(); replacement.parent = parent;
    parent.children.splice(index, 0, replacement);
  }
  matches(selector) {
    if (selector.startsWith("#")) return this.attributes.get("id") === selector.slice(1);
    const match = selector.match(/^([a-z]+)?(?:\[([^=\]]+)(?:=([^\]]+))?\])?$/);
    return Boolean(match && (!match[1] || this.tagName === match[1]) && (!match[2] || (this.hasAttribute(match[2]) && (!match[3] || this.getAttribute(match[2]) === match[3]))));
  }
  closest(selector) { return this.matches(selector) ? this : this.parent?.closest(selector) || null; }
  querySelectorAll(selector) {
    const selectors = selector.split(",").map(part => part.trim());
    return this.children.flatMap(child => [...(selectors.some(part => child.matches(part)) ? [child] : []), ...child.querySelectorAll(selector)]);
  }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  focus() { this.focused = true; }
  select() { this.selected = true; }
  showModal() { this.open = true; }
  close() { this.open = false; return this.emit("close"); }
}
class Form extends Element {
  constructor() { super("form", { action: "/account/api/keys" }); this.entries = [["csrf_token", "fixture-csrf"]]; }
}

function accountPanel({ withSecret = false } = {}) {
  const panel = new Element("section", { "data-agent-access-panel": "" });
  const close = new Element("button", { "data-close-agent": "" });
  const status = new Element("p", { "data-api-status": "", "data-copy-success": "Copied", "data-copy-failed": "Copy failed", "data-request-failed": "Request failed" });
  const form = new Form();
  panel.append(close, form, status);
  const sensitive = [];
  if (withSecret) {
    const section = new Element("div", { "data-new-secret": "" });
    const secret = new Element("input", { id: "api-new-sk", type: "password", value: fixtureSecret, "data-secret": "" });
    const command = new Element("textarea", { id: "api-install-command", "data-secret": "" }, `install ${fixtureSecret}`);
    const prompt = new Element("textarea", { id: "api-agent-prompt", "data-secret": "" }, `Use this command: ${fixtureSecret}`);
    const reveal = new Element("button", { "data-reveal-secret": "", "aria-pressed": "false" });
    const copy = new Element("button", { "data-copy-target": "api-install-command" });
    section.append(secret, command, prompt, reveal, copy);
    panel.append(section);
    sensitive.push(secret, command, prompt);
  }
  return { panel, close, form, status, sensitive };
}

function deferred() {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return { promise, resolve };
}

function browser({ standalone, clipboardFailure = false } = {}) {
  const document = new Element("document");
  Object.defineProperty(document, "isConnected", { value: true });
  document.body = new Element("body"); document.append(document.body);
  if (standalone) document.body.append(standalone.panel);
  document.createElement = tag => new Element(tag);
  document.importNode = node => node;
  const window = new Events();
  const requests = [];
  const replies = [];
  const parsed = new Map();
  const clipboard = [];
  const navigations = [];
  const link = new Element("a", { "data-agent-access": "", href: "/account/api" });
  document.body.append(link);
  let nextResponse = 0;
  function response(panel, url = `${origin}/account/api`) {
    const markup = `fixture-response-${nextResponse++}`;
    parsed.set(markup, panel);
    return { url, text: async () => markup };
  }
  class Parser { parseFromString(markup) { return { querySelector: () => parsed.get(markup) }; } }
  class FormDataFixture { constructor(form) { this.form = form; } [Symbol.iterator]() { return this.form.entries[Symbol.iterator](); } }
  vm.runInNewContext(script, {
    document, window, URL, URLSearchParams, AbortController, CSS: { escape: value => value },
    HTMLFormElement: Form, FormData: FormDataFixture, DOMParser: Parser,
    location: { href: `${origin}/`, origin, assign: url => navigations.push(url) },
    navigator: { clipboard: { async writeText(value) { if (clipboardFailure) throw new Error("clipboard denied"); clipboard.push(value); } } },
    fetch: async (url, options) => { requests.push({ url, options }); assert.ok(replies.length, "unexpected request"); return replies.shift(); },
  });
  const click = (target, properties = {}) => document.emit("click", { target, button: 0, ...properties });
  return {
    document, window, link, requests, clipboard, navigations, click, response,
    reply(panel) { replies.push(response(panel)); },
    queue(value) { replies.push(value); },
    submit(form) { return document.emit("submit", { target: form }); },
    async open(panel) { this.reply(panel); await click(link).done; return document.querySelector("dialog"); },
  };
}

function assertErased(fields) {
  assert.equal(fields.length, 3, "fixture must exercise SK input, command and agent prompt");
  for (const field of fields) {
    assert.equal(field.value, "", "live form value retained secret");
    assert.equal(field.defaultValue, "", "default form value retained secret");
    assert.equal(field.textContent, "", "child text retained secret");
    assert.equal(field.getAttribute("value"), null, "value attribute retained secret");
    assert.equal(field.isConnected, false, "one-time credential section remained on page");
  }
}

test("copy and reveal work on one page; modal close erases every credential representation", async () => {
  const view = accountPanel({ withSecret: true });
  const page = browser();
  const modal = await page.open(view.panel);
  assert.equal(modal.open, true);
  assert.equal(page.requests[0].options.cache, "no-store");
  assert.equal(page.requests[0].options.credentials, "same-origin");
  assert.equal(view.sensitive[0].defaultValue, fixtureSecret);
  await page.click(view.panel.querySelector("[data-reveal-secret]")).done;
  assert.equal(view.sensitive[0].type, "text");
  assert.equal(view.panel.querySelector("[data-reveal-secret]").getAttribute("aria-pressed"), "true");
  await page.click(view.panel.querySelector("[data-copy-target]")).done;
  assert.deepEqual(page.clipboard, [`install ${fixtureSecret}`]);
  assert.equal(view.status.textContent, "Copied");
  await page.click(view.close).done;
  assertErased(view.sensitive);
  assert.equal(modal.open, false);
  assert.equal(modal.children.length, 0);
  assert.equal(page.link.focused, true);
});

test("pagehide scrubs standalone SK inputs and textareas including default values", async () => {
  const view = accountPanel({ withSecret: true });
  const page = browser({ standalone: view });
  await page.window.emit("pagehide").done;
  assertErased(view.sensitive);
  assert.equal(view.panel.isConnected, true, "non-sensitive page must remain usable for restoration");
});

test("a pending creation blocks close, Escape and duplicate submit until its one-time result arrives", async () => {
  const before = accountPanel();
  const after = accountPanel({ withSecret: true });
  const page = browser();
  const modal = await page.open(before.panel);
  const network = deferred(); page.queue(network.promise);
  const creation = page.submit(before.form);
  assert.equal(creation.event.defaultPrevented, true);
  await page.click(before.close).done;
  assert.equal(modal.open, true, "close discarded pending one-time secret");
  const escape = modal.emit("cancel"); await escape.done;
  assert.equal(escape.event.defaultPrevented, true, "Escape discarded pending one-time secret");
  const duplicate = page.submit(before.form); await duplicate.done;
  assert.equal(duplicate.event.defaultPrevented, true);
  assert.equal(page.requests.length, 2, "duplicate submit sent a second creation request");
  network.resolve(page.response(after.panel)); await creation.done;
  assert.equal(after.panel.isConnected, true);
  assert.equal(after.sensitive[1].focused, true, "new install command did not receive focus");
  assert.equal(modal.emit("cancel").event.defaultPrevented, false);
  await page.click(after.close).done;
  assertErased(after.sensitive);
});

test("leaving during creation aborts the fetch and does not attach a late secret response", async () => {
  const before = accountPanel({ withSecret: true });
  const after = accountPanel({ withSecret: true });
  const page = browser({ standalone: before });
  const network = deferred(); page.queue(network.promise);
  const creation = page.submit(before.form);
  await page.window.emit("pagehide").done;
  assertErased(before.sensitive);
  assert.equal(page.requests[0].options.signal.aborted, true);
  network.resolve(page.response(after.panel)); await creation.done;
  assert.equal(after.panel.isConnected, false);
  assert.equal(page.document.querySelectorAll("[data-secret]").length, 0);
});

test("successful form replacement erases old one-time credentials before detaching them", async () => {
  const before = accountPanel({ withSecret: true });
  const after = accountPanel();
  const page = browser({ standalone: before }); page.reply(after.panel);
  await page.submit(before.form).done;
  assertErased(before.sensitive);
  assert.equal(after.panel.isConnected, true);
  assert.equal(page.requests[0].options.method, "POST");
  assert.equal(String(page.requests[0].options.body), "csrf_token=fixture-csrf");
});

test("clipboard denial selects the existing command without discarding the only copy", async () => {
  const view = accountPanel({ withSecret: true });
  const page = browser({ standalone: view, clipboardFailure: true });
  await page.click(view.panel.querySelector("[data-copy-target]")).done;
  assert.equal(view.sensitive[1].focused, true);
  assert.equal(view.sensitive[1].selected, true);
  assert.equal(view.sensitive[1].value, `install ${fixtureSecret}`);
  assert.equal(view.status.textContent, "Copy failed");
});

test("modified link clicks retain native browser navigation", async () => {
  const page = browser();
  for (const modifiers of [{ ctrlKey: true }, { metaKey: true }, { shiftKey: true }, { button: 1 }]) {
    const click = page.click(page.link, modifiers); await click.done;
    assert.equal(click.event.defaultPrevented, false);
  }
  assert.equal(page.requests.length, 0);
});
