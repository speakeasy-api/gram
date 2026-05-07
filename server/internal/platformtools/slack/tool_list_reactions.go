package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameListReactions = "platform_slack_list_reactions"

type listReactionsInput struct {
	UserID *string `json:"user_id,omitempty" jsonschema:"Slack user ID whose reactions to list. Defaults to the authenticated bot user."`
	Full   *bool   `json:"full,omitempty" jsonschema:"Return the complete reaction list rather than the truncated default."`
	Limit  *int    `json:"limit,omitempty" jsonschema:"Maximum number of items to return per page (1-200)."`
	Cursor *string `json:"cursor,omitempty" jsonschema:"Pagination cursor returned by a previous call as response_metadata.next_cursor."`
}

func NewListReactionsTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "list_reactions",
			Name:        toolNameListReactions,
			Description: "List Slack messages and files that the bot (or a specified user) has reacted to using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[listReactionsInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callListReactions,
	}
}

func callListReactions(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input listReactionsInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "user", input.UserID)
	setOptionalBool(request, "full", input.Full)
	setOptionalInt(request, "limit", input.Limit)
	setOptionalString(request, "cursor", input.Cursor)

	body, err := client.call(ctx, "reactions.list", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
