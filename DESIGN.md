# GitHub Radar design system

This document describes the current product direction from September 7, 2026:
a visual trend dashboard, a daily-discovery archive, and a persistent project
library. It supersedes the earlier catalogue-only homepage.

## Product job

Help a researcher see where attention is moving, meet interesting new projects,
and keep observing them after discovery. A project detail must explain what
the project does before asking the reader to interpret its numbers.

The three main reading tasks are scanning a trend, browsing today's additions,
and finding a previously observed project. Each has its own page.

## Visual direction

- Use a dark fixed sidebar to anchor navigation and a bright main area for
  charts, descriptions, lists, and forms.
- Use restrained green/teal selection and action accents. Reserve semantic
  green, amber, and red for actual data or state.
- Keep the product recognizable through its repository identities, dated
  observations, charts, and clear categories.
- Preserve the Go templates, semantic HTML, local CSS, native JavaScript, and
  server-rendered SVG stack.

The design is a working research application. It needs useful charts and
comfortable reading space as well as exact tables.

## App shell

The sidebar groups Trends, Daily discoveries, Project library, and Agent
categories. My watchlist and Add project are easy to reach; collection history
and the source-repository link stay in the utility area.

The bright content header shows page context and language selection. The page
heading carries the data date and an Add project action where appropriate.
A stale-data notice links to collection history.

Navigation uses ordinary anchors with an active-page state. On narrow screens,
the shell reorganizes into compact navigation without root horizontal overflow
or hiding the primary destinations. Do not force a desktop sidebar beside
mobile content.

See [.design/github-radar/INFORMATION_ARCHITECTURE.md](.design/github-radar/INFORMATION_ARCHITECTURE.md)
for routes, labels, and content order.

## Page roles

| Route | What the page helps the user understand |
| --- | --- |
| `/` | The user sees overall movement, fastest growth, slow growth, and slowing momentum. |
| `/discoveries` | The user reads projects first discovered on a selected date. |
| `/repositories` | The user searches and compares the full monitored project library. |
| `/repositories/{id}` | The user learns what a project does and reviews its observed history. |
| `/topics` | The user explores product categories and classification coverage. |
| `/topics/{slug}` | The user examines the projects and observations behind one category. |
| `/watch/new` | The operator adds a public project, an optional category, and a note. |
| `/runs` | The operator checks collection outcomes and data completeness. |

## Trend dashboard

The first screen combines date/period/category controls, a small number of
meaningful counts, and a large attention-history chart. It should be possible
to scan the result without starting with a dense data table.

The chart compares the same group of repositories across the chosen interval,
with the first observation normalized to 100. A companion distribution shows
positive, zero, and negative Star changes among comparable projects.

Below the charts, three distinct groups show fastest growth, slow or zero
growth, and slowing momentum. Each row includes project identity and a real
number. Short bars help compare magnitude; clicking a project opens its detail.

Counts and chart labels identify the date range and comparable sample.
Operational details belong in an expandable explanation or collection history,
not in the primary reading path.

## Daily discoveries

Use readable project cards or records with the repository name, description,
category, observed Stars, and a clear detail link. This page is a reading
surface, not another leaderboard.

The date selector refers to entry into this system. When GitHub creation time
is available, show it separately. Every discovered project is already in the
library for continued monitoring.

An empty day explains that no projects entered the radar on that date and
offers date navigation or Add project. Do not imply a collector error merely
because a date has no additions.

## Project library

Search, category, period, date, ordering, and the watchlist filter describe the
current slice. Keep advanced source and monitoring-state controls secondary.

Desktop rows align numbers and leave enough room for descriptions. Mobile uses
readable records rather than compressing a wide table. New, unclassified,
awaiting-observation, and stale states remain visible.

Cursor pagination supports a growing library. Sort and filter changes reset
continuation; browser Back and shared URLs preserve the selected context.

## Add project

Accept a public GitHub URL or owner/name. The category and personal note are
optional. On success, open the detail page with real repository metadata and
the initial Star observation.

The form must preserve useful input after recoverable validation errors. It
must explain inaccessible projects, GitHub rate limits, and unavailable write
access without exposing internal errors or credentials.

Direct loopback access is convenient for local use. A published deployment
requires HTTPS and its operator token for writes. The token is never echoed
back into the page or persisted as project data.

## Categories

Root categories describe product purpose: coding, research, browser/computer
operation, workflow automation, agent platforms, general agents, and AI
infrastructure. Components such as Skills, Memory, and Harness are supporting
tags under infrastructure; coordination and workspace tags belong to platforms.

A parent category includes its children. Show unclassified projects explicitly.
Do not imply that every open-source project in the library is an AI agent.

Automatic assignment is a conservative suggestion. Generic words such as
skills or agent are not enough. Manual positive decisions and manual removal
remain protected.

## Project detail

Lead with identity, the original project description, and a clear GitHub link.
When a saved research interpretation exists, give its summary, capabilities,
use cases, and provenance useful reading space.

Show Star history, effective history start, category, and collection evidence
after the explanation. A missing analysis is a normal state, not a broken
page. The original GitHub description must never be labeled as AI research.

Interpretations are imported outside the Web interface. Show the source,
model, analysis time, and revision counter of the current body.

## Data semantics

- Growth compares successful observations at `D - W` and `D`, where `W` is
  1, 7, or 30 days. Missing endpoints produce no growth value.
- Slowing momentum compares gains in two adjacent equal windows. It requires
  successful observations at `D - 2W`, `D - W`, and `D`.
- Slow growth includes zero and the smallest nonnegative gains. A decline in
  cumulative Stars is a separate state.
- Sample ranks are calculated within one scoped common cohort. They never
  imply a global GitHub rank.
- Attention history fixes the endpoint-comparable group. If a member lacks an
  intermediate observation, preserve a chart gap.
- Show the date beside a last-known Star value. Never use it as an exact
  endpoint or as evidence of zero growth.
- Distinguish first discovery, GitHub creation time, and observation time.

## Typography and tokens

Production tokens live in `internal/web/static/tokens.css`. Semantic roles
cover light content surfaces, dark navigation, text, borders, selection, focus,
warning, positive/negative values, and data series.

Use one system sans stack with Chinese fallbacks. Native monospace is reserved
for repository names, identifiers, dates, ranks, and measured values. Numbers
use tabular spacing; long names wrap safely.

Spacing follows the shared 4px-based scale. Controls use small radii and clear
boundaries. Separate sections with spacing and rules before adding more
containers or shadows.

## Charts and interaction

- Use a line for history, horizontal bars for ranked magnitudes, and a compact
  distribution chart for positive/zero/negative groups.
- Identify the unit and period. Do not mix absolute Stars and growth on a dual
  axis or present whole-registry accumulation as community growth.
- Provide exact values through an adjacent or expandable table.
- Positive, negative, zero, missing, stale, and unavailable comparisons must
  remain distinguishable without color.
- Motion communicates focus, selection, expansion, and submission state.
  Respect reduced-motion preferences.
- Provide visible keyboard focus, semantic labels, sufficient contrast, and
  useful loading, disabled, error, empty, and success states.

## Implementation and verification

Keep business logic outside templates. GET forms and anchors express browsing
state; the protected Add project form is the scoped Web write path.

Verify charts against repository observations, check manual-add success and
failure paths, and inspect Chinese/English layouts at 320, 375, 414, 768, and
1440px. Check long project names, sparse history, stale data, and browser Back.

This document defines the intended design and acceptance criteria. It does not
assert that screenshots, a fixed audit score, or a release have passed.

## Avoid

- Decorative marketing sections, invented growth numbers, and empty charts
  drawn from fabricated data.
- Treating a slowdown as a Star-count decline or missing data as zero growth.
- Treating newly added projects as a rise in the fixed-cohort trend.
- Gradient text, neon glow, glass panels, or a rounded box around every label.
- Technical implementation details inside the normal reading flow.
- External fonts, imagery, or chart runtimes that are unnecessary for the task.
