package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameListUsergroups = "platform_slack_list_usergroups"

type listUsergroupsInput struct {
	IncludeCount    *bool   `json:"include_count,omitempty" jsonschema:"Include the user count for each user group."`
	IncludeDisabled *bool   `json:"include_disabled,omitempty" jsonschema:"Include disabled user groups in the result."`
	IncludeUsers    *bool   `json:"include_users,omitempty" jsonschema:"Include the list of users for each user group."`
	TeamID          *string `json:"team_id,omitempty" jsonschema:"Encoded team ID. Required when calling with an org-level token."`
}

func NewListUsergroupsTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "list_usergroups",
			Name:        toolNameListUsergroups,
			Description: "List the user groups in the Slack workspace using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[listUsergroupsInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callListUsergroups,
	}
}

func callListUsergroups(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input listUsergroupsInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalBool(request, "include_count", input.IncludeCount)
	setOptionalBool(request, "include_disabled", input.IncludeDisabled)
	setOptionalBool(request, "include_users", input.IncludeUsers)
	setOptionalString(request, "team_id", input.TeamID)

	body, err := client.call(ctx, "usergroups.list", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
