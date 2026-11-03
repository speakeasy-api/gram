package assets_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	testidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	admingen "github.com/speakeasy-api/gram/server/gen/admin_assets"
	"github.com/speakeasy-api/gram/server/gen/assets"
	orggen "github.com/speakeasy-api/gram/server/gen/organization_assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_UploadPlatformImage_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	adminCtx := withAdmin(t, ctx)

	imageContent := "fake platform logo content"
	sha := sha256.Sum256([]byte(imageContent))
	expectedSha256 := hex.EncodeToString(sha[:])
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)

	result, err := ti.service.UploadPlatformImage(adminCtx, &admingen.UploadPlatformImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: int64(len(imageContent)),
	}, io.NopCloser(strings.NewReader(imageContent)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)
	require.Equal(t, "image", result.Asset.Kind)
	require.Equal(t, expectedSha256, result.Asset.Sha256)

	// The row must land in the platform tier: both owner columns NULL and the
	// blob stored under the platform/ prefix.
	row, err := ti.repo.GetPlatformAssetBySHA256(ctx, expectedSha256)
	require.NoError(t, err)
	require.Equal(t, result.Asset.ID, row.ID.String())
	require.False(t, row.ProjectID.Valid)
	require.False(t, row.OrganizationID.Valid)
	require.Contains(t, row.Url, "/platform/")

	// Platform assets have no owning organization, so no auditlogs row is
	// written (audit_log.organization_id is NOT NULL); audit is
	// structured-logs only.
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestService_UploadPlatformImage_ServedByPublicServeImage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	adminCtx := withAdmin(t, ctx)

	imageContent := "platform logo served publicly"

	result, err := ti.service.UploadPlatformImage(adminCtx, &admingen.UploadPlatformImageForm{
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

func TestService_UploadPlatformImage_Duplicate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	adminCtx := withAdmin(t, ctx)

	imageContent := "duplicate platform logo content"
	form := &admingen.UploadPlatformImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: int64(len(imageContent)),
	}

	result1, err := ti.service.UploadPlatformImage(adminCtx, form, io.NopCloser(strings.NewReader(imageContent)))
	require.NoError(t, err)

	result2, err := ti.service.UploadPlatformImage(adminCtx, form, io.NopCloser(strings.NewReader(imageContent)))
	require.NoError(t, err)

	require.Equal(t, result1.Asset.ID, result2.Asset.ID)
}

func TestService_UploadPlatformImage_DoesNotCollideWithOrganizationTier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	adminCtx := withAdmin(t, ctx)

	imageContent := "same bytes at platform and org tiers"
	sha := sha256.Sum256([]byte(imageContent))
	expectedSha256 := hex.EncodeToString(sha[:])

	// An organization-owned asset with identical bytes must never be touched
	// by a platform-tier upload: the platform dedupe index only spans rows
	// with both owner columns NULL.
	orgAsset, err := ti.repo.CreateOrganizationAsset(ctx, repo.CreateOrganizationAssetParams{
		Name:           "image-" + expectedSha256 + ".png",
		Url:            "file://organizations/mock/image.png",
		OrganizationID: testidp.MockOrgID,
		Sha256:         expectedSha256,
		Kind:           "image",
		ContentType:    "image/png",
		ContentLength:  int64(len(imageContent)),
	})
	require.NoError(t, err)

	result, err := ti.service.UploadPlatformImage(adminCtx, &admingen.UploadPlatformImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: int64(len(imageContent)),
	}, io.NopCloser(strings.NewReader(imageContent)))
	require.NoError(t, err)
	require.NotEqual(t, orgAsset.ID.String(), result.Asset.ID)

	orgRow, err := ti.repo.GetOrganizationAssetBySHA256(ctx, repo.GetOrganizationAssetBySHA256Params{
		OrganizationID: testidp.MockOrgID,
		Sha256:         expectedSha256,
	})
	require.NoError(t, err)
	require.Equal(t, orgAsset.ID, orgRow.ID)
	require.Equal(t, "file://organizations/mock/image.png", orgRow.Url, "organization asset url must be untouched by the platform upload")
}

func TestService_UploadPlatformImage_ForbiddenForNonAdmin(t *testing.T) {
	t.Parallel()

	// The default test auth context is an org admin but NOT a platform admin;
	// org-level privileges must not reach the platform tier.
	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.UploadPlatformImage(ctx, &admingen.UploadPlatformImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: 100,
	}, io.NopCloser(strings.NewReader("denied")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	_, err = ti.repo.GetPlatformAssetBySHA256(ctx, mustSHA256("denied"))
	require.Error(t, err, "no platform asset row may exist after a denied upload")
}

func TestService_UploadPlatformImage_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAssetsService(t)

	_, err := ti.service.UploadPlatformImage(t.Context(), &admingen.UploadPlatformImageForm{
		SessionToken:  nil,
		ContentType:   "image/png",
		ContentLength: 100,
	}, io.NopCloser(strings.NewReader("test")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_UploadPlatformImage_UnsupportedContentType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	adminCtx := withAdmin(t, ctx)

	_, err := ti.service.UploadPlatformImage(adminCtx, &admingen.UploadPlatformImageForm{
		SessionToken:  nil,
		ContentType:   "application/zip",
		ContentLength: int64(len("fake zip content")),
	}, io.NopCloser(strings.NewReader("fake zip content")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnsupportedMedia, oopsErr.Code)
}

func TestService_UploadPlatformImage_InvalidSVGAndLengths(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0 L1 1"/></svg>`
	for _, tc := range []struct {
		name, body, mime string
		size             int64
	}{
		{"empty", "", "image/svg+xml", 0},
		{"short", svg, "image/svg+xml", int64(len(svg) - 1)},
		{"long", svg, "image/svg+xml", int64(len(svg) + 1)},
		{"oversize", strings.Repeat("x", 4*1024*1024+1), "image/svg+xml", 4*1024*1024 + 1},
		{"forged", "not SVG", "image/svg+xml", 7},
		{"hostile raster MIME", `<svg xmlns="http://www.w3.org/2000/svg"><script/></svg>`, "image/png", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAssetsService(t)
			if tc.size == 0 && tc.body != "" {
				tc.size = int64(len(tc.body))
			}
			result, err := ti.service.UploadPlatformImage(withAdmin(t, ctx), &admingen.UploadPlatformImageForm{ContentType: tc.mime, ContentLength: tc.size}, io.NopCloser(strings.NewReader(tc.body)))
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
	_, err := ti.service.UploadImage(ctx, &assets.UploadImageForm{ContentType: "image/svg+xml", ContentLength: int64(len(svg))}, io.NopCloser(strings.NewReader(svg)))
	var unsupported *oops.ShareableError
	require.ErrorAs(t, err, &unsupported)
	require.Equal(t, oops.CodeUnsupportedMedia, unsupported.Code)
	_, err = ti.service.UploadOrganizationImage(ctx, &orggen.UploadOrganizationImageForm{ContentType: "image/svg+xml", ContentLength: int64(len(svg))}, io.NopCloser(strings.NewReader(svg)))
	require.ErrorAs(t, err, &unsupported)
	require.Equal(t, oops.CodeUnsupportedMedia, unsupported.Code)
}

func TestService_UploadPlatformImage_RejectsEncodedXMLBeforeStorage(t *testing.T) {
	t.Parallel()
	hostile := `<svg xmlns="http://www.w3.org/2000/svg"><script/></svg>`
	for _, tc := range []struct {
		name, bom string
		width     int
		order     binary.AppendByteOrder
	}{
		{"utf8 BOM", "\xef\xbb\xbf", 1, binary.LittleEndian},
		{"utf16 LE BOM", "\xff\xfe", 2, binary.LittleEndian},
		{"utf16 BE BOM", "\xfe\xff", 2, binary.BigEndian},
		{"utf32 LE BOM", "\xff\xfe\x00\x00", 4, binary.LittleEndian},
		{"utf32 BE BOM", "\x00\x00\xfe\xff", 4, binary.BigEndian},
		{"utf16 LE signature", "", 2, binary.LittleEndian},
		{"utf16 BE signature", "", 2, binary.BigEndian},
		{"utf32 LE signature", "", 4, binary.LittleEndian},
		{"utf32 BE signature", "", 4, binary.BigEndian},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAssetsService(t)
			for _, leading := range []string{"", " \t\r\n"} {
				data := []byte(tc.bom)
				for _, c := range []byte(leading + hostile) {
					switch tc.width {
					case 1:
						data = append(data, c)
					case 2:
						data = tc.order.AppendUint16(data, uint16(c))
					case 4:
						data = tc.order.AppendUint32(data, uint32(c))
					}
				}
				body := string(data)
				result, err := ti.service.UploadPlatformImage(withAdmin(t, ctx), &admingen.UploadPlatformImageForm{ContentType: "image/png", ContentLength: int64(len(body))}, io.NopCloser(strings.NewReader(body)))
				require.Error(t, err)
				require.Nil(t, result)
				var unsupported *oops.ShareableError
				require.ErrorAs(t, err, &unsupported)
				require.Equal(t, oops.CodeUnsupportedMedia, unsupported.Code)
				_, err = ti.repo.GetPlatformAssetBySHA256(ctx, mustSHA256(body))
				require.ErrorIs(t, err, pgx.ErrNoRows, "encoded XML must not reach storage")
			}
		})
	}
}
