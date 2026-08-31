# GitHub Radar design system

A locked product design system for a public research dashboard. All routes use
the same app shell, token vocabulary, interaction rules, and data semantics.
Page-specific layout varies only when the page answers a different research
question.

## Product job

Help a technical researcher answer four questions quickly:

1. What happened after a project first entered the radar?
2. Which monitored projects are gaining or losing momentum now?
3. How has a project's absolute Star position changed inside a trustworthy,
   comparable sample?
4. What does the project actually do, and what evidence supports that reading?

The product is an operational research tool, not a marketing site. Real GitHub
data, provenance, and data quality are the visual identity.

## Scene and genre

A researcher reviews a large, growing project catalogue on a laptop in a bright
office. They move between time windows and topic scopes, compare exact values,
and keep enough context to distrust incomplete data.

- Genre: modern-minimal product UI.
- App macrostructure: Catalogue.
- Color strategy: restrained light palette with one cool-blue selection accent.
- Enrichment: none. Repository names, observed values, and historical evidence
  carry the interface.

## App shell

- One app bar contains GitHub Radar identity, the two primary routes, and
  compact utilities.
- Primary navigation has only Projects and Topics. Projects points to `/`, the
  canonical monitoring catalogue.
- Collection history is a utility link to `/runs`; language selection remains a
  utility. Neither competes visually with the primary routes.
- Route links are normal anchors with `aria-current="page"`; they are not ARIA
  tabs. Same-page period and sort choices are compact URL-backed view controls.
- Project and topic detail pages preserve the app bar and add one breadcrumb to
  their parent collection.
- Mobile wraps or reduces the single app bar without introducing another
  navigation hierarchy. Every core destination remains directly reachable.

See [.design/github-radar/INFORMATION_ARCHITECTURE.md](.design/github-radar/INFORMATION_ARCHITECTURE.md)
for the complete route, naming, comparison, and content-priority model.

## Page jobs

| Route | Question it answers | Primary content |
| --- | --- | --- |
| `/` | Which monitored projects are moving in the selected scope and period? | Period, sort, date and topic controls; cohort coverage; project trend catalogue |
| `/repositories` | Compatibility entry to the canonical project catalogue | Same monitoring view and URL-backed controls as `/` |
| `/repositories/{id}` | What does this project do, and how has its attention developed? | Stored interpretation, identity, Star history, provenance, exact snapshots |
| `/topics` | Which technical directions are gaining attention? | Period-controlled topic ranking and hierarchy-aware exact table |
| `/topics/{slug}` | What drives one topic? | Rolled-up topic metrics, trend, concentration, representative projects |
| `/runs` | Can this dataset be trusted on the selected dates? | Collection outcomes, coverage, failures, quota and search-integrity evidence |
| `/discoveries` | Compatibility entry for the former discovery page | Redirect to the project catalogue with `new=1` |

## Project-monitoring catalogue

The homepage is not an aggregate overview. A monotonically growing registry
makes registry-wide Star totals and daily totals easy to misread, so the first
screen is a comparison tool:

1. Period and ordering controls establish the research question.
2. Search, hierarchical topic, endpoint date, and “new on this date” establish
   the visible slice. Source and monitoring-state filters remain progressive
   details.
3. A compact cohort summary exposes scope, endpoint observations, comparable
   observations, new repositories, and the exact baseline-to-endpoint range.
4. The catalogue table presents current Stars, period change, growth rate,
   daily velocity, sample rank transition, rank change, and topics.
5. New discoveries appear as a row badge and an optional filter, not a separate
   content silo.
6. Cursor pagination keeps very large registries stable as new repositories are
   added.

## Comparison semantics

- Periods are strict 1-day, 7-day, or 30-day calendar comparisons ending on the
  selected date.
- A repository is comparable only when both exact endpoint dates have a
  successful Star observation. No interpolation and no last-known-value
  substitution is allowed.
- Rank uses `DENSE_RANK` within the scoped common cohort. Positive rank change
  means the repository moved upward: baseline rank minus current rank.
- Topic, discovery-source, and monitoring-state scope is established before
  ranks are calculated. Text search and the new-only switch narrow the rendered
  results without redefining a repository's rank inside that scoped cohort.
- Every rank label says “monitored sample” or equivalent; it never implies a
  global GitHub ranking.
- A parent topic includes direct child topics. A child selection remains scoped
  to that child. Unclassified repositories are an explicit filter and coverage
  state, never silently discarded.
- Topic deltas use the same exact endpoint rule and show the comparable count
  beside the topic's total scope. Classification coverage names the classified
  and unclassified populations explicitly.
- Topic history uses one fixed cohort observed successfully on both the first
  displayed date and the endpoint. Missing intermediate cohort observations
  remain chart gaps.

## Project interpretation

- The project detail page leads with a durable Chinese interpretation when one
  exists: summary, key points, use cases, technical notes, source, model,
  revision, and analysis date.
- Codex-generated, imported, and human-written analyses use the same stored
  structure and show the provenance and revision counter of the current body.
- Until an analysis exists, the page labels the state as pending and falls back
  explicitly to a manual note or the original GitHub description. A description
  must never be presented as generated research.
- Analysis creation and regeneration are offline workflows. The collector never
  reads README content automatically, and the Web surface remains read-only.

## Principles

- Data first: numbers align, labels stay close, and provenance remains visible.
- Decision first: every section answers a named research question.
- Honest states: missing, failed, partial, zero, new, and successful observations
  remain visually and semantically distinct.
- Few containers: use spacing and rules before adding surfaces or cards.
- Progressive detail: catalogue, object interpretation, trend, then raw evidence.
- URL state: date, period, sort, filters, cursor, and language survive refresh,
  sharing, and browser back.
- Consistency over novelty: pages share controls and tokens; body rhythm differs
  only because their tasks differ.

## Tokens

Production tokens live in `internal/web/static/tokens.css`. All colors use OKLCH
variables. The system defines semantic roles for paper, surface, ink, rule,
accent, positive, negative, warning, information, focus, and data series.
Semantic colors, fonts, motion, and the core spacing scale are locked tokens;
limited literal dimensions remain only for optical alignment, chart geometry,
and responsive layout thresholds.

- Radius: 6–10px for controls and bounded surfaces; pills only for compact
  status or view controls.
- Spacing: 4px base with named 8 / 12 / 16 / 24 / 32 / 48 / 64px steps.
- Depth: rules and surface changes first; no decorative shadow stacks.

## Typography

- Use one complete system sans stack with explicit Chinese fallbacks.
- Use the native monospace stack only for repository names, identifiers, dates,
  ranks, and measured values.
- Body copy stays at 16px on small screens and uses 1.5–1.65 line height.
- Numbers use `tabular-nums`; long identifiers use `overflow-wrap: anywhere`.
- Headings use sentence case and balanced wrapping. No decorative uppercase
  eyebrows, italic headings, or gratuitous display-font pairing.

## Layout and density

- Desktop content width is 1440px with adaptive gutters.
- The homepage gives its largest area to the filterable project catalogue; the
  cohort summary is compact context, not a fake hero.
- Tables remain the primary comparison surface. Mobile converts the project
  table into readable records instead of forcing a wide viewport.
- Project and topic history may use one primary chart each. Exact-value tables
  remain available alongside visual summaries.
- Responsive verification widths: 320, 375, 414, 768, and 1440px, plus 200%
  zoom and long Chinese / repository-name cases.

## Motion and interaction

- Motion communicates state only: focus, press, selection, expansion, and
  responsive navigation.
- Frequently used navigation is instant; color and opacity transitions stay
  between 120 and 200ms.
- Never animate layout dimensions or block interaction during motion.
- `prefers-reduced-motion` removes non-essential transitions.
- Every interactive control has visible hover, active, focus-visible, disabled,
  loading, error, and success behavior where those states apply.

## Charts and data

- Time trend: line; daily interval change: columns; category comparison:
  horizontal bars; source proportions: stacked bar with four or fewer sources.
- One chart never mixes absolute scale and growth on a dual axis.
- Axes identify period and unit. Pointer tooltips expose exact values; keyboard
  users receive the same values through an expandable or adjacent table.
- Positive, negative, zero, missing, stale, and non-comparable states cannot
  rely on color alone.
- No 3D, glow, thick gradients, decorative area fills, or meaningless
  sparklines.

## Product anti-patterns

- No separate overview based on totals from a monotonically growing registry.
- No top-level destinations for New discoveries or Collection status.
- No marketing hero, invented metrics, testimonial proof, or logo walls.
- No purple-blue AI gradients, gradient text, glassmorphism, or neon glow.
- No equal icon-title-description card grids or cards nested inside cards.
- No oversized rounded rectangles around every section.
- No framework migration, remote fonts, remote imagery, or third-party chart
  runtime. Keep Go templates, semantic HTML, local CSS, native JS, and SSR SVG.
