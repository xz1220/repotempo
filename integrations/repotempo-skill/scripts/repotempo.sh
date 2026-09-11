#!/bin/sh
set +x
set -eu
rt_skill_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
. "$rt_skill_dir/scripts/lib.sh"
rt_dependencies
rt_load_config
rt_action=${1-}; [ "$#" -eq 0 ] || shift
case "$rt_action" in
  account)
    [ "$#" -eq 0 ] || rt_fail 'Usage: account'
    rt_path=/api/v1/me
    ;;
  repository)
    [ "$#" -eq 1 ] || rt_fail 'Usage: repository <numeric ID from search>'
    printf '%s\n' "$1" | grep -Eq '^[1-9][0-9]{0,15}$' || rt_fail 'Repository ID must be a positive numeric ID from search.'
    rt_path="/api/v1/repositories/$1"
    ;;
  search|watchlist)
    rt_query=''; rt_page=1; rt_size=20; rt_filters=''
    if [ "$rt_action" = search ] && [ "$#" -gt 0 ]; then
      case "$1" in --*) ;; *) rt_query=$1; shift ;; esac
    fi
    [ "$(printf '%s' "$rt_query" | wc -c)" -le 800 ] || rt_fail 'Search query is too long.'
    while [ "$#" -gt 0 ]; do
      [ "$#" -ge 2 ] || rt_fail 'Each option needs a value.'
      rt_option=$1; rt_value=$2; shift 2
      case "$rt_option" in
        --page) printf '%s\n' "$rt_value" | grep -Eq '^[1-9][0-9]{0,3}$|^10000$' || rt_fail 'Page must be 1–10000.'; rt_page=$rt_value ;;
        --size) case "$rt_value" in 6|12|20) rt_size=$rt_value ;; *) rt_fail 'Size must be 6, 12 or 20.' ;; esac ;;
        --period) case "$rt_value" in 1d|7d|30d) ;; *) rt_fail 'Period must be 1d, 7d or 30d.' ;; esac ;;
        --sort) case "$rt_value" in stars|delta|rank_change|growth_rate|low_growth|slowdown|newest|name|velocity) ;; *) rt_fail 'Unsupported sort order.' ;; esac ;;
        --tag) [ "$(printf '%s' "$rt_value" | wc -c)" -le 320 ] || rt_fail 'Tag is too long.' ;;
        --date) printf '%s\n' "$rt_value" | grep -Eq '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' || rt_fail 'Date must be YYYY-MM-DD.' ;;
        *) rt_fail 'Unknown option. Use --page, --size, --period, --sort, --tag or --date.' ;;
      esac
      case "$rt_option" in --page|--size) ;; *) rt_filters="$rt_filters&${rt_option#--}=$(printf '%s' "$rt_value" | rt_encode)" ;; esac
    done
    if [ "$rt_action" = watchlist ]; then rt_view=focus; else rt_view=all; fi
    rt_path="/api/v1/repositories?view=$rt_view&page=$rt_page&size=$rt_size$rt_filters"
    [ -z "$rt_query" ] || rt_path="$rt_path&q=$(printf '%s' "$rt_query" | rt_encode)"
    ;;
  *) rt_fail 'Usage: account | search [query] [options] | repository <ID> | watchlist [options]' ;;
esac
rt_request
