import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { prepare, readingEvidence, validRepository, validRepositoryID } from './prepare-readings.mjs';

const project = (id = 1, name = 'owner/project') => ({ id, full_name: name, description: '公开项目简介', first_seen_at: '2026-08-30T02:00:00Z' });
const readme = (text, sha = 'fixture-readme-sha') => ({ encoding: 'base64', content: Buffer.from(text, 'utf8').toString('base64'), sha });
const fetchedAt = '2026-09-08T01:02:03.000Z';

test('repository names and IDs reject URLs, traversal, invalid owners, and unsafe numbers', () => {
  for (const name of ['o/r', 'OpenAI/codex', 'my-org/repo.name_1', 'owner/.github', `${'a'.repeat(39)}/${'r'.repeat(100)}`]) {
    assert.equal(validRepository(name), true, name);
  }
  for (const name of [null, 123, '', 'owner', 'owner/repo/subdir', 'owner/.', 'owner/..', '-owner/repo', 'owner-/repo', '---/repo', `${'a'.repeat(40)}/repo`, `owner/${'r'.repeat(101)}`, 'https://github.com/owner/repo', 'owner/repo?token=test', 'owner/repo;echo secret', 'owner/项目']) {
    assert.equal(validRepository(name), false, String(name));
  }
  for (const id of [1, Number.MAX_SAFE_INTEGER, '42', '9223372036854775807']) assert.equal(validRepositoryID(id), true);
  for (const id of [null, undefined, 0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1, NaN, Infinity, '', '0', '01', '../escaped', '1/../../escaped', '1.json', '12345678901234567890']) {
    assert.equal(validRepositoryID(id), false, String(id));
    assert.throws(() => readingEvidence({ ...project(), id }, undefined, fetchedAt), /Invalid repository ID/);
  }
});

test('README evidence preserves identity, UTF-8, and exact provenance without modifying input', () => {
  const input = project(42, 'team/chinese-writer');
  const before = structuredClone(input);
  const value = readingEvidence(input, readme('  # 中文写作🧭\n这是项目原始说明。\n边界：尚未测试。  '), fetchedAt);
  assert.deepEqual(input, before);
  assert.deepEqual(value, {
    ...input, fetched_at: fetchedAt,
    evidence_url: 'https://github.com/team/chinese-writer#readme', evidence_kind: 'readme',
    readme_sha: 'fixture-readme-sha', readme_text: '# 中文写作🧭\n这是项目原始说明。\n边界：尚未测试。',
  });
});

test('Chinese README truncation preserves code points at both ends and marks the gap', () => {
  const full = '头部说明' + '中文🧭'.repeat(6000) + '最后的限制与许可';
  const runes = Array.from(full);
  const value = readingEvidence(project(), readme(full), fetchedAt);
  assert.equal(value.evidence_kind, 'readme_excerpt');
  assert.equal(value.readme_text, `${runes.slice(0, 12000).join('')}\n[中间内容省略]\n${runes.slice(-4000).join('')}`);
  assert.equal(value.readme_text.includes('\ufffd'), false);
  assert.equal(Buffer.from(value.readme_text).toString('utf8'), value.readme_text);
  assert.equal(value.readme_text.endsWith('最后的限制与许可'), true);
  const exact = readingEvidence(project(), readme('中'.repeat(16000)), fetchedAt);
  assert.equal(exact.evidence_kind, 'readme');
  assert.equal(Array.from(exact.readme_text).length, 16000);
});

test('missing, blank, or unsupported README evidence falls back to the GitHub description', () => {
  for (const input of [undefined, null, {}, readme(' \n\t '), { encoding: 'none', content: 'unavailable' }]) {
    const value = readingEvidence(project(), input, fetchedAt);
    assert.equal(value.evidence_kind, 'github_description');
    assert.equal(value.evidence_url, 'https://github.com/owner/project');
    assert.equal(value.readme_text, '');
    assert.equal(value.description, '公开项目简介');
  }
});

async function temporaryWorkspace(t) {
  const root = await mkdtemp(join(tmpdir(), 'repotempo-readings-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  return root;
}

// Only this temporary executable is exposed as gh. It returns fixture JSON or
// a fixture failure; it never talks to GitHub or invokes a model.
async function fakeGitHub(t, root, responses = {}) {
  const bin = join(root, 'bin');
  const calls = join(root, 'gh-calls.jsonl');
  await mkdir(bin);
  const executable = `#!${process.execPath}\nconst fs = require('node:fs');\nconst args = process.argv.slice(2);\nfs.appendFileSync(${JSON.stringify(calls)}, JSON.stringify(args) + '\\n');\nconst responses = ${JSON.stringify(responses)};\nconst response = responses[args[1]];\nif (args[0] !== 'api' || !response || response.error) { process.stderr.write('fixture-sensitive-stderr-never-copy'); process.exit(1); }\nprocess.stdout.write(JSON.stringify(response));\n`;
  await writeFile(join(bin, 'gh'), executable, { mode: 0o700 });
  const previousPath = process.env.PATH;
  process.env.PATH = bin;
  t.after(() => { if (previousPath === undefined) delete process.env.PATH; else process.env.PATH = previousPath; });
  return async () => {
    try { return (await readFile(calls, 'utf8')).trim().split('\n').filter(Boolean).map(JSON.parse); }
    catch (error) { if (error.code === 'ENOENT') return []; throw error; }
  };
}

test('prepare degrades unavailable README data, excludes empty evidence, and writes auditable batches', async t => {
  const root = await temporaryWorkspace(t);
  const readCalls = await fakeGitHub(t, root, { 'repos/owner/has-readme/readme': readme('真实 README 内容。') });
  const manifest = { date: '2026-09-08', projects: [project(1, 'owner/has-readme'), project(2, 'owner/description-only'), { ...project(3, 'owner/no-evidence'), description: ' \t ' }] };
  const input = join(root, 'queue.json');
  const output = join(root, 'output');
  await writeFile(input, JSON.stringify(manifest));
  const value = await prepare(input, output);
  assert.deepEqual(value.batches.map(batch => batch.map(item => item.full_name)), [['owner/has-readme', 'owner/description-only']]);
  assert.deepEqual(value.unavailable, ['owner/no-evidence']);
  assert.equal(value.date, manifest.date);
  assert.equal(value.batches[0][0].evidence_kind, 'readme');
  assert.equal(value.batches[0][1].evidence_kind, 'github_description');
  assert.match(value.batches[0][1].fetch_note, /only repository description available/);
  const saved = await readFile(join(output, 'reading-input.json'), 'utf8');
  assert.deepEqual(JSON.parse(saved), value);
  assert.equal(saved.includes('fixture-sensitive-stderr'), false);
  assert.deepEqual((await readCalls()).map(args => args[1]).sort(), manifest.projects.map(item => `repos/${item.full_name}/readme`).sort());
  for (const name of ['reading-input.json', 'evidence/1.json', 'evidence/2.json', 'evidence/3.json']) {
    assert.equal((await stat(join(output, name))).mode & 0o777, 0o600);
  }
});

test('same-day cached evidence avoids gh and the queue limit and 12-item batch bounds are respected', async t => {
  const root = await temporaryWorkspace(t);
  const readCalls = await fakeGitHub(t, root);
  const output = join(root, 'output');
  await mkdir(join(output, 'evidence'), { recursive: true });
  const projects = Array.from({ length: 14 }, (_, index) => project(index + 1, `owner/repo-${index + 1}`));
  for (const input of projects) {
    const evidence = readingEvidence(input, readme(`项目 ${input.id} 的缓存说明。`), new Date().toISOString());
    await writeFile(join(output, 'evidence', `${input.id}.json`), JSON.stringify(evidence));
  }
  const input = join(root, 'queue.json');
  await writeFile(input, JSON.stringify({ date: '2026-09-08', projects }));
  const value = await prepare(input, output, 13);
  assert.deepEqual(value.batches.map(batch => batch.length), [12, 1]);
  assert.deepEqual(value.batches.flat().map(item => item.id), projects.slice(0, 13).map(item => item.id));
  assert.deepEqual(await readCalls(), []);
});

test('invalid queued IDs are rejected before using them as filesystem paths or invoking gh', async t => {
  const root = await temporaryWorkspace(t);
  const readCalls = await fakeGitHub(t, root);
  const input = join(root, 'queue.json');
  const output = join(root, 'output');
  await writeFile(input, JSON.stringify({ projects: [project('../../escaped', 'owner/repo')] }));
  await assert.rejects(prepare(input, output), /Invalid repository ID/);
  assert.deepEqual(await readdir(join(output, 'evidence')), []);
  await assert.rejects(stat(join(root, 'escaped.json')), { code: 'ENOENT' });
  assert.deepEqual(await readCalls(), []);
});
