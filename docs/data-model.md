# Data model

The first release has exactly five tables.

1. `repositories` is the registry, keyed by immutable GitHub repository ID.
2. `daily_snapshots` stores one outcome per repository and Shanghai calendar
   date. Failed outcomes use a null star count, never zero.
3. `topics` stores a taxonomy with at most two levels.
4. `repository_topics` is the many-to-many mapping and preserves assignment
   provenance. Confirmed manual assignments win over automatic ones.
5. `job_runs` records discovery, import, snapshot, export, and daily-run evidence.

Current star count and growth windows are derived from successful snapshots.
They are not duplicated as mutable repository fields. A normal rerun may fill a
missing or failed observation, but it may not overwrite an existing success.

Historical imports keep only real observations. GitHub does not expose exact
daily star history for dates before monitoring began, so gaps remain gaps.

A confirmed manual assignment with confidence `0` is a negative topic decision.
It remains in raw exports as an auditable veto, is excluded from topic views and
aggregates, and prevents later automatic discovery from silently restoring a
topic the user removed. A later explicit manual assignment replaces the veto.
