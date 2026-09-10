#!/usr/bin/env python3
"""Upgrade the existing RepoTempo service with an isolated SQLite backup.

Run as root under the existing daily.lock, with the absolute release binary.
The script changes only this service's binary pointer and public-signup setting.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import time
import urllib.request

DATA = Path("/home/xingzheng/data/github-radar")
DATABASE = DATA / "github-radar.db"
ENV = Path("/etc/github-radar/github-radar.env")
BINARY = Path("/opt/github-radar/github-radar")
SERVICE = "github-radar-web.service"
TABLES = ("repositories", "daily_snapshots", "repository_analyses", "repository_topics", "topics")

def command(*args):
    subprocess.run(args, check=True, stdout=subprocess.DEVNULL)

def evidence(path):
    db = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    result = {"integrity": db.execute("PRAGMA integrity_check").fetchone()[0], "tables": {}}
    for table in TABLES:
        digest = hashlib.sha256()
        count = 0
        # Startup re-applies the unchanged taxonomy config and refreshes only
        # topics.updated_at. Compare its business columns, not that run timestamp.
        columns = "id,slug,name,parent_id,description,status,created_at" if table == "topics" else "*"
        for row in db.execute(f'SELECT {columns} FROM "{table}" ORDER BY rowid'):
            digest.update(json.dumps(row, ensure_ascii=False, separators=(",", ":"), default=str).encode())
            digest.update(b"\n")
            count += 1
        result["tables"][table] = {"count": count, "sha256": digest.hexdigest()}
    result["foreign_keys"] = sorted(db.execute("PRAGMA foreign_key_check").fetchall())
    db.close()
    return result

def wait_ready():
    for _ in range(30):
        try:
            with urllib.request.urlopen("http://127.0.0.1:8787/readyz", timeout=2) as response:
                if response.status == 200:
                    return
        except Exception:
            time.sleep(1)
    raise RuntimeError("readiness did not become healthy")

def main():
    if os.geteuid() != 0 or len(sys.argv) != 2:
        raise SystemExit("Run as root with the absolute release binary path")
    candidate = Path(sys.argv[1]).resolve(strict=True)
    releases = (DATA / "releases").resolve()
    if releases not in candidate.parents or candidate.name != "repotempo":
        raise SystemExit("Candidate must be a repotempo binary under the releases directory")
    command("mountpoint", "-q", "/home/xingzheng/data")
    if not DATABASE.is_file() or not ENV.is_file() or not BINARY.is_symlink():
        raise SystemExit("Existing service layout does not match this upgrade")
    backup = DATA / "deployment-backups" / candidate.parent.name
    backup.mkdir(mode=0o700, parents=True, exist_ok=False)
    previous = BINARY.resolve(strict=True)
    shutil.copy2(ENV, backup / "environment")
    command("systemctl", "stop", SERVICE)
    switched = False
    env_changed = False
    try:
        src, dst = sqlite3.connect(DATABASE), sqlite3.connect(backup / "before.db")
        pending = src.execute("SELECT COUNT(*) FROM job_runs WHERE job_type='repository_import' AND status='running'").fetchone()[0]
        if pending:
            src.close(); dst.close()
            raise RuntimeError("pending imports must finish before this upgrade")
        src.backup(dst); dst.close(); src.close()
        os.chmod(backup / "before.db", 0o600)
        before = evidence(backup / "before.db")
        if before["integrity"] != "ok":
            raise RuntimeError("backup integrity check failed")
        (backup / "evidence-before.json").write_text(json.dumps(before, indent=2))
        old_stat = ENV.stat()
        lines = [line for line in ENV.read_text().splitlines() if not line.startswith("GITHUB_RADAR_PUBLIC_SIGNUP=")]
        lines.append("GITHUB_RADAR_PUBLIC_SIGNUP=1")
        next_env = ENV.with_name("github-radar.env.next")
        with next_env.open("x") as handle:
            os.chmod(next_env, 0o600)
            handle.write("\n".join(lines) + "\n")
        os.chown(next_env, old_stat.st_uid, old_stat.st_gid)
        os.chmod(next_env, old_stat.st_mode & 0o777)
        os.replace(next_env, ENV)
        env_changed = True
        next_binary = BINARY.with_name("github-radar.next")
        next_binary.symlink_to(candidate)
        os.replace(next_binary, BINARY)
        switched = True
        command("systemctl", "start", SERVICE)
        wait_ready()
        after = evidence(DATABASE)
        if before != after:
            raise RuntimeError("business records changed during schema migration")
        db = sqlite3.connect(f"file:{DATABASE}?mode=ro", uri=True)
        version = db.execute("PRAGMA user_version").fetchone()[0]
        owner = db.execute("SELECT owner_id FROM account_migrations WHERE name='legacy-workspace'").fetchone()
        counts = {table: db.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0] for table in ("users", "user_repositories", "agent_keys")}
        db.close()
        if version != 9 or not owner:
            raise RuntimeError("account migration is incomplete")
        report = {"release": str(candidate), "previous": str(previous), "backup": str(backup), "schema": version, "legacy_owner": owner[0], "counts": counts, "business_records_preserved": True}
        (backup / "result.json").write_text(json.dumps(report, indent=2))
        print(json.dumps(report))
    except Exception:
        if switched:
            command("systemctl", "stop", SERVICE)
            rollback = BINARY.with_name("github-radar.rollback")
            rollback.symlink_to(previous)
            os.replace(rollback, BINARY)
        if env_changed:
            shutil.copy2(backup / "environment", ENV)
        # Additive tables are preserved. No database rollback can discard new data.
        command("systemctl", "start", SERVICE)
        raise

if __name__ == "__main__":
    main()
