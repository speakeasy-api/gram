package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameGetUserProfileFields = "platform_slack_get_user_profile_fields"

type getUserProfileFieldsInput struct {
	UserID        *string `json:"user_id,omitempty" jsonschema:"Slack user ID to inspect. Defaults to the authed user when omitted."`
	IncludeLabels *bool   `json:"include_labels,omitempty" jsonschema:"Include labels for the workspace's custom profile fields."`
}

func NewGetUserProfileFieldsTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "get_user_profile_fields",
			Name:        toolNameGetUserProfileFields,
			Description: "Get the full profile (including custom fields) for a Slack user via users.profile.get using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[getUserProfileFieldsInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callGetUserProfileFields,
	}
}

func callGetUserProfileFields(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input getUserProfileFieldsInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "user", input.UserID)
	setOptionalBool(request, "include_labels", input.IncludeLabels)

	body, err := client.call(ctx, "users.profile.get", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
