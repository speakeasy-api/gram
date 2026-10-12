package main

import (
	"context"
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
	// fixturesPath holds the fixtures.
	fixturesPath = "server/internal/scanners/promptinjection/testdata"

	// runIDLayout starts a run's directory name with its start time, to the
	// nanosecond and fixed width, so runs sort oldest first.
	runIDLayout = "20060102T150405.000000000Z"
)

// runCommit is the commit a run measured.
type runCommit struct {
	// SHA is the commit's full hash.
	SHA string `json:"sha"`

	// Subject is the commit's subject line.
	Subject string `json:"subject"`
}

// name is how progress lines refer to the run: the commit's short hash.
func (c runCommit) name() string {
	return c.SHA[:min(len(c.SHA), 10)]
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
	commit.SHA, commit.Subject, _ = strings.Cut(strings.TrimSpace(head), "\x00")
	return commit, nil
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
// with resume the most recent run of the same commit. Resuming with no such
// run starts a new one.
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
// commit, or empty when there is none. A run without a manifest is
// skipped; an unreadable one is an error, so resuming never quietly falls back
// to an older run.
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
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if manifest.Commit.SHA == commit.SHA {
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
