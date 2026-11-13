package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const home = "/home/kit"
const assertionPath = "/var/run/speakeasy/workload/assertion"
const maxBootstrap = 2 << 20

var bootstrapFiles = []string{".kit/AGENTS.md", ".kit/config.toml", ".kit/credentials/mcp.json", ".kit/credentials/gateway-token"}

type initializer struct {
	home      string
	assertion string
	mu        sync.Mutex
	started   bool
	launched  bool
	launch    chan struct{}
}

func (s *initializer) token() ([]byte, error) {
	token, err := os.ReadFile(filepath.Join(s.home, ".kit/credentials/gateway-token"))
	if err != nil {
		return nil, fmt.Errorf("read gateway credential: %w", err)
	}
	return token, nil
}

func (s *initializer) authorized(r *http.Request) bool {
	token, err := s.token()
	return err == nil && len(token) > 0 && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), append([]byte("Bearer "), token...)) == 1
}

func (s *initializer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/readyz":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == "/kit-readyz":
		conn, err := net.DialTimeout("tcp", "127.0.0.1:8081", time.Second)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = conn.Close()
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Path == "/bootstrap":
		// Initial provisioning is reachable only through the authenticated ATE ingress.
		if s.started {
			w.WriteHeader(http.StatusConflict)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBootstrap))
		if err != nil || s.install(body) != nil {
			http.Error(w, "invalid bootstrap", http.StatusBadRequest)
			return
		}
		s.started = true
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/assertion":
		if !s.started || !s.authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
		if err != nil || len(body) == 0 || bytes.ContainsAny(body, "\r\n\x00") {
			http.Error(w, "invalid assertion", http.StatusBadRequest)
			return
		}
		if err := atomicWrite(s.assertion, body); err != nil {
			http.Error(w, "assertion write failed", http.StatusInternalServerError)
			return
		}
		if !s.launched {
			s.launched = true
			close(s.launch)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create atomic file directory: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ate-init-*")
	if err != nil {
		return fmt.Errorf("create atomic temporary file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return fmt.Errorf("set atomic file permissions: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write atomic file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync atomic file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close atomic file: %w", err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("install atomic file: %w", err)
	}
	return nil
}

func (s *initializer) install(body []byte) error {
	reader := tar.NewReader(bytes.NewReader(body))
	files := make(map[string][]byte)
	for {
		entry, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read bootstrap archive header: %w", err)
		}
		if entry.Typeflag == tar.TypeDir && (entry.Name == ".kit/" || entry.Name == ".kit/credentials/" || entry.Name == "workspace/") {
			continue
		}
		allowed := false
		for _, name := range bootstrapFiles {
			if entry.Name == name {
				allowed = true
			}
		}
		if !allowed || entry.Typeflag != tar.TypeReg || entry.Size < 0 || entry.Size > maxBootstrap {
			return errors.New("invalid archive entry")
		}
		if _, exists := files[entry.Name]; exists {
			return errors.New("duplicate archive entry")
		}
		data, err := io.ReadAll(io.LimitReader(reader, maxBootstrap+1))
		if err != nil || int64(len(data)) != entry.Size {
			return errors.New("invalid archive data")
		}
		files[entry.Name] = data
	}
	if len(files) != len(bootstrapFiles) {
		return errors.New("incomplete bootstrap")
	}
	if len(files[".kit/credentials/gateway-token"]) < 32 || bytes.ContainsAny(files[".kit/credentials/gateway-token"], " \r\n\x00") {
		return errors.New("invalid token")
	}
	for _, dir := range []string{".kit", ".kit/credentials", "workspace"} {
		path := filepath.Join(s.home, dir)
		if err := os.MkdirAll(path, 0700); err != nil {
			return fmt.Errorf("create bootstrap directory: %w", err)
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("invalid bootstrap directory")
		}
	}
	for _, name := range bootstrapFiles {
		if err := atomicWrite(filepath.Join(s.home, name), files[name]); err != nil {
			return err
		}
	}
	return atomicWrite(filepath.Join(s.home, ".kit/ate-initialized"), []byte("1"))
}

func run(ctx context.Context) error {
	state := new(initializer)
	state.home = home
	state.assertion = assertionPath
	state.launch = make(chan struct{})
	if _, err := os.Stat(filepath.Join(home, ".kit/ate-initialized")); err == nil {
		state.started = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check initialization marker: %w", err)
	}
	server := &http.Server{Addr: ":8082", Handler: state, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, MaxHeaderBytes: 8192}
	errs := make(chan error, 2)
	go func() { errs <- server.ListenAndServe() }()
	defer func() { _ = server.Close() }()
	select {
	case <-ctx.Done():
		return fmt.Errorf("initialization canceled: %w", ctx.Err())
	case err := <-errs:
		return err
	case <-state.launch:
	}
	command := exec.CommandContext(ctx, "/usr/local/bin/kit", "serve", "--root", home+"/workspace", "--remote-acp", "--no-a2a", "--no-stdio", "--a2a", "0.0.0.0:8081", "--server-credential-file", home+"/.kit/credentials/gateway-token")
	command.Env = append(os.Environ(), "HOME="+home)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	go func() { errs <- command.Run() }()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		return fmt.Errorf("initialization canceled: %w", ctx.Err())
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	err := run(ctx)
	cancel()
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Print("ATE Kit initialization stopped")
		os.Exit(1)
	}
}
