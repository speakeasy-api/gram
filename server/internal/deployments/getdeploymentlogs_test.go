package deployments_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDeploymentsService_GetDeploymentLogs_Success(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload OpenAPI asset
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	// Create deployment
	created, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-get-deployment-logs",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "test-doc",
				Slug:    "test-doc",
			},
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

	// Test GetDeploymentLogs
	result, err := ti.service.GetDeploymentLogs(ctx, &gen.GetDeploymentLogsPayload{
		DeploymentID:     created.Deployment.ID,
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "get deployment logs")

	// Verify response structure
	require.NotNil(t, result.Events, "events should not be nil")
	require.NotEmpty(t, result.Status, "status should not be empty")
	// Note: NextCursor can be nil if there are no more pages

	// Verify that we get some deployment events (deployment creation should generate events)
	require.NotEmpty(t, result.Events, "should have at least one log event")

	// Verify event structure
	for _, event := range result.Events {
		require.NotEmpty(t, event.ID, "event ID should not be empty")
		require.NotEmpty(t, event.Event, "event type should not be empty")
		require.NotEmpty(t, event.Message, "event message should not be empty")
		require.NotEmpty(t, event.CreatedAt, "event created at should not be empty")
	}
}

func TestDeploymentsService_GetDeploymentLogs_InvalidDeploymentID(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test with invalid UUID
	_, err := ti.service.GetDeploymentLogs(ctx, &gen.GetDeploymentLogsPayload{
		DeploymentID:     "invalid-uuid",
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad deployment id")
}

func TestDeploymentsService_GetDeploymentLogs_InvalidCursor(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test with invalid cursor UUID
	deploymentID := uuid.New().String()
	invalidCursor := "invalid-cursor-uuid"
	_, err := ti.service.GetDeploymentLogs(ctx, &gen.GetDeploymentLogsPayload{
		DeploymentID:     deploymentID,
		Cursor:           &invalidCursor,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cursor")
}

func TestDeploymentsService_GetDeploymentLogs_NonExistentDeployment(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test with non-existent deployment ID (should return empty results, not error)
	nonExistentID := uuid.New().String()
	result, err := ti.service.GetDeploymentLogs(ctx, &gen.GetDeploymentLogsPayload{
		DeploymentID:     nonExistentID,
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "should not error for non-existent deployment")
	require.NotNil(t, result.Events, "events should not be nil")
	require.Empty(t, result.Events, "should have no events for non-existent deployment")
	require.Equal(t, "unknown", result.Status, "status should be unknown for non-existent deployment")
	require.Nil(t, result.NextCursor, "next cursor should be nil for empty results")
}

func TestDeploymentsService_GetDeploymentLogs_Unauthorized(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	_, ti := newTestDeploymentService(t, assetStorage)

	// Test with context that has no auth context
	ctx := t.Context()
	deploymentID := uuid.New().String()

	_, err := ti.service.GetDeploymentLogs(ctx, &gen.GetDeploymentLogsPayload{
		DeploymentID:     deploymentID,
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestDeploymentsService_GetDeploymentLogs_ValidCursor(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload OpenAPI asset
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	// Create deployment
	created, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-get-deployment-logs-valid-cursor",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "test-doc",
				Slug:    "test-doc",
			},
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

	// Test with valid cursor (using base64-encoded "seq:uuid" format)
	// A cursor pointing to a very high seq number should return empty results
	validCursor := base64.RawURLEncoding.EncodeToString([]byte("999999999:" + uuid.New().String()))
	result, err := ti.service.GetDeploymentLogs(ctx, &gen.GetDeploymentLogsPayload{
		DeploymentID:     created.Deployment.ID,
		Cursor:           &validCursor,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "should not error with valid cursor format")
	require.NotEmpty(t, result.Status, "status should not be empty")
	require.NotNil(t, result.Events, "events should not be nil")
	require.Empty(t, result.Events, "events should be empty with cursor past all logs")
}

func TestDeploymentsService_GetDeploymentLogs_InvalidCursorNotBase64(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test with invalid base64 string
	deploymentID := uuid.New().String()
	invalidCursor := "not-valid-base64!!!"
	_, err := ti.service.GetDeploymentLogs(ctx, &gen.GetDeploymentLogsPayload{
		DeploymentID:     deploymentID,
		Cursor:           &invalidCursor,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cursor")
}

func TestDeploymentsService_GetDeploymentLogs_InvalidCursorNotNumeric(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test with valid base64 and format but non-numeric seq
	deploymentID := uuid.New().String()
	invalidCursor := base64.RawURLEncoding.EncodeToString([]byte("not-a-number:" + uuid.New().String()))
	_, err := ti.service.GetDeploymentLogs(ctx, &gen.GetDeploymentLogsPayload{
		DeploymentID:     deploymentID,
		Cursor:           &invalidCursor,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cursor")
}

func TestDeploymentsService_GetDeploymentLogs_InvalidCursorBadFormat(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test with valid base64 but missing colon separator
	deploymentID := uuid.New().String()
	invalidCursor := base64.RawURLEncoding.EncodeToString([]byte("12345"))
	_, err := ti.service.GetDeploymentLogs(ctx, &gen.GetDeploymentLogsPayload{
		DeploymentID:     deploymentID,
		Cursor:           &invalidCursor,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cursor")
}

func TestDeploymentsService_GetDeploymentLogs_InvalidCursorBadUUID(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test with valid seq but invalid UUID
	deploymentID := uuid.New().String()
	invalidCursor := base64.RawURLEncoding.EncodeToString([]byte("12345:not-a-uuid"))
	_, err := ti.service.GetDeploymentLogs(ctx, &gen.GetDeploymentLogsPayload{
		DeploymentID:     deploymentID,
		Cursor:           &invalidCursor,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cursor")
}
