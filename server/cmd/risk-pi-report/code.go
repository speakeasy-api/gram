package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// fixturesPath holds the fixtures. An uncommitted fixture edit leaves the
	// measured code unchanged, so a resumed run scores just the edited cases.
	fixturesPath = "server/internal/scanners/promptinjection/testdata"

	// uncommittedHexLen is the length of an uncommitted change's hash.
	uncommittedHexLen = 8

	// runIDLayout starts a run's directory name with its start time, to the
	// nanosecond and fixed width, so runs sort oldest first.
	runIDLayout = "20060102T150405.000000000Z"
)

// runCommit is the code a run measured: a commit, plus any change the
// checkout had outside the fixtures that the commit lacks.
type runCommit struct {
	// SHA is the commit's full hash.
	SHA string `json:"sha"`

	// Subject is the commit's subject line.
	Subject string `json:"subject"`

	// Uncommitted hashes the checkout's changes outside the fixtures that the
	// commit lacks; empty for a clean checkout.
	Uncommitted string `json:"uncommitted,omitempty"`
}

// sameCode reports whether c and other are the same code. The subject only
// labels a commit.
func (c runCommit) sameCode(other runCommit) bool {
	return c.SHA == other.SHA && c.Uncommitted == other.Uncommitted
}

// name is how progress lines refer to the run: the commit's short hash,
// marked when the code had uncommitted changes.
func (c runCommit) name() string {
	name := c.SHA
	if len(name) > 10 {
		name = name[:10]
	}
	if c.Uncommitted != "" {
		name += " + uncommitted"
	}
	return name
}

// checkoutCommit reads the git checkout that holds dir, or the working
// directory when dir is empty. The binary must be built from that checkout,
// so the checkout's code is the code it runs.
func checkoutCommit(ctx context.Context, dir string) (runCommit, error) {
	var commit runCommit
	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return commit, err
	}
	root = strings.TrimSpace(root)
	head, err := git(ctx, root, "log", "-1", "--format=%H%x00%s", "HEAD")
	if err != nil {
		return commit, err
	}
	uncommitted, err := uncommittedCode(ctx, root)
	if err != nil {
		return commit, err
	}
	commit.SHA, commit.Subject, _ = strings.Cut(strings.TrimSpace(head), "\x00")
	if uncommitted != nil {
		commit.Uncommitted = fmt.Sprintf("%x", sha256.Sum256(uncommitted))[:uncommittedHexLen]
	}
	return commit, nil
}

// uncommittedCode returns the checkout's changes outside the fixtures that
// HEAD lacks: the diff of tracked files, and the name and content hash of
// each untracked file. It returns nil for a clean checkout.
func uncommittedCode(ctx context.Context, root string) ([]byte, error) {
	exclude := ":(exclude)" + fixturesPath
	diff, err := git(ctx, root, "diff", "--binary", "--no-color", "--no-ext-diff", "HEAD", "--", ".", exclude)
	if err != nil {
		return nil, err
	}
	untracked, err := git(ctx, root, "ls-files", "-z", "--others", "--exclude-standard", "--", ".", exclude)
	if err != nil {
		return nil, err
	}
	var changes bytes.Buffer
	changes.WriteString(diff)
	for name := range strings.SplitSeq(untracked, "\x00") {
		if name == "" {
			continue
		}
		content, err := untrackedContent(filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(content)
		changes.WriteString(name)
		changes.WriteByte(0)
		changes.Write(sum[:])
	}
	if changes.Len() == 0 {
		return nil, nil
	}
	return changes.Bytes(), nil
}

// untrackedContent is what an untracked path contributes to the uncommitted
// hash, as git would store it: a symlink's target rather than the file it
// points to, a nested repository's name only, and a file's bytes.
func untrackedContent(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("stat untracked file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return nil, fmt.Errorf("read untracked symlink: %w", err)
		}
		return []byte(target), nil
	}
	if info.IsDir() {
		return nil, nil
	}
	content, err := os.ReadFile(path) // #nosec G304 -- git lists the file inside the checkout.
	if err != nil {
		return nil, fmt.Errorf("read untracked file: %w", err)
	}
	return content, nil
}

// git runs a git command in dir, or in the working directory when dir is
// empty, and returns its output.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed git subcommands; only paths vary.
	cmd.Dir = dir
	out, err := cmd.Output()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// pickRunDir returns the run directory to write under runs: a new run, or
// with resume the most recent run of the same code. Resuming with no such run
// starts a new one.
func pickRunDir(runs string, commit runCommit, resume bool, now time.Time) (string, error) {
	if !resume {
		return newRunDir(runs, now)
	}
	latest, err := latestRunOf(runs, commit)
	if err != nil {
		return "", err
	}
	if latest != "" {
		return latest, nil
	}
	fmt.Fprintf(os.Stderr, "no earlier run of %s to resume; starting a new run\n", commit.name())
	return newRunDir(runs, now)
}

// newRunDir makes the directory of a run that starts at now. The suffix
// keeps two runs that start at the same instant apart.
func newRunDir(runs string, now time.Time) (string, error) {
	if err := os.MkdirAll(runs, 0o750); err != nil {
		return "", fmt.Errorf("create runs dir: %w", err)
	}
	dir, err := os.MkdirTemp(runs, now.UTC().Format(runIDLayout)+"-")
	if err != nil {
		return "", fmt.Errorf("create run dir: %w", err)
	}
	return dir, nil
}

// latestRunOf returns the most recently started run under runs that measured
// commit's code, or empty when there is none.
func latestRunOf(runs string, commit runCommit) (string, error) {
	entries, err := os.ReadDir(runs)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("list runs: %w", err)
	}
	// ReadDir sorts by name, which is start time.
	for _, entry := range slices.Backward(entries) {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(runs, entry.Name())
		manifest, err := readManifest(filepath.Join(dir, manifestFile))
		if err == nil && manifest.Commit.sameCode(commit) {
			return dir, nil
		}
	}
	return "", nil
}

// runsDir holds every run, one directory each.
func runsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the cache directory: %w", err)
	}
	return filepath.Join(home, ".cache", "gram-pi-eval", "runs"), nil
}
