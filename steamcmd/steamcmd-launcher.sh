#!/bin/bash

# Keep $0 stable while executing SteamCMD's mutable launcher. Its self-update
# path re-execs $0, returning here even if steamcmd.sh was replaced without an
# executable bit.
source "${HBY_STEAMCMD_HOME:?HBY_STEAMCMD_HOME must be set}/steamcmd.sh"
