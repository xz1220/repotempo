import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";

const script = await readFile(new URL("../internal/web/static/reading-state.js", import.meta.url), "utf8");
const key = (id) => `repotempo:reading:v1:${id}`;

class Storage {
  values = new Map();
  writes = [];
  failGet = false;
  failSet = false;
  failRemove = false;
  getItem(name) {
    if (this.failGet) throw new Error("access denied");
    return this.values.get(name) ?? null;
  }
  setItem(name, value) {
    if (this.failSet) throw new Error("quota exceeded");
    this.writes.push(["set", name]);
    this.values.set(name, value);
  }
  removeItem(name) {
    if (this.failRemove) throw new Error("storage blocked");
    this.writes.push(["remove", name]);
    this.values.delete(name);
  }
}

class Element extends EventTarget {
  dataset = {};
  hidden = true;
  textContent = "";
  attributes = new Map();
  classes = new Set();
  classList = { toggle: (name, enabled) => enabled ? this.classes.add(name) : this.classes.delete(name) };
  setAttribute(name, value) { this.attributes.set(name, value); }
  click() { this.dispatchEvent(new Event("click")); }
}

function card(id) {
  const value = new Element();
  value.dataset.readingRepository = id;
  value.button = new Element();
  value.button.dataset = {
    readingMarkRead: "标记已读", readingMarkUnread: "已读 · 标记未读",
    readingLoadError: "无法读取状态。", readingSaveError: "未保存，请重试。",
  };
  value.note = new Element();
  value.querySelector = (selector) => ({
    "[data-reading-toggle]": value.button, "[data-reading-note]": value.note,
  })[selector];
  return value;
}

function browser(ids, storage = new Storage(), options = {}) {
  const cards = ids.map(card);
  const window = new EventTarget();
  Object.defineProperty(window, "localStorage", { get() {
    if (options.denied) throw new Error("SecurityError");
    return storage;
  } });
  const help = new Element();
  help.textContent = "仅保存在当前浏览器和站点，不跨设备同步";
  const document = { body: { dataset: { readingEnabled: options.readingEnabled } }, querySelectorAll: () => cards, querySelector: () => help };
  vm.runInNewContext(script, { window, document });
  return { cards, window, storage, help };
}

function storageEvent(window, storage, name, newValue = null) {
  const event = new Event("storage");
  Object.defineProperties(event, {
    key: { value: name }, storageArea: { value: storage }, newValue: { value: newValue },
  });
  window.dispatchEvent(event);
}

function assertRead(card, read) {
  assert.equal(card.classes.has("is-read"), read);
  assert.equal(card.dataset.readingState, read ? "read" : "unread");
  assert.equal(card.button.attributes.get("aria-pressed"), String(read));
  assert.equal(card.button.textContent, read ? "已读 · 标记未读" : "标记已读");
}

test("OAuth-anonymous pages neither read nor mutate browser-local reading marks", () => {
  const storage = new Storage();
  storage.values.set(key("101"), "read");
  const page = browser(["101"], storage, { readingEnabled: "false", denied: true });
  assert.equal(page.cards[0].button.hidden, true);
  assert.equal(page.help.hidden, true);
  assert.equal(page.cards[0].dataset.readingState, undefined);
  page.cards[0].button.click();
  assert.equal(storage.writes.length, 0);
  assert.equal(storage.values.get(key("101")), "read");
});

test("signed-in pages keep their existing browser-local reading marks", () => {
  const storage = new Storage();
  storage.values.set(key("101"), "read");
  const page = browser(["101"], storage, { readingEnabled: "true" });
  assertRead(page.cards[0], true);
  page.cards[0].button.click();
  assertRead(page.cards[0], false);
});

test("only an explicit button click marks a project, and the action is reversible", () => {
  const page = browser(["101", "102"]);
  const [first, second] = page.cards;
  assertRead(first, false);
  assert.equal(first.button.hidden, false);
  assert.equal(first.note.hidden, true);
  assert.equal(first.note.textContent, "");
  assert.equal(page.help.hidden, false);
  assert.match(page.help.textContent, /当前浏览器和站点，不跨设备同步/);
  first.click();
  page.window.dispatchEvent(new Event("scroll"));
  assert.equal(page.storage.writes.length, 0);
  first.button.click();
  assertRead(first, true);
  assertRead(second, false);
  assert.equal(page.storage.getItem(key("101")), "read");
  first.button.click();
  assertRead(first, false);
  assert.equal(page.storage.getItem(key("101")), null);
});

test("marks persist across reloads and pagination without dropping other project keys", () => {
  const storage = new Storage();
  const firstPage = browser(["101"], storage);
  firstPage.cards[0].button.click();
  const nextPage = browser(["102"], storage);
  nextPage.cards[0].button.click();
  const reloaded = browser(["101", "102", "103"], storage);
  assertRead(reloaded.cards[0], true);
  assertRead(reloaded.cards[1], true);
  assertRead(reloaded.cards[2], false);
  assert.equal(storage.values.size, 2);
});

test("same-origin storage events refresh marks, removals, and clear from the latest stored value", () => {
  const page = browser(["101", "102"]);
  page.storage.values.set(key("101"), "read");
  storageEvent(page.window, page.storage, key("101"), "outdated-event-value");
  assertRead(page.cards[0], true);
  assertRead(page.cards[1], false);
  page.storage.values.delete(key("101"));
  storageEvent(page.window, page.storage, key("101"), "read");
  assertRead(page.cards[0], false);
  page.cards[1].button.click();
  page.storage.values.clear();
  storageEvent(page.window, page.storage, null);
  assertRead(page.cards[1], false);
});

test("unrelated and sessionStorage events do not change this browser's local marks", () => {
  const page = browser(["101"]);
  page.storage.values.set(key("101"), "read");
  storageEvent(page.window, page.storage, "other-app:101");
  assertRead(page.cards[0], false);
  storageEvent(page.window, new Storage(), key("101"));
  assertRead(page.cards[0], false);
  page.window.dispatchEvent(new Event("pageshow"));
  assertRead(page.cards[0], true);
});

test("all visible copies of the same repository agree and long numeric IDs stay distinct", () => {
  const page = browser(["9007199254740992", "9007199254740993", "9007199254740992"]);
  page.cards[0].button.click();
  assertRead(page.cards[0], true);
  assertRead(page.cards[1], false);
  assertRead(page.cards[2], true);
  page.cards[1].button.click();
  assert.equal(page.storage.values.size, 2);
});

test("a concurrent tab write cannot invert the explicit action shown on the button", () => {
  const page = browser(["101"]);
  // The other tab has written a mark, but its storage event is still queued.
  page.storage.values.set(key("101"), "read");
  page.cards[0].button.click();
  assertRead(page.cards[0], true);
  assert.equal(page.storage.getItem(key("101")), "read");
});

test("blocked storage access is contained and clicking reports not saved", () => {
  const page = browser(["101"], new Storage(), { denied: true });
  assert.match(page.cards[0].note.textContent, /无法读取/);
  assert.doesNotThrow(() => page.cards[0].button.click());
  assert.match(page.cards[0].note.textContent, /未保存/);
  assert.equal(page.cards[0].note.hidden, false);
  assert.equal(page.cards[0].classes.has("is-read"), false);
  assert.equal(page.storage.writes.length, 0);
  assert.doesNotThrow(() => page.window.dispatchEvent(new Event("pageshow")));
  assert.doesNotThrow(() => storageEvent(page.window, page.storage, key("101")));
});

test("read failures can recover without reloading the page", () => {
  const storage = new Storage();
  storage.failGet = true;
  const page = browser(["101"], storage);
  assert.match(page.cards[0].note.textContent, /无法读取/);
  storage.failGet = false;
  page.cards[0].button.click();
  assertRead(page.cards[0], true);
  assert.equal(page.cards[0].note.dataset.readingError, undefined);
  assert.equal(page.cards[0].note.hidden, true);
  assert.equal(page.cards[0].note.textContent, "");
});

test("failed writes and removals keep the last saved state and allow retry", () => {
  const page = browser(["101"]);
  page.storage.failSet = true;
  page.cards[0].button.click();
  assertRead(page.cards[0], false);
  assert.match(page.cards[0].note.textContent, /未保存/);
  page.storage.failSet = false;
  page.cards[0].button.click();
  assertRead(page.cards[0], true);
  page.storage.failRemove = true;
  page.cards[0].button.click();
  assertRead(page.cards[0], true);
  assert.match(page.cards[0].note.textContent, /未保存/);
  page.storage.failRemove = false;
  page.cards[0].button.click();
  assertRead(page.cards[0], false);
  assert.equal(page.cards[0].note.dataset.readingError, undefined);
});

test("malformed stored values never pretend a repository was read", () => {
  const storage = new Storage();
  storage.values.set(key("101"), '{"read":true}');
  const page = browser(["101", "../102", "0"], storage);
  assertRead(page.cards[0], false);
  assert.equal(storage.writes.length, 0);
  assert.equal(page.cards[1].button.hidden, true);
  assert.equal(page.cards[2].button.hidden, true);
});
