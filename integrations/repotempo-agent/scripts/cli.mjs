#!/usr/bin/env node
import { ClientError, createClient } from './client.mjs';

const usage = 'Usage: node cli.mjs <repositories|repository|watchlist|me> [JSON arguments]\nExamples:\n  node cli.mjs repositories \'{"q":"agent","period":"7d","size":6}\'\n  node cli.mjs repository \'{"id":123}\'\n  node cli.mjs watchlist\n  node cli.mjs me\nCredentials: REPOTEMPO_URL, REPOTEMPO_AK, REPOTEMPO_SK environment variables.\n';
const commands = { repositories: 'search_repositories', repository: 'get_repository', watchlist: 'list_watchlist', me: 'get_account' };
const [command, rawArgs = '{}', ...extra] = process.argv.slice(2);
if (command === '--help' || command === '-h') {
  process.stdout.write(usage);
} else {
  try {
    if (!Object.hasOwn(commands, command || '') || extra.length) throw new ClientError(usage.trim());
    let args;
    try { args = JSON.parse(rawArgs); } catch { throw new ClientError('Arguments must be valid JSON.'); }
    const result = await createClient().call(commands[command], args);
    process.stdout.write(JSON.stringify(result, null, 2) + '\n');
  } catch (error) {
    process.stderr.write((error instanceof ClientError ? error.message : 'RepoTempo client failed.') + '\n');
    process.exitCode = 1;
  }
}
