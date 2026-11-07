// Package git is a thin, dependency-free wrapper around the git CLI.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Repo is a handle on a git working tree.
type Repo struct {
	Root string
	Name string
}

// Open locates the repository containing path.
func Open(path string) (*Repo, error) {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %s", path)
	}
	root := strings.TrimSpace(string(out))
	return &Repo{Root: root, Name: filepath.Base(root)}, nil
}

func (r *Repo) cmd(args ...string) *exec.Cmd {
	c := exec.Command("git", append([]string{"-C", r.Root, "-c", "color.ui=never", "-c", "core.quotepath=off"}, args...)...)
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	return c
}

// Run executes git and returns stdout. Errors carry stderr.
func (r *Repo) Run(args ...string) (string, error) {
	return r.RunInput("", args...)
}

// RunInput executes git with stdin.
func (r *Repo) RunInput(stdin string, args ...string) (string, error) {
	c := r.cmd(args...)
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

// Interactive returns an *exec.Cmd suitable for handing the terminal over.
func (r *Repo) Interactive(args ...string) *exec.Cmd {
	c := exec.Command("git", append([]string{"-C", r.Root}, args...)...)
	c.Env = os.Environ()
	return c
}

// ─── Commits ────────────────────────────────────────────────────────────────

type RefKind int

const (
	RefHead RefKind = iota
	RefLocal
	RefRemote
	RefTag
)

type Ref struct {
	Name string
	Kind RefKind
}

type Commit struct {
	Hash    string
	Short   string
	Parents []string
	Author  string
	Email   string
	Time    time.Time
	Subject string
	Refs    []Ref
	IsHead  bool
	WIP     bool // synthetic "uncommitted changes" row
}

const (
	us = "\x1f"
	rs = "\x1e"
)

// Log returns up to limit commits from all branches, remotes and tags in
// topological order.
func (r *Repo) Log(limit int) ([]Commit, error) {
	out, err := r.Run("log", "--branches", "--remotes", "--tags", "HEAD",
		"--topo-order", "--decorate=full", fmt.Sprintf("-n%d", limit),
		"--format=%H"+us+"%P"+us+"%an"+us+"%ae"+us+"%at"+us+"%s"+us+"%D"+rs)
	if err != nil {
		if strings.Contains(err.Error(), "does not have any commits") || strings.Contains(err.Error(), "unknown revision") {
			return nil, nil
		}
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(out, rs) {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, us)
		if len(f) < 7 {
			continue
		}
		ts, _ := strconv.ParseInt(f[4], 10, 64)
		c := Commit{
			Hash:    f[0],
			Short:   short(f[0]),
			Author:  f[2],
			Email:   f[3],
			Time:    time.Unix(ts, 0),
			Subject: f[5],
		}
		if f[1] != "" {
			c.Parents = strings.Fields(f[1])
		}
		c.Refs, c.IsHead = parseDecorations(f[6])
		commits = append(commits, c)
	}
	return commits, nil
}

func short(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

func parseDecorations(d string) ([]Ref, bool) {
	if strings.TrimSpace(d) == "" {
		return nil, false
	}
	var refs []Ref
	head := false
	for _, part := range strings.Split(d, ", ") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "HEAD -> "):
			head = true
			refs = append(refs, Ref{Name: strings.TrimPrefix(strings.TrimPrefix(part, "HEAD -> "), "refs/heads/"), Kind: RefHead})
		case part == "HEAD":
			head = true
			refs = append(refs, Ref{Name: "HEAD", Kind: RefHead})
		case strings.HasPrefix(part, "tag: "):
			refs = append(refs, Ref{Name: strings.TrimPrefix(strings.TrimPrefix(part, "tag: "), "refs/tags/"), Kind: RefTag})
		case strings.HasPrefix(part, "refs/heads/"):
			refs = append(refs, Ref{Name: strings.TrimPrefix(part, "refs/heads/"), Kind: RefLocal})
		case strings.HasPrefix(part, "refs/remotes/"):
			name := strings.TrimPrefix(part, "refs/remotes/")
			if strings.HasSuffix(name, "/HEAD") {
				continue
			}
			refs = append(refs, Ref{Name: name, Kind: RefRemote})
		case strings.HasPrefix(part, "refs/stash"):
		default:
			refs = append(refs, Ref{Name: part, Kind: RefLocal})
		}
	}
	return refs, head
}

// ─── Commit detail ──────────────────────────────────────────────────────────

type FileChange struct {
	Path    string
	OldPath string
	Status  byte // A M D R C T
	Added   int
	Deleted int
	Binary  bool
}

type CommitDetail struct {
	Hash      string
	Author    string
	Email     string
	Time      time.Time
	Committer string
	CTime     time.Time
	Subject   string
	Body      string
	Parents   []string
	Files     []FileChange
}

func (r *Repo) Detail(hash string) (*CommitDetail, error) {
	out, err := r.Run("log", "-1", "--format=%H"+us+"%an"+us+"%ae"+us+"%at"+us+"%cn"+us+"%ct"+us+"%P"+us+"%B", hash)
	if err != nil {
		return nil, err
	}
	f := strings.SplitN(out, us, 8)
	if len(f) < 8 {
		return nil, fmt.Errorf("unexpected log output")
	}
	at, _ := strconv.ParseInt(f[3], 10, 64)
	ct, _ := strconv.ParseInt(f[5], 10, 64)
	d := &CommitDetail{Hash: f[0], Author: f[1], Email: f[2], Time: time.Unix(at, 0), Committer: f[4], CTime: time.Unix(ct, 0), Parents: strings.Fields(f[6])}
	msg := strings.TrimRight(f[7], "\n ")
	subj, body, _ := strings.Cut(msg, "\n")
	d.Subject, d.Body = subj, strings.Trim(body, "\n")
	d.Files, err = r.changes(d.diffBase(), hash)
	return d, err
}

func (d *CommitDetail) diffBase() string {
	if len(d.Parents) == 0 {
		return emptyTree
	}
	return d.Parents[0]
}

const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

func (r *Repo) changes(from, to string) ([]FileChange, error) {
	args := []string{"diff", "-M", "--numstat", "-z", from}
	if to != "" {
		args = append(args, to)
	}
	num, err := r.Run(args...)
	if err != nil {
		return nil, err
	}
	args[2] = "--name-status"
	ns, err := r.Run(args...)
	if err != nil {
		return nil, err
	}
	// name-status -z: STATUS\0path\0 (or STATUS\0old\0new\0 for renames)
	statusOf := map[string]byte{}
	parts := strings.Split(ns, "\x00")
	for i := 0; i < len(parts); i++ {
		s := parts[i]
		if s == "" {
			continue
		}
		code := s[0]
		if code == 'R' || code == 'C' {
			if i+2 < len(parts) {
				statusOf[parts[i+2]] = code
			}
			i += 2
		} else {
			if i+1 < len(parts) {
				statusOf[parts[i+1]] = code
			}
			i++
		}
	}
	// numstat -z: "add\tdel\tpath\0" or "add\tdel\t\0old\0new\0"
	var files []FileChange
	parts = strings.Split(num, "\x00")
	for i := 0; i < len(parts); i++ {
		s := parts[i]
		if s == "" {
			continue
		}
		cols := strings.SplitN(s, "\t", 3)
		if len(cols) < 3 {
			continue
		}
		fc := FileChange{}
		if cols[0] == "-" {
			fc.Binary = true
		} else {
			fc.Added, _ = strconv.Atoi(cols[0])
			fc.Deleted, _ = strconv.Atoi(cols[1])
		}
		if cols[2] == "" && i+2 < len(parts) {
			fc.OldPath, fc.Path = parts[i+1], parts[i+2]
			i += 2
		} else {
			fc.Path = cols[2]
		}
		fc.Status = statusOf[fc.Path]
		if fc.Status == 0 {
			fc.Status = 'M'
		}
		files = append(files, fc)
	}
	return files, nil
}

// CommitFileDiff returns the patch for one file in a commit.
func (r *Repo) CommitFileDiff(d *CommitDetail, path string) (string, error) {
	return r.Run("diff", "-M", d.diffBase(), d.Hash, "--", path)
}

// ─── Status ─────────────────────────────────────────────────────────────────

type FileStatus struct {
	Path      string
	OrigPath  string
	Code      byte // status letter for this side: M A D R C U ?
	Staged    bool
	Untracked bool
	Conflict  bool
}

type Status struct {
	Head     string // commit oid
	Branch   string
	Upstream string
	Ahead    int
	Behind   int
	Detached bool
	Staged   []FileStatus
	Unstaged []FileStatus
	State    string // rebasing, merging, ...
}

func (s Status) Dirty() bool { return len(s.Staged)+len(s.Unstaged) > 0 }

func (r *Repo) Status() (Status, error) {
	out, err := r.Run("--no-optional-locks", "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	var st Status
	if err != nil {
		return st, err
	}
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		line := parts[i]
		if line == "" {
			continue
		}
		switch line[0] {
		case '#':
			f := strings.Fields(line)
			if len(f) < 3 {
				continue
			}
			switch f[1] {
			case "branch.oid":
				if f[2] != "(initial)" {
					st.Head = f[2]
				}
			case "branch.head":
				st.Branch = f[2]
				st.Detached = f[2] == "(detached)"
			case "branch.upstream":
				st.Upstream = f[2]
			case "branch.ab":
				if len(f) >= 4 {
					st.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
					st.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
				}
			}
		case '1', '2':
			n := 9
			if line[0] == '2' {
				n = 10
			}
			f := strings.SplitN(line, " ", n)
			if len(f) < n {
				continue
			}
			path := f[n-1]
			orig := ""
			if line[0] == '2' && i+1 < len(parts) {
				orig = parts[i+1]
				i++
			}
			xy := f[1]
			if xy[0] != '.' {
				st.Staged = append(st.Staged, FileStatus{Path: path, OrigPath: orig, Code: xy[0], Staged: true})
			}
			if xy[1] != '.' {
				st.Unstaged = append(st.Unstaged, FileStatus{Path: path, OrigPath: orig, Code: xy[1]})
			}
		case 'u':
			f := strings.SplitN(line, " ", 11)
			if len(f) == 11 {
				st.Unstaged = append(st.Unstaged, FileStatus{Path: f[10], Code: 'U', Conflict: true})
			}
		case '?':
			st.Unstaged = append(st.Unstaged, FileStatus{Path: line[2:], Code: '?', Untracked: true})
		}
	}
	st.State = r.opState()
	return st, nil
}

func (r *Repo) gitDir() string {
	out, err := r.Run("rev-parse", "--absolute-git-dir")
	if err != nil {
		return filepath.Join(r.Root, ".git")
	}
	return strings.TrimSpace(out)
}

func (r *Repo) opState() string {
	gd := r.gitDir()
	exists := func(p string) bool { _, err := os.Stat(filepath.Join(gd, p)); return err == nil }
	switch {
	case exists("rebase-merge"), exists("rebase-apply"):
		return "rebasing"
	case exists("MERGE_HEAD"):
		return "merging"
	case exists("CHERRY_PICK_HEAD"):
		return "cherry-picking"
	case exists("REVERT_HEAD"):
		return "reverting"
	case exists("BISECT_LOG"):
		return "bisecting"
	}
	return ""
}

// Fingerprint changes whenever any ref or HEAD moves.
func (r *Repo) Fingerprint() string {
	out, _ := r.Run("for-each-ref", "--format=%(objectname)%(refname)")
	head, _ := r.Run("rev-parse", "HEAD")
	sym, _ := r.Run("symbolic-ref", "-q", "HEAD")
	return out + head + sym
}

// WorkingDiff returns the diff for a single file in the index or worktree.
func (r *Repo) WorkingDiff(f FileStatus) (string, error) {
	switch {
	case f.Untracked:
		out, err := r.Run("diff", "--no-index", "--", "/dev/null", f.Path)
		if err != nil && out != "" {
			return out, nil // --no-index exits 1 when files differ
		}
		return out, err
	case f.Staged:
		return r.Run("diff", "--cached", "-M", "--", f.Path)
	default:
		return r.Run("diff", "--", f.Path)
	}
}

// ─── Branches, stashes ──────────────────────────────────────────────────────

type Branch struct {
	Name     string
	Hash     string
	Upstream string
	Ahead    int
	Behind   int
	Gone     bool
	Head     bool
	Remote   bool
	Subject  string
	Date     time.Time
}

func (r *Repo) Branches() ([]Branch, error) {
	out, err := r.Run("for-each-ref", "--sort=-committerdate",
		"--format=%(HEAD)"+us+"%(refname)"+us+"%(refname:short)"+us+"%(upstream:short)"+us+"%(upstream:track,nobracket)"+us+"%(objectname:short)"+us+"%(committerdate:unix)"+us+"%(contents:subject)",
		"refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var bs []Branch
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, us)
		if len(f) < 8 || strings.HasSuffix(f[1], "/HEAD") {
			continue
		}
		ts, _ := strconv.ParseInt(f[6], 10, 64)
		b := Branch{Head: f[0] == "*", Name: f[2], Upstream: f[3], Hash: f[5], Date: time.Unix(ts, 0), Subject: f[7], Remote: strings.HasPrefix(f[1], "refs/remotes/")}
		for _, t := range strings.Split(f[4], ", ") {
			switch {
			case t == "gone":
				b.Gone = true
			case strings.HasPrefix(t, "ahead "):
				b.Ahead, _ = strconv.Atoi(strings.TrimPrefix(t, "ahead "))
			case strings.HasPrefix(t, "behind "):
				b.Behind, _ = strconv.Atoi(strings.TrimPrefix(t, "behind "))
			}
		}
		if b.Remote && !strings.Contains(b.Name, "/") {
			continue // bare remote name such as "origin"
		}
		bs = append(bs, b)
	}
	return bs, nil
}

type Stash struct {
	Index   int
	Ref     string
	Hash    string
	Message string
	Date    time.Time
}

func (r *Repo) Stashes() ([]Stash, error) {
	out, err := r.Run("stash", "list", "--format=%gd"+us+"%H"+us+"%ct"+us+"%gs")
	if err != nil {
		return nil, err
	}
	var ss []Stash
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, us)
		if len(f) < 4 {
			continue
		}
		ts, _ := strconv.ParseInt(f[2], 10, 64)
		ss = append(ss, Stash{Index: i, Ref: f[0], Hash: f[1], Date: time.Unix(ts, 0), Message: f[3]})
	}
	return ss, nil
}

func (r *Repo) StashDiff(ref string) (string, error) {
	return r.Run("stash", "show", "-p", "--include-untracked", ref)
}

func (r *Repo) Remotes() []string {
	out, _ := r.Run("remote")
	return strings.Fields(out)
}

// ─── Mutations ──────────────────────────────────────────────────────────────

// ChangesDiff shows several changes at once, as for a folder in the working
// tree: tracked paths are diffed against HEAD (so staged and unstaged edits
// read as one), untracked files against /dev/null.
func (r *Repo) ChangesDiff(staged, unstaged []FileStatus) (string, error) {
	var tracked, untracked []string
	for _, f := range append(append([]FileStatus{}, staged...), unstaged...) {
		if f.Untracked {
			untracked = append(untracked, f.Path)
			continue
		}
		tracked = append(tracked, f.Path)
		if f.OrigPath != "" {
			tracked = append(tracked, f.OrigPath)
		}
	}
	var b strings.Builder
	add := func(out string) {
		if out == "" {
			return
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(out)
	}
	if len(tracked) > 0 {
		if _, err := r.Run("rev-parse", "--verify", "-q", "HEAD"); err == nil {
			out, err := r.Run(append([]string{"diff", "-M", "HEAD", "--"}, tracked...)...)
			if err != nil {
				return out, err
			}
			add(out)
		} else {
			out, _ := r.Run(append([]string{"diff", "--cached", "--"}, tracked...)...)
			add(out)
			out, _ = r.Run(append([]string{"diff", "--"}, tracked...)...)
			add(out)
		}
	}
	for i, p := range untracked {
		if i >= 50 {
			break
		}
		out, _ := r.Run("diff", "--no-index", "--", "/dev/null", p)
		add(out)
	}
	return b.String(), nil
}

func (r *Repo) Stage(paths ...string) error {
	_, err := r.Run(append([]string{"add", "-A", "--"}, paths...)...)
	return err
}

func (r *Repo) StageAll() error { _, err := r.Run("add", "-A"); return err }

func (r *Repo) Unstage(paths ...string) error {
	if _, err := r.Run("rev-parse", "--verify", "-q", "HEAD"); err != nil {
		_, err := r.Run(append([]string{"rm", "--cached", "-r", "-q", "--"}, paths...)...)
		return err
	}
	_, err := r.Run(append([]string{"reset", "-q", "HEAD", "--"}, paths...)...)
	return err
}

func (r *Repo) UnstageAll() error {
	if _, err := r.Run("rev-parse", "--verify", "-q", "HEAD"); err != nil {
		_, err := r.Run("rm", "--cached", "-r", "-q", ".")
		return err
	}
	_, err := r.Run("reset", "-q")
	return err
}

func (r *Repo) Discard(f FileStatus) error {
	if f.Untracked {
		_, err := r.Run("clean", "-f", "--", f.Path)
		return err
	}
	_, err := r.Run("checkout", "--", f.Path)
	return err
}

func (r *Repo) ApplyPatch(patch string, cached, reverse bool) error {
	args := []string{"apply", "--whitespace=nowarn"}
	if cached {
		args = append(args, "--cached")
	}
	if reverse {
		args = append(args, "-R")
	}
	args = append(args, "-")
	_, err := r.RunInput(patch, args...)
	return err
}

func (r *Repo) Commit(msg string, amend bool) error {
	args := []string{"commit", "-F", "-"}
	if amend {
		args = append(args, "--amend")
	}
	_, err := r.RunInput(msg, args...)
	return err
}

func (r *Repo) LastMessage() string {
	out, _ := r.Run("log", "-1", "--format=%B")
	return strings.TrimSpace(out)
}

func (r *Repo) Checkout(ref string) error { _, err := r.Run("switch", ref); return err }

func (r *Repo) CheckoutDetached(hash string) error {
	_, err := r.Run("switch", "--detach", hash)
	return err
}

// CheckoutRemote creates a local tracking branch for origin/foo, or switches
// to it if it already exists.
func (r *Repo) CheckoutRemote(remoteBranch string) error {
	_, local, _ := strings.Cut(remoteBranch, "/")
	if _, err := r.Run("rev-parse", "--verify", "-q", "refs/heads/"+local); err == nil {
		return r.Checkout(local)
	}
	_, err := r.Run("switch", "--track", remoteBranch)
	return err
}

func (r *Repo) CreateBranch(name, start string, checkout bool) error {
	var err error
	if checkout {
		args := []string{"switch", "-c", name}
		if start != "" {
			args = append(args, start)
		}
		_, err = r.Run(args...)
	} else {
		args := []string{"branch", name}
		if start != "" {
			args = append(args, start)
		}
		_, err = r.Run(args...)
	}
	return err
}

func (r *Repo) DeleteBranch(name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := r.Run("branch", flag, name)
	return err
}

func (r *Repo) DeleteRemoteBranch(remoteBranch string) error {
	remote, br, _ := strings.Cut(remoteBranch, "/")
	_, err := r.Run("push", remote, "--delete", br)
	return err
}

func (r *Repo) RenameBranch(old, new string) error {
	_, err := r.Run("branch", "-m", old, new)
	return err
}

func (r *Repo) Merge(ref string) error   { _, err := r.Run("merge", "--no-edit", ref); return err }
func (r *Repo) Rebase(onto string) error { _, err := r.Run("rebase", onto); return err }
func (r *Repo) Fetch() error             { _, err := r.Run("fetch", "--all", "--prune", "--tags"); return err }
func (r *Repo) Pull() error              { _, err := r.Run("pull", "--autostash"); return err }

func (r *Repo) Push(force bool, st Status) error {
	args := []string{"push"}
	if force {
		args = append(args, "--force-with-lease")
	}
	if st.Upstream == "" && !st.Detached && st.Branch != "" {
		remotes := r.Remotes()
		if len(remotes) == 0 {
			return errors.New("no remote configured")
		}
		remote := remotes[0]
		for _, rm := range remotes {
			if rm == "origin" {
				remote = rm
			}
		}
		args = append(args, "-u", remote, st.Branch)
	}
	_, err := r.Run(args...)
	return err
}

func (r *Repo) StashPush(msg string, untracked bool) error {
	args := []string{"stash", "push"}
	if untracked {
		args = append(args, "--include-untracked")
	}
	if msg != "" {
		args = append(args, "-m", msg)
	}
	_, err := r.Run(args...)
	return err
}

func (r *Repo) StashPop(ref string) error   { _, err := r.Run("stash", "pop", ref); return err }
func (r *Repo) StashApply(ref string) error { _, err := r.Run("stash", "apply", ref); return err }
func (r *Repo) StashDrop(ref string) error  { _, err := r.Run("stash", "drop", ref); return err }

func (r *Repo) CherryPick(hash string) error { _, err := r.Run("cherry-pick", hash); return err }
func (r *Repo) Revert(hash string) error     { _, err := r.Run("revert", "--no-edit", hash); return err }

func (r *Repo) Reset(hash, mode string) error {
	_, err := r.Run("reset", "--"+mode, hash)
	return err
}

func (r *Repo) Tag(name, hash, msg string) error {
	args := []string{"tag"}
	if msg != "" {
		args = append(args, "-a", "-m", msg)
	}
	args = append(args, name, hash)
	_, err := r.Run(args...)
	return err
}

func (r *Repo) DeleteTag(name string) error { _, err := r.Run("tag", "-d", name); return err }

// Continue / abort an in-progress operation.
func (r *Repo) Abort(state string) error {
	switch state {
	case "rebasing":
		_, err := r.Run("rebase", "--abort")
		return err
	case "merging":
		_, err := r.Run("merge", "--abort")
		return err
	case "cherry-picking":
		_, err := r.Run("cherry-pick", "--abort")
		return err
	case "reverting":
		_, err := r.Run("revert", "--abort")
		return err
	}
	return errors.New("nothing to abort")
}

func (r *Repo) ContinueCmd(state string) []string {
	switch state {
	case "rebasing":
		return []string{"rebase", "--continue"}
	case "merging":
		return []string{"commit", "--no-edit"}
	case "cherry-picking":
		return []string{"cherry-pick", "--continue"}
	case "reverting":
		return []string{"revert", "--continue"}
	}
	return nil
}
