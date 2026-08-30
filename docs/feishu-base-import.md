# Importing a Feishu Base export

The production Go process does not receive a Feishu credential. A small bridge
script reads the existing Base through the authenticated `lark-cli`, resolves
every project through the authenticated GitHub CLI, and writes the same
identity-checked CSV contract used by `import-legacy`.

Requirements:

- `lark-cli` signed in as a user who can read the Base.
- `gh` signed in to GitHub.
- `jq`.
- The Base uses the existing Chinese field names documented below.

```sh
scripts/export-feishu-base.sh \
  <base-token> \
  <project-table-id> \
  <daily-table-id> \
  /tmp/github-radar-feishu.csv

github-radar import-legacy --csv /tmp/github-radar-feishu.csv
```

The bridge paginates serially in batches of 200 until `has_more=false`. It reads
the project fields `项目`, `GitHub`, `语言`, `分类`, `首次出现`, `最后观察`,
`累计 Stars`, and `人工收藏`, plus the daily fields `项目`, `GitHub`, `日期`,
`累计 Stars`, `今日排名`, and `今日 Stars 增量`.

Base rows do not contain GitHub repository ID. The bridge therefore resolves
each `owner/name` through GitHub before writing a CSV row, and the Go importer
performs the same ID check again before persistence. An unresolved repository is
reported and skipped. The Base is never modified.

Only dated cumulative-star values become observations. Missing days remain
missing. Project-table values without a real `最后观察` timestamp are imported
as project metadata only. Daily rows are written before project-summary rows so
same-day duplicates keep the daily rank evidence.

