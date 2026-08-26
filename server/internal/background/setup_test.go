package background

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	// Temporal is the only capability this package's tests need, and the dev
	// server behind it starts lazily on the first NewTemporalEnv call, so the
	// workflow tests that run purely in-process pay nothing for it.
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Temporal: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}
