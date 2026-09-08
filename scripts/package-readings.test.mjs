import test from 'node:test';
import assert from 'node:assert/strict';
import { packageReadings, validTimestamp } from './package-readings.mjs';

const importedAt = '2026-09-08T08:09:10.000Z';
const analyzedAt = '2026-09-08T03:04:05.000Z';
const reading = (overrides = {}) => ({
  full_name: 'owner/project', repository_id: 42,
  summary_zh: '这个项目帮助研究人员持续观察开源项目的发展。',
  key_points: ['保存真实观测'], use_cases: ['阅读和研究'], technical_notes: '仅阅读资料，未运行源码。',
  source: 'codex_readme', model: 'fixture-model', analyzed_at: analyzedAt,
  ...overrides,
});

test('direct projects and ODW value wrappers produce the same strict import envelope', () => {
  const project = reading();
  const direct = { projects: [project], failed: ['owner/missing'] };
  const wrapped = { status: 'done', value: direct, source_path: '/private/workflow/result.json' };
  const before = structuredClone(wrapped);
  const expected = { projects: [project] };
  assert.deepEqual(packageReadings([direct], importedAt), expected);
  assert.deepEqual(packageReadings([wrapped], importedAt), expected);
  assert.deepEqual(wrapped, before);
  assert.deepEqual(Object.keys(packageReadings([wrapped])), ['projects']);
  assert.deepEqual(packageReadings([{ projects: [] }]), { projects: [] });
  for (const input of [{}, { projects: null }, { projects: {} }, { value: {} }]) {
    assert.throws(() => packageReadings([input]), /Missing projects array/);
  }
});

test('project identity validates canonical names and emits exact JSON numeric IDs', () => {
  for (const id of [42, '42', Number.MAX_SAFE_INTEGER, String(Number.MAX_SAFE_INTEGER)]) {
    const value = packageReadings([{ projects: [reading({ repository_id: id })] }]);
    assert.equal(value.projects[0].repository_id, Number(id));
    assert.equal(typeof value.projects[0].repository_id, 'number');
    assert.equal(Number.isSafeInteger(value.projects[0].repository_id), true);
    assert.equal(JSON.parse(JSON.stringify(value)).projects[0].repository_id, Number(id));
  }
  const legacyID = reading({ repository_id: undefined, github_repo_id: '42' });
  assert.equal(packageReadings([{ projects: [legacyID] }]).projects[0].repository_id, 42);
  const aliasesAgree = reading({ repository_id: 42, github_repo_id: '42' });
  assert.equal(packageReadings([{ projects: [aliasesAgree] }]).projects[0].repository_id, 42);
  for (const id of [null, undefined, 0, -1, '0', '01', '../42', '42.json', 1.5, Number.MAX_SAFE_INTEGER + 1, '9007199254740993', '9223372036854775807']) {
    assert.throws(() => packageReadings([{ projects: [reading({ repository_id: id })] }]), /Invalid project identity|exact JSON integer range/, String(id));
  }
  for (const name of ['https://github.com/owner/project', 'owner/../project', '-owner/project', 'owner-/project', 'owner/.', 'owner/project?token=fixture']) {
    assert.throws(() => packageReadings([{ projects: [reading({ full_name: name })] }]), /Invalid project identity/);
  }
});

test('ambiguous ID aliases and same-name records with conflicting IDs are rejected', () => {
  assert.throws(() => packageReadings([{ projects: [reading({ repository_id: 42, github_repo_id: 43 })] }]), /Conflicting repository IDs/);
  assert.throws(() => packageReadings([
    { projects: [reading({ full_name: 'Owner/Project', repository_id: 42 })] },
    { value: { projects: [reading({ full_name: 'owner/project', repository_id: 43 })] } },
  ]), /Conflicting repository IDs/);
});

test('legacy notes disclose unknown research time and never copy private source paths', () => {
  const project = reading({
    source: 'legacy_research', github_repo_id: 42, repository_id: undefined,
    analyzed_at: '2020-01-01T00:00:00Z', model: '', source_path: '/srv/private/raw/history.sqlite',
    readme_url: 'https://github.com/owner/project#readme', readme_sha: 'archive-readme-sha',
    legacy_record_updated_at: '2026-08-25T01:00:00Z',
  });
  const packaged = packageReadings([{ projects: [project] }], importedAt);
  const value = packaged.projects[0];
  assert.equal(value.source, 'legacy_research');
  assert.equal(value.analyzed_at, importedAt);
  assert.equal(value.model, '');
  for (const text of [project.technical_notes, project.readme_url, project.readme_sha, project.legacy_record_updated_at, importedAt, '不是原解读生成时间', '原生成时间未记录', 'analyzed_at 为导入时间', '不代表重新研究过该项目']) {
    assert.ok(value.technical_notes.includes(text), text);
  }
  const serialized = JSON.stringify(packaged);
  assert.equal(serialized.includes(project.source_path), false);
  assert.equal(serialized.includes('source_path'), false);
  assert.equal(serialized.includes(project.analyzed_at), false);
});

test('missing legacy source metadata remains explicitly unknown instead of invented', () => {
  const project = reading({ source: 'legacy_research', technical_notes: undefined, analyzed_at: undefined, model: undefined });
  const value = packageReadings([{ projects: [project] }], importedAt).projects[0];
  assert.ok(value.technical_notes.includes('原资料：旧日报研究卡'));
  assert.ok(value.technical_notes.includes('旧记录更新时间：未记录'));
  assert.ok(value.technical_notes.includes('原生成时间未记录'));
  assert.equal(value.technical_notes.includes('README SHA'), false);
  assert.equal(value.analyzed_at, importedAt);
});

test('fresh README readings supersede archived notes regardless of input order', () => {
  const legacy = reading({ source: 'legacy_research', summary_zh: '历史日报中的旧版项目说明。', model: '' });
  const fresh = reading({ source: 'codex_readme', summary_zh: '重新依据公开资料整理的中文项目说明。' });
  for (const projects of [[legacy, fresh], [fresh, legacy]]) {
    const value = packageReadings(projects.map(item => ({ projects: [item] })), importedAt).projects;
    assert.equal(value.length, 1);
    assert.deepEqual(value[0], fresh);
    assert.equal(value[0].technical_notes.includes('本次导入时间'), false);
  }
});

test('two nonlegacy readings preserve the first one rather than guessing priority from age or source', () => {
  const first = reading({ source: 'kimi_readme', summary_zh: '首先提供的完整项目解读应该被保留。' });
  const second = reading({ source: 'codex_readme', summary_zh: '后续提供的另一份项目解读。', analyzed_at: '2026-09-08T07:00:00.000Z' });
  const value = packageReadings([{ projects: [first] }, { value: { projects: [second] } }], importedAt);
  assert.deepEqual(value, { projects: [first] });
});

test('source and summary whitespace is normalized before legacy priority decisions', () => {
  const legacy = reading({ source: ' legacy_research ', summary_zh: ' 历史项目说明。 ', analyzed_at: undefined });
  const fresh = reading({ source: ' codex_readme ', model: ' fixture-model ', summary_zh: ' 真实中文解读。 ' });
  const one = packageReadings([{ projects: [legacy] }], importedAt).projects[0];
  assert.equal(one.source, 'legacy_research');
  assert.equal(one.summary_zh, '历史项目说明。');
  assert.ok(one.technical_notes.includes('原生成时间未记录'));
  const two = packageReadings([{ projects: [legacy, fresh] }], importedAt).projects[0];
  assert.equal(two.source, 'codex_readme');
  assert.equal(two.summary_zh, '真实中文解读。');
  assert.equal(two.model, 'fixture-model');
  assert.equal(two.analyzed_at, analyzedAt);
});

test('summary and provenance must be nonempty strings and typed lists cannot leak invalid JSON to Go', () => {
  for (const field of ['summary_zh', 'source']) {
    for (const invalid of [undefined, null, '', ' \n\t ', 1, [], {}]) {
      assert.throws(() => packageReadings([{ projects: [reading({ [field]: invalid })] }]), /Missing reading summary|Missing reading provenance/);
    }
  }
  for (const field of ['key_points', 'use_cases']) {
    for (const invalid of [null, '', {}, 1, false, [1], ['valid', null], ['valid', {}]]) {
      assert.throws(() => packageReadings([{ projects: [reading({ [field]: invalid })] }]), /Invalid reading list/);
    }
  }
  const defaults = packageReadings([{ projects: [reading({ key_points: undefined, use_cases: undefined, technical_notes: undefined, model: undefined })] }]).projects[0];
  assert.deepEqual(defaults.key_points, []);
  assert.deepEqual(defaults.use_cases, []);
  assert.equal(defaults.technical_notes, '');
  assert.equal(defaults.model, '');
});

test('notes and model fields reject objects, numbers, null, and other non-string values', () => {
  for (const field of ['technical_notes', 'model']) {
    for (const invalid of [null, 1, false, ['one'], {}]) {
      assert.throws(() => packageReadings([{ projects: [reading({ [field]: invalid })] }]), /Invalid technical_notes|Invalid model/);
    }
  }
});

test('timestamps must be valid RFC3339 values accepted by the strict Go importer', () => {
  for (const timestamp of [analyzedAt, '2024-02-29T23:59:59Z', '2000-02-29T00:00:00.123456789Z', '2026-09-08T11:04:05+08:00', '2026-09-07T23:04:05-04:00']) {
    assert.equal(validTimestamp(timestamp), true, timestamp);
    const value = packageReadings([{ projects: [reading({ analyzed_at: timestamp })] }], importedAt).projects[0];
    assert.equal(value.analyzed_at, timestamp);
  }
  for (const timestamp of [null, undefined, 0, {}, '', '0', 'September 8, 2026', '2026-09-08', '2026-09-08T03:04:05', '2026-02-30T00:00:00Z', '2026-02-29T00:00:00Z', '1900-02-29T00:00:00Z', '2026-13-01T00:00:00Z', '2026-09-08T24:00:00Z', '2026-09-08T00:60:00Z', '2026-09-08T00:00:60Z', '2026-09-08T00:00:00+24:00', '2026-09-08T00:00:00+08:60', '2026-09-08T03:04:05.1234567890Z', '0001-01-01T00:00:00Z', '0001-01-01T00:00:00.000000000Z', '0001-01-01T08:00:00+08:00']) {
    assert.equal(validTimestamp(timestamp), false, String(timestamp));
    assert.throws(() => packageReadings([{ projects: [reading({ analyzed_at: timestamp })] }]), /RFC3339 analysis timestamp/);
  }
  for (const timestamp of ['2026-02-30T00:00:00Z', '2026-09-08', 'not-a-date']) {
    assert.throws(() => packageReadings([{ projects: [reading({ source: 'legacy_research' })] }], timestamp), /RFC3339 analysis timestamp/);
  }
});
