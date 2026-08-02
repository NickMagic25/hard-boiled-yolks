#!/bin/bash

#
# Copyright (c) 2021 Matthew Penner
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.
#

# Modes are used by the Helm chart to separate installation from the supervised
# game process. The default keeps the historical update-then-run behavior.
MODE="${1:-${HBY_STEAMCMD_MODE:-all}}"
SERVER_DIR="${HBY_SERVER_DIR:-/home/container}"
case "${MODE}" in
	all|install|run) ;;
	*) echo "unknown SteamCMD entrypoint mode: ${MODE}" >&2; exit 2 ;;
esac

if [ "${HBY_STEAMCMD_SKIP_INIT_DELAY:-0}" != "1" ]; then
	sleep 1
fi

# Default the TZ environment variable to UTC.
TZ=${TZ:-UTC}
export TZ

# Set environment for Steam Proton.
if command -v proton >/dev/null 2>&1; then
	if [ -n "${SRCDS_APPID}" ]; then
		mkdir -p "${SERVER_DIR}/.steam/steam/steamapps/compatdata/${SRCDS_APPID}"
		export STEAM_COMPAT_CLIENT_INSTALL_PATH="${SERVER_DIR}/.steam/steam"
		export STEAM_COMPAT_DATA_PATH="${SERVER_DIR}/.steam/steam/steamapps/compatdata/${SRCDS_APPID}"
		# Fix for pipx with protontricks
		export PATH=$PATH:/root/.local/bin
	else
		echo -e "----------------------------------------------------------------------------------"
		echo -e "WARNING!!! SRCDS_APPID is missing and must be set when using Proton"
		echo -e "Server will now terminate"
		echo -e "----------------------------------------------------------------------------------"
		exit 1
	fi
fi

# Switch to the container's working directory.
cd "${SERVER_DIR}" || exit 1

# SteamCMD derives its mutable Steam directory from HOME. The image runs as a
# non-root UID without a passwd-managed home, so keep that data on the
# server's persistent volume instead of falling back to /Steam.
export HOME="${HBY_HOME:-${SERVER_DIR}}"

# SteamCMD always updates its own client before handling a game update. The
# image ships a bootstrap client on its read-only filesystem, so keep its
# mutable runtime beside the game data instead.
export HBY_STEAMCMD_HOME="${HBY_STEAMCMD_HOME:-${SERVER_DIR}/.steamcmd}"

# Set default values for steam if not provided
STEAM_USER=${STEAM_USER:-anonymous}
if [ "${STEAM_USER}" == "anonymous" ]; then
	STEAM_PASS=""
	STEAM_AUTH=""
fi

install_server() {
	if [ -z "${SRCDS_APPID:-}" ]; then
		echo "SRCDS_APPID is required to install a Steam application" >&2
		return 2
	fi

	local steamcmd_bin="${STEAMCMD_BIN:-/usr/bin/steamcmd}"
	local -a command
	command=("${steamcmd_bin}" +force_install_dir "${SERVER_DIR}" +login "${STEAM_USER}" "${STEAM_PASS}" "${STEAM_AUTH}")
	if [ "${WINDOWS_INSTALL:-0}" = "1" ]; then command+=(+@sSteamCmdForcePlatformType windows); fi
	if [ -n "${HLDS_GAME:-}" ]; then command+=(+app_set_config 90 mod "${HLDS_GAME}"); fi
	command+=(+app_update "${SRCDS_APPID}")
	if [ -n "${SRCDS_BETAID:-}" ]; then command+=(-beta "${SRCDS_BETAID}"); fi
	if [ -n "${SRCDS_BETAPASS:-}" ]; then command+=(-betapassword "${SRCDS_BETAPASS}"); fi
	if [ -n "${INSTALL_FLAGS:-}" ]; then
		local -a install_flags
		read -r -a install_flags <<< "${INSTALL_FLAGS}"
		command+=("${install_flags[@]}")
	fi
	if [ "${VALIDATE:-0}" = "1" ]; then command+=(validate); fi
	if [ "${UPDATE_STEAMWORKS:-0}" = "1" ]; then command+=(+app_update 1007); fi
	command+=(+quit)
	"${command[@]}" || return $?
	touch "${SERVER_DIR}/.hby_steamcmd_installed"
}

if [ "${MODE}" = "install" ]; then
	if [ ! -f "${SERVER_DIR}/.hby_steamcmd_installed" ] || [ "${AUTO_UPDATE:-1}" = "1" ]; then
		install_server || exit $?
	else
		echo "Steam application already installed and automatic updates are disabled."
	fi
	exit 0
fi

if [ "${MODE}" = "all" ] && { [ -z "${AUTO_UPDATE:-}" ] || [ "${AUTO_UPDATE}" = "1" ]; }; then
	if [ -n "${SRCDS_APPID:-}" ]; then install_server || exit $?; fi
fi

# Replace Startup Variables
MODIFIED_STARTUP=$(echo ${STARTUP} | sed -e 's/{{/${/g' -e 's/}}/}/g')
echo -e ":${SERVER_DIR}$ ${MODIFIED_STARTUP}"

# Run the Server
if command -v hby-control >/dev/null 2>&1; then
	exec hby-control run -- /bin/bash -lc "${MODIFIED_STARTUP}"
fi
eval ${MODIFIED_STARTUP}
