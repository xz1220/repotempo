# RepoTempo

RepoTempo is an independent, self-hosted workspace for discovering trending GitHub
projects and following what happens after discovery. It stores daily Star
observations and makes the history readable through charts, project pages,
categories, and a personal watchlist. It is not affiliated with GitHub.

中文界面只有趋势看板和项目库两个主入口；项目库支持每日新入库筛选、分类、我的关注和手动添加关注。
项目入库后持续观测，便于回看它从首次发现到后续增长的过程。

## What you can do

- Scan a dashboard for fastest growth, slow or zero growth, and projects losing
  momentum compared with the preceding period.
- Read projects newly added on a selected date; they automatically enter the
  library for continued observation.
- Paste a public GitHub URL or owner/name to follow it, with an optional category
  and note. The Web form saves its first real Star snapshot immediately.
- Open any project to read what it does, inspect Star history, and view saved
  research interpretations when available.
- Browse by coding agents, research agents, browser/computer agents, workflow
  agents, agent platforms, general agents, or AI infrastructure.
- Export monitoring data as CSV, JSON, or a consistent SQLite backup.

The interface uses a dark sidebar and bright chart/list surfaces. Chinese and
English are supported without a frontend build chain.

## Data sources

GitHub Trending is the primary discovery source. Each daily run reads the
public all-language daily, weekly, and monthly boards, then verifies repository
identity through the GitHub API. Featured projects have no additional Star
threshold and stay monitored after leaving the boards.

GitHub Search supplements Trending. Configurable queries cover
recently created projects with early interest, recently active projects, topic
leaders, and mature benchmarks. The repository API supplies absolute Stars for
every actively monitored repository.

OSS Insight is disabled by default. Its adapter and historical import support
remain available, but GitHub discovery and daily collection work independently
of it. An omitted OSS Insight configuration is also treated as disabled.

Board rank, capture time, displayed period gain, and displayed total Stars are
kept in discovery job details, separately from API-derived daily snapshots.
HTML parsing failures are recorded, never treated as empty successful boards;
other sources and tracked-project snapshots continue. Trending does not provide
a documented public API, so the adapter reads public HTML at a low frequency
and respects access and rate-limit responses.

A repository's first entry into this system is different from its GitHub
creation time. Search ordering by Stars or recent pushes does not measure Star
growth; growth comes from successive real observations.

GitHub Search has a 1,000-result boundary per query. The collector partitions
broad queries, preserves incomplete/truncated-result evidence, and respects
separate Search and Core rate limits. No query set enumerates all of GitHub.

See [GitHub discovery and classification](docs/github-data.md) for the current
profiles, limits, and classification rules.

## Storage and runtime

One Go binary provides collection commands and the Web application. SQLite WAL
stores the registry, observations, categories, job records, and current project
interpretations. Repository identity uses GitHub's permanent ID across renames
and transfers.

Cron runs collection; systemd keeps the Web process available. The supplied
deployment places business data and backups on a dedicated data disk.

See [architecture](docs/architecture.md), [data model](docs/data-model.md), and
[operations](docs/operations.md) for implementation and deployment details.

## Requirements and build

- Go 1.27 or newer.
- A GitHub token for useful daily collection capacity. Public metadata can be
  fetched without authentication, but the unauthenticated limits are much lower.
- Linux for the supplied Cron and systemd deployment files.

```sh
git clone https://github.com/xz1220/repotempo.git
cd repotempo
make ci
make build
```

Cross-compile with `make build-linux`. The deployment build uses
`CGO_ENABLED=0`; no system SQLite C toolchain is required.

For a local Chinese preview, run `bash scripts/dev.sh /path/to/copied-radar.db`.
It builds the current code and serves on `http://127.0.0.1:8878`. Without a
database argument it creates a local database. Restart this command after
editing the embedded templates or styles. A local preview does not start a
scheduled collector; use the deployment schedule for daily observations.

## Configure a local instance

The former name was GitHub Radar. Existing `GITHUB_RADAR_*` environment keys,
`github-radar` commands, and data paths remain supported; renaming the product
does not move or replace an existing database. New builds also provide
`bin/repotempo` and `bin/repotempo-linux-amd64`.

```sh
cp config/discovery.example.yaml discovery.yaml
cp config/topics.example.yaml topics.yaml

export GITHUB_RADAR_DB_PATH="$PWD/github-radar.db"
export GITHUB_RADAR_DISCOVERY_CONFIG="$PWD/discovery.yaml"
export GITHUB_RADAR_TOPICS_CONFIG="$PWD/topics.yaml"
export GITHUB_RADAR_LOCALE=zh-CN
export GITHUB_RADAR_LISTEN_ADDR=127.0.0.1:8787
```

Set `GITHUB_RADAR_GITHUB_TOKEN` through your shell or environment-file workflow.
Keep real tokens outside tracked files, command arguments, screenshots, and
logs. An environment file is not automatically sourced by the binary.

Check configuration, seed the category definitions, and start with a small
discovery profile:

```sh
./bin/github-radar doctor
./bin/github-radar topic list
./bin/github-radar discover --source github-search --profile benchmark-projects
./bin/github-radar snapshot
./bin/github-radar serve
```

Open [the local dashboard](http://127.0.0.1:8787/?lang=zh-CN). To preview real
history from another host, use a consistent SQLite export as a separate local
database and point `GITHUB_RADAR_DB_PATH` at that copy.

Run the complete daily sequence with:

```sh
./bin/github-radar run-daily
```

The sequence performs due discovery, snapshots, exports, and retention.
A discovery failure is recorded and does not cancel snapshots for projects
already in the registry. A same-day rerun skips successful snapshots and can
repair failed ones.

## Browse and add projects

| Page | What it contains |
| --- | --- |
| Trends, `/` | Fixed-cohort charts and fastest, slowest, and slowing growth groups. |
| Project library, `/repositories` | Search, category filters, date/period controls, new-entry filtering, My watchlist, and cursor pagination. |
| Agent categories, `/topics` | Purpose-based categories, supporting tags, and classification coverage. |
| Add project, `/watch/new` | A form for a public repository URL, optional category, and note. |
| Collection history, `/runs` | Run outcomes, source warnings, failures, and completeness. |

Only Trends and Project library appear in primary navigation. Add project is
a compact library-toolbar button, and My watchlist is a library tab at
`/repositories?focus=1`. Daily discoveries use the library's date and ordering
controls, with `new=1` to show only projects first added on the selected date.
Old `/discoveries` links redirect to this filtered library view. Project details use
`/repositories/{github_repository_id}`. Health endpoints are `/healthz` and
`/readyz`.

The default direct loopback server supports the Add project form locally.
For public writes, configure `GITHUB_RADAR_WEB_WRITE_TOKEN` with a separate
24–512-character operator secret and publish through HTTPS. The form requests
that secret; it is distinct from the GitHub API token.

Public browsing requires no token. Forwarded/proxied requests cannot use the
direct-local write allowance. Keep the backend on loopback and configure the
HTTPS proxy to set its forwarding headers; do not expose the database directory.

The Web form validates GitHub identity and atomically saves the project,
initial snapshot, and any selected category. Adding an existing project
preserves existing notes and category decisions. It does not duplicate the
repository or overwrite an already successful same-day snapshot.

CLI watch management remains available:

```sh
./bin/github-radar watch add --repo owner/name --focus --note "benchmark"
./bin/github-radar snapshot
./bin/github-radar watch pause --repo owner/name
./bin/github-radar watch resume --repo owner/name
```

Unlike the Web add flow, CLI `watch add` registers the project; use
`snapshot` or the next daily run to collect its initial observation.

## How to read growth

Choose an endpoint date and a 1, 7, or 30-day period. A project is comparable
only when both exact endpoints have successful observations.

- Fastest growth means the largest positive Star gain.
- Slow growth includes zero and the smallest positive gains.
- Losing momentum means a smaller Star gain than the previous equal-length
  period. It requires three successful observations, including the earlier
  period's starting date.
- Losing Stars means the cumulative count decreased. It is distinct from a
  slower positive gain.
- Missing or failed observations do not become zero growth.

The dashboard attention curve uses the same endpoint-comparable repositories,
normalized to 100 at the start. Missing intermediate observations create gaps.
New library entries do not directly increase this fixed-cohort curve.

Rank changes refer to the monitored sample and selected scope, not all of
GitHub. Last-known Stars show their actual observation date and are not used
in place of missing comparison endpoints.

Dates use the Asia/Shanghai calendar; observation timestamps are stored in UTC.
Real imported history can extend the effective history start. The system
does not fabricate daily observations before collection began.

## Categories

The seven root slugs are `coding-agents`, `research-agents`,
`browser-computer-agents`, `workflow-agents`, `agent-platforms`, `ai-agent`,
and `ai-infrastructure`. Components retain a second-level hierarchy.

A parent includes its direct children. A project can have several categories,
and projects without supported classification remain visible.

Automatic rules use explicit repository descriptions and GitHub topics.
Generic `skills` or `agent` tags alone do not establish an AI Agent product.
Manual additions and manual removals take precedence.

```sh
./bin/github-radar topic list
./bin/github-radar topic assign --repo owner/name --topic memory
./bin/github-radar topic remove --repo owner/name --topic memory

./bin/github-radar --json topic reclassify --dry-run
./bin/github-radar topic reclassify
```

Reclassification uses saved metadata without network requests. Dry-run reports
suggestions and protected decisions without writing. The formal run adds
conservative automatic assignments and retains historical mappings.

## Import existing history

```sh
./bin/github-radar import-legacy --path /path/to/legacy-digest/state.db
./bin/github-radar import-legacy --csv /path/to/history.csv
```

The legacy importer reads repository records and real daily observations
without changing the source database. OSS Insight rolling-window trend values
are not converted into absolute Star history.

A read-only Feishu Base bridge is available for the existing project and daily
record tables:

```sh
scripts/export-feishu-base.sh \
  <base-token> <project-table-id> <daily-table-id> \
  /tmp/github-radar-feishu.csv

./bin/github-radar import-legacy --csv /tmp/github-radar-feishu.csv
```

The bridge uses an authenticated `lark-cli` and resolves permanent repository
IDs through `gh`. The Go service does not receive Feishu credentials.
See [Feishu Base import](docs/feishu-base-import.md).

## Save a project interpretation

An external Codex or human research workflow can prepare this JSON shape:

```json
{
  "summary_zh": "这个项目解决的问题和核心做法。",
  "key_points": ["主要能力一", "主要能力二"],
  "use_cases": ["适用场景"],
  "technical_notes": "需要保留的实现说明。",
  "source": "codex",
  "model": "model-name"
}
```

```sh
./bin/github-radar analysis import \
  --repo owner/name \
  --file config/analysis.example.json
```

Import replaces the current interpretation and increments its revision
counter. Source, model, and analysis time remain visible in the detail page.
Prior interpretation bodies are not retained in this iteration.

SQLite backups include interpretations. Regular monitoring CSV/JSON exports
do not yet include them. The Web application displays analyses but does not
invoke models or generate research.

## Export and deploy

```sh
./bin/github-radar export --format csv
./bin/github-radar export --format json
./bin/github-radar export --format sqlite
```

Use the SQLite export for a consistent backup that includes WAL state.

The supplied Tencent Cloud deployment uses `/opt/github-radar` for the binary,
`/etc/github-radar` for configuration, and a dedicated mounted data directory
for the database, exports, backups, and imports.

Both Cron and systemd check the data-disk mount. The supplied collector schedule
is 09:15 Asia/Shanghai with `flock` to prevent overlapping runs. The original
Feishu digest remains an independent job. See [operations](docs/operations.md)
and the host-specific files in `deploy/tencent2` before deploying.

Existing installations must update their deployed discovery and taxonomy
configuration to adopt the new defaults. Rebuilding the binary alone does not
replace custom configuration files.

## Commands and verification

```text
discover --source ossinsight|github-search|legacy|all [--profile NAME]
snapshot
import-legacy --path PATH
analysis import --repo OWNER/NAME --file PATH
topic list|assign|remove|reclassify
watch add|pause|resume
export --format csv|json|sqlite
doctor
run-daily
serve
```

Commands support human-readable output and `--json`. Collection, topic,
watchlist, and export operations expose applicable dry-run paths.

```sh
make fmt-check
make vet
make test
make race
make build-linux
make security
```

Current acceptance should cover fixed-cohort chart values, slowdown across
three exact dates, missing/stale states, pagination, manual addition, and
Chinese/English desktop and mobile layouts. Mock-API tests and live collection
results should be reported separately. OSS Insight availability is not a
requirement for GitHub-only operation.

The [August 30 acceptance report](docs/acceptance-2026-08-30.md),
[data-disk and Chinese UI report](docs/data-disk-i18n-acceptance-2026-08-30.md),
and [older screenshots](docs/screenshots/) describe earlier versions.
They are historical records, not evidence that the current redesign is
deployed or has passed a new release gate.

## Scope and license

RepoTempo stores public repository metadata and attention observations.
It does not clone repositories, collect user profiles, or infer causality.
Commit/release activity correlation remains a later research module.

See [SECURITY.md](SECURITY.md) for security guidance. Source code is MIT licensed.
