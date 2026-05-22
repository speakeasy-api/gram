package slack

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameGetTeamDnd = "platform_slack_get_team_dnd"

type getTeamDndInput struct {
	UserIDs []string `json:"user_ids" jsonschema:"Slack user IDs whose Do Not Disturb status should be returned."`
}

func NewGetTeamDndTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "get_team_dnd",
			Name:        toolNameGetTeamDnd,
			Description: "Get the Do Not Disturb status for multiple Slack users via dnd.teamInfo using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[getTeamDndInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callGetTeamDnd,
	}
}

func callGetTeamDnd(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input getTeamDndInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	if len(input.UserIDs) == 0 {
		return fmt.Errorf("user_ids is required")
	}

	request := map[string]any{
		"users": input.UserIDs,
	}

	body, err := client.call(ctx, "dnd.teamInfo", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
