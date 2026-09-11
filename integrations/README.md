# RepoTempo Skill

The default connection is a local Skill. Sign in with GitHub, open **Agent 访问**
and generate an AK/SK. The same page then supplies one install command containing
that account’s credentials. Run it locally, then start a new agent task and ask
it to use the `repotempo` Skill.

The installer uses POSIX `sh`, `curl`, `openssl`, `tar` and standard system
utilities available on macOS and typical Linux installations. It does not need
Node, Python, npm, a separate MCP server or a model API key. Windows users need a
compatible shell environment such as WSL; this is not a native PowerShell package.
Missing utilities produce an actionable error before installation.

## Installation contract

`GET /integrations/install-skill.sh` and
`GET /integrations/repotempo-skill.tar.gz` return public, credential-free files.
The copied command passes `REPOTEMPO_URL`, `REPOTEMPO_AK` and `REPOTEMPO_SK` as
environment variables to the installer. No credential is added to a download
URL. The installer fetches only the configured origin and rejects redirects.

By default the Skill is installed in `~/.agents/skills/repotempo`. To use another
agent’s supported Skill directory, set `REPOTEMPO_SKILL_DIR` to an absolute path
ending in `/repotempo` when running the install command. This does not edit the
agent’s global configuration, create symlinks or install an MCP plugin.

Credentials are written only to the installed `.credentials` file (permission
0600, parent directory 0700), not the public archive or `SKILL.md`. The Skill tells
the agent to invoke its shell helper, which loads that file without evaluating it
as code. The local install instruction itself contains an SK: treat it as a
secret, do not share it or save it to a repository, and avoid recording it in
terminal history or shared conversation logs. The website shows it only in the
key-creation response. Lost credentials cannot be recovered; create a new pair.

Re-running an install command updates the known files of an installer-managed
RepoTempo Skill and atomically replaces its credential file. Other files are
preserved. An unrelated existing directory or symlink is refused. Revoking the
key on the website immediately disables that installation’s API access; local
removal alone does not revoke the key.

## API access

The helper offers `account`, `search`, `repository` and `watchlist`. Read
[the Skill](repotempo-skill/SKILL.md) for arguments and data interpretation.
Search results supply the numeric ID required by the detail API. All calls are
read-only and account-scoped; imports, following and credential management stay
on the website.

The shell helper implements the existing HMAC protocol: SHA-256 over the full
SK derives the 32-byte signing key; HMAC-SHA256 signs method, exact encoded path,
timestamp, fresh nonce and body hash with newline separators. SK and derived key
never enter process arguments, request URLs or HTTP payloads. Curl configuration
and HMAC intermediate files live in a temporary 0700 directory and are removed
on exit. Redirects are not followed; requests time out after 15 seconds and
responses are limited to 2 MiB. HTTPS is required except on loopback.

## Optional compatibility bundle

The existing [Node MCP / Codex plugin bundle](repotempo-agent/README.md) remains
available for users who explicitly need MCP. It is not required for the default
Skill installation and still requires Node 20+.

## Verification

Run `go test ./integrations`. Portable tests install into a temporary directory
with no Node, Python or jq in `PATH`, then call real Go API handlers backed by
SQLite keys and replay protection. They cover encoded queries, per-user follows,
repository detail, revocation, credentials permissions, managed reinstall,
redirect rejection and unsafe destinations. They do not use production keys.
