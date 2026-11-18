package deployments_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestCreateDeployment_FunctionsRefusedWhenFunctionsDisabled(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentServiceWithFunctions(t, assetStorage, functions.Disabled{}, false)

	_, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "functions-disabled-create",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: uuid.NewString(),
				Name:    "refused-functions",
				Slug:    "refused-functions",
				Runtime: "nodejs:24",
			},
		},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotImplemented, oopsErr.Code)
	require.ErrorIs(t, err, functions.ErrFunctionsDisabled)
}

func TestEvolve_UpsertFunctionsRefusedWhenFunctionsDisabled(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentServiceWithFunctions(t, assetStorage, functions.Disabled{}, false)

	_, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: uuid.NewString(),
				Name:    "refused-functions",
				Slug:    "refused-functions",
				Runtime: "nodejs:24",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotImplemented, oopsErr.Code)
	require.ErrorIs(t, err, functions.ErrFunctionsDisabled)
}

func TestCreateDeployment_OpenAPICompletesWhenFunctionsDisabled(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentServiceWithFunctions(t, assetStorage, functions.Disabled{}, false)

	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "functions-disabled-openapi",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{AssetID: ares.Asset.ID, Name: "todo", Slug: "todo"},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment")
	require.Equal(t, "completed", dep.Deployment.Status)
}

// TestCreateDeployment_CarriedFunctionsFailWithReasonWhenFunctionsDisabled
// models functions carried into a deployment that runs with the disabled
// provider (an evolve or redeploy clones them forward): the API accepts the
// deployment, and deploying its runners fails it with a readable log.
func TestCreateDeployment_CarriedFunctionsFailWithReasonWhenFunctionsDisabled(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentServiceWithFunctions(t, assetStorage, functions.Disabled{}, true)

	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:24")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "functions-disabled-carried",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "carried-functions",
				Slug:    "carried-functions",
				Runtime: "nodejs:24",
			},
		},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment")
	require.Equal(t, "failed", dep.Deployment.Status)

	logs, err := ti.service.GetDeploymentLogs(ctx, &gen.GetDeploymentLogsPayload{
		DeploymentID:     dep.Deployment.ID,
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "get deployment logs")

	messages := make([]string, 0, len(logs.Events))
	for _, event := range logs.Events {
		messages = append(messages, event.Message)
	}
	require.Contains(t, messages, functions.ErrFunctionsDisabled.Error())
}

// Excluding functions is how a project on a deployment without Functions
// drops the ones it carried over, so it must not be refused.
func TestEvolve_ExcludeFunctionsAllowedWhenFunctionsDisabled(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:24")
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	created, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "functions-disabled-exclude",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{AssetID: ares.Asset.ID, Name: "todo", Slug: "todo"},
		},
		Functions: []*gen.AddFunctionsForm{
			{AssetID: fres.Asset.ID, Name: "carried-functions", Slug: "carried-functions", Runtime: "nodejs:24"},
		},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment with functions")
	require.Equal(t, "completed", created.Deployment.Status)

	logger := testenv.NewLogger(t)
	disabled := deployments.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.temporalEnv, ti.sessionManager, assetStorage,
		posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", ""), testenv.DefaultSiteURL(t),
		authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		audit.NewLogger(),
		false,
	)
	evolved, err := disabled.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:            nil,
		SessionToken:           nil,
		ProjectSlugInput:       nil,
		DeploymentID:           nil,
		UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{fres.Asset.ID},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve excluding functions")
	require.Equal(t, "completed", evolved.Deployment.Status)
	require.Empty(t, evolved.Deployment.FunctionsAssets)
	require.Len(t, evolved.Deployment.Openapiv3Assets, 1)
}
