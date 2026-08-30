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
boundary are split by configured star bands, creation dates, or topics. The run
record keeps the original query, partitions, total count, pages, completeness,
and rate state.

All sources resolve to a GitHub repository ID before persistence. Multiple
source hits merge into one registry row while discovery provenance remains in
the repository JSON fields and the job run details.

