package memory

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var memoryInfra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres:   true,
		Redis:      false,
		ClickHouse: false,
		Temporal:   false,
		Presidio:   false,
	})
	if err != nil {
		log.Fatalf("launch memory test infrastructure: %v", err)
	}
	memoryInfra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup memory test infrastructure: %v", err)
	}
	os.Exit(code)
}
