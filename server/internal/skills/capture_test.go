package skills_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func TestService_Capture_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

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

	skill, err := ti.skillsRepo.GetSkillBySlug(ctx, skillsrepo.GetSkillBySlugParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "golang",
	})
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, skill.OrganizationID)
	require.Equal(t, *authCtx.ProjectID, skill.ProjectID)
	require.Equal(t, "golang", skill.Name)
	require.Equal(t, "golang", skill.Slug)
	require.Equal(t, authCtx.UserID, skill.CreatedByUserID)
	require.True(t, skill.ActiveVersionID.Valid)

	version, err := ti.skillsRepo.GetSkillVersionByHash(ctx, skillsrepo.GetSkillVersionByHashParams{
		SkillID:       skill.ID,
		ContentSha256: expectedSHA,
		ProjectID:     *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, skill.ID, version.SkillID)
	require.Equal(t, "zip", version.AssetFormat)
	require.Equal(t, int64(len(content)), version.SizeBytes)
	require.Equal(t, "pending_review", version.State)
	require.Equal(t, authCtx.UserID, version.CapturedByUserID)
	require.Equal(t, version.ID, skill.ActiveVersionID.UUID)
}

func TestService_Capture_DedupesExistingAsset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

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

	skill, err := ti.skillsRepo.GetSkillBySlug(ctx, skillsrepo.GetSkillBySlugParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "golang",
	})
	require.NoError(t, err)

	versions, err := ti.skillsRepo.ListSkillVersions(ctx, skillsrepo.ListSkillVersionsParams{
		ProjectID: *authCtx.ProjectID,
		SkillID:   skill.ID,
	})
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, expectedSHA, versions[0].ContentSha256)
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

func TestService_Capture_ConflictingNonSkillAssetBySHA(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	content := []byte("PK\x03\x04skill-zip-content-conflict")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])
	payload := newCapturePayload("application/zip", int64(len(content)), expectedSHA)

	_, err := ti.repo.CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name:          fmt.Sprintf("image-%s.png", expectedSHA),
		Url:           "file://existing/image",
		ProjectID:     *authCtx.ProjectID,
		Sha256:        expectedSHA,
		Kind:          "image",
		ContentType:   "image/png",
		ContentLength: 123,
	})
	require.NoError(t, err)

	_, err = ti.service.Capture(ctx, payload, io.NopCloser(bytes.NewReader(content)))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "non-skill asset")
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

func TestService_Capture_PolicyDisabledByDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsServiceWithCaptureMode(t, nil)

	content := []byte("PK\x03\x04skill-zip-content-policy-default")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])

	_, err := ti.service.Capture(
		ctx,
		newCapturePayload("application/zip", int64(len(content)), expectedSHA),
		io.NopCloser(bytes.NewReader(content)),
	)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "skill capture is disabled")
}

func TestService_Capture_ProjectOnlyPolicyAllowsProjectScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	_, err := ti.skillsRepo.UpsertOrganizationCapturePolicy(ctx, skillsrepo.UpsertOrganizationCapturePolicyParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Mode:           "project_only",
	})
	require.NoError(t, err)

	content := []byte("PK\x03\x04skill-zip-content-policy-project")
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
}

func TestService_Capture_ProjectOnlyPolicyRejectsUserScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.skillsRepo.UpsertOrganizationCapturePolicy(ctx, skillsrepo.UpsertOrganizationCapturePolicyParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Mode:           "project_only",
	})
	require.NoError(t, err)

	content := []byte("PK\x03\x04skill-zip-content-policy-user-scope")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])
	payload := newCapturePayload("application/zip", int64(len(content)), expectedSHA)
	payload.Scope = "user"

	_, err = ti.service.Capture(ctx, payload, io.NopCloser(bytes.NewReader(content)))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "not permitted by effective mode")
}

func TestService_Capture_UserOnlyPolicyAllowsUserScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.skillsRepo.UpsertOrganizationCapturePolicy(ctx, skillsrepo.UpsertOrganizationCapturePolicyParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Mode:           "user_only",
	})
	require.NoError(t, err)

	content := []byte("PK\x03\x04skill-zip-content-policy-user-only-allow")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])
	payload := newCapturePayload("application/zip", int64(len(content)), expectedSHA)
	payload.Scope = "user"

	result, err := ti.service.Capture(
		ctx,
		payload,
		io.NopCloser(bytes.NewReader(content)),
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)
}

func TestService_Capture_UserOnlyPolicyRejectsProjectScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.skillsRepo.UpsertOrganizationCapturePolicy(ctx, skillsrepo.UpsertOrganizationCapturePolicyParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Mode:           "user_only",
	})
	require.NoError(t, err)

	content := []byte("PK\x03\x04skill-zip-content-policy-user-only-reject")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])
	payload := newCapturePayload("application/zip", int64(len(content)), expectedSHA)
	payload.Scope = "project"

	_, err = ti.service.Capture(ctx, payload, io.NopCloser(bytes.NewReader(content)))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "not permitted by effective mode")
}

func TestService_Capture_ProjectOverrideTakesPrecedence(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	_, err := ti.skillsRepo.UpsertOrganizationCapturePolicy(ctx, skillsrepo.UpsertOrganizationCapturePolicyParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Mode:           "disabled",
	})
	require.NoError(t, err)

	_, err = ti.skillsRepo.UpsertProjectCapturePolicyOverride(ctx, skillsrepo.UpsertProjectCapturePolicyOverrideParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Mode:           "project_and_user",
	})
	require.NoError(t, err)

	content := []byte("PK\x03\x04skill-zip-content-policy-override")
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
}

func TestService_Capture_RejectsNameWithoutAlphanumericSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)

	content := []byte("PK\x03\x04skill-zip-content-invalid-slug")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])
	payload := newCapturePayload("application/zip", int64(len(content)), expectedSHA)
	payload.Name = "!!!"

	_, err := ti.service.Capture(ctx, payload, io.NopCloser(bytes.NewReader(content)))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "skill name must include at least one alphanumeric character")
}

func TestService_Capture_BackfillsLineageForExistingAsset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	content := []byte("PK\x03\x04skill-zip-content-backfill")
	sha := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(sha[:])
	payload := newCapturePayload("application/zip", int64(len(content)), expectedSHA)

	existingAsset, err := ti.repo.CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name:          fmt.Sprintf("skill-%s.zip", expectedSHA),
		Url:           fmt.Sprintf("file://skills/%s.zip", expectedSHA),
		ProjectID:     *authCtx.ProjectID,
		Sha256:        expectedSHA,
		Kind:          "skill",
		ContentType:   "application/zip",
		ContentLength: int64(len(content)),
	})
	require.NoError(t, err)

	result, err := ti.service.Capture(ctx, payload, io.NopCloser(bytes.NewReader(content)))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)
	require.Equal(t, existingAsset.ID.String(), result.Asset.ID)

	skill, err := ti.skillsRepo.GetSkillBySlug(ctx, skillsrepo.GetSkillBySlugParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "golang",
	})
	require.NoError(t, err)
	require.True(t, skill.ActiveVersionID.Valid)

	version, err := ti.skillsRepo.GetSkillVersionByHash(ctx, skillsrepo.GetSkillVersionByHashParams{
		SkillID:       skill.ID,
		ContentSha256: expectedSHA,
		ProjectID:     *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, existingAsset.ID, version.AssetID)
	require.Equal(t, "pending_review", version.State)
	require.Equal(t, version.ID, skill.ActiveVersionID.UUID)
}
