package slack

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameScheduleMessage = "platform_slack_schedule_message"

type scheduleMessageInput struct {
	ChannelID string  `json:"channel_id" jsonschema:"Slack conversation ID to post into."`
	Text      string  `json:"text" jsonschema:"Message text to schedule."`
	PostAt    int64   `json:"post_at" jsonschema:"UNIX timestamp when Slack should send the message."`
	ThreadTS  *string `json:"thread_ts,omitempty" jsonschema:"Optional thread timestamp to schedule a threaded reply."`
}

func NewScheduleMessageTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "schedule_message",
			Name:        toolNameScheduleMessage,
			Description: "Schedule a Slack message using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[scheduleMessageInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callScheduleMessage,
	}
}

func callScheduleMessage(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input scheduleMessageInput
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
	if input.PostAt <= 0 {
		return fmt.Errorf("post_at is required")
	}

	request := map[string]any{
		"channel": channelID,
		"text":    text,
		"post_at": input.PostAt,
	}
	setOptionalString(request, "thread_ts", input.ThreadTS)

	body, err := client.call(ctx, "chat.scheduleMessage", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
