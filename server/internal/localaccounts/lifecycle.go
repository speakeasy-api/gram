package localaccounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// DaemonRunner is scoped to the validated worktree root and exact daemon ID.
type DaemonRunner func(context.Context, string, ...string) ([]byte, error)

func RunDaemon(ctx context.Context, root, stateDir, xdgStateHome string, args ...string) ([]byte, error) {
	// status --json exposes only the namespace, not the originating directory.
	// Pitchfork 2.22 persists that directory in its supervisor state. Recheck it
	// before every operation, including recovery, rather than trusting basename.
	if len(args) < 2 {
		return nil, errors.New("missing daemon ID")
	}
	if err := checkDaemonOrigin(root, stateDir, xdgStateHome, args[1]); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "pitchfork", args...) // #nosec G204 -- fixed executable and validated worktree daemon origin.
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("pitchfork operation failed")
	}
	return out, nil
}

// WithStoppedWriters must be called after target validation and while holding the
// worktree lifecycle lock. Restore every originally running writer, including a
// writer whose stop failed: stop may have taken effect before returning an error.
func WithStoppedWriters(ctx context.Context, root string, run DaemonRunner, action func(context.Context) (Result, error)) (result Result, err error) {
	namespace := filepath.Base(root)
	var running []string
	for _, name := range []string{"server", "worker"} {
		checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		data, e := run(checkCtx, root, "status", namespace+"/"+name, "--json")
		cancel()
		if e != nil {
			return result, e
		}
		active, e := runningWorktreeDaemon(data, namespace, name)
		if e != nil {
			return result, e
		}
		if active {
			running = append(running, name)
		}
	}
	defer func() {
		// Cancellation of the action must not cancel recovery. Bound each restart
		// independently so a failed server restart does not prevent worker recovery.
		for _, name := range running {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
			_, e := run(cleanup, root, "start", namespace+"/"+name)
			cancel()
			if e != nil {
				err = errors.Join(err, fmt.Errorf("restore %s failed (account profile committed=%t): %w", name, result.Committed, e))
			}
		}
	}()
	for _, name := range running {
		stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, e := run(stopCtx, root, "stop", namespace+"/"+name)
		cancel()
		if e != nil {
			return result, fmt.Errorf("stop %s failed: %w", name, e)
		}
	}
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("account lifecycle canceled: %w", err)
	}
	return action(ctx)
}

func runningWorktreeDaemon(data []byte, namespace, name string) (bool, error) {
	var v struct {
		ID        string `json:"id"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		PID       *int   `json:"pid"`
		Status    string `json:"status"`
	}
	if json.Unmarshal(data, &v) != nil || v.ID != namespace+"/"+name || v.Namespace != namespace || v.Name != name {
		return false, errors.New("daemon evidence does not belong to this worktree")
	}
	if v.Status == "running" && v.PID != nil && *v.PID > 0 {
		return true, nil
	}
	if err := stoppedWorktreeDaemon(data, namespace, name); err != nil {
		return false, err
	}
	return false, nil
}

// No fallback to namespace/config discovery: a never-registered daemon has no
// supervisor provenance and must fail closed. This reads only selected metadata;
// commands, environment and other stacks' records are never reported.
func checkDaemonOrigin(root, stateDir, xdgStateHome, id string) error {
	// Match Pitchfork: its explicit override takes precedence over XDG state.
	if stateDir == "" && xdgStateHome != "" {
		stateDir = filepath.Join(xdgStateHome, "pitchfork")
	}
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return errors.New("cannot determine pitchfork state directory")
		}
		stateDir = filepath.Join(home, ".local", "state", "pitchfork")
	}
	if !filepath.IsAbs(stateDir) {
		return errors.New("pitchfork state directory must be absolute")
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "state.toml")) // #nosec G304 -- caller-selected absolute local supervisor state directory.
	if err != nil {
		return errors.New("cannot verify pitchfork daemon origin")
	}
	return validateDaemonOrigin(data, root, id)
}

func validateDaemonOrigin(data []byte, root, id string) error {
	var state struct {
		Daemons map[string]struct {
			ID  string `toml:"id"`
			Dir string `toml:"dir"`
		} `toml:"daemons"`
	}
	if _, err := toml.Decode(string(data), &state); err != nil {
		return errors.New("cannot parse pitchfork daemon origin")
	}
	daemon, ok := state.Daemons[id]
	if !ok || daemon.ID != id || !filepath.IsAbs(daemon.Dir) {
		return errors.New("pitchfork daemon origin is unknown")
	}
	actual, err := filepath.EvalSymlinks(daemon.Dir)
	if err != nil {
		return errors.New("cannot resolve pitchfork daemon origin")
	}
	expected, err := filepath.EvalSymlinks(root)
	if err != nil || actual != expected {
		return errors.New("pitchfork daemon originates from another worktree; refusing lifecycle operation")
	}
	return nil
}
