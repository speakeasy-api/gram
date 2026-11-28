package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
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

const (
	// home is the Kit actor's persistent home directory inside the sandbox rootfs.
	home = "/home/kit"

	// maxBootstrap bounds the tar archive uploaded to POST /bootstrap.
	maxBootstrap = 2 << 20 // 2 MiB

	// minGatewayTokenBytes enforces minimum entropy on the per-actor ACP bearer token.
	minGatewayTokenBytes = 32
)

// bootstrapFiles lists the exact archive entries accepted by POST /bootstrap:
// non-secret Kit config plus the per-actor ACP server credential file.
// Speakeasy API credentials are never delivered into the sandbox; ATE's egress
// gateway injects the actor JWT on outbound requests.
var bootstrapFiles = []string{
	".kit/AGENTS.md",
	".kit/config.toml",
	".kit/credentials/mcp.json",
	".kit/credentials/gateway-token",
}

type initializer struct {
	home        string
	mu          sync.Mutex
	started     bool
	launched    bool
	hasBodyHash bool
	bodyHash    [32]byte
	launch      chan struct{}
}

func (s *initializer) signalLaunchLocked() {
	if !s.launched {
		s.launched = true
		close(s.launch)
	}
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
		// Initial write-once provisioning is reachable only through the authenticated ATE ingress.
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBootstrap))
		if err != nil {
			http.Error(w, "invalid bootstrap", http.StatusBadRequest)
			return
		}
		sum := sha256.Sum256(body)
		if s.started {
			if s.hasBodyHash && sum == s.bodyHash {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusConflict)
			return
		}
		if s.install(body) != nil {
			http.Error(w, "invalid bootstrap", http.StatusBadRequest)
			return
		}
		s.started = true
		s.bodyHash = sum
		s.hasBodyHash = true
		s.signalLaunchLocked()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create atomic file directory: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ate-init-*")
	if err != nil {
		return fmt.Errorf("create atomic temporary file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err := f.Chmod(0o600); err != nil {
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
	if len(files[".kit/credentials/gateway-token"]) < minGatewayTokenBytes || bytes.ContainsAny(files[".kit/credentials/gateway-token"], " \r\n\x00") {
		return errors.New("invalid token")
	}
	for _, dir := range []string{".kit", ".kit/credentials", "workspace"} {
		path := filepath.Join(s.home, dir)
		if err := os.MkdirAll(path, 0o700); err != nil {
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

func newInitializer(homeDir string) (*initializer, error) {
	state := &initializer{
		home:        homeDir,
		mu:          sync.Mutex{},
		started:     false,
		launched:    false,
		hasBodyHash: false,
		bodyHash:    [32]byte{},
		launch:      make(chan struct{}),
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".kit/ate-initialized")); err == nil {
		// Resuming from a durable rootfs snapshot: config is already on disk,
		// so launch Kit immediately without waiting for POST /bootstrap.
		state.started = true
		state.signalLaunchLocked()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("check initialization marker: %w", err)
	}
	return state, nil
}

func run(ctx context.Context) error {
	state, err := newInitializer(home)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              ":8082",
		Handler:           state,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		MaxHeaderBytes:    8192,
	}
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
