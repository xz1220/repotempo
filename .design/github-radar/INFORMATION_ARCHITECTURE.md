# Information architecture: GitHub Radar

## Site map

- Overview `/`
- Projects `/repositories`
  - Project detail `/repositories/{github_repository_id}`
- Topics `/topics`
  - Topic detail `/topics/{topic_slug}`
- New discoveries `/discoveries`
- Collection status `/runs`
- Service endpoints `/healthz`, `/readyz`

The application has two navigation levels: stable product routes and contextual
controls within one route. It does not need a sidebar, command palette, or third
navigation hierarchy.

## Navigation model

- **Primary navigation:** Overview, Projects, Topics, New discoveries,
  Collection status. Maximum five items and stable across every route.
- **Secondary navigation:** URL-backed segmented controls, filters, pagination,
  and section links that change or narrow the current view.
- **Utility navigation:** language switch and product identity. Utilities never
  compete with product routes.
- **Object orientation:** project and topic detail pages add a breadcrumb back
  to their collection page.
- **Mobile:** compact utility header plus menu-controlled route list. Primary
  controls and route links are at least 44px tall; compact language, tag, and
  breadcrumb links remain legible and keyboard-visible. Labels stay on one
  line, and the page never gains root horizontal overflow.

## Content hierarchy

### Overview

1. Freshness and coverage: determines whether all following signals are usable.
2. Daily comparable Star growth: the primary trend signal.
3. Fastest projects: names the projects driving the movement.
4. Latest collection status: exposes the nearest operational caveat.
5. Secondary totals and methodology: useful context, visually quieter.

### Projects

1. Search, topic, source, and monitoring-state controls.
2. Result count with the selected values visible in their controls.
3. Comparison table: project, topic, current Star, 1 / 7 / 30-day change,
   first source, first seen.
4. Pagination and empty-state recovery.

### Project detail

1. Breadcrumb and repository identity.
2. Current scale, growth, monitoring state, first seen, and valid history start.
3. Absolute Star history and OSS Insight rank evidence.
4. Discovery, topic, and rename provenance.
5. Failed observations and exact snapshot table.

### Topics

1. Period selector and leaf-topic growth ranking.
2. Exact hierarchy-aware topic metrics table.
3. Empty comparison guidance when the selected period has no baseline.

### Topic detail

1. Breadcrumb and topic identity.
2. Scale, growth, concentration, and optional leader exclusion.
3. Topic Star history.
4. Representative projects and exact repository metrics.

### New discoveries

1. First-discovery source composition.
2. Search-profile evidence and query completeness.
3. Explicit distinction between first discovery, repository creation, and later
   rediscovery.

### Collection status

1. Latest successful snapshot date and current coverage.
2. Run outcomes and counts.
3. Failure targets, API quota, and search-integrity evidence.
4. Pagination through older evidence.

## Critical user flows

### Find a rising technical direction

1. Open Overview and confirm coverage.
2. Inspect the growth signal and fast projects.
3. Open Topics, choose 1 / 7 / 30 days, and select a leaf topic.
4. Inspect representative projects and open one project detail.

### Investigate one project

1. Open Projects and apply URL-backed filters.
2. Compare absolute Star and period growth.
3. Open the project detail.
4. Check history, first source, topic assignments, and failed dates.
5. Use the browser Back action to return to the URL-backed filtered project list. A future return-link enhancement may carry that query explicitly.

### Validate the dataset

1. Open Collection status.
2. Check coverage and latest successful date.
3. Expand a partial or failed run.
4. Review target failures, quota evidence, and search completeness.

## Naming conventions

| Concept | Chinese label | English label | Notes |
| --- | --- | --- | --- |
| monitored GitHub repository | 项目 | Project | Use “repository” only in GitHub-specific evidence |
| taxonomy category | 主题 | Topic | Preserve configured topic names verbatim |
| first entry into the registry | 首次发现 | First discovery | Never imply repository creation time |
| discovery evidence | 新发现 | New discoveries | Route `/discoveries` |
| collector execution evidence | 采集状态 | Collection status | Route `/runs`; “run” remains a record-level term |
| cumulative GitHub stars | Star | Star | Do not translate the GitHub metric name |
| period change | 增长 / 变化 | Growth / change | Always name the period |

## Component reuse map

| Component | Used on | Variations |
| --- | --- | --- |
| App shell | all HTML pages | active route and language only |
| Page heading | all product routes | object detail adds breadcrumb |
| Segmented control | Topics and future same-view modes | state always represented in URL |
| Filter toolbar | Projects and future discovery filters | GET form, responsive grouping |
| Metric rail | Overview and detail summaries | density varies; semantics do not |
| Chart frame | Overview, project, topic, discoveries | chart type and data table alternative |
| Evidence table/list | Projects, topics, discoveries, runs | columns and mobile reduction vary by task |
| Empty state | every data surface | explains why and gives next useful action |

## Content growth plan

- Projects and run history use server pagination.
- Topics remain a two-level taxonomy; parent and leaf roles stay visible.
- Filters and period selectors scale through URL query parameters.
- Wide evidence tables gain task-specific mobile reductions, not global
  horizontal page scrolling.
- Historical charts aggregate or reduce ticks before rendering more points.

## URL strategy

- Resource patterns remain `/repositories/{id}` and `/topics/{slug}`.
- Project filters and pagination, Topic ranking period, leader exclusion, and
  language selection use query parameters and survive refresh, sharing, and
  browser back.
- Navigation uses anchors, not JavaScript-only state.
- Language may be selected through `?lang=zh-CN|en` and persisted by the
  existing same-site cookie.
