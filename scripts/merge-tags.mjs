#!/usr/bin/env node
// Merge already-exported evidence; never query GitHub or open a business DB.
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const own = (value, key) => Object.prototype.hasOwnProperty.call(value, key);
const object = value => value !== null && typeof value === "object" && !Array.isArray(value);

export function validRepositoryName(name) {
  if (typeof name !== "string" || !/^[A-Za-z0-9][A-Za-z0-9-]{0,38}\/[A-Za-z0-9_.-]{1,100}$/.test(name)) return false;
  const [owner, repository] = name.split("/");
  return !owner.endsWith("-") && !owner.includes("--") && repository !== "." && repository !== ".." && !repository.endsWith(".git");
}

function wellFormed(value) {
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(++index);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
    } else if (code >= 0xdc00 && code <= 0xdfff) return false;
  }
  return true;
}

export function normalizeTags(values, { context = "tags", allowBlank = true } = {}) {
  if (!Array.isArray(values)) throw new Error(`${context}: expected an array, not null or another type`);
  if (values.length > 256) throw new Error(`${context}: input exceeds 256 entries`);
  const result = [], seen = new Set();
  for (const value of values) {
    if (typeof value !== "string" || !wellFormed(value)) throw new Error(`${context}: expected well-formed Unicode strings`);
    // Reject controls before trimming, so malformed input cannot be hidden.
    if (/\p{Cc}/u.test(value)) throw new Error(`${context}: control characters are not allowed`);
    const trimmed = value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
    // Go strings.ToLower uses simple, per-rune lowercase, not contextual final
    // sigma or the JS multi-character expansion for capital dotted I.
    const tag = [...trimmed].map(char => char === "\u0130" ? "i" : char.toLowerCase()).join("");
    if (!tag) {
      if (!allowBlank) throw new Error(`${context}: blank tag is not a valid API observation`);
      continue;
    }
    if ([...tag].length > 80) throw new Error(`${context}: tag exceeds 80 Unicode characters`);
    if (!seen.has(tag)) { result.push(tag); seen.add(tag); }
  }
  if (result.length > 64) throw new Error(`${context}: exceeds 64 unique tags`);
  return result;
}

function projectsOf(document, context) {
  if (!object(document) || !Array.isArray(document.projects)) throw new Error(`${context}: expected an object with projects`);
  return document.projects;
}

function identity(project, context) {
  if (!object(project) || !Number.isSafeInteger(project.repository_id) || project.repository_id <= 0 || !validRepositoryName(project.full_name)) throw new Error(`${context}: invalid repository identity`);
}

function validateFields(project, fields, context) {
  for (const key of Object.keys(project)) {
    if (!fields.includes(key)) throw new Error(`${context}: unexpected field ${key}`);
  }
}

export function mergeTagDocuments({ roster, currentLayers, legacy }) {
  const registry = new Map(), names = new Set();
  for (const project of projectsOf(roster, "roster")) {
    identity(project, "roster");
    if (registry.has(project.repository_id) || names.has(project.full_name.toLowerCase())) throw new Error("roster: duplicate repository identity");
    registry.set(project.repository_id, project.full_name);
    names.add(project.full_name.toLowerCase());
  }
  if (!Array.isArray(currentLayers) || currentLayers.length === 0) throw new Error("at least one current data/report pair is required");
  const current = new Map(), currentRequested = new Set(), research = new Map(), nameCorrections = [];

  for (const [layerIndex, layer] of currentLayers.entries()) {
    const context = `current layer ${layerIndex + 1}`;
    const records = projectsOf(layer.document, context);
    if (!object(layer.report) || layer.report.finished !== true || !Array.isArray(layer.report.observations)) throw new Error(`${context}: a finished observation report is required`);
    const observations = new Map();
    for (const observation of layer.report.observations) {
      identity(observation, context);
      if (observation.status !== "success" && observation.status !== "failed") throw new Error(`${context}: invalid observation status`);
      if (observations.has(observation.repository_id)) throw new Error(`${context}: duplicate observation identity`);
      observations.set(observation.repository_id, observation);
    }
    const seen = new Set();
    let known = 0;
    for (const project of records) {
      identity(project, context);
      validateFields(project, ["repository_id", "full_name", "github_topics"], context);
      const id = project.repository_id, canonical = registry.get(id);
      if (!canonical || canonical.toLowerCase() !== project.full_name.toLowerCase()) throw new Error(`${context}: full_name does not match registry ID ${id}`);
      if (seen.has(id)) throw new Error(`${context}: duplicate project ID ${id}`);
      seen.add(id);
      currentRequested.add(id);
      const observation = observations.get(id);
      if (!observation || observation.full_name.toLowerCase() !== project.full_name.toLowerCase()) throw new Error(`${context}: missing or mismatched observation for ${id}`);
      if (!own(project, "github_topics")) {
        if (observation.status === "success") throw new Error(`${context}: successful observation has no github_topics for ${id}`);
        continue;
      }
      if (observation.status !== "success") throw new Error(`${context}: failed observation cannot provide github_topics for ${id}`);
      const topics = normalizeTags(project.github_topics, { context: `${context} ${id}.github_topics` });
      const previous = current.get(id);
      if (previous && JSON.stringify([...previous].sort()) !== JSON.stringify([...topics].sort())) throw new Error(`${context}: conflicting successful observations for ${id}`);
      current.set(id, topics);
      known++;
    }
    if (seen.size !== observations.size) throw new Error(`${context}: report and data identify different project sets`);
    if (layer.report.stats?.successful !== undefined && layer.report.stats.successful !== known) throw new Error(`${context}: successful count does not match report`);
  }

  const legacySeen = new Set();
  for (const project of projectsOf(legacy, "legacy")) {
    identity(project, "legacy");
    validateFields(project, ["repository_id", "full_name", "github_topics", "research_tags"], "legacy");
    const id = project.repository_id, canonical = registry.get(id);
    if (!canonical) throw new Error(`legacy: ID ${id} is not in the registry`);
    if (legacySeen.has(id)) throw new Error(`legacy: duplicate project ID ${id}`);
    legacySeen.add(id);
    if (project.full_name !== canonical) nameCorrections.push({ repository_id: id, legacy_full_name: project.full_name, full_name: canonical });
    const supplied = own(project, "research_tags") || own(project, "github_topics");
    if (!supplied) continue;
    const labels = [];
    for (const field of ["research_tags", "github_topics"]) {
      if (own(project, field)) labels.push(...normalizeTags(project[field], { context: `legacy ${id}.${field}` }));
    }
    // Old GitHub topics remain old-report evidence, never current native tags.
    research.set(id, normalizeTags(labels, { context: `legacy ${id}.merged_research_tags` }));
  }

  const projects = [], skipped = [];
  for (const [id, full_name] of [...registry].sort((a, b) => a[0] - b[0])) {
    const project = { repository_id: id, full_name };
    if (current.has(id)) project.github_topics = current.get(id);
    if (research.has(id)) project.research_tags = research.get(id);
    if (!own(project, "github_topics") && !own(project, "research_tags")) {
      skipped.push({ repository_id: id, full_name, reason: "no_tag_fields" });
    } else projects.push(project);
  }
  return {
    document: { projects },
    report: {
      stats: {
        registry: registry.size, emitted: projects.length, skipped_no_fields: skipped.length,
        current_requested: currentRequested.size, current_known: current.size,
        current_nonempty: [...current.values()].filter(value => value.length > 0).length,
        current_empty: [...current.values()].filter(value => value.length === 0).length,
        current_unknown: registry.size - current.size,
        research_provided: research.size, research_covered: [...research.values()].filter(value => value.length > 0).length,
        research_labels: [...research.values()].reduce((sum, value) => sum + value.length, 0),
        legacy_name_corrections: nameCorrections.length,
      },
      semantics: "Only successful current API arrays populate github_topics. Complete legacy native arrays and old tags are combined as research_tags. Unknown native arrays remain absent; observed empty arrays remain [].",
      name_corrections: nameCorrections, skipped,
    },
  };
}

function parseArgs(argv) {
  const options = { current: [], currentReport: [] };
  const names = { "--roster": "roster", "--current": "current", "--current-report": "currentReport", "--legacy": "legacy", "--output": "output", "--report": "report" };
  for (let index = 0; index < argv.length; index++) {
    const key = names[argv[index]];
    if (!key || index + 1 >= argv.length) throw new Error("Expected --roster, --legacy, --current/--current-report pairs, --output, and --report");
    const path = resolve(argv[++index]);
    if (Array.isArray(options[key])) options[key].push(path);
    else if (options[key]) throw new Error(`Repeated ${key} option`);
    else options[key] = path;
  }
  if (!options.roster || !options.legacy || !options.output || !options.report || options.current.length === 0 || options.current.length !== options.currentReport.length) throw new Error("Missing paths or mismatched current data/report pairs");
  const inputs = [options.roster, options.legacy, ...options.current, ...options.currentReport];
  if (options.output === options.report || inputs.includes(options.output) || inputs.includes(options.report)) throw new Error("Outputs must differ from all inputs");
  if (existsSync(options.output) || existsSync(options.report)) throw new Error("Refusing to overwrite an existing export");
  return options;
}

function main() {
  const cfg = parseArgs(process.argv.slice(2));
  const read = path => JSON.parse(readFileSync(path, "utf8"));
  const merged = mergeTagDocuments({ roster: read(cfg.roster), legacy: read(cfg.legacy), currentLayers: cfg.current.map((path, index) => ({ document: read(path), report: read(cfg.currentReport[index]) })) });
  const contents = `${JSON.stringify(merged.document, null, 2)}\n`;
  const sha256 = createHash("sha256").update(contents).digest("hex");
  const report = { prepared_at: new Date().toISOString(), inputs: { roster: cfg.roster, legacy: cfg.legacy, current: cfg.current, current_reports: cfg.currentReport }, ...merged.report, sha256 };
  for (const path of [cfg.output, cfg.report]) mkdirSync(dirname(path), { recursive: true });
  writeFileSync(cfg.output, contents, { flag: "wx", mode: 0o600 });
  writeFileSync(cfg.report, `${JSON.stringify(report, null, 2)}\n`, { flag: "wx", mode: 0o600 });
  process.stdout.write(`${JSON.stringify({ ...report.stats, sha256 })}\n`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(); } catch (error) { process.stderr.write(`Tag merge failed: ${error.message}\n`); process.exitCode = 1; }
}
