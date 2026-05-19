package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameDeleteMessage = "platform_slack_delete_message"

type deleteMessageInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Channel containing the message to delete."`
	TS        string `json:"ts" jsonschema:"Timestamp of the message to delete."`
}

func NewChatDeleteTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := true
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "delete_message",
			Name:        toolNameDeleteMessage,
			Description: "Delete an existing Slack message using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[deleteMessageInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callDeleteMessage,
	}
}

func callDeleteMessage(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input deleteMessageInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	ts, err := requireString("ts", input.TS)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
		"ts":      ts,
	}

	body, err := client.call(ctx, "chat.delete", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
