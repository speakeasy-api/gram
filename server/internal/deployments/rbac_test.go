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
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDeployments_RBAC_ReadOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	_, err = ti.service.GetDeployment(ctx, &gen.GetDeploymentPayload{
		ID:               uuid.NewString(),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	_, err = ti.service.GetLatestDeployment(ctx, &gen.GetLatestDeploymentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	_, err = ti.service.GetActiveDeployment(ctx, &gen.GetActiveDeploymentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestDeployments_RBAC_ReadOps_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	projectID := authCtx.ProjectID.String()
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, projectID))

	_, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.GetLatestDeployment(ctx, &gen.GetLatestDeploymentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.GetActiveDeployment(ctx, &gen.GetActiveDeploymentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestDeployments_RBAC_ReadOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	projectID := authCtx.ProjectID.String()
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, projectID))

	_, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestDeployments_RBAC_ReadOps_DeniedWithWrongResourceID(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, uuid.NewString()))

	_, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestDeployments_RBAC_WriteOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:   "rbac-test-create",
		Openapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ExternalMcps:     []*gen.AddExternalMCPForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
		NonBlocking:      nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	_, err = ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:            nil,
		SessionToken:           nil,
		ProjectSlugInput:       nil,
		DeploymentID:           nil,
		UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		UpsertExternalMcps:     []*gen.AddExternalMCPForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
		ExcludeExternalMcps:    []string{},
		NonBlocking:            nil,
	})
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	_, err = ti.service.Redeploy(ctx, &gen.RedeployPayload{
		DeploymentID:     uuid.NewString(),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestDeployments_RBAC_WriteOps_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	projectID := authCtx.ProjectID.String()
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, projectID))

	_, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:   "rbac-test-create-readonly",
		Openapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ExternalMcps:     []*gen.AddExternalMCPForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
		NonBlocking:      nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestDeployments_RBAC_WriteOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	projectID := authCtx.ProjectID.String()
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, projectID))

	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	}, io.NopCloser(bs))
	require.NoError(t, err)

	_, err = ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "rbac-test-create-write",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{AssetID: ares.Asset.ID, Name: "rbac-doc", Slug: "rbac-doc"},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ExternalMcps:     []*gen.AddExternalMCPForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
		NonBlocking:      nil,
	})
	require.NoError(t, err)
}
