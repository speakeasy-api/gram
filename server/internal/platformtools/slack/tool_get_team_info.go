package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameGetTeamInfo = "platform_slack_get_team_info"

type getTeamInfoInput struct {
	Team   *string `json:"team,omitempty" jsonschema:"Optional workspace identifier. Defaults to the team owning the calling token."`
	Domain *string `json:"domain,omitempty" jsonschema:"Optional workspace domain to look up instead of team ID. Only resolves within the same enterprise."`
}

func NewGetTeamInfoTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "get_team_info",
			Name:        toolNameGetTeamInfo,
			Description: "Get information about a Slack workspace using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[getTeamInfoInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callGetTeamInfo,
	}
}

func callGetTeamInfo(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input getTeamInfoInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "team", input.Team)
	setOptionalString(request, "domain", input.Domain)

	body, err := client.call(ctx, "team.info", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
