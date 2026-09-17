#!/bin/sh
set -eu

BINARY=${1:?path to the darwin binary is required}
OUT=${2:?output app path is required}
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
ICON_DIR="$OUT/Contents/Resources/boxdeck-bar.iconset"
ICON_SOURCE="$OUT/Contents/Resources/boxdeck-bar.png"
rm -rf "$OUT"
mkdir -p "$OUT/Contents/MacOS" "$ICON_DIR"
cp "$BINARY" "$OUT/Contents/MacOS/boxdeck-bar"
cp "$ROOT/packaging/macos/Info.plist" "$OUT/Contents/Info.plist"

"$BINARY" --icon "$ICON_SOURCE"
for size in 16 32 128 256 512; do
	if command -v sips >/dev/null 2>&1; then
		sips -z "$size" "$size" "$ICON_SOURCE" --out "$ICON_DIR/icon_${size}x${size}.png" >/dev/null
		sips -z "$((size * 2))" "$((size * 2))" "$ICON_SOURCE" --out "$ICON_DIR/icon_${size}x${size}@2x.png" >/dev/null
	fi
done
if command -v iconutil >/dev/null 2>&1; then
	iconutil -c icns "$ICON_DIR" -o "$OUT/Contents/Resources/boxdeck-bar.icns"
fi
rm -rf "$ICON_DIR"
rm -f "$ICON_SOURCE"
codesign -s - "$OUT"
