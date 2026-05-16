#!/bin/sh

# SkyrimTogether Egg Installation Script
# Author: Hayden Andreyka (haydenandreyka@gmail.com)
# Adapted for Wolfi by Nick Majkic

# Description: Copies SkyrimTogether server binaries from the installer image
# to the persistent mounted storage on the Pterodactyl server.

if [ -d "/mnt/server/bin" ]; then
    rm -rf /mnt/server/bin/*
else
    mkdir -p /mnt/server/bin
fi

cp /st-server/* /mnt/server/bin

echo "Done installing. Re-run the installer to update the server."
