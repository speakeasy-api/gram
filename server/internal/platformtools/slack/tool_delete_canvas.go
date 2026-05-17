package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameDeleteCanvas = "platform_slack_delete_canvas"

type deleteCanvasInput struct {
	CanvasID string `json:"canvas_id" jsonschema:"ID of the canvas to delete."`
}

func NewDeleteCanvasTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := true
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "delete_canvas",
			Name:        toolNameDeleteCanvas,
			Description: "Delete a Slack canvas using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[deleteCanvasInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callDeleteCanvas,
	}
}

func callDeleteCanvas(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input deleteCanvasInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	canvasID, err := requireString("canvas_id", input.CanvasID)
	if err != nil {
		return err
	}

	request := map[string]any{
		"canvas_id": canvasID,
	}

	body, err := client.call(ctx, "canvases.delete", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
