#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
export PATH="/usr/local/go/bin:$PATH"
if [[ ! -f "$ROOT/frontend/dist/index.html" ]]; then
  "$ROOT/scripts/build.sh"
elif command -v go >/dev/null 2>&1; then
  mkdir -p "$ROOT/bin" "$ROOT/doubao-web-api/bin"
  (cd "$ROOT/doubao-web-api" && go build -o bin/doubao-web-api ./cmd/server)
  (cd "$ROOT/backend" && go build -o ../bin/novaly-drama .)
elif [[ ! -x "$ROOT/bin/novaly-drama" || ! -x "$ROOT/doubao-web-api/bin/doubao-web-api" ]]; then
  echo "缺少已构建的后端，且当前 PATH 里没有 go。请先运行 ./scripts/build.sh" >&2
  exit 1
fi
cd "$ROOT/backend"
exec ../bin/novaly-drama
