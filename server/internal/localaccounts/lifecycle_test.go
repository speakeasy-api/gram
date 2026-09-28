package localaccounts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriterLifecycle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                      string
		server, worker                            bool
		stopFail, restartFail, actionFail, cancel bool
		want                                      []string
	}{
		{name: "both running", server: true, worker: true, want: []string{"stop server", "stop worker", "action", "start server", "start worker"}},
		{name: "both stopped", want: []string{"action"}},
		{name: "server only", server: true, want: []string{"stop server", "action", "start server"}},
		{name: "worker only", worker: true, want: []string{"stop worker", "action", "start worker"}},
		{name: "stop failure", server: true, worker: true, stopFail: true, want: []string{"stop server", "start server", "start worker"}},
		{name: "action failure", server: true, worker: true, actionFail: true, want: []string{"stop server", "stop worker", "action", "start server", "start worker"}},
		{name: "restart failure", server: true, worker: true, restartFail: true, want: []string{"stop server", "stop worker", "action", "start server", "start worker"}},
		{name: "cancellation", server: true, worker: true, cancel: true, want: []string{"stop server", "stop worker", "action", "start server", "start worker"}},
	} {
		t.Log(tc.name)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var calls []string
		run := func(ctx context.Context, root string, args ...string) ([]byte, error) {
			require.Equal(t, "/work/fixture", root)
			require.True(t, strings.HasPrefix(args[1], "fixture/"))
			name := strings.TrimPrefix(args[1], "fixture/")
			if args[0] == "status" {
				active := tc.server
				if name == "worker" {
					active = tc.worker
				}
				pid, status := "null", "stopped"
				if active {
					pid, status = "123", "running"
				}
				return fmt.Appendf(nil, `{"id":"fixture/%s","namespace":"fixture","name":"%s","pid":%s,"status":"%s"}`, name, name, pid, status), nil
			}
			calls = append(calls, args[0]+" "+name)
			require.NoError(t, ctx.Err())
			_, bounded := ctx.Deadline()
			require.True(t, bounded)
			if tc.stopFail && args[0] == "stop" {
				return nil, errors.New("stop failed")
			}
			if tc.restartFail && args[0] == "start" && name == "server" {
				return nil, errors.New("restart failed")
			}
			return nil, nil
		}
		result, err := WithStoppedWriters(ctx, "/work/fixture", run, func(ctx context.Context) (Result, error) {
			calls = append(calls, "action")
			if tc.actionFail {
				return Result{}, errors.New("action failed")
			}
			if tc.cancel {
				cancel()
				return Result{}, ctx.Err()
			}
			return Result{Committed: true}, nil
		})
		require.Equal(t, tc.want, calls)
		if tc.stopFail || tc.restartFail || tc.actionFail || tc.cancel {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		if tc.restartFail {
			require.True(t, result.Committed)
			require.ErrorContains(t, err, "restore server failed (account profile committed=true)")
		}
		if tc.cancel {
			require.ErrorIs(t, err, context.Canceled)
		}
	}
}

func TestWriterSnapshotFailsClosed(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		`{}`, `{"id":"other/server","namespace":"other","name":"server","pid":123,"status":"running"}`,
		`{"id":"fixture/server","namespace":"fixture","name":"server","pid":null,"status":"running"}`,
		`{"id":"fixture/server","namespace":"fixture","name":"server","status":"stopped"}`,
		`{"id":"fixture/server","namespace":"fixture","name":"server","pid":123,"status":"starting"}`,
	} {
		_, err := WithStoppedWriters(t.Context(), "/work/fixture", func(_ context.Context, _ string, args ...string) ([]byte, error) {
			require.Equal(t, "status", args[0])
			return []byte(data), nil
		}, func(context.Context) (Result, error) { t.Fatal("unsafe snapshot reached action"); return Result{}, nil })
		require.Error(t, err)
	}
}

func TestDaemonOrigin(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "fixture")
	collision := filepath.Join(t.TempDir(), "fixture")
	require.NoError(t, os.Mkdir(root, 0700))
	require.NoError(t, os.Mkdir(collision, 0700))
	link := filepath.Join(t.TempDir(), "linked")
	require.NoError(t, os.Symlink(root, link))
	for _, tc := range []struct {
		name, dir string
		bad       bool
	}{
		{"exact", root, false}, {"canonical symlink", link, false},
		{"same basename other checkout", collision, true}, {"unknown", "", true}, {"relative", "fixture", true},
	} {
		t.Log(tc.name)
		data := fmt.Appendf(nil, "[daemons.\"fixture/server\"]\nid = \"fixture/server\"\ndir = %q\n", tc.dir)
		err := validateDaemonOrigin(data, root, "fixture/server")
		if tc.bad {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
	require.Error(t, validateDaemonOrigin([]byte("[daemons]"), root, "fixture/server"))
	require.Error(t, validateDaemonOrigin([]byte("invalid ["), root, "fixture/server"))
}

func TestDaemonOriginCollisionNeverInvokesPitchfork(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fixture")
	other := filepath.Join(t.TempDir(), "fixture")
	require.NoError(t, os.Mkdir(root, 0700))
	require.NoError(t, os.Mkdir(other, 0700))
	dir := t.TempDir()
	marker := filepath.Join(dir, "invoked")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pitchfork"), fmt.Appendf(nil, "#!/bin/sh\ntouch %q\n", marker), 0700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	data := fmt.Appendf(nil, "[daemons.\"fixture/server\"]\nid = \"fixture/server\"\ndir = %q\n", other)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "state.toml"), data, 0600))
	for _, op := range []string{"status", "stop", "start"} {
		_, err := RunDaemon(t.Context(), root, dir, "", op, "fixture/server")
		require.ErrorContains(t, err, "another worktree")
		_, err = os.Stat(marker)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestDaemonOriginStateDirectoryPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	explicit := t.TempDir()
	xdg := t.TempDir()
	data := fmt.Appendf(nil, "[daemons.\"fixture/server\"]\nid = \"fixture/server\"\ndir = %q\n", root)
	// Populate one candidate at a time: a wrong precedence or missing fallback
	// cannot accidentally pass by reading the same valid metadata elsewhere.
	for _, tc := range []struct {
		name         string
		stateDir     string
		xdgStateHome string
		wantDir      string
	}{
		{name: "explicit before XDG", stateDir: explicit, xdgStateHome: xdg, wantDir: explicit},
		{name: "XDG before home", stateDir: "", xdgStateHome: xdg, wantDir: filepath.Join(xdg, "pitchfork")},
		{name: "home fallback", stateDir: "", xdgStateHome: "", wantDir: filepath.Join(home, ".local", "state", "pitchfork")},
	} {
		t.Log(tc.name)
		require.NoError(t, os.MkdirAll(tc.wantDir, 0700))
		stateFile := filepath.Join(tc.wantDir, "state.toml")
		require.NoError(t, os.WriteFile(stateFile, data, 0600))
		require.NoError(t, checkDaemonOrigin(root, tc.stateDir, tc.xdgStateHome, "fixture/server"))
		require.NoError(t, os.Remove(stateFile))
	}
	require.ErrorContains(t, checkDaemonOrigin(root, "relative", xdg, "fixture/server"), "must be absolute")
	require.ErrorContains(t, checkDaemonOrigin(root, "", "relative", "fixture/server"), "must be absolute")
}
