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
	"strings"
)

const (
	// fixturesPath is left out of the code key, so a commit that only edits
	// fixtures reuses every unedited case's record.
	fixturesPath = "server/internal/scanners/promptinjection/testdata"

	// codeKeyHexLen and uncommittedKeyHexLen are the lengths of the code key's
	// two parts: the committed code, and the uncommitted changes when present.
	codeKeyHexLen        = 12
	uncommittedKeyHexLen = 8
)

// runCommit names the commit whose code produced a run.
type runCommit struct {
	// SHA is the commit's full hash.
	SHA string `json:"sha"`

	// Subject is the commit's subject line.
	Subject string `json:"subject"`

	// Uncommitted reports that the code had changes outside the fixtures that
	// the commit lacks.
	Uncommitted bool `json:"uncommitted,omitempty"`
}

// name is how progress lines refer to the run: the commit's short hash,
// marked when the code had uncommitted changes.
func (c runCommit) name() string {
	name := c.SHA
	if len(name) > 10 {
		name = name[:10]
	}
	if c.Uncommitted {
		name += " + uncommitted"
	}
	return name
}

// measured is the code a run measures.
type measured struct {
	// key names the run directory. Code that differs outside the fixtures
	// gets another key.
	key string

	// commit is the checkout's HEAD.
	commit runCommit
}

// measuredCode reads the git checkout that holds dir, or the working
// directory when dir is empty. The binary must be built from that checkout,
// so the checkout's code is the code it runs.
func measuredCode(ctx context.Context, dir string) (measured, error) {
	var code measured
	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return code, err
	}
	root = strings.TrimSpace(root)
	tree, err := git(ctx, root, "ls-tree", "-r", "-z", "--full-tree", "HEAD")
	if err != nil {
		return code, err
	}
	head, err := git(ctx, root, "log", "-1", "--format=%H%x00%s", "HEAD")
	if err != nil {
		return code, err
	}
	uncommitted, err := uncommittedCode(ctx, root)
	if err != nil {
		return code, err
	}

	sha, subject, _ := strings.Cut(strings.TrimSpace(head), "\x00")
	code.commit = runCommit{SHA: sha, Subject: subject, Uncommitted: uncommitted != nil}
	code.key = shortHash([]byte(withoutFixtures(tree)), codeKeyHexLen)
	if uncommitted != nil {
		code.key += "-" + shortHash(uncommitted, uncommittedKeyHexLen)
	}
	return code, nil
}

// withoutFixtures drops the fixtures' entries from a NUL-separated
// `git ls-tree -r -z` listing.
func withoutFixtures(tree string) string {
	var kept strings.Builder
	for entry := range strings.SplitSeq(tree, "\x00") {
		_, path, _ := strings.Cut(entry, "\t")
		if entry == "" || strings.HasPrefix(path, fixturesPath+"/") {
			continue
		}
		kept.WriteString(entry)
		kept.WriteByte(0)
	}
	return kept.String()
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
		content, err := os.ReadFile(filepath.Join(root, name)) // #nosec G304 -- git lists the file inside the checkout.
		if err != nil {
			return nil, fmt.Errorf("read untracked file: %w", err)
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

func shortHash(b []byte, n int) string {
	return fmt.Sprintf("%x", sha256.Sum256(b))[:n]
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

// runsDir holds every run, one directory per code key.
func runsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the cache directory: %w", err)
	}
	return filepath.Join(home, ".cache", "gram-pi-eval", "runs"), nil
}
