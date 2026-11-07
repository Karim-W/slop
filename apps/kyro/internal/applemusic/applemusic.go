// Package applemusic mirrors loves and star ratings into the macOS Music
// app (formerly iTunes), so they follow your library to iPhone and iPod.
//
// It drives Music with AppleScript through osascript. Music calls "loving" a
// track "favorited" since macOS 14 (the property was "loved" before); the
// rating is 0–100, 20 per star. The first time, macOS asks whether your
// terminal may control Music.
package applemusic

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrUnsupported means this isn't macOS.
	ErrUnsupported = errors.New("Apple Music sync needs macOS")
	// ErrNotFound means Music has no track for that file.
	ErrNotFound = errors.New("not in your Music library")
	// ErrNotRunning means Music is closed (and we won't open it uninvited).
	ErrNotRunning = errors.New("Music isn't running")
)

// Available reports whether syncing can work on this machine.
func Available() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := exec.LookPath("osascript")
	return err == nil
}

// Running reports whether Music is open. Asking never launches it.
func Running() bool {
	if !Available() {
		return false
	}
	return exec.Command("pgrep", "-xq", "Music").Run() == nil
}

func osascript(ctx context.Context, script string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "osascript", append([]string{"-e", script}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", errors.New(strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// The track is found by searching its title and matching the file path;
// Music's own search is much faster than scanning the whole library.
const pushScript = `on run argv
	set p to item 1 of argv
	set stars to (item 3 of argv) as integer
	set lov to (item 4 of argv) is "true"
	tell application "Music"
		set ts to (search library playlist 1 for (item 2 of argv) only songs)
		repeat with t in ts
			try
				if POSIX path of (location of t as alias) is p then
					set rating of t to stars * 20
					set %s of t to lov
					return "ok"
				end if
			end try
		end repeat
	end tell
	return "notfound"
end run`

// Push sets a track's stars (0–5) and love in Music. With launch false it
// returns ErrNotRunning instead of opening Music.
func Push(path, title string, stars int, loved, launch bool) error {
	if !Available() {
		return ErrUnsupported
	}
	if !launch && !Running() {
		return ErrNotRunning
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	args := []string{path, title, strconv.Itoa(stars), strconv.FormatBool(loved)}
	out, err := osascript(ctx, strings.Replace(pushScript, "%s", "favorited", 1), args...)
	if err != nil && strings.Contains(err.Error(), "favorited") {
		// Music before macOS 14 calls it "loved".
		out, err = osascript(ctx, strings.Replace(pushScript, "%s", "loved", 1), args...)
	}
	switch {
	case err != nil:
		return err
	case out == "notfound":
		return ErrNotFound
	}
	return nil
}

// Rated is a track Music has a rating or love for.
type Rated struct {
	Path  string
	Stars int
	Loved bool
}

const pullScript = `tell application "Music"
	set out to ""
	repeat with t in (every file track of library playlist 1 whose rating > 0 or %s is true)
		try
			set out to out & (POSIX path of (location of t as alias)) & tab & (rating of t) & tab & (%s of t) & linefeed
		end try
	end repeat
	return out
end tell`

// Pull reads every rated or loved file track from Music.
func Pull(launch bool) ([]Rated, error) {
	if !Available() {
		return nil, ErrUnsupported
	}
	if !launch && !Running() {
		return nil, ErrNotRunning
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, err := osascript(ctx, strings.ReplaceAll(pullScript, "%s", "favorited"))
	if err != nil && strings.Contains(err.Error(), "favorited") {
		out, err = osascript(ctx, strings.ReplaceAll(pullScript, "%s", "loved"))
	}
	if err != nil {
		return nil, err
	}
	return parsePull(out), nil
}

func parsePull(out string) []Rated {
	var res []Rated
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) != 3 {
			continue
		}
		r, _ := strconv.Atoi(strings.TrimSpace(f[1]))
		res = append(res, Rated{Path: f[0], Stars: (r + 10) / 20, Loved: strings.TrimSpace(f[2]) == "true"})
	}
	return res
}
