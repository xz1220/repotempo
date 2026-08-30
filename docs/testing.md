# Testing

Run the local release gate with:

```sh
make ci
make security
```

The automated suite covers migrations, uniqueness, snapshot retry semantics,
repository identity, topic precedence, external API response classes, Search
pagination and partitioning, import gaps, statistics, template escaping, empty
states, and HTTP routes.

Release verification additionally uses live GitHub and OSS Insight requests,
imports a copy of the legacy SQLite database, compares SQL with CSV and JSON,
opens every main page in a real desktop and mobile browser, scans for secrets,
and rebuilds from a clean public clone. Live checks are reported separately from
fixture-based tests.

The v0.1.0 browser evidence is stored under `docs/screenshots/` and covers a
desktop overview, mobile overview/menu, repository filters, repository detail
and charts, and run status. Browser verification also opened Topics,
Discoveries, health, and readiness routes over the public HTTPS endpoint.
