# GitHub Radar

GitHub Radar helps a researcher discover interesting GitHub projects and keep
following them after the first report. It combines a daily project archive,
continuous Star observations, and a visual trend dashboard.

## Users and recurring decisions

The first user is a technical researcher and investor who follows open-source
projects on desktop, checks selected projects on mobile, and exports data for
deeper research and personal open-source project selection.

The daily session starts with three questions: which projects are growing
fastest, which are growing slowly, and which have lost momentum compared with
the previous period? From there, the user opens a project to understand what
it does or adds an interesting repository for continued observation.

## Core capabilities

1. Show a trend dashboard with fastest growth, slow or zero growth, and slowing
   momentum. Charts compare a stable group of repositories and expose the
   coverage behind the comparison.
2. Archive newly discovered projects by their first entry into the radar.
   Discovery automatically registers projects for continued collection.
3. Let an operator add a public GitHub URL or owner/name from the Web interface,
   save an optional note and category, and see the project with its first real
   Star snapshot immediately.
4. Keep all tracked projects searchable in a separate project library, including
   newly added projects and those awaiting a valid observation.
5. Explain what each project does through its original description and any
   stored research interpretation, with Star history and source information.
6. Organize projects by a clear product category: coding, research, browser and
   computer use, workflow automation, agent platforms, general agents, or AI
   infrastructure. Unclassified projects remain visible.
7. Export monitoring records as CSV, JSON, and SQLite. SQLite backups also
   include saved project interpretations.

## Navigation

The current product direction, agreed on September 7, 2026, uses a dashboard
homepage and a dedicated daily-discovery page. This replaces the earlier
catalogue-only homepage and its restrictions on separate overview/discovery
destinations.

| Destination | What the user does |
| --- | --- |
| Trends, `/` | The user scans charts and growth groups for a chosen date and period. |
| Daily discoveries, `/discoveries` | The user reads projects entering the radar on a selected date. |
| Project library, `/repositories` | The user searches, filters, and revisits the full monitored catalogue. |
| Agent categories, `/topics` | The user explores projects by purpose and supporting capability. |
| Add project, `/watch/new` | The operator registers a public project for observation. |
| Collection history, `/runs` | The operator checks freshness, failures, and completeness. |

Project and category detail pages retain the same navigation. My watchlist is
a focused view of the project library, not a separate copy of the data.

## Data collection

GitHub Search is the default discovery source. Queries cover recently created
projects with early interest, active projects, topic leaders, and mature
benchmarks. The GitHub repository API supplies absolute Star snapshots.

OSS Insight is an optional historical adapter and is disabled by default. The
system must remain useful when it is unavailable or returns no candidates.
Existing imports and manual additions remain supported.

Repository identity uses GitHub's permanent ID. Each monitored project gets one
daily success or failure outcome. Missing days remain missing; a growing
registry must not manufacture community growth in the charts.

## Comparisons users can trust

A selected period is 1, 7, or 30 calendar days ending on a chosen date. Growth
uses successful observations at both exact endpoints. Slowing momentum needs
three successful observations spanning two adjacent periods of equal length.

Fastest growth is the largest positive Star gain. Slow growth includes zero
and the smallest positive gains. Slowing momentum means the current period
gained fewer Stars than the preceding period; it does not necessarily mean
the total Star count fell.

The dashboard trend uses a fixed endpoint-comparable group, normalized to 100
at the starting date. Missing observations produce gaps. Sample rankings never
claim to be rankings across all of GitHub.

New discovery means new to this system. Repository creation time is a different
field. Stale observations show their actual date, and failed requests do not
appear as zero growth.

## Product boundaries

- The Go collector gathers public repository metadata, Star history, discovery
  provenance, and collection evidence. Commit, release, and code-activity
  analysis remain outside this iteration.
- Stored project interpretations are prepared outside the Web application and
  imported with source, model, time, and revision metadata. The Web application
  does not invoke an AI model or generate analyses.
- The current interpretation body is retained; prior bodies and interpretation
  CSV/JSON exports are not included in this iteration.
- Browsing is public. Adding projects is an operator capability: direct local
  access works on a loopback-bound server; public writes require HTTPS and a
  separate configured operator token. There are no personal accounts.
- Manual category decisions are preserved. Automated rules can leave a project
  unclassified when the evidence is insufficient.
- Star history is an attention signal for research. It does not establish
  demand, causality, investment quality, or full history before observation
  began.

## Experience

Use a dark navigation sidebar and a bright content area, with readable charts,
short project descriptions, and familiar filters. Chinese and English are
supported. The interface should make the next useful action obvious without
requiring the user to understand the collector or database.
