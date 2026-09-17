#!/bin/sh
# Download the latest boxdeck binary, then run its interactive installer.
set -eu
case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "boxdeck supports Linux and macOS" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "boxdeck supports amd64 and arm64" >&2; exit 1 ;;
esac
bindir="${BOXDECK_BIN_DIR:-$HOME/.local/bin}"
mkdir -p "$bindir"
tmp="$(mktemp "$bindir/.boxdeck.XXXXXX")"
trap 'rm -f "$tmp"' EXIT HUP INT TERM
curl -fL --retry 3 "https://github.com/wicolian/boxdeck/releases/latest/download/boxdeck_${os}_${arch}" -o "$tmp"
chmod 755 "$tmp"
mv "$tmp" "$bindir/boxdeck"
echo "Installed $bindir/boxdeck"
"$bindir/boxdeck" install
