# Testing

Run the local release gate with:

```sh
make ci
make security
```

The automated suite covers migrations, uniqueness, snapshot retry semantics,
repository identity, topic precedence, external API response classes, Search
pagination and partitioning, import gaps, strict two/three-date comparisons,
fixed-cohort statistics, offline classification, protected manual additions,
template escaping, empty states, and HTTP routes.

Release verification additionally uses live GitHub requests, imports a copy of
the existing SQLite history, compares SQL with CSV and JSON,
opens every main page in a real desktop and mobile browser, scans for secrets,
and rebuilds from a clean public clone. Live checks are reported separately from
fixture-based tests.

OSS Insight availability is not required for the default GitHub-only mode.
Verify that a disabled or unavailable optional source does not stop existing
repository snapshots. Browser checks should include the three reading pages,
category filters, Add project, stale data, missing comparisons, and pagination.

Historical v0.1.0 browser evidence is stored under `docs/screenshots/` and covers a
desktop overview, mobile overview/menu, repository filters, repository detail
and charts, and run status. Browser verification also opened Topics,
Discoveries, health, and readiness routes over the public HTTPS endpoint.
These files describe the earlier release and do not certify the current
redesign or its deployment.
