# RepoTempo

RepoTempo helps a researcher discover trending GitHub projects and keep
following them after the first report. It combines a daily project archive,
continuous Star observations, and a visual trend dashboard.

## Users and recurring decisions

The first user is a technical researcher and investor who follows open-source
projects on desktop, checks selected projects on mobile, and exports data for
deeper research and personal open-source project selection.

The daily session starts with the fastest-growing projects and those losing
the most momentum compared with the previous period. The user then reads the
project library for context, opens a detail, or adds a repository to follow.

## Core capabilities

1. Show two dashboard lists: the fastest-growth Top 10 and up to six projects
   with the largest momentum declines. Keep the selected scope and comparable
   sample count visible.
2. Archive newly discovered projects by their first entry into the radar.
   Discovery automatically registers projects for continued collection.
3. Let an operator add a public GitHub URL or owner/name from the Web interface,
   save an optional note and category, and see the project with its first real
   Star snapshot immediately.
4. Present the project library as a single-column reading feed, paginated at
   20 projects per page, including new and awaiting-observation projects.
5. Each card combines the original description, any saved AI or human brief
   with source and date, Stars, gain, growth rate, comparable-sample ranks,
   entry date, and detail/GitHub links. Details retain the full saved analysis
   and an individual project's Star-history chart.
6. Organize projects by a clear product category: coding, research, browser and
   computer use, workflow automation, agent platforms, general agents, or AI
   infrastructure. Unclassified projects remain visible.
7. Export monitoring records as CSV, JSON, and SQLite. SQLite backups also
   include saved project interpretations.

## Navigation

Primary navigation contains only Trends and Project library. The dashboard
remains the homepage. Daily discoveries are reviewed inside the library using
the date and “new on selected date” filter, together with any comparison period
and ordering, including Stars or growth.

| Destination | What the user does |
| --- | --- |
| Trends, `/` | The user scans fastest growth and largest momentum declines for a chosen date and period. |
| Project library, `/repositories` | The user reads 20 project cards per page, filters new entries, or switches to My watchlist. |

My watchlist is a tab within the project library. Add project appears once as
a compact library-toolbar action opening `/watch/new`; neither is repeated in
the sidebar or global page heading.

Category filters and contextual category pages support both reading modes.
Collection history remains a utility. Existing detail and form routes retain
the same two-item primary navigation.

## Data collection

GitHub Trending is the primary discovery source. Capture the all-language daily,
weekly and monthly boards once per daily run; preserve page evidence separately
from API Star snapshots. Verify repository IDs through GitHub and keep following
projects after they leave the boards. RepoTempo is independent of GitHub.

GitHub Search supplies supplementary coverage. Queries cover recently created
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

Fastest growth is the largest positive Star gain. Slowing momentum means the
current period gained fewer Stars than the preceding period; it does not
necessarily mean the total Star count fell.

Sample rankings compare the same endpoint-comparable repositories and never
claim to cover all of GitHub. Individual history charts preserve missing days
as gaps; the homepage does not aggregate them into an attention curve.

New discovery means new to this system. Repository creation time is a different
field. Stale observations show their actual date, and failed requests do not
appear as zero growth.

## Product boundaries

- The Go collector gathers public repository metadata, Star history, discovery
  provenance, and collection evidence. Commit, release, and code-activity
  analysis remain outside this iteration.
- Stored project interpretations are prepared outside the Web application and
  imported with source, model, time, and revision metadata. Cards load the
  existing interpretations for the current page in one database batch. Reading
  a card or detail does not invoke an external model; there is no new on-demand
  LLM generation pipeline.
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
