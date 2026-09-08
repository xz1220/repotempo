import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";

const script = await readFile(new URL("../internal/web/static/imports.js", import.meta.url), "utf8");
const origin = "https://repotempo.example";
const statusPath = "/watch/imports/abcdefgh12345678/status?lang=zh-CN";
const status = (stage = "reading", terminal = false, overrides = {}) => ({ stage, terminal, label: "读取 README", message: "可稍后查看进度", ...overrides });

class Events {
  listeners = new Map();
  addEventListener(type, listener) {
    this.listeners.set(type, [...(this.listeners.get(type) || []), listener]);
  }
  emit(type, properties = {}) {
    for (const listener of this.listeners.get(type) || []) listener({ type, ...properties });
  }
}

const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };

function browser(options = {}) {
  const document = Object.assign(new Events(), { hidden: options.hidden ?? false });
  const window = new Events();
  let reloads = 0, htmlWrites = 0, clock = 0, sequence = 0;
  window.location = { href: `${origin}/watch/imports/abcdefgh12345678`, origin, reload: () => reloads++ };
  const stage = { textContent: "等待处理" };
  const message = { textContent: "任务仍在后台处理" };
  for (const node of [stage, message]) Object.defineProperty(node, "innerHTML", { set() { htmlWrites++; } });
  const panel = {
    dataset: { terminal: options.terminal ? "true" : "false", pollUrl: options.url ?? statusPath, pausedLabel: "自动刷新已暂停，可手动刷新。任务仍在后台处理。" },
    querySelector: selector => ({ "[data-import-stage]": stage, "[data-import-message]": message })[selector] || null,
  };
  document.querySelector = selector => selector === "[data-import-status]" && options.panel !== false ? panel : null;
  const timers = new Map();
  const setTimeout = (callback, delay = 0) => { const id = ++sequence; timers.set(id, { callback, at: clock + delay, delay }); return id; };
  const clearTimeout = id => timers.delete(id);
  const requests = [];
  const replies = [...(options.replies || [])];
  const fetch = (url, requestOptions) => {
    requests.push({ url, options: requestOptions });
    if (options.fetch) return options.fetch(url, requestOptions);
    const reply = replies.length ? replies.shift() : status();
    if (reply instanceof Error) return Promise.reject(reply);
    return Promise.resolve({ ok: reply?.ok !== false, json: async () => reply });
  };
  vm.runInNewContext(script, { document, window, URL, AbortController, fetch, setTimeout, clearTimeout });
  return {
    document, window, panel, stage, message, requests, timers,
    get reloads() { return reloads; },
    get htmlWrites() { return htmlWrites; },
    nextDelay() { return timers.size ? Math.min(...[...timers.values()].map(timer => timer.at - clock)) : null; },
    async tick(milliseconds) {
      const target = clock + milliseconds;
      for (let steps = 0; steps < 1000; steps++) {
        const next = [...timers.entries()].sort((a, b) => a[1].at - b[1].at || a[0] - b[0])[0];
        if (!next || next[1].at > target) { clock = target; await flush(); return; }
        clock = next[1].at;
        timers.delete(next[0]);
        // Do not await a pending request: pagehide/visibility events can arrive
        // while fetch is in flight, as they do in a real browser.
        next[1].callback();
        await flush();
      }
      throw new Error("runaway timer loop");
    },
  };
}

test("import status polls every three seconds with same-origin, uncached JSON requests", async () => {
  const page = browser();
  assert.equal(page.requests.length, 0);
  assert.equal(page.nextDelay(), 3000);
  await page.tick(2999);
  assert.equal(page.requests.length, 0);
  await page.tick(1);
  assert.equal(page.requests.length, 1);
  const request = page.requests[0];
  assert.equal(request.url, origin + statusPath);
  assert.equal(request.options.credentials, "same-origin");
  assert.equal(request.options.cache, "no-store");
  assert.equal(request.options.headers.Accept, "application/json");
  assert.ok(request.options.signal instanceof AbortSignal);
  assert.equal(page.stage.textContent, "读取 README");
  assert.equal(page.nextDelay(), 3000);
  await page.tick(3000);
  assert.equal(page.requests.length, 2);
});

test("done, partial, and failed statuses reload once so server-rendered actions become available", async () => {
  for (const state of ["done", "partial", "failed"]) {
    const page = browser({ replies: [status(state, true)] });
    await page.tick(3000);
    assert.equal(page.reloads, 1, state);
    assert.equal(page.nextDelay(), null, state);
    await page.tick(30000);
    assert.equal(page.requests.length, 1, state);
    assert.equal(page.reloads, 1, state);
  }
});

test("three consecutive failures pause polling honestly and retain the existing stage", async () => {
  const page = browser({ replies: [new Error("network"), { ok: false }, { stage: "not-a-stage", terminal: false, label: "bad", message: "bad" }] });
  await page.tick(3000);
  assert.equal(page.nextDelay(), 6000);
  await page.tick(6000);
  assert.equal(page.nextDelay(), 6000);
  await page.tick(6000);
  assert.equal(page.nextDelay(), null);
  assert.equal(page.message.textContent, page.panel.dataset.pausedLabel);
  assert.equal(page.stage.textContent, "等待处理");
  assert.equal(page.reloads, 0);
  await page.tick(60000);
  assert.equal(page.requests.length, 3);
});

test("a successful poll resets the consecutive-failure count and normal interval", async () => {
  const page = browser({ replies: [new Error("temporary"), status(), new Error("temporary"), new Error("temporary"), status()] });
  for (const delay of [3000, 6000, 3000, 6000, 6000]) await page.tick(delay);
  assert.equal(page.requests.length, 5);
  assert.equal(page.nextDelay(), 3000);
  assert.notEqual(page.message.textContent, page.panel.dataset.pausedLabel);
});

test("hidden pages do not poll and visibility restoration resumes immediately", async () => {
  const page = browser({ hidden: true });
  assert.equal(page.nextDelay(), null);
  await page.tick(30000);
  assert.equal(page.requests.length, 0);
  page.document.hidden = false;
  page.document.emit("visibilitychange");
  assert.equal(page.nextDelay(), 0);
  await page.tick(0);
  assert.equal(page.requests.length, 1);
  page.document.hidden = true;
  page.document.emit("visibilitychange");
  assert.equal(page.nextDelay(), null);
  await page.tick(30000);
  assert.equal(page.requests.length, 1);
});

test("foreign, malformed, and non-status URLs never trigger a request", async () => {
  for (const url of [
    `https://evil.example${statusPath}`, `//evil.example${statusPath}`,
    "javascript:alert(1)", "http://[invalid", "/repositories", "/watch/imports/short/status",
    "/watch/imports/abcdefgh12345678/status/other", "/watch/imports/../../status",
  ]) {
    const page = browser({ url });
    await page.tick(60000);
    assert.equal(page.requests.length, 0, url);
    assert.equal(page.nextDelay(), null, url);
  }
  for (const options of [{ panel: false }, { terminal: true }]) {
    const page = browser(options);
    await page.tick(60000);
    assert.equal(page.requests.length, 0);
  }
});

test("API strings use textContent rather than interpreting markup", async () => {
  const label = '<img src=x onerror="alert(1)">';
  const message = '<script>globalThis.compromised = true</script> 中文';
  const page = browser({ replies: [status("reading", false, { label, message })] });
  await page.tick(3000);
  assert.equal(page.stage.textContent, label);
  assert.equal(page.message.textContent, message);
  assert.equal(page.htmlWrites, 0);
});

test("pagehide cancels scheduled polling and bfcache pageshow resumes it", async () => {
  const page = browser();
  page.window.emit("pagehide");
  await page.tick(30000);
  assert.equal(page.requests.length, 0);
  page.window.emit("pageshow", { persisted: true });
  assert.equal(page.nextDelay(), 0);
  await page.tick(0);
  assert.equal(page.requests.length, 1);
  assert.equal(page.nextDelay(), 3000);
});

test("a request completing after pagehide must not reload or overwrite the leaving page", async () => {
  let resolve;
  const page = browser({ fetch: () => new Promise(done => { resolve = done; }) });
  await page.tick(3000);
  assert.equal(page.requests.length, 1);
  page.window.emit("pagehide");
  resolve({ ok: true, json: async () => status("done", true) });
  await flush();
  assert.equal(page.reloads, 0);
  assert.equal(page.stage.textContent, "等待处理");
  assert.equal(page.nextDelay(), null);
});
