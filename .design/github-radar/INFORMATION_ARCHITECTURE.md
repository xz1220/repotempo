# Information architecture: GitHub Radar

## Site map

- Project monitoring `/` (canonical home)
  - Compatibility collection route `/repositories`
  - Project detail `/repositories/{github_repository_id}`
- Topics `/topics`
  - Topic detail `/topics/{topic_slug}`
- Collection history `/runs` (utility destination)
- Former discoveries route `/discoveries` → `/?new=1`
- Service endpoints `/healthz`, `/readyz`

The application has one stable app bar and contextual controls within each
route. It does not need a sidebar, command palette, separate overview, or third
navigation hierarchy.

## Navigation model

- **Primary navigation:** Projects and Topics, in that order. Projects points to
  the canonical `/` route.
- **Contextual controls:** URL-backed period, sort, date, filters, cursor, and
  section links that change or narrow the current view.
- **Utility navigation:** Collection history, language switch, and product
  identity. Collection history stays available without presenting operational
  evidence as a primary research mode.
- **Object orientation:** project and topic detail pages add one breadcrumb back
  to their collection page.
- **Compatibility:** `/repositories` renders the same catalogue as `/` so old
  links continue to work. `/discoveries` redirects to the canonical catalogue
  with `new=1` and preserves other applicable query values.
- **Mobile:** the single app bar may wrap or reduce spacing. Core links are at
  least 44px tall where possible; compact language, tag, and breadcrumb links
  remain legible and keyboard-visible. Labels stay intact, and the page never
  gains root horizontal overflow.

## Content hierarchy

### Project monitoring (home)

1. Period and ordering: 1 / 7 / 30 days; momentum, rank change, Star stock, or
   growth rate.
2. Scope: search, hierarchical topic, endpoint date, and new-on-date switch;
   source and monitoring state are advanced filters.
3. Comparison coverage: exact baseline and endpoint dates, scoped repository
   count, successfully observed count, comparable count, and newly discovered
   count.
4. Project catalogue: identity, new badge, current Star, period change, growth
   rate, daily velocity, monitored-sample rank transition, and topics.
5. Cursor continuation and empty-state recovery.

There is no separate aggregate overview. Since the monitored registry grows
monotonically, registry-wide total Stars and raw daily totals are not the
primary decision signal.

### Project detail

1. Breadcrumb, repository identity, GitHub link, and state.
2. Persisted project interpretation: Chinese summary, key points, use cases,
   technical notes, and analysis provenance.
3. Current scale and short-window changes.
4. Discovery, topic, language, valid-history, and rename provenance.
5. Absolute Star history and available OSS Insight rank evidence.
6. Failed observations and exact snapshot table.

When no stored analysis exists, the page states that interpretation is pending
and falls back explicitly to a manual note or GitHub description. The collector
does not create this analysis; Codex or a researcher prepares it offline and an
import workflow persists it for read-only display.

### Topics

1. Classification coverage: monitored, classified, unclassified, and coverage
   percentage using current active assignments.
2. Period selector and hierarchy-aware topic growth ranking; every delta names
   its exact-endpoint comparable count.
3. Exact topic metrics with parent and child roles visible.
4. Guidance when a period lacks a strict baseline.

A selected parent includes repositories assigned to the parent or any direct
child. A selected child contains only that child. The current model remains a
two-level taxonomy; unknown repositories are not forced into an invented topic.

### Topic detail

1. Breadcrumb and topic identity.
2. Rolled-up scale, growth, concentration, and optional leader exclusion.
3. Topic Star history for one fixed first-date/endpoint cohort, with incomplete
   intermediate dates preserved as gaps.
4. Representative projects and exact repository metrics.

### Collection history

1. Latest successful snapshot date and current coverage.
2. Run outcomes and counts.
3. Failure targets, API quota, and search-integrity evidence.
4. Pagination through older operational evidence.

Collection history is reached from a compact utility link, not a top-level tab.

## Comparison model

For an endpoint `D` and a selected window `W` in `{1, 7, 30}`, the baseline is
the exact calendar date `D - W`.

- **Scope count:** repositories remaining after topic, discovery-source, and
  monitoring-state scope is applied.
- **Observed count:** scoped repositories with a successful observation on `D`.
- **Comparable count:** scoped repositories with successful observations on
  both `D - W` and `D`.
- **New count:** scoped, endpoint-observed repositories whose local first-seen
  date is `D`.
- **Star delta:** endpoint Stars minus baseline Stars for a comparable
  repository.
- **Growth rate:** Star delta divided by baseline Stars when the baseline is
  greater than zero.
- **Daily velocity:** Star delta divided by `W`.
- **Sample rank:** `DENSE_RANK` by Star count across the same comparable scoped
  cohort at each endpoint.
- **Rank change:** baseline rank minus endpoint rank; a positive number means
  movement upward.

Failed, missing, or non-exact endpoint observations never become zero and never
borrow a nearby value. Text search and the new-only switch narrow what is shown
after cohort ranks have been calculated, preventing a search result from being
relabelled rank one.

## Critical user flows

### Follow previously discovered projects

1. Open Projects at `/`.
2. Select the endpoint date and 1 / 7 / 30-day window.
3. Choose momentum, rank change, Star stock, or growth-rate ordering.
4. Narrow by a parent topic, child topic, or Unclassified when useful.
5. Continue through the stable cursor and open a project detail without losing
   the meaning of the comparison URL.

### Review today's additions without losing historical context

1. Open Projects and enable “new on this date”, or follow an old
   `/discoveries` link.
2. Read the new badge in the same catalogue used for existing projects.
3. Disable the filter to compare those projects with the monitored population.

### Investigate one project

1. Open a project from the monitoring catalogue.
2. Read the stored interpretation and its provenance before relying on the
   GitHub description alone.
3. Compare current scale, Star history, and any rank evidence.
4. Check discovery source, topics, valid-history start, rename history, and
   failed dates.
5. Use browser Back to return to the URL-backed catalogue state.

### Validate the dataset

1. Open History from the app-bar utility.
2. Check coverage and the latest successful date.
3. Inspect a partial or failed run.
4. Review target failures, quota evidence, and search completeness.

## Naming conventions

| Concept | Chinese label | English label | Notes |
| --- | --- | --- | --- |
| canonical project collection | 项目监测 | Project monitoring | `/`; not “Overview” |
| monitored GitHub repository | 项目 | Project | Use “repository” only in GitHub-specific evidence |
| taxonomy category | 主题 | Topic | Preserve configured topic names verbatim |
| parent scope | 含子主题 | Includes child topics | Parent metrics and filters roll up direct children |
| no active topic assignment | 未分类 | Unclassified | Explicit state and filter; never silently hidden |
| first entry into the registry | 首次发现 | First discovery | Never imply repository creation time |
| new-on-date state | 新发现 | New | Row badge/filter, not a primary route |
| collector execution evidence | 采集历史 | Collection history | `/runs`; “run” remains a record-level term |
| cumulative GitHub stars | Star | Star | Do not translate the GitHub metric name |
| exact-endpoint period change | 增长 / 变化 | Growth / change | Always name the window |
| cohort-relative rank | 监测样本内排名 | Monitored-sample rank | Never imply all-GitHub rank |
| stored repository explanation | 项目解读 | Project interpretation | Show source, revision, and analysis date |

## Component reuse map

| Component | Used on | Variations |
| --- | --- | --- |
| Single app bar | all HTML pages | active primary route, history state, and language |
| Page heading | all product routes | object detail adds breadcrumb |
| View options | Project monitoring and Topics | period or ordering; state represented in URL |
| Filter toolbar | Project monitoring | GET form with progressive advanced filters |
| Cohort summary | Project monitoring | exact dates and comparison coverage |
| Trend catalogue | Project monitoring | desktop table and mobile record list |
| Analysis block | Project detail | stored interpretation or explicit fallback state |
| Chart frame | project and topic detail | chart type and exact-value alternative vary |
| Evidence table/list | projects, topics, and runs | columns and mobile reduction vary by task |
| Empty state | every data surface | explains why and gives the next useful action |

## Content growth plan

- Projects use opaque keyset cursors, capped server-side page sizes, and a fixed
  endpoint date so a growing registry does not reshuffle an active traversal.
- Run history retains offset pagination because it is an append-only operational
  log with a much smaller interaction surface.
- Topics remain a two-level taxonomy; parent, child, and unclassified roles stay
  visible while taxonomy expansion and backfill happen separately.
- Wide evidence tables gain task-specific mobile reductions, not global
  horizontal page scrolling.
- Historical charts aggregate or reduce ticks before rendering more points.
- Analysis storage keeps source, model, revision, and timestamps so regenerated
  interpretations can be audited instead of silently replacing provenance.

## URL strategy

- Canonical project monitoring is `/`; `/repositories` remains compatible.
- Resource patterns remain `/repositories/{id}` and `/topics/{slug}`.
- Project state uses `period=1d|7d|30d`, `sort`, `date`, `q`, `topic`, `source`,
  `status`, `new=1`, and an opaque `cursor` query parameter.
- The Unclassified topic filter uses a reserved value and is presented through a
  human label; users do not need to understand its storage representation.
- Topic ranking period, leader exclusion, run-history continuation, and language
  selection also use query parameters and survive refresh, sharing, and browser
  back.
- Navigation uses anchors and GET forms, not JavaScript-only state.
- Language may be selected through `?lang=zh-CN|en` and persisted by the
  existing same-site cookie.
