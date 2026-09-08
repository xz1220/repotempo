#!/usr/bin/env node
// Export real public GitHub topics for an existing registry. This tool never
// opens the business database, changes authentication, or invokes an AI model.
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import { normalizeTags, validRepositoryName } from "./merge-tags.mjs";

const query = `query($ids:[ID!]!){
  rateLimit{cost remaining resetAt}
  nodes(ids:$ids){__typename ... on Repository{
    id databaseId nameWithOwner isPrivate
    repositoryTopics(first:100){nodes{topic{name}} pageInfo{hasNextPage}}
  }}
}`;

function options(argv) {
  const result = { batchSize: 50, interval: 1000, reserve: 100, limit: 0, date: new Date(Date.now() + 8 * 3600 * 1000).toISOString().slice(0, 10) };
  const names = { "--input": "input", "--output": "output", "--report": "report", "--batch-size": "batchSize", "--interval-ms": "interval", "--reserve": "reserve", "--limit": "limit", "--date": "date" };
  for (let index = 0; index < argv.length; index++) {
    const key = names[argv[index]];
    if (!key || index + 1 >= argv.length) throw new Error("Expected --input, --output and --report, with optional --batch-size/--interval-ms/--reserve/--limit/--date");
    const value = argv[++index];
    result[key] = ["batchSize", "interval", "reserve", "limit"].includes(key) ? Number(value) : value;
  }
  if (!result.input || !result.output || !result.report) throw new Error("--input, --output and --report are required");
  for (const key of ["input", "output", "report"]) result[key] = resolve(result[key]);
  if (new Set([result.input, result.output, result.report]).size !== 3) throw new Error("Input and output paths must be distinct");
  if (existsSync(result.output) || existsSync(result.report)) throw new Error("Refusing to overwrite an existing export; choose new output paths");
  if (!Number.isInteger(result.batchSize) || result.batchSize < 1 || result.batchSize > 50) throw new Error("batch-size must be 1..50");
  if (!Number.isInteger(result.interval) || result.interval < 1000) throw new Error("interval-ms must be at least 1000");
  if (!Number.isInteger(result.reserve) || result.reserve < 20 || !Number.isInteger(result.limit) || result.limit < 0) throw new Error("Invalid reserve or limit");
  if (!/^\d{4}-\d{2}-\d{2}$/.test(result.date)) throw new Error("date must be YYYY-MM-DD");
  return result;
}

function runGh(endpoint, body) {
  const args = ["api", endpoint, "--hostname", "github.com"];
  if (body !== undefined) args.push("--input", "-");
  let text = "", commandFailure = "";
  try {
    text = execFileSync("gh", args, { input: body === undefined ? undefined : JSON.stringify(body), encoding: "utf8", timeout: 45000, maxBuffer: 32 * 1024 * 1024, stdio: ["pipe", "pipe", "pipe"] });
  } catch (error) {
    text = typeof error.stdout === "string" ? error.stdout : String(error.stdout || "");
    // Never persist stderr, headers, config, or credential-bearing diagnostics.
    commandFailure = error.code === "ETIMEDOUT" ? "gh_timeout" : "gh_failed";
  }
  try { return { payload: JSON.parse(text), commandFailure }; }
  catch { return { payload: null, commandFailure: commandFailure || "invalid_json_response" }; }
}

export function normalizedTopics(values) {
  return normalizeTags(values, { context: "github_topics", allowBlank: false });
}

export const validName = validRepositoryName;

function shanghaiDay(value) {
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? new Date(parsed + 8 * 3600 * 1000).toISOString().slice(0, 10) : "";
}

function writeJSON(path, value) {
  mkdirSync(dirname(path), { recursive: true });
  const pending = `${path}.pending-${process.pid}`;
  writeFileSync(pending, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
  renameSync(pending, path);
}

async function main() {
  const cfg = options(process.argv.slice(2));
  const input = JSON.parse(readFileSync(cfg.input, "utf8"));
  if (!Array.isArray(input.projects)) throw new Error("Input must contain projects");
  const ids = new Set();
  for (const project of input.projects) {
    if (!Number.isSafeInteger(project.repository_id) || project.repository_id <= 0 || !validName(project.full_name) || ids.has(project.repository_id)) throw new Error("Invalid or duplicate registry identity");
    ids.add(project.repository_id);
  }
  const selected = [...input.projects].sort((a, b) => Number(shanghaiDay(b.first_seen_at) === cfg.date) - Number(shanghaiDay(a.first_seen_at) === cfg.date) || a.repository_id - b.repository_id);
  if (cfg.limit > 0) selected.splice(cfg.limit);
  const output = selected.map(({ repository_id, full_name }) => ({ repository_id, full_name }));
  const records = new Map(output.map(project => [project.repository_id, project]));
  const observations = new Map();
  const startedAt = new Date().toISOString();
  const batches = [];
  let graphqlRate = null, coreRate = null, stoppedReason = "", lastRequest = 0;

  const request = async (endpoint, body) => {
    const remaining = lastRequest + cfg.interval - Date.now();
    if (remaining > 0) await sleep(remaining);
    lastRequest = Date.now();
    return runGh(endpoint, body);
  };
  const failure = (project, reason, source) => observations.set(project.repository_id, {
    repository_id: project.repository_id, full_name: project.full_name, status: "failed", reason, source,
    captured_at: new Date().toISOString(),
  });
  const success = (project, topics, observedName, source, nodeID) => {
    records.get(project.repository_id).github_topics = topics;
    observations.set(project.repository_id, {
      repository_id: project.repository_id, full_name: project.full_name, observed_full_name: observedName,
      status: "success", topic_count: topics.length, source, captured_at: new Date().toISOString(),
      ...(nodeID ? { observed_node_id: nodeID } : {}),
      ...(project.full_name.toLowerCase() !== observedName.toLowerCase() ? { renamed: true } : {}),
    });
  };
  const checkpoint = complete => {
    const current = [...observations.values()];
    const succeeded = current.filter(item => item.status === "success");
    const todayIDs = new Set(selected.filter(item => shanghaiDay(item.first_seen_at) === cfg.date).map(item => item.repository_id));
    const failures = current.filter(item => item.status !== "success");
    const report = {
      started_at: startedAt, exported_at: new Date().toISOString(), finished: complete,
      complete: complete && !stoppedReason && failures.length === 0 && current.length === selected.length,
      stopped_reason: stoppedReason,
      input_path: cfg.input, date: cfg.date,
      source: "GitHub public repository API; no model inference",
      unknown_rule: "A failed project has no github_topics field; [] only means a successfully observed empty array.",
      stats: {
        registry_total: input.projects.length, selected: selected.length, successful: succeeded.length,
        nonempty: succeeded.filter(item => item.topic_count > 0).length, empty: succeeded.filter(item => item.topic_count === 0).length,
        failed: failures.length, pending: selected.length - current.length,
        today_selected: todayIDs.size, today_successful: succeeded.filter(item => todayIDs.has(item.repository_id)).length,
        tags: succeeded.reduce((sum, item) => sum + item.topic_count, 0),
        distinct_tags: new Set(output.flatMap(item => item.github_topics || [])).size,
        graphql_batches: batches.length,
      },
      rate_limits: { graphql: graphqlRate, core: coreRate }, batches, failures, observations: current,
    };
    writeJSON(cfg.output, { projects: output });
    writeJSON(cfg.report, report);
    return report;
  };

  const preflight = await request("graphql", { query: "query{rateLimit{cost remaining resetAt}}" });
  graphqlRate = preflight.payload?.data?.rateLimit || null;
  if (!graphqlRate || !Number.isFinite(graphqlRate.remaining)) stoppedReason = "graphql_rate_or_auth_unavailable";

  const nodes = selected.filter(item => typeof item.github_node_id === "string" && item.github_node_id.trim());
  for (let offset = 0; offset < nodes.length && !stoppedReason; offset += cfg.batchSize) {
    const batch = nodes.slice(offset, offset + cfg.batchSize);
    if (graphqlRate.remaining < cfg.reserve + Math.max(1, graphqlRate.cost || 1)) { stoppedReason = "graphql_reserve_reached"; break; }
    const response = await request("graphql", { query, variables: { ids: batch.map(item => item.github_node_id) } });
    const data = response.payload?.data;
    if (data?.rateLimit) graphqlRate = data.rateLimit;
    const evidence = { source: "https://api.github.com/graphql", captured_at: new Date().toISOString(), repository_ids: batch.map(item => item.repository_id), rate_limit: data?.rateLimit || null };
    if (Array.isArray(response.payload?.errors)) evidence.error_types = response.payload.errors.map(item => item.type || "graphql_error");
    batches.push(evidence);
    if (!Array.isArray(data?.nodes) || data.nodes.length !== batch.length) {
      for (const project of batch) failure(project, response.commandFailure || "unavailable_graphql_batch", evidence.source);
      stoppedReason = "graphql_batch_unavailable";
      checkpoint(false);
      break;
    }
    for (let index = 0; index < batch.length; index++) {
      const project = batch[index], node = data.nodes[index];
      try {
        if (!node) throw new Error("unresolved_node");
        if (node.__typename !== "Repository" || !Number.isSafeInteger(node.databaseId) || node.databaseId !== project.repository_id || !validName(node.nameWithOwner)) throw new Error("repository_identity_mismatch");
        if (node.isPrivate !== false) throw new Error("not_confirmed_public");
        if (node.repositoryTopics?.pageInfo?.hasNextPage !== false || !Array.isArray(node.repositoryTopics?.nodes)) throw new Error("incomplete_topics_connection");
        const topics = normalizedTopics(node.repositoryTopics.nodes.map(item => item?.topic?.name));
        success(project, topics, node.nameWithOwner, evidence.source, node.id);
      } catch (error) { failure(project, error.message, evidence.source); }
    }
    const report = checkpoint(false);
    process.stdout.write(`GraphQL ${batches.length}: ${report.stats.successful}/${selected.length} successful; remaining=${graphqlRate.remaining}\n`);
  }

  // REST is reserved for entries that genuinely lack a saved NodeID. Never
  // invent a NodeID or switch identities after a blocked GraphQL request.
  const missingNodes = selected.filter(item => typeof item.github_node_id !== "string" || !item.github_node_id.trim());
  if (!stoppedReason && missingNodes.length > 0) {
    const rateResponse = await request("rate_limit");
    coreRate = rateResponse.payload?.resources?.core || null;
    if (!coreRate || !Number.isFinite(coreRate.remaining) || coreRate.remaining < missingNodes.length + 10) stoppedReason = "insufficient_core_quota_for_missing_node_ids";
    for (const project of missingNodes) {
      if (stoppedReason) break;
      const source = `https://api.github.com/repositories/${project.repository_id}`;
      const response = await request(`repositories/${project.repository_id}`);
      if (coreRate) coreRate.remaining--;
      const repository = response.payload;
      if ([403, 429].includes(Number(repository?.status))) stoppedReason = "rest_access_or_rate_limit";
      try {
        if (response.commandFailure) throw new Error("repository_request_failed");
        if (!repository || repository.id !== project.repository_id || !validName(repository.full_name)) throw new Error("repository_identity_mismatch");
        if (repository.private !== false) throw new Error("not_confirmed_public");
        success(project, normalizedTopics(repository.topics), repository.full_name, source, repository.node_id);
      } catch (error) { failure(project, error.message, source); }
      checkpoint(false);
    }
  }
  for (const project of selected) {
    if (!observations.has(project.repository_id)) failure(project, stoppedReason || "not_requested", "not requested");
  }
  const final = checkpoint(true);
  process.stdout.write(`${JSON.stringify(final.stats)}\n`);
  if (final.stats.failed > 0 || stoppedReason) process.exitCode = 3;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(error => { process.stderr.write(`Tag export failed: ${error.message}\n`); process.exitCode = 1; });
}
