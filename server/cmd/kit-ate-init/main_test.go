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
		if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestBootstrapAndAssertion(t *testing.T) {
	t.Parallel()
	s := new(initializer)
	s.home = t.TempDir()
	s.assertion = filepath.Join(t.TempDir(), "assertion")
	s.launch = make(chan struct{})
	post := func(path, token string, body []byte) int {
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	archive := testArchive(t, bootstrapFiles)
	if code := post("/bootstrap", "", archive); code != http.StatusNoContent {
		t.Fatalf("bootstrap status %d", code)
	}
	select {
	case <-s.launch:
		t.Fatal("Kit launched before receiving a fresh assertion")
	default:
	}
	if code := post("/bootstrap", "", archive); code != http.StatusConflict {
		t.Fatalf("overwrite status %d", code)
	}
	if code := post("/assertion", "wrong", []byte("jwt.fixture")); code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status %d", code)
	}
	if code := post("/assertion", strings.Repeat("x", 43), []byte("jwt.fixture")); code != http.StatusNoContent {
		t.Fatalf("assertion status %d", code)
	}
	select {
	case <-s.launch:
	default:
		t.Fatal("Kit was not released after assertion installation")
	}
	data, err := os.ReadFile(s.assertion)
	if err != nil || string(data) != "jwt.fixture" {
		t.Fatal("assertion was not installed")
	}
	info, err := os.Stat(s.assertion)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("assertion must be private")
	}
	restored := new(initializer)
	restored.home = s.home
	restored.assertion = s.assertion
	request := httptest.NewRequest(http.MethodPost, "/assertion", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 43))
	if !restored.authorized(request) {
		t.Fatal("durable credential was not restored")
	}
	if _, err := os.Stat(filepath.Join(s.home, ".kit/ate-initialized")); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapRejectsTraversalAndSymlinks(t *testing.T) {
	t.Parallel()
	s := new(initializer)
	s.home = t.TempDir()
	if err := s.install(testArchive(t, append(append([]string{}, bootstrapFiles...), "../outside"))); err == nil {
		t.Fatal("path traversal accepted")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.home, ".kit")); err != nil {
		t.Fatal(err)
	}
	if err := s.install(testArchive(t, bootstrapFiles)); err == nil {
		t.Fatal("symlink directory accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("bootstrap escaped HOME")
	}
}
