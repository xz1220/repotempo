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
proxy.

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

