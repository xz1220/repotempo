import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";

const script = await readFile(new URL("../internal/web/static/reading-position.js", import.meta.url), "utf8");
const storageKey = "repotempo:reading-position:v1";
const origin = "https://repotempo.example";
const now = Date.UTC(2026, 8, 8, 9);
const projectID = "9007199254740993";
const listURL = "/repositories?view=focus&sort=velocity&period=7d&date=2026-09-08&cursor=2az&tag=skills&topic=coding-agents&source=github_search&status=active&q=中文+Agent&focus=1&new=0&lang=zh-CN";
const targetURL = `${listURL}#project-${projectID}`;

function canonical(raw) {
  const url = new URL(raw, origin);
  url.searchParams.sort();
  return url.pathname + url.search + url.hash;
}

function detailURL(id = projectID, target = targetURL) {
  return `/repositories/${id}?date=2026-09-08&lang=zh-CN&return_to=${encodeURIComponent(target)}`;
}

class Storage {
  values = new Map();
  writes = [];
  failGet = false;
  failSet = false;
  getItem(key) {
    if (this.failGet) throw new Error("storage access denied");
    return this.values.get(key) ?? null;
  }
  setItem(key, value) {
    if (this.failSet) throw new Error("storage quota exceeded");
    this.values.set(key, value);
    this.writes.push([key, value]);
  }
  state() { return JSON.parse(this.values.get(storageKey) || "{}"); }
}

// Synchronous event delivery makes listener errors fail the test, rather than
// becoming uncaught exceptions on a later Node tick.
class Events {
  listeners = new Map();
  addEventListener(type, callback, options = {}) {
    const entries = this.listeners.get(type) || [];
    entries.push({ callback, once: options.once });
    this.listeners.set(type, entries);
  }
  removeEventListener(type, callback) {
    this.listeners.set(type, (this.listeners.get(type) || []).filter(entry => entry.callback !== callback));
  }
  emit(type, properties = {}) {
    const event = { type, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; }, ...properties };
    for (const entry of [...(this.listeners.get(type) || [])]) {
      if (entry.once) this.removeEventListener(type, entry.callback);
      entry.callback(event);
    }
    return event;
  }
}

function browser(options = {}) {
  const storage = options.storage || new Storage();
  const window = new Events();
  window.location = new URL(options.href || listURL, origin);
  window.scrollY = options.scrollY ?? 0;
  window.performance = { getEntriesByType: () => [{ type: options.navigationType || "navigate" }] };
  Object.defineProperty(window, "sessionStorage", { get() {
    if (options.denied) throw new Error("SecurityError");
    return storage;
  } });
  const scrolls = [];
  const replacements = [];
  window.scrollTo = value => { scrolls.push({ ...value }); window.scrollY = value.top; };
  window.history = {
    state: { existingState: "preserve-me" },
    scrollRestoration: "auto",
    replaceState(state, title, url) {
      if (options.historyDenied) throw new Error("history unavailable");
      replacements.push({ state, title, url });
      window.location = new URL(url, window.location.href);
    },
  };
  const frames = [];
  window.requestAnimationFrame = callback => { frames.push(callback); return frames.length; };
  const details = (options.tagIDs || ["101", projectID, "303"]).map(id => ({
    open: (options.expanded || []).includes(id),
    closest: selector => selector === "[data-repository-id]" ? { dataset: { repositoryId: id } } : null,
  }));
  const anchors = new Map((options.anchorIDs || [projectID]).map(id => [
    `project-${id}`,
    { getBoundingClientRect: () => ({ top: (options.documentTop?.(id, details) ?? 4000) - window.scrollY }) },
  ]));
  const links = (options.detailURLs || [detailURL()]).map(href => Object.assign(new Events(), { href }));
  const returns = (options.returnURLs || []).map(href => Object.assign(new Events(), { href }));
  const list = options.list === false ? null : { dataset: { listUrl: options.listURL || listURL } };
  const document = {
    querySelector: selector => selector === "[data-list-url]" ? list : null,
    querySelectorAll: selector => ({
      "[data-repository-detail]": links,
      "[data-library-return]": returns,
      ".project-more-tags": details,
      ".project-more-tags[open]": details.filter(detail => detail.open),
    })[selector] || [],
    getElementById: id => anchors.get(id) || null,
  };
  class ClockDate extends Date { static now() { return now; } }
  vm.runInNewContext(script, { window, document, URL, Date: ClockDate });
  return {
    window, storage, scrolls, replacements, frames, anchors, details, links, returns,
    show(properties) { window.emit("pageshow", properties); },
    frame() { const callbacks = frames.splice(0); callbacks.forEach(callback => callback()); },
  };
}

function savedPosition(options = {}) {
  const page = browser({ scrollY: 4120, expanded: ["101", projectID], ...options });
  assert.equal(page.links[0].emit("click").defaultPrevented, false);
  return page.storage;
}

function markReturn(storage, index = 0, event = "click") {
  const page = browser({ list: false, href: detailURL(), storage, returnURLs: [targetURL, targetURL] });
  assert.equal(page.returns[index].emit(event).defaultPrevented, false);
  return page;
}

function readyReturn(options = {}) {
  const storage = options.storage || savedPosition();
  markReturn(storage);
  return browser({ href: targetURL, storage, ...options });
}

test("detail clicks save the effective canonical scope, exact long ID, negative offset and every expanded card", () => {
  const page = browser({
    // Incoming daily URLs may be shorter than the resolved server list scope.
    href: "/repositories?lang=zh-CN", scrollY: 4120, expanded: ["101", projectID],
  });
  page.links[0].emit("click");
  const state = page.storage.state();
  assert.deepEqual(state.records, [{
    key: canonical(targetURL), anchor: `project-${projectID}`, offset: -120,
    expanded: ["101", projectID], at: now,
  }]);
  assert.equal(state.pending, null);
  const query = new URL(state.records[0].key, origin).searchParams;
  for (const [key, value] of new URL(listURL, origin).searchParams) assert.equal(query.get(key), value, key);
  assert.equal(page.scrolls.length, 0);
});

test("positive viewport offsets and reordered query parameters save under the same canonical key", () => {
  const page = browser({ scrollY: 3900, detailURLs: [detailURL(projectID, canonical(targetURL))] });
  page.links[0].emit("auxclick", { button: 1 });
  assert.equal(page.storage.state().records[0].key, canonical(targetURL));
  assert.equal(page.storage.state().records[0].offset, 100);
});

for (const [label, index, event] of [["breadcrumb", 0, "click"], ["sidebar", 1, "auxclick"]]) {
  test(`${label} return marks pending and restores only after two animation frames`, () => {
    const storage = savedPosition();
    markReturn(storage, index, event);
    assert.deepEqual(storage.state().pending, { key: canonical(targetURL), at: now });
    const page = browser({
      href: targetURL, storage, scrollY: 200,
      expanded: ["303"],
      // Expanding an earlier card changes the target's actual document top.
      documentTop: (_id, details) => 4000 + (details[0].open ? 280 : 0),
    });
    page.show();
    assert.equal(storage.state().pending, null);
    assert.equal(page.scrolls.length, 0);
    page.frame();
    assert.equal(page.scrolls.length, 0);
    page.frame();
    assert.deepEqual(page.details.map(detail => detail.open), [true, true, false]);
    assert.deepEqual(page.scrolls, [{ top: 4400, left: 0, behavior: "instant" }]);
    assert.equal(page.window.location.hash, "");
    assert.equal(page.replacements[0].url, new URL(listURL, origin).pathname + new URL(listURL, origin).search);
    assert.equal(page.replacements[0].state, page.window.history.state);
    assert.equal(page.window.history.scrollRestoration, "auto");
  });
}

test("pending is consumed once even when pageshow repeats or another document opens the same fragment", () => {
  const page = readyReturn();
  page.show();
  page.show();
  page.frame();
  page.frame();
  assert.equal(page.scrolls.length, 1);
  const reopened = browser({ href: targetURL, storage: page.storage });
  reopened.show();
  reopened.frame();
  reopened.frame();
  assert.equal(reopened.scrolls.length, 0);
  assert.equal(page.storage.state().records.length, 1);
});

test("ordinary pagination, changed filters, and no-fragment list visits do not restore old positions", () => {
  for (const change of [
    { href: listURL },
    { href: targetURL.replace("cursor=2az", "cursor=2b0"), listURL: listURL.replace("cursor=2az", "cursor=2b0") },
    { href: targetURL.replace("tag=skills", "tag=investment"), listURL: listURL.replace("tag=skills", "tag=investment") },
  ]) {
    const page = readyReturn(change);
    page.show();
    page.frame();
    page.frame();
    assert.equal(page.scrolls.length, 0);
    assert.equal(page.replacements.length, 0);
  }
  const unmarked = browser({ href: targetURL, storage: savedPosition() });
  unmarked.show();
  assert.equal(unmarked.frames.length, 0, "a bookmarked fragment alone is not an explicit return");
});

for (const navigationType of ["back_forward", "reload", "bfcache"]) {
  test(`${navigationType} leaves native scroll and history state alone`, () => {
    const page = readyReturn({ navigationType });
    const before = page.storage.state();
    page.show({ persisted: navigationType === "bfcache" });
    page.frame();
    page.frame();
    assert.equal(page.scrolls.length, 0);
    assert.equal(page.replacements.length, 0);
    assert.equal(page.window.history.scrollRestoration, "auto");
    assert.deepEqual(page.storage.state(), before);
  });
}

test("denied storage access and failed reads or writes never break detail and return navigation", () => {
  for (const failure of ["denied", "failGet", "failSet"]) {
    const storage = new Storage();
    if (failure !== "denied") storage[failure] = true;
    const options = { storage, denied: failure === "denied" };
    const list = browser(options);
    assert.doesNotThrow(() => list.links[0].emit("click"));
    const detail = browser({ ...options, list: false, href: detailURL(), returnURLs: [targetURL] });
    assert.doesNotThrow(() => detail.returns[0].emit("click"));
    const returned = browser({ ...options, href: targetURL });
    assert.doesNotThrow(() => { returned.show(); returned.frame(); returned.frame(); });
    assert.equal(returned.scrolls.length, 0);
  }
});

test("malformed JSON and wrong storage schemas fall back without throwing or scrolling", () => {
  for (const value of ["{broken", "null", "[]", '"text"', '{"records":{},"pending":true}', '{"records":[null,42,{}]}']) {
    const storage = new Storage();
    storage.values.set(storageKey, value);
    const page = browser({ storage, href: targetURL });
    assert.doesNotThrow(() => { page.show(); page.frame(); page.frame(); });
    assert.equal(page.scrolls.length, 0);
    assert.doesNotThrow(() => page.links[0].emit("click"));
    assert.equal(storage.state().records.length, 1, "a later valid click can replace corrupt state");
  }
});

test("a missing target consumes pending without scrolling, including removal during layout settling", () => {
  for (const removeLater of [false, true]) {
    const page = readyReturn(removeLater ? {} : { anchorIDs: [] });
    page.show();
    if (removeLater) {
      page.frame();
      page.anchors.delete(`project-${projectID}`);
    }
    page.frame();
    page.frame();
    assert.equal(page.scrolls.length, 0);
    assert.equal(page.storage.state().pending, null);
    assert.equal(page.replacements.length, 0);
  }
});

for (const input of ["wheel", "touchstart", "pointerdown", "keydown"]) {
  test(`${input} before restoration cancels automatic scrolling and layout changes`, () => {
    const page = readyReturn();
    page.show();
    page.frame();
    page.window.emit(input);
    page.frame();
    assert.equal(page.scrolls.length, 0);
    assert.deepEqual(page.details.map(detail => detail.open), [false, false, false]);
    assert.equal(page.storage.state().pending, null);
    assert.equal(page.replacements.length, 0);
  });
}

test("records remain separate for multiple projects and tabs without a shared last-list pointer", () => {
  const second = "9007199254740992";
  const page = browser({
    scrollY: 4100, anchorIDs: [projectID, second],
    detailURLs: [detailURL(), detailURL(second, `${listURL}#project-${second}`)],
  });
  page.links[0].emit("click");
  page.window.scrollY = 3600;
  page.links[1].emit("click");
  assert.deepEqual(page.storage.state().records.map(record => [record.anchor, record.offset]), [
    [`project-${projectID}`, -100], [`project-${second}`, 400],
  ]);
  const independentTab = browser({ href: targetURL });
  independentTab.show();
  assert.equal(independentTab.scrolls.length, 0);
  assert.equal(independentTab.storage.values.size, 0);
  assert.equal(page.storage.state().records.length, 2);
});

test("external links, another list scope and invalid project anchors are never remembered", () => {
  for (const href of [
    "https://github.com/owner/repository",
    `https://other.example${detailURL()}`,
    detailURL(projectID, "https://other.example/repositories#project-101"),
    detailURL(projectID, targetURL.replace("cursor=2az", "cursor=2b0")),
    detailURL(projectID, `${listURL}#project-0`),
    detailURL(projectID, `${listURL}#other`),
    `/repositories/${projectID}`,
  ]) {
    const page = browser({ detailURLs: [href] });
    page.links[0].emit("click");
    assert.equal(page.storage.values.size, 0, href);
  }
});

test("expired and invalid records are ignored and history replacement failures are contained", () => {
  for (const modification of [
    record => { record.at = now - 8 * 60 * 60 * 1000; },
    record => { record.at = now + 1; },
    record => { record.offset = "120"; },
    record => { record.offset = 1e8; },
    record => { record.expanded = ["../101"]; },
  ]) {
    const storage = savedPosition();
    const state = storage.state();
    modification(state.records[0]);
    state.pending = { key: canonical(targetURL), at: now };
    storage.values.set(storageKey, JSON.stringify(state));
    const page = browser({ storage, href: targetURL });
    page.show();
    page.frame();
    page.frame();
    assert.equal(page.scrolls.length, 0);
  }
  const page = readyReturn({ historyDenied: true });
  page.show();
  page.frame();
  assert.doesNotThrow(() => page.frame());
  assert.equal(page.scrolls.length, 1);
  assert.equal(page.window.location.hash, `#project-${projectID}`);
});
