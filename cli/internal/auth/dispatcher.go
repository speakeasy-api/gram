package auth

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
)

// ErrNoBrowser reports that no browser opener is available on this machine.
var ErrNoBrowser = errors.New("no browser opener available")

// BrowserOpener opens target, a local file path or URL, in the user's browser.
type BrowserOpener func(target string) error

// Dispatcher handles opening the auth URL in a browser.
type Dispatcher struct {
	logger *slog.Logger
	open   BrowserOpener
}

// NewDispatcher creates a new auth dispatcher that opens the auth URL with
// open. Pass OpenSystemBrowser outside of tests.
func NewDispatcher(logger *slog.Logger, open BrowserOpener) *Dispatcher {
	return &Dispatcher{logger: logger, open: open}
}

// Dispatch opens the authentication URL in the user's browser, or prints it if
// the browser cannot be launched. The URL asks the dashboard for a one-time
// producer key code bound to codeChallenge. The returned cleanup removes the
// redirect file handed to the browser; call it once the login attempt is over.
func (d *Dispatcher) Dispatch(
	ctx context.Context,
	webAppURL string,
	callbackURL string,
	codeChallenge string,
) (cleanup func(), err error) {
	authURL, err := buildAuthURL(webAppURL, callbackURL, codeChallenge)
	if err != nil {
		return func() {}, fmt.Errorf("failed to build auth URL: %w", err)
	}

	// The auth URL carries the callback's state token, and process arguments
	// are readable by every local account, so the browser is handed a
	// user-only redirect file instead of the URL itself.
	redirectPath, err := writeRedirectFile(authURL)
	if err != nil {
		d.logger.WarnContext(
			ctx,
			"failed to prepare browser redirect, please visit URL manually",
			slog.String("error", err.Error()),
		)
		fmt.Printf("\nPlease visit: %s\n\n", authURL)
		return func() {}, nil
	}
	cleanup = func() { _ = os.Remove(redirectPath) }

	err = d.open(redirectPath)
	switch {
	case err == nil:
		// The system may hand an .html file to an app other than the
		// browser, so the URL is always printed as a fallback.
		fmt.Printf("\nIf your browser did not open, visit: %s\n\n", authURL)
	case errors.Is(err, ErrNoBrowser):
		fmt.Printf("\nPlease visit: %s\n\n", authURL)
	default:
		d.logger.WarnContext(
			ctx,
			"failed to open browser, please visit URL manually",
			slog.String("error", err.Error()),
		)
		fmt.Printf("\nPlease visit: %s\n\n", authURL)
	}

	return cleanup, nil
}

func buildAuthURL(webAppURL, callbackURL, codeChallenge string) (string, error) {
	parsed, err := url.Parse(webAppURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse web app URL: %w", err)
	}

	q := parsed.Query()
	q.Set("from_cli", "true")
	q.Set("cli_callback_url", callbackURL)
	q.Set("key_scope", "producer")
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", pkceMethod)
	parsed.RawQuery = q.Encode()

	return parsed.String(), nil
}

// writeRedirectFile writes an HTML page that forwards the browser to authURL.
// os.CreateTemp creates the file with mode 0600, so other local users cannot
// read the URL from it.
func writeRedirectFile(authURL string) (string, error) {
	f, err := os.CreateTemp("", "speakeasy-login-*.html")
	if err != nil {
		return "", fmt.Errorf("create redirect file: %w", err)
	}

	escaped := html.EscapeString(authURL)
	page := `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=` +
		escaped + `"><title>Speakeasy</title><body style="font-family:sans-serif">Redirecting to <a href="` +
		escaped + `">Speakeasy sign-in</a>&hellip;</body>`

	if _, err := f.WriteString(page); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write redirect file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("close redirect file: %w", err)
	}

	return f.Name(), nil
}

// OpenSystemBrowser opens target with the macOS `open` command. It returns
// ErrNoBrowser when `open` is not installed.
func OpenSystemBrowser(target string) error {
	if _, err := exec.LookPath("open"); err != nil {
		return ErrNoBrowser
	}

	cmd := exec.Command("open", target) //#nosec G204 -- target is a redirect file path this process created, not user-controlled shell input
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start browser: %w", err)
	}
	return nil
}
