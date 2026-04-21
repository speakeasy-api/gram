package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameReadThreadMessages = "platform_slack_read_thread_messages"

type readThreadMessagesInput struct {
	ChannelID          string  `json:"channel_id" jsonschema:"Slack conversation ID containing the thread."`
	ThreadTS           string  `json:"thread_ts" jsonschema:"Slack timestamp for the parent message or any reply in the thread."`
	Cursor             *string `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous response."`
	Latest             *string `json:"latest,omitempty" jsonschema:"Only messages before this Slack timestamp are returned."`
	Oldest             *string `json:"oldest,omitempty" jsonschema:"Only messages after this Slack timestamp are returned."`
	Inclusive          *bool   `json:"inclusive,omitempty" jsonschema:"Include messages matching oldest or latest timestamps."`
	Limit              *int    `json:"limit,omitempty" jsonschema:"Maximum number of messages to return. Slack allows up to 1000."`
	IncludeAllMetadata *bool   `json:"include_all_metadata,omitempty" jsonschema:"Include all message metadata in the response."`
}

func NewReadThreadMessagesTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "read_thread_messages",
			Name:        toolNameReadThreadMessages,
			Description: "Read a Slack thread using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[readThreadMessagesInput](
				core.WithPropertyNumberRange("limit", 1, 1000),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callReadThreadMessages,
	}
}

func callReadThreadMessages(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input readThreadMessagesInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	threadTS, err := requireString("thread_ts", input.ThreadTS)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
		"ts":      threadTS,
	}
	setOptionalString(request, "cursor", input.Cursor)
	setOptionalString(request, "latest", input.Latest)
	setOptionalString(request, "oldest", input.Oldest)
	setOptionalBool(request, "inclusive", input.Inclusive)
	setOptionalInt(request, "limit", input.Limit)
	setOptionalBool(request, "include_all_metadata", input.IncludeAllMetadata)

	body, err := client.call(ctx, "conversations.replies", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
