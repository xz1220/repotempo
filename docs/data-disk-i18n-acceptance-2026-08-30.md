# Data-disk and Chinese dashboard acceptance — 2026-08-30

## Result

The Tencent Cloud deployment now keeps all active GitHub Radar business data on
the dedicated `/dev/vdb` data disk and serves the dashboard in Simplified
Chinese by default. English remains available from the page header. The public
endpoint is:

- <https://github-radar.43-160-242-46.sslip.io>

Application and deployment revision: `ce170b98ba6fdff133856e4b31a68c0dc17dda1d`.
The installed Linux binary SHA-256 is
`719276aaa50624682d51db152bd5845e06b5538ced744d85d2062e71c9819d99`.

## Storage cutover

| Data | Former path | Active path | Device |
| --- | --- | --- | --- |
| SQLite | `/var/lib/github-radar/github-radar.db` | `/home/xingzheng/data/github-radar/github-radar.db` | `/dev/vdb` |
| CSV / JSON / SQLite exports | `/var/lib/github-radar/exports` | `/home/xingzheng/data/github-radar/exports` | `/dev/vdb` |
| Scheduled backups | `/var/lib/github-radar/backups` | `/home/xingzheng/data/github-radar/backups` | `/dev/vdb` |
| Import evidence | `/var/lib/github-radar/import` | `/home/xingzheng/data/github-radar/import` | `/dev/vdb` |
| Daily lock | `/var/lib/github-radar/daily.lock` | `/home/xingzheng/data/github-radar/daily.lock` | `/dev/vdb` |

At acceptance time `/dev/vdb` was an ext4 filesystem with 133 GiB available
and 33% reported use. The former active directory no longer exists on the
system disk.

The Web service runs as `github-radar`, with the data root at `0750` and the
live database at `0640`. Minimal execute-only ACLs allow the service account to
traverse the private home path. systemd uses `ProtectHome=tmpfs`, an explicit
bind for the data root, `ReadWritePaths`, `RequiresMountsFor`, and a real
mount-point condition.

## Database equivalence

Immediately before cutover, both the source and destination returned:

```text
repositories          4,023
daily_snapshots       6,610
topics                   10
repository_topics     1,153
job_runs                 16
latest snapshot date  2026-08-30
snapshot star sum     131,314,431
schema user_version              1
integrity_check                  ok
```

The source database contained 161 pre-existing `repository_topics` foreign-key
findings. The source, system-disk emergency backup, and data-disk copy produced
the same ordered output with SHA-256
`fccbdbaa60e2cd558826b484bfcd72864c3ddb161f345b21243cdd738da0276d`.
The migration preserved this condition without deleting records or creating a
new discrepancy. Repairing those historical assignments is a separate data
maintenance task.

The first migration attempt required an empty foreign-key report and therefore
rolled back automatically. The original service, env, Cron, and database were
restored before the comparison policy was corrected to require exact source and
destination equivalence.

## Chinese and English dashboard

- `GITHUB_RADAR_LOCALE=zh-CN` is active in production.
- Default responses contain `Content-Language: zh-CN` and
  `<html lang="zh-CN">`.
- `?lang=en` renders the English catalog and `<html lang="en">`.
- The header language switch persists the choice in an HttpOnly, SameSite
  cookie and preserves the current route and query filters.
- Navigation, filters, status and source labels, empty states, errors, chart
  guidance, warnings, and ARIA labels are localized.
- Browser acceptance covered the desktop layout, a 390 × 844 mobile viewport,
  the collapsed mobile menu, language switching, cookie persistence, and the
  repository filters page against a production database backup.
- The production endpoint was checked separately through HTTPS for Chinese and
  English content, health, readiness, CSP, content type, and anti-framing
  headers.

## Collection and export acceptance

The installed Cron wrapper was executed manually after cutover. Run
`dc7f1908-b9b7-4674-8981-32ade253ccc3` completed with status `success`:

- 4,011 active repositories were evaluated.
- All 4,011 were correctly skipped because a successful same-day snapshot
  already existed; there were no failures or duplicate observations.
- Discovery run `8fb5918a-f8f0-42ab-9527-fb3337806040` completed 230 / 230.
- Three scheduled outputs were written on the data disk:
  - `exports/github-radar-20260830T130758Z-csv`
  - `exports/github-radar-20260830T130759Z.json`
  - `backups/github-radar-20260830T130800Z.db`

A separate export acceptance produced CSV, JSON, and SQLite artifacts. Their
repository, snapshot, topic, and assignment counts matched the live database;
the SQLite export returned `integrity_check=ok`.

The existing Feishu digest remains unchanged at 09:00 Asia/Shanghai. GitHub
Radar remains isolated at 09:15 through `/etc/cron.d/github-radar` and its own
data-disk lock.

## Failure and quality gates

Passed checks:

- `make ci`: formatting, vet, unit tests, race tests, and static Linux build.
- `govulncheck`: no known vulnerabilities found.
- `shellcheck` and `sh -n` for all Tencent deployment scripts.
- `systemd-analyze verify` plus a real `ProtectHome=tmpfs` bind-path write probe.
- Independent multi-pass review of migration, rollback, lock, mount, and locale
  behavior; no remaining P0 or P1 findings.
- Missing-mount probe: the Cron wrapper logged the missing data disk and exited
  before opening SQLite.
- Escaping-path probe: a normalized escape-shaped DB path was rejected because
  production paths must equal the three fixed data-disk locations.
- `doctor`: token and configs readable, destination writable, disk usage check
  passed against the data disk.
- `/healthz` and `/readyz`: successful after migration, binary upgrade, source
  archival, and the live daily run.

## Rollback evidence

- Compact system-disk emergency database:
  `/var/backups/github-radar/pre-data-disk-20260830T125818Z.db` (14,082,048
  bytes).
- Full former runtime directory on the data disk:
  `/home/xingzheng/data/github-radar/migration-backups/system-disk-original-20260830T130557Z`
  (193 MiB).
- The emergency copy from the automatically rolled-back first attempt was also
  moved under the data-disk migration backups.

No imported evidence, exports, scheduled backups, or former live database files
were deleted during cutover.
