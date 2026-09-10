# RepoTempo agent integration

This bundle connects local AI agents to a RepoTempo account. It supplies a
dependency-free Node.js CLI, a stdio MCP server, a research Skill and a Codex plugin.
It reads the real project library, saved analysis, observed Star history and the
credential owner’s watchlist. It does not select or invoke a language model.

## Configure your account

Use Node.js 20 or newer. Sign in to RepoTempo with GitHub, open API / Agent in the
account menu (`/account/api`), and create a key with the read scopes needed for your work. Save its SK
when first displayed; it cannot be recovered later. Revoke that key if exposed.

Set these variables in the process that starts the agent:

| Variable | Value |
| --- | --- |
| `REPOTEMPO_URL` | Your RepoTempo HTTPS origin, with no path, query or fragment |
| `REPOTEMPO_AK` | The account’s `rt_ak_…` access key |
| `REPOTEMPO_SK` | The account’s `rt_sk_…` secret key |

For a terminal session, Bash can read credentials without placing their values in
shell history. Replace the example origin with your deployment:

```bash
export REPOTEMPO_URL='https://repotempo.example.com'
read -r -p 'RepoTempo AK: ' REPOTEMPO_AK
read -r -s -p 'RepoTempo SK: ' REPOTEMPO_SK
export REPOTEMPO_AK REPOTEMPO_SK
```

Keep secrets in your local secret manager or protected process environment. Do
not paste them into model prompts, commit them to a repository, or put them in MCP
command arguments. Desktop apps launched separately may not inherit a terminal’s
environment; configure the three variables in the environment of the actual MCP
host. Missing values fail startup without sending a request.

## Use the CLI

From this bundle’s directory:

```sh
node scripts/cli.mjs me
node scripts/cli.mjs repositories '{"q":"agent","period":"7d","sort":"delta","size":6}'
node scripts/cli.mjs repository '{"id":123}'
node scripts/cli.mjs watchlist '{"page":1,"size":20}'
```

The repository ID must come from a real search result. JSON output preserves the
server’s pagination, observation dates and data-availability flags. Commands only
read data; they cannot create credentials, import projects or change follows.

## Connect MCP directly

The executable is `node /absolute/path/to/repotempo-agent/scripts/mcp.mjs`. For Codex,
add this table to the chosen host’s `config.toml` and replace the script path:

```toml
[mcp_servers.repotempo]
command = "node"
args = ["/absolute/path/to/repotempo-agent/scripts/mcp.mjs"]
env_vars = ["REPOTEMPO_URL", "REPOTEMPO_AK", "REPOTEMPO_SK"]
startup_timeout_sec = 10
tool_timeout_sec = 30
```

Codex supports environment forwarding through `env_vars`; no secret value belongs
in this table. See [official MCP configuration](https://learn.chatgpt.com/docs/extend/mcp?surface=cli).

Other local MCP hosts can launch the same command with the three environment
variables. Tool names are `search_repositories`, `get_repository`, `list_watchlist`
and `get_account`. The adapter uses the signed REST API, so it works with hosts
that cannot calculate per-request HTTP signatures. A raw `/mcp` URL plus static
headers is insufficient: signatures bind the exact body, timestamp and nonce.

This implementation supports MCP `2025-06-18` and `2025-03-26`, with initialization,
ping, tool discovery and read calls. It uses newline-delimited JSON on stdout,
bounded messages and sanitized errors, following the [stdio transport](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports)
and [lifecycle](https://modelcontextprotocol.io/specification/2025-06-18/basic/lifecycle).
It is not an implementation of the newer 2026 protocol; clients must support the
2025 initialization flow (or its standard fallback). No server-side sampling or
model API key is required.

## Install the Skill or Codex plugin

For just the Skill, copy `skills/repotempo` to the target agent’s supported skill
directory and configure MCP above. Codex discovers personal skills under
`~/.agents/skills/`; create the destination only if it does not already exist.
For example, from the bundle directory:

```sh
mkdir -p ~/.agents/skills
cp -R skills/repotempo ~/.agents/skills/repotempo
```

For Codex, install the full bundle from its included local marketplace instead.
Replace the example path with the directory containing this README:

```sh
codex plugin marketplace add /absolute/path/to/repotempo-agent
codex plugin add repotempo-agent@repotempo-local
```

Then start a new Codex task with the three variables available to the host. The
plugin includes both the Skill and MCP server; avoid enabling a duplicate direct
MCP connection. `.codex-plugin/plugin.json` and `.mcp.json` use the supported
[Codex compatibility layout](https://developers.openai.com/plugins/build/plugins).
The package-local marketplace points to this bundle’s root. Its MCP `cwd: "."`
resolves to the installed plugin directory, so `scripts/mcp.mjs` keeps working
after Codex copies the bundle into its cache. This path handling follows the
[official Codex plugin parser](https://github.com/openai/codex/blob/main/codex-rs/codex-mcp/src/plugin_config.rs).

The marketplace commands are installation instructions, not operations run by the
RepoTempo deployment. This bundle is for local Codex/MCP hosts; it has not been
submitted to the public plugin directory and does not make a local process
available to ChatGPT web.

## Request signature

The client sends `X-RepoTempo-Key`, `X-RepoTempo-Timestamp`, `X-RepoTempo-Nonce` and
`X-RepoTempo-Signature`. SK itself is never sent. The signing key is the **32 raw
bytes** of SHA-256 over the **complete UTF-8 SK string**, including `rt_sk_`; it is
not the hex text of that digest. The signature is lowercase hex HMAC-SHA256 over:

```text
METHOD
PATH_WITH_QUERY
UNIX_TIMESTAMP
NONCE
SHA256_BODY
```

Lines are joined with `\n`, with no trailing newline. The path includes the exact
encoded query transmitted over HTTP; the empty GET body has its own SHA-256
digest. Timestamp is Unix seconds. Each request gets a fresh random nonce; server
clock tolerance is five minutes. HTTPS is required except `localhost`, `127.0.0.1`
and `[::1]`. Redirects are rejected, responses are capped at 2 MiB and requests time
out after 15 seconds. Credentials are never forwarded to a redirect destination.

## Verify changes

```sh
node --test test/*.test.mjs
```

Tests use a loopback fixture and randomly generated test-only credentials. They
verify signatures independently, unique nonces, encoded queries, watchlist scope,
input bounds, rejected redirects, credential-safe errors, CLI requests and MCP
initialization/tool flow. They make no production requests.
