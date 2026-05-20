package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameGetUserPresence = "platform_slack_get_user_presence"

type getUserPresenceInput struct {
	UserID *string `json:"user_id,omitempty" jsonschema:"Slack user ID to inspect. Defaults to the authed user when omitted."`
}

func NewGetUserPresenceTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "get_user_presence",
			Name:        toolNameGetUserPresence,
			Description: "Get the presence (active/away) of a Slack user via users.getPresence using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[getUserPresenceInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callGetUserPresence,
	}
}

func callGetUserPresence(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input getUserPresenceInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "user", input.UserID)

	body, err := client.call(ctx, "users.getPresence", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
