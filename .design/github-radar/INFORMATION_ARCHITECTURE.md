# GitHub Radar information architecture

The September 7, 2026 product direction uses three main reading modes: Trends,
Daily discoveries, and Project library. Agent categories provide another way
to enter the same collection. Earlier restrictions on a dashboard homepage,
sidebar, or standalone discovery page no longer apply.

## Site map

```text
GitHub Radar
├── Trends /
├── Daily discoveries /discoveries
├── Project library /repositories
│   ├── My watchlist /repositories?focus=1
│   └── Project detail /repositories/{github_repository_id}
├── Agent categories /topics
│   └── Category detail /topics/{topic_slug}
├── Add project /watch/new
└── Collection history /runs
```

Health endpoints remain `/healthz` and `/readyz`.

## Navigation

A dark fixed desktop sidebar contains the reading destinations, My watchlist,
and Add project. Collection history belongs to the utility area. The main
content remains bright; its header supplies page context and a language switch.

The sidebar reorganizes on narrow screens. Main destinations remain reachable
without root horizontal scrolling. Navigation links are ordinary anchors with
active-route states, not simulated application tabs.

Project and category details include a breadcrumb. Date, period, category, and
list filters use URL state so links remain useful after refresh, sharing, and
browser Back.

## Trends

The homepage answers which observed projects are moving now and where to look
next. It presents information in this order:

1. The selected data date, period, and category.
2. Counts for gaining Stars, unchanged Stars, slowing momentum, and new entries.
3. A fixed-cohort attention chart and positive/zero/negative distribution.
4. Fastest-growth, slow-growth, and slowing-momentum project groups.
5. A short explanation of the comparison and links to the relevant projects.

The fixed cohort contains repositories observed successfully at both exact
endpoints. Its attention index starts at 100. Adding another project to the
library cannot directly raise the curve.

Missing intermediate observations remain gaps. The chart's companion table
contains exact values and coverage. A period without enough history offers a
shorter window or another date.

## Daily discoveries

This page archives projects by their first entry into the radar. It has a date
selector, category filtering, readable project records, and continuation
through additional results.

Each record leads with what the project does. It includes the name, category,
observed Stars, and detail link. GitHub creation time is shown separately when
known; entering the radar is not the same as being newly created.

Discovered projects are already registered for continued monitoring. The page
does not require another add action. An empty date offers another date or a
manual addition.

## Project library

The library is the durable home for every tracked project. Search, category,
date, period, ordering, and My watchlist narrow the collection.

Rows or mobile records show the project explanation, relevant Star values,
changes, and category. They preserve new, awaiting-observation, stale, and
unclassified states instead of hiding incomplete projects.

Cursor continuation supports a large collection. Changing the sort or a filter
starts a new traversal. A fixed selected date keeps pagination meaningful while
the registry grows.

## Add project

The form at `/watch/new` accepts a GitHub URL or owner/name, an optional category,
and a note. Submission uses `POST /watch`.

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
2. Stored research interpretation, when available, with its provenance.
3. Current observed scale and comparable period changes.
4. Star history, category, discovery date, and effective history start.
5. Exact observations and collection evidence.

A saved interpretation explains capabilities and use cases. Without one, the
page still explains the project using its original description and clearly
marks further research as unavailable.

Codex or a researcher prepares interpretations outside the Web interface. The
current body retains source, model, time, and revision; the revision counter
does not imply that earlier bodies remain available.

## Agent categories

The roots are coding agents, research agents, browser/computer agents, workflow
agents, agent platforms, general agents, and AI infrastructure. Supporting
tags retain a two-level hierarchy.

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

Let `D` be the selected endpoint and `W` the number of days in the period.

| Measure | Meaning |
| --- | --- |
| Current-period gain | Stars at `D` minus Stars at `D-W`, using two successful exact observations. |
| Previous-period gain | Stars at `D-W` minus Stars at `D-2W`, using two successful exact observations. |
| Momentum change | Current-period gain minus previous-period gain, requiring all three dates. |
| Growth rate | Current-period gain divided by baseline Stars, when baseline Stars are positive. |
| Daily velocity | Current-period gain divided by the window length. |
| Sample rank change | Baseline rank minus endpoint rank within the same scoped cohort. |
| New on date | The repository first entered this system on the selected date. |

Fastest growth uses the largest positive gain. Slow growth uses zero and the
smallest positive gains. Slowing momentum requires negative momentum change;
the project may still have gained Stars.

A failed or missing observation is never zero. Last-known values show their
actual date and do not replace exact comparison endpoints.

Topic, discovery source, monitoring state, and focus define a comparison scope.
Text search and the new-only display filter do not turn a matching project
into rank one. Ranks always refer to the monitored sample.

## Common reading sessions

A daily review begins on Trends. The user chooses a window, scans the chart and
three growth groups, then opens a project or the corresponding library view.

Reading new projects begins on Daily discoveries. The user chooses a date,
reads descriptions, opens interesting projects, and returns to the archive.
Those projects continue to appear in the library on later days.

An external recommendation begins with Add project. After GitHub validation,
the project detail is available with a real initial observation and the project
appears in My watchlist.

A category review begins on Agent categories or a category filter. The user
compares projects serving a similar purpose and can inspect incomplete
classification or observation coverage.

## Labels

| Concept | Chinese | English |
| --- | --- | --- |
| Main trend view | 趋势看板 | Trends |
| Discovery archive | 每日发现 | Daily discoveries |
| Persistent catalogue | 项目库 | Project library |
| Purpose-based taxonomy | Agent 分类 | Agent categories |
| Operator addition | 添加关注 | Add project |
| Focused projects | 我的关注 | My watchlist |
| No supported classification | 待分类 | Unclassified |
| First local registration | 首次发现 / 入库日期 | First discovery |
| Actual GitHub creation | 创建于 | Created |
| Largest positive period gains | 涨得最快 | Fastest growth |
| Zero or small positive gains | 增长平缓 | Slowest growth |
| Lower gains than the previous window | 势头回落 | Losing momentum |
| Collector evidence | 采集历史 | Collection history |
| Stored research reading | 项目解读 | Project interpretation |

## Components and growth

The sidebar, page header, period controls, category controls, project identity,
state labels, chart frames, and empty-state patterns are shared.

Project reading cards and numerical comparison rows use different density for
their different tasks. Forms remain short. Exact-value evidence can be expanded
without filling the first screen.

The project library and discovery archive use capped cursor pages. Collection
history may retain its smaller offset-paginated log. Charts reduce tick density
before compromising date labels or exact-value access.

## URLs and compatibility

- `/` is the trend dashboard; `/repositories` is the project library.
- `/discoveries` is a full page. The previous discovery redirect is replaced.
- Old root catalogue links with search, cursor, or `new=1` can redirect to
  `/repositories` while retaining their query values.
- Browsing state uses `date`, `period=1d|7d|30d`, `sort`, `topic`, `q`,
  `source`, `status`, `focus=1`, `new=1`, and `cursor` where applicable.
- Category labels hide reserved filter values from the normal reading path.
- Language uses `lang=zh-CN|en` and the existing same-site preference cookie.
- Operator credentials never appear in query strings.
