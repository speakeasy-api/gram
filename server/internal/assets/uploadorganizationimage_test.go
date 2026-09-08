package assets_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	testidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/gen/assets"
	orggen "github.com/speakeasy-api/gram/server/gen/organization_assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestService_UploadOrganizationImage_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	imageContent := "fake org logo content"
	sha := sha256.Sum256([]byte(imageContent))
	expectedSha256 := hex.EncodeToString(sha[:])
	contentLength := int64(len(imageContent))
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)

	result, err := ti.service.UploadOrganizationImage(ctx, &orggen.UploadOrganizationImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: contentLength,
	}, io.NopCloser(strings.NewReader(imageContent)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)
	require.Equal(t, "image", result.Asset.Kind)
	require.Equal(t, expectedSha256, result.Asset.Sha256)

	// The row must land in the organization tier: organization_id set,
	// project_id NULL, and the blob stored under the organizations/ prefix.
	row, err := ti.repo.GetOrganizationAssetBySHA256(ctx, repo.GetOrganizationAssetBySHA256Params{
		OrganizationID: testidp.MockOrgID,
		Sha256:         expectedSha256,
	})
	require.NoError(t, err)
	require.Equal(t, result.Asset.ID, row.ID.String())
	require.False(t, row.ProjectID.Valid)
	require.True(t, row.OrganizationID.Valid)
	require.Equal(t, testidp.MockOrgID, row.OrganizationID.String)
	require.Contains(t, row.Url, "/organizations/"+testidp.MockOrgID+"/")

	// Organization-tier uploads are audited normally (an owning org exists).
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestService_UploadOrganizationImage_ServedByPublicServeImage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	imageContent := "org logo served publicly"

	result, err := ti.service.UploadOrganizationImage(ctx, &orggen.UploadOrganizationImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: int64(len(imageContent)),
	}, io.NopCloser(strings.NewReader(imageContent)))
	require.NoError(t, err)

	serveResult, body, err := ti.service.ServeImage(ctx, &assets.ServeImageForm{
		ID: result.Asset.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, serveResult)

	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, imageContent, string(bodyBytes))
	require.NoError(t, body.Close())
}

func TestService_UploadOrganizationImage_Duplicate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	imageContent := "duplicate org logo content"
	form := &orggen.UploadOrganizationImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: int64(len(imageContent)),
	}

	result1, err := ti.service.UploadOrganizationImage(ctx, form, io.NopCloser(strings.NewReader(imageContent)))
	require.NoError(t, err)

	result2, err := ti.service.UploadOrganizationImage(ctx, form, io.NopCloser(strings.NewReader(imageContent)))
	require.NoError(t, err)

	require.Equal(t, result1.Asset.ID, result2.Asset.ID)
}

func TestService_UploadOrganizationImage_DoesNotCollideWithPlatformTier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	imageContent := "same bytes at two tiers"
	sha := sha256.Sum256([]byte(imageContent))
	expectedSha256 := hex.EncodeToString(sha[:])

	// A staff-created platform asset with identical bytes must never be
	// touched by an organization-tier upload: the tiers are separate
	// partitions with separate dedupe indexes.
	platformAsset, err := ti.repo.CreatePlatformAsset(ctx, repo.CreatePlatformAssetParams{
		Name:          "image-" + expectedSha256 + ".png",
		Url:           "file://platform/image.png",
		Sha256:        expectedSha256,
		Kind:          "image",
		ContentType:   "image/png",
		ContentLength: int64(len(imageContent)),
	})
	require.NoError(t, err)

	result, err := ti.service.UploadOrganizationImage(ctx, &orggen.UploadOrganizationImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: int64(len(imageContent)),
	}, io.NopCloser(strings.NewReader(imageContent)))
	require.NoError(t, err)
	require.NotEqual(t, platformAsset.ID.String(), result.Asset.ID)

	platformRow, err := ti.repo.GetPlatformAssetBySHA256(ctx, expectedSha256)
	require.NoError(t, err)
	require.Equal(t, platformAsset.ID, platformRow.ID)
	require.Equal(t, "file://platform/image.png", platformRow.Url, "platform asset url must be untouched by the organization upload")
}

func TestService_UploadOrganizationImage_DoesNotCollideWithOtherOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	imageContent := "same bytes at two organizations"
	sha := sha256.Sum256([]byte(imageContent))
	expectedSha256 := hex.EncodeToString(sha[:])

	// An identical-bytes asset owned by another organization must never be
	// touched by this organization's upload: the org-tier dedupe index is
	// keyed on (organization_id, sha256).
	otherOrgID := "org_other_tenant"
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          otherOrgID,
		Name:        "Other Tenant",
		Slug:        "other-tenant",
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	otherAsset, err := ti.repo.CreateOrganizationAsset(ctx, repo.CreateOrganizationAssetParams{
		Name:           "image-" + expectedSha256 + ".png",
		Url:            "file://organizations/other/image.png",
		OrganizationID: otherOrgID,
		Sha256:         expectedSha256,
		Kind:           "image",
		ContentType:    "image/png",
		ContentLength:  int64(len(imageContent)),
	})
	require.NoError(t, err)

	result, err := ti.service.UploadOrganizationImage(ctx, &orggen.UploadOrganizationImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: int64(len(imageContent)),
	}, io.NopCloser(strings.NewReader(imageContent)))
	require.NoError(t, err)
	require.NotEqual(t, otherAsset.ID.String(), result.Asset.ID)

	otherRow, err := ti.repo.GetOrganizationAssetBySHA256(ctx, repo.GetOrganizationAssetBySHA256Params{
		OrganizationID: otherOrgID,
		Sha256:         expectedSha256,
	})
	require.NoError(t, err)
	require.Equal(t, otherAsset.ID, otherRow.ID)
	require.Equal(t, "file://organizations/other/image.png", otherRow.Url, "other organization's asset url must be untouched")
}

func TestService_UploadOrganizationImage_Forbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	// RBAC active with zero grants: the caller is authenticated but not an
	// org admin.
	deniedCtx := authztest.WithExactGrants(t, ctx)

	_, err := ti.service.UploadOrganizationImage(deniedCtx, &orggen.UploadOrganizationImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: 100,
	}, io.NopCloser(strings.NewReader("denied")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	_, err = ti.repo.GetOrganizationAssetBySHA256(ctx, repo.GetOrganizationAssetBySHA256Params{
		OrganizationID: testidp.MockOrgID,
		Sha256:         mustSHA256("denied"),
	})
	require.Error(t, err, "no organization asset row may exist after a denied upload")
}

func TestService_UploadOrganizationImage_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAssetsService(t)

	_, err := ti.service.UploadOrganizationImage(t.Context(), &orggen.UploadOrganizationImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: 100,
	}, io.NopCloser(strings.NewReader("test")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_UploadOrganizationImage_UnsupportedContentType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.UploadOrganizationImage(ctx, &orggen.UploadOrganizationImageForm{
		SessionToken:  nil,
		ContentType:   "application/pdf",
		ContentLength: 100,
	}, io.NopCloser(strings.NewReader("fake pdf content")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnsupportedMedia, oopsErr.Code)
}

func mustSHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
