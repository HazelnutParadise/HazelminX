#!/usr/bin/env bash
# Builds web/dist, copies it into core/web/dist and builds hazelminx-core with the UI embedded.
#
# Usage: scripts/embed-web.sh [output-binary]   (default: core/bin/hazelminx-core)
# Needs Node.js 24 with web/ dependencies installed (cd web && vp install) and Go.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="${1:-$root/core/bin/hazelminx-core}"

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  sed -n '2,5p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
fi

(cd "$root/web" && ./node_modules/.bin/vp build)

rm -rf "$root/core/web/dist"
cp -R "$root/web/dist" "$root/core/web/dist"

(cd "$root/core" && go build -tags embedweb -o "$out" ./cmd/hazelminx-core)
echo "built $out with the web interface embedded"
