#!/bin/sh
set +x
set -eu
umask 077
fail() { printf '%s\n' "RepoTempo: $*" >&2; exit 1; }
for dependency in curl openssl tar gzip mktemp chmod mkdir mv rm rmdir od awk grep date wc tr cat ls id dirname uname; do
  command -v "$dependency" >/dev/null 2>&1 || fail "Install the missing system utility: $dependency."
done
install_url=${REPOTEMPO_URL-}; install_ak=${REPOTEMPO_AK-}; install_sk=${REPOTEMPO_SK-}
unset REPOTEMPO_URL REPOTEMPO_AK REPOTEMPO_SK
[ "$(printf '%s' "$install_url$install_ak$install_sk" | tr -d '\r\n')" = "$install_url$install_ak$install_sk" ] || fail 'Credentials and origin must each be a single line.'
printf '%s\n' "$install_url" | LC_ALL=C grep -Eq '^https://([A-Za-z0-9][A-Za-z0-9.-]*|\[[0-9A-Fa-f:]+\])(:[0-9]{1,5})?$|^http://(localhost|127\.0\.0\.1|\[::1\])(:[0-9]{1,5})?$' || fail 'Set REPOTEMPO_URL to an HTTPS origin (HTTP only on loopback).'
printf '%s\n' "$install_ak" | grep -Eq '^rt_ak_[a-f0-9]{32}$' || fail 'Invalid AK. Copy the complete install instruction from Agent 访问.'
printf '%s\n' "$install_sk" | grep -Eq '^rt_sk_[a-f0-9]{64}$' || fail 'Invalid SK. Copy the complete install instruction from Agent 访问.'
install_target=${REPOTEMPO_SKILL_DIR:-${HOME:?}/.agents/skills/repotempo}
case "$install_target" in /*/repotempo) ;; *) fail 'REPOTEMPO_SKILL_DIR must be an absolute path ending in /repotempo.' ;; esac
case "$install_target" in */../*|*/./*|*//*) fail 'Install path must not contain /../, /./ or doubled slashes.' ;; esac
[ ! -L "$install_target" ] || fail 'Refusing to replace a symlinked Skill directory.'
install_parent=${install_target%/*}
mkdir -p "$install_parent"
install_stage=$(mktemp -d "$install_parent/.repotempo-install.XXXXXXXX") || fail 'Cannot create a private install directory.'
install_platform=$(uname -s)
cleanup() {
  rm -f "$install_stage/archive.tar.gz" "$install_stage/SKILL.md" "$install_stage/repotempo.sh" "$install_stage/lib.sh" "$install_stage/.credentials" "$install_stage/.repotempo-skill"
  rmdir "$install_stage" 2>/dev/null || :
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
# Clear inherited macOS ACLs before any credential is written. Only this newly
# created private staging directory is changed here, never the user's parent.
if [ "$install_platform" = Darwin ]; then chmod -N "$install_stage"; fi
install_status=$(curl -q --silent --show-error --proto '=https,http' --connect-timeout 10 --max-time 30 --max-filesize 1048576 --output "$install_stage/archive.tar.gz" --write-out '%{http_code}' "$install_url/integrations/repotempo-skill.tar.gz") || fail 'Skill download failed; check the network.'
[ "$install_status" = 200 ] || fail 'Skill download failed or redirected. Use the final HTTPS origin.'
[ "$(wc -c < "$install_stage/archive.tar.gz")" -le 1048576 ] || fail 'Skill download is unexpectedly large.'
# Extract only named file contents: archive paths or symlinks cannot escape staging.
tar -xzOf "$install_stage/archive.tar.gz" repotempo/SKILL.md > "$install_stage/SKILL.md" || fail 'Invalid Skill archive.'
tar -xzOf "$install_stage/archive.tar.gz" repotempo/scripts/repotempo.sh > "$install_stage/repotempo.sh" || fail 'Invalid Skill client.'
tar -xzOf "$install_stage/archive.tar.gz" repotempo/scripts/lib.sh > "$install_stage/lib.sh" || fail 'Invalid Skill library.'
printf '%s\n%s\n%s\n' "$install_url" "$install_ak" "$install_sk" > "$install_stage/.credentials"
unset install_ak install_sk
printf 'repotempo-skill-v1\n' > "$install_stage/.repotempo-skill"
for install_file in SKILL.md repotempo.sh lib.sh .credentials .repotempo-skill; do
  [ -s "$install_stage/$install_file" ] || fail 'Downloaded Skill contains an empty file.'
  chmod 600 "$install_stage/$install_file"
done
if [ -e "$install_target" ]; then
  [ -d "$install_target" ] && [ -f "$install_target/.repotempo-skill" ] && [ ! -L "$install_target/.repotempo-skill" ] || fail 'Destination already exists and is not managed by this installer; choose another Skill directory.'
  [ "$(cat "$install_target/.repotempo-skill")" = repotempo-skill-v1 ] || fail 'Unrecognized existing Skill; leaving it untouched.'
  [ "$(LC_ALL=C ls -nd "$install_target" | awk '{print $3}')" = "$(id -u)" ] || fail 'Existing Skill is not owned by you.'
  for install_file in SKILL.md .credentials .repotempo-skill scripts scripts/repotempo.sh scripts/lib.sh; do
    [ ! -L "$install_target/$install_file" ] || fail 'Refusing to overwrite a symlink in the existing Skill.'
  done
  [ ! -e "$install_target/scripts" ] || [ -d "$install_target/scripts" ] || fail 'Existing scripts path is not a directory.'
  for install_file in SKILL.md .credentials .repotempo-skill scripts/repotempo.sh scripts/lib.sh; do
    [ ! -e "$install_target/$install_file" ] || [ -f "$install_target/$install_file" ] || fail 'Existing managed path is not a regular file.'
  done
else
  mkdir "$install_target"
fi
mkdir -p "$install_target/scripts"
if [ "$install_platform" = Darwin ]; then
  chmod -N "$install_target" "$install_target/scripts" "$install_stage/.credentials"
fi
chmod 700 "$install_target" "$install_target/scripts"
mv "$install_stage/SKILL.md" "$install_target/SKILL.md"
mv "$install_stage/repotempo.sh" "$install_target/scripts/repotempo.sh"
mv "$install_stage/lib.sh" "$install_target/scripts/lib.sh"
mv "$install_stage/.repotempo-skill" "$install_target/.repotempo-skill"
# Replace credentials last, atomically. Preserve unrelated files in managed installs.
mv "$install_stage/.credentials" "$install_target/.credentials"
printf 'RepoTempo Skill installed: %s\n' "$install_target"
printf 'Open a new agent task and ask it to use the repotempo Skill. No Node or Python is needed.\n'
