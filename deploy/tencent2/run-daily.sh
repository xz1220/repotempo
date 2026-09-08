#!/bin/sh
set -eu

data_mount=/home/xingzheng/data
data_root=$data_mount/github-radar

if ! /usr/bin/mountpoint -q "$data_mount"; then
  echo "github-radar: data disk is not mounted at $data_mount; collection skipped" >&2
  exit 1
fi

# The single-quoted body is intentionally expanded by the clean child shell.
# shellcheck disable=SC2016
exec /usr/bin/flock -n "$data_root/daily.lock" \
  /usr/bin/env -i \
  HOME="$data_root" \
  GITHUB_RADAR_DATA_ROOT="$data_root" \
  PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  /bin/sh -c '
    set -eu
    expected_data_root=$GITHUB_RADAR_DATA_ROOT
    set -a
    . /etc/github-radar/github-radar.env
    set +a
    if [ "${GITHUB_RADAR_DB_PATH:-}" != "$expected_data_root/github-radar.db" ] ||
       [ "${GITHUB_RADAR_EXPORT_DIR:-}" != "$expected_data_root/exports" ] ||
       [ "${GITHUB_RADAR_BACKUP_DIR:-}" != "$expected_data_root/backups" ]; then
      echo "github-radar: production data paths do not match $expected_data_root" >&2
      exit 1
    fi
    # Supplementary facts run after Star snapshots, with a three-minute
    # deadline and reserved API headroom. Set the existing env value to 0
    # to disable this cache refresh without changing the primary collector.
    export GITHUB_RADAR_ACTIVITY_DAILY_LIMIT=${GITHUB_RADAR_ACTIVITY_DAILY_LIMIT:-500}
    exec /opt/github-radar/github-radar run-daily
  '
