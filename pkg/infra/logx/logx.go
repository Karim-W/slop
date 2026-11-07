// Package logx gives TUI apps a structured logger that never writes to the
// terminal. A full-screen app owns stdout and stderr, so logs go to a file
// under the app's state folder, and only when the user asks for them.
//
// Logging turns on when <APP>_LOG (for example GITMON_LOG) or SLOP_LOG is
// set to a level: debug, info, warn or error. Otherwise New returns a logger
// that discards everything at almost no cost.
//
//	log, closeLog := logx.New("gitmon")
//	defer closeLog()
//	log.Info("opened repo", "path", path)
package logx

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/karim-w/slop/pkg/infra/paths"
)

// New returns app's logger and a function that flushes and closes it.
func New(app string) (*slog.Logger, func()) {
	level, ok := levelFromEnv(app)
	if !ok {
		return Discard(), func() {}
	}
	path := Path(app)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Discard(), func() {}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Discard(), func() {}
	}
	log := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})).With("app", app, "pid", os.Getpid())
	return log, func() { _ = f.Close() }
}

// Path is where app's log file lives: <state dir>/<app>.log.
func Path(app string) string { return filepath.Join(paths.State(app), app+".log") }

// Discard returns a logger that drops every record.
func Discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func levelFromEnv(app string) (slog.Level, bool) {
	v := os.Getenv(strings.ToUpper(app) + "_LOG")
	if v == "" {
		v = os.Getenv("SLOP_LOG")
	}
	if v == "" {
		return 0, false
	}
	var l slog.Level
	if l.UnmarshalText([]byte(v)) != nil {
		l = slog.LevelDebug // "1", "true", … mean "everything"
	}
	return l, true
}
