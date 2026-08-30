#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "install.sh must run as root" >&2
  exit 1
fi

release_binary=${1:-./github-radar-linux-amd64}
if [ ! -f "$release_binary" ]; then
  echo "binary not found: $release_binary" >&2
  exit 1
fi

if ! id github-radar >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/github-radar --shell /usr/sbin/nologin github-radar
fi

install -d -o root -g root -m 0755 /opt/github-radar /etc/github-radar
install -d -o github-radar -g github-radar -m 0750 \
  /var/lib/github-radar \
  /var/lib/github-radar/exports \
  /var/lib/github-radar/backups \
  /var/log/github-radar
install -o root -g root -m 0755 "$release_binary" /opt/github-radar/github-radar

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
