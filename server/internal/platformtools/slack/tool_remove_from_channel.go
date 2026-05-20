package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameRemoveFromChannel = "platform_slack_remove_from_channel"

type removeFromChannelInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Slack conversation ID to remove the user from."`
	UserID    string `json:"user_id" jsonschema:"Slack user ID to remove from the conversation."`
}

func NewRemoveFromChannelTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := true
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "remove_from_channel",
			Name:        toolNameRemoveFromChannel,
			Description: "Remove a user from a Slack conversation using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[removeFromChannelInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callRemoveFromChannel,
	}
}

func callRemoveFromChannel(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input removeFromChannelInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	userID, err := requireString("user_id", input.UserID)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
		"user":    userID,
	}

	body, err := client.call(ctx, "conversations.kick", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
