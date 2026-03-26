package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
)

// Dispatcher handles opening the auth URL in a browser.
type Dispatcher struct {
	logger *slog.Logger
}

// NewDispatcher creates a new auth dispatcher.
func NewDispatcher(logger *slog.Logger) *Dispatcher {
	return &Dispatcher{logger: logger}
}

// Dispatch opens the authentication URL in the user's browser,
// or prints it if the browser cannot be launched.
func (d *Dispatcher) Dispatch(
	ctx context.Context,
	webAppURL string,
	callbackURL string,
) error {
	authURL, err := buildAuthURL(webAppURL, callbackURL)
	if err != nil {
		return fmt.Errorf("failed to build auth URL: %w", err)
	}

	if canOpenBrowser() {
		if err := openBrowser(authURL); err != nil {
			d.logger.WarnContext(
				ctx,
				"failed to open browser, please visit URL manually",
				slog.String("error", err.Error()),
			)
			fmt.Printf("\nPlease visit: %s\n\n", authURL)
		}
	} else {
		fmt.Printf("\nPlease visit: %s\n\n", authURL)
	}

	return nil
}

func buildAuthURL(webAppURL, callbackURL string) (string, error) {
	parsed, err := url.Parse(webAppURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse web app URL: %w", err)
	}

	q := parsed.Query()
	q.Set("from_cli", "true")
	q.Set("cli_callback_url", callbackURL)
	parsed.RawQuery = q.Encode()

	return parsed.String(), nil
}

func canOpenBrowser() bool {
	_, err := exec.LookPath("open")
	return err == nil
}

func openBrowser(url string) error {
	cmd := exec.Command("open", url) //#nosec G204 -- url is a constructed auth URL, not user-controlled shell input
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start browser: %w", err)
	}
	return nil
}
