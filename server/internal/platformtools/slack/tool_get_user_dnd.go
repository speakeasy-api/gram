package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameGetUserDnd = "platform_slack_get_user_dnd"

type getUserDndInput struct {
	UserID *string `json:"user_id,omitempty" jsonschema:"Slack user ID to inspect. Defaults to the authed user when omitted."`
}

func NewGetUserDndTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "get_user_dnd",
			Name:        toolNameGetUserDnd,
			Description: "Get a Slack user's Do Not Disturb status and schedule via dnd.info using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[getUserDndInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callGetUserDnd,
	}
}

func callGetUserDnd(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input getUserDndInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "user", input.UserID)

	body, err := client.call(ctx, "dnd.info", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
