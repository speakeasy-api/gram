//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/assets"
)

const createOrgImageUploadURLToolName = "create_org_image_upload_url"

const createOrgImageUploadURLDescription = "Create a single-use upload token, valid for 15 minutes, that stores one image as an asset of your organization, for example an MCP server's install-page logo. " +
	"PUT the raw file bytes to upload_url with the returned headers (the token is sent as an Authorization bearer header), as example_command shows with the token in UPLOAD_TOKEN; keep the token out of command arguments and never show it to anyone. " +
	"PNG, JPEG, GIF or WebP up to 4 MiB; the type is detected from the bytes and SVG is refused. " +
	"The PUT response contains the asset ID and the image's public URL. Use the asset ID wherever an organization image is asked for, such as update_mcp_metadata's logo_asset_id. Identical images share one asset. " +
	"Nothing changes until something uses the asset."

// createOrgImageUploadURLMeta is shared by the live and unavailable
// registrations. Uploading means running a command with the caller's own
// shell, which only an external client has, and the image belongs to the
// organization rather than to one project.
var createOrgImageUploadURLMeta = ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}

// OrganizationImageUploader issues single-use upload tokens that each store
// one image owned by an organization.
type OrganizationImageUploader interface {
	Issue(ctx context.Context, organizationID, userID string) (assets.OrganizationImageUpload, error)
}

type CreateOrgImageUploadURLInput struct{}

type OrgImageUploadURLOutput struct {
	UploadURL string `json:"upload_url"`
	Method    string `json:"method"`
	// Headers carries the single-use upload token. It is a header, not part of
	// the URL, so request logs never record it.
	Headers             map[string]string `json:"headers"`
	MaxBytes            int64             `json:"max_bytes"`
	AllowedContentTypes []string          `json:"allowed_content_types"`
	ExpiresAt           string            `json:"expires_at"`
	ExampleCommand      string            `json:"example_command"`
}

// WithOrganizationImageUploads serves create_org_image_upload_url. A nil
// uploader leaves the tool registered as unavailable.
func (r *PostgresReader) WithOrganizationImageUploads(uploads OrganizationImageUploader) *PostgresReader {
	if r != nil && uploads != nil {
		r.organizationImageUploads = uploads
	}
	return r
}

func createOrgImageUploadURL(ctx context.Context, uploads OrganizationImageUploader) (OrgImageUploadURLOutput, error) {
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return OrgImageUploadURLOutput{}, err
	}
	// The organization and uploader come from the authenticated caller, never
	// from input, so a token can only ever store an image for the caller's
	// own organization.
	upload, err := uploads.Issue(ctx, principal.OrganizationID, principal.UserID)
	if err != nil {
		return OrgImageUploadURLOutput{}, fmt.Errorf("issue organization image upload URL: %w", err)
	}
	return OrgImageUploadURLOutput{
		UploadURL: upload.URL, Method: http.MethodPut, MaxBytes: assets.MaxFileSizeImage,
		Headers:             map[string]string{"Authorization": "Bearer " + upload.Token},
		AllowedContentTypes: slices.Clone(assets.PlatformImageTypes),
		ExpiresAt:           upload.ExpiresAt.UTC().Format(time.RFC3339),
		// The example reads the header from stdin through a shell builtin, so
		// the token never appears in curl's arguments or a process listing.
		ExampleCommand: "printf 'Authorization: Bearer %s' \"$UPLOAD_TOKEN\" | curl -fsS -X PUT -H @- --data-binary @logo.png '" + upload.URL + "'",
	}, nil
}

func registerOrgImageUploadTool(reg *Registrar, uploads OrganizationImageUploader) {
	tool := &mcp.Tool{
		Name:        createOrgImageUploadURLToolName,
		Title:       "Create Organization Image Upload URL",
		Description: createOrgImageUploadURLDescription,
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false)},
	}
	if uploads == nil {
		tool.Description = "Create a single-use upload URL that stores one image as an asset of your organization. This is not switched on for your organization yet."
		addTool(reg, tool, createOrgImageUploadURLMeta, unavailableTool("organization_image_uploads"))
		return
	}
	addTool(reg, tool, createOrgImageUploadURLMeta, func(ctx context.Context, _ *mcp.CallToolRequest, _ CreateOrgImageUploadURLInput) (*mcp.CallToolResult, OrgImageUploadURLOutput, error) {
		out, err := createOrgImageUploadURL(ctx, uploads)
		return nil, out, err
	})
}
