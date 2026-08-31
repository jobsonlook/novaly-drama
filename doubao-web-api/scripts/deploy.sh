#!/usr/bin/env bash
# Deploy doubao-web-api to an Ubuntu server (VNC + Chrome CDP + API systemd units).
#
# Usage:
#   DEPLOY_SSH_PASS='your-password' ./scripts/deploy.sh 124.221.76.123
#   DEPLOY_SSH_PASS='...' ./scripts/deploy.sh --sync-session 124.221.76.123
#   ./scripts/deploy.sh --key ~/.ssh/id_rsa ubuntu@124.221.76.123
#
# Env:
#   DEPLOY_SSH_PASS   SSH password (uses sshpass). Prefer SSH keys when possible.
#   DEPLOY_API_KEY    Optional; only used on first install if remote .env is missing.
#   DEPLOY_VNC_PASS   Optional; only used on first install if VNC passwd is missing.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REMOTE_DIR="${REMOTE_DIR:-doubao-web-api}"
SSH_USER="${SSH_USER:-ubuntu}"
SSH_PORT="${SSH_PORT:-22}"
SSH_KEY=""
SYNC_SESSION=0
SKIP_DEPS=0
HOST=""

usage() {
  cat <<'EOF'
Usage: deploy.sh [options] <host|user@host>

Options:
  --user NAME       SSH user (default: ubuntu)
  --port PORT       SSH port (default: 22)
  --key PATH        SSH private key (disables password auth)
  --remote-dir DIR  Remote project dir under home (default: doubao-web-api)
  --sync-session    Also rsync local ./session to remote (stops Chrome first)
  --skip-deps       Skip apt/Go/Chrome install (faster update)
  -h, --help        Show help

Environment:
  DEPLOY_SSH_PASS   Password for sshpass when not using --key
  DEPLOY_API_KEY    First-install API key (auto-generated if empty)
  DEPLOY_VNC_PASS   First-install VNC password (default DoubaoVnc9f3a2c)
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --user) SSH_USER="$2"; shift 2 ;;
    --port) SSH_PORT="$2"; shift 2 ;;
    --key) SSH_KEY="$2"; shift 2 ;;
    --remote-dir) REMOTE_DIR="$2"; shift 2 ;;
    --sync-session) SYNC_SESSION=1; shift ;;
    --skip-deps) SKIP_DEPS=1; shift ;;
    -h|--help) usage; exit 0 ;;
    -*)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
    *)
      if [[ -n "$HOST" ]]; then
        echo "Unexpected argument: $1" >&2
        exit 1
      fi
      HOST="$1"
      shift
      ;;
  esac
done

if [[ -z "$HOST" ]]; then
  usage >&2
  exit 1
fi

if [[ "$HOST" == *@* ]]; then
  SSH_USER="${HOST%%@*}"
  HOST="${HOST#*@}"
fi

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing required command: $1" >&2
    exit 1
  }
}

need_cmd rsync
need_cmd ssh

SSH_BASE=(ssh -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=15 -p "$SSH_PORT")
RSYNC_SSH="ssh -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=15 -p $SSH_PORT"

if [[ -n "$SSH_KEY" ]]; then
  SSH_BASE+=(-i "$SSH_KEY")
  RSYNC_SSH+=" -i $SSH_KEY"
elif [[ -n "${DEPLOY_SSH_PASS:-}" ]]; then
  need_cmd sshpass
  export SSHPASS="$DEPLOY_SSH_PASS"
  SSH_BASE=(sshpass -e "${SSH_BASE[@]}")
  RSYNC_SSH="sshpass -e $RSYNC_SSH"
else
  echo "Set DEPLOY_SSH_PASS or pass --key for SSH auth." >&2
  exit 1
fi

TARGET="${SSH_USER}@${HOST}"
REMOTE_HOME=""
REMOTE_PATH=""

ssh_run() {
  "${SSH_BASE[@]}" "$TARGET" "$@"
}

ssh_bash() {
  "${SSH_BASE[@]}" "$TARGET" "bash -s"
}

echo "==> Target: $TARGET:$SSH_PORT  remote=~/$REMOTE_DIR"

echo "==> Resolve remote home"
REMOTE_HOME="$(ssh_run 'printf %s "$HOME"')"
REMOTE_PATH="$REMOTE_HOME/$REMOTE_DIR"

echo "==> Sync project code (preserve .env / session / data)"
rsync -az --delete \
  --exclude '.git/' \
  --exclude '.env' \
  --exclude '.vnc_credentials' \
  --exclude 'session/' \
  --exclude 'sessions/' \
  --exclude 'data/' \
  --exclude 'doubao-web-api' \
  --exclude '*.db' \
  --exclude '.DS_Store' \
  -e "$RSYNC_SSH" \
  "$ROOT/" \
  "$TARGET:$REMOTE_PATH/"

if [[ "$SKIP_DEPS" -eq 0 ]]; then
  echo "==> Install / refresh dependencies (Go, Chrome, VNC)"
  ssh_bash <<'REMOTE'
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
export PATH="/usr/local/go/bin:$PATH"

sudo apt-get update -qq
sudo apt-get install -y -qq \
  curl wget ca-certificates gnupg unzip \
  tigervnc-standalone-server tigervnc-common \
  openbox xterm dbus-x11 fonts-noto-cjk \
  fonts-liberation libnss3 libatk-bridge2.0-0 libgtk-3-0 libxss1 libasound2t64 \
  xdg-utils >/dev/null || sudo apt-get install -y -qq \
  curl wget ca-certificates gnupg unzip \
  tigervnc-standalone-server tigervnc-common \
  openbox xterm dbus-x11 fonts-noto-cjk \
  fonts-liberation libnss3 libatk-bridge2.0-0 libgtk-3-0 libxss1 libasound2 \
  xdg-utils >/dev/null

if ! /usr/local/go/bin/go version 2>/dev/null | grep -q 'go1.24'; then
  cd /tmp
  curl -fsSL -o go.tgz https://go.dev/dl/go1.24.4.linux-amd64.tar.gz
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf go.tgz
  rm -f go.tgz
fi
grep -q '/usr/local/go/bin' ~/.profile 2>/dev/null || echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile

if ! command -v google-chrome >/dev/null 2>&1; then
  cd /tmp
  curl -fsSL -o chrome.deb https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
  sudo apt-get install -y -qq ./chrome.deb >/dev/null || sudo apt-get install -y -f -qq >/dev/null
  rm -f chrome.deb
fi

echo "go=$(/usr/local/go/bin/go version)"
echo "chrome=$(command -v google-chrome)"
REMOTE
fi

if [[ "$SYNC_SESSION" -eq 1 ]]; then
  if [[ ! -d "$ROOT/session" ]]; then
    echo "Local session/ not found; skip --sync-session" >&2
  else
    echo "==> Stop Chrome/API and sync local session/"
    ssh_bash <<REMOTE
set -euo pipefail
sudo systemctl stop doubao-web-api.service doubao-chrome.service 2>/dev/null || true
sleep 2
pkill -f 'remote-debugging-port=9222' 2>/dev/null || true
sleep 1
mkdir -p "$REMOTE_PATH/session"
# Drop Chrome singleton locks from macOS profile
rm -f "$REMOTE_PATH/session"/SingletonLock \
      "$REMOTE_PATH/session"/SingletonSocket \
      "$REMOTE_PATH/session"/SingletonCookie
REMOTE
    rsync -az --delete \
      --exclude 'Crashpad/' \
      --exclude 'BrowserMetrics*' \
      --exclude 'ShaderCache/' \
      --exclude 'GPUCache/' \
      --exclude 'GrShaderCache/' \
      --exclude 'Code Cache/' \
      --exclude 'Service Worker/CacheStorage/' \
      --exclude 'SingletonLock' \
      --exclude 'SingletonSocket' \
      --exclude 'SingletonCookie' \
      -e "$RSYNC_SSH" \
      "$ROOT/session/" \
      "$TARGET:$REMOTE_PATH/session/"
  fi
fi

API_KEY_DEFAULT="${DEPLOY_API_KEY:-}"
VNC_PASS_DEFAULT="${DEPLOY_VNC_PASS:-DoubaoVnc9f3a2c}"

echo "==> Configure .env, VNC, systemd, build, restart"
ssh_bash <<REMOTE
set -euo pipefail
export PATH="/usr/local/go/bin:\$PATH"
cd "$REMOTE_PATH"
mkdir -p session data sessions ~/.vnc

# Preserve existing API key; generate only on first install.
if [[ -f .env ]] && grep -q '^DOUBAO_API_KEY=.' .env; then
  API_KEY="\$(grep '^DOUBAO_API_KEY=' .env | head -1 | cut -d= -f2-)"
elif [[ -n "$API_KEY_DEFAULT" ]]; then
  API_KEY="$API_KEY_DEFAULT"
else
  API_KEY="\$(openssl rand -hex 24)"
fi

if [[ -f .vnc_credentials ]]; then
  VNC_PASS="\$(grep '^VNC_PASS=' .vnc_credentials | head -1 | cut -d= -f2-)"
else
  VNC_PASS="$VNC_PASS_DEFAULT"
fi

if [[ ! -f .env ]]; then
  cat > .env <<EOF
DOUBAO_CDP_URL=http://127.0.0.1:9222
DOUBAO_CDP_PORT=9222
DOUBAO_SESSION_DIR=./session
DOUBAO_ACCOUNTS_DB=./data/accounts.db
DOUBAO_ACTIVE_SESSION_FILE=./data/active_session
DOUBAO_CHROME_SCRIPT=./scripts/start-chrome.sh
DOUBAO_AUTO_RESTART_CHROME=1
PORT=8080
DOUBAO_API_KEY=\${API_KEY}
REQUEST_TIMEOUT=120
VIDEO_TIMEOUT=600
VIDEO_UI_MODE=skill
DISPLAY=:1
EOF
else
  # Ensure required keys exist without wiping custom values.
  grep -q '^DOUBAO_CDP_URL=' .env || echo 'DOUBAO_CDP_URL=http://127.0.0.1:9222' >> .env
  grep -q '^DOUBAO_CDP_PORT=' .env || echo 'DOUBAO_CDP_PORT=9222' >> .env
  grep -q '^PORT=' .env || echo 'PORT=8080' >> .env
  grep -q '^DISPLAY=' .env || echo 'DISPLAY=:1' >> .env
  grep -q '^DOUBAO_API_KEY=' .env || echo "DOUBAO_API_KEY=\${API_KEY}" >> .env
fi
chmod 600 .env

printf '%s\\n' "\$VNC_PASS" | vncpasswd -f > ~/.vnc/passwd
chmod 600 ~/.vnc/passwd
printf 'VNC_PASS=%s\\n' "\$VNC_PASS" > .vnc_credentials
chmod 600 .vnc_credentials

cat > ~/.vnc/xstartup <<'XEOF'
#!/bin/sh
unset SESSION_MANAGER
unset DBUS_SESSION_BUS_ADDRESS
export XKL_XMODMAP_DISABLE=1
[ -x /etc/vnc/xstartup ] && exec /etc/vnc/xstartup
[ -r \$HOME/.Xresources ] && xrdb \$HOME/.Xresources
xsetroot -solid grey
xterm -geometry 100x30+10+10 -ls -title "\$VNCDESKTOP Desktop" &
exec openbox
XEOF
chmod +x ~/.vnc/xstartup
chmod +x scripts/start-chrome.sh

# Drop stale Chrome locks (common after macOS session copy)
rm -f session/SingletonLock session/SingletonSocket session/SingletonCookie

echo "== build =="
go build -o doubao-web-api ./cmd/server

sudo tee /etc/systemd/system/doubao-vnc.service >/dev/null <<EOF
[Unit]
Description=Doubao TigerVNC :1
After=network.target

[Service]
Type=forking
User=$SSH_USER
Group=$SSH_USER
WorkingDirectory=$REMOTE_HOME
ExecStartPre=-/usr/bin/vncserver -kill :1
ExecStart=/usr/bin/vncserver :1 -geometry 1280x800 -depth 24 -localhost no -SecurityTypes VncAuth
ExecStop=/usr/bin/vncserver -kill :1
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

sudo tee /etc/systemd/system/doubao-chrome.service >/dev/null <<EOF
[Unit]
Description=Doubao Chrome CDP
After=doubao-vnc.service
Requires=doubao-vnc.service

[Service]
Type=simple
User=$SSH_USER
Group=$SSH_USER
WorkingDirectory=$REMOTE_PATH
EnvironmentFile=$REMOTE_PATH/.env
Environment=DISPLAY=:1
Environment=HOME=$REMOTE_HOME
ExecStartPre=/bin/sleep 2
ExecStart=$REMOTE_PATH/scripts/start-chrome.sh
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo tee /etc/systemd/system/doubao-web-api.service >/dev/null <<EOF
[Unit]
Description=Doubao Web API
After=doubao-chrome.service network.target
Wants=doubao-chrome.service

[Service]
Type=simple
User=$SSH_USER
Group=$SSH_USER
WorkingDirectory=$REMOTE_PATH
EnvironmentFile=$REMOTE_PATH/.env
Environment=DISPLAY=:1
Environment=PATH=/usr/local/go/bin:/usr/bin:/bin
ExecStartPre=/bin/sleep 8
ExecStart=$REMOTE_PATH/doubao-web-api
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable doubao-vnc doubao-chrome doubao-web-api >/dev/null

sudo systemctl restart doubao-vnc
sleep 2
sudo systemctl restart doubao-chrome
for i in \$(seq 1 30); do
  if curl -fsS -m 2 http://127.0.0.1:9222/json/version >/dev/null 2>&1; then
    echo "CDP ready after ~\$((i*2))s"
    break
  fi
  sleep 2
done
curl -fsS -m 3 http://127.0.0.1:9222/json/version >/dev/null || echo "WARN: CDP not ready yet"

sudo systemctl restart doubao-web-api
sleep 6

echo "=== status ==="
systemctl is-active doubao-vnc doubao-chrome doubao-web-api || true
ss -lntp | grep -E '5901|9222|8080' || true
HTTP=\$(curl -sS -m 5 -o /dev/null -w '%{http_code}' -H "Authorization: Bearer \$API_KEY" http://127.0.0.1:8080/admin || true)
echo "admin_http=\$HTTP"
echo "API_KEY=\$API_KEY"
echo "VNC_PASS=\$VNC_PASS"
REMOTE

echo
echo "==> Deploy done: $HOST"
echo "    API:  http://$HOST:8080  (needs security group TCP 8080)"
echo "    VNC:  $HOST:5901         (needs security group TCP 5901)"
echo "    Tunnel tip:"
echo "      ssh -L 8080:127.0.0.1:8080 -L 5901:127.0.0.1:5901 $TARGET"
echo "    Credentials are printed above (API_KEY / VNC_PASS)."
echo "    Admin: http://127.0.0.1:8080/admin?key=<API_KEY>"
