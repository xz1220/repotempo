# Operations

## Runtime layout

- Binary: `/opt/github-radar/github-radar`
- Data-disk mount: `/home/xingzheng/data`
- Data root: `/home/xingzheng/data/github-radar`
- Database: `/home/xingzheng/data/github-radar/github-radar.db`
- Exports: `/home/xingzheng/data/github-radar/exports`
- Backups: `/home/xingzheng/data/github-radar/backups`
- Imports: `/home/xingzheng/data/github-radar/import`
- Environment: `/etc/github-radar/github-radar.env`
- Discovery config: `/etc/github-radar/discovery.yaml`
- Topic config: `/etc/github-radar/topics.yaml`

The production host is reached through the local SSH alias `tecent2`. The
business name is `tencent2`; it is not the name of a Cron job.

## Daily schedule

The existing Python Feishu digest keeps its 09:00 Asia/Shanghai schedule.
GitHub Radar runs at 09:15 with `flock`. The Web process runs under systemd,
binds to `127.0.0.1:8787`, and is published through the existing HTTPS reverse
proxy as an independent host. Do not mount it under a URL prefix because the
embedded dashboard intentionally uses root-relative routes and assets.

The Web process can add public GitHub projects through `/watch/new`. Direct
loopback access permits local management; public writes require HTTPS and
`GITHUB_RADAR_WEB_WRITE_TOKEN` (24–512 characters) in the protected environment
file. Use an independent random management token, not the GitHub token. The
reverse proxy must overwrite forwarded scheme headers. Collection and watchlist
writes share the same SQLite data-disk database.

The systemd unit requires `/home/xingzheng/data` to be a real mount point. The
Cron command performs the same check before creating a lock or opening SQLite.
This is deliberate fail-closed behavior: an unmounted data disk must not result
in an empty database being created on the system disk. The service user receives
execute-only ACLs on the private parent directories and full access only to the
GitHub Radar data root.
The service sees only the bound data directory under a private `ProtectHome`
namespace; other home-directory contents remain hidden. Cron calls
`/opt/github-radar/run-daily`, which logs an explicit error and exits before
opening SQLite if the data disk is not mounted.

## Install and upgrade

Production CLI commands must load the protected environment file explicitly;
the Go binary does not read dotenv files. Define this helper in the current
administrator shell:

```sh
run_radar() {
  sudo -u github-radar /usr/bin/env -i \
    HOME=/home/xingzheng/data/github-radar \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    /bin/sh -c '
      set -eu
      set -a
      . /etc/github-radar/github-radar.env
      set +a
      exec /opt/github-radar/github-radar "$@"
    ' github-radar "$@"
}
```

Then build from a clean revision and pass the artifact explicitly:

```sh
make ci
sudo deploy/tencent2/install.sh bin/github-radar-linux-amd64
run_radar doctor
sudo systemctl restart github-radar-web.service
```

Before an upgrade, create a native SQLite backup and copy it off-host if the
change includes a migration:

```sh
run_radar export --format sqlite \
  --output /home/xingzheng/data/github-radar/backups
```

The installer preserves the current binary as
`/opt/github-radar/github-radar.previous` and never overwrites an existing env,
discovery, or topic configuration.

For the one-time move from the former system-disk layout, run the checked-in
migration from a trusted checkout:

```sh
sudo deploy/tencent2/migrate-data-disk.sh
```

It holds the old daily lock, stops the Web process, creates a native SQLite
backup, checks integrity and foreign keys, compares key table counts, installs
the new service and Cron definitions, runs `doctor`, and verifies readiness. It
keeps the source directory in place until post-migration verification is done
and creates a compact emergency database backup under `/var/backups/github-radar`.
Run this migration before the first data-disk-aware `install.sh` upgrade; the
installer deliberately refuses to create an empty destination while it detects
the legacy live database.

The migration does not silently repair historical data. If the source database
already has foreign-key findings, the emergency and destination copies must
reproduce the exact same findings; any difference aborts the cutover. Record and
resolve legacy findings as a separate, reviewed data-maintenance change.

Run the migration away from the 09:15 collection minute. Both the old and new
lock files are held during cutover, so a scheduled run cannot overlap the copy;
if it fires while migration is in progress, run `run-daily` manually afterward.

If a post-copy check fails, the script restores the former env, unit, Cron, and
Web process. The failed candidate is preserved as
`/home/xingzheng/data/github-radar.failed-<timestamp>` so evidence is not
deleted and the migration can be retried without a destination collision.

After the Web, Cron environment, exports, and next daily run have all passed,
move the retained source directory onto the data disk rather than deleting it:

```sh
stamp=$(date -u +%Y%m%dT%H%M%SZ)
sudo systemctl stop github-radar-web.service
sudo mv /var/lib/github-radar \
  "/home/xingzheng/data/github-radar/migration-backups/system-disk-original-$stamp"
sudo systemctl start github-radar-web.service
```

Keep `/var/backups/github-radar/pre-data-disk-*.db` on the system disk through
at least one successful scheduled collection. It is the compact rollback copy
if the data disk itself becomes unavailable.

## Rollback

For a binary-only rollback:

```sh
sudo systemctl stop github-radar-web.service
sudo install -o root -g root -m 0755 \
  /opt/github-radar/github-radar.previous /opt/github-radar/github-radar
sudo systemctl start github-radar-web.service
```

If a future release changes the schema incompatibly, stop both Cron and Web,
restore the matching SQLite backup, then restore the previous binary. Never run
two schema versions against the same live database during rollback.

For a data-path rollback, stop the Web service, restore the pre-migration env,
unit, and Cron definitions, restore the emergency SQLite backup under
`/var/lib/github-radar`, reload systemd, then start the former configuration.
Do not point two running collectors at the old and new databases.

## Routine checks

```sh
run_radar doctor
systemctl status github-radar-web.service
journalctl -u github-radar-web.service --since today
tail -n 100 /var/log/github-radar/daily.log
findmnt -T /home/xingzheng/data/github-radar/github-radar.db
df -h /home/xingzheng/data
```

Backups and exports use configured retention periods. Repository contents,
README files, commits, and raw long-lived API responses are not stored.
