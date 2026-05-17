package slack

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameSetCanvasAccess = "platform_slack_set_canvas_access"

type setCanvasAccessInput struct {
	CanvasID    string   `json:"canvas_id" jsonschema:"ID of the canvas to update access on."`
	AccessLevel string   `json:"access_level" jsonschema:"Access level to grant. One of read, write, none."`
	ChannelIDs  []string `json:"channel_ids,omitempty" jsonschema:"Channel IDs to grant access to."`
	UserIDs     []string `json:"user_ids,omitempty" jsonschema:"User IDs to grant access to."`
}

func NewSetCanvasAccessTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "set_canvas_access",
			Name:        toolNameSetCanvasAccess,
			Description: "Set access on a Slack canvas via canvases.access.set using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[setCanvasAccessInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSetCanvasAccess,
	}
}

func callSetCanvasAccess(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input setCanvasAccessInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	canvasID, err := requireString("canvas_id", input.CanvasID)
	if err != nil {
		return err
	}
	accessLevel, err := requireString("access_level", input.AccessLevel)
	if err != nil {
		return err
	}
	if len(input.ChannelIDs) == 0 && len(input.UserIDs) == 0 {
		return fmt.Errorf("at least one of channel_ids or user_ids is required")
	}

	request := map[string]any{
		"canvas_id":    canvasID,
		"access_level": accessLevel,
	}
	if len(input.ChannelIDs) > 0 {
		request["channel_ids"] = input.ChannelIDs
	}
	if len(input.UserIDs) > 0 {
		request["user_ids"] = input.UserIDs
	}

	body, err := client.call(ctx, "canvases.access.set", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
