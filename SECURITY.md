# Security policy

## Supported versions

Security fixes are provided for the latest tagged release.

## Reporting a vulnerability

Do not open a public issue for a vulnerability that could expose a GitHub token,
the SQLite database, or server details. Use GitHub private vulnerability
reporting for this repository. Include the affected version, reproduction steps,
impact, and any suggested mitigation.

## Deployment boundary

- GitHub tokens belong in `/etc/github-radar/github-radar.env`, never in source,
  command history, Cron entries, screenshots, or logs.
- The environment file should be readable only by root and the service group.
- The Web service is read-only and binds to loopback by default.
- The SQLite file and exports are never served as static assets.
- A reverse proxy should provide HTTPS and any desired access control.

