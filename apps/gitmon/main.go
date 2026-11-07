// gitmon — a beautiful, themeable git TUI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/gitmon/internal/config"
	"github.com/karim-w/slop/apps/gitmon/internal/git"
	"github.com/karim-w/slop/apps/gitmon/internal/ui"
	"github.com/karim-w/slop/pkg/domain/buildinfo"
	"github.com/karim-w/slop/pkg/domain/theme"
	"github.com/karim-w/slop/pkg/infra/logx"
)

func main() {
	themeName := flag.String("theme", "", "theme to use (overrides config)")
	list := flag.Bool("themes", false, "list available themes")
	dump := flag.String("dump-theme", "", "print a theme as JSON (a starting point for your own)")
	ver := flag.Bool("version", false, "print version")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "gitmon %s — a beautiful git TUI\n\nusage: gitmon [flags] [path]\n\n", buildinfo.Get())
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\ncustom themes: %s/*.json\n", config.ThemesDir())
		fmt.Fprintf(os.Stderr, "set GITMON_LOG=debug to log to %s\n", logx.Path(config.App))
	}
	flag.Parse()

	switch {
	case *ver:
		fmt.Println("gitmon", buildinfo.Get())
		return
	case *list:
		cfg := config.Load()
		for _, t := range theme.All(config.ThemesDir()) {
			mark := "  "
			if t.Name == cfg.Theme {
				mark = "* "
			}
			fmt.Println(mark + t.Name)
		}
		return
	case *dump != "":
		t, ok := theme.Find(theme.All(config.ThemesDir()), *dump)
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
	log, closeLog := logx.New(config.App)
	repo, err := git.Open(path)
	if err != nil {
		closeLog()
		fmt.Fprintln(os.Stderr, "gitmon:", err)
		os.Exit(1)
	}
	log.Info("starting", "version", buildinfo.Get().String(), "repo", repo.Name)
	_, err = tea.NewProgram(ui.New(repo, *themeName)).Run()
	if err != nil {
		log.Error("exited", "err", err)
	}
	closeLog()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitmon:", err)
		os.Exit(1)
	}
}
