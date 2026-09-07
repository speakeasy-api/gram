package chrepo_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{ClickHouse: true})
	if err != nil {
		log.Fatalf("launch metering ClickHouse infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup metering ClickHouse infrastructure: %v", err)
	}
	os.Exit(code)
}

func newTestClickhouse(t *testing.T) clickhouse.Conn {
	t.Helper()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	return conn
}
