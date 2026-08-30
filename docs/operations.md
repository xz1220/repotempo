# Operations

## Runtime layout

- Binary: `/opt/github-radar/github-radar`
- Database: `/var/lib/github-radar/github-radar.db`
- Exports: `/var/lib/github-radar/exports`
- Backups: `/var/lib/github-radar/backups`
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

## Install and upgrade

Build from a clean revision and pass the artifact explicitly:

```sh
make ci
sudo deploy/tencent2/install.sh bin/github-radar-linux-amd64
sudo -u github-radar /opt/github-radar/github-radar doctor
sudo systemctl restart github-radar-web.service
```

Before an upgrade, create a native SQLite backup and copy it off-host if the
change includes a migration:

```sh
sudo -u github-radar /opt/github-radar/github-radar export --format sqlite \
  --output /var/lib/github-radar/backups
```

The installer preserves the current binary as
`/opt/github-radar/github-radar.previous` and never overwrites an existing env,
discovery, or topic configuration.

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

## Routine checks

```sh
sudo -u github-radar /opt/github-radar/github-radar doctor
systemctl status github-radar-web.service
journalctl -u github-radar-web.service --since today
tail -n 100 /var/log/github-radar/daily.log
df -h /
```

Backups and exports use configured retention periods. Repository contents,
README files, commits, and raw long-lived API responses are not stored.
