# GitHub Radar

GitHub Radar is a self-hosted, read-only research system for monitoring how
open-source repositories and technical topics gain GitHub stars over time.

## Users

The first user is a technical investor and open-source researcher who reviews
the dashboard on desktop, occasionally checks it on mobile, and exports data
for deeper analysis.

## Core jobs

1. Discover repositories through OSS Insight, GitHub Search, historical data,
   and a manual watchlist.
2. Record one explicit daily outcome for every actively monitored repository.
3. Compare absolute star stock with 1-day, 7-day, and 30-day growth.
4. Organize repositories into a two-level topic taxonomy.
5. Export auditable CSV, JSON, and SQLite data.

## Product boundaries

- The first release does not collect commits, releases, README changes, or
  repository contents.
- It does not infer causality or provide investment advice.
- It never fabricates missing history or replace failed observations with zero.
- The Web application is read-only and has no authentication or admin console.

## Register

Product dashboard. Information density is moderate to high. Correctness,
provenance, scanability, and clear failure states take priority over decoration.

