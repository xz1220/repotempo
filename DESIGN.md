# RepoTempo design system

The product uses a visual trend dashboard and a persistent project library.
Daily discoveries and My watchlist are library views. Primary navigation has
only Trends and GitHub projects (GitHub 项目). The library page heading remains
Project library (项目库).

## Product job

Help a researcher see where attention is moving, meet interesting new projects,
and keep observing them after discovery. A project detail must explain what
the project does before asking the reader to interpret its numbers.

The main reading tasks are scanning a trend and browsing projects. Today's
additions and previously observed projects share the library's filters and
records.

## Visual direction

- Use a dark fixed sidebar to anchor navigation and a bright main area for
  charts, descriptions, lists, and forms.
- Use restrained green/teal selection and action accents. Reserve semantic
  green, amber, and red for actual data or state.
- Keep the product recognizable through its repository identities, dated
  observations, charts, and clear categories.
- Preserve the Go templates, semantic HTML, local CSS, native JavaScript, and
  server-rendered SVG stack.

The design is a working research application. The homepage supports a quick
scan of two ranked lists; the library provides comfortable project reading,
and individual history charts remain on detail pages.

## App shell

The sidebar primary navigation contains only Trends and GitHub projects.
Collection history and the source-repository link stay in the utility area.
The dashboard uses category controls; the library uses searchable tags and
includes My watchlist as an in-page view.

The bright content header shows page context and language selection. The page
heading carries the data date. Add project appears only as a compact button
in the project-library toolbar. A stale-data notice links to collection history.

Navigation uses ordinary anchors with an active-page state. On narrow screens,
the shell reorganizes into compact navigation without root horizontal overflow
or hiding the primary destinations. Do not force a desktop sidebar beside
mobile content.

See [.design/github-radar/INFORMATION_ARCHITECTURE.md](.design/github-radar/INFORMATION_ARCHITECTURE.md)
for routes, labels, and content order.

## Page roles

| Route | What the page helps the user understand |
| --- | --- |
| `/` | The user sees fastest growth Top 10 and up to six largest momentum declines. |
| `/repositories` | The user reads a 20-card single-column feed, filters new entries, or opens My watchlist. |
| `/discoveries` | Old discovery links lead into the project library's new-entry view. |
| `/repositories/{id}` | The user learns what a project does and reviews its observed history. |
| `/topics` | The user explores product categories and classification coverage. |
| `/topics/{slug}` | The user examines the projects and observations behind one category. |
| `/watch/new` | The operator adds a public project, an optional category, and a note. |
| `/runs` | The operator checks collection outcomes and data completeness. |

## Trend dashboard

The homepage contains date/period/category controls and two lists: fastest
growth Top 10 as the main section, and up to six largest momentum declines as
the secondary section. Short bars may support the fastest-growth values.

Project identity, description, Stars, and real period change support scanning.
The smaller slowdown list compares current and preceding gains. Available
evidence determines row counts; empty groups explain missing comparisons.

Keep the selected scope, comparable sample, date range, and new-entry link as
compact context. Do not add a slow-growth group, performance distribution, or
aggregate attention-history curve. Method details stay expandable.

## New discoveries in the library

An unscoped library entry defaults to “当天新入库”: the actual current Shanghai
date, Stars order, and a 1-day comparison. An empty today remains empty, with
an explicit route to All projects instead of falling back to an older day.

The date and “仅看当天新入库” filter also support historical reading with any
explicit period or ordering. Preserve existing scoped links and the same
project records, descriptions, tags, and detail actions.

The date selector sets the observation date; the new-only filter restricts it
to that day's entries. GitHub creation time, when available, remains separate
in project details. Every discovered project is already monitored in the library.

An empty result offers clearing filters; users can also change the date or
disable the new-only filter. The toolbar's Add project button remains the
single addition entry. An empty date is not a collector error.

## Project library

Today’s additions, All projects, and My watchlist are tabs inside the library.
Search, tags, observation date, ordering, and the new-only filter describe the
current slice. There is no hierarchical topic selector, discovery-source
selector, or category-tab strip on this page.

Label the period control Compare growth (增长对比). Its choices are Against
1/7/30 days earlier (与 1/7/30 天前比), measured backward from the observation
date. Keep the endpoint date and comparison interval visible.

Use a single column of project cards on desktop and mobile, with 20 projects
per page. Within each card, the reading area leads and the metrics remain easy
to compare. New, untagged, awaiting-observation, and stale states stay visible.

Each card includes the original description and a separate saved brief with
its source and date. Label AI-generated and human/imported notes appropriately;
show an explicit not-reviewed state when no saved explanation exists.

Show Stars, period gain, growth rate, comparable-sample rank movement, entry
date, and separate detail/GitHub buttons. Load existing explanations for all
cards on a page in one database batch, without calling an external model.

Cursor pagination keeps the feed bounded. Sort and filter changes reset
continuation; browser Back and shared URLs preserve the selected context.

## Searchable project tags

Use a search input with available tag suggestions. Match exact labels after
trimming and case normalization; preserve Chinese text and unknown GitHub tags.
Options cover the full registry entered by the observation date.

Card tags combine native GitHub topics, archived research labels, and effective
classifications without duplicates. Show eight initially; a native expandable
section retains all remaining labels, including less frequent ones such as SaaS.

Every card and detail tag is a link to its exact library filter. Preserve date,
growth comparison, search, and view state while resetting the pagination cursor.
Tags are current saved attributes, not a history of past label assignments.

Old `topic` and `source` URLs remain supported. Show their active filters with
individual clear actions and preserve them as hidden form fields until cleared.
Compatibility does not restore the removed dropdown controls.

## Reading marks

Provide an explicit read/unread toggle per repository ID. Persist it only in
localStorage for the current browser profile and origin. The interface must
state that another browser, device, domain, scheme, or port has separate marks.

Do not mark read on opening, scrolling, or viewing an AI brief. A read mark is
independent of the saved-analysis state and must not hide projects or imply
that the server can filter the whole library by unread status.

Keep the last known state and explain a storage failure. The saved mark may
be reflected across tabs on the same browser origin; it is not account sync.

## Add project

Open the form from the compact project-library toolbar button. Do not repeat
the action in the sidebar, dashboard, or global page heading.

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
infrastructure. Skills, Memory, and Harness are second-level taxonomy entries
under infrastructure; coordination and workspace entries belong to platforms.

This hierarchy supports the trend dashboard and classification-rule pages.
It remains separate from the two raw tag fields; saving an unfamiliar author
label must not create a new category or overwrite a classification decision.

A parent category includes its children. Show unclassified projects explicitly.
Do not imply that every open-source project in the library is an AI agent.

Automatic assignment is a conservative suggestion. Generic words such as
skills or agent are not enough. Manual positive decisions and manual removal
remain protected.

## Project detail

Lead with identity, the original project description, and a clear GitHub link.
When a saved research interpretation exists, show its full summary,
capabilities, use cases, technical notes, and provenance beyond the card excerpt.

Show Star history, effective history start, clickable tags, and collection evidence
after the explanation. A missing analysis is a normal state, not a broken
page. The original GitHub description must never be labeled as AI research.

Interpretations are imported outside the Web interface. Show the source,
model, analysis time, and revision counter of the current body. Reading details
does not trigger model generation or a new on-demand LLM workflow.

Missing briefs may be prepared in explicit background batches, including reuse
of existing historical research. Preserve unknown source/model dates rather
than inventing attribution. Batch import fills empty summaries in the existing table.

Do not suggest that the full catalogue is already enriched or that automatic
daily brief generation is active. The current backfill scope and scheduled
Star collection are separate from the page’s reading behavior.

## Data semantics

- Growth compares successful observations at `D - W` and `D`, where `W` is
  1, 7, or 30 days. Missing endpoints produce no growth value.
- Slowing momentum compares gains in two adjacent equal windows. It requires
  successful observations at `D - 2W`, `D - W`, and `D`.
- Sample ranks are calculated within one scoped common cohort. They never
  imply a global GitHub rank.
- Individual project history uses its real observations and preserves missing
  intermediate dates as gaps.
- Show the date beside a last-known Star value. Never use it as an exact
  endpoint or as evidence of zero growth.
- Distinguish first discovery, GitHub creation time, and observation time.
- Native GitHub topics use `NULL` for not yet observed and `[]` for known empty.
  A full API response updates this field; 304 responses preserve it. Unknown
  topics require a full metadata request instead of a conditional one.
- Archived research tags survive native-topic refreshes. Schema version 5
  adds two JSON columns to `repositories`; it adds no table or tag-history model.
- The offline tag importer fills only unknown native lists, merges research
  tags, and clears the ETag for a newly filled native list. It uses no model.

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

- Use a line for individual history on detail pages and short horizontal bars
  for fastest-growth magnitudes. The dashboard is limited to its two lists.
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

Verify the Top 10/six-row limits, 20-card pagination, saved-brief source/date,
single-project chart values, exact tag filters, complete expanded tag sets,
legacy-filter clear actions, and manual-add paths. Inspect Chinese/English
layouts at 320, 375, 414, 768, and 1440px, including long names, sparse history,
stale data, absent explanations, true empty-today states, local read toggles,
storage failures, and browser Back.

This document defines the intended design and acceptance criteria. It does not
assert that screenshots, a fixed audit score, or a release have passed.

## Avoid

- Decorative marketing sections, invented growth numbers, and empty charts
  drawn from fabricated data.
- Treating a slowdown as a Star-count decline or missing data as zero growth.
- Adding aggregate attention charts or extra leaderboard groups to the homepage.
- Gradient text, neon glow, glass panels, or a rounded box around every label.
- Technical implementation details inside the normal reading flow.
- External fonts, imagery, or chart runtimes that are unnecessary for the task.
