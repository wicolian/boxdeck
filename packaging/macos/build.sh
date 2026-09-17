#!/bin/sh
set -eu

BINARY=${1:?path to the universal binary is required}
OUT=${2:?output app path is required}
VERSION=${3:-$(git describe --tags --always 2>/dev/null || echo 0.0.0)}
VERSION=${VERSION#v}
case "$VERSION" in
	*[!0-9.]*) VERSION=0.0.0 ;;
esac
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
rm -rf "$OUT"
mkdir -p "$OUT/Contents/MacOS" "$OUT/Contents/Resources"
cp "$BINARY" "$OUT/Contents/MacOS/BoxdeckBar"
sed "s/0.0.0/$VERSION/g" "$ROOT/packaging/macos/Info.plist" > "$OUT/Contents/Info.plist"
chmod 755 "$OUT/Contents/MacOS/BoxdeckBar"
codesign --force --deep --sign - "$OUT"
