package authz_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	pgContainer, cloneFunc, err := testenv.NewTestPostgres(ctx)
	if err != nil {
		log.Fatalf("launch test postgres: %v", err)
	}
	chContainer, chFactory, err := testenv.NewTestClickhouse(ctx)
	if err != nil {
		_ = pgContainer.Terminate(ctx)
		log.Fatalf("launch test clickhouse: %v", err)
	}
	authz.SetTestInfrastructure(authz.TestInfrastructure{
		ClonePostgres:       cloneFunc,
		NewClickhouseClient: chFactory,
		NewLogger:           testenv.NewLogger,
		NewMeterProvider:    testenv.NewMeterProvider,
		BeginTx:             testenv.BeginTx,
	})
	code := m.Run()
	if err := chContainer.Terminate(ctx); err != nil {
		log.Fatalf("terminate clickhouse container: %v", err)
	}
	if err := pgContainer.Terminate(ctx); err != nil {
		log.Fatalf("terminate postgres container: %v", err)
	}
	os.Exit(code)
}
