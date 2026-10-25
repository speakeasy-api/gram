package assistant_platform_mcp_adapter

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var adapterInfra *testenv.Environment

func TestMain(m *testing.M) {
	infra, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	adapterInfra = infra

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}
