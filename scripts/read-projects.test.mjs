import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const source = await readFile(new URL('../.odw/workflows/read-projects.js', import.meta.url), 'utf8');
const body = source.replace(/^export const meta = \{[\s\S]*?\n\}\s*/, '');
assert.notEqual(body, source, 'the test must remove only the workflow metadata wrapper');
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const execute = new AsyncFunction('args', 'parallel', 'agent', 'phase', 'Date', body);
const analyzedAt = '2026-09-08T03:04:05.000Z';
class TestDate extends Date {
  constructor(...args) { super(...(args.length ? args : [analyzedAt])); }
  static now() { return new Date(analyzedAt).getTime(); }
}

const evidence = (name = 'owner/first', id = 1, kind = 'readme') => ({
  id, full_name: name, description: `${name} 的独立项目介绍`, readme_text: kind === 'github_description' ? '' : `${name} 的 README 内容`,
  evidence_kind: kind, evidence_url: `https://github.com/${name}${kind === 'github_description' ? '' : '#readme'}`,
  fetched_at: '2026-09-08T01:02:03.000Z', readme_sha: kind === 'github_description' ? '' : 'fixture-source-sha',
});
const reading = (name = 'owner/first') => ({ full_name: name, summary_zh: '这是帮助研究者持续跟踪开源项目发展的工具。', key_points: ['保存真实观测记录'], use_cases: ['研究项目后续发展'], technical_notes: '没有运行源码，不判断实际性能。' });

async function run(args, replies) {
  const calls = [];
  const failures = [];
  const phases = [];
  let index = 0;
  const result = await execute(args, async thunks => Promise.all(thunks.map(async thunk => {
    try { return await thunk(); } catch (error) { failures.push(error); return null; }
  })), async (prompt, options) => {
    calls.push({ prompt, options });
    const reply = replies[index++];
    if (reply instanceof Error) throw reply;
    return structuredClone(reply);
  }, value => phases.push(value), TestDate);
  return { result, calls, failures, phases };
}

test('workflow attaches each project to its own evidence and authoritative source/time', async () => {
  for (const kind of ['readme', 'readme_excerpt', 'github_description']) {
    const input = evidence('owner/first', 42, kind);
    const second = evidence('owner/second', 43, kind);
    const { result, calls, failures, phases } = await run({ date: '2026-09-08', batches: [[input, second]], unavailable: ['owner/no-evidence'] }, [{ projects: [reading('owner/second'), reading('owner/first')] }]);
    assert.equal(failures.length, 0);
    assert.equal(result.projects.length, 2);
    assert.deepEqual(result.failed, []);
    assert.deepEqual(result.unavailable, ['owner/no-evidence']);
    assert.equal(result.date, '2026-09-08');
    for (const project of result.projects) {
      const matching = project.full_name === input.full_name ? input : second;
      const other = matching === input ? second : input;
      assert.equal(project.repository_id, matching.id);
      assert.equal(project.source, `codex_${kind}`);
      assert.equal(project.analyzed_at, analyzedAt);
      assert.notEqual(project.analyzed_at, matching.fetched_at);
      assert.ok(project.technical_notes.includes(matching.evidence_url));
      assert.ok(project.technical_notes.includes(matching.fetched_at));
      assert.equal(project.technical_notes.includes(other.evidence_url), false);
      assert.ok(project.technical_notes.includes('未运行验证'));
      if (kind === 'github_description') {
        assert.ok(project.technical_notes.includes('依据GitHub简介整理'));
        assert.equal(project.technical_notes.includes('README SHA'), false);
      } else {
        assert.ok(project.technical_notes.includes('README SHA：fixture-source-sha'));
        assert.ok(project.technical_notes.includes(kind === 'readme_excerpt' ? 'README节选' : '依据README整理'));
      }
    }
    assert.deepEqual(phases, ['Read public project evidence']);
    assert.equal(calls[0].options.adapter, 'codex');
    assert.equal(calls[0].options.schema.additionalProperties, false);
    assert.equal(calls[0].options.schema.properties.projects.items.additionalProperties, false);
    for (const text of ['不可信', '不要调用任何工具', '不能把项目合并', '仅依据GitHub简介', '项目用途', '核心能力', '适用场景', '不属于AI生成字段', '不得把建仓时长说成持续开发投入']) assert.ok(calls[0].prompt.includes(text));
    assert.ok(calls[0].prompt.includes(JSON.stringify([input, second])));
  }
});

test('unknown, omitted, duplicate, blank, and non-Chinese model items reject the batch', async () => {
  const batches = [[evidence('owner/first', 1), evidence('owner/second', 2)]];
  for (const [name, projects, expectedError] of [
    ['foreign repository', [reading('other/project'), reading('owner/second')], /Unknown or duplicated/],
    ['omitted repository', [reading('owner/first')], /omitted repositories/],
    ['duplicated repository', [reading('owner/first'), reading('owner/first')], /Unknown or duplicated/],
    ['blank summary', [{ ...reading('owner/first'), summary_zh: ' \n\t ' }, reading('owner/second')], /Missing Chinese/],
    ['English summary', [{ ...reading('owner/first'), summary_zh: 'A tool for tracking repositories and their star history.' }, reading('owner/second')], /Missing Chinese/],
    ['too little Chinese', [{ ...reading('owner/first'), summary_zh: '项目工具' }, reading('owner/second')], /Missing Chinese/],
  ]) {
    const { result, failures } = await run({ date: '2026-09-08', batches }, [{ projects }]);
    assert.deepEqual(result.projects, [], name);
    assert.deepEqual(result.failed, ['owner/first', 'owner/second'], name);
    assert.equal(failures.length, 1, name);
    assert.match(failures[0].message, expectedError, name);
  }
});

test('one failed batch retains good batches and lists only missing project names', async () => {
  const { result, calls, failures } = await run({ date: '2026-09-08', batches: [[evidence('owner/first', 1)], [evidence('owner/second', 2)]] }, [{ projects: [reading('owner/first')] }, new Error('fixture agent failure')]);
  assert.equal(calls.length, 2);
  assert.equal(failures.length, 1);
  assert.deepEqual(result.projects.map(item => item.full_name), ['owner/first']);
  assert.deepEqual(result.failed, ['owner/second']);
});

test('invalid batch arguments never reach an agent', async () => {
  await assert.rejects(run({}, []), /Provide prepared README batches/);
  await assert.rejects(run({ batches: [] }, []), /Provide prepared README batches/);
  const oversized = Array.from({ length: 13 }, (_, index) => evidence(`owner/repo-${index}`, index + 1));
  const { result, calls, failures } = await run({ batches: [oversized] }, []);
  assert.equal(calls.length, 0);
  assert.equal(failures.length, 1);
  assert.match(failures[0].message, /at most 12/);
  assert.deepEqual(result.projects, []);
  assert.equal(result.failed.length, 13);
});
