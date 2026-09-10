#!/usr/bin/env python3
"""Verify a release's export against a private copy of production data."""
import csv
import io
import json
from pathlib import Path
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.request

binary = str(Path(sys.argv[1]).resolve(strict=True))
directory = Path(tempfile.mkdtemp(prefix="repotempo-export-check-"))
database = directory / "verify.db"
source = sqlite3.connect("file:/home/xingzheng/data/github-radar/github-radar.db?mode=ro", uri=True)
destination = sqlite3.connect(database)
source.backup(destination)
expected = source.execute("SELECT COUNT(*) FROM repositories").fetchone()[0]
source.close(); destination.close()
with socket.socket() as probe:
    probe.bind(("127.0.0.1", 0))
    port = probe.getsockname()[1]
with (directory / "server.log").open("w") as log:
    process = subprocess.Popen(["/bin/sh", "-c", 'set -a; . /etc/github-radar/github-radar.env; set +a; export GITHUB_RADAR_DB_PATH="$2"; export GITHUB_RADAR_LISTEN_ADDR="127.0.0.1:$3"; exec "$1" serve', "verify", binary, str(database), str(port)], stdout=log, stderr=log)
    try:
        for _ in range(30):
            if process.poll() is not None: raise RuntimeError("preview process exited")
            try:
                with urllib.request.urlopen(f"http://127.0.0.1:{port}/readyz", timeout=1) as response:
                    if response.status == 200: break
            except Exception: time.sleep(0.2)
        start = time.monotonic()
        with urllib.request.urlopen(f"http://127.0.0.1:{port}/repositories/export?view=all&new=0", timeout=20) as response:
            rows = list(csv.reader(io.StringIO(response.read().decode("utf-8-sig"))))
        if len(rows)-1 != expected: raise RuntimeError("export count differs from production copy")
        print(json.dumps({"projects": expected, "seconds": round(time.monotonic()-start, 3), "status": 200, "production_modified": False}))
    finally:
        process.terminate()
        try: process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill(); process.wait()
