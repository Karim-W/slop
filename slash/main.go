package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/karim-w/slop/slash/internal/config"
	"github.com/karim-w/slop/slash/internal/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "slash:", err)
		os.Exit(1)
	}
}

func run() error {
	path, err := config.DefaultPath()
	if err != nil {
		return err
	}
	store, err := config.Load(path)
	if err != nil {
		return err
	}
	program := tea.NewProgram(ui.New(store), tea.WithAltScreen())
	_, err = program.Run()
	return err
}
