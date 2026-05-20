package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameLookupUserByEmail = "platform_slack_lookup_user_by_email"

type lookupUserByEmailInput struct {
	Email string `json:"email" jsonschema:"Email address registered to the Slack workspace user."`
}

func NewLookupUserByEmailTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "lookup_user_by_email",
			Name:        toolNameLookupUserByEmail,
			Description: "Look up a Slack workspace user by email address via users.lookupByEmail using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[lookupUserByEmailInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callLookupUserByEmail,
	}
}

func callLookupUserByEmail(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input lookupUserByEmailInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	email, err := requireString("email", input.Email)
	if err != nil {
		return err
	}

	request := map[string]any{
		"email": email,
	}

	body, err := client.call(ctx, "users.lookupByEmail", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
