#!/bin/sh
# slop installer.
#
#   curl --proto '=https' --tlsv1.2 -sSf https://raw.githubusercontent.com/karim-w/slop/main/install.sh | sh
#   curl --proto '=https' --tlsv1.2 -sSf https://raw.githubusercontent.com/karim-w/slop/main/install.sh | sh -s -- gitmon
#   curl ... | sh -s -- --all --dir /usr/local/bin
#
# With no app named, it asks which ones to install. It downloads a prebuilt
# binary from the latest GitHub release and checks its SHA-256. If there is
# no release for this platform (or with --source) it builds from source with Go.
set -eu

REPO="karim-w/slop"

# One "name|description" per line. scripts/new-app.sh adds to this list.
APPS="
gitmon|A themeable git TUI with a horizontal commit graph
slash|A two-pane SFTP client with parallel transfers
kyro|A local music player with playlists, hooks and cover art
"

VERSION="latest"
DIR="${SLOP_INSTALL_DIR:-$HOME/.local/bin}"
FROM_SOURCE=0
REF="main"
SELECTED=""

# ─── output ──────────────────────────────────────────────────────────────────

if [ -t 2 ] && [ -z "${NO_COLOR:-}" ]; then
	B=$(printf '\033[1m'); D=$(printf '\033[2m'); A=$(printf '\033[38;2;167;139;250m')
	G=$(printf '\033[38;2;74;222;128m'); R=$(printf '\033[38;2;251;113;133m'); N=$(printf '\033[0m')
else
	B=""; D=""; A=""; G=""; R=""; N=""
fi
say() { printf '%s\n' "$*" >&2; }
step() { say "${A}›${N} $*"; }
ok() { say "${G}✓${N} $*"; }
die() { say "${R}✗ $*${N}"; exit 1; }

usage() {
	cat >&2 <<EOF
${B}slop installer${N}

usage: install.sh [app ...] [options]

apps:
$(app_list | while IFS='|' read -r n d; do printf '  %-10s %s\n' "$n" "$d"; done)

options:
  -a, --all            install every app
  -d, --dir DIR        install into DIR (default: \$SLOP_INSTALL_DIR or ~/.local/bin)
  -v, --version TAG    install release TAG instead of the latest
  -s, --source         build from source with Go instead of downloading
      --ref REF        git ref to build from with --source (default: main)
  -h, --help           show this help
EOF
}

app_list() { printf '%s\n' "$APPS" | sed '/^$/d'; }
app_names() { app_list | cut -d'|' -f1; }
is_app() { app_names | grep -qx "$1"; }

# ─── arguments ───────────────────────────────────────────────────────────────

while [ $# -gt 0 ]; do
	case $1 in
	-a | --all) SELECTED=$(app_names | tr '\n' ' ') ;;
	-d | --dir) DIR=${2:?--dir needs a value}; shift ;;
	-v | --version) VERSION=${2:?--version needs a value}; shift ;;
	-s | --source) FROM_SOURCE=1 ;;
	--ref) REF=${2:?--ref needs a value}; shift ;;
	-h | --help) usage; exit 0 ;;
	-*) usage; die "unknown option $1" ;;
	*) is_app "$1" || die "unknown app '$1' (choose from: $(app_names | tr '\n' ' '))"
	   SELECTED="$SELECTED $1" ;;
	esac
	shift
done

# ─── choose apps ─────────────────────────────────────────────────────────────

prompt() {
	# stdin is the curl pipe, so talk to the terminal directly.
	[ -r /dev/tty ] || die "no app given and no terminal to ask on; pass one, e.g. 'sh -s -- gitmon'"
	say ""
	say "${B}slop${N} ${D}— which apps should I install?${N}"
	say ""
	i=0
	app_list | while IFS='|' read -r n d; do
		i=$((i + 1))
		say "  ${A}${i})${N} ${B}$(printf '%-8s' "$n")${N} ${D}$d${N}"
	done
	say "  ${A}a)${N} ${B}all${N}"
	say ""
	printf '%s' "Choose (numbers or names, space-separated) [a]: " >&2
	read -r answer </dev/tty || answer=""
	[ -n "$answer" ] || answer="a"
	for pick in $answer; do
		case $pick in
		a | all) SELECTED=$(app_names | tr '\n' ' '); return ;;
		*[!0-9]*) is_app "$pick" || die "unknown app '$pick'"; SELECTED="$SELECTED $pick" ;;
		*) name=$(app_names | sed -n "${pick}p")
		   [ -n "$name" ] || die "no app number $pick"
		   SELECTED="$SELECTED $name" ;;
		esac
	done
}

[ -n "$(printf '%s' "$SELECTED" | tr -d ' ')" ] || prompt

# ─── platform & tools ────────────────────────────────────────────────────────

case $(uname -s) in
Darwin) OS=darwin ;;
Linux) OS=linux ;;
*) die "unsupported OS $(uname -s); build from source: https://github.com/$REPO" ;;
esac
case $(uname -m) in
x86_64 | amd64) ARCH=amd64 ;;
arm64 | aarch64) ARCH=arm64 ;;
*) die "unsupported CPU $(uname -m)" ;;
esac

if command -v curl >/dev/null; then
	fetch() { curl --proto '=https' --tlsv1.2 -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null; then
	fetch() { wget --https-only -qO "$2" "$1"; }
else
	die "need curl or wget"
fi

sha256() {
	if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1
	else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

if [ "$VERSION" = latest ]; then
	BASE="https://github.com/$REPO/releases/latest/download"
else
	BASE="https://github.com/$REPO/releases/download/$VERSION"
fi

# ─── install ─────────────────────────────────────────────────────────────────

install_binary() { # app
	asset="$1_${OS}_${ARCH}"
	fetch "$BASE/$asset.tar.gz" "$TMP/$asset.tar.gz" 2>/dev/null || return 1
	if fetch "$BASE/checksums.txt" "$TMP/checksums.txt" 2>/dev/null; then
		want=$(grep " $asset.tar.gz\$" "$TMP/checksums.txt" | cut -d' ' -f1)
		[ -n "$want" ] || die "$asset.tar.gz is missing from checksums.txt"
		[ "$want" = "$(sha256 "$TMP/$asset.tar.gz")" ] || die "checksum mismatch for $asset.tar.gz"
	else
		say "${D}  (no checksums.txt in this release; skipping verification)${N}"
	fi
	tar -xzf "$TMP/$asset.tar.gz" -C "$TMP"
	mkdir -p "$DIR"
	install -m 0755 "$TMP/$asset/$1" "$DIR/$1"
}

SRC=""
fetch_source() {
	[ -n "$SRC" ] && return 0
	command -v go >/dev/null || die "no prebuilt binary for $OS/$ARCH and Go is not installed (https://go.dev/dl)"
	step "downloading source ($REF)"
	fetch "https://codeload.github.com/$REPO/tar.gz/$REF" "$TMP/src.tar.gz" 2>/dev/null || die "could not download source at $REF"
	mkdir -p "$TMP/src"
	tar -xzf "$TMP/src.tar.gz" -C "$TMP/src" --strip-components 1
	SRC="$TMP/src"
}

install_source() { # app
	fetch_source
	mkdir -p "$DIR"
	(cd "$SRC/apps/$1" && go build -trimpath -ldflags "-s -w -X github.com/karim-w/slop/pkg/domain/buildinfo.Version=$REF" -o "$DIR/$1" .) ||
		die "building $1 failed"
}

for app in $SELECTED; do
	if [ "$FROM_SOURCE" = 1 ]; then
		step "building ${B}$app${N} from source"
		install_source "$app"
	else
		step "installing ${B}$app${N} ($VERSION, $OS/$ARCH)"
		if ! install_binary "$app"; then
			say "${D}  no release asset found; falling back to a source build${N}"
			install_source "$app"
		fi
	fi
	ok "$app → $DIR/$app"
	if [ "$app" = kyro ] && ! command -v mpv >/dev/null; then
		say "${D}  kyro plays audio through mpv, which isn't installed yet:${N}"
		case $OS in
		darwin) say "    brew install mpv" ;;
		*) say "    sudo apt install mpv   # or your distro's package manager" ;;
		esac
	fi
done

case ":$PATH:" in
*":$DIR:"*) ;;
*) say ""
   say "${B}$DIR is not on your PATH.${N} Add it to your shell profile:"
   say "  export PATH=\"$DIR:\$PATH\"" ;;
esac
