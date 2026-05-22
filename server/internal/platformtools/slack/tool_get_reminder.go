package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameGetReminder = "platform_slack_get_reminder"

type getReminderInput struct {
	Reminder string  `json:"reminder" jsonschema:"Slack reminder ID to fetch (e.g. Rm12345678)."`
	TeamID   *string `json:"team_id,omitempty" jsonschema:"Encoded team identifier, required only when calling with an org-level token."`
}

func NewGetReminderTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "get_reminder",
			Name:        toolNameGetReminder,
			Description: "Fetch a Slack reminder via reminders.info. Requires a user token with reminders:read (SLACK_USER_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[getReminderInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callGetReminder,
	}
}

func callGetReminder(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input getReminderInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	reminder, err := requireString("reminder", input.Reminder)
	if err != nil {
		return err
	}

	request := map[string]any{
		"reminder": reminder,
	}
	setOptionalString(request, "team_id", input.TeamID)

	body, err := client.call(ctx, "reminders.info", request, tokenRequireUser, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
