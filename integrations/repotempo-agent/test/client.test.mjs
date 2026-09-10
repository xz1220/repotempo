import assert from 'node:assert/strict';
import { createHash, createHmac, randomBytes } from 'node:crypto';
import { spawn, execFile } from 'node:child_process';
import { once } from 'node:events';
import http from 'node:http';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import test from 'node:test';
import { createClient, loadConfig, tools, validateArguments } from '../scripts/client.mjs';

const run = promisify(execFile);
const mcpPath = fileURLToPath(new URL('../scripts/mcp.mjs', import.meta.url));
const cliPath = fileURLToPath(new URL('../scripts/cli.mjs', import.meta.url));

async function fixture(t, respond) {
  const env = {
    REPOTEMPO_AK: 'rt_ak_' + randomBytes(16).toString('hex'),
    REPOTEMPO_SK: 'rt_sk_' + randomBytes(32).toString('hex'),
  };
  const requests = [];
  const nonces = new Set();
  const errors = [];
  const server = http.createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const body = Buffer.concat(chunks);
    try {
      assert.equal(request.headers['x-repotempo-key'], env.REPOTEMPO_AK);
      const timestamp = request.headers['x-repotempo-timestamp'];
      assert.match(timestamp, /^[0-9]+$/);
      assert.ok(Math.abs(Number(timestamp) - Date.now() / 1000) < 10);
      const nonce = request.headers['x-repotempo-nonce'];
      assert.match(nonce, /^[A-Za-z0-9_-]{16,128}$/);
      assert.equal(nonces.has(nonce), false, 'nonce must be fresh for every request');
      nonces.add(nonce);
      const signingKey = createHash('sha256').update(env.REPOTEMPO_SK).digest();
      const canonical = [request.method, request.url, timestamp, nonce, createHash('sha256').update(body).digest('hex')].join('\n');
      const signature = createHmac('sha256', signingKey).update(canonical).digest('hex');
      assert.equal(request.headers['x-repotempo-signature'], signature);
      assert.equal(request.method, 'GET');
      assert.equal(body.length, 0);
      assert.equal(JSON.stringify(request.headers).includes(env.REPOTEMPO_SK), false);
      requests.push(request.url);
      if (respond) respond(request, response, env);
      else {
        response.setHeader('Content-Type', 'application/json');
        response.end(JSON.stringify({ items: [{ id: 42, full_name: 'example/repo' }], has_more: false, request: request.url }));
      }
    } catch (error) { errors.push(error); response.writeHead(500).end(); }
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  env.REPOTEMPO_URL = 'http://127.0.0.1:' + server.address().port;
  t.after(() => { server.closeAllConnections(); server.close(); assert.deepEqual(errors, []); });
  return { env, requests, nonces };
}

function startMCP(t, env) {
  const child = spawn(process.execPath, [mcpPath], { env: { ...process.env, ...env }, stdio: ['pipe', 'pipe', 'pipe'] });
  const lines = createInterface({ input: child.stdout });
  const messages = [];
  const waiters = new Map();
  let stderr = '';
  child.stderr.setEncoding('utf8').on('data', (value) => { stderr += value; });
  lines.on('line', (line) => {
    const value = JSON.parse(line);
    messages.push(value);
    waiters.get(value.id)?.(value);
  });
  t.after(() => { child.stdin.end(); child.kill(); lines.close(); });
  const receive = (id) => {
    const existing = messages.find((message) => message.id === id);
    if (existing) return Promise.resolve(existing);
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('MCP response timed out')), 5000);
      waiters.set(id, (value) => { clearTimeout(timer); waiters.delete(id); resolve(value); });
    });
  };
  return {
    child, messages, stderr: () => stderr, receive,
    send: (value) => child.stdin.write(JSON.stringify(value) + '\n'),
    request: async (id, method, params) => {
      child.stdin.write(JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n');
      return receive(id);
    },
  };
}

test('signed reads preserve encoded queries, paging and personal watchlist scope with unique nonces', async (t) => {
  const { env, requests, nonces } = await fixture(t);
  const client = createClient(env);
  const search = await client.call('search_repositories', { q: '中文 + & agent', page: 2, size: 6, period: '7d', sort: 'growth_rate', date: '2026-09-10' });
  assert.equal(search.items[0].id, 42);
  const query = new URL(search.request, env.REPOTEMPO_URL).searchParams;
  assert.equal(query.get('q'), '中文 + & agent');
  assert.equal(query.get('view'), 'all');
  assert.equal(query.get('page'), '2');
  assert.equal(query.get('size'), '6');
  await client.call('list_watchlist', { tag: 'ai' });
  await client.call('get_repository', { id: 42, date: '2026-09-10' });
  await client.call('get_account');
  assert.equal(new URL(requests[1], env.REPOTEMPO_URL).searchParams.get('view'), 'focus');
  assert.equal(requests[2], '/api/v1/repositories/42?date=2026-09-10');
  assert.equal(requests[3], '/api/v1/me');
  assert.equal(nonces.size, 4);
});

test('missing credentials, insecure origins and invalid credential shapes fail closed', async (t) => {
  const { env, requests } = await fixture(t);
  for (const name of ['REPOTEMPO_URL', 'REPOTEMPO_AK', 'REPOTEMPO_SK']) assert.throws(() => loadConfig({ ...env, [name]: '' }), new RegExp(name));
  for (const url of ['http://example.com', 'ftp://localhost', 'https://u:p@example.com', 'https://example.com/api', 'https://example.com/?x=1', 'https://example.com/#token']) {
    assert.throws(() => loadConfig({ ...env, REPOTEMPO_URL: url }), /HTTPS origin/);
  }
  assert.doesNotThrow(() => loadConfig({ ...env, REPOTEMPO_URL: 'https://example.com' }));
  assert.throws(() => loadConfig({ ...env, REPOTEMPO_SK: 'not-a-secret' }), /credential format/);
  assert.equal(requests.length, 0);
});

test('tool arguments cannot choose arbitrary URL, route, account, page size or invalid date', async (t) => {
  const { env, requests } = await fixture(t);
  const client = createClient(env);
  for (const args of [{ url: 'https://example.com' }, { user_id: 2 }, { page: 0 }, { page: 10001 }, { size: 1000 }, { sort: 'bogus' }, { q: 'x'.repeat(201) }, { date: '2026-02-30' }, { date: null }, null, []]) {
    await assert.rejects(client.call('search_repositories', args));
  }
  await assert.rejects(client.call('list_watchlist', { view: 'all' }));
  await assert.rejects(client.call('get_repository', { id: '../me' }));
  await assert.rejects(client.call('get_repository', {}));
  assert.doesNotThrow(() => validateArguments('search_repositories', { date: '2024-02-29' }));
  assert.equal(requests.length, 0);
});

test('redirects and remote errors are not followed or echoed', async (t) => {
  const { env, requests } = await fixture(t, (request, response, config) => {
    response.writeHead(302, { Location: 'http://127.0.0.1:1/steal' });
    response.end(config.REPOTEMPO_SK);
  });
  await assert.rejects(createClient(env).call('get_account'), (error) => /redirected/.test(error.message) && !error.message.includes(env.REPOTEMPO_SK));
  assert.equal(requests.length, 1);
});

test('authentication errors are useful without exposing the server body', async (t) => {
  const { env } = await fixture(t, (request, response, config) => { response.writeHead(401); response.end(config.REPOTEMPO_SK); });
  await assert.rejects(createClient(env).call('get_account'), (error) => /authentication failed/.test(error.message) && !error.message.includes(env.REPOTEMPO_SK));
});

test('MCP initializes, lists strict read-only tools and calls the signed API', async (t) => {
  const { env, requests } = await fixture(t);
  const mcp = startMCP(t, env);
  assert.equal((await mcp.request(1, 'server/discover')).error.code, -32601);
  assert.equal((await mcp.request(2, 'tools/list')).error.code, -32000);
  const init = await mcp.request(3, 'initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'test', version: '1' } });
  assert.equal(init.result.protocolVersion, '2025-06-18');
  mcp.send({ jsonrpc: '2.0', method: 'notifications/initialized' });
  assert.deepEqual((await mcp.request(4, 'ping')).result, {});
  const catalog = await mcp.request(5, 'tools/list');
  assert.equal(catalog.result.tools.length, 4);
  assert.ok(catalog.result.tools.every((tool) => tool.annotations.readOnlyHint && tool.inputSchema.additionalProperties === false));
  const result = await mcp.request(6, 'tools/call', { name: 'list_watchlist', arguments: { size: 12 } });
  assert.equal(result.result.structuredContent.items[0].id, 42);
  assert.equal(JSON.parse(result.result.content[0].text).items[0].id, 42);
  assert.match(requests[0], /view=focus/);
  assert.equal((await mcp.request(7, 'tools/call', { name: 'get_repository', arguments: { id: 'oops' } })).error.code, -32602);
  assert.equal(requests.length, 1);
  assert.equal(mcp.stderr(), '');
});

test('MCP tool execution failure uses isError and does not expose credentials', async (t) => {
  const { env } = await fixture(t, (request, response, config) => { response.writeHead(403); response.end(config.REPOTEMPO_SK); });
  const mcp = startMCP(t, env);
  await mcp.request(1, 'initialize', { protocolVersion: '2025-03-26', capabilities: {}, clientInfo: { name: 'test', version: '1' } });
  mcp.send({ jsonrpc: '2.0', method: 'notifications/initialized' });
  const response = await mcp.request(2, 'tools/call', { name: 'get_account' });
  assert.equal(response.result.isError, true);
  assert.equal(JSON.stringify(mcp.messages).includes(env.REPOTEMPO_SK), false);
  assert.equal(mcp.stderr().includes(env.REPOTEMPO_SK), false);
});

test('MCP startup with missing credentials exits nonzero and stdout remains empty', async () => {
  await assert.rejects(run(process.execPath, [mcpPath], { env: { ...process.env, REPOTEMPO_URL: '', REPOTEMPO_AK: '', REPOTEMPO_SK: '' } }), (error) => {
    assert.equal(error.code, 1);
    assert.equal(error.stdout, '');
    assert.match(error.stderr, /REPOTEMPO_URL/);
    return true;
  });
});

test('CLI executes signed requests and rejects unsupported commands', async (t) => {
  const { env } = await fixture(t);
  const output = await run(process.execPath, [cliPath, 'repository', '{"id":42}'], { env: { ...process.env, ...env } });
  assert.equal(JSON.parse(output.stdout).items[0].id, 42);
  assert.equal(output.stderr, '');
  await assert.rejects(run(process.execPath, [cliPath, 'delete'], { env: { ...process.env, ...env } }), (error) => error.code === 1 && error.stdout === '');
});
