#!/bin/sh
# Scaffold a new app under apps/<name>, wired to the shared packages and the
# Go workspace, and register it with the installer.
#
#   ./scripts/new-app.sh weather "Weather in your terminal"
set -eu

name=${1:?usage: new-app.sh <name> [description]}
desc=${2:-"A slop terminal app"}
root=$(cd "$(dirname "$0")/.." && pwd)
dir="$root/apps/$name"

case $name in
  *[!a-z0-9-]* | -*) echo "app names are lowercase letters, digits and dashes" >&2; exit 1 ;;
esac
[ -e "$dir" ] && { echo "apps/$name already exists" >&2; exit 1; }

mkdir -p "$dir"
cat > "$dir/go.mod" <<EOF
module github.com/karim-w/slop/apps/$name

go 1.26.0

require (
	github.com/karim-w/slop/pkg/domain v0.0.0
	github.com/karim-w/slop/pkg/infra v0.0.0
	github.com/karim-w/slop/pkg/tui v0.0.0
	github.com/karim-w/slop/pkg/utils v0.0.0
)

replace (
	github.com/karim-w/slop/pkg/domain => ../../pkg/domain
	github.com/karim-w/slop/pkg/infra => ../../pkg/infra
	github.com/karim-w/slop/pkg/tui => ../../pkg/tui
	github.com/karim-w/slop/pkg/utils => ../../pkg/utils
)
EOF

cat > "$dir/main.go" <<EOF
// $name: $desc.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/pkg/domain/buildinfo"
	"github.com/karim-w/slop/pkg/domain/theme"
	"github.com/karim-w/slop/pkg/infra/logx"
	"github.com/karim-w/slop/pkg/tui"
)

func main() {
	themeName := flag.String("theme", "", "theme to use")
	ver := flag.Bool("version", false, "print version")
	flag.Parse()
	if *ver {
		fmt.Println("$name", buildinfo.Get())
		return
	}

	log, closeLog := logx.New("$name")
	defer closeLog()
	log.Info("starting", "version", buildinfo.Get().String())

	t, _ := theme.Find(theme.All(""), *themeName)
	if _, err := tea.NewProgram(&model{pal: tui.NewPalette(t)}).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "$name:", err)
		os.Exit(1)
	}
}

type model struct {
	pal    *tui.Palette
	w, h   int
	cursor int
	scroll tui.Scroll
}

var items = []string{"Edit apps/$name/main.go", "Build views from pkg/tui", "Run make run APP=$name"}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.cursor = max(m.cursor-1, 0)
		case "down", "j":
			m.cursor = min(m.cursor+1, len(items)-1)
		}
	}
	return m, nil
}

func (m *model) View() tea.View {
	root := tui.VStack(
		tui.HStack(tui.Gradient(" $name ").Bold(), tui.Spacer(), tui.Text(buildinfo.Get().Version+" ").Fg(tui.Muted)),
		tui.Panel(
			tui.List(len(items), m.cursor, &m.scroll, func(i int, sel bool) tui.View {
				if sel {
					return tui.Text(" › " + items[i]).Fg(tui.Accent).Bold()
				}
				return tui.Text("   " + items[i])
			}).Highlight(tui.Selection),
		).Title("Getting started").Focused(true).Grow(),
		tui.Pad(tui.KeyHints(tui.H("↑↓", "move"), tui.H("q", "quit")), 0, 1),
	)
	v := tea.NewView(tui.Render(m.pal, root, m.w, m.h))
	v.AltScreen = true
	v.BackgroundColor = m.pal.Bg
	return v
}
EOF

cat > "$dir/README.md" <<EOF
# $name

$desc.

\`\`\`
make run APP=$name
\`\`\`
EOF

cd "$root"
go work use "./apps/$name"
(cd "$dir" && GOWORK=off go mod tidy >/dev/null)

# Register with the installer: add "name|desc" to the APPS list.
awk -v line="$name|$desc" '
  /^APPS="$/ { print; inlist = 1; next }
  inlist && /^"$/ { print line; inlist = 0 }
  { print }
' install.sh > install.sh.tmp && mv install.sh.tmp install.sh && chmod +x install.sh

echo "created apps/$name — try: make run APP=$name"
