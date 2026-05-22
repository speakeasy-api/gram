package slack

import (
	"context"
	"io"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameListUserConversations = "platform_slack_list_user_conversations"

type listUserConversationsInput struct {
	UserID          *string  `json:"user_id,omitempty" jsonschema:"Slack user ID to inspect. Defaults to the authed user when omitted."`
	ChannelTypes    []string `json:"channel_types,omitempty" jsonschema:"Conversation types to include. Allowed values are public_channel, private_channel, mpim, and im."`
	Cursor          *string  `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous response."`
	Limit           *int     `json:"limit,omitempty" jsonschema:"Maximum number of conversations to fetch per page. Slack allows up to 1000."`
	ExcludeArchived *bool    `json:"exclude_archived,omitempty" jsonschema:"Exclude archived conversations from the response."`
}

func NewListUserConversationsTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "list_user_conversations",
			Name:        toolNameListUserConversations,
			Description: "List conversations the calling or supplied Slack user is a member of via users.conversations using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[listUserConversationsInput](
				core.WithPropertyNumberRange("limit", 1, 1000),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callListUserConversations,
	}
}

func callListUserConversations(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input listUserConversationsInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "user", input.UserID)
	setOptionalString(request, "cursor", input.Cursor)
	setOptionalInt(request, "limit", input.Limit)
	setOptionalBool(request, "exclude_archived", input.ExcludeArchived)
	if len(input.ChannelTypes) > 0 {
		request["types"] = strings.Join(input.ChannelTypes, ",")
	}

	body, err := client.call(ctx, "users.conversations", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
