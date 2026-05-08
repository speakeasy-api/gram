package toolsets_test

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
	dgen "github.com/speakeasy-api/gram/server/gen/deployments"
	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/externalmcptest"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functionstest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	packages "github.com/speakeasy-api/gram/server/internal/packages"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, Temporal: true, ClickHouse: true})
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
	service        *toolsets.Service
	feature        *feature.InMemory
	deployments    *deployments.Service
	assets         *assets.Service
	packages       *packages.Service
	conn           *pgxpool.Pool
	temporalEnv    *temporal.Environment
	sessionManager *sessions.Manager
	assetStorage   assets.BlobStore
}

func newTestToolsetsService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	// Create a test blob store for testing
	assetStorage := assetstest.NewTestBlobStore(t)

	enc := testenv.NewEncryptionClient(t)
	funcs := functionstest.NewOrchestrator(t, assetStorage)
	mcpRegistryClient := externalmcptest.NewRegistryClient(t, logger, tracerProvider)
	temporalEnv, _ := infra.NewTemporalEnv(t)
	auditLogger := audit.NewLogger()
	f := &feature.InMemory{}

	worker := background.NewTemporalWorker(temporalEnv, logger, tracerProvider, meterProvider, background.ForDeploymentProcessing(guardianPolicy, conn, f, assetStorage, enc, funcs, mcpRegistryClient, auditLogger))
	t.Cleanup(func() {
		worker.Stop()
	})
	require.NoError(t, worker.Start(), "start temporal worker")

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	svc := toolsets.NewService(logger, tracerProvider, conn, sessionManager, nil, authzEngine, auditLogger)
	deploymentsSvc := deployments.NewService(logger, tracerProvider, conn, temporalEnv, sessionManager, assetStorage, posthog, testenv.DefaultSiteURL(t), mcpRegistryClient, authzEngine, auditLogger)
	assetsSvc := assets.NewService(logger, tracerProvider, guardianPolicy, conn, sessionManager, chatSessionsManager, assetStorage, "test-jwt-secret", authzEngine, auditLogger)
	packagesSvc := packages.NewService(logger, tracerProvider, conn, sessionManager, authzEngine)

	return ctx, &testInstance{
		service:        svc,
		feature:        f,
		deployments:    deploymentsSvc,
		assets:         assetsSvc,
		packages:       packagesSvc,
		conn:           conn,
		temporalEnv:    temporalEnv,
		sessionManager: sessionManager,
		assetStorage:   assetStorage,
	}
}

func createPetstoreDeployment(t *testing.T, ctx context.Context, ti *testInstance) *dgen.CreateDeploymentResult {
	t.Helper()

	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))

	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload petstore openapi v3 asset")

	dep, err := ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey: "test-petstore-idempotency-key",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "petstore-doc",
				Slug:    "petstore-doc",
			},
		},
		Functions:        []*dgen.AddFunctionsForm{},
		Packages:         []*dgen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create petstore deployment")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

	return dep
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

func createFunctionsDeployment(t *testing.T, ctx context.Context, ti *testInstance) *dgen.CreateDeploymentResult {
	t.Helper()

	// Upload functions file
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:24")

	dep, err := ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey:  "test-functions-toolset",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*dgen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "test-functions",
				Slug:    "test-functions",
				Runtime: "nodejs:24",
			},
		},
		Packages:         []*dgen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create functions deployment")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

	return dep
}

func createFunctionsDeploymentWithResources(t *testing.T, ctx context.Context, ti *testInstance) *dgen.CreateDeploymentResult {
	t.Helper()

	// Upload functions file with resources in manifest
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-with-resources.json", "nodejs:24")

	dep, err := ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey:  "test-functions-with-resources",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*dgen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "test-functions-with-resources",
				Slug:    "test-functions-with-resources",
				Runtime: "nodejs:24",
			},
		},
		Packages:         []*dgen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create functions deployment with resources")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

	return dep
}

func withProAccount(t *testing.T, ctx context.Context) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	authCtx.AccountType = "pro"

	return contextvalues.SetAuthContext(ctx, authCtx)
}

func createMinimalPrivateToolset(t *testing.T, ctx context.Context, ti *testInstance, name string) *types.Toolset {
	t.Helper()

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Name:                   name,
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	return created
}

func createMinimalPublicToolset(t *testing.T, ctx context.Context, ti *testInstance, name string) *types.Toolset {
	t.Helper()

	created := createMinimalPrivateToolset(t, ctx, ti, name)
	public := true

	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            &public,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.McpIsPublic)
	require.True(t, *updated.McpIsPublic)

	return updated
}
