#!/usr/bin/env bash
# Installs boxdeck as a user systemd service. Run from the repo folder. Needs node >= 18.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
cfgdir="$HOME/.config/boxdeck"; cfg="$cfgdir/config.json"
mkdir -p "$cfgdir" "$HOME/.config/systemd/user"
if [ ! -f "$cfg" ]; then
  read -rp "Login user [admin]: " user; user="${user:-admin}"
  read -rsp "Password: " pass; echo
  read -rp "Hostname your browser uses for this box [$(hostname)]: " host; host="${host:-$(hostname)}"
  cat > "$cfg" <<JSON
{
  "user": "$user",
  "password": "$pass",
  "host": "$host",
  "port": 8100,
  "filesPort": 0,
  "quick": [],
  "reportRoots": ["~/reports", "~/box"],
  "known": {}
}
JSON
  chmod 600 "$cfg"; echo "wrote $cfg"
fi
cat > "$HOME/.config/systemd/user/boxdeck.service" <<UNIT
[Unit]
Description=boxdeck: one-page cockpit for this box

[Service]
ExecStart=$(command -v node) $here/server.js
Restart=always
RestartSec=3
Nice=10

[Install]
WantedBy=default.target
UNIT
systemctl --user daemon-reload
systemctl --user enable --now boxdeck.service
loginctl enable-linger "$USER" 2>/dev/null || true
sleep 1; systemctl --user --no-pager status boxdeck.service | sed -n 1,3p
echo "boxdeck is on http://127.0.0.1:$(node -e "try{console.log(require('$cfg').port||8100)}catch{console.log(8100)}")  -  expose it on your tailnet, never on the public internet."
