# Architecture

GitHub Radar is a modular monolith: one Go module, one binary, one SQLite
database, one scheduled writer, and one read-only Web process.

```text
OSS Insight -------+
GitHub Search -----+--> repository registry --> daily GitHub snapshots
legacy import -----+              |                       |
manual watchlist --+              +--> topic mapping      +--> SQLite
Codex analysis JSON -----------------> analysis revision ------+
                                                              |
                                                   CSV / JSON / Web
```

Discovery and snapshotting are deliberately independent. A discovery outage
never prevents the system from recording explicit outcomes for repositories
that are already active. GitHub repository ID is the identity boundary, so a
rename or organization transfer does not split history.

Project interpretation is a third, explicit workflow. Codex or a human writes
a traceable JSON artifact and imports it through the CLI. The scheduled
collector never invokes a model or reads repository contents, and the Web
process remains read-only.

The existing Python OSS Insight to Feishu digest remains unchanged and runs at
09:00 Asia/Shanghai. GitHub Radar runs beside it at 09:15. The two systems share
neither code nor a writable database.

## Package boundaries

- `internal/source`: external API and import adapters.
- `internal/domain`: durable business values and states.
- `internal/store`: persistence contracts and SQLite implementation.
- `internal/service`: discovery, snapshot, topic, and export use cases.
- `internal/app`: CLI command orchestration.
- `internal/web`: read-only HTTP views and embedded assets.

The main package parses commands and hands control to `internal/app`. SQL,
business decisions, and HTTP rendering stay outside `main.go`.
