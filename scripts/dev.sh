#!/usr/bin/env bash
set -euo pipefail

# Run a local preview with an explicit copied database, or a fresh local one.
radar_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$radar_root"
export GITHUB_RADAR_DB_PATH="${1:-${GITHUB_RADAR_DB_PATH:-$radar_root/github-radar.db}}"
export GITHUB_RADAR_DISCOVERY_CONFIG="${GITHUB_RADAR_DISCOVERY_CONFIG:-$radar_root/config/discovery.example.yaml}"
export GITHUB_RADAR_TOPICS_CONFIG="${GITHUB_RADAR_TOPICS_CONFIG:-$radar_root/config/topics.example.yaml}"
export GITHUB_RADAR_LISTEN_ADDR="${GITHUB_RADAR_LISTEN_ADDR:-127.0.0.1:8878}"
export GITHUB_RADAR_LOCALE="${GITHUB_RADAR_LOCALE:-zh-CN}"

mkdir -p "$radar_root/bin"
go build -o "$radar_root/bin/github-radar-local" ./cmd/github-radar
exec "$radar_root/bin/github-radar-local" serve
