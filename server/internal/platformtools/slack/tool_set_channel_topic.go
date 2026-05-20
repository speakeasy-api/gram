package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameSetChannelTopic = "platform_slack_set_channel_topic"

type setChannelTopicInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Slack conversation ID whose topic should be set."`
	Topic     string `json:"topic" jsonschema:"New topic. Max 250 characters."`
}

func NewSetChannelTopicTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "set_channel_topic",
			Name:        toolNameSetChannelTopic,
			Description: "Set the topic of a Slack conversation using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[setChannelTopicInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSetChannelTopic,
	}
}

func callSetChannelTopic(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input setChannelTopicInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	topic, err := requireString("topic", input.Topic)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
		"topic":   topic,
	}

	body, err := client.call(ctx, "conversations.setTopic", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
