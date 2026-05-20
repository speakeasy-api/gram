package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameGetPermalink = "platform_slack_get_permalink"

type getPermalinkInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Conversation containing the target message."`
	MessageTS string `json:"message_ts" jsonschema:"Timestamp of the message to get a permalink for."`
}

func NewChatGetPermalinkTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "get_permalink",
			Name:        toolNameGetPermalink,
			Description: "Retrieve a permanent link to a Slack message using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[getPermalinkInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callGetPermalink,
	}
}

func callGetPermalink(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input getPermalinkInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	messageTS, err := requireString("message_ts", input.MessageTS)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel":    channelID,
		"message_ts": messageTS,
	}

	// Slack documents chat.getPermalink as a GET endpoint, but it also accepts
	// form-encoded POST requests like the rest of the Web API, which keeps the
	// shared apiClient transport in play.
	body, err := client.call(ctx, "chat.getPermalink", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
