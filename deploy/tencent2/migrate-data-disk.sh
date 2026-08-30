#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "migrate-data-disk.sh must run as root" >&2
  exit 1
fi

old_root=/var/lib/github-radar
data_mount=/home/xingzheng/data
new_root=$data_mount/github-radar
env_file=/etc/github-radar/github-radar.env
unit_file=/etc/systemd/system/github-radar-web.service
cron_file=/etc/cron.d/github-radar
daily_script=/opt/github-radar/run-daily
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
stamp=$(date -u +%Y%m%dT%H%M%SZ)
stage=$data_mount/github-radar.migrating-$stamp
failed_root=$data_mount/github-radar.failed-$stamp
rollback_dir=$(mktemp -d /var/tmp/github-radar-data-migration.XXXXXX)
emergency_dir=/var/backups/github-radar
service_was_active=false
service_stopped=false
destination_created=false
daily_script_existed=false
daily_script_changed=false
rollback_needed=true
[ ! -f "$daily_script" ] || daily_script_existed=true

cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  set +e
  if [ "$rollback_needed" = true ]; then
    echo "Migration failed; restoring service configuration." >&2
    if [ "$service_stopped" = true ]; then
      systemctl stop github-radar-web.service || true
    fi
    [ ! -f "$rollback_dir/github-radar.env" ] || install -o root -g github-radar -m 0640 "$rollback_dir/github-radar.env" "$env_file"
    [ ! -f "$rollback_dir/github-radar-web.service" ] || install -o root -g root -m 0644 "$rollback_dir/github-radar-web.service" "$unit_file"
    [ ! -f "$rollback_dir/github-radar.cron" ] || install -o root -g root -m 0644 "$rollback_dir/github-radar.cron" "$cron_file"
    if [ "$daily_script_changed" = true ]; then
      if [ "$daily_script_existed" = true ]; then
        install -o root -g root -m 0755 "$rollback_dir/run-daily" "$daily_script"
      elif [ -f "$daily_script" ]; then
        rm -f "$daily_script"
      fi
    fi
    if [ "$destination_created" = true ] && [ -d "$new_root" ]; then
      mv "$new_root" "$failed_root"
      echo "Failed destination preserved at $failed_root" >&2
    fi
    systemctl daemon-reload || true
    if [ "$service_was_active" = true ]; then
      systemctl start github-radar-web.service || true
    fi
    if [ -d "$stage" ]; then
      rm -rf "$stage"
    fi
  fi
  rm -rf "$rollback_dir"
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

for command_name in curl flock mountpoint rsync setfacl sqlite3 sudo; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "required command not found: $command_name" >&2
    exit 1
  fi
done
if ! mountpoint -q "$data_mount"; then
  echo "data disk is not mounted at $data_mount" >&2
  exit 1
fi
if [ ! -f "$old_root/github-radar.db" ]; then
  echo "source database not found: $old_root/github-radar.db" >&2
  exit 1
fi
if [ -e "$new_root" ]; then
  echo "destination already exists: $new_root" >&2
  exit 1
fi
setfacl -m u:github-radar:--x /home/xingzheng "$data_mount"

install -o root -g github-radar -m 0640 "$env_file" "$rollback_dir/github-radar.env"
install -o root -g root -m 0644 "$unit_file" "$rollback_dir/github-radar-web.service"
install -o root -g root -m 0644 "$cron_file" "$rollback_dir/github-radar.cron"
if [ -f "$daily_script" ]; then
  install -o root -g root -m 0755 "$daily_script" "$rollback_dir/run-daily"
fi

exec 9>"$old_root/daily.lock"
if ! flock -n 9; then
  echo "the daily collection lock is busy; retry after the run completes" >&2
  exit 1
fi
if systemctl is-active --quiet github-radar-web.service; then
  service_was_active=true
fi
systemctl stop github-radar-web.service
service_stopped=true

echo "Creating the system-disk emergency backup."
install -d -o root -g root -m 0750 "$emergency_dir"
sqlite3 "$old_root/github-radar.db" 'PRAGMA wal_checkpoint(TRUNCATE);' >/dev/null
source_foreign_keys=$(sqlite3 "$old_root/github-radar.db" 'PRAGMA foreign_key_check;')
sqlite3 "$old_root/github-radar.db" ".backup '$emergency_dir/pre-data-disk-$stamp.db'"
test "$(sqlite3 "$emergency_dir/pre-data-disk-$stamp.db" 'PRAGMA integrity_check;')" = ok
emergency_foreign_keys=$(sqlite3 "$emergency_dir/pre-data-disk-$stamp.db" 'PRAGMA foreign_key_check;')
test "$source_foreign_keys" = "$emergency_foreign_keys"
if [ -n "$source_foreign_keys" ]; then
  echo "Warning: preserving pre-existing foreign-key findings from the source database." >&2
fi

echo "Copying exports, backups, imports, and a consistent live database."
install -d -o github-radar -g github-radar -m 0750 \
  "$stage" "$stage/exports" "$stage/backups" "$stage/import" "$stage/migration-backups"
for directory in exports backups import; do
  if [ -d "$old_root/$directory" ]; then
    rsync -a "$old_root/$directory/" "$stage/$directory/"
  fi
done
sqlite3 "$old_root/github-radar.db" ".backup '$stage/github-radar.db'"
chmod 0640 "$stage/github-radar.db"
test "$(sqlite3 "$stage/github-radar.db" 'PRAGMA integrity_check;')" = ok
stage_foreign_keys=$(sqlite3 "$stage/github-radar.db" 'PRAGMA foreign_key_check;')
test "$source_foreign_keys" = "$stage_foreign_keys"

echo "Comparing source and destination database metrics."
old_counts=$(sqlite3 -separator : "$old_root/github-radar.db" \
  "SELECT (SELECT COUNT(*) FROM repositories),(SELECT COUNT(*) FROM daily_snapshots),(SELECT COUNT(*) FROM topics),(SELECT COUNT(*) FROM repository_topics),(SELECT COUNT(*) FROM job_runs),(SELECT COALESCE(MAX(snapshot_date), '') FROM daily_snapshots),(SELECT COALESCE(SUM(star_count),0) FROM daily_snapshots WHERE star_count IS NOT NULL),(SELECT user_version FROM pragma_user_version);")
new_counts=$(sqlite3 -separator : "$stage/github-radar.db" \
  "SELECT (SELECT COUNT(*) FROM repositories),(SELECT COUNT(*) FROM daily_snapshots),(SELECT COUNT(*) FROM topics),(SELECT COUNT(*) FROM repository_topics),(SELECT COUNT(*) FROM job_runs),(SELECT COALESCE(MAX(snapshot_date), '') FROM daily_snapshots),(SELECT COALESCE(SUM(star_count),0) FROM daily_snapshots WHERE star_count IS NOT NULL),(SELECT user_version FROM pragma_user_version);")
if [ "$old_counts" != "$new_counts" ]; then
  echo "database count mismatch: old=$old_counts new=$new_counts" >&2
  exit 1
fi
chown -R github-radar:github-radar "$stage"
chmod 0750 "$stage" "$stage/exports" "$stage/backups" "$stage/import" "$stage/migration-backups"
chmod 0640 "$stage/github-radar.db"
destination_created=true
mv "$stage" "$new_root"
install -o github-radar -g github-radar -m 0640 /dev/null "$new_root/daily.lock"
exec 8<>"$new_root/daily.lock"
flock -n 8

echo "Installing the data-disk runtime configuration."
env_tmp=$(mktemp "$rollback_dir/env.XXXXXX")
awk -v db="$new_root/github-radar.db" -v exports="$new_root/exports" -v backups="$new_root/backups" '
  BEGIN { seen_db=0; seen_exports=0; seen_backups=0; seen_locale=0 }
  /^GITHUB_RADAR_DB_PATH=/ { print "GITHUB_RADAR_DB_PATH=" db; seen_db=1; next }
  /^GITHUB_RADAR_EXPORT_DIR=/ { print "GITHUB_RADAR_EXPORT_DIR=" exports; seen_exports=1; next }
  /^GITHUB_RADAR_BACKUP_DIR=/ { print "GITHUB_RADAR_BACKUP_DIR=" backups; seen_backups=1; next }
  /^GITHUB_RADAR_LOCALE=/ { print "GITHUB_RADAR_LOCALE=zh-CN"; seen_locale=1; next }
  { print }
  END {
    if (!seen_db) print "GITHUB_RADAR_DB_PATH=" db
    if (!seen_exports) print "GITHUB_RADAR_EXPORT_DIR=" exports
    if (!seen_backups) print "GITHUB_RADAR_BACKUP_DIR=" backups
    if (!seen_locale) print "GITHUB_RADAR_LOCALE=zh-CN"
  }
' "$env_file" >"$env_tmp"
install -o root -g github-radar -m 0640 "$env_tmp" "$env_file"
install -o root -g root -m 0644 "$script_dir/github-radar-web.service" "$unit_file"
daily_script_changed=true
install -o root -g root -m 0755 "$script_dir/run-daily.sh" "$daily_script"
install -o root -g root -m 0644 "$script_dir/github-radar.cron" "$cron_file"
systemctl daemon-reload

echo "Checking the migrated runtime and Web readiness."
sudo -u github-radar /usr/bin/env -i \
  HOME="$new_root" \
  PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  /bin/sh -c '
    set -eu
    set -a
    . /etc/github-radar/github-radar.env
    set +a
    exec /opt/github-radar/github-radar doctor
  '
systemctl start github-radar-web.service
for attempt in 1 2 3 4 5; do
  if curl --fail --silent --show-error http://127.0.0.1:8787/readyz >/dev/null; then
    break
  fi
  if [ "$attempt" -eq 5 ]; then
    echo "dashboard readiness check failed" >&2
    exit 1
  fi
  sleep 1
done

if [ "$service_was_active" = false ]; then
  systemctl stop github-radar-web.service
fi

rollback_needed=false
rm -rf "$rollback_dir"
trap - EXIT HUP INT TERM
echo "Migration complete. Database counts: $new_counts"
echo "Emergency database backup: $emergency_dir/pre-data-disk-$stamp.db"
echo "The source directory remains at $old_root until post-migration verification is complete."
