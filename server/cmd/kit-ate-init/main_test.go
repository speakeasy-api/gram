package main

import (
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

func testPayloadJSON(t *testing.T, p bootstrapPayload) []byte {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	return raw
}

func testConfigForRoot(rootDir, probeAddr string) config {
	return config{
		rootDir:   rootDir,
		probeAddr: probeAddr,
		listen:    defaultListenAddr,
		marker:    defaultMarkerRelPath,
		command:   []string{"/bin/true"},
	}
}

func TestParseConfigRequiresFlagsAndCommand(t *testing.T) {
	t.Parallel()
	_, err := parseConfig(nil)
	require.Error(t, err)
	_, err = parseConfig([]string{"--root=/tmp/r", "--probe-addr=127.0.0.1:8081"})
	require.Error(t, err)

	cfg, err := parseConfig([]string{"--root=/tmp/r", "--probe-addr=127.0.0.1:8081", "--", "/usr/bin/app", "--flag"})
	require.NoError(t, err)
	require.Equal(t, "/tmp/r", cfg.rootDir)
	require.Equal(t, "127.0.0.1:8081", cfg.probeAddr)
	require.Equal(t, []string{"/usr/bin/app", "--flag"}, cfg.command)
}

func TestBootstrapGenericPayloadWriteOnceAndResume(t *testing.T) {
	t.Parallel()
	rootDir := t.TempDir()
	probeLis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = probeLis.Close() }()

	cfg := testConfigForRoot(rootDir, probeLis.Addr().String())
	s, err := newInitializer(cfg)
	require.NoError(t, err)

	post := func(path string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}

	// Legacy assertion path does not exist.
	w := post("/assertion", []byte("jwt.fixture"))
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, strconv.Itoa(wireVersion), w.Header().Get(wireVersionHeader))

	// Unsupported wire version is rejected.
	badVersion := testPayloadJSON(t, bootstrapPayload{
		Version: 99,
		Files:   []bootstrapFile{{Path: "conf/app.toml", Mode: 0o600, Content: []byte("ok")}},
	})
	require.Equal(t, http.StatusBadRequest, post("/bootstrap", badVersion).Code)

	payload := testPayloadJSON(t, bootstrapPayload{
		Version: wireVersion,
		Dirs: []bootstrapDir{
			{Path: "conf", Mode: 0o700},
			{Path: "work", Mode: 0o700},
		},
		Files: []bootstrapFile{
			{Path: "conf/app.toml", Mode: 0o600, Content: []byte("model = 'test'\n")},
			{Path: "conf/secret.token", Mode: 0o600, Content: []byte(strings.Repeat("x", 43))},
		},
		Env: map[string]string{
			"APP_ENDPOINT": "https://api.example.test/v1",
			"APP_TOKEN":    "placeholder-value",
		},
	})
	require.Equal(t, http.StatusNoContent, post("/bootstrap", payload).Code)

	select {
	case <-s.launch:
	default:
		t.Fatal("workload was not launched after bootstrap")
	}
	require.ElementsMatch(t, []string{"APP_ENDPOINT=https://api.example.test/v1", "APP_TOKEN=placeholder-value"}, s.childEnv())

	// Write-once: subsequent bootstrap attempts are rejected.
	require.Equal(t, http.StatusConflict, post("/bootstrap", payload).Code)

	// Files and generic marker exist on disk with exact content and permissions.
	data, err := os.ReadFile(filepath.Join(rootDir, "conf/app.toml"))
	require.NoError(t, err)
	require.Equal(t, "model = 'test'\n", string(data))
	_, err = os.Stat(filepath.Join(rootDir, defaultMarkerRelPath))
	require.NoError(t, err)

	// Generic /app-readyz probes the configured --probe-addr.
	req := httptest.NewRequest(http.MethodGet, "/app-readyz", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, strconv.Itoa(wireVersion), rec.Header().Get(wireVersionHeader))

	// Resuming from a durable rootfs snapshot launches immediately without /bootstrap
	// and restores the persisted bootstrap environment variables.
	restored, err := newInitializer(cfg)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"APP_ENDPOINT=https://api.example.test/v1", "APP_TOKEN=placeholder-value"}, restored.childEnv())
	select {
	case <-restored.launch:
	default:
		t.Fatal("resumed actor did not launch workload immediately")
	}
	rec = httptest.NewRecorder()
	restored.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/bootstrap", bytes.NewReader(payload)))
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestBootstrapRejectsTraversalAbsolutePathsSymlinksAndBadModes(t *testing.T) {
	t.Parallel()
	rootDir := t.TempDir()
	s, err := newInitializer(testConfigForRoot(rootDir, "127.0.0.1:8081"))
	require.NoError(t, err)

	cases := []struct {
		name    string
		payload bootstrapPayload
	}{
		{name: "parent traversal file", payload: bootstrapPayload{Version: wireVersion, Files: []bootstrapFile{{Path: "../outside", Mode: 0o600, Content: []byte("x")}}}},
		{name: "absolute file path", payload: bootstrapPayload{Version: wireVersion, Files: []bootstrapFile{{Path: "/etc/passwd", Mode: 0o600, Content: []byte("x")}}}},
		{name: "reserved marker file", payload: bootstrapPayload{Version: wireVersion, Files: []bootstrapFile{{Path: defaultMarkerRelPath, Mode: 0o600, Content: []byte("x")}}}},
		{name: "reserved env file", payload: bootstrapPayload{Version: wireVersion, Files: []bootstrapFile{{Path: defaultEnvRelPath, Mode: 0o600, Content: []byte("x")}}}},
		{name: "empty env key", payload: bootstrapPayload{Version: wireVersion, Files: []bootstrapFile{{Path: "ok.txt", Mode: 0o600, Content: []byte("x")}}, Env: map[string]string{"": "v"}}},
		{name: "env key with equals", payload: bootstrapPayload{Version: wireVersion, Files: []bootstrapFile{{Path: "ok.txt", Mode: 0o600, Content: []byte("x")}}, Env: map[string]string{"A=B": "v"}}},
		{name: "env value with NUL byte", payload: bootstrapPayload{Version: wireVersion, Files: []bootstrapFile{{Path: "ok.txt", Mode: 0o600, Content: []byte("x")}}, Env: map[string]string{"K": "bad\x00val"}}},
		{name: "duplicate file path", payload: bootstrapPayload{Version: wireVersion, Files: []bootstrapFile{{Path: "a/b", Mode: 0o600, Content: []byte("1")}, {Path: "a/b", Mode: 0o600, Content: []byte("2")}}}},
		{name: "setuid file mode", payload: bootstrapPayload{Version: wireVersion, Files: []bootstrapFile{{Path: "a/b", Mode: os.ModeSetuid | 0o600, Content: []byte("x")}}}},
		{name: "parent traversal dir", payload: bootstrapPayload{Version: wireVersion, Dirs: []bootstrapDir{{Path: "../escape", Mode: 0o700}}, Files: []bootstrapFile{{Path: "ok.txt", Mode: 0o600, Content: []byte("x")}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sub, subErr := newInitializer(testConfigForRoot(t.TempDir(), "127.0.0.1:8081"))
			require.NoError(t, subErr)
			require.Error(t, sub.install(testPayloadJSON(t, tc.payload)))
		})
	}

	t.Run("marker collides with env file", func(t *testing.T) {
		t.Parallel()
		cfg := testConfigForRoot(t.TempDir(), "127.0.0.1:8081")
		cfg.marker = defaultEnvRelPath
		_, initErr := newInitializer(cfg)
		require.Error(t, initErr)
	})

	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(rootDir, "conf")))
	require.Error(t, s.install(testPayloadJSON(t, bootstrapPayload{
		Version: wireVersion,
		Files:   []bootstrapFile{{Path: "conf/escape.txt", Mode: 0o600, Content: []byte("nope")}},
	})))

	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestInitTrapsSIGTERMWithoutExitingAndAdvertisesDraining(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, defaultMarkerRelPath), []byte("1"), 0o600))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := lis.Addr().String()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	pidFile := filepath.Join(rootDir, "child.pid")
	cfg := testConfigForRoot(rootDir, "127.0.0.1:8081")
	termCh := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, cfg, lis, termCh, func(cmdCtx context.Context) *exec.Cmd {
			return exec.CommandContext(cmdCtx, "sh", "-c", `echo $$ > "$1"; exec sleep 30`, "_", pidFile)
		})
	}()

	getStatus := func() statusResponse {
		t.Helper()
		resp, err := http.Get("http://" + addr + "/status")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, strconv.Itoa(wireVersion), resp.Header.Get(wireVersionHeader))
		var out statusResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return out
	}

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
	require.Equal(t, wireVersion, before.Version)
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
