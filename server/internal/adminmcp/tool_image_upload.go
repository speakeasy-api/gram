//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/assets"
)

// PlatformImageUploader issues signed URLs that each store one public
// platform image.
type PlatformImageUploader interface {
	Issue(ctx context.Context, operatorEmail string) (assets.PlatformImageUpload, error)
}

type CreateImageUploadURLInput struct{}

type ImageUploadURLOutput struct {
	UploadURL           string   `json:"upload_url"`
	Method              string   `json:"method"`
	MaxBytes            int64    `json:"max_bytes"`
	AllowedContentTypes []string `json:"allowed_content_types"`
	ExpiresAt           string   `json:"expires_at"`
	ExampleCommand      string   `json:"example_command"`
}

// createImageUploadURL is a direct write: an uploaded image is inert until
// something references it, and the only staff reference, a registry entry,
// goes through its own approved proposal.
func createImageUploadURL(ctx context.Context, uploads PlatformImageUploader, writes WriteConfig) (ImageUploadURLOutput, error) {
	authority, err := requireWriteAuthority(ctx, writes, OperationUploadPlatformImage)
	if err != nil {
		return ImageUploadURLOutput{}, err
	}
	upload, err := uploads.Issue(ctx, authority.Staff.Email)
	if err != nil {
		return ImageUploadURLOutput{}, fmt.Errorf("issue image upload URL: %w", err)
	}
	return ImageUploadURLOutput{
		UploadURL: upload.URL, Method: http.MethodPut, MaxBytes: assets.MaxFileSizeImage,
		AllowedContentTypes: slices.Clone(assets.PlatformImageTypes),
		ExpiresAt:           upload.ExpiresAt.UTC().Format(time.RFC3339),
		ExampleCommand:      "curl -fsS -X PUT --data-binary @icon.png '" + upload.URL + "'",
	}, nil
}

func registerImageUploadTool(server *mcp.Server, uploads PlatformImageUploader, writes WriteConfig) {
	mcp.AddTool(server, &mcp.Tool{Name: "create_image_upload_url", Title: "Create Image Upload URL", Description: "Create a single-use signed URL, valid for 15 minutes, that publishes one image as a public Speakeasy asset, for example a registry entry icon. PUT the raw file bytes to upload_url from a shell with access to the admin site. PNG, JPEG, GIF or WebP up to 4 MiB; the type is detected from the bytes and SVG is refused. The PUT response contains the asset ID and its public URL, usable as a registry entry's server.icons[].src (registry icons accept PNG, JPEG or WebP). Identical images share one asset. Requires admin:write.", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false), IdempotentHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, _ CreateImageUploadURLInput) (*mcp.CallToolResult, ImageUploadURLOutput, error) {
		out, err := createImageUploadURL(ctx, uploads, writes)
		return nil, out, err
	})
}
