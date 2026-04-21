package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameReadChannelMessages = "platform_slack_read_channel_messages"

type readChannelMessagesInput struct {
	ChannelID          string  `json:"channel_id" jsonschema:"Slack conversation ID to read."`
	Cursor             *string `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous response."`
	Latest             *string `json:"latest,omitempty" jsonschema:"Only messages before this Slack timestamp are returned."`
	Oldest             *string `json:"oldest,omitempty" jsonschema:"Only messages after this Slack timestamp are returned."`
	Inclusive          *bool   `json:"inclusive,omitempty" jsonschema:"Include messages matching oldest or latest timestamps."`
	Limit              *int    `json:"limit,omitempty" jsonschema:"Maximum number of messages to return. Slack allows up to 1000."`
	IncludeAllMetadata *bool   `json:"include_all_metadata,omitempty" jsonschema:"Include all message metadata in the response."`
}

func NewReadChannelMessagesTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "read_channel_messages",
			Name:        toolNameReadChannelMessages,
			Description: "Read messages from a Slack conversation using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[readChannelMessagesInput](
				core.WithPropertyNumberRange("limit", 1, 1000),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callReadChannelMessages,
	}
}

func callReadChannelMessages(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input readChannelMessagesInput
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
	setOptionalString(request, "cursor", input.Cursor)
	setOptionalString(request, "latest", input.Latest)
	setOptionalString(request, "oldest", input.Oldest)
	setOptionalBool(request, "inclusive", input.Inclusive)
	setOptionalInt(request, "limit", input.Limit)
	setOptionalBool(request, "include_all_metadata", input.IncludeAllMetadata)

	body, err := client.call(ctx, "conversations.history", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
