// Package functions implements the project-side work behind the
// `speakeasy functions` commands: scaffolding a project from the embedded
// templates and driving the project's own Gram Functions SDK through Node.js.
package functions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Runner runs the external tools a functions project needs: node, git and a
// package manager. Commands resolve against the PATH in Env, so tests can put
// fake binaries in front of the real ones without touching the process
// environment.
type Runner struct {
	// Env is the environment for child processes, in os.Environ form.
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// NewRunner returns a Runner that uses the process environment and standard
// streams.
func NewRunner() Runner {
	return Runner{
		Env:    os.Environ(),
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Getenv returns the value of key in r.Env. Names are case-insensitive on
// Windows, where the environment holds Path rather than PATH.
func (r Runner) Getenv(key string) string {
	value := ""
	for _, kv := range r.Env {
		name, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if name == key || (runtime.GOOS == "windows" && strings.EqualFold(name, key)) {
			// Later entries win, as they do for exec.Cmd.
			value = v
		}
	}
	return value
}

// errCommandNotFound reports that a command is not on the Runner's PATH.
var errCommandNotFound = errors.New("command not found")

// lookPath finds name on the PATH in r.Env.
func (r Runner) lookPath(name string) (string, error) {
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = filepath.SplitList(strings.ToLower(r.Getenv("PATHEXT")))
		if len(exts) == 0 {
			exts = []string{".com", ".exe", ".bat", ".cmd"}
		}
	}

	for _, dir := range filepath.SplitList(r.Getenv("PATH")) {
		// Skip empty and relative entries, which resolve against the project
		// directory, as exec.LookPath refuses them with exec.ErrDot.
		if !filepath.IsAbs(dir) {
			continue
		}
		for _, ext := range exts {
			candidate := filepath.Join(dir, name+ext)
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() {
				continue
			}
			if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
				continue
			}
			return candidate, nil
		}
	}

	return "", fmt.Errorf("%s: %w", name, errCommandNotFound)
}

// command builds a command that runs in dir with the Runner's environment
// and streams.
func (r Runner) command(ctx context.Context, dir string, name string, args ...string) (*exec.Cmd, error) {
	path, err := r.lookPath(name)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, path, args...) // #nosec G204 -- the binary is resolved from PATH and arguments are passed without a shell.
	cmd.Dir = dir
	cmd.Env = r.Env
	cmd.Stdin = r.Stdin
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	return cmd, nil
}

// run runs a command to completion.
func (r Runner) run(ctx context.Context, dir string, name string, args ...string) error {
	cmd, err := r.command(ctx, dir, name, args...)
	if err != nil {
		return err
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
