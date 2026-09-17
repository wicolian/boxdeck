#!/bin/sh
set -eu

VERSION=${1:?usage: update.sh VERSION}
VERSION=${VERSION#v}
REPO=${REPO:-wicolian/boxdeck}
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

download_hash() {
	asset=$1
	url="https://github.com/$REPO/releases/download/v$VERSION/$asset"
	curl -fsSL "$url" -o "$TMP/$asset"
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$TMP/$asset" | awk '{print $1}'
	else
		sha256sum "$TMP/$asset" | awk '{print $1}'
	fi
}

darwin_amd64=$(download_hash boxdeck_darwin_amd64)
darwin_arm64=$(download_hash boxdeck_darwin_arm64)
linux_amd64=$(download_hash boxdeck_linux_amd64)
linux_arm64=$(download_hash boxdeck_linux_arm64)
bar_darwin=$(download_hash boxdeck-bar_darwin_universal.zip)

sed_script="$TMP/render.sed"
cat > "$sed_script" <<EOF
s/REPLACE_DARWIN_AMD64/$darwin_amd64/g
s/REPLACE_DARWIN_ARM64/$darwin_arm64/g
s/REPLACE_LINUX_AMD64/$linux_amd64/g
s/REPLACE_LINUX_ARM64/$linux_arm64/g
s/REPLACE_BAR_DARWIN/$bar_darwin/g
s/0.1.0/$VERSION/g
EOF

cat > "$TMP/boxdeck.rb" <<'EOF'
class Boxdeck < Formula
  desc "Live console for a remote dev box"
  homepage "https://github.com/wicolian/boxdeck"
  version "0.1.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_arm64"
      sha256 "REPLACE_DARWIN_ARM64"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_amd64"
      sha256 "REPLACE_DARWIN_AMD64"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_arm64"
      sha256 "REPLACE_LINUX_ARM64"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_amd64"
      sha256 "REPLACE_LINUX_AMD64"
    end
  end

  def install
    bin.install Dir["boxdeck*"][0] => "boxdeck"
  end

  service do
    run [opt_bin/"boxdeck", "serve"]
    keep_alive true
  end
end
EOF
sed -f "$sed_script" "$TMP/boxdeck.rb" > "$ROOT/scripts/tap/Formula/boxdeck.rb"

cat > "$TMP/boxdeck-bar.rb" <<'EOF'
cask "boxdeck-bar" do
  version "0.1.0"
  sha256 "REPLACE_BAR_DARWIN"

  url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck-bar_darwin_universal.zip"
  name "boxdeck-bar"
  desc "Cross-platform menu bar client for boxdeck"
  homepage "https://github.com/wicolian/boxdeck"

  app "boxdeck-bar.app"
end
EOF
sed -f "$sed_script" "$TMP/boxdeck-bar.rb" > "$ROOT/scripts/tap/Casks/boxdeck-bar.rb"

printf '%s\n' "Rendered Homebrew files for v$VERSION. Push them to wicolian/homebrew-tap with:"
printf '%s\n' "git add Formula/boxdeck.rb Casks/boxdeck-bar.rb"
printf '%s\n' "git commit -m 'boxdeck $VERSION' && git push"
