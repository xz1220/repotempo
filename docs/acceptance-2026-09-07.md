# 2026-09-07 local acceptance

The redesign provides a trend dashboard, daily discoveries, the project
library, Agent categories, and an Add project form. This record covers local
verification; it does not certify a production deployment.

## Data and discovery

The remote collector was inspected read-only. It held 4,126 repositories and
39,062 snapshots through 2026-09-07. The latest batch recorded 4,105 successful
and 9 failed observations. The current OSS Insight configuration still enabled
the adapter, but all four windows returned zero candidates.

The new default disables OSS Insight and uses GitHub Search plus the repository
API. Optional empty OSS results now produce a warning. Tests verify that
GitHub discovery and snapshots continue independently.

A consistent copy of the completed remote database was used for the local
preview. No production configuration, schedule, or database was changed.

## Reading flows

- The dashboard displays positive growth, flat observations, slowing momentum,
  and new discoveries. Three separate lists show fastest positive gains,
  slowest nonnegative gains, and reduced growth across equal windows.
- Period and category controls preserve their scope. The trend graph uses one
  fixed cohort, preserves missing dates, and uses calendar spacing. Small
  index movements have distinct, readable axis labels.
- The project library includes missing and failed observations with last-known
  values and explicit pending labels. Cursor traversal preserves focus,
  category, period, and observation date.
- Daily discoveries distinguish first registry entry from repository creation.
  A selected day's records link directly to project details.
- Root categories describe product purpose. Component tags remain available.
  Exact reviewed mappings cover OpenCode, LangGraph, Browser Use, Orca, and
  DeepSeek Harness; automatic suggestions remain subject to manual decisions.

## Manual tracking

The browser form was tested against the real public GitHub API with
`golang/example`. It saved the project description, a real 2,973-Star
observation, and a watchlist note, then navigated to the project detail.
The added repository is confined to the local preview database.

Automated tests also cover duplicate identities, existing notes, manual
classification vetoes, invalid categories, private repositories, source errors,
CSRF expiry/replay, cross-origin requests, body limits, and local/public write
authorization.

Default dashboard dates follow completed collection batches. Library dates
also include newly registered projects, even before a successful snapshot.
Individual detail pages include new manual observations; explicit historical
dates remain strict.

## Validation

- `make ci`: formatting, vet, all tests, race tests, and Linux cross-build.
- `make security`: no known vulnerabilities found by govulncheck.
- Seven main routes were checked at 320, 375, 768, and 1440 CSS pixels with no
  root horizontal overflow.
- English routes were checked at 414 CSS pixels. No untranslated UI keys or
  browser console errors were observed.
- Form submission, saved description display, category navigation, and
  project detail links were exercised in the real browser.

The synthetic benchmark contains 32 snapshot dates per repository. On the
local test machine, 4,000 repositories took roughly 343 ms for the 7-day
dashboard and 72 ms for a project page. At 10,000 repositories those queries
took roughly 893 ms and 176–181 ms. These are local measurements, not hosting
performance guarantees.

## Remaining boundaries

The local preview is a database copy and does not run a scheduled collector.
Existing deployments must explicitly adopt the new configuration. Research
interpretations retain only the current body and are included in SQLite
backups, not yet the regular CSV/JSON monitoring exports.

The copied source database retains 161 pre-existing orphan topic mappings.
This redesign does not repair or delete historical database records.
