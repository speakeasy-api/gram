package assets_test

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/assets"
	assetsinternal "github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_CreateSignedChatAttachmentURL_Success(t *testing.T) {
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

	// Create asset in database
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

	// Call CreateSignedChatAttachmentURL
	result, err := ti.service.CreateSignedChatAttachmentURL(ctx, &assets.CreateSignedChatAttachmentURLForm{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ProjectID:        projectID.String(),
		ID:               asset.ID.String(),
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.URL)
	require.NotEmpty(t, result.ExpiresAt)

	// Verify the URL contains a token
	require.Contains(t, result.URL, "token=")

	// Verify expiration is in the future
	expiresAt, err := time.Parse(time.RFC3339, result.ExpiresAt)
	require.NoError(t, err)
	require.True(t, expiresAt.After(time.Now()))
}

func TestService_CreateSignedChatAttachmentURL_CustomTTL(t *testing.T) {
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

	// Create asset in database
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

	// Call with custom TTL of 120 seconds
	result, err := ti.service.CreateSignedChatAttachmentURL(ctx, &assets.CreateSignedChatAttachmentURLForm{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ProjectID:        projectID.String(),
		ID:               asset.ID.String(),
		TTLSeconds:       conv.Ptr(120),
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	expiresAt, err := time.Parse(time.RFC3339, result.ExpiresAt)
	require.NoError(t, err)

	// Should expire approximately 120 seconds from now (allow 5 second tolerance)
	expectedExpiry := time.Now().Add(120 * time.Second)
	require.WithinDuration(t, expectedExpiry, expiresAt, 5*time.Second)
}

func TestService_CreateSignedChatAttachmentURL_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAssetsService(t)

	// Create context without auth
	ctx := t.Context()

	_, err := ti.service.CreateSignedChatAttachmentURL(ctx, &assets.CreateSignedChatAttachmentURLForm{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ProjectID:        uuid.New().String(),
		ID:               uuid.New().String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_CreateSignedChatAttachmentURL_AssetNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	// Call with non-existent asset ID
	_, err := ti.service.CreateSignedChatAttachmentURL(ctx, &assets.CreateSignedChatAttachmentURLForm{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ProjectID:        projectID.String(),
		ID:               uuid.New().String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_ServeChatAttachmentSigned_Success(t *testing.T) {
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

	// Create asset in database
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

	// First create a signed URL
	signedResult, err := ti.service.CreateSignedChatAttachmentURL(ctx, &assets.CreateSignedChatAttachmentURLForm{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ProjectID:        projectID.String(),
		ID:               asset.ID.String(),
	})
	require.NoError(t, err)

	// Extract token from URL
	token := extractTokenFromURL(t, signedResult.URL)

	// Call ServeChatAttachmentSigned without auth context (NoSecurity)
	unauthCtx := t.Context()
	result, body, err := ti.service.ServeChatAttachmentSigned(unauthCtx, &assets.ServeChatAttachmentSignedForm{
		Token: token,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, body)

	require.Equal(t, contentType, result.ContentType)
	require.Equal(t, contentLength, result.ContentLength)
	require.NotEmpty(t, result.LastModified)
	require.NotNil(t, result.AccessControlAllowOrigin)
	require.Equal(t, "*", *result.AccessControlAllowOrigin)

	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, testContent, string(bodyBytes))

	err = body.Close()
	require.NoError(t, err)
}

func TestService_ServeChatAttachmentSigned_ExpiredToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	// Generate an expired token
	token, _, err := assetsinternal.GenerateSignedAssetToken("test-jwt-secret", uuid.New(), projectID, -1) // negative TTL = already expired
	require.NoError(t, err)

	_, _, err = ti.service.ServeChatAttachmentSigned(t.Context(), &assets.ServeChatAttachmentSignedForm{
		Token: token,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_ServeChatAttachmentSigned_InvalidToken(t *testing.T) {
	t.Parallel()

	_, ti := newTestAssetsService(t)

	_, _, err := ti.service.ServeChatAttachmentSigned(t.Context(), &assets.ServeChatAttachmentSignedForm{
		Token: "not-a-valid-jwt-token",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_ServeChatAttachmentSigned_TamperedToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	// Generate a valid token but sign it with a different secret
	token, _, err := assetsinternal.GenerateSignedAssetToken("wrong-secret", uuid.New(), projectID, 600*time.Second)
	require.NoError(t, err)

	_, _, err = ti.service.ServeChatAttachmentSigned(t.Context(), &assets.ServeChatAttachmentSignedForm{
		Token: token,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_ServeChatAttachmentSigned_AssetNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	// Generate a valid token for a non-existent asset
	token, _, err := assetsinternal.GenerateSignedAssetToken("test-jwt-secret", uuid.New(), projectID, 600*time.Second)
	require.NoError(t, err)

	_, _, err = ti.service.ServeChatAttachmentSigned(t.Context(), &assets.ServeChatAttachmentSignedForm{
		Token: token,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_SignedChatAttachment_EndToEnd(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	contentType := "application/json"
	testContent := `{"message": "hello world"}`
	contentLength := int64(len(testContent))
	filename := "test-data.json"

	// Step 1: Setup storage with test content
	writer, uri, err := ti.storage.Write(ctx, filename, contentType, contentLength)
	require.NoError(t, err)

	_, err = io.Copy(writer, strings.NewReader(testContent))
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Step 2: Create asset in database
	asset, err := ti.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:          filename,
		Url:           uri.String(),
		ProjectID:     projectID,
		Sha256:        "def456",
		Kind:          "chat_attachment",
		ContentType:   contentType,
		ContentLength: contentLength,
	})
	require.NoError(t, err)

	// Step 3: Create signed URL (requires auth)
	signedResult, err := ti.service.CreateSignedChatAttachmentURL(ctx, &assets.CreateSignedChatAttachmentURLForm{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ProjectID:        projectID.String(),
		ID:               asset.ID.String(),
		TTLSeconds:       conv.Ptr(60), // 1 minute
	})
	require.NoError(t, err)
	require.NotEmpty(t, signedResult.URL)

	// Step 4: Extract token
	token := extractTokenFromURL(t, signedResult.URL)

	// Step 5: Serve content using signed URL (no auth required)
	unauthCtx := t.Context()
	result, body, err := ti.service.ServeChatAttachmentSigned(unauthCtx, &assets.ServeChatAttachmentSignedForm{
		Token: token,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, body)

	// Step 6: Verify content
	require.Equal(t, contentType, result.ContentType)
	require.Equal(t, contentLength, result.ContentLength)

	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, testContent, string(bodyBytes))

	err = body.Close()
	require.NoError(t, err)
}

// extractTokenFromURL extracts the token query parameter from a signed URL
func extractTokenFromURL(t *testing.T, signedURL string) string {
	t.Helper()

	// URL is in format: /rpc/assets.serveChatAttachmentSigned?token=...
	require.Contains(t, signedURL, "token=")

	// Simple extraction - find token= and take everything after it
	idx := strings.Index(signedURL, "token=")
	require.Greater(t, idx, -1)

	token := signedURL[idx+len("token="):]
	// Remove any trailing query params
	if ampIdx := strings.Index(token, "&"); ampIdx > -1 {
		token = token[:ampIdx]
	}

	return token
}
