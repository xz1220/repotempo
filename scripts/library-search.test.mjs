import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";

const script = await readFile(new URL("../internal/web/static/app.js", import.meta.url), "utf8");

function page(searchValue, tagValue = "") {
  class Element extends EventTarget {
    value = "";
    dataset = {};
    attributes = new Map();
    querySelector() { return null; }
    querySelectorAll() { return []; }
    setAttribute(name, value) { this.attributes.set(name, String(value)); }
    getAttribute(name) { return this.attributes.get(name); }
  }
  const search = new Element();
  search.value = searchValue;
  search.name = "q";
  const tag = new Element();
  tag.value = tagValue;
  const form = new Element();
  const period = new Element();
  period.value = "1d";
  const sort = new Element();
  sort.value = "stars";
  form.querySelector = (selector) => ({
    "[data-library-search]": search,
    'input[type="hidden"][name="tag"]': tagValue ? tag : null,
    'select[name="period"]': period,
    'select[name="sort"]': sort,
  })[selector] ?? null;
  form.querySelectorAll = () => [{ value: "ai" }, { value: "python" }];
  search.form = form;
  const count = new Element();
  const toggle = new Element();
  toggle.querySelector = () => count;
  const document = new EventTarget();
  document.documentElement = { classList: { add() {} } };
  document.querySelector = (selector) => ({
    "[data-library-search]": search,
    "[data-filter-toggle]": toggle,
    "[data-filter-panel]": form,
  })[selector] ?? null;
  document.querySelectorAll = () => [];
  vm.runInNewContext(script, { document, window: new EventTarget(), HTMLInputElement: Element, HTMLSelectElement: Element });
  const submit = () => {
    form.dispatchEvent(new Event("submit"));
    const values = new URLSearchParams();
    values.append(search.name, search.value);
    if (tagValue) values.append("tag", tag.value);
    return values;
  };
  return { search, form, count, submit };
}

test("a combined search and tag counts both and survives a filter submission", () => {
  const p = page("audio.cpp", "ai");
  assert.equal(p.count.textContent, "2");
  assert.equal(p.submit().toString(), "q=audio.cpp&tag=ai");
});

test("editing within a tag keeps a separate search even when it matches another tag", () => {
  const p = page("audio.cpp", "ai");
  p.search.value = "python";
  p.search.dispatchEvent(new Event("input"));
  assert.equal(p.submit().toString(), "q=python&tag=ai");
});

test("a query loaded from its URL keeps query semantics when another filter changes", () => {
  const p = page("python");
  assert.equal(p.submit().toString(), "q=python");
});

test("an edited exact tag still selects a tag when no tag is active", () => {
  const p = page("");
  p.search.value = "python";
  p.search.dispatchEvent(new Event("input"));
  assert.equal(p.submit().toString(), "tag=python");
});
