# GitHub Radar design system

A locked product design system for a public research dashboard. All routes use
the same app shell, token vocabulary, interaction rules, and data semantics.
Page-specific layout may vary only when the page answers a different research
question.

## Product job

Help a technical researcher answer three questions quickly:

1. Is the dataset current and trustworthy?
2. Where is open-source attention moving?
3. Which projects or topics deserve deeper inspection?

The product is an operational research tool, not a marketing site. Real GitHub
data, provenance, and data quality are the visual identity.

## Scene and genre

A researcher reviews trend signals on a laptop in a bright office, comparing
many projects while keeping enough context to distrust incomplete data.

- Genre: modern-minimal product UI.
- App macrostructure: Research Workbench.
- Color strategy: restrained light palette with one cool-blue selection accent.
- Enrichment: none. Charts, repository names, and observed values carry the UI.

## App shell

- Utility row: GitHub Radar identity on the left; language and compact utilities
  on the right.
- Primary row: five route links in a stable order: Overview, Projects, Topics,
  New discoveries, Collection status.
- Route links are normal anchors with `aria-current="page"`; they are not ARIA
  tabs. Segmented controls are reserved for changing one view on the same route,
  such as 1 / 7 / 30 days.
- The current route uses weight, shape, and an indicator, never color alone.
- Deep object pages keep the primary navigation and add a breadcrumb back to
  Projects or Topics.
- Mobile keeps one compact utility row and a menu-controlled primary route list.

See [.design/github-radar/INFORMATION_ARCHITECTURE.md](.design/github-radar/INFORMATION_ARCHITECTURE.md)
for the complete route, naming, and content-priority model.

## Page jobs

| Route | Question it answers | Primary content |
| --- | --- | --- |
| `/` | Is the data healthy, and what is accelerating? | Coverage summary, daily Star growth, fast projects, latest run |
| `/repositories` | Which projects are large, fast, or newly relevant? | URL-backed filters and comparison table |
| `/repositories/{id}` | How did one project reach its current attention? | Identity, Star history, provenance, snapshots |
| `/topics` | Which technical directions are gaining attention? | Period-controlled leaf-topic ranking and exact table |
| `/topics/{slug}` | What drives one topic? | Topic metrics, trend, concentration, representative projects |
| `/discoveries` | What entered the radar, and through which source? | First-source composition and discovery-profile evidence |
| `/runs` | Can this dataset be trusted today? | Coverage, run outcomes, failure and quota evidence |

## Principles

- Data first: numbers align, labels stay close, provenance remains visible.
- Decision first: every section answers a named research question.
- Honest states: missing, failed, partial, zero, and successful observations
  remain visually and semantically distinct.
- Few containers: use spacing and rules before adding surfaces or cards.
- Progressive detail: overview, comparison, object detail, then raw evidence.
- URL state: filters, periods, pagination, and other shareable view state live in
  the URL.
- Consistency over novelty: pages share controls and tokens; their body rhythm
  differs only because their tasks differ.

## Tokens

Production tokens live in `internal/web/static/tokens.css`. All colors use
OKLCH variables. The system defines semantic roles for paper, surface, ink,
rule, accent, positive, negative, warning, information, focus, and data series.
Semantic colors, fonts, motion, and the core spacing scale are locked tokens;
limited literal dimensions remain only for optical alignment, chart geometry,
and responsive layout thresholds.

- Radius: 6–10px for controls and bounded surfaces; pills only for compact
  status or segmented controls.
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
- The homepage gives its largest area to one attention-trend chart; summary
  metrics are compact support, not a fake hero.
- Tables remain the primary comparison surface. Mobile converts the project
  table into readable records instead of forcing a nine-column viewport.
- Each page has at most one primary chart. Supporting visuals stay subordinate
  and retain exact-value alternatives.
- Responsive verification widths: 320, 375, 414, 768, and 1440px, plus 200%
  zoom and long Chinese / repository-name cases.

## Motion and interaction

- Motion communicates state only: focus, press, selection, expansion, and
  mobile navigation.
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
- Positive, negative, zero, missing, and stale states cannot rely on color alone.
- No 3D, glow, thick gradients, decorative area fills, or meaningless sparklines.

## Product anti-patterns

- No marketing hero, invented metrics, testimonial proof, or logo walls.
- No purple-blue AI gradients, gradient text, glassmorphism, or neon glow.
- No equal icon-title-description card grids or cards nested inside cards.
- No repeated numbered section labels or identical entrance animations.
- No oversized rounded rectangles around every section.
- No framework migration, remote fonts, remote imagery, or third-party chart
  runtime. Keep Go templates, semantic HTML, local CSS, native JS, and SSR SVG.
