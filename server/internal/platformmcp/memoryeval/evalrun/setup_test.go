package evalrun_test

import (
	"context"
	"log"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Postgres starts on first use, so the corpus and check tests run without it.
var (
	infraOnce    sync.Once
	infraEnv     *testenv.Environment
	infraCleanup func() error
	errInfra     error
)

// testInfra returns the shared Postgres environment, launching it once.
func testInfra(t *testing.T) *testenv.Environment {
	t.Helper()
	infraOnce.Do(func() {
		infraEnv, infraCleanup, errInfra = testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	})
	require.NoError(t, errInfra, "launch test infrastructure")
	return infraEnv
}

func TestMain(m *testing.M) {
	code := m.Run()

	if infraCleanup != nil {
		if err := infraCleanup(); err != nil {
			log.Fatalf("cleanup test infrastructure: %v", err)
		}
	}
	os.Exit(code)
}
