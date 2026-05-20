package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameJoinChannel = "platform_slack_join_channel"

type joinChannelInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Slack conversation ID to join."`
}

func NewJoinChannelTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "join_channel",
			Name:        toolNameJoinChannel,
			Description: "Join a Slack channel using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[joinChannelInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callJoinChannel,
	}
}

func callJoinChannel(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input joinChannelInput
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

	body, err := client.call(ctx, "conversations.join", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
