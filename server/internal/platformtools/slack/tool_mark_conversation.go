package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameMarkConversation = "platform_slack_mark_conversation"

type markConversationInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Slack conversation ID whose read cursor should be moved."`
	Timestamp string `json:"timestamp" jsonschema:"Timestamp of the message to mark as the most recently seen (e.g. \"1234567890.123456\")."`
}

func NewMarkConversationTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "mark_conversation",
			Name:        toolNameMarkConversation,
			Description: "Move the read cursor in a Slack conversation using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[markConversationInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callMarkConversation,
	}
}

func callMarkConversation(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input markConversationInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	timestamp, err := requireString("timestamp", input.Timestamp)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
		"ts":      timestamp,
	}

	body, err := client.call(ctx, "conversations.mark", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
