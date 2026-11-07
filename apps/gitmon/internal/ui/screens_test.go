package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/karim-w/slop/apps/gitmon/internal/git"
)

// fixtureRepo builds a small repo with a merged branch, a tag, an unmerged
// branch, a stash and a mix of staged, unstaged and untracked changes.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	date := "2024-03-01T10:00:00Z"
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date,
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string) { run("add", "-A"); run("commit", "-q", "-m", msg) }

	run("init", "-q", "-b", "main")
	run("config", "user.name", "Ada Lovelace")
	run("config", "user.email", "ada@example.com")
	write("parser.go", "package parser\n\nfunc Parse() {}\n")
	write("README.md", "# demo\n")
	commit("feat: initial parser")
	run("checkout", "-q", "-b", "feature/graph")
	write("graph.go", "package graph\n")
	commit("feat(graph): lanes")
	run("checkout", "-q", "main")
	write("parser.go", "package parser\n\nfunc Parse() error { return nil }\n")
	commit("fix: parser returns error")
	run("merge", "-q", "--no-ff", "feature/graph", "-m", "Merge branch 'feature/graph'")
	run("tag", "v1.0.0")
	run("checkout", "-q", "-b", "hotfix")
	write("notes.md", "notes\n")
	commit("hotfix: notes")
	run("checkout", "-q", "main")
	write("README.md", "# demo\nstash me\n")
	run("stash", "-q")
	write("parser.go", "package parser\n\nfunc Parse() error {\n\treturn nil\n}\n")
	run("add", "parser.go")
	write("internal/ui/view.go", "package ui\n")
	run("add", "internal/ui/view.go")
	write("internal/ui/model.go", "package ui\n")
	write("todo.txt", "untracked\n")
	return dir
}

// TestScreens walks the main screens and dialogs on a fixture repo and
// checks each frame fills the terminal exactly and shows what it should.
func TestScreens(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cases := []struct {
		name string
		keys []string
		want []string
	}{
		{"start", nil, []string{"◆ gitmon", "Working tree", "✓2/4 staged", "History", "◇ 1 stashed", "◐ nebula"}},
		{"diff", []string{"tab"}, []string{"● internal/ui/", "partly staged"}},
		{"graph", []string{"tab", "tab"}, []string{"● History", "Uncommitted changes", "◉ main"}},
		{"inspect", []string{"3", "left", "enter"}, []string{"Commit ", "FILES", "y copy sha"}},
		{"branches", []string{"B"}, []string{"Branches", "feature/graph", "hotfix", "⏎ checkout"}},
		{"stashes", []string{"Z"}, []string{"Stashes", "⏎/p pop"}},
		{"palette", []string{"ctrl+p"}, []string{"Commands", "results"}},
		{"themes", []string{"T"}, []string{"Themes", "tokyo-night", "preview live"}},
		{"commit box", []string{"c"}, []string{"Commit · 2 files staged", "newline"}},
		{"flat", []string{"v"}, []string{"model.go internal/ui"}},
		{"focus mode", []string{"3", "F"}, []string{"FOCUS"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, err := git.Open(fixtureRepo(t))
			if err != nil {
				t.Fatal(err)
			}
			m := New(repo, "nebula")
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 34})
			drive(m, m.Init(), 0)
			for _, k := range tc.keys {
				key(m, k)
			}
			settle(m)
			out := ansi.Strip(m.View().Content)
			lines := strings.Split(out, "\n")
			if len(lines) != 34 {
				t.Fatalf("%d lines, want 34", len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != 120 {
					t.Fatalf("line %d is %d cells, want 120\n%s", i, w, out)
				}
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Fatalf("missing %q\n%s", w, out)
				}
			}
		})
	}
}
