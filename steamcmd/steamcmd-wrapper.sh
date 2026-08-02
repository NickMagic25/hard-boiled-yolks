#!/bin/sh

STEAMCMD_BOOTSTRAP=/usr/lib/games/steam
STEAMROOT="${HBY_STEAMCMD_HOME:-${HOME:-/home/container}/.steamcmd}"

# SteamCMD updates itself. Its client state is disposable, and an executable
# launcher from an earlier image can still refer to unavailable runtime files.
# Always restore the image's known-good bootstrap before an install. This only
# resets .steamcmd; the game server files on the persistent volume are kept.
rm -rf "${STEAMROOT}"
mkdir -p "${STEAMROOT}"
cp -R "${STEAMCMD_BOOTSTRAP}/." "${STEAMROOT}/"

if [ ! -x "${STEAMROOT}/steamcmd.sh" ]; then
  echo "SteamCMD bootstrap is incomplete at ${STEAMROOT}" >&2
  exit 1
fi

export LD_LIBRARY_PATH="${STEAMROOT}/linux32:/usr/lib/i386-linux-gnu:${LD_LIBRARY_PATH:-}"
ulimit -n 2048

STEAMCMD_STDERR_LOG="${HBY_STEAMCMD_STDERR_LOG:-${PWD}/Steam/logs/stderr.txt}"
tail_pid=""
if mkdir -p "$(dirname "${STEAMCMD_STDERR_LOG}")" && : > "${STEAMCMD_STDERR_LOG}"; then
  tail -n 0 -f "${STEAMCMD_STDERR_LOG}" &
  tail_pid=$!
fi

cleanup() {
  if [ -n "${tail_pid}" ]; then
    kill "${tail_pid}" 2>/dev/null || true
    wait "${tail_pid}" 2>/dev/null || true
  fi
}
trap cleanup EXIT HUP INT TERM

"${STEAMROOT}/steamcmd.sh" "$@"
status=$?
exit "${status}"
