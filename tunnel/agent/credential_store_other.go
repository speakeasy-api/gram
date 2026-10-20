//go:build !linux

package agent

import (
	"context"
	"errors"
	"log/slog"
)

// Credentials mode needs Linux: memory-backed filesystem checks and
// descriptor-relative file handling are implemented for Linux only.
const credentialsSupported = false

func openCredentialStore(context.Context, string, *slog.Logger) (credentialStore, error) {
	return nil, errors.New("stdio credentials mode requires a Linux tunnel agent")
}
