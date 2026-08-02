#!/bin/bash
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/../.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/server" "$tmp/bin"

cat > "$tmp/bin/steamcmd" <<'EOF'
#!/bin/bash
printf '%q ' "$@" > "$STEAMCMD_LOG"
printf '%s' "${HBY_STEAMCMD_HOME:-}" > "$HBY_STEAMCMD_HOME_LOG"
printf '%s' "${HOME:-}" > "$HBY_HOME_LOG"
if [[ "${STEAMCMD_FAIL:-0}" == 1 ]]; then exit 42; fi
EOF
chmod +x "$tmp/bin/steamcmd"

cat > "$tmp/bin/hby-control" <<'EOF'
#!/bin/bash
printf '%s\n' "$@" > "$CONTROL_LOG"
EOF
chmod +x "$tmp/bin/hby-control"

common=(
  HBY_SERVER_DIR="$tmp/server"
  HBY_STEAMCMD_SKIP_INIT_DELAY=1
  STEAMCMD_BIN="$tmp/bin/steamcmd"
  STEAMCMD_LOG="$tmp/steamcmd.log"
  HBY_STEAMCMD_HOME_LOG="$tmp/steamcmd-home.log"
  HBY_HOME_LOG="$tmp/home.log"
  SRCDS_APPID=380870
  PATH="$tmp/bin:$PATH"
)

env "${common[@]}" AUTO_UPDATE=0 bash "$repo_root/steamcmd/entrypoint.sh" install
test -f "$tmp/server/.hby_steamcmd_installed"
grep -q 'app_update 380870' "$tmp/steamcmd.log"
test "$(cat "$tmp/steamcmd-home.log")" = "$tmp/server/.steamcmd"
test "$(cat "$tmp/home.log")" = "$tmp/server"

printf 'unchanged' > "$tmp/steamcmd.log"
env "${common[@]}" AUTO_UPDATE=0 bash "$repo_root/steamcmd/entrypoint.sh" install
test "$(cat "$tmp/steamcmd.log")" = unchanged

env "${common[@]}" AUTO_UPDATE=1 WINDOWS_INSTALL=1 VALIDATE=1 SRCDS_BETAID=unstable SRCDS_BETAPASS=secret INSTALL_FLAGS='-foo bar' bash "$repo_root/steamcmd/entrypoint.sh" install
grep -q '@sSteamCmdForcePlatformType windows' "$tmp/steamcmd.log"
grep -q -- '-beta unstable' "$tmp/steamcmd.log"
grep -q -- '-betapassword secret' "$tmp/steamcmd.log"
grep -q -- '-foo bar validate' "$tmp/steamcmd.log"

unlink "$tmp/server/.hby_steamcmd_installed"
if env "${common[@]}" STEAMCMD_FAIL=1 AUTO_UPDATE=1 bash "$repo_root/steamcmd/entrypoint.sh" install; then
  echo 'failed SteamCMD install unexpectedly succeeded' >&2
  exit 1
fi
test ! -f "$tmp/server/.hby_steamcmd_installed"
env "${common[@]}" AUTO_UPDATE=0 bash "$repo_root/steamcmd/entrypoint.sh" install

env "${common[@]}" CONTROL_LOG="$tmp/control.log" STARTUP='echo {{MESSAGE}}' MESSAGE=hello bash "$repo_root/steamcmd/entrypoint.sh" run
grep -q '^run$' "$tmp/control.log"
grep -q '^--$' "$tmp/control.log"
grep -q 'echo ${MESSAGE}' "$tmp/control.log"

if env HBY_SERVER_DIR="$tmp/server" HBY_STEAMCMD_SKIP_INIT_DELAY=1 bash "$repo_root/steamcmd/entrypoint.sh" invalid; then
  echo 'invalid mode unexpectedly succeeded' >&2
  exit 1
fi

echo 'entrypoint tests passed'
