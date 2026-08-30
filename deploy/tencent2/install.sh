#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "install.sh must run as root" >&2
  exit 1
fi

for command_name in mountpoint setfacl; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "required command not found: $command_name" >&2
    exit 1
  fi
done

release_binary=${1:-./github-radar-linux-amd64}
data_mount=/home/xingzheng/data
data_root=$data_mount/github-radar
if [ ! -f "$release_binary" ]; then
  echo "binary not found: $release_binary" >&2
  exit 1
fi
if ! mountpoint -q "$data_mount"; then
  echo "data disk is not mounted at $data_mount" >&2
  exit 1
fi
if [ -f /var/lib/github-radar/github-radar.db ] && [ ! -f "$data_root/github-radar.db" ]; then
  echo "legacy data detected; run deploy/tencent2/migrate-data-disk.sh before installing" >&2
  exit 1
fi

if ! id github-radar >/dev/null 2>&1; then
  useradd --system --home-dir "$data_root" --shell /usr/sbin/nologin github-radar
else
  usermod --home "$data_root" github-radar
fi

install -d -o root -g root -m 0755 /opt/github-radar /etc/github-radar
setfacl -m u:github-radar:--x /home/xingzheng "$data_mount"
install -d -o github-radar -g github-radar -m 0750 \
  "$data_root" \
  "$data_root/exports" \
  "$data_root/backups" \
  "$data_root/import" \
  "$data_root/migration-backups" \
  /var/log/github-radar
if [ -x /opt/github-radar/github-radar ]; then
  install -o root -g root -m 0755 /opt/github-radar/github-radar \
    /opt/github-radar/github-radar.previous
fi
install -o root -g root -m 0755 "$release_binary" /opt/github-radar/github-radar
install -o root -g root -m 0755 deploy/tencent2/run-daily.sh /opt/github-radar/run-daily

if [ ! -f /etc/github-radar/github-radar.env ]; then
  install -o root -g github-radar -m 0640 deploy/tencent2/github-radar.env.example \
    /etc/github-radar/github-radar.env
fi

if [ ! -f /etc/github-radar/discovery.yaml ]; then
  install -o root -g root -m 0644 config/discovery.example.yaml /etc/github-radar/discovery.yaml
fi
if [ ! -f /etc/github-radar/topics.yaml ]; then
  install -o root -g root -m 0644 config/topics.example.yaml /etc/github-radar/topics.yaml
fi
install -o root -g root -m 0644 deploy/tencent2/github-radar-web.service \
  /etc/systemd/system/github-radar-web.service
install -o root -g root -m 0644 deploy/tencent2/github-radar.cron /etc/cron.d/github-radar
install -o root -g root -m 0644 deploy/tencent2/github-radar.logrotate /etc/logrotate.d/github-radar

systemctl daemon-reload
systemctl enable github-radar-web.service
echo "Installed. Add a GitHub token to /etc/github-radar/github-radar.env, then run doctor."
