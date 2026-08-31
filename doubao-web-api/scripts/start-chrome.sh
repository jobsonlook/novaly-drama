#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Preserve caller-provided overrides before .env (multi-Chrome pool sets these).
_PRESERVE_SESSION="${DOUBAO_SESSION_DIR:-}"
_PRESERVE_PORT="${DOUBAO_CDP_PORT:-}"
_PRESERVE_IGNORE="${DOUBAO_IGNORE_ACTIVE_SESSION:-}"

if [[ -f "$ROOT/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$ROOT/.env"
  set +a
fi

if [[ -n "$_PRESERVE_SESSION" ]]; then
  export DOUBAO_SESSION_DIR="$_PRESERVE_SESSION"
fi
if [[ -n "$_PRESERVE_PORT" ]]; then
  export DOUBAO_CDP_PORT="$_PRESERVE_PORT"
fi
if [[ -n "$_PRESERVE_IGNORE" ]]; then
  export DOUBAO_IGNORE_ACTIVE_SESSION="$_PRESERVE_IGNORE"
fi

SESSION_DIR="${DOUBAO_SESSION_DIR:-$ROOT/session}"
CDP_PORT="${DOUBAO_CDP_PORT:-9222}"
ACTIVE_SESSION_FILE="${DOUBAO_ACTIVE_SESSION_FILE:-$ROOT/data/active_session}"

# When the caller already set DOUBAO_SESSION_DIR (multi-Chrome pool), do not
# override it with data/active_session. Set DOUBAO_IGNORE_ACTIVE_SESSION=1 to
# force the same behavior.
if [[ "${DOUBAO_IGNORE_ACTIVE_SESSION:-}" != "1" && -z "${DOUBAO_SESSION_DIR:-}" ]]; then
  if [[ -f "$ACTIVE_SESSION_FILE" ]]; then
    ACTIVE_DIR="$(tr -d '\r\n' < "$ACTIVE_SESSION_FILE" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    if [[ -n "$ACTIVE_DIR" ]]; then
      # Resolve relative paths against project root.
      if [[ "$ACTIVE_DIR" != /* ]]; then
        ACTIVE_DIR="$ROOT/$ACTIVE_DIR"
      fi
      SESSION_DIR="$ACTIVE_DIR"
      echo "Using active session from $ACTIVE_SESSION_FILE"
    fi
  fi
elif [[ -n "${DOUBAO_SESSION_DIR:-}" ]]; then
  SESSION_DIR="$DOUBAO_SESSION_DIR"
  # Resolve relative paths against project root.
  if [[ "$SESSION_DIR" != /* ]]; then
    SESSION_DIR="$ROOT/$SESSION_DIR"
  fi
  echo "Using explicit DOUBAO_SESSION_DIR=$SESSION_DIR (port=$CDP_PORT)"
fi

mkdir -p "$SESSION_DIR"

CHROME=""
if [[ -n "${CHROME_BIN:-}" ]]; then
  CHROME="$CHROME_BIN"
elif [[ "$(uname)" == "Darwin" ]]; then
  CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
elif command -v google-chrome >/dev/null 2>&1; then
  CHROME="$(command -v google-chrome)"
elif command -v google-chrome-stable >/dev/null 2>&1; then
  CHROME="$(command -v google-chrome-stable)"
elif command -v chromium >/dev/null 2>&1; then
  CHROME="$(command -v chromium)"
elif command -v chromium-browser >/dev/null 2>&1; then
  CHROME="$(command -v chromium-browser)"
fi

if [[ -z "$CHROME" || ! -x "$CHROME" ]]; then
  echo "Chrome/Chromium not found. Set CHROME_BIN to the binary path." >&2
  exit 1
fi

echo "Chrome:      $CHROME"
echo "Session dir: $SESSION_DIR"
echo "CDP port:    $CDP_PORT"
echo "DISPLAY:     ${DISPLAY:-<unset>}"
echo "Open https://www.doubao.com/chat/ and login if needed."
echo "Switch account: use http://127.0.0.1:8080/admin (or set DOUBAO_SESSION_DIR), quit Chrome, run this script again."

# Linux/server-only flags (headless VMs). Skip on macOS to avoid the yellow
# "--no-sandbox" banner and GPU/compositing issues.
if [[ "$(uname)" == "Darwin" ]]; then
  exec "$CHROME" \
    --remote-debugging-port="$CDP_PORT" \
    --user-data-dir="$SESSION_DIR" \
    --no-first-run \
    --no-default-browser-check \
    --disable-background-timer-throttling \
    --disable-backgrounding-occluded-windows \
    --disable-renderer-backgrounding \
    --disable-features=CalculateNativeWinOcclusion \
    "https://www.doubao.com/chat/"
fi

exec "$CHROME" \
  --remote-debugging-port="$CDP_PORT" \
  --user-data-dir="$SESSION_DIR" \
  --no-first-run \
  --no-default-browser-check \
  --disable-background-timer-throttling \
  --disable-backgrounding-occluded-windows \
  --disable-renderer-backgrounding \
  --disable-features=CalculateNativeWinOcclusion \
  --disable-dev-shm-usage \
  --disable-gpu \
  --no-sandbox \
  "https://www.doubao.com/chat/"
