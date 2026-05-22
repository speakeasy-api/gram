package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameListChannelMembers = "platform_slack_list_channel_members"

type listChannelMembersInput struct {
	ChannelID string  `json:"channel_id" jsonschema:"Slack conversation ID whose members should be listed."`
	Cursor    *string `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous response."`
	Limit     *int    `json:"limit,omitempty" jsonschema:"Maximum number of members to return. Slack default is 100."`
}

func NewListChannelMembersTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "list_channel_members",
			Name:        toolNameListChannelMembers,
			Description: "List the members of a Slack conversation using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[listChannelMembersInput](
				core.WithPropertyNumberRange("limit", 1, 1000),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callListChannelMembers,
	}
}

func callListChannelMembers(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input listChannelMembersInput
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
	setOptionalInt(request, "limit", input.Limit)

	body, err := client.call(ctx, "conversations.members", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
