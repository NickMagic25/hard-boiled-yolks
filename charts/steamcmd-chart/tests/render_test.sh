#!/bin/bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../../.." && pwd)
chart="$root/charts/steamcmd-chart"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

helm template features "$chart" -f "$chart/tests/shared-features.values.yaml" > "$tmp/features.yaml"
grep -q '^kind: ExternalSecret$' "$tmp/features.yaml"
grep -q '^kind: HTTPRoute$' "$tmp/features.yaml"
grep -q '^kind: TCPRoute$' "$tmp/features.yaml"
grep -q '^kind: UDPRoute$' "$tmp/features.yaml"
grep -q 'protocol: UDP' "$tmp/features.yaml"

helm template palworld "$chart" -f "$chart/examples/palworld.values.yaml" > "$tmp/palworld.yaml"
install_line=$(grep -n 'name: steam-install' "$tmp/palworld.yaml" | head -1 | cut -d: -f1)
bootstrap_line=$(grep -n 'name: bootstrap' "$tmp/palworld.yaml" | tail -1 | cut -d: -f1)
test "$install_line" -lt "$bootstrap_line"
grep -q 'checksum/bootstrap:' "$tmp/palworld.yaml"
grep -q 'name: palworld-steamcmd-chart-bootstrap' "$tmp/palworld.yaml"

helm template existing "$chart" -f "$chart/examples/7-days-to-die.values.yaml" \
  --set hby.persistence.existingClaim=existing-game-data > "$tmp/existing.yaml"
! grep -q '^kind: PersistentVolumeClaim$' "$tmp/existing.yaml"
grep -q 'claimName: existing-game-data' "$tmp/existing.yaml"

if helm template invalid "$chart" -f "$chart/examples/7-days-to-die.values.yaml" --set hby.replicaCount=2 >/dev/null 2>&1; then
  echo 'replicaCount=2 unexpectedly passed schema validation' >&2
  exit 1
fi

echo 'chart render tests passed'
