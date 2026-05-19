package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameDeleteReminder = "platform_slack_delete_reminder"

type deleteReminderInput struct {
	Reminder string  `json:"reminder" jsonschema:"Slack reminder ID to delete (e.g. Rm12345678)."`
	TeamID   *string `json:"team_id,omitempty" jsonschema:"Encoded team identifier, required only when calling with an org-level token."`
}

func NewDeleteReminderTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := true
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "delete_reminder",
			Name:        toolNameDeleteReminder,
			Description: "Delete a Slack reminder via reminders.delete. Requires a user token with reminders:write (SLACK_USER_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[deleteReminderInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callDeleteReminder,
	}
}

func callDeleteReminder(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input deleteReminderInput
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

	body, err := client.call(ctx, "reminders.delete", request, tokenRequireUser, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
