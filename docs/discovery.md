# Discovery

GitHub Search is the default online discovery source. Historical imports and
manual additions complement it. OSS Insight is optional and disabled by default.

- GitHub Repository Search finds mature high-stock projects, topic leaders,
  recently created projects with early interest, and active benchmarks.
- Legacy import preserves the projects and real observations already collected.
- The manual watchlist adds explicit research targets.
- Historical OSS Insight signals use rolling-window growth, not absolute
  GitHub Star counts. They remain separate from daily observations.

GitHub Search cannot be treated as a complete site crawl. A query returns at
most 1,000 results and may report `incomplete_results`. Profiles that exceed the
boundary are split by configured star bands, creation dates, or pushed dates. The run
record keeps the original query, partitions, total count, pages, completeness,
and rate state.

All sources resolve to a GitHub repository ID before persistence. Multiple
source hits merge into one registry row while discovery provenance remains in
the repository JSON fields and the job run details.

## Capacity boundary

Authenticated GitHub Core REST capacity is normally 5,000 requests per hour.
Because a complete panel needs roughly one Core request per active repository,
the configured discovery population must fit that budget with headroom for
retries and other API users. The example keeps mature and recently active
thresholds at 20,000 Stars and leaves monthly broad coverage disabled until the
operator explicitly checks `total_count` and available Core capacity. Search
can discover more projects than a single token can snapshot; it must not be
allowed to silently grow the active panel beyond its collection budget.
The global recent-created query uses a 500-Star floor. Targeted AI Agent and
coding-agent queries use 30 Stars over 30 days, and an LLM query uses 100 Stars
over 14 days. These narrower queries catch early projects without lowering the
threshold for the whole site. Query thresholds are configuration, not a hard
enforced cap on the active population; watch collection duration and quota.

New discoveries automatically enter the registry. A first-seen date means new
to the radar, not newly created on GitHub. Pushed time and absolute-Star search
ordering must not be described as measured Star growth.

See [GitHub data and classification](github-data.md) for category rules and
offline reclassification commands.

## Authoritative API references

- [GitHub Search repositories](https://docs.github.com/en/rest/search/search#search-repositories)
- [GitHub repository search qualifiers](https://docs.github.com/en/search-github/searching-on-github/searching-for-repositories)
- [GitHub REST rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)
- [GitHub REST best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api)
- [GitHub REST API versions](https://docs.github.com/en/rest/about-the-rest-api/api-versions)
