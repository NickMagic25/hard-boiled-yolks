#!/bin/sh
set -eu

version_manifest_url="https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
server_jar="server.jar"
memory="${MC_MEMORY:-1024M}"
version="${MC_VERSION:-latest}"

if [ ! -f eula.txt ]; then
  echo "eula=true" > eula.txt
fi

if [ ! -f "$server_jar" ]; then
  manifest="$(curl -fsSL "$version_manifest_url")"

  if [ "$version" = "latest" ]; then
    version="$(printf '%s' "$manifest" | sed -n 's/.*"release"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  fi

  version_url="$(printf '%s' "$manifest" | tr '{' '\n' | sed -n "/\"id\"[[:space:]]*:[[:space:]]*\"$version\"/s/.*\"url\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" | head -n 1)"
  if [ -z "$version_url" ]; then
    echo "Unable to find Minecraft version '$version' in Mojang version manifest" >&2
    exit 1
  fi

  version_metadata="$(curl -fsSL "$version_url")"
  server_url="$(printf '%s' "$version_metadata" | tr '{' '\n' | awk '
    /"downloads"[[:space:]]*:/ { in_downloads = 1; next }
    in_downloads {
      line = $0
      if (line ~ /"server"[[:space:]]*:/) {
        in_server = 1
        sub(/^.*"server"[[:space:]]*:[[:space:]]*/, "", line)
      }
    }
    in_downloads && in_server && line ~ /"url"[[:space:]]*:/ {
      sub(/^.*"url"[[:space:]]*:[[:space:]]*"/, "", line)
      sub(/".*$/, "", line)
      print line
      exit
    }
  ')"
  if [ -z "$server_url" ]; then
    echo "Unable to find server download URL for Minecraft version '$version'" >&2
    exit 1
  fi

  echo "Downloading Minecraft server $version"
  curl -fsSL "$server_url" -o "$server_jar"
fi

exec java -Xms"$memory" -Xmx"$memory" -jar "$server_jar" nogui
