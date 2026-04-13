package skills_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_Capture_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)

	content := []byte("PK\x03\x04skill-zip-content")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])

	result, err := ti.service.Capture(
		ctx,
		newCapturePayload("application/zip", int64(len(content)), expectedSHA),
		io.NopCloser(bytes.NewReader(content)),
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)
	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "skill", result.Asset.Kind)
	require.Equal(t, expectedSHA, result.Asset.Sha256)
	require.Equal(t, "application/zip", result.Asset.ContentType)
	require.Equal(t, int64(len(content)), result.Asset.ContentLength)
	require.NotEmpty(t, result.Asset.CreatedAt)
	require.NotEmpty(t, result.Asset.UpdatedAt)
}

func TestService_Capture_DedupesExistingAsset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)

	content := []byte("PK\x03\x04skill-zip-content-dedupe")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])
	payload := newCapturePayload("application/zip", int64(len(content)), expectedSHA)

	first, err := ti.service.Capture(ctx, payload, io.NopCloser(bytes.NewReader(content)))
	require.NoError(t, err)
	require.NotNil(t, first)
	require.NotNil(t, first.Asset)

	second, err := ti.service.Capture(ctx, payload, io.NopCloser(bytes.NewReader(content)))
	require.NoError(t, err)
	require.NotNil(t, second)
	require.NotNil(t, second.Asset)
	require.Equal(t, first.Asset.ID, second.Asset.ID)
	require.Equal(t, first.Asset.Sha256, second.Asset.Sha256)
}

func TestService_Capture_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestSkillsService(t)

	content := []byte("PK\x03\x04skill-zip-content-unauthorized")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])

	_, err := ti.service.Capture(
		t.Context(),
		newCapturePayload("application/zip", int64(len(content)), expectedSHA),
		io.NopCloser(bytes.NewReader(content)),
	)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_Capture_UnsupportedContentType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)

	content := []byte("PK\x03\x04skill-zip-content-content-type")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])

	_, err := ti.service.Capture(
		ctx,
		newCapturePayload("application/octet-stream", int64(len(content)), expectedSHA),
		io.NopCloser(bytes.NewReader(content)),
	)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnsupportedMedia, oopsErr.Code)
}

func TestService_Capture_ZeroContentLength(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)

	content := []byte("PK\x03\x04skill-zip-content-zero-length")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])

	_, err := ti.service.Capture(
		ctx,
		newCapturePayload("application/zip", 0, expectedSHA),
		io.NopCloser(bytes.NewReader(content)),
	)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "content length must be > 0")
}

func TestService_Capture_ContentLengthExceedsLimit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)

	content := []byte("PK\x03\x04skill-zip-content-size-limit")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])

	_, err := ti.service.Capture(
		ctx,
		newCapturePayload("application/zip", 10*1024*1024+1, expectedSHA),
		io.NopCloser(bytes.NewReader(content)),
	)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "content length exceeds 10 MiB limit")
}

func TestService_Capture_ContentSHA256Mismatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)

	content := []byte("PK\x03\x04skill-zip-content-sha-mismatch")
	wrongSHA := strings.Repeat("0", 64)

	_, err := ti.service.Capture(
		ctx,
		newCapturePayload("application/zip", int64(len(content)), wrongSHA),
		io.NopCloser(bytes.NewReader(content)),
	)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "content sha256 mismatch")
}

func TestService_Capture_ContentLengthMismatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)

	content := []byte("PK\x03\x04skill-zip-content-length-mismatch")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])

	_, err := ti.service.Capture(
		ctx,
		newCapturePayload("application/zip", int64(len(content)+1), expectedSHA),
		io.NopCloser(bytes.NewReader(content)),
	)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "content length mismatch")
}

func TestService_Capture_MissingProjectIDInAuthContext(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	content := []byte("PK\x03\x04skill-zip-content-auth-project")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])

	_, err := ti.service.Capture(
		ctx,
		newCapturePayload("application/zip", int64(len(content)), expectedSHA),
		io.NopCloser(bytes.NewReader(content)),
	)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
