package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameGetReactions = "platform_slack_get_reactions"

type getReactionsInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Slack conversation ID containing the message."`
	Timestamp string `json:"timestamp" jsonschema:"Timestamp of the message to read reactions from (e.g. \"1234567890.123456\")."`
	Full      *bool  `json:"full,omitempty" jsonschema:"Return the complete reaction list rather than the truncated default."`
}

func NewGetReactionsTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "get_reactions",
			Name:        toolNameGetReactions,
			Description: "Get the reactions on a Slack message using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[getReactionsInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callGetReactions,
	}
}

func callGetReactions(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input getReactionsInput
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
		"channel":   channelID,
		"timestamp": timestamp,
	}
	setOptionalBool(request, "full", input.Full)

	body, err := client.call(ctx, "reactions.get", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
