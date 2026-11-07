// gitmon — a beautiful, themeable git TUI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/gitmon/internal/git"
	"github.com/karim-w/gitmon/internal/theme"
	"github.com/karim-w/gitmon/internal/ui"
)

var version = "dev"

func main() {
	themeName := flag.String("theme", "", "theme to use (overrides config)")
	list := flag.Bool("themes", false, "list available themes")
	dump := flag.String("dump-theme", "", "print a theme as JSON (a starting point for your own)")
	ver := flag.Bool("version", false, "print version")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "gitmon %s — a beautiful git TUI\n\nusage: gitmon [flags] [path]\n\n", version)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\ncustom themes: %s/themes/*.json\n", theme.ConfigDir())
	}
	flag.Parse()

	switch {
	case *ver:
		fmt.Println("gitmon", version)
		return
	case *list:
		cfg := theme.LoadConfig()
		for _, t := range theme.All() {
			mark := "  "
			if t.Name == cfg.Theme {
				mark = "* "
			}
			fmt.Println(mark + t.Name)
		}
		return
	case *dump != "":
		t, ok := theme.Find(theme.All(), *dump)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown theme %q\n", *dump)
			os.Exit(1)
		}
		b, _ := json.MarshalIndent(t, "", "  ")
		fmt.Println(string(b))
		return
	}

	path := "."
	if flag.NArg() > 0 {
		path = flag.Arg(0)
	}
	repo, err := git.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitmon:", err)
		os.Exit(1)
	}
	if _, err := tea.NewProgram(ui.New(repo, *themeName)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gitmon:", err)
		os.Exit(1)
	}
}
