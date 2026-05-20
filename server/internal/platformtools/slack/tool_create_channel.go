package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameCreateChannel = "platform_slack_create_channel"

type createChannelInput struct {
	Name      string  `json:"name" jsonschema:"Channel name. Slack lowercases the name and rejects characters outside letters, numbers, hyphens, and underscores; max 80 characters."`
	IsPrivate *bool   `json:"is_private,omitempty" jsonschema:"Create a private channel instead of a public one."`
	TeamID    *string `json:"team_id,omitempty" jsonschema:"Workspace ID to create the channel in. Required only when using an org-level token."`
}

func NewCreateChannelTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "create_channel",
			Name:        toolNameCreateChannel,
			Description: "Create a new Slack channel using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[createChannelInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callCreateChannel,
	}
}

func callCreateChannel(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input createChannelInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	name, err := requireString("name", input.Name)
	if err != nil {
		return err
	}

	request := map[string]any{
		"name": name,
	}
	setOptionalBool(request, "is_private", input.IsPrivate)
	setOptionalString(request, "team_id", input.TeamID)

	body, err := client.call(ctx, "conversations.create", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
