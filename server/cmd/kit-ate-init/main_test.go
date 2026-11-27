package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testArchive(t *testing.T, names []string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, name := range names {
		data := []byte("fixture")
		if name == ".kit/credentials/gateway-token" {
			data = []byte(strings.Repeat("x", 43))
		}
		require.NoError(t, w.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}))
		_, err := w.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return b.Bytes()
}

func TestBootstrapLaunchesOnceAndRemovesAssertionEndpoint(t *testing.T) {
	t.Parallel()
	homeDir := t.TempDir()
	s, err := newInitializer(homeDir)
	require.NoError(t, err)

	post := func(path string, body []byte) int {
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}

	// Assertion delivery is removed; ATE egress injects actor JWTs.
	require.Equal(t, http.StatusNotFound, post("/assertion", []byte("jwt.fixture")))

	archive := testArchive(t, bootstrapFiles)
	require.Equal(t, http.StatusNoContent, post("/bootstrap", archive))

	select {
	case <-s.launch:
	default:
		t.Fatal("Kit was not launched after bootstrap")
	}

	// Write-once: subsequent bootstrap attempts are rejected.
	require.Equal(t, http.StatusConflict, post("/bootstrap", archive))

	_, err = os.Stat(filepath.Join(homeDir, ".kit/ate-initialized"))
	require.NoError(t, err)

	// Resuming from a durable rootfs snapshot launches Kit immediately without /bootstrap.
	restored, err := newInitializer(homeDir)
	require.NoError(t, err)
	select {
	case <-restored.launch:
	default:
		t.Fatal("resumed actor did not launch Kit immediately")
	}

	r := httptest.NewRequest(http.MethodPost, "/bootstrap", bytes.NewReader(archive))
	w := httptest.NewRecorder()
	restored.ServeHTTP(w, r)
	require.Equal(t, http.StatusConflict, w.Code)
}

func TestBootstrapRejectsTraversalAndSymlinks(t *testing.T) {
	t.Parallel()
	s, err := newInitializer(t.TempDir())
	require.NoError(t, err)

	require.Error(t, s.install(testArchive(t, append(append([]string{}, bootstrapFiles...), "../outside"))))

	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(s.home, ".kit")))
	require.Error(t, s.install(testArchive(t, bootstrapFiles)))

	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestInitTrapsSIGTERMWithoutExitingAndAdvertisesDraining(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".kit"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".kit", "ate-initialized"), []byte("1"), 0o600))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := lis.Addr().String()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	pidFile := filepath.Join(homeDir, "child.pid")
	termCh := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, homeDir, lis, termCh, func(cmdCtx context.Context) *exec.Cmd {
			return exec.CommandContext(cmdCtx, "sh", "-c", `echo $$ > "$1"; exec sleep 30`, "_", pidFile)
		})
	}()

	getStatus := func() statusResponse {
		t.Helper()
		resp, err := http.Get("http://" + addr + "/status")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out statusResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return out
	}

	// Wait until HTTP server is accepting requests and child process has written its PID.
	var childPID int
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/readyz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
		raw, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil || pid <= 0 {
			return false
		}
		childPID = pid
		return true
	}, 3*time.Second, 10*time.Millisecond)

	before := getStatus()
	require.False(t, before.Draining)
	require.True(t, before.Started)

	// Deliver SIGTERM (worker drain). Both PID 1 and the launched child must
	// stay alive while advertising draining=true for synchronous suspend.
	termCh <- syscall.SIGTERM

	require.Eventually(t, func() bool {
		return getStatus().Draining
	}, 2*time.Second, 10*time.Millisecond)

	select {
	case err := <-done:
		t.Fatalf("init exited on SIGTERM (must stay alive for synchronous suspend): %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, syscall.Kill(childPID, 0), "child process must survive SIGTERM until harness suspends actor")

	resp, err := http.Get("http://" + addr + "/readyz")
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected run error on cancel: %v", err)
	}
}
