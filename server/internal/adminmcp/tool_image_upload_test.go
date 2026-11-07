package adminmcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

type fakeImageUploads struct {
	operator string
	err      error
}

func (f *fakeImageUploads) Issue(_ context.Context, operatorEmail string) (assets.PlatformImageUpload, error) {
	f.operator = operatorEmail
	if f.err != nil {
		return assets.PlatformImageUpload{}, f.err
	}
	return assets.PlatformImageUpload{URL: "https://admin.example.test/admin/assets.uploadImage", Token: "upload-token", ExpiresAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}, nil
}

func TestCreateImageUploadURLIssuesForTheVerifiedStaffMember(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_image_upload_url")
	ctx := writeContext(t, f)
	uploads := &fakeImageUploads{}
	enabled := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationUploadPlatformImage: true}} //nolint:exhaustive // Only the upload operation is enabled.

	out, err := createImageUploadURL(ctx, uploads, enabled)
	require.NoError(t, err)
	require.Equal(t, "staff@example.test", uploads.operator)
	require.Equal(t, "PUT", out.Method)
	require.Equal(t, "2026-10-15T12:00:00Z", out.ExpiresAt)
	require.Equal(t, "https://admin.example.test/admin/assets.uploadImage", out.UploadURL)
	require.NotContains(t, out.UploadURL, "upload-token", "the token stays out of the URL")
	require.Equal(t, map[string]string{"Authorization": "Bearer upload-token"}, out.Headers)
	require.Equal(t, `printf 'Authorization: Bearer %s' "$UPLOAD_TOKEN" | curl -fsS -X PUT -H @- --data-binary @icon.png 'https://admin.example.test/admin/assets.uploadImage'`, out.ExampleCommand)
	require.NotContains(t, out.ExampleCommand, "upload-token", "the example keeps the token out of command arguments")
	require.Equal(t, "Bearer upload-token", out.Headers["Authorization"])
	require.Contains(t, out.AllowedContentTypes, "image/svg+xml")

	_, err = createImageUploadURL(ctx, uploads, WriteConfig{})
	require.ErrorIs(t, err, ErrWriteDisabled)
	_, err = createImageUploadURL(t.Context(), uploads, enabled)
	require.ErrorIs(t, err, ErrWriteIdentity, "a caller without verified staff context gets no URL")
	_, err = createImageUploadURL(ctx, &fakeImageUploads{err: errors.New("cache down")}, enabled)
	require.ErrorContains(t, err, "cache down")
}

func TestAttachWritesImageUploadNeedsAssetStorage(t *testing.T) {
	t.Parallel()
	enabled := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationUploadPlatformImage: true}} //nolint:exhaustive // Only the upload operation is enabled.
	deps := WriteDependencies{Features: &productfeatures.Client{}, Registry: mcpregistry.New(nil, nil)}

	err := AttachWrites(NewRuntime(nil, ""), &StaffOAuth{Approval: &StaffProposalApproval{}}, deps, enabled)
	require.ErrorContains(t, err, "not implemented")

	runtime := NewRuntime(nil, "")
	oauth := &StaffOAuth{Approval: &StaffProposalApproval{}}
	deps.ImageUploads = &fakeImageUploads{}
	require.NoError(t, AttachWrites(runtime, oauth, deps, enabled))
	require.True(t, runtime.writes.implements(OperationUploadPlatformImage))
	require.NotContains(t, oauth.Approval.operations, OperationUploadPlatformImage, "a direct write has no approval page")
}

func TestWriteWorkflowsSeparateDirectUploadFromProposals(t *testing.T) {
	t.Parallel()
	require.Empty(t, writeWorkflows(nil))

	uploadOnly := writeWorkflows([]WriteOperation{OperationUploadPlatformImage})
	require.Len(t, uploadOnly, 1)
	require.Contains(t, uploadOnly[0], "without browser approval")
	require.NotContains(t, uploadOnly[0], "proposal", "a direct upload alone offers no proposal workflow")

	both := writeWorkflows([]WriteOperation{OperationCreateRegistryEntry, OperationUploadPlatformImage})
	require.Len(t, both, 2)
	require.Contains(t, both[0], "separate browser approval")
	require.Equal(t, uploadOnly[0], both[1])
}

type fakePromotionReviews struct {
	actor  string
	email  string
	target catalogpromotion.Target
	err    error
}

func (f *fakePromotionReviews) StartReview(_ context.Context, target catalogpromotion.Target, actor string, actorEmail *string) (*mcpapproval.StaffReview, error) {
	f.target, f.actor, f.email = target, actor, *actorEmail
	if f.err != nil {
		return nil, f.err
	}
	return &mcpapproval.StaffReview{RequestID: "synthetic-review"}, nil
}

func TestStartPromotionReviewIsADirectWriteForVerifiedStaff(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_promotion_review")
	ctx := writeContext(t, f)
	reviews := &fakePromotionReviews{}
	enabled := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationStartRegistryPromotionReview: true}} //nolint:exhaustive // Only the review operation is enabled.
	input := CatalogPromotionTargetInput{OrganizationID: "org-synthetic", ProjectID: uuid.NewString(), MCPServerID: uuid.NewString()}

	require.True(t, OperationStartRegistryPromotionReview.direct())
	out, err := startPromotionReview(ctx, reviews, enabled, input)
	require.NoError(t, err)
	require.Equal(t, "synthetic-review", out.Review.RequestID)
	require.Equal(t, "staff@example.test", reviews.email)
	require.NotEmpty(t, reviews.actor)
	require.Equal(t, input.OrganizationID, reviews.target.OrganizationID)

	_, err = startPromotionReview(ctx, reviews, WriteConfig{}, input)
	require.ErrorIs(t, err, ErrWriteDisabled)
	_, err = startPromotionReview(t.Context(), reviews, enabled, input)
	require.ErrorIs(t, err, ErrWriteIdentity)
	_, err = startPromotionReview(ctx, reviews, enabled, CatalogPromotionTargetInput{OrganizationID: "org-synthetic", ProjectID: "not-a-uuid", MCPServerID: uuid.NewString()})
	require.Error(t, err)
	_, err = startPromotionReview(ctx, &fakePromotionReviews{err: catalogpromotion.ErrReviewNotAllowed}, enabled, input)
	require.ErrorIs(t, err, catalogpromotion.ErrReviewNotAllowed)
	_, err = startPromotionReview(ctx, &fakePromotionReviews{err: errors.New("synthetic outage")}, enabled, input)
	require.ErrorIs(t, err, errRegistryUnavailable)
}
