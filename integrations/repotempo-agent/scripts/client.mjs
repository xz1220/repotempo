import { createHash, createHmac, randomBytes } from 'node:crypto';
import http from 'node:http';
import https from 'node:https';

export class ClientError extends Error {}

const text = (maxLength) => ({ type: 'string', minLength: 1, maxLength });
const choice = (...values) => ({ type: 'string', enum: values });
const date = { type: 'string', pattern: '^\\d{4}-\\d{2}-\\d{2}$', description: 'Observation date in Asia/Shanghai, YYYY-MM-DD. Omit for today.' };
const properties = {
  q: text(200), tag: text(80), date,
  period: choice('1d', '7d', '30d'),
  sort: choice('stars', 'delta', 'rank_change', 'growth_rate', 'low_growth', 'slowdown', 'newest', 'name', 'velocity'),
  view: choice('all', 'daily', 'focus'),
  page: { type: 'integer', minimum: 1, maximum: 10000 },
  size: { type: 'integer', enum: [6, 12, 20] },
};
const object = (props, required = []) => ({ type: 'object', properties: props, required, additionalProperties: false });
const annotations = { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false };
export const tools = [
  { name: 'search_repositories', description: 'Search saved GitHub projects and observed Star growth. Returns one page, with server dates and pagination. Defaults to all projects.', inputSchema: object(properties), annotations },
  { name: 'get_repository', description: 'Read one saved project, its interpretation and observed Star history by numeric RepoTempo repository ID.', inputSchema: object({ id: { type: 'integer', minimum: 1, maximum: Number.MAX_SAFE_INTEGER }, date }, ['id']), annotations },
  { name: 'list_watchlist', description: 'Read the current credential owner’s personal watchlist. Returns one page; never another account’s watchlist.', inputSchema: object(Object.fromEntries(Object.entries(properties).filter(([key]) => key !== 'view'))), annotations },
  { name: 'get_account', description: 'Read the account associated with the configured RepoTempo credential.', inputSchema: object({}), annotations },
];

export function validateArguments(name, args = {}) {
  const tool = tools.find((item) => item.name === name);
  if (!tool) throw new ClientError('Unknown RepoTempo tool.');
  if (!args || typeof args !== 'object' || Array.isArray(args)) throw new ClientError('Arguments must be an object.');
  const schema = tool.inputSchema;
  if (Object.keys(args).some((key) => !Object.hasOwn(schema.properties, key))) throw new ClientError('Unsupported argument.');
  for (const field of schema.required) {
    if (!Object.hasOwn(args, field)) throw new ClientError('Missing required argument: ' + field + '.');
  }
  for (const [key, value] of Object.entries(args)) {
    const rule = schema.properties[key];
    let valid = rule.type === 'integer' ? Number.isSafeInteger(value) : typeof value === rule.type;
    if (rule.enum) valid &&= rule.enum.includes(value);
    if (rule.minimum !== undefined) valid &&= value >= rule.minimum;
    if (rule.maximum !== undefined) valid &&= value <= rule.maximum;
    if (rule.minLength !== undefined) valid &&= value.length >= rule.minLength;
    if (rule.maxLength !== undefined) valid &&= value.length <= rule.maxLength;
    if (rule.pattern) valid &&= new RegExp(rule.pattern).test(value);
    if (key === 'date') {
      const parsed = new Date(value + 'T00:00:00Z');
      valid &&= !Number.isNaN(parsed.valueOf()) && parsed.toISOString().slice(0, 10) === value;
    }
    if (!valid) throw new ClientError('Invalid argument: ' + key + '.');
  }
  return args;
}

export function loadConfig(env = process.env) {
  for (const name of ['REPOTEMPO_URL', 'REPOTEMPO_AK', 'REPOTEMPO_SK']) {
    if (typeof env[name] !== 'string' || !env[name].trim()) throw new ClientError('Set ' + name + ' in the agent process environment.');
  }
  let origin;
  try { origin = new URL(env.REPOTEMPO_URL); } catch { throw new ClientError('REPOTEMPO_URL must be an HTTPS origin.'); }
  const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(origin.hostname);
  if ((origin.protocol !== 'https:' && !(origin.protocol === 'http:' && loopback)) || origin.username || origin.password || origin.search || origin.hash || origin.pathname !== '/') {
    throw new ClientError('REPOTEMPO_URL must be an HTTPS origin without credentials, path, query or fragment (HTTP is allowed only on loopback).');
  }
  if (!/^rt_ak_[a-f0-9]{32}$/.test(env.REPOTEMPO_AK) || !/^rt_sk_[a-f0-9]{64}$/.test(env.REPOTEMPO_SK)) {
    throw new ClientError('Invalid RepoTempo credential format; use the AK and SK from your account settings.');
  }
  return { origin, accessKey: env.REPOTEMPO_AK, signingKey: createHash('sha256').update(env.REPOTEMPO_SK, 'utf8').digest() };
}

export function signRequest(config, method, requestPath, body = '', timestamp = String(Math.floor(Date.now() / 1000)), nonce = randomBytes(24).toString('base64url')) {
  const bodyHash = createHash('sha256').update(body).digest('hex');
  const message = [method, requestPath, timestamp, nonce, bodyHash].join('\n');
  return {
    'X-RepoTempo-Key': config.accessKey,
    'X-RepoTempo-Timestamp': timestamp,
    'X-RepoTempo-Nonce': nonce,
    'X-RepoTempo-Signature': createHmac('sha256', config.signingKey).update(message).digest('hex'),
  };
}

// Deliberately limited to the three read endpoints: a tool cannot choose an origin,
// redirect destination, arbitrary path, HTTP method, credential, or account owner.
function get(config, path, args, signal) {
  const url = new URL(path, config.origin);
  for (const [key, value] of Object.entries(args)) url.searchParams.set(key, String(value));
  const requestPath = url.pathname + url.search;
  const transport = url.protocol === 'https:' ? https : http;
  return new Promise((resolve, reject) => {
    const request = transport.request(url, {
      method: 'GET', signal,
      headers: { Accept: 'application/json', ...signRequest(config, 'GET', requestPath) },
    }, (response) => {
      const fail = (message) => { response.resume(); reject(new ClientError(message)); };
      const status = response.statusCode || 0;
      if (status >= 300 && status < 400) return fail('RepoTempo redirected the request; set REPOTEMPO_URL to the final HTTPS origin.');
      if (status === 401 || status === 403) return fail('RepoTempo authentication failed. Check the credential, expiry/revocation and system clock.');
      if (status === 429) return fail('RepoTempo rate limit reached. Wait before retrying.');
      if (status === 404) return fail('RepoTempo project or API endpoint was not found.');
      if (status < 200 || status >= 300) return fail('RepoTempo request failed (HTTP ' + status + ').');
      if (!String(response.headers['content-type'] || '').toLowerCase().startsWith('application/json')) return fail('RepoTempo returned an unexpected content type.');
      const chunks = [];
      let bytes = 0;
      response.on('data', (chunk) => {
        bytes += chunk.length;
        if (bytes > 2 * 1024 * 1024) {
          reject(new ClientError('RepoTempo response exceeds the 2 MiB limit.'));
          response.destroy();
        } else chunks.push(chunk);
      });
      response.on('error', () => reject(new ClientError('RepoTempo response was interrupted.')));
      response.on('end', () => {
        try { resolve(JSON.parse(Buffer.concat(chunks).toString('utf8'))); }
        catch { reject(new ClientError('RepoTempo returned invalid JSON.')); }
      });
    });
    const timer = setTimeout(() => request.destroy(new Error('timeout')), 15000);
    request.on('close', () => clearTimeout(timer));
    request.on('error', () => reject(new ClientError(signal?.aborted ? 'RepoTempo request cancelled.' : 'Unable to reach RepoTempo within 15 seconds; check the endpoint and network.')));
    request.end();
  });
}

export function createClient(env = process.env) {
  const config = loadConfig(env);
  return {
    async call(name, args = {}, signal) {
      validateArguments(name, args);
      if (name === 'get_account') return get(config, '/api/v1/me', {}, signal);
      if (name === 'get_repository') {
        const { id, ...query } = args;
        return get(config, '/api/v1/repositories/' + id, query, signal);
      }
      const query = { view: 'all', page: 1, size: 20, ...args };
      if (name === 'list_watchlist') query.view = 'focus';
      return get(config, '/api/v1/repositories', query, signal);
    },
  };
}
