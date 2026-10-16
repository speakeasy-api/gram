package adminmcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets"
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
	return assets.PlatformImageUpload{URL: "https://admin.example.test/admin/assets.uploadImage/token", ExpiresAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}, nil
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
	require.Contains(t, out.ExampleCommand, out.UploadURL)
	require.NotContains(t, out.AllowedContentTypes, "image/svg+xml")

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
