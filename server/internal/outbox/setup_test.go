package outbox_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("failed to launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Printf("cleanup failed: %v", err)
	}
	os.Exit(code)
}

type outboxTestInstance struct {
	conn *pgxpool.Pool
}

func newOutboxTestInstance(t *testing.T) *outboxTestInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	return &outboxTestInstance{conn: conn}
}
