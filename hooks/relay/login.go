package relay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLoginTimeout  = 240 * time.Second
	defaultLoginCooldown = 6 * time.Hour
)

// loginFlow runs the browser-based device sign-in: it serves a one-shot
// localhost callback, opens the Gram dashboard pointed at it, and caches the
// hooks key the dashboard returns. It replaces the legacy nc/mkfifo listener
// with a net/http server so no external tools are required.
type loginFlow struct {
	cfg      Config
	timeout  time.Duration
	cooldown time.Duration
}

func newLoginFlow(cfg Config) *loginFlow {
	return &loginFlow{
		cfg:      cfg,
		timeout:  envDuration("GRAM_HOOKS_LOGIN_TIMEOUT_SECONDS", defaultLoginTimeout),
		cooldown: envDuration("GRAM_HOOKS_LOGIN_COOLDOWN_SECONDS", defaultLoginCooldown),
	}
}

// tryInteractive runs a best-effort sign-in for the SessionStart preflight:
// guards and cooldown suppress it silently, and any failure is non-fatal.
func (l *loginFlow) tryInteractive(ctx context.Context) {
	if insecureServerURL(l.cfg.ServerURL) {
		return
	}
	if ok, _ := loginViable(); !ok {
		return
	}
	if !l.cooldownElapsed(loginForced()) {
		return
	}
	_ = l.run(ctx, loginForced())
}

// Run performs an explicit sign-in (the login subcommand). force re-mints even
// when a credential is already cached and bypasses the cooldown.
func (l *loginFlow) Run(ctx context.Context, force bool) error {
	if _, ok := resolveAuth(l.cfg); ok && !force {
		return nil
	}
	// A key minted for a plaintext non-loopback server would be refused by
	// every send; don't open a browser to it in the first place.
	if insecureServerURL(l.cfg.ServerURL) {
		return fmt.Errorf("refusing insecure Gram server URL %q; use https:// (or an http://localhost dev server)", l.cfg.ServerURL)
	}
	if ok, reason := loginViable(); !ok {
		return fmt.Errorf("browser sign-in is unavailable: %s", reason)
	}
	if force {
		forgetAuth()
	}
	return l.run(ctx, true)
}

// run serves the localhost callback, opens the browser, and waits for the
// dashboard to deliver the minted key.
func (l *loginFlow) run(ctx context.Context, force bool) error {
	l.markAttempt()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("open callback listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	state, err := randomToken()
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("generate state token: %w", err)
	}

	resultCh := make(chan creds, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/gram-probe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/callback", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		apiKey := strings.TrimSpace(q.Get("api_key"))
		if apiKey == "" {
			http.Error(w, "missing api_key", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><meta charset=utf-8><title>Speakeasy</title><body style=\"font-family:sans-serif\">Speakeasy hooks are connected. You can close this tab.</body>"))
		select {
		case resultCh <- creds{
			ServerURL: l.cfg.ServerURL,
			APIKey:    apiKey,
			Project:   firstNonEmpty(q.Get("project"), l.cfg.ProjectSlug),
			Email:     q.Get("email"),
			Org:       firstNonEmpty(q.Get("organization_id"), l.cfg.OrgID),
			Source:    credCache,
		}:
		default:
		}
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	authURL := l.dashboardURL(port, state)
	openBrowser(authURL)
	fmt.Fprintf(os.Stderr, "Speakeasy hooks: complete sign-in in your browser.\nIf it did not open, visit:\n  %s\n", authURL)

	waitCtx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	select {
	case c := <-resultCh:
		if err := writeAuth(c); err != nil {
			return fmt.Errorf("cache credentials: %w", err)
		}
		l.clearAttempt()
		fmt.Fprintln(os.Stderr, "Speakeasy hooks: connected.")
		return nil
	case <-waitCtx.Done():
		return errors.New("timed out waiting for browser sign-in")
	}
}

// dashboardURL builds the Gram sign-in URL pointed at the localhost callback.
func (l *loginFlow) dashboardURL(port int, state string) string {
	callback := fmt.Sprintf("http://127.0.0.1:%d/callback?state=%s", port, url.QueryEscape(state))
	q := url.Values{}
	q.Set("from_cli", "true")
	q.Set("cli_callback_url", callback)
	q.Set("key_scope", "hooks")
	if l.cfg.ProjectSlug != "" {
		q.Set("project", l.cfg.ProjectSlug)
	}
	if l.cfg.OrgID != "" {
		q.Set("organization_id", l.cfg.OrgID)
	}
	return strings.TrimRight(l.cfg.ServerURL, "/") + "/?" + q.Encode()
}

// cooldownElapsed reports whether enough time has passed since the last sign-in
// attempt to try again. force always returns true.
func (l *loginFlow) cooldownElapsed(force bool) bool {
	if force {
		return true
	}
	info, err := os.Stat(l.attemptMarker())
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) >= l.cooldown
}

func (l *loginFlow) attemptMarker() string { return authFilePath() + ".login-attempt" }

func (l *loginFlow) markAttempt() {
	path := l.attemptMarker()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	_ = f.Close()
	_ = os.Chtimes(path, time.Now(), time.Now())
}

func (l *loginFlow) clearAttempt() { _ = os.Remove(l.attemptMarker()) }

// loginViable reports whether an interactive browser sign-in can run here.
func loginViable() (bool, string) {
	if disableLocalAuth() {
		return false, "local sign-in disabled"
	}
	if os.Getenv("CI") != "" || os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false, "non-interactive environment"
	}
	// Only X11/Wayland platforms signal a display through the environment;
	// macOS and Windows desktop sessions always have one.
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false, "no display available"
	}
	return true, ""
}

func disableLocalAuth() bool {
	return os.Getenv("GRAM_HOOKS_DISABLE_LOCAL_AUTH") == "1"
}

func loginForced() bool {
	return os.Getenv("GRAM_HOOKS_LOGIN_FORCE") == "1"
}

// openBrowser launches the platform browser opener, ignoring failures (the URL
// is also printed to stderr as a fallback). It is a var so tests can drive the
// callback without a real browser.
var openBrowser = func(target string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	_ = cmd.Start()
}

func randomToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func envDuration(name string, fallback time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return fallback
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
