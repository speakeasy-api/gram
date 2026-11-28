package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// wireVersion is the version of the HTTP contract between the server and this init helper.
	wireVersion = 2

	// wireVersionHeader advertises wireVersion on every helper HTTP response so a
	// mismatched runtime image fails fast and clearly.
	wireVersionHeader = "X-ATE-Init-Version"

	// defaultListenAddr is the helper HTTP control address inside the actor sandbox.
	defaultListenAddr = ":8082"

	// maxBootstrapBytes bounds the JSON payload uploaded to POST /bootstrap.
	maxBootstrapBytes = 2 << 20 // 2 MiB

	// maxBootstrapEntries bounds the number of directories, files, or env entries in one bootstrap payload.
	maxBootstrapEntries = 64

	// maxEnvValueBytes bounds a single bootstrapped environment variable value.
	maxEnvValueBytes = 4 << 10 // 4 KiB
)

type bootstrapDir struct {
	Path string      `json:"path"`
	Mode os.FileMode `json:"mode"`
}

type bootstrapFile struct {
	Path    string      `json:"path"`
	Mode    os.FileMode `json:"mode"`
	Content []byte      `json:"content"`
}

type bootstrapPayload struct {
	Version int               `json:"version"`
	Dirs    []bootstrapDir    `json:"dirs"`
	Files   []bootstrapFile   `json:"files"`
	Env     map[string]string `json:"env,omitempty"`
}

type statusResponse struct {
	Version  int  `json:"version"`
	Draining bool `json:"draining"`
	Started  bool `json:"started"`
}

type config struct {
	rootDir   string
	probeAddr string
	listen    string
	command   []string
}

type initializer struct {
	rootDir   string
	probeAddr string

	mu          sync.Mutex
	started     bool
	launched    bool
	draining    bool
	hasBodyHash bool
	bodyHash    [32]byte
	env         map[string]string
	launch      chan struct{}
}

func (s *initializer) childEnv() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.env) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.env))
	for k, v := range s.env {
		out = append(out, k+"="+v)
	}
	return out
}

func (s *initializer) signalLaunchLocked() {
	if !s.launched {
		s.launched = true
		close(s.launch)
	}
}

func (s *initializer) markDraining() {
	s.mu.Lock()
	s.draining = true
	s.mu.Unlock()
}

func (s *initializer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(wireVersionHeader, fmt.Sprintf("%d", wireVersion))

	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/readyz":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == "/status":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(statusResponse{
			Version:  wireVersion,
			Draining: s.draining,
			Started:  s.started,
		})
	case r.Method == http.MethodGet && r.URL.Path == "/app-readyz":
		conn, err := net.DialTimeout("tcp", s.probeAddr, time.Second)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = conn.Close()
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Path == "/bootstrap":
		// Provisioning is reachable only through the authenticated ATE ingress.
		// On cold start, resume, or revert, PID 1 starts fresh and waits for
		// POST /bootstrap to rewrite provisioned files before launching the workload.
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBootstrapBytes))
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

func validateRelativePath(raw string) (string, error) {
	if raw == "" || strings.ContainsRune(raw, 0) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "\\") || filepath.IsAbs(raw) {
		return "", errors.New("path must be relative")
	}
	clean := filepath.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\\") {
		return "", errors.New("path escapes root")
	}
	for part := range strings.SplitSeq(clean, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("invalid path segment")
		}
	}
	return clean, nil
}

func validateMode(mode os.FileMode, defaultMode os.FileMode) (os.FileMode, error) {
	if mode == 0 {
		return defaultMode, nil
	}
	if mode&^os.ModePerm != 0 {
		return 0, errors.New("special permission bits are forbidden")
	}
	return mode, nil
}

// ensureNoSymlinks checks every existing path component from rootDir down to rel
// so neither directories nor files can follow a symlink out of rootDir.
func ensureNoSymlinks(rootDir, rel string) error {
	info, err := os.Lstat(rootDir)
	if err != nil {
		return fmt.Errorf("stat root directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("root directory must be a real directory")
	}
	cur := rootDir
	for part := range strings.SplitSeq(rel, "/") {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stat bootstrap path: %w", err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink path component rejected")
		}
	}
	return nil
}

func ensureDirUnderRoot(rootDir, rel string, mode os.FileMode) error {
	if err := ensureNoSymlinks(rootDir, rel); err != nil {
		return err
	}
	cur := rootDir
	for part := range strings.SplitSeq(rel, "/") {
		cur = filepath.Join(cur, part)
		if err := os.Mkdir(cur, mode); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create bootstrap directory: %w", err)
		}
		info, err := os.Lstat(cur)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("invalid bootstrap directory")
		}
	}
	return nil
}

func atomicWriteUnderRoot(rootDir, rel string, data []byte, mode os.FileMode) error {
	if err := ensureNoSymlinks(rootDir, rel); err != nil {
		return err
	}
	if parent := filepath.Dir(rel); parent != "." {
		if err := ensureDirUnderRoot(rootDir, parent, 0o700); err != nil {
			return err
		}
	}
	target := filepath.Join(rootDir, rel)
	if info, err := os.Lstat(target); err == nil {
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("bootstrap target must be a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat bootstrap target: %w", err)
	}

	f, err := os.CreateTemp(filepath.Dir(target), ".ate-init-*")
	if err != nil {
		return fmt.Errorf("create atomic temporary file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err := f.Chmod(mode); err != nil {
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
	if err := os.Rename(f.Name(), target); err != nil {
		return fmt.Errorf("install atomic file: %w", err)
	}
	return nil
}

// allowedBootstrapEnvKeys is the fixed allowlist of workload environment
// variable names accepted in POST /bootstrap. Process environment is held only
// in memory and never persisted to or loaded from the sandbox-writable root.
var allowedBootstrapEnvKeys = map[string]struct{}{
	"OPENROUTER_BASE_URL": {},
	"OPENROUTER_API_KEY":  {},
}

func validateBootstrapEnv(env map[string]string) (map[string]string, error) {
	if len(env) > len(allowedBootstrapEnvKeys) {
		return nil, errors.New("too many bootstrap environment variables")
	}
	if len(env) == 0 {
		return nil, nil
	}
	clean := make(map[string]string, len(env))
	for k, v := range env {
		if _, allowed := allowedBootstrapEnvKeys[k]; !allowed {
			return nil, fmt.Errorf("bootstrap environment key %q is not allowlisted", k)
		}
		if len(v) > maxEnvValueBytes || strings.ContainsRune(v, 0) {
			return nil, errors.New("invalid bootstrap environment entry")
		}
		clean[k] = v
	}
	return clean, nil
}

func (s *initializer) install(body []byte) error {
	var payload bootstrapPayload
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return fmt.Errorf("decode bootstrap payload: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing data in bootstrap payload")
	}
	if payload.Version != wireVersion {
		return fmt.Errorf("unsupported bootstrap version %d", payload.Version)
	}
	if len(payload.Dirs) > maxBootstrapEntries || len(payload.Files) == 0 || len(payload.Files) > maxBootstrapEntries {
		return errors.New("invalid bootstrap entry count")
	}
	cleanEnv, err := validateBootstrapEnv(payload.Env)
	if err != nil {
		return err
	}

	type cleanDir struct {
		rel  string
		mode os.FileMode
	}
	type cleanFile struct {
		rel     string
		mode    os.FileMode
		content []byte
	}

	seenDirs := make(map[string]struct{}, len(payload.Dirs))
	dirs := make([]cleanDir, 0, len(payload.Dirs))
	for _, d := range payload.Dirs {
		rel, err := validateRelativePath(d.Path)
		if err != nil {
			return errors.New("invalid bootstrap directory path")
		}
		if _, dup := seenDirs[rel]; dup {
			return errors.New("duplicate bootstrap directory")
		}
		mode, err := validateMode(d.Mode, 0o700)
		if err != nil {
			return err
		}
		if err := ensureNoSymlinks(s.rootDir, rel); err != nil {
			return err
		}
		seenDirs[rel] = struct{}{}
		dirs = append(dirs, cleanDir{rel: rel, mode: mode})
	}

	seenFiles := make(map[string]struct{}, len(payload.Files))
	files := make([]cleanFile, 0, len(payload.Files))
	totalBytes := 0
	for _, f := range payload.Files {
		rel, err := validateRelativePath(f.Path)
		if err != nil {
			return errors.New("invalid bootstrap file path")
		}
		if _, dup := seenFiles[rel]; dup {
			return errors.New("duplicate bootstrap file")
		}
		if _, isDir := seenDirs[rel]; isDir {
			return errors.New("bootstrap path cannot be both directory and file")
		}
		mode, err := validateMode(f.Mode, 0o600)
		if err != nil {
			return err
		}
		totalBytes += len(f.Content)
		if totalBytes > maxBootstrapBytes {
			return errors.New("bootstrap content exceeds size limit")
		}
		if err := ensureNoSymlinks(s.rootDir, rel); err != nil {
			return err
		}
		seenFiles[rel] = struct{}{}
		files = append(files, cleanFile{rel: rel, mode: mode, content: f.Content})
	}

	for _, d := range dirs {
		if err := ensureDirUnderRoot(s.rootDir, d.rel, d.mode); err != nil {
			return err
		}
	}
	for _, f := range files {
		if err := atomicWriteUnderRoot(s.rootDir, f.rel, f.content, f.mode); err != nil {
			return err
		}
	}
	s.env = cleanEnv
	return nil
}

func newInitializer(cfg config) (*initializer, error) {
	if !filepath.IsAbs(cfg.rootDir) {
		return nil, errors.New("root directory must be an absolute path")
	}
	if _, _, err := net.SplitHostPort(cfg.probeAddr); err != nil {
		return nil, fmt.Errorf("invalid probe address: %w", err)
	}

	return &initializer{
		rootDir:     filepath.Clean(cfg.rootDir),
		probeAddr:   cfg.probeAddr,
		mu:          sync.Mutex{},
		started:     false,
		launched:    false,
		draining:    false,
		hasBodyHash: false,
		bodyHash:    [32]byte{},
		env:         nil,
		launch:      make(chan struct{}),
	}, nil
}

// run manages bootstrap HTTP handling, worker-drain SIGTERM trapping, and the
// launched workload process. On SIGTERM (ATE worker drain), PID 1 intentionally
// stays alive and leaves the child running while advertising draining=true on
// GET /status so the harness can synchronously suspend the actor with a durable
// snapshot before the worker's shutdown budget elapses.
func run(ctx context.Context, cfg config, listener net.Listener, termCh <-chan os.Signal, childFactory func(context.Context) *exec.Cmd) error {
	state, err := newInitializer(cfg)
	if err != nil {
		return err
	}

	stopDrainWatch := make(chan struct{})
	defer close(stopDrainWatch)
	go func() {
		for {
			select {
			case <-termCh:
				state.markDraining()
			case <-stopDrainWatch:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	listenAddr := cfg.listen
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           state,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		MaxHeaderBytes:    8192,
	}
	errs := make(chan error, 2)
	go func() {
		if listener != nil {
			errs <- server.Serve(listener)
			return
		}
		errs <- server.ListenAndServe()
	}()
	defer func() { _ = server.Close() }()

	select {
	case <-ctx.Done():
		return fmt.Errorf("initialization canceled: %w", ctx.Err())
	case err := <-errs:
		return err
	case <-state.launch:
	}

	command := childFactory(ctx)
	if extraEnv := state.childEnv(); len(extraEnv) > 0 {
		if len(command.Env) == 0 {
			command.Env = append(os.Environ(), extraEnv...)
		} else {
			command.Env = append(command.Env, extraEnv...)
		}
	}
	go func() { errs <- command.Run() }()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		return fmt.Errorf("initialization canceled: %w", ctx.Err())
	}
}

func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("ate-init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var cfg config
	fs.StringVar(&cfg.rootDir, "root", "", "Absolute root directory that bootstrap files may be written under")
	fs.StringVar(&cfg.probeAddr, "probe-addr", "", "TCP host:port probed by GET /app-readyz")
	fs.StringVar(&cfg.listen, "listen", defaultListenAddr, "HTTP listen address for init endpoints")
	if err := fs.Parse(args); err != nil {
		return config{}, fmt.Errorf("parse init flags: %w", err)
	}
	cfg.command = fs.Args()
	if cfg.rootDir == "" || cfg.probeAddr == "" || len(cfg.command) == 0 {
		return config{}, errors.New("--root, --probe-addr, and a workload launch command are required")
	}
	return cfg, nil
}

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		log.Printf("ATE init configuration invalid: %v", err)
		os.Exit(1)
	}

	// Trap SIGTERM for ATE worker-drain eviction without exiting or killing the
	// child process. Only SIGINT cancels the process context.
	termCh := make(chan os.Signal, 1)
	signal.Notify(termCh, syscall.SIGTERM)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT)
	err = run(ctx, cfg, nil, termCh, func(cmdCtx context.Context) *exec.Cmd {
		//nolint:gosec // Launch command is supplied explicitly by the container image ENTRYPOINT/CMD.
		command := exec.CommandContext(cmdCtx, cfg.command[0], cfg.command[1:]...)
		command.Env = append(os.Environ(), "HOME="+cfg.rootDir)
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		return command
	})
	cancel()
	signal.Stop(termCh)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Print("ATE init stopped")
		os.Exit(1)
	}
}
