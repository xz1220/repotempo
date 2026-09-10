# RepoTempo

GitHub accounts, private watchlists and Agent access are now available. See
[account setup](docs/github-login.md) and the
[MCP / Skill / Plugin bundle](integrations/repotempo-agent/README.md).
After signing in, open API access from the sidebar account menu to create
a read-only credential and download the integration bundle.

RepoTempo is an independent, self-hosted workspace for discovering trending GitHub
projects and following what happens after discovery. It stores daily Star
observations and makes the history readable through charts, project pages,
searchable tags, and a personal watchlist. It is not affiliated with GitHub.

中文界面只有趋势看板和 GitHub 项目两个主入口；项目页支持每日新入库、标签搜索、我的关注和手动添加关注。
项目入库后持续观测，便于回看它从首次发现到后续增长的过程。

## What you can do

- Scan a fastest-growth Top 10 and up to six projects with the largest momentum
  declines compared with the preceding period.
- Start the library with projects newly added today in Shanghai, then switch
  to All projects or My watchlist. An empty today remains visibly empty.
- Paste a public GitHub URL or owner/name to follow it, with an optional category
  and note. The Web form saves its first real Star snapshot immediately.
- Read a single-column project feed, 20 cards per page, with original
  descriptions and saved AI or human briefs showing their source and date.
- Explicitly mark projects read or unread in this browser. These local marks
  are separate from saved research coverage and do not sync across devices.
- Open a detail for the full saved analysis and individual Star-history chart,
  or follow the card's separate GitHub link.
- Search or click exact tags such as 投资, agent, skills, or saas. Cards show
  eight tags initially and retain every additional label in an expandable section.
- Use purpose-based categories on the trend dashboard and classification pages.
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

Schema version 5 adds `github_topics_json` and `research_tags_json` to the
existing `repositories` table. No tag table is added, and category definitions
and assignments remain separate from these raw labels.

Cron runs collection; systemd keeps the Web process available. The supplied
deployment places business data and backups on a dedicated data disk.

See [architecture](docs/architecture.md), [data model](docs/data-model.md), and
[operations](docs/operations.md) for implementation and deployment details.

Optional [GitHub administrator login](docs/github-login.md) protects imports,
watchlist queries, and collection history while leaving project browsing public.
It uses a GitHub-ID allowlist, not open registration or separate user workspaces.
OAuth credentials must be configured before this login can be enabled.

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
| Trends, `/` | Fastest growth Top 10 and up to six largest momentum declines. |
| GitHub projects, `/repositories` | The user reads a 20-card feed with tag search, ordering, observation date, growth comparison, and My watchlist. |
| Agent categories, `/topics` | Purpose-based categories, supporting tags, and classification coverage. |
| Add project, `/watch/new` | A form for a public repository URL, optional category, and note. |
| Collection history, `/runs` | Run outcomes, source warnings, failures, and completeness. |

Only Trends and GitHub projects appear in primary navigation. The library's
page heading remains Project library (项目库).

Add project is a compact library-toolbar button, and My watchlist is a library tab at
`/repositories?view=focus`. An unscoped `/repositories` visit defaults to
Today’s additions using the real Shanghai date, `sort=stars`, and `period=1d`.

Choose `view=all` for the full library or `view=daily` for additions. `new=1`
limits the selected date to new entries, while explicit dates and ordering
remain available. Today is never replaced with the last date that has data.

Old `/discoveries` links redirect to this filtered library view. Project details use
`/repositories/{github_repository_id}`. Health endpoints are `/healthz` and
`/readyz`.

Each library card includes the original description, any saved AI or human
brief with source and date, Stars, period gain, growth rate, comparable-sample
ranks, clickable tags, entry date, and detail/GitHub actions. Missing explanations are marked
as not reviewed. Existing briefs are loaded in one database batch per page;
browsing does not request a new model-generated summary.

The read/unread button saves one localStorage value per permanent repository
ID. It changes only after an explicit click. Page opening, scrolling, and
brief availability do not automatically mark a project read.

Reading marks belong to the current browser profile and origin (scheme, host,
and port). They do not cross devices or site addresses, and they are not a
server-side filter for finding every unread project in the database.

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

Choose an observation date and Compare growth (增长对比). The choices compare
against 1, 7, or 30 days earlier (与 1/7/30 天前比), using the observation date
as the endpoint. Both exact dates must have successful observations.

- Fastest growth means the largest positive Star gain.
- Losing momentum means a smaller Star gain than the previous equal-length
  period. It requires three successful observations, including the earlier
  period's starting date.
- Losing Stars means the cumulative count decreased. It is distinct from a
  slower positive gain.
- Missing or failed observations do not become zero growth.

The dashboard presents only the two project rankings. Individual Star-history
charts remain on project details, where missing dates stay visible as gaps.

Rank changes refer to the monitored sample and selected scope, not all of
GitHub. Last-known Stars show their actual observation date and are not used
in place of missing comparison endpoints.

Dates use the Asia/Shanghai calendar; observation timestamps are stored in UTC.
Real imported history can extend the effective history start. The system
does not fabricate daily observations before collection began.

## Project tags

The library has one searchable tag field, plus ordering, observation date, and
growth comparison. It has no hierarchical category dropdown, discovery-source
dropdown, or category-tab strip.

Tag suggestions include native GitHub topics, archived research labels, and
effective category names or slugs. Matching is exact after trimming and case
normalization; Chinese and unfamiliar upstream labels are preserved.

Suggestions cover all projects entered by the observation date, even when the
current view shows only today's additions or a narrow search result. A tag
filter applies before sample ranking and pagination.

Cards preview eight deduplicated labels and expose the rest through an
expandable section. Card and detail tags open the matching library filter.
Tag changes reset the cursor while retaining the other selected filters.

Tags represent current saved attributes. Selecting an earlier observation
date changes the repository-entry scope and Star history; it does not recover
the labels that a project had on that date.

Existing `topic` and `source` URLs still work. Their restrictions appear as
clearable active-filter badges and remain in hidden form fields until cleared.
The old selectors are not shown.

Native topics use `NULL` when not yet collected and `[]` for a known empty
list. A 200 response containing topics updates them; a 304 preserves them.
Unknown topics disable conditional metadata requests so a full response can fill them.

API refreshes do not overwrite archived research tags. Raw labels are not
inserted into the category tables; the two systems have different purposes.

## Agent categories

The seven root slugs are `coding-agents`, `research-agents`,
`browser-computer-agents`, `workflow-agents`, `agent-platforms`, `ai-agent`,
and `ai-infrastructure`. Components retain a second-level hierarchy for the
dashboard and category-rule pages, independently of raw project tags.

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

### Import saved project tags

Use `tags import-batch` for already registered projects. Supply the permanent
repository ID, matching full name, and the tag fields available in the source.

```json
{
  "projects": [
    {
      "repository_id": 123,
      "full_name": "owner/name",
      "github_topics": ["agent", "skills"],
      "research_tags": ["投资"]
    }
  ]
}
```

```sh
./bin/repotempo tags import-batch --file /path/to/tags.json
```

The transaction fills only unknown native-topic lists, including a supplied
empty array. It preserves known native lists and merges research tags without
dropping existing labels. Omit a field when that source did not provide it.

Filling an unknown native list clears that repository's ETag so later metadata
collection can fetch the full current response. Research-only additions do not
change a known native list or its ETag. No GitHub or model call runs during this import.

Duplicate or mismatched identities, unknown repositories, and invalid tag
values fail the transaction without partial updates. This operation adds no
table and does not change Star snapshots or category assignments.

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
counter. A card shows its saved brief, source, and date; the detail shows the
complete saved analysis and revision metadata. Prior bodies are not retained.

SQLite backups include interpretations. Regular monitoring CSV/JSON exports
do not yet include them. The Web application batch-loads existing card briefs
and displays complete analyses; it does not invoke external models or add an
on-demand LLM pipeline.

Brief generation runs separately as explicit background work. Existing
historical research can be reused with its provenance; unknown authors,
models, or original generation dates must remain unknown.

Neither full-library coverage nor automatic daily brief generation is implied
by these tools.

### Fill missing summaries in a batch

`analysis import-batch` accepts a strict JSON object containing `projects`.
Each item requires `full_name`, `summary_zh`, and `source`.

Optional fields are `repository_id`, `key_points`, `use_cases`,
`technical_notes`, `model`, and an RFC3339 `analyzed_at`.

```json
{
  "projects": [
    {
      "full_name": "owner/name",
      "summary_zh": "根据已阅读资料整理的项目说明。",
      "key_points": [],
      "use_cases": [],
      "source": "legacy_research",
      "model": "",
      "technical_notes": "保留实际资料来源；原始生成模型与时间未留存。"
    }
  ]
}
```

```sh
./bin/github-radar analysis import-batch --file /path/to/analyses.json
```

The batch contains 1–5,000 already registered projects and runs in one
transaction. It fills only missing or empty summaries, skips existing nonempty
ones, and creates no new table.

When `analyzed_at` is omitted, the stored timestamp is the import time. For
reused notes, record that distinction and any unknown original generation
date in `technical_notes`.

Invalid payloads, duplicate identities, unknown repositories, or mismatched
repository IDs fail the batch without partial writes. Unlike single-project
`analysis import`, this command does not replace an existing summary.

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
discover --source github-trending|github-search|ossinsight|legacy|all [--profile NAME]
snapshot
import-legacy --path PATH
analysis import --repo OWNER/NAME --file PATH
analysis import-batch --file PATH
tags import-batch --file PATH
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
make test-js
make race
make build-linux
make security
```

`make test-js` uses Node.js 24 and its built-in test runner, with no npm
dependencies. Override the executable with `make test-js NODE=/path/to/node`
when needed. Production serving and collection still use the Go binary only.

Current acceptance should cover the Top 10/six-row dashboard, slowdown across
three exact dates, true empty-today views, 20-card pagination, explicit local
read marks, saved-brief source/date and missing states, atomic fill-only batch
imports, exact tag filtering and complete tag expansion, preserved research
labels, 200/304 tag refresh behavior, individual history charts, manual addition, and Chinese/English
desktop and mobile layouts. Mock-API tests and live collection
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
