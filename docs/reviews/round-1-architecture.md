# Kimi Code review 1: data and discovery architecture

- Date: 2026-08-30
- Kimi Code: 0.38.0
- Session: `session_da6f76ac-8311-4606-bf2c-6627d02bf0fa`
- Scope: domain, five-table schema, SQLite store, discovery configuration,
  GitHub Search, OSS Insight, legacy/manual/CSV inputs, discovery registry, and
  architecture documentation
- Access: read-only repository review; no environment variables or production
  credentials were provided

Kimi independently ran build, vet, and the full Go test suite. They passed at
the reviewed revision.

## Verdict

Gate: **PASS with integration conditions**.

No P0 data-model or discovery-architecture defect was found. The five-table
model was judged sufficient, with unusually strong database constraints for a
small first release. Search paging and recursive partitions retain explicit
incomplete and truncated signals. OSS Insight rolling stars remain isolated
from absolute GitHub stars in types, adapters, columns, and snapshot logic.

## P1 findings

1. The production CLI/orchestrator had not yet connected source adapters,
   registry writes, snapshots, and `job_runs`. This is the next implementation
   stage, not a schema redesign.
2. `SearchIntegrityError` returns valid partial hits together with an integrity
   error. The orchestrator must retain hits, mark the job partial, and persist
   query reports, incomplete flags, and split evidence. It must never silently
   discard the hits or describe them as complete.
3. `config/topics.example.yaml` was referenced by README, defaults, and the
   installer but was missing.
4. CSV candidates did not independently confirm repository ID through GitHub,
   so the CLI must resolve each `full_name` with the expected ID before registry
   insertion.

## P2 findings

1. The migration runner currently relies on idempotent migration SQL and does
   not gate files by `user_version`. This is acceptable for 0001 but must change
   before adding a non-idempotent 0002 migration.
2. Existing-success snapshots were counted as both success and skipped in the
   run report.
3. GitHub 404 responses remain `unreachable` because the public API cannot
   reliably distinguish deletion from an inaccessible private repository.
4. A high retry count could produce impractically long exponential backoff.
5. Discovery documentation named topic partitioning, while code implements
   star, created-date, and pushed-date partitions.

## Requested missing tests

- Search partition max-depth exhaustion.
- Search resource quota reset waiting.
- Repository foreign-key cascade behavior.
- Topic re-parenting update trigger.
- 304 without a successful baseline.
- Service-level same-day failure-to-success repair.
- End-to-end proof that OSS window stars never enter `star_count`.
- Job tracker invalid terminal status and missing-store paths.

## Main-agent disposition

- Production CLI/orchestration: assigned and required before deployment.
- Partial Search contract: required in CLI orchestration and job details.
- Topic example and strict loader: implemented after review.
- CSV ID verification: required in CLI import path.
- Multi-source provenance: fixed so every merged source is persisted.
- Legacy GitHub status: preserved by the registry.
- Snapshot report double-count: fixed.
- Retry count: capped at five in configuration and client validation.
- Documentation partition wording: fixed.
- 304, repair, and OSS/absolute-star service tests: added.
- Remaining database and Search edge tests: retained as the next test gate.

