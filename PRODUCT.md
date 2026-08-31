# GitHub Radar

GitHub Radar is a self-hosted research system with a read-only Web interface
for following what happens to popular GitHub repositories after they first
enter the radar. It
turns daily discovery reports into a durable repository registry, one explicit
observation per day, and comparable historical views.

## Users

The first user is a technical investor and open-source researcher who reviews
the system on desktop, occasionally checks it on mobile, and exports the data
for deeper analysis.

## Core jobs

1. Build and maintain one deduplicated registry from OSS Insight, GitHub Search,
   historical imports, and a manual watchlist.
2. Record one explicit daily outcome for every actively monitored repository,
   preserving successes, failures, and missing observations as different
   states.
3. Show how monitored repositories develop after discovery: current Star
   stock, 1-day / 7-day / 30-day momentum, and changes in their rank within a
   comparable monitored sample.
4. Let the researcher move the comparison endpoint to a specific date and
   narrow the catalogue by topic, discovery source, monitoring state, search,
   or “new on this date”.
5. Organize repositories in a two-level topic taxonomy. A parent topic includes
   its children; repositories without an active assignment remain explicitly
   visible as unclassified.
6. Preserve a traceable current explanation of what each important project
   does. A Codex-generated or human-written analysis is produced outside the
   collector, stored with provenance and revision metadata, and rendered
   read-only in the project detail page.
7. Export auditable monitoring data as CSV, JSON, and SQLite. Project
   interpretations are included in SQLite backups in this release.

## Product boundaries

- The collector gathers repository metadata, discovery provenance, daily Star
  observations, and collection evidence. It does not automatically read
  READMEs or collect commits, releases, repository contents, or code activity.
- Repository interpretation is a separate offline research/import workflow.
  The Web application does not invoke an AI model and does not write analyses.
- The current interpretation is stored with a revision counter; prior
  interpretation bodies and CSV/JSON interpretation export are out of scope.
- The product reports attention signals; it does not infer causality or provide
  investment advice.
- It never interpolates missing endpoint observations, fabricates history, or
  replaces a failed observation with zero.
- The Web application is read-only and has no authentication or admin console.

## Primary experience

The canonical home route, `/`, is the project-monitoring catalogue. It answers
the product's main question directly: among repositories we already observe,
which ones are gaining or losing momentum now? `/repositories` remains a
compatibility route to the same view. New discoveries are a filter on that
catalogue, and collection history is a compact utility rather than a competing
top-level destination.

## Register

Product dashboard. Information density is moderate to high. Correctness,
provenance, scanability, and clear failure states take priority over decoration.
