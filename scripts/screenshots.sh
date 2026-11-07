#!/bin/sh
# Regenerate docs/screenshots/*.svg. Each app's render test writes one
# captured frame; tools/ansisvg turns it into an SVG with window chrome.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
out="$root/docs/screenshots"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$out"

(cd "$root/apps/gitmon" && GITMON_DEMO="$root" GITMON_THEME=nebula SIZE=120x32 SCREENSHOT="$tmp/gitmon.ansi" \
	go test ./internal/ui -run TestSnapshot -count=1 >/dev/null)
(cd "$root/apps/slash" && SCREENSHOT_DIR="$tmp" go test ./internal/ui -run TestConnectBrowseAndDialogs -count=1 >/dev/null)

for app in gitmon slash; do
	(cd "$root/tools/ansisvg" && go run . -title "$app" -bg '#0d0e1a' -fg '#e6e6fa') <"$tmp/$app.ansi" >"$out/$app.svg"
	echo "wrote docs/screenshots/$app.svg"
done
