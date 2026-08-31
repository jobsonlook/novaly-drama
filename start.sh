#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
if [[ ! -x "$ROOT/bin/novaly-drama" || ! -x "$ROOT/doubao-web-api/bin/doubao-web-api" || ! -f "$ROOT/frontend/dist/index.html" ]]; then
 "$ROOT/scripts/build.sh"
fi
cd "$ROOT/backend"
exec ../bin/novaly-drama
