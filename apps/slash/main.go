// slash is a two-pane SFTP client for the terminal.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/slash/internal/config"
	"github.com/karim-w/slop/apps/slash/internal/ui"
	"github.com/karim-w/slop/pkg/domain/buildinfo"
	"github.com/karim-w/slop/pkg/domain/theme"
	"github.com/karim-w/slop/pkg/infra/logx"
)

func main() {
	themeName := flag.String("theme", os.Getenv("SLASH_THEME"), `theme to use, or "terminal" to follow the terminal's colours`)
	list := flag.Bool("themes", false, "list available themes")
	ver := flag.Bool("version", false, "print version")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "slash %s — a two-pane SFTP client\n\nusage: slash [flags]\n\n", buildinfo.Get())
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nset SLASH_LOG=debug to log to %s\n", logx.Path("slash"))
	}
	flag.Parse()

	switch {
	case *ver:
		fmt.Println("slash", buildinfo.Get())
		return
	case *list:
		fmt.Println("terminal")
		for _, t := range theme.All("") {
			fmt.Println(t.Name)
		}
		return
	}

	if err := run(*themeName); err != nil {
		fmt.Fprintln(os.Stderr, "slash:", err)
		os.Exit(1)
	}
}

func run(themeName string) error {
	log, closeLog := logx.New("slash")
	defer closeLog()

	path, err := config.DefaultPath()
	if err != nil {
		return err
	}
	store, err := config.Load(path)
	if err != nil {
		return err
	}
	log.Info("starting", "version", buildinfo.Get().String(), "sites", len(store.Sites))
	_, err = tea.NewProgram(ui.New(store, themeName)).Run()
	if err != nil {
		log.Error("exited", "err", err)
	}
	return err
}
