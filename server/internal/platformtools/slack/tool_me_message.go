package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameMeMessage = "platform_slack_me_message"

type meMessageInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Channel, private group, or IM channel to post the /me message into."`
	Text      string `json:"text" jsonschema:"Text of the /me message."`
}

func NewChatMeMessageTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "me_message",
			Name:        toolNameMeMessage,
			Description: "Share a Slack /me message as the authenticated user using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[meMessageInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callMeMessage,
	}
}

func callMeMessage(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input meMessageInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	text, err := requireString("text", input.Text)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
		"text":    text,
	}

	body, err := client.call(ctx, "chat.meMessage", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
