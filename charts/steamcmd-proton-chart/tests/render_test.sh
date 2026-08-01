#!/bin/bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../../.." && pwd)
chart="$root/charts/steamcmd-proton-chart"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

helm template enshrouded "$chart" -f "$chart/examples/enshrouded.values.yaml" > "$tmp/enshrouded.yaml"

grep -q 'value: "1"' "$tmp/enshrouded.yaml"
grep -q 'value: "2278520"' "$tmp/enshrouded.yaml"
grep -q 'value: run' "$tmp/enshrouded.yaml"
grep -q 'proton run ./enshrouded_server.exe' "$tmp/enshrouded.yaml"
grep -q 'kubernetes.io/arch: amd64' "$tmp/enshrouded.yaml"
test "$(grep -c 'name: enshrouded-secrets' "$tmp/enshrouded.yaml")" -eq 1

install_line=$(grep -n 'name: steam-install' "$tmp/enshrouded.yaml" | head -1 | cut -d: -f1)
bootstrap_line=$(grep -n 'name: bootstrap' "$tmp/enshrouded.yaml" | tail -1 | cut -d: -f1)
test "$install_line" -lt "$bootstrap_line"

if helm template invalid "$chart" -f "$chart/examples/enshrouded.values.yaml" \
  --set steamcmd.appId=not-a-number >/dev/null 2>&1; then
  echo 'invalid App ID unexpectedly passed schema validation' >&2
  exit 1
fi

echo 'Proton chart render tests passed'
