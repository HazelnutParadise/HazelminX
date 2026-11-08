#!/usr/bin/env bash
# Builds web/dist, copies it into core/web/dist and builds hazelminx-core with the UI embedded.
#
# Usage: scripts/embed-web.sh [output-binary]   (default: core/bin/hazelminx-core; relative paths are from the current directory)
# Needs Node.js 24 with web/ dependencies installed (cd web && vp install) and Go.
set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  sed -n '2,5p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
out="${1:-$root/core/bin/hazelminx-core}"
mkdir -p "$(dirname "$out")"
out="$(cd "$(dirname "$out")" && pwd)/$(basename "$out")"

(cd "$root/web" && ./node_modules/.bin/vp build)

# Copy next to the target and swap it in, so a failed copy never leaves a partial dist to embed.
staging="$(mktemp -d "$root/core/web/.dist.XXXXXX")"
trap 'rm -rf "$staging"' EXIT
cp -R "$root/web/dist/." "$staging/"
rm -rf "$root/core/web/dist"
mv "$staging" "$root/core/web/dist"
trap - EXIT

(cd "$root/core" && go build -tags embedweb -o "$out" ./cmd/hazelminx-core)
echo "built $out with the web interface embedded"
