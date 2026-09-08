# RepoTempo information architecture

Primary navigation has two reading modes: Trends and GitHub projects. The
library defaults to Today’s additions, with All projects and My watchlist as
sibling views. Tags, observation dates, and ordering remain library controls;
purpose-based categories remain on the trend dashboard.

## Site map

```text
RepoTempo
├── Trends /
├── GitHub projects /repositories
│   ├── Today’s additions /repositories?view=daily
│   ├── All projects /repositories?view=all
│   ├── My watchlist /repositories?view=focus
│   ├── Add project toolbar action /watch/new
│   ├── Project detail /repositories/{github_repository_id}
│   └── Category context /topics and /topics/{topic_slug}
└── Collection history /runs
```

Only Trends and GitHub projects appear in primary navigation. The library page
heading remains Project library (项目库).

Collection history is a utility; `/discoveries` is a compatibility entry for
the library's new-entry view. Health endpoints remain `/healthz` and `/readyz`.

## Navigation

A dark fixed desktop sidebar contains Trends and GitHub projects. Collection
history belongs to the utility area. The main content remains bright; its
header supplies page context and a language switch.

My watchlist appears only as a tab inside the project library. Add project
appears only as a compact button in that page's toolbar, opening `/watch/new`.
Neither is repeated in the sidebar or global heading.

The sidebar reorganizes on narrow screens. Main destinations remain reachable
without root horizontal scrolling. Navigation links are ordinary anchors with
active-route states, not simulated application tabs.

Project and category details include a breadcrumb. Date, growth comparison, tags, and
list filters use URL state so links remain useful after refresh, sharing, and
browser Back.

## Trends

The homepage answers which observed projects are moving now and where to look
next. It presents information in this order:

1. The selected data date, period, and category.
2. Compact scope/comparable-sample context and the selected date's new-entry link.
3. The fastest-growth Top 10 as the main list.
4. Up to six projects with the largest momentum declines as the secondary list.
5. An expandable comparison explanation and links to project details.

The homepage has no slow-growth group, performance distribution, or aggregate
attention-history chart. Both lists use real comparable observations; a group
without enough evidence explains its empty state. Individual history charts
remain on project details.

## New discoveries in the library

An unscoped library visit starts with the real current Shanghai date, Stars
order, and a 1-day comparison. No data today produces a real empty state and
an All projects link; an older populated date must not be substituted.

The date selector and “仅看当天新入库” filter support other dates, periods,
and orders in the same card feed. Explicit shared-link scopes remain intact.

Each card leads with what the project does and uses the same saved-brief and
metric layout as the rest of the library. Entry date and GitHub creation time
remain distinct; a newly discovered project is not necessarily newly created.

Discovered projects are already registered for continued monitoring. An empty
date offers another date or clearing the new-only filter. The library toolbar
retains the single Add project action.

## Project library

The library is the durable home for every tracked project. Today’s additions,
All projects, and My watchlist are in-page tabs. Search, tags, observation date,
growth comparison, ordering, and the new-only filter narrow the collection.

Use a searchable tag field, with available values from the full registry
entered by the selected date. Do not show hierarchical topic or discovery-source
dropdowns, or a category-tab strip, on the project-library page.

Desktop and mobile use one column of cards, with 20 projects per page. Each
card contains the original description, an existing AI or human brief with
source and date, Stars, gain, growth rate, comparable-sample ranks, entry date,
clickable tags, and distinct detail/GitHub buttons.

Card labels combine native topics, archived research tags, and effective
classifications without duplicates. Preview eight labels and retain every
additional label in an expandable section. Details expose the complete set.

A tag link selects one exact normalized tag and clears the pagination cursor.
It retains the current observation date, growth comparison, search, and view.

The current page's saved explanations are read together in one database batch.
Browsing never calls an external model. Missing explanations have an explicit
not-reviewed state; new, pending, stale, and unclassified projects stay visible.

Cursor continuation supports a large collection without loading an unbounded
feed. Changing sort or filters starts a new traversal. The selected date and
view state survive refresh and browser Back.

## Reading marks

A read/unread toggle records an explicit user decision for a permanent
repository ID. It is stored in this browser profile’s localStorage for the
current origin and never sent to the server.

Other devices, browsers, profiles, or origins have separate marks. Same-origin
tabs can reflect a changed mark. Opening a page, scrolling, or loading an
explanation does not mark a project read.

The mark is separate from whether a saved brief exists. It does not remove
cards or support a whole-library unread filter. Storage errors preserve the
last known state and receive a visible explanation.

## Add project

The compact library-toolbar button opens `/watch/new`. The form accepts a
GitHub URL or owner/name, an optional category, and a note. Submission uses
`POST /watch`.

The server resolves the public repository through GitHub, records its permanent
ID and an initial absolute Star observation, and opens the project detail.
Adding an existing project preserves its existing note and classification.

Without an explicit category, the system can add conservative automatic
suggestions. A selected category is a manual decision.

Direct requests to a loopback-bound local server can add projects. A public
deployment requires HTTPS and a configured operator token. Unavailable write
access and recoverable GitHub errors have explicit form states.

## Project detail

Content order:

1. Repository identity, GitHub link, original description, and state.
2. The complete saved research interpretation, when available, with its provenance.
3. Current observed scale and comparable period changes.
4. The individual project's Star-history chart, clickable tags, entry date, and history start.
5. Exact observations and collection evidence.

A saved interpretation explains capabilities and use cases. Without one, the
page still explains the project using its original description and clearly
marks further research as unavailable.

Codex or a researcher prepares interpretations outside the Web interface. The
current body retains source, model, time, and revision; the revision counter
does not imply that earlier bodies remain available. Card excerpts and detail
pages use the same saved analysis, with no on-demand page-triggered LLM pipeline.

Explicit background work can prepare missing briefs and reuse old research
cards. A strict transactional batch import fills empty summaries while
preserving existing ones in the current analysis table.

Current brief backfilling does not establish full-library coverage or an
automatic daily generation schedule. Historical backfill beyond the selected
scope remains a separate user decision.

## Agent categories

The roots are coding agents, research agents, browser/computer agents, workflow
agents, agent platforms, general agents, and AI infrastructure. Supporting
taxonomy entries retain a two-level hierarchy for trend/category pages.

These classifications remain separate from raw GitHub and research labels.
Their valid names and slugs are searchable as labels for compatibility;
unfamiliar raw labels do not create taxonomy entries.

Category pages show classification coverage and comparable sample sizes. A
parent includes direct child assignments; a child selection remains scoped to
that child. Unclassified is an explicit filter.

A project can have several categories. Manual assignments and manual removals
are protected against later automatic suggestions. Generic words such as
skills, agent, or browser automation alone do not establish an AI product type.

## Category detail and collection history

Category detail shows what the category means, the repositories behind it, and
its observed trend. It keeps the fixed-cohort and exact-endpoint rules visible
where they affect interpretation.

Collection history shows run outcomes, successful and failed counts, source
warnings, rate-limit evidence, and search completeness. Stale-data notices
link here. It is an operational utility rather than a reading-mode tab.

## Comparison model

The library control is Compare growth (增长对比), with Against 1/7/30 days
earlier (与 1/7/30 天前比) choices. Let `D` be the observation-date endpoint
and `W` the selected number of days.

| Measure | Meaning |
| --- | --- |
| Current-period gain | Stars at `D` minus Stars at `D-W`, using two successful exact observations. |
| Previous-period gain | Stars at `D-W` minus Stars at `D-2W`, using two successful exact observations. |
| Momentum change | Current-period gain minus previous-period gain, requiring all three dates. |
| Growth rate | Current-period gain divided by baseline Stars, when baseline Stars are positive. |
| Daily velocity | Current-period gain divided by the window length. |
| Sample rank change | Baseline rank minus endpoint rank within the same scoped cohort. |
| New on date | The repository first entered this system on the selected date. |

Fastest growth uses the largest positive gain. Slowing momentum requires
negative momentum change; the project may still have gained Stars.

A failed or missing observation is never zero. Last-known values show their
actual date and do not replace exact comparison endpoints.

Tags, compatible topic/source filters, monitoring state, and focus define a comparison scope.
Text search and the new-only display filter do not turn a matching project
into rank one. Ranks always refer to the monitored sample.

Tags are current saved metadata, not historical tag snapshots. The selected
date limits project entry and Star observations, not the version of a project's
labels. Tag-option counts refer to the entered registry rather than the current page.

## Saved label fields

Schema version 5 adds only `github_topics_json` and `research_tags_json` to
`repositories`. No tag table is created. Category definitions and relationships
retain their existing purpose and manual protections.

Native-topic `NULL` means unknown; an empty array means successfully observed
empty. Full 200 metadata responses update native topics, 304 responses preserve
them, and unknown native topics require a full request rather than an ETag-only check.

Research tags remain separate and survive native refreshes. `tags import-batch`
fills only unknown native lists, merges research tags, and clears the ETag when
filling a native list. The import is transactional and invokes no model or network.

## Common reading sessions

A daily review begins on Trends. The user chooses a window, scans the Top 10
and up to six momentum declines, then opens a detail or the library's card feed.

Reading new projects begins with Today’s additions in the project library.
The user can change the date/order, explicitly mark individual projects read,
or switch to All projects and My watchlist.

An external recommendation begins with the library's Add project button.
After GitHub validation, the detail is available with a real initial observation
and the project appears in the library's My watchlist tab.

A category review begins with a category filter or contextual link. The user
compares projects serving a similar purpose and can inspect incomplete
classification or observation coverage.

## Labels

| Concept | Chinese | English |
| --- | --- | --- |
| Main trend view | 趋势看板 | Trends |
| New-entry filter | 仅看当天新入库 | New on selected date |
| Default library view | 当天新入库 | Today’s additions |
| Persistent catalogue | 项目库 | Project library |
| Sidebar project entry | GitHub 项目 | GitHub projects |
| Exact project attribute filter | 标签 | Tag |
| Star comparison window | 增长对比 | Compare growth |
| Purpose-based taxonomy | Agent 分类 | Agent categories |
| Operator addition | 添加关注 | Add project |
| Focused projects | 我的关注 | My watchlist |
| Local reading decision | 标为已读 / 标为未读 | Mark read / Mark unread |
| No supported classification | 待分类 | Unclassified |
| First local registration | 首次发现 / 入库日期 | First discovery |
| Actual GitHub creation | 创建于 | Created |
| Largest positive period gains | 涨得最快 | Fastest growth |
| Lower gains than the previous window | 势头回落 | Losing momentum |
| Collector evidence | 采集历史 | Collection history |
| Saved card reading | AI 简读 / 已保存的项目说明 | AI reading brief / Saved project note |
| Full detail reading | 项目解读 | Project interpretation |

## Components and growth

The sidebar, page header, growth controls, tag controls, project identity,
state labels, chart frames, and empty-state patterns are shared.

The library uses a single-column card feed, while the dashboard uses compact
ranked rows. Saved briefs keep source and date visible. Forms remain short;
the full explanation and individual history belong on details.

The project library, including its new-entry view, uses 20-project cursor pages.
Existing analyses are fetched once for the current page. Collection history
may retain its smaller offset-paginated log. Detail charts reduce tick density
before compromising date labels or exact-value access.

## URLs and compatibility

- `/` is the trend dashboard; an unscoped `/repositories` defaults to today's
  new entries in the Shanghai calendar, ordered by Stars with a 1-day period.
- Library views use `view=daily|all|focus`. Explicit older links retain their
  date, filters, and comparison scope instead of silently becoming today-only.
- `/discoveries` is a compatibility entry into the library with `new=1`;
  existing date, sort, and other filters are preserved, and `cursor` is cleared.
  Missing sort and period default to `sort=stars` and `period=1d`.
- Old root catalogue links with search, tags, cursor, or `new=1` can redirect to
  `/repositories` while retaining their query values.
- Browsing state uses `date`, `period=1d|7d|30d`, `sort`, `tag`, `topic`, `q`,
  `source`, `status`, `focus=1`, `new=1`, and `cursor` where applicable.
- Old `topic` and `source` restrictions remain visible as individually clearable
  active-filter badges. Hidden inputs preserve them on form submission until
  cleared; neither receives a dropdown in the library.
- Language uses `lang=zh-CN|en` and the existing same-site preference cookie.
- Operator credentials never appear in query strings.
- Reading marks use localStorage by repository ID, not query parameters or a
  server-side unread filter.
