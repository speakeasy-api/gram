package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameDeleteScheduledMessage = "platform_slack_delete_scheduled_message"

type deleteScheduledMessageInput struct {
	ChannelID          string `json:"channel_id" jsonschema:"Channel the scheduled message is queued for."`
	ScheduledMessageID string `json:"scheduled_message_id" jsonschema:"scheduled_message_id returned from chat.scheduleMessage."`
}

func NewChatDeleteScheduledMessageTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := true
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "delete_scheduled_message",
			Name:        toolNameDeleteScheduledMessage,
			Description: "Cancel a Slack scheduled message before it is sent, using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[deleteScheduledMessageInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callDeleteScheduledMessage,
	}
}

func callDeleteScheduledMessage(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input deleteScheduledMessageInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	scheduledMessageID, err := requireString("scheduled_message_id", input.ScheduledMessageID)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel":              channelID,
		"scheduled_message_id": scheduledMessageID,
	}

	body, err := client.call(ctx, "chat.deleteScheduledMessage", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
