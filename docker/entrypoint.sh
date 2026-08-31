#!/usr/bin/env bash
set -euo pipefail
umask 077
ui_pids=()
app_pid=""
cleanup() {
  trap - EXIT TERM INT
  if [[ -n "$app_pid" ]]; then
    kill -TERM "$app_pid" 2>/dev/null || true
    wait "$app_pid" 2>/dev/null || true
  fi
  for pid in "${ui_pids[@]}"; do kill -TERM "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 0' TERM INT
# Bind raw VNC only inside the container; never expose the Chrome debug ports.
# Generate a per-container password without printing it to Docker logs.
python3 - <<'PYCODE'
import secrets
from pathlib import Path
p=Path('/tmp/novaly-vnc-password')
p.write_text(secrets.token_urlsafe(6))
p.chmod(0o600)
PYCODE
x11vnc -storepasswd "$(cat /tmp/novaly-vnc-password)" /tmp/novaly-vnc-auth >/dev/null 2>&1
Xvfb "$DISPLAY" -screen 0 1440x900x24 -nolisten tcp &
ui_pids+=("$!")
ready=false
for attempt in {1..50}; do
  if xdpyinfo -display "$DISPLAY" >/dev/null 2>&1; then ready=true; break; fi
  sleep 0.1
done
if [[ "$ready" != true ]]; then echo "Virtual display failed" >&2; exit 1; fi
openbox --sm-disable >/tmp/novaly-openbox.log 2>&1 &
ui_pids+=("$!")
x11vnc -display "$DISPLAY" -localhost -rfbport 5900 -rfbauth /tmp/novaly-vnc-auth -forever -shared -noxdamage >/tmp/novaly-vnc.log 2>&1 &
ui_pids+=("$!")
websockify --web=/usr/share/novnc 0.0.0.0:6080 127.0.0.1:5900 &
ui_pids+=("$!")
cd /app/backend
../bin/novaly-drama > >(tee -a "${NOVALY_LOG_PATH:-/app/backend/data/novaly.log}") 2>&1 &
app_pid=$!
echo "Novaly: http://127.0.0.1:8085 ; browser desktop: http://127.0.0.1:6080/vnc.html"
echo "Read desktop password with: docker compose exec novaly cat /tmp/novaly-vnc-password"
# If any essential process exits, shut down the rest; Compose may restart the container.
set +e
wait -n "$app_pid" "${ui_pids[@]}"
status=$?
set -e
exit "$status"
