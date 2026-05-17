package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameUnarchiveChannel = "platform_slack_unarchive_channel"

type unarchiveChannelInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Slack conversation ID to unarchive."`
}

func NewUnarchiveChannelTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "unarchive_channel",
			Name:        toolNameUnarchiveChannel,
			Description: "Unarchive a Slack conversation using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN. Slack currently rejects bot tokens for this method; configure SLACK_USER_TOKEN if the bot token call fails.",
			InputSchema: core.BuildInputSchema[unarchiveChannelInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callUnarchiveChannel,
	}
}

func callUnarchiveChannel(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input unarchiveChannelInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
	}

	body, err := client.call(ctx, "conversations.unarchive", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
