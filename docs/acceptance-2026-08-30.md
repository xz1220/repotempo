# Production acceptance report, 2026-08-30

## Delivered endpoints and source

- Public repository: <https://github.com/xz1220/github-radar>
- First formal release: <https://github.com/xz1220/github-radar/releases/tag/v0.1.0>
- Public dashboard: <https://github-radar.43-160-242-46.sslip.io>
- Business host name: `tencent2`
- Actual local SSH alias: `tecent2`

The existing Python project remained at
`/home/xingzheng/repos/ossinsight-feishu-digest-claude` with a clean worktree.
Its user Cron remained unchanged at 09:00 Asia/Shanghai, and its latest observed
run remained `sync_publish / success`.

## Production layout

- Binary: `/opt/github-radar/github-radar`
- Database: `/var/lib/github-radar/github-radar.db`
- Exports: `/var/lib/github-radar/exports`
- Backups: `/var/lib/github-radar/backups`
- Import evidence: `/var/lib/github-radar/import`
- Environment: `/etc/github-radar/github-radar.env` (`root:github-radar`, 0640)
- Discovery config: `/etc/github-radar/discovery.yaml`
- Topic config: `/etc/github-radar/topics.yaml`
- Cron: `/etc/cron.d/github-radar`, 09:15 Asia/Shanghai with `flock`
- Web: `github-radar-web.service`, enabled and active
- Reverse proxy: standalone Nginx host with a Certbot-managed ECDSA certificate
  valid through 2026-11-28

The Web process bound only to `127.0.0.1:8787`. Public HTTP redirected to HTTPS.
All eight primary routes plus health and readiness returned HTTP 200.

## Imported and discovered data

Legacy SQLite import read all 1,169 repositories rather than only the 415-row
catalog. The combined SQLite and Feishu-CSV import evaluated 4,978 real dated
observations; 2,713 were inserted, 2,264 duplicate successful values were
protected, and one fork observation was explicitly rejected because ordinary
legacy discovery does not admit forks.

The live Feishu bridge paginated both Base tables in batches of 200 and emitted
3,402 CSV rows. Ten deleted or inaccessible repository names were reported as
partial identity failures, not guessed or silently accepted.

OSS Insight's four live windows produced 230 unique candidates, including 48
new repositories. The final capacity-safe GitHub Search run covered:

- `global-high-star`, 20,000-Star floor, weekly
- `topic-popular`, weekly
- `recently-created`, 500-Star floor over 30 days, daily
- `recently-active`, 20,000-Star floor over 90 days, daily
- `benchmark-projects`, owner-scoped, weekly
- `monthly-coverage`, present but explicit opt-in

The final real `run-daily` discovery merged 3,180 candidates and kept the active
panel at 4,012 before one repeated 404 was paused. The sustainable final active
population is 4,011, below the normal 5,000-request/hour Core API ceiling.

## Final data counts and coverage

| Measure | Count |
|---|---:|
| Repositories | 4,023 |
| Active repositories | 4,011 |
| Topics | 10 |
| Repository-topic rows | 1,153 |
| Daily snapshot rows | 6,610 |
| Active repositories with a successful 2026-08-30 snapshot | 4,011 |
| Active repositories missing a 2026-08-30 row | 0 |
| Active panel coverage | 100% |

The initial full-panel run fetched 1,122 new successes, reused 120 real same-day
successes, and recorded five explicit 404 failures. The capacity-safe daily run
then fetched 2,769 additional successes and recorded one new 404. Each 404 was
retried once, retained as a failed row with `star_count=NULL`, marked
`unreachable`, and paused without deleting history:

- `amirappleidfd-stack/spider--panel`
- `Contrary7nit/SteamDaddy`
- `DuarteSantos8/openGym`
- `NeedInvestor/Imei-Repair-tool`
- `Nervercc/gpt_nerver`
- `yuhuangerdi/InduSecAgent`

## Export reconciliation

Final artifacts:

- CSV directory:
  `/var/lib/github-radar/exports/github-radar-20260830T113413Z-csv`
- JSON:
  `/var/lib/github-radar/exports/github-radar-20260830T113414Z.json`
- SQLite:
  `/var/lib/github-radar/backups/github-radar-20260830T113415Z.db`

Repository, snapshot, Topic, and assignment counts matched exactly across the
live database, five CSV files, JSON, and SQLite backup: `4,023 / 6,610 / 10 /
1,153`. CSV failure stars were empty rather than zero.

An early live capacity experiment was fully recoverable: the oversized database
and WAL evidence were moved under `/var/lib/github-radar/backups`, and the live
database was atomically restored from the reconciled 1,253-project export before
the sustainable thresholds were rerun. No production history was silently
deleted.

## Automated and independent verification

- `gofmt -l .`: clean
- `go vet ./...`: passed
- `go test ./...`: passed
- `go test -race ./...`: passed
- linux/amd64 static build: passed
- `govulncheck v1.7.0`: no vulnerabilities found
- TruffleHog current tree and full history: zero real secrets
- GitHub Actions on the public repository: passed
- Kimi Code architecture review: P0=0, Gate PASS with conditions, all addressed
- Kimi Code adversarial review and recheck: P0=0, P1=0, Gate PASS
- Kimi Code fresh-clone release audit: P0=0, P1=0, Gate PASS

Review evidence is in `docs/reviews/`. Browser evidence is in
`docs/screenshots/` and covers desktop, mobile navigation, repository filtering,
details, Star and OSS rank charts, and task runs. Topics, Discoveries, health,
and readiness also opened in the isolated browser.

## Known limits and follow-ups

1. GitHub does not expose exact daily Star totals from before monitoring began.
   Pages show the effective history start, and missing dates remain missing.
2. A delta after a gap spans the two real successful observations. It is not
   interpolated into synthetic daily values.
3. GitHub 404 cannot reliably distinguish deletion from an inaccessible private
   repository; repeated 404s are recorded as `not_found_or_private`, marked
   unreachable, and paused for manual review.
4. Root-disk utilization was 86.6-86.7% during acceptance. Doctor and daily job
   details expose the warning; retention is configured for 30 days. Disk cleanup
   or expansion remains recommended.
5. The deployed token is the host's existing authenticated GitHub OAuth token,
   stored outside the repository with mode 0640. A dedicated fine-grained,
   read-only token is a recommended hardening step.
6. Activity, commit, release, README, causality, scoring, and investment advice
   remain deliberately outside v0.1.x.

