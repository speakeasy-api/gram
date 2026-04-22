package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameSendMessage = "platform_slack_send_message"

type sendMessageInput struct {
	ChannelID      string  `json:"channel_id" jsonschema:"Slack conversation ID to post into."`
	Text           string  `json:"text" jsonschema:"Message text to send."`
	ThreadTS       *string `json:"thread_ts,omitempty" jsonschema:"Optional thread timestamp to reply in an existing thread."`
	ReplyBroadcast *bool   `json:"reply_broadcast,omitempty" jsonschema:"Broadcast a threaded reply to the channel."`
	UnfurlLinks    *bool   `json:"unfurl_links,omitempty" jsonschema:"Control Slack link unfurling for the message."`
	UnfurlMedia    *bool   `json:"unfurl_media,omitempty" jsonschema:"Control Slack media unfurling for the message."`
}

func NewSendMessageTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "send_message",
			Name:        toolNameSendMessage,
			Description: "Send a Slack message using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[sendMessageInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSendMessage,
	}
}

func callSendMessage(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input sendMessageInput
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
	setOptionalString(request, "thread_ts", input.ThreadTS)
	setOptionalBool(request, "reply_broadcast", input.ReplyBroadcast)
	setOptionalBool(request, "unfurl_links", input.UnfurlLinks)
	setOptionalBool(request, "unfurl_media", input.UnfurlMedia)

	body, err := client.call(ctx, "chat.postMessage", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
