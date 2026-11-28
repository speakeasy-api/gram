package main

import (
	"archive/tar"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestBootstrapLaunchesOnce(t *testing.T) {
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

	archive := testArchive(t, bootstrapFiles)
	require.Equal(t, http.StatusNoContent, post("/bootstrap", archive))

	select {
	case <-s.launch:
	default:
		t.Fatal("Kit was not launched after bootstrap")
	}

	// Identical re-POST is idempotent (204); a different body is rejected (409).
	require.Equal(t, http.StatusNoContent, post("/bootstrap", archive))

	var diffBuf bytes.Buffer
	tw := tar.NewWriter(&diffBuf)
	for _, name := range bootstrapFiles {
		data := []byte("different-fixture")
		if name == ".kit/credentials/gateway-token" {
			data = []byte(strings.Repeat("y", 43))
		}
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}))
		_, err := tw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.Equal(t, http.StatusConflict, post("/bootstrap", diffBuf.Bytes()))

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
