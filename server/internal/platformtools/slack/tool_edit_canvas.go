package slack

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameEditCanvas = "platform_slack_edit_canvas"

type canvasEditChange struct {
	Operation       string                 `json:"operation" jsonschema:"Edit operation. One of insert_after, insert_before, insert_at_start, insert_at_end, replace, delete."`
	SectionID       *string                `json:"section_id,omitempty" jsonschema:"Target section ID. Required for insert_after, insert_before, replace and delete."`
	DocumentContent *canvasDocumentContent `json:"document_content,omitempty" jsonschema:"Content to insert or use as replacement. Omitted for delete."`
}

type editCanvasInput struct {
	CanvasID string             `json:"canvas_id" jsonschema:"ID of the canvas to edit."`
	Changes  []canvasEditChange `json:"changes" jsonschema:"Ordered list of edit operations to apply to the canvas."`
}

func NewEditCanvasTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "edit_canvas",
			Name:        toolNameEditCanvas,
			Description: "Apply edit operations to a Slack canvas using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[editCanvasInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callEditCanvas,
	}
}

func callEditCanvas(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input editCanvasInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	canvasID, err := requireString("canvas_id", input.CanvasID)
	if err != nil {
		return err
	}
	if len(input.Changes) == 0 {
		return fmt.Errorf("changes is required")
	}

	request := map[string]any{
		"canvas_id": canvasID,
		"changes":   input.Changes,
	}

	body, err := client.call(ctx, "canvases.edit", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
