package mcpregistry

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"log"
	"os"
	"testing"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	env, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatal(err)
	}
	infra = env
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatal(err)
	}
	os.Exit(code)
}

func newTestService(t *testing.T) (context.Context, *Service, *pgxpool.Pool) {
	t.Helper()

	db, err := infra.CloneTestDatabase(t, "registrytestdb")
	require.NoError(t, err)
	v, err := LoadValidator()
	require.NoError(t, err)
	return t.Context(), New(db, v), db
}
