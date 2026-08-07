package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameInspectFile = "platform_slack_inspect_file"

// inlineImageResultKey marks the tool-result field carrying the fetched
// image as a data: URI. The assistant runner strips this field from the tool
// result and re-injects the image as vision content on the next model
// request; keep the key in sync with INLINE_IMAGE_KEY in
// agents/runner/src/vision.rs.
const inlineImageResultKey = "gram_inline_image"

type inspectFileInput struct {
	FileID string `json:"file_id" jsonschema:"Slack file ID of the image to inspect."`
}

func NewInspectFileTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "inspect_file",
			Name:        toolNameInspectFile,
			Description: "Fetch a Slack image file (png, jpeg, gif, or webp, up to 10 MiB) by file ID and attach it to the conversation so the assistant can see it. The tool result carries the file metadata; the image itself arrives as a follow-up user message. Requires the files:read scope on the server's Slack token (SLACK_BOT_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[inspectFileInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callInspectFile,
	}
}

func callInspectFile(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input inspectFileInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	fileID, err := requireString("file_id", input.FileID)
	if err != nil {
		return err
	}

	token, err := client.Token(tokenPreferBot, env)
	if err != nil {
		return err
	}

	img, err := client.FetchImageFile(ctx, fileID, token)
	if err != nil {
		return err
	}

	body, err := json.Marshal(map[string]any{
		"file": map[string]any{
			"id":       img.FileID,
			"name":     img.Name,
			"title":    img.Title,
			"mimetype": img.MimeType,
			"size":     len(img.Data),
		},
		"note": "image fetched; it is attached to the conversation as a user message for inspection",
		inlineImageResultKey: map[string]any{
			"file_id":    img.FileID,
			"mime_type":  img.MimeType,
			"size_bytes": len(img.Data),
			"data_uri":   img.DataURI(),
		},
	})
	if err != nil {
		return fmt.Errorf("encode inspect file response: %w", err)
	}
	return writeResponse(wr, body)
}
