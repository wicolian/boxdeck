#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
failed=0
printf '%-24s %-16s %s\n' component pinned latest

major() { printf '%s' "$1" | sed 's/^v//' | cut -d. -f1; }

check_action() {
  name=$1
  repo=$2
  pinned=$(grep -RhoE "actions/$name@v[0-9]+" "$ROOT/.github/workflows" | sed 's/.*@//' | sort -V | tail -1)
  latest=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | jq -r .tag_name)
  printf '%-24s %-16s %s\n' "actions/$name" "$pinned" "$latest"
  [ "$(major "$pinned")" -ge "$(major "$latest")" ] || failed=1
}

check_action checkout actions/checkout
check_action setup-go actions/setup-go
check_action upload-artifact actions/upload-artifact
check_action download-artifact actions/download-artifact

go_pinned=$(sed -n 's/^go //p' "$ROOT/go.mod")
go_latest=$(curl -fsSL https://go.dev/doc/devel/release | grep -oE 'go1\.[0-9]+\.[0-9]+' | sort -V | tail -1)
printf '%-24s %-16s %s\n' go "$go_pinned" "$go_latest"
[ "$(major "$go_pinned")" -ge "$(major "${go_latest#go}")" ] || failed=1

tray_pinned=$(sed -n 's/.*fyne.io\/systray v//p' "$ROOT/cmd/boxdeck-bar/go.mod")
tray_latest=$(curl -fsSL https://proxy.golang.org/fyne.io/systray/@latest | jq -r .Version)
printf '%-24s %-16s %s\n' fyne.io/systray "$tray_pinned" "$tray_latest"
[ "$(major "v$tray_pinned")" -ge "$(major "$tray_latest")" ] || failed=1

xcodegen_latest=$(curl -fsSL https://formulae.brew.sh/api/formula/xcodegen.json | jq -r .versions.stable)
printf '%-24s %-16s %s\n' XcodeGen brew-latest "$xcodegen_latest"

if [ "$failed" -ne 0 ]; then
  echo "A dependency is behind by a major version."
  exit 1
fi
