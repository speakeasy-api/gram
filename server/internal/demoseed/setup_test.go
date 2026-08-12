// The demo seed safety test runs as its own CI job (demo-seed-safety in
// pr.yaml), not inside the sharded server test suite — hence the build tag.
// Run locally with:
//
//	mise run test:server -tags=demoseed_safety ./internal/demoseed/...
//
//go:build demoseed_safety

package demoseed

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres:   true,
		ClickHouse: true,
	})
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
