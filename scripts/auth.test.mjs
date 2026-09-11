import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";

const script = await readFile(new URL("../internal/web/static/auth.js", import.meta.url), "utf8");

function page(dataset) {
  const listeners = new Map();
  let reloads = 0;
  const window = {
    location: { reload: () => reloads++ },
    addEventListener(type, listener) {
      listeners.set(type, [...(listeners.get(type) || []), listener]);
    },
  };
  Object.defineProperty(window, "localStorage", { get() { throw new Error("authentication must not change local preferences"); } });
  vm.runInNewContext(script, { document: { body: { dataset } }, window });
  return {
    get reloads() { return reloads; },
    get listenerCount() { return listeners.get("pageshow")?.length || 0; },
    show(persisted) { for (const listener of listeners.get("pageshow") || []) listener({ persisted }); },
  };
}

test("OAuth pages revalidate only bfcache restores without client-side account state", () => {
  const current = page({ authEnabled: "true" });
  assert.equal(current.listenerCount, 1);
  current.show(false);
  assert.equal(current.reloads, 0, "ordinary page load must not loop");
  current.show(true);
  assert.equal(current.reloads, 1, "cached page must consult server session state again");
});

test("disabled OAuth and legacy pages do not alter browser navigation or local preferences", () => {
  for (const dataset of [{}, { authEnabled: "false" }]) {
    const current = page(dataset);
    current.show(false);
    current.show(true);
    assert.equal(current.listenerCount, 0);
    assert.equal(current.reloads, 0);
  }
});
