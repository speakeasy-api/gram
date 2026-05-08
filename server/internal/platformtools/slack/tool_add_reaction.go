package slack

import (
	"context"
	"io"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameAddReaction = "platform_slack_add_reaction"

type addReactionInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Slack conversation ID containing the message."`
	Timestamp string `json:"timestamp" jsonschema:"Timestamp of the message to react to (e.g. \"1234567890.123456\")."`
	Name      string `json:"name" jsonschema:"Reaction (emoji) name without surrounding colons (e.g. \"thumbsup\")."`
}

func NewAddReactionTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "add_reaction",
			Name:        toolNameAddReaction,
			Description: "Add an emoji reaction to a Slack message using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[addReactionInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callAddReaction,
	}
}

func callAddReaction(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input addReactionInput
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
	name, err := requireString("name", input.Name)
	if err != nil {
		return err
	}
	name = strings.Trim(name, ":")

	request := map[string]any{
		"channel":   channelID,
		"timestamp": timestamp,
		"name":      name,
	}

	body, err := client.call(ctx, "reactions.add", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
