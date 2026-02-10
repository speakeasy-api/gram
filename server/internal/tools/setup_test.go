package tools_test

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

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	packages "github.com/speakeasy-api/gram/server/internal/packages"
	"github.com/speakeasy-api/gram/server/internal/templates"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/tools"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
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
	service        *tools.Service
	feature        *feature.InMemory
	deployments    *deployments.Service
	assets         *assets.Service
	packages       *packages.Service
	templates      *templates.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
}

func newTestToolsService(t *testing.T, assetStorage assets.BlobStore) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-local"), "", billingClient)
	require.NoError(t, err)

	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	enc := testenv.NewEncryptionClient(t)
	funcs := testenv.NewFunctionsTestOrchestrator(t)
	mcpRegistryClient := testenv.NewMCPRegistryClient(t, logger, tracerProvider)

	f := &feature.InMemory{}

	temporal := infra.NewTemporalClient(t)
	worker := background.NewTemporalWorker(temporal, logger, tracerProvider, meterProvider, background.ForDeploymentProcessing(conn, f, assetStorage, enc, funcs, mcpRegistryClient))
	t.Cleanup(func() {
		worker.Stop()
		temporal.Close()
	})
	require.NoError(t, worker.Start(), "start temporal worker")

	toolsSvc := tools.NewService(logger, conn, sessionManager)
	deploymentsSvc := deployments.NewService(logger, tracerProvider, conn, temporal, sessionManager, assetStorage, posthog, testenv.DefaultSiteURL(t), mcpRegistryClient)
	assetsSvc := assets.NewService(logger, conn, sessionManager, chatSessionsManager, assetStorage, "test-jwt-secret")
	packagesSvc := packages.NewService(logger, conn, sessionManager)
	toolsetsSvc := toolsets.NewService(logger, conn, sessionManager, cache.NewRedisCacheAdapter(redisClient))
	templatesSvc := templates.NewService(logger, conn, sessionManager, toolsetsSvc)

	return ctx, &testInstance{
		service:        toolsSvc,
		feature:        f,
		deployments:    deploymentsSvc,
		assets:         assetsSvc,
		packages:       packagesSvc,
		templates:      templatesSvc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

// zipManifest creates a zip file containing a manifest.json and a stub functions file.
func zipManifest(t *testing.T, path string, runtime string) (rdr io.Reader, err error) {
	t.Helper()

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

// uploadFunctionsWithManifest uploads a functions zip file with the given manifest.
func uploadFunctionsWithManifest(t *testing.T, ctx context.Context, assetsService *assets.Service, manifestPath, runtime string) *agen.UploadFunctionsResult {
	t.Helper()

	// Create functions zip with manifest
	zipReader, err := zipManifest(t, manifestPath, runtime)
	require.NoError(t, err, "failed to create functions zip with manifest")

	// Read the zip content
	zipBytes, err := io.ReadAll(zipReader)
	require.NoError(t, err, "failed to read zip content")

	result, err := assetsService.UploadFunctions(ctx, &agen.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/zip",
		ContentLength:    int64(len(zipBytes)),
	}, io.NopCloser(bytes.NewBuffer(zipBytes)))
	require.NoError(t, err, "failed to upload functions")

	return result
}
