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

// PlatformImageUploader issues single-use upload tokens that each store one
// public platform image.
type PlatformImageUploader interface {
	Issue(ctx context.Context, operatorEmail string) (assets.PlatformImageUpload, error)
}

type CreateImageUploadURLInput struct{}

type ImageUploadURLOutput struct {
	UploadURL string `json:"upload_url"`
	Method    string `json:"method"`
	// Headers carries the single-use upload token. It is a header, not part
	// of the URL, so request logs never record it.
	Headers             map[string]string `json:"headers"`
	MaxBytes            int64             `json:"max_bytes"`
	AllowedContentTypes []string          `json:"allowed_content_types"`
	ExpiresAt           string            `json:"expires_at"`
	ExampleCommand      string            `json:"example_command"`
}

// createImageUploadURL is a direct write. The stored image is public and
// persistent, but no customer sees it until something references it, and the
// only staff reference, a registry entry, goes through its own approved
// proposal.
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
		Headers:             map[string]string{"Authorization": "Bearer " + upload.Token},
		AllowedContentTypes: slices.Clone(assets.PlatformImageTypes),
		ExpiresAt:           upload.ExpiresAt.UTC().Format(time.RFC3339),
		// The example reads the header from stdin through a shell builtin, so
		// the token never appears in curl's arguments or a process listing.
		ExampleCommand: "printf 'Authorization: Bearer %s' \"$UPLOAD_TOKEN\" | curl -fsS -X PUT -H @- --data-binary @icon.png '" + upload.URL + "'",
	}, nil
}

func registerImageUploadTool(server *mcp.Server, uploads PlatformImageUploader, writes WriteConfig) {
	mcp.AddTool(server, &mcp.Tool{Name: "create_image_upload_url", Title: "Create Image Upload URL", Description: "Create a single-use upload token, valid for 15 minutes, that publishes one image as a public Speakeasy asset, for example a registry entry icon. PUT the raw file bytes to upload_url with the returned headers (the token is sent as an Authorization bearer header), from a shell with access to the admin site, as example_command shows with the token in UPLOAD_TOKEN; keep the token out of command arguments. The upload is a direct write without browser approval, not a proposal for later execution. The image is publicly served immediately and stays stored once uploaded. PNG, JPEG, GIF, WebP or validated static SVG up to 4 MiB; the type is detected from the bytes and SVG is canonicalized before storage. The PUT response contains the asset ID, public URL, canonical MIME type and length. SVG allows static paths, basic shapes and local clip paths; scripts, external references and embedded images are rejected. Identical images share one asset. Requires admin:write.", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false)}}, func(ctx context.Context, _ *mcp.CallToolRequest, _ CreateImageUploadURLInput) (*mcp.CallToolResult, ImageUploadURLOutput, error) {
		out, err := createImageUploadURL(ctx, uploads, writes)
		return nil, out, err
	})
}
