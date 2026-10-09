#!/bin/sh
set -eu
cd "$(dirname "$0")"
icon_build=$(mktemp -d "${TMPDIR:?Set TMPDIR}/loafer-icon-build.XXXXXX")
xcrun actool Loafer.icon --compile "$icon_build" --app-icon Loafer \
  --platform macosx --target-device mac --minimum-deployment-target 26.0 \
  --output-partial-info-plist "$icon_build/partial-info.plist" \
  --output-format human-readable-text
test -s "$icon_build/Assets.car"
test "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIconName' "$icon_build/partial-info.plist")" = Loafer
cp "$icon_build/Assets.car" "$icon_build/partial-info.plist" .
mkdir "$icon_build/AppIcon.iconset"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" icon-1024.png --out "$icon_build/AppIcon.iconset/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z "$double" "$double" icon-1024.png --out "$icon_build/AppIcon.iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$icon_build/AppIcon.iconset" -o AppIcon.icns
printf 'Build intermediates: %s\n' "$icon_build"
