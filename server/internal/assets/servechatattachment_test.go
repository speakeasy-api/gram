package assets_test

import (
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestService_ServeChatAttachment_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	contentType := "text/plain"
	testContent := "fake chat attachment content"
	contentLength := int64(len(testContent))
	filename := "test-attachment.txt"

	// Setup storage with test content first
	writer, uri, err := ti.storage.Write(ctx, filename, contentType, contentLength)
	require.NoError(t, err)

	_, err = io.Copy(writer, strings.NewReader(testContent))
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Create asset in database using the URI from storage
	asset, err := ti.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:          filename,
		Url:           uri.String(),
		ProjectID:     projectID,
		Sha256:        "abc123",
		Kind:          "chat_attachment",
		ContentType:   contentType,
		ContentLength: contentLength,
	})
	require.NoError(t, err)

	// Call ServeChatAttachment
	result, body, err := ti.service.ServeChatAttachment(ctx, &assets.ServeChatAttachmentForm{
		SessionToken: nil,
		ApikeyToken:  nil,
		ProjectID:    projectID.String(),
		ID:           asset.ID.String(),
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, body)

	require.Equal(t, contentType, result.ContentType)
	require.Equal(t, contentLength, result.ContentLength)
	require.NotEmpty(t, result.LastModified)

	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, testContent, string(bodyBytes))

	err = body.Close()
	require.NoError(t, err)
}

func TestService_ServeChatAttachment_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAssetsService(t)

	// Create context without auth
	ctx := t.Context()

	_, _, err := ti.service.ServeChatAttachment(ctx, &assets.ServeChatAttachmentForm{
		SessionToken: nil,
		ApikeyToken:  nil,
		ProjectID:    "",
		ID:           uuid.New().String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_ServeChatAttachment_InvalidAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	_, _, err := ti.service.ServeChatAttachment(ctx, &assets.ServeChatAttachmentForm{
		SessionToken: nil,
		ApikeyToken:  nil,
		ProjectID:    projectID.String(),
		ID:           "invalid-uuid",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "invalid asset id", oopsErr.Error())
}

func TestService_ServeChatAttachment_EmptyAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	_, _, err := ti.service.ServeChatAttachment(ctx, &assets.ServeChatAttachmentForm{
		SessionToken: nil,
		ApikeyToken:  nil,
		ProjectID:    projectID.String(),
		ID:           "00000000-0000-0000-0000-000000000000",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "asset id cannot be empty", oopsErr.Error())
}

func TestService_ServeChatAttachment_InvalidProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	_, _, err := ti.service.ServeChatAttachment(ctx, &assets.ServeChatAttachmentForm{
		SessionToken: nil,
		ApikeyToken:  nil,
		ProjectID:    "invalid-uuid",
		ID:           uuid.New().String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "invalid project id")
}

func TestService_ServeChatAttachment_EmptyProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	_, _, err := ti.service.ServeChatAttachment(ctx, &assets.ServeChatAttachmentForm{
		SessionToken: nil,
		ApikeyToken:  nil,
		ProjectID:    "00000000-0000-0000-0000-000000000000",
		ID:           uuid.New().String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "project id cannot be empty", oopsErr.Error())
}

func TestService_ServeChatAttachment_AssetNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	nonExistentID := uuid.New()

	_, _, err := ti.service.ServeChatAttachment(ctx, &assets.ServeChatAttachmentForm{
		SessionToken: nil,
		ApikeyToken:  nil,
		ProjectID:    projectID.String(),
		ID:           nonExistentID.String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_ServeChatAttachment_WrongProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	// Create asset for different project
	differentProjectID := uuid.New()

	asset, err := ti.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:          "test-attachment.txt",
		Url:           "file://test-attachment.txt",
		ProjectID:     differentProjectID,
		Sha256:        "abc123",
		Kind:          "chat_attachment",
		ContentType:   "text/plain",
		ContentLength: 1024,
	})
	require.NoError(t, err)

	_, _, err = ti.service.ServeChatAttachment(ctx, &assets.ServeChatAttachmentForm{
		SessionToken: nil,
		ApikeyToken:  nil,
		ProjectID:    projectID.String(),
		ID:           asset.ID.String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_ServeChatAttachment_FileNotInStorage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	// Create asset in database but don't put file in storage
	asset, err := ti.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:          "missing-attachment.txt",
		Url:           "file://missing-asset.txt",
		ProjectID:     projectID,
		Sha256:        "abc123",
		Kind:          "chat_attachment",
		ContentType:   "text/plain",
		ContentLength: 1024,
	})
	require.NoError(t, err)

	_, _, err = ti.service.ServeChatAttachment(ctx, &assets.ServeChatAttachmentForm{
		SessionToken: nil,
		ApikeyToken:  nil,
		ProjectID:    projectID.String(),
		ID:           asset.ID.String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_ServeChatAttachment_CrossProjectAccess(t *testing.T) {
	t.Parallel()

	// Create test service instance
	ctx1, ti := newTestAssetsService(t)

	authCtx1, ok := contextvalues.GetAuthContext(ctx1)
	require.True(t, ok)
	require.NotNil(t, authCtx1.ProjectID)
	project1ID := *authCtx1.ProjectID

	// Create second auth context with different project using the same database connection
	ctx2 := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessionManager)

	authCtx2, ok := contextvalues.GetAuthContext(ctx2)
	require.True(t, ok)
	require.NotNil(t, authCtx2.ProjectID)
	project2ID := *authCtx2.ProjectID

	// Ensure we have different projects
	require.NotEqual(t, project1ID, project2ID)

	contentType := "text/plain"
	testContent := "fake attachment content for project 1"
	contentLength := int64(len(testContent))
	filename := "project1-attachment.txt"

	// Creates an asset in first project
	writer, uri, err := ti.storage.Write(ctx1, filename, contentType, contentLength)
	require.NoError(t, err)

	_, err = io.Copy(writer, strings.NewReader(testContent))
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	asset, err := ti.repo.CreateAsset(ctx1, repo.CreateAssetParams{
		Name:          filename,
		Url:           uri.String(),
		ProjectID:     project1ID,
		Sha256:        "project1hash",
		Kind:          "chat_attachment",
		ContentType:   contentType,
		ContentLength: contentLength,
	})
	require.NoError(t, err)

	// Ensure we can access it with the same auth context
	result, body, err := ti.service.ServeChatAttachment(ctx1, &assets.ServeChatAttachmentForm{
		SessionToken: nil,
		ApikeyToken:  nil,
		ProjectID:    project1ID.String(),
		ID:           asset.ID.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, body)

	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, testContent, string(bodyBytes))

	err = body.Close()
	require.NoError(t, err)

	// Ensure we cannot access it from the auth context of a different project
	_, _, err = ti.service.ServeChatAttachment(ctx2, &assets.ServeChatAttachmentForm{
		SessionToken: nil,
		ApikeyToken:  nil,
		ProjectID:    project2ID.String(),
		ID:           asset.ID.String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}
