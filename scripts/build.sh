#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/frontend"
npm ci
npm run build
mkdir -p "$ROOT/bin" "$ROOT/doubao-web-api/bin"
cd "$ROOT/doubao-web-api"
go build -o bin/doubao-web-api ./cmd/server
cd "$ROOT/backend"
go build -o ../bin/novaly-drama .
