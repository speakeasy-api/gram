package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameListUsergroupMembers = "platform_slack_list_usergroup_members"

type listUsergroupMembersInput struct {
	Usergroup       string  `json:"usergroup" jsonschema:"Encoded user group ID (e.g. \"S0604QSJC\")."`
	IncludeDisabled *bool   `json:"include_disabled,omitempty" jsonschema:"Include members of disabled user groups."`
	TeamID          *string `json:"team_id,omitempty" jsonschema:"Encoded team ID. Required when calling with an org-level token."`
}

func NewListUsergroupMembersTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "list_usergroup_members",
			Name:        toolNameListUsergroupMembers,
			Description: "List the members of a Slack user group using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[listUsergroupMembersInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callListUsergroupMembers,
	}
}

func callListUsergroupMembers(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input listUsergroupMembersInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	usergroup, err := requireString("usergroup", input.Usergroup)
	if err != nil {
		return err
	}

	request := map[string]any{
		"usergroup": usergroup,
	}
	setOptionalBool(request, "include_disabled", input.IncludeDisabled)
	setOptionalString(request, "team_id", input.TeamID)

	body, err := client.call(ctx, "usergroups.users.list", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
