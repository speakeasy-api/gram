package platformmcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets"
)

type recordingOrganizationImageUploader struct {
	organizationID, userID string
}

func (u *recordingOrganizationImageUploader) Issue(_ context.Context, organizationID, userID string) (assets.OrganizationImageUpload, error) {
	u.organizationID, u.userID = organizationID, userID
	return assets.OrganizationImageUpload{URL: "https://api.example.test" + assets.OrganizationImageUploadPath, Token: "secret-token", ExpiresAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}, nil
}

func orgImageUploadDescriptor(t *testing.T, uploads OrganizationImageUploader) Descriptor {
	t.Helper()
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil))
	registerOrgImageUploadTool(reg, uploads)
	for _, descriptor := range reg.Descriptors() {
		if descriptor.Name == createOrgImageUploadURLToolName {
			return descriptor
		}
	}
	t.Fatalf("%s was not registered", createOrgImageUploadURLToolName)
	return Descriptor{}
}

func TestCreateOrgImageUploadURLBindsTheCallersOrganization(t *testing.T) {
	t.Parallel()
	uploads := &recordingOrganizationImageUploader{}
	descriptor := orgImageUploadDescriptor(t, uploads)
	require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
	require.Equal(t, externalOnly, descriptor.Meta.Audiences, "uploading needs the caller's own shell")
	require.Equal(t, ProjectScopeNone, descriptor.Meta.ProjectScope, "the image belongs to the organization, not a project")

	ctx := ContextWithPrincipal(t.Context(), Principal{UserID: "user_admin", OrganizationID: "org_caller"})
	// The input names no organization, so a caller cannot aim a token at
	// another one.
	result, err := descriptor.Invoke(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	output, ok := result.(OrgImageUploadURLOutput)
	require.True(t, ok)
	require.Equal(t, "org_caller", uploads.organizationID)
	require.Equal(t, "user_admin", uploads.userID)

	require.Equal(t, "PUT", output.Method)
	require.Equal(t, "https://api.example.test/rpc/organizationAssets.uploadImage", output.UploadURL)
	require.Equal(t, map[string]string{"Authorization": "Bearer secret-token"}, output.Headers)
	require.Equal(t, int64(assets.MaxFileSizeImage), output.MaxBytes)
	require.Equal(t, []string{"image/png", "image/jpeg", "image/gif", "image/webp"}, output.AllowedContentTypes)
	require.Equal(t, "2026-01-02T03:04:05Z", output.ExpiresAt)
	require.NotContains(t, output.UploadURL, "secret-token")
	require.NotContains(t, output.ExampleCommand, "secret-token", "the example keeps the token out of argv")
	require.Contains(t, output.ExampleCommand, "$UPLOAD_TOKEN")
}

func TestCreateOrgImageUploadURLUnavailableWithoutUploads(t *testing.T) {
	t.Parallel()
	descriptor := orgImageUploadDescriptor(t, nil)
	require.Equal(t, createOrgImageUploadURLMeta, descriptor.Meta)
	ctx := ContextWithPrincipal(t.Context(), Principal{UserID: "user_admin", OrganizationID: "org_caller"})
	_, err := descriptor.Invoke(ctx, json.RawMessage(`{}`))
	require.Error(t, err)
}
