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
- The Web service binds to loopback by default. Direct local requests may add
  public projects to the watchlist; public access is read-only unless an
  independent management token is configured.
- Public writes require HTTPS, GITHUB_RADAR_WEB_WRITE_TOKEN, a same-origin
  request, and a single-use CSRF token. Do not reuse the GitHub API token as the
  management token, and do not expose loopback exemptions through proxy headers.
- The SQLite file and exports are never served as static assets.
- A reverse proxy should provide HTTPS and any desired access control.
