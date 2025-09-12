package deployments_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	packages "github.com/speakeasy-api/gram/server/internal/packages"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background())
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *deployments.Service
	feature        *feature.InMemory
	assets         *assets.Service
	packages       *packages.Service
	conn           *pgxpool.Pool
	temporal       client.Client
	sessionManager *sessions.Manager
}

func newTestDeploymentService(t *testing.T, assetStorage assets.BlobStore) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	f := &feature.InMemory{}

	temporal, devserver := infra.NewTemporalClient(t)
	worker := background.NewTemporalWorker(temporal, logger, meterProvider, background.ForDeploymentProcessing(conn, f, assetStorage))
	t.Cleanup(func() {
		worker.Stop()
		temporal.Close()
		require.NoError(t, devserver.Stop(), "shutdown temporal")
	})
	require.NoError(t, worker.Start(), "start temporal worker")

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-local"), "", billingClient)
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	svc := deployments.NewService(logger, tracerProvider, conn, temporal, sessionManager, assetStorage)
	assetsSvc := assets.NewService(logger, conn, sessionManager, assetStorage)
	packagesSvc := packages.NewService(logger, conn, sessionManager)

	return ctx, &testInstance{
		service:        svc,
		feature:        f,
		assets:         assetsSvc,
		packages:       packagesSvc,
		conn:           conn,
		temporal:       temporal,
		sessionManager: sessionManager,
	}
}

func zipManifest(t *testing.T, path string, runtime string) (rdr io.Reader, err error) {
	buf := &bytes.Buffer{}
	rdr = buf

	manifest := testenv.ReadFixture(t, path)
	zipWriter := zip.NewWriter(buf)
	defer o11y.LogDefer(t.Context(), testenv.NewLogger(t), func() error {
		return zipWriter.Close()
	})

	writer, err := zipWriter.Create("manifest.json")
	if err != nil {
		return nil, fmt.Errorf("create manifest in zip: %w", err)
	}

	_, err = writer.Write(manifest)
	if err != nil {
		return nil, fmt.Errorf("write manifest to zip: %w", err)
	}

	var funcwriter io.Writer
	var comment string
	switch {
	case strings.HasPrefix(runtime, "nodejs"):
		comment = "// JavaScript functions"
		if funcwriter, err = zipWriter.Create("functions.js"); err != nil {
			return nil, fmt.Errorf("create functions.js in zip: %w", err)
		}
	case strings.HasPrefix(runtime, "python"):
		comment = "# Python functions"
		if funcwriter, err = zipWriter.Create("functions.py"); err != nil {
			return nil, fmt.Errorf("create functions.py in zip: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported runtime: %s", runtime)
	}

	// Create an empty functions file with a comment so the file exists. It does
	// not need to have any actual code when testing deployments.
	_, err = funcwriter.Write([]byte(comment + "\n"))
	if err != nil {
		return nil, fmt.Errorf("write functions to zip: %w", err)
	}

	return buf, nil
}
