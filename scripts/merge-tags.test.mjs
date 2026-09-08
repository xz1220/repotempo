import test from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync, existsSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { mergeTagDocuments, normalizeTags, validRepositoryName } from "./merge-tags.mjs";
import { normalizedTopics, validName } from "./prepare-tags.mjs";

const project = (id, name, fields = {}) => ({ repository_id: id, full_name: name, ...fields });
const document = (...projects) => ({ projects });
function layer(...projects) {
  const observations = projects.map(value => ({ repository_id: value.repository_id, full_name: value.full_name, status: Object.hasOwn(value, "github_topics") ? "success" : "failed" }));
  return { document: { projects }, report: { finished: true, observations, stats: { successful: observations.filter(value => value.status === "success").length } } };
}

test("normalization retains Chinese labels and follows simple Unicode lowercase", () => {
  assert.deepEqual(normalizeTags(["　投资　", " AGENT ", "agent", "ÖKO", "中文 标签", " "]), ["投资", "agent", "öko", "中文 标签"]);
  assert.deepEqual(normalizeTags(["ΟΣ", "İ"]), ["οσ", "i"]);
  assert.deepEqual(normalizeTags([]), []);
  assert.equal(normalizeTags(Array(256).fill("SaaS")).length, 1);
});

test("invalid controls, Unicode, counts and lengths fail without truncation", () => {
  for (const value of [["agent\n"], ["\tagent"], ["\u0085agent"], ["a\u0000b"], ["\ud800"], [null], [17], null, "agent", ["中".repeat(81)], ["😀".repeat(81)], Array(257).fill("same"), Array.from({ length: 65 }, (_, i) => `tag-${i}`)]) {
    assert.throws(() => normalizeTags(value));
  }
  assert.equal(normalizeTags(["中".repeat(80)])[0].length, 80);
  assert.equal([...normalizeTags(["😀".repeat(80)])[0]].length, 80);
  assert.equal(normalizeTags(Array.from({ length: 64 }, (_, i) => `tag-${i}`)).length, 64);
});

test("prepare validation uses 80 runes and rejects invalid GitHub identities", () => {
  for (const name of ["owner/repo", "a/.github", `${"a".repeat(39)}/${"r".repeat(100)}`]) {
    assert.equal(validRepositoryName(name), true);
    assert.equal(validName(name), true);
  }
  for (const name of ["a_b/repo", "a.b/repo", "-a/repo", "a-/repo", "a--b/repo", "a/.", "a/..", "a/repo.git", "a/b/c", "https://github.com/a/b", `${"a".repeat(40)}/repo`, `a/${"r".repeat(101)}`]) assert.equal(validName(name), false, name);
  assert.deepEqual(normalizedTopics([]), []);
  assert.equal([...normalizedTopics(["😀".repeat(80)])[0]].length, 80);
  for (const value of [["a".repeat(81)], ["x\n"], [" "], Array.from({ length: 65 }, (_, i) => `tag-${i}`)]) assert.throws(() => normalizedTopics(value));
});

test("native empty remains empty and old raw tags only enrich research labels", () => {
  const roster = document(project(1, "now/one"), project(2, "now/two"), project(3, "now/three"));
  const original = layer(project(1, "now/one", { github_topics: [] }), project(2, "now/two"), project(3, "now/three"));
  const legacy = document(project(1, "old/one", { github_topics: ["Investment", "Agent"], research_tags: [" agent ", " 中文 "] }), project(2, "old/two", { github_topics: ["SaaS"] }));
  const output = mergeTagDocuments({ roster, currentLayers: [original], legacy });
  assert.deepEqual(output.document.projects, [project(1, "now/one", { github_topics: [], research_tags: ["agent", "中文", "investment"] }), project(2, "now/two", { research_tags: ["saas"] })]);
  assert.equal(output.report.stats.current_known, 1);
  assert.equal(output.report.stats.current_unknown, 2);
  assert.equal(output.report.stats.research_covered, 2);
  assert.equal(output.report.stats.skipped_no_fields, 1);
  assert.equal(Object.hasOwn(original.document.projects[1], "github_topics"), false);
});

test("legacy names are remapped solely by ID without moving labels to a namesake", () => {
  const roster = document(project(1, "current/name"), project(2, "old/name"));
  const result = mergeTagDocuments({ roster, currentLayers: [layer(project(1, "CURRENT/name", { github_topics: ["Fresh"] }), project(2, "old/name", { github_topics: [] }))], legacy: document(project(1, "old/name", { research_tags: ["历史"] })) });
  assert.deepEqual(result.document.projects[0], project(1, "current/name", { github_topics: ["fresh"], research_tags: ["历史"] }));
  assert.equal(Object.hasOwn(result.document.projects[1], "research_tags"), false);
  assert.equal(result.report.name_corrections[0].repository_id, 1);
});

test("legacy raw topics are not capped at eight and combined limits are enforced", () => {
  const roster = document(project(1, "owner/one"));
  const currentLayers = [layer(project(1, "owner/one"))];
  const raw = Array.from({ length: 20 }, (_, i) => `topic-${i}`);
  const result = mergeTagDocuments({ roster, currentLayers, legacy: document(project(1, "owner/one", { github_topics: raw, research_tags: ["中文", "TOPIC-19"] })) });
  assert.equal(result.document.projects[0].research_tags.length, 21);
  assert.ok(result.document.projects[0].research_tags.includes("topic-19"));
  assert.throws(() => mergeTagDocuments({ roster, currentLayers, legacy: document(project(1, "owner/one", { github_topics: ["new"], research_tags: Array.from({ length: 64 }, (_, i) => `tag-${i}`) })) }), /64 unique/);
});

test("recovery adds verified current data but conflicting successes are rejected", () => {
  const roster = document(project(1, "owner/one"));
  const legacy = document(project(1, "owner/one", { github_topics: ["old"] }));
  const recovered = layer(project(1, "owner/one", { github_topics: ["Current"] }));
  const result = mergeTagDocuments({ roster, legacy, currentLayers: [layer(project(1, "owner/one")), recovered] });
  assert.deepEqual(result.document.projects[0], project(1, "owner/one", { github_topics: ["current"], research_tags: ["old"] }));
  assert.throws(() => mergeTagDocuments({ roster, legacy, currentLayers: [recovered, layer(project(1, "owner/one", { github_topics: ["different"] }))] }), /conflicting/);
});

test("invalid identities, duplicate rows and untrusted success claims fail", () => {
  const base = { roster: document(project(1, "owner/one")), legacy: document(), currentLayers: [layer(project(1, "owner/one", { github_topics: ["x"] }))] };
  const variations = [
    { ...base, roster: document(project(1, "owner/one"), project(1, "owner/two")) },
    { ...base, currentLayers: [layer(project(2, "owner/two", { github_topics: [] }))] },
    { ...base, currentLayers: [layer(project(1, "wrong/name", { github_topics: [] }))] },
    { ...base, currentLayers: [layer(project(1, "owner/one", { github_topics: [] }), project(1, "owner/one", { github_topics: [] }))] },
    { ...base, legacy: document(project(2, "owner/two", { research_tags: [] })) },
    { ...base, legacy: document(project(1, "owner/one", { research_tags: ["bad\n"] })) },
    { ...base, legacy: document(project(1, "owner/one", { github_topics: null })) },
  ];
  const badSuccess = structuredClone(base);
  badSuccess.currentLayers[0].report.observations[0].status = "failed";
  variations.push(badSuccess);
  const unfinished = structuredClone(base);
  unfinished.currentLayers[0].report.finished = false;
  variations.push(unfinished);
  const invalidStatus = structuredClone(base);
  invalidStatus.currentLayers[0].report.observations[0].status = "pending";
  variations.push(invalidStatus);
  for (const input of variations) assert.throws(() => mergeTagDocuments(input));
});

test("CLI writes a hash-verified import or writes nothing after invalid data", t => {
  const dir = mkdtempSync(join(tmpdir(), "repotempo-tags-merge-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const inputs = { roster: document(project(1, "owner/one")), legacy: document(), current: layer(project(1, "owner/one", { github_topics: [] })) };
  const paths = Object.fromEntries(["roster", "legacy", "current", "current-report", "output", "report"].map(name => [name, join(dir, `${name}.json`)]));
  writeFileSync(paths.roster, JSON.stringify(inputs.roster));
  writeFileSync(paths.legacy, JSON.stringify(inputs.legacy));
  writeFileSync(paths.current, JSON.stringify(inputs.current.document));
  writeFileSync(paths["current-report"], JSON.stringify(inputs.current.report));
  const script = fileURLToPath(new URL("./merge-tags.mjs", import.meta.url));
  const argv = Object.entries(paths).flatMap(([key, value]) => [`--${key}`, value]);
  const result = spawnSync(process.execPath, [script, ...argv], { encoding: "utf8" });
  assert.equal(result.status, 0, result.stderr);
  const output = readFileSync(paths.output);
  assert.equal(JSON.parse(readFileSync(paths.report, "utf8")).sha256, createHash("sha256").update(output).digest("hex"));
  assert.notEqual(spawnSync(process.execPath, [script, ...argv], { encoding: "utf8" }).status, 0);
  writeFileSync(paths.current, JSON.stringify(document(project(1, "owner/one", { github_topics: ["bad\n"] }))));
  const invalidOutput = join(dir, "invalid-output.json"), invalidReport = join(dir, "invalid-report.json");
  const badArgs = [...argv];
  badArgs[badArgs.indexOf("--output") + 1] = invalidOutput;
  badArgs[badArgs.indexOf("--report") + 1] = invalidReport;
  assert.notEqual(spawnSync(process.execPath, [script, ...badArgs], { encoding: "utf8" }).status, 0);
  assert.equal(existsSync(invalidOutput), false);
  assert.equal(existsSync(invalidReport), false);
});
