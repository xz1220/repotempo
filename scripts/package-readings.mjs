// Convert offline reading results to the Go CLI's strict batch-import format.
import { readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { validRepository, validRepositoryID } from './prepare-readings.mjs';

export function validTimestamp(value) {
  if (typeof value !== 'string') return false;
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(?:Z|[+-](\d{2}):(\d{2}))$/.exec(value);
  if (!match) return false;
  const [, year, month, day, hour, minute, second, , zoneHour = '0', zoneMinute = '0'] = match;
  const y = Number(year), m = Number(month), d = Number(day);
  const leap = y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  const instant = Date.parse(value);
  const isGoZero = instant === Date.parse('0001-01-01T00:00:00Z') && Number(match[7] || '0') === 0;
  return m >= 1 && m <= 12 && d >= 1 && d <= days[m - 1] && Number(hour) <= 23 && Number(minute) <= 59 && Number(second) <= 59 && Number(zoneHour) <= 23 && Number(zoneMinute) <= 59 && Number.isFinite(instant) && !isGoZero;
}

export function packageReadings(inputs, importedAt = new Date().toISOString()) {
  const projects = new Map();
  for (const raw of inputs) {
    const input = raw.value ?? raw;
    if (!Array.isArray(input.projects)) throw new Error('Missing projects array');
    for (const item of input.projects) {
      if (item.repository_id != null && item.github_repo_id != null && String(item.repository_id) !== String(item.github_repo_id)) throw new Error('Conflicting repository IDs');
      const rawID = item.repository_id ?? item.github_repo_id;
      if (!validRepository(item.full_name) || !validRepositoryID(rawID)) throw new Error('Invalid project identity');
      const id = Number(rawID);
      if (!Number.isSafeInteger(id)) throw new Error('Repository ID exceeds exact JSON integer range');
      if (typeof item.summary_zh !== 'string' || !item.summary_zh.trim()) throw new Error('Missing reading summary');
      if (typeof item.source !== 'string' || !item.source.trim()) throw new Error('Missing reading provenance');
      const source = item.source.trim();
      for (const field of ['technical_notes', 'model']) {
        if (item[field] !== undefined && typeof item[field] !== 'string') throw new Error(`Invalid ${field}`);
      }
      for (const field of ['key_points', 'use_cases']) {
        if (item[field] !== undefined && (!Array.isArray(item[field]) || !item[field].every(text => typeof text === 'string'))) throw new Error('Invalid reading list');
      }
      const legacy = source === 'legacy_research';
      let notes = item.technical_notes || '';
      if (legacy) {
        notes += `\n原资料：${item.readme_url || '旧日报研究卡'}${item.readme_sha ? `\nREADME SHA：${item.readme_sha}` : ''}`;
        notes += `\n旧记录更新时间：${item.legacy_record_updated_at || '未记录'}，不是原解读生成时间。`;
        notes += `\n本次导入时间：${importedAt}。原生成时间未记录；本记录的 analyzed_at 为导入时间，不代表重新研究过该项目。`;
      }
      const analyzedAt = legacy ? importedAt : item.analyzed_at;
      if (!validTimestamp(analyzedAt)) throw new Error('Invalid RFC3339 analysis timestamp');
      const entry = {
        full_name: item.full_name, repository_id: id, summary_zh: item.summary_zh.trim(),
        key_points: item.key_points || [], use_cases: item.use_cases || [],
        technical_notes: notes, source, model: (item.model || '').trim(), analyzed_at: analyzedAt,
      };
      const key = item.full_name.toLowerCase();
      const existing = projects.get(key);
      if (existing && String(existing.repository_id) !== String(id)) throw new Error('Conflicting repository IDs');
      // A fresh README reading takes priority over an archived note in this package.
      // The Go importer still preserves any existing non-empty database analysis.
      if (!existing || (existing.source === 'legacy_research' && !legacy)) projects.set(key, entry);
    }
  }
  return { projects: [...projects.values()] };
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const [, , output, ...files] = process.argv;
  if (!output || !files.length) throw new Error('Usage: node scripts/package-readings.mjs OUTPUT.json INPUT.json ...');
  const result = packageReadings(await Promise.all(files.map(async file => JSON.parse(await readFile(file, 'utf8')))));
  await writeFile(output, JSON.stringify(result, null, 2) + '\n', { mode: 0o600 });
  console.log(JSON.stringify({ projects: result.projects.length, output }));
}
