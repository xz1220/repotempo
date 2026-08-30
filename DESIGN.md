# Dashboard design language

## Scene

A researcher reviews trend signals on a laptop in a bright office, comparing
many projects quickly while keeping enough context to distrust incomplete data.
The interface therefore uses a light theme, strong ink contrast, compact rows,
and one cool blue accent for links and active filters.

## Principles

- Data first: numbers align, labels stay close, provenance is visible.
- Honest states: missing, failed, partial, and successful observations differ.
- Few containers: use whitespace and rules before adding cards.
- Progressive detail: overview first, repository and run evidence one click away.
- Responsive by subtraction: mobile keeps core metrics and moves wide tables into
  horizontal scroll regions with explicit labels.

## Tokens

All production colors are expressed with OKLCH CSS variables. The palette uses
true neutral surfaces, dark blue-gray ink, and a single blue accent. Corners use
an 8px default radius, with pills reserved for status and filter controls.

## Typography

Use the native system sans-serif stack for text and the native monospace stack
for star counts, deltas, ranks, dates, and identifiers. Body copy is capped at
72 characters where prose appears.

## Motion

Only interaction feedback and small chart transitions animate. Reduced-motion
preferences disable non-essential transitions.

