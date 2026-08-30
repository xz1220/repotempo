# Discovery

Discovery combines four signals that answer different questions.

- OSS Insight finds short-term momentum. Its `stars` field is growth inside a
  rolling window, not the repository's absolute GitHub star count.
- GitHub Repository Search finds mature high-stock projects, topic leaders,
  recently created movers, and active benchmarks.
- Legacy import preserves the projects and real observations already collected.
- The manual watchlist adds explicit research targets.

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

## Authoritative API references

- [GitHub Search repositories](https://docs.github.com/en/rest/search/search#search-repositories)
- [GitHub repository search qualifiers](https://docs.github.com/en/search-github/searching-on-github/searching-for-repositories)
- [GitHub REST rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)
- [GitHub REST best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api)
- [GitHub REST API versions](https://docs.github.com/en/rest/about-the-rest-api/api-versions)
