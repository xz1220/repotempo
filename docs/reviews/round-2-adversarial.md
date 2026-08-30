# Kimi Code review 2: collector and Web adversarial audit

- Date: 2026-08-30
- Kimi Code: 0.38.0
- Session: `session_c52306bd-8152-4183-b0e2-150fb5bf07cc`
- Scope: collector, imports, store, export, CLI, Web, deployment, and claimed tests
- Access: read-only repository review; no environment variables or production
  credentials were provided

Kimi independently ran `gofmt -l .`, `go vet ./...`, `go test ./...`, and
`go test -race ./...`; all passed at the reviewed revision. The initial Release
Candidate Gate was **FAIL** because nine P1 findings remained.

## P0

None. The audit confirmed that failed observations cannot carry a star count,
successful observations are protected from normal reruns, the database has no
snapshot delete path, retention is scoped by prefix, and deployment uses a new
Cron file rather than editing the existing Python task.

## P1 findings and dispositions

1. **An empty intermediate GitHub Search page could be treated as complete.**
   Fixed by marking the query and result incomplete, returning
   `SearchIntegrityError`, retaining prior hits, and adding a regression test.
2. **Repository ETag was persisted before the star snapshot.** A later 304 could
   reuse an older baseline if the snapshot write failed. Fixed by persisting the
   daily star first and updating ETag/name/status only after it is durable.
3. **One repository-level database error stopped the full snapshot loop.** Fixed
   by recording an explicit repository failure and continuing. Metadata-update
   failures are tracked separately without discarding a successful star row.
4. **Manual topic removal had no durable veto.** Fixed within the five-table
   model by storing a confirmed manual confidence-zero tombstone, excluding it
   from topic queries, and protecting it from future automatic assignments.
5. **Daily config-watchlist replay could undo CLI pause/focus/note changes.**
   Fixed by labeling config entries and treating later loads as discovery
   refreshes, not authoritative control updates.
6. **Default settings pointed directly at example configs and the example
   watchlist.** Fixed by defaulting to non-example paths and leaving the example
   discovery watchlist empty until explicitly configured.
7. **The Feishu bridge assumed field order and ISO dates, and unresolved IDs did
   not change its exit status.** Fixed by field-name mapping, schema checks,
   epoch-millisecond support, code 3 for partial identity resolution, code 1 for
   no usable rows, and a shuffled-field round-trip test. The misleading mapping
   from GitHub daily delta to OSS window stars was removed.
8. **The public runs page rendered raw internal error summaries and path-like
   failure targets.** Fixed with generic summaries and repository-name-only
   targets, verified through a real SQLite Web-adapter test.
9. **Deployment could overwrite live YAML, documented an incompatible subpath
   proxy, and lacked coverage for the latest Web adapter.** Fixed by preserving
   existing configs, using an independent Nginx host, enabling the service, and
   adding store-to-Web mapping coverage. The release binary is rebuilt from the
   clean final revision during the release gate.

## Additional hardening completed

- CSV cells beginning with spreadsheet formula characters are neutralized.
- Search max-depth, Search quota reset, foreign-key cascade, topic re-parenting,
  304-without-baseline, same-day repair, dry-run immutability, config replay,
  manual topic veto, Feishu round-trip, and Web error privacy all have tests.

## Recheck gate

The reviewed revision failed. A follow-up Kimi recheck is required after the
fixes above and before deployment or release. P0 and P1 must both be zero.

