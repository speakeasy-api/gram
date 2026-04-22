package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameReadUserProfile = "platform_slack_read_user_profile"

type readUserProfileInput struct {
	UserID        string `json:"user_id" jsonschema:"Slack user ID to inspect."`
	IncludeLocale *bool  `json:"include_locale,omitempty" jsonschema:"Include locale in the returned profile data when available."`
}

func NewReadUserProfileTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "read_user_profile",
			Name:        toolNameReadUserProfile,
			Description: "Read a Slack user profile using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[readUserProfileInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callReadUserProfile,
	}
}

func callReadUserProfile(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input readUserProfileInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	userID, err := requireString("user_id", input.UserID)
	if err != nil {
		return err
	}

	request := map[string]any{
		"user": userID,
	}
	setOptionalBool(request, "include_locale", input.IncludeLocale)

	body, err := client.call(ctx, "users.info", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
