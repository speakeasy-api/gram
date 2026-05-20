package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameListScheduledMessages = "platform_slack_list_scheduled_messages"

type listScheduledMessagesInput struct {
	ChannelID *string `json:"channel_id,omitempty" jsonschema:"Optional channel to filter scheduled messages by."`
	Cursor    *string `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous chat.scheduledMessages.list response."`
	Latest    *string `json:"latest,omitempty" jsonschema:"Unix timestamp for the latest scheduled time to return."`
	Oldest    *string `json:"oldest,omitempty" jsonschema:"Unix timestamp for the earliest scheduled time to return."`
	Limit     *int    `json:"limit,omitempty" jsonschema:"Maximum number of scheduled messages to return."`
	TeamID    *string `json:"team_id,omitempty" jsonschema:"Encoded team id to scope the listing to. Required when calling with an org-level token."`
}

func NewChatListScheduledMessagesTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "list_scheduled_messages",
			Name:        toolNameListScheduledMessages,
			Description: "List Slack scheduled messages queued by the calling app using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[listScheduledMessagesInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callListScheduledMessages,
	}
}

func callListScheduledMessages(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input listScheduledMessagesInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "channel", input.ChannelID)
	setOptionalString(request, "cursor", input.Cursor)
	setOptionalString(request, "latest", input.Latest)
	setOptionalString(request, "oldest", input.Oldest)
	setOptionalString(request, "team_id", input.TeamID)
	setOptionalInt(request, "limit", input.Limit)

	body, err := client.call(ctx, "chat.scheduledMessages.list", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
