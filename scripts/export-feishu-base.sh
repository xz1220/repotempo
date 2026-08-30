#!/bin/sh
set -eu

usage() {
  echo "usage: export-feishu-base.sh BASE_TOKEN PROJECT_TABLE_ID DAILY_TABLE_ID OUTPUT.csv" >&2
  exit 2
}

[ "$#" -eq 4 ] || usage

base_token=$1
project_table_id=$2
daily_table_id=$3
output_path=$4

case "$output_path" in
  /*) ;;
  *) output_path=$(pwd)/$output_path ;;
esac

if [ -e "$output_path" ]; then
  echo "refusing to overwrite existing output: $output_path" >&2
  exit 1
fi

LARK_CLI_BIN=${LARK_CLI_BIN:-lark-cli}
LARK_CLI_NODE=${LARK_CLI_NODE:-}
GH_BIN=${GH_BIN:-gh}
JQ_BIN=${JQ_BIN:-jq}

command -v "$GH_BIN" >/dev/null 2>&1 || { echo "missing gh" >&2; exit 1; }
command -v "$JQ_BIN" >/dev/null 2>&1 || { echo "missing jq" >&2; exit 1; }
if [ -z "$LARK_CLI_NODE" ]; then
  command -v "$LARK_CLI_BIN" >/dev/null 2>&1 || { echo "missing lark-cli" >&2; exit 1; }
elif [ ! -x "$LARK_CLI_NODE" ]; then
  echo "LARK_CLI_NODE is not executable: $LARK_CLI_NODE" >&2
  exit 1
fi

run_lark() {
  if [ -n "$LARK_CLI_NODE" ]; then
    "$LARK_CLI_NODE" "$LARK_CLI_BIN" "$@"
  else
    "$LARK_CLI_BIN" "$@"
  fi
}

umask 077
temporary_directory=$(mktemp -d)
temporary_output=$(mktemp "${output_path}.tmp.XXXXXX")
cleanup() {
  rm -rf "$temporary_directory"
  rm -f "$temporary_output"
}
trap cleanup EXIT HUP INT TERM

project_rows=$temporary_directory/projects.jsonl
daily_rows=$temporary_directory/daily.jsonl
identity_rows=$temporary_directory/identities.jsonl
identity_map=$temporary_directory/identities.json
: >"$project_rows"
: >"$daily_rows"
: >"$identity_rows"

offset=0
while :; do
  page=$(run_lark base +record-list \
    --base-token "$base_token" \
    --table-id "$project_table_id" \
    --field-id "项目" \
    --field-id "GitHub" \
    --field-id "语言" \
    --field-id "分类" \
    --field-id "首次出现" \
    --field-id "最后观察" \
    --field-id "累计 Stars" \
    --field-id "人工收藏" \
    --offset "$offset" \
    --limit 200 \
    --format json \
    --as user)
  printf '%s\n' "$page" | "$JQ_BIN" -c '.data.data[] | {
    full_name: .[0], language: (.[2] // ""), categories: (.[3] // []),
    first_seen_at: (.[4] // ""), observed_at: (.[5] // ""),
    star_count: .[6], is_focus: (.[7] // false)
  }' >>"$project_rows"
  count=$(printf '%s\n' "$page" | "$JQ_BIN" -r '.data.data | length')
  has_more=$(printf '%s\n' "$page" | "$JQ_BIN" -r '.data.has_more')
  [ "$count" -gt 0 ] || break
  offset=$((offset + count))
  [ "$has_more" = "true" ] || break
done

offset=0
while :; do
  page=$(run_lark base +record-list \
    --base-token "$base_token" \
    --table-id "$daily_table_id" \
    --field-id "项目" \
    --field-id "GitHub" \
    --field-id "日期" \
    --field-id "累计 Stars" \
    --field-id "今日排名" \
    --field-id "今日 Stars 增量" \
    --offset "$offset" \
    --limit 200 \
    --format json \
    --as user)
  printf '%s\n' "$page" | "$JQ_BIN" -c '.data.data[] | {
    full_name: .[0], observed_at: (.[2] // ""), star_count: .[3],
    oss_today_rank: .[4], oss_window_stars: .[5]
  }' >>"$daily_rows"
  count=$(printf '%s\n' "$page" | "$JQ_BIN" -r '.data.data | length')
  has_more=$(printf '%s\n' "$page" | "$JQ_BIN" -r '.data.has_more')
  [ "$count" -gt 0 ] || break
  offset=$((offset + count))
  [ "$has_more" = "true" ] || break
done

{
  "$JQ_BIN" -r '.full_name' "$project_rows"
  "$JQ_BIN" -r '.full_name' "$daily_rows"
} | sed '/^$/d' | sort -u | while IFS= read -r full_name; do
  if repository=$("$GH_BIN" api "repos/$full_name" --jq '{id:.id,html_url:.html_url}' 2>/dev/null); then
    "$JQ_BIN" -cn --arg full_name "$full_name" --argjson repository "$repository" \
      '{full_name:$full_name,id:$repository.id,html_url:$repository.html_url}' >>"$identity_rows"
  else
    echo "warning: GitHub identity could not be resolved for $full_name; rows skipped" >&2
  fi
done

"$JQ_BIN" -s 'map({key:.full_name,value:{id:.id,html_url:.html_url}}) | from_entries' \
  "$identity_rows" >"$identity_map"

printf '%s\n' 'github_repo_id,full_name,html_url,primary_language,first_seen_at,observed_at,snapshot_date,star_count,source,is_focus,topics,fixed_panel,oss_today_rank,oss_window_stars' >"$temporary_output"

# Daily rows come first. If the project table repeats the same repository/date,
# the Go importer keeps this richer row with its OSS rank evidence.
"$JQ_BIN" -r --slurpfile identities "$identity_map" '
  . as $row | ($identities[0][.full_name] // null) as $identity |
  select($identity != null and .observed_at != "" and .star_count != null) |
  [$identity.id, .full_name, $identity.html_url, "", "", .observed_at,
   (.observed_at[0:10]), .star_count, "legacy", false, "", false,
   (.oss_today_rank // ""), (.oss_window_stars // "")] | @csv
' "$daily_rows" >>"$temporary_output"

"$JQ_BIN" -r --slurpfile identities "$identity_map" '
  def topic_slugs:
    [.categories[]? |
      if . == "Agent 与多智能体" then "ai-agent"
      elif . == "Agent Skills 与工作流" then "skills"
      elif . == "模型/数据/AI 基础设施" then "ai-infrastructure"
      else empty end] | join(";");
  . as $row | ($identities[0][.full_name] // null) as $identity |
  select($identity != null) |
  [$identity.id, .full_name, $identity.html_url, .language, .first_seen_at,
   .observed_at, (if .observed_at == "" then "" else .observed_at[0:10] end),
   (if .observed_at == "" then "" else (.star_count // "") end),
   "legacy", .is_focus, topic_slugs, false, "", ""] | @csv
' "$project_rows" >>"$temporary_output"

mv "$temporary_output" "$output_path"
trap - EXIT HUP INT TERM
rm -rf "$temporary_directory"
echo "$output_path"

