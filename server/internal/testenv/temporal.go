package testenv

import (
	"context"
	"fmt"
	"io"
	"testing"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
)

func NewTemporalDevServer(t *testing.T, ctx context.Context) (*testsuite.DevServer, error) {
	t.Helper()

	var stdout io.Writer
	var stderr io.Writer
	if !testing.Verbose() {
		stdout = io.Discard
		stderr = io.Discard
	}

	var temporal *testsuite.DevServer
	var err error
	logger := NewLogger(t)

	for range 5 {
		temporal, err = testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
			LogLevel: "error",
			ClientOptions: &client.Options{
				Namespace: fmt.Sprintf("test_%s", nextRandom()),
				Logger:    logger,
			},
			Stdout: stdout,
			Stderr: stderr,
		})
		if err == nil {
			break
		}
	}

	if err != nil {
		return nil, fmt.Errorf("start temporal dev server: %w", err)
	}

	return temporal, nil
}
