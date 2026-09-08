// Prepare public GitHub evidence for the offline reading workflow. No database writes.
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';

const exec = promisify(execFile);
export const validRepository = name => typeof name === 'string' && /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?\/[A-Za-z0-9_.-]{1,100}$/.test(name) && !['.', '..'].includes(name.split('/')[1]);
export const validRepositoryID = id => (typeof id === 'number' && Number.isSafeInteger(id) && id > 0) || (typeof id === 'string' && /^[1-9][0-9]{0,18}$/.test(id));

export function readingEvidence(project, readme, fetchedAt) {
  if (!validRepository(project.full_name)) throw new Error('Invalid repository name');
  if (!validRepositoryID(project.id)) throw new Error('Invalid repository ID');
  const content = readme?.encoding === 'base64' ? Buffer.from(readme.content ?? '', 'base64').toString('utf8').trim() : '';
  // Keep the opening explanation and final limitations, with truncation recorded.
  const runes = Array.from(content);
  const truncated = runes.length > 16000;
  const text = truncated ? `${runes.slice(0, 12000).join('')}\n[中间内容省略]\n${runes.slice(-4000).join('')}` : content;
  return {
    id: project.id, full_name: project.full_name, description: project.description || '',
    first_seen_at: project.first_seen_at, fetched_at: fetchedAt,
    evidence_url: `https://github.com/${project.full_name}${text ? '#readme' : ''}`,
    evidence_kind: text ? (truncated ? 'readme_excerpt' : 'readme') : 'github_description',
    readme_sha: readme?.sha || '', readme_text: text,
  };
}

export async function prepare(input, output, limit = Infinity) {
  const manifest = JSON.parse(await readFile(input, 'utf8'));
  const projects = manifest.projects.slice(0, limit);
  await mkdir(join(output, 'evidence'), { recursive: true });
  const results = new Array(projects.length);
  let next = 0;
  await Promise.all(Array.from({ length: 3 }, async () => {
    for (;;) {
      const index = next++;
      if (index >= projects.length) return;
      const project = projects[index];
      if (!validRepository(project.full_name)) throw new Error('Invalid repository name in queue');
      if (!validRepositoryID(project.id)) throw new Error('Invalid repository ID in queue');
      const target = join(output, 'evidence', `${project.id}.json`);
      try {
        const cached = JSON.parse(await readFile(target, 'utf8'));
        if (cached.id === project.id && cached.full_name === project.full_name && cached.fetched_at?.slice(0, 10) === new Date().toISOString().slice(0, 10)) {
          results[index] = cached;
          continue;
        }
      } catch (error) {
        if (error.code !== 'ENOENT' && !(error instanceof SyntaxError)) throw error;
      }
      let readme;
      let failure = '';
      try {
        const { stdout } = await exec('gh', ['api', `repos/${project.full_name}/readme`], { timeout: 30000, maxBuffer: 5 * 1024 * 1024 });
        readme = JSON.parse(stdout);
      } catch (error) {
        // Never include command stderr (which may include environment details) in evidence.
        failure = `README fetch failed (${error.killed ? 'timeout' : 'GitHub/API response'}); only repository description available`;
      }
      const evidence = readingEvidence(project, readme, new Date().toISOString());
      if (failure) evidence.fetch_note = failure;
      await writeFile(target, JSON.stringify(evidence, null, 2) + '\n', { mode: 0o600 });
      results[index] = evidence;
      console.log(`${index + 1}/${projects.length} ${project.full_name} ${evidence.evidence_kind}`);
    }
  }));
  const usable = results.filter(item => item.readme_text || item.description.trim());
  const unavailable = results.filter(item => !item.readme_text && !item.description.trim()).map(item => item.full_name);
  const batches = [];
  for (let offset = 0; offset < usable.length; offset += 12) batches.push(usable.slice(offset, offset + 12));
  const result = { date: manifest.date, batches, unavailable };
  await writeFile(join(output, 'reading-input.json'), JSON.stringify(result, null, 2) + '\n', { mode: 0o600 });
  console.log(JSON.stringify({ prepared: usable.length, batches: batches.length, unavailable }));
  return result;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const [, , input, output, rawLimit] = process.argv;
  if (!input || !output) throw new Error('Usage: node scripts/prepare-readings.mjs QUEUE.json OUTPUT_DIR [LIMIT]');
  const limit = rawLimit ? Number(rawLimit) : Infinity;
  if (rawLimit && (!Number.isSafeInteger(limit) || limit < 1)) throw new Error('LIMIT must be a positive integer');
  await prepare(resolve(input), resolve(output), limit);
}
