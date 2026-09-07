# Architecture

GitHub Radar is a modular monolith: one Go module, one binary, one SQLite
database, a scheduled collector, and a Web process with public browsing and
protected operator additions.

```text
GitHub Search -----+--> repository registry --> daily GitHub snapshots
legacy import -----+              |                       |
manual watchlist --+              +--> topic mapping      +--> SQLite
Web project add ---+--> initial GitHub snapshot --------------+
Codex analysis JSON -----------------> analysis revision ----+
                                                            |
                                                 CSV / JSON / Web
```

Discovery and snapshotting are deliberately independent. A discovery outage
never prevents the system from recording explicit outcomes for repositories
that are already active. GitHub repository ID is the identity boundary, so a
rename or organization transfer does not split history.

OSS Insight is an optional adapter, disabled by default. GitHub Search covers
new candidates and mature projects, and existing projects continue to receive
daily repository-API snapshots independently of discovery availability.

Project interpretation is a third, explicit workflow. Codex or a human writes
a traceable JSON artifact and imports it through the CLI. The scheduled
collector never invokes a model or reads repository contents. The Web process
displays interpretations; its write capability is limited to operator project
addition and the corresponding initial observation.

The existing Python OSS Insight to Feishu digest remains unchanged and runs at
09:00 Asia/Shanghai. GitHub Radar runs beside it at 09:15. The two systems share
neither code nor a writable database.

## Package boundaries

- `internal/source`: external API and import adapters.
- `internal/domain`: durable business values and states.
- `internal/store`: persistence contracts and SQLite implementation.
- `internal/service`: discovery, snapshot, classification, topic, and watch use cases.
- `internal/app`: CLI command orchestration.
- `internal/web`: HTTP views, protected add-project forms, and embedded assets.

The main package parses commands and hands control to `internal/app`. SQL,
business decisions, and HTTP rendering stay outside `main.go`.
