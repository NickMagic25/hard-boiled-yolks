#!/bin/bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../../.." && pwd)
chart="$root/charts/minecraft-modrinth-fabric"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

helm template minecraft "$chart" -f "$chart/tests/features.values.yaml" > "$tmp/features.yaml"
grep -q '^kind: ExternalSecret$' "$tmp/features.yaml"
grep -q '^kind: TCPRoute$' "$tmp/features.yaml"
grep -q '^kind: HTTPRoute$' "$tmp/features.yaml"
grep -q 'name: MODRINTH_TOKEN' "$tmp/features.yaml"
grep -q 'libraries/example.jar' "$tmp/features.yaml"
grep -q 'checksum/installer:' "$tmp/features.yaml"

hash=$(printf 'a%.0s' {1..128})
helm template pinned "$chart" --set minecraft.eula=true --set modrinth.auth.enabled=false \
  --set modrinth.pack.url=https://example.com/modpack.mrpack --set modrinth.pack.sha512="$hash" > "$tmp/pinned.yaml"
grep -q 'MODRINTH_PACK_URL' "$tmp/pinned.yaml"
grep -q 'https://example.com/modpack.mrpack' "$tmp/pinned.yaml"

echo 'minecraft render tests passed'
