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

# kyro needs music with cover art; generate a tiny library with ffmpeg.
if command -v ffmpeg >/dev/null; then
	music="$tmp/music/Nebula Drift/Afterglow"
	mkdir -p "$music"
	ffmpeg -loglevel error -y -f lavfi -i "gradients=s=600x600:c0=0x2b1650:c1=0xff2a6d:c2=0x05d9e8:c3=0xfede5d:n=4:type=spiral:seed=7" -frames:v 1 "$tmp/cover.png"
	for t in "1:Solar Wind" "2:Event Horizon" "3:Afterimage"; do
		n=${t%%:*}; title=${t#*:}
		ffmpeg -loglevel error -y -f lavfi -i "sine=frequency=330:duration=2" -i "$tmp/cover.png" -map 0 -map 1 \
			-disposition:v attached_pic -ar 96000 -sample_fmt s32 -metadata title="$title" -metadata artist="Nebula Drift" \
			-metadata album="Afterglow" -metadata track="$n" -metadata date=2023 "$music/0$n $title.flac"
	done
	(cd "$root/apps/kyro" && KYRO_FIXTURES="$tmp/music" KYRO_SHOTS="$tmp" go test ./internal/ui -run TestScreenshots -count=1 >/dev/null)
	for v in full pane zen zen-dynamic; do
		bg='#0d0e1a' fg='#e6e6fa'
		if [ -f "$tmp/TestScreenshots_$v.colors" ]; then # the dynamic theme sets its own
			read -r bg fg <"$tmp/TestScreenshots_$v.colors"
		fi
		(cd "$root/tools/ansisvg" && go run . -title kyro -bg "$bg" -fg "$fg") <"$tmp/TestScreenshots_$v.ansi" >"$out/kyro-$v.svg"
		echo "wrote docs/screenshots/kyro-$v.svg"
	done
else
	echo "ffmpeg not found: skipping kyro screenshots"
fi

for app in gitmon slash; do
	(cd "$root/tools/ansisvg" && go run . -title "$app" -bg '#0d0e1a' -fg '#e6e6fa') <"$tmp/$app.ansi" >"$out/$app.svg"
	echo "wrote docs/screenshots/$app.svg"
done
