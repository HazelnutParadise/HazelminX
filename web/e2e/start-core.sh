#!/bin/sh
# Builds hazelminx-core from ../core and starts it on an empty data directory for the Playwright run.
# The first-run setup code is printed to stdout, which goes to $E2E_DIR/core.log (read by e2e/core.ts).
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
dir="${HAZELMINX_E2E_DIR:-$here/.e2e}"
listen="${HAZELMINX_E2E_CORE_ADDR:-127.0.0.1:19890}"
agent="${HAZELMINX_E2E_AGENT_ADDR:-127.0.0.1:19891}"
rm -rf "$dir"
mkdir -p "$dir/data"
(cd "$here/../core" && go build -o "$dir/hazelminx-core" ./cmd/hazelminx-core)
exec "$dir/hazelminx-core" -listen "$listen" -agent-listen "$agent" -data-dir "$dir/data" -log-format text >"$dir/core.log" 2>&1
