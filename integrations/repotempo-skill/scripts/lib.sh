#!/bin/sh
# Shared by the installed client. Never source the credential file as shell code.
rt_fail() { printf '%s\n' "RepoTempo: $*" >&2; exit 1; }
rt_dependencies() {
  for rt_dependency in curl openssl od awk grep mktemp date wc tr cat ls id rm rmdir dirname; do
    command -v "$rt_dependency" >/dev/null 2>&1 || rt_fail "Install the missing system utility: $rt_dependency."
  done
}
rt_validate_config() {
  # Origins only: no userinfo, path, query, fragment, whitespace or shell syntax.
  [ "$(printf '%s' "$rt_url$rt_ak$rt_sk" | tr -d '\r\n')" = "$rt_url$rt_ak$rt_sk" ] || rt_fail 'Credentials and origin must each be a single line.'
  printf '%s\n' "$rt_url" | LC_ALL=C grep -Eq '^https://([A-Za-z0-9][A-Za-z0-9.-]*|\[[0-9A-Fa-f:]+\])(:[0-9]{1,5})?$|^http://(localhost|127\.0\.0\.1|\[::1\])(:[0-9]{1,5})?$' || rt_fail 'Use an HTTPS origin without a trailing slash or path (HTTP is allowed only on loopback).'
  printf '%s\n' "$rt_ak" | LC_ALL=C grep -Eq '^rt_ak_[a-f0-9]{32}$' || rt_fail 'Invalid AK; copy a new install instruction from Agent 访问.'
  printf '%s\n' "$rt_sk" | LC_ALL=C grep -Eq '^rt_sk_[a-f0-9]{64}$' || rt_fail 'Invalid SK; copy a new install instruction from Agent 访问.'
}
rt_load_config() {
  if [ -n "${REPOTEMPO_URL-}${REPOTEMPO_AK-}${REPOTEMPO_SK-}" ]; then
    rt_url=${REPOTEMPO_URL-}; rt_ak=${REPOTEMPO_AK-}; rt_sk=${REPOTEMPO_SK-}
  else
    rt_credentials="$rt_skill_dir/.credentials"
    [ -f "$rt_credentials" ] && [ ! -L "$rt_credentials" ] || rt_fail 'No local credentials. Run the install instruction from Agent 访问.'
    [ "$(LC_ALL=C ls -nd "$rt_credentials" | awk '{mode=$1; sub(/@$/, "", mode); print mode ":" $3}')" = "-rw-------:$(id -u)" ] || rt_fail 'Credential file must be owned by you with permission 600 and no extended ACL.'
    { IFS= read -r rt_url && IFS= read -r rt_ak && IFS= read -r rt_sk && { ! IFS= read -r rt_extra && [ -z "$rt_extra" ]; }; } < "$rt_credentials" || rt_fail 'Invalid local credential file. Reinstall from Agent 访问.'
  fi
  unset REPOTEMPO_URL REPOTEMPO_AK REPOTEMPO_SK
  rt_validate_config
}
rt_encode() {
  # Encode UTF-8 bytes, not characters; the signed path is the transmitted path.
  LC_ALL=C od -An -tu1 -v | LC_ALL=C awk '{ for (i=1; i<=NF; i++) printf "%%%02X", $i }'
}
rt_hash() { openssl dgst -sha256 -binary | od -An -tx1 -v | tr -d ' \n'; }
rt_cleanup() {
  [ -n "${rt_tmp-}" ] || return 0
  rm -f "$rt_tmp/key" "$rt_tmp/inner" "$rt_tmp/outer" "$rt_tmp/headers" "$rt_tmp/body"
  rmdir "$rt_tmp" 2>/dev/null || :
}
rt_request() {
  umask 077
  rt_tmp=$(mktemp -d "${TMPDIR:-/tmp}/repotempo-request.XXXXXXXX") || rt_fail 'Cannot create a private request directory.'
  trap rt_cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM HUP
  printf '%s' "$rt_sk" | openssl dgst -sha256 -binary > "$rt_tmp/key" || rt_fail 'Cannot derive a request signature.'
  unset rt_sk
  # Build HMAC pads via stdin/private files, keeping both SK and the equivalent
  # signing key out of process arguments (openssl -macopt would expose the key).
  for rt_pad in inner outer; do
    if [ "$rt_pad" = inner ]; then rt_pad_byte=54; else rt_pad_byte=92; fi
    od -An -tu1 -v "$rt_tmp/key" | LC_ALL=C awk -v pad="$rt_pad_byte" '
      function rt_xor(a,b, value,p) {value=0; p=1; while(a>0 || b>0) {if(a%2 != b%2) value+=p; a=int(a/2); b=int(b/2); p*=2} return value}
      {for(i=1;i<=NF;i++) {printf "%c", rt_xor($i,pad); count++}}
      END {for(i=count;i<64;i++) printf "%c", pad}' > "$rt_tmp/$rt_pad"
  done
  rt_timestamp=$(date +%s)
  rt_nonce=$(openssl rand -hex 24) || rt_fail 'Cannot create a request nonce.'
  rt_body_hash=$(printf '' | rt_hash)
  rt_signature=$({ cat "$rt_tmp/outer"; { cat "$rt_tmp/inner"; printf 'GET\n%s\n%s\n%s\n%s' "$rt_path" "$rt_timestamp" "$rt_nonce" "$rt_body_hash"; } | openssl dgst -sha256 -binary; } | rt_hash)
  printf 'header = "Accept: application/json"\nheader = "X-RepoTempo-Key: %s"\nheader = "X-RepoTempo-Timestamp: %s"\nheader = "X-RepoTempo-Nonce: %s"\nheader = "X-RepoTempo-Signature: %s"\n' "$rt_ak" "$rt_timestamp" "$rt_nonce" "$rt_signature" > "$rt_tmp/headers"
  # Disable user curlrc, never follow redirects and never put credentials in URL.
  rt_response=$(curl -q --silent --show-error --proto '=https,http' --connect-timeout 10 --max-time 15 --max-filesize 2097152 --config "$rt_tmp/headers" --output "$rt_tmp/body" --write-out '%{http_code} %{content_type}' "$rt_url$rt_path") || rt_fail 'Request failed; check the endpoint and network.'
  case "$rt_response" in
    200\ application/json*) ;;
    401\ *|403\ *) rt_fail 'Authentication failed; check expiry/revocation, scopes and system clock.' ;;
    429\ *) rt_fail 'Rate limit reached. Wait before retrying.' ;;
    30[0-9]\ *) rt_fail 'Redirect rejected. Reinstall using the final HTTPS origin.' ;;
    404\ *) rt_fail 'Project or API endpoint not found.' ;;
    *) rt_fail 'Unexpected HTTP response; no result returned.' ;;
  esac
  [ "$(wc -c < "$rt_tmp/body")" -le 2097152 ] || rt_fail 'Response exceeds 2 MiB.'
  cat "$rt_tmp/body"
  printf '\n'
}
