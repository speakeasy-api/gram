package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameRemoveCanvasAccess = "platform_slack_remove_canvas_access"

type removeCanvasAccessInput struct {
	CanvasID   string   `json:"canvas_id" jsonschema:"ID of the canvas to remove access from."`
	ChannelIDs []string `json:"channel_ids,omitempty" jsonschema:"Channel IDs whose access should be revoked."`
	UserIDs    []string `json:"user_ids,omitempty" jsonschema:"User IDs whose access should be revoked."`
}

func NewRemoveCanvasAccessTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := true
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "remove_canvas_access",
			Name:        toolNameRemoveCanvasAccess,
			Description: "Revoke access from a Slack canvas via canvases.access.delete using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN. Omitting both channel_ids and user_ids removes access for all non-owners.",
			InputSchema: core.BuildInputSchema[removeCanvasAccessInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callRemoveCanvasAccess,
	}
}

func callRemoveCanvasAccess(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input removeCanvasAccessInput
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
	if len(input.ChannelIDs) > 0 {
		request["channel_ids"] = input.ChannelIDs
	}
	if len(input.UserIDs) > 0 {
		request["user_ids"] = input.UserIDs
	}

	body, err := client.call(ctx, "canvases.access.delete", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
