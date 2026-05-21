package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameCompleteReminder = "platform_slack_complete_reminder"

type completeReminderInput struct {
	Reminder string  `json:"reminder" jsonschema:"Slack reminder ID to mark complete (e.g. Rm12345678)."`
	TeamID   *string `json:"team_id,omitempty" jsonschema:"Encoded team identifier, required only when calling with an org-level token."`
}

func NewCompleteReminderTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "complete_reminder",
			Name:        toolNameCompleteReminder,
			Description: "Mark a Slack reminder complete via reminders.complete. Requires a user token with reminders:write (SLACK_USER_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[completeReminderInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callCompleteReminder,
	}
}

func callCompleteReminder(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input completeReminderInput
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

	body, err := client.call(ctx, "reminders.complete", request, tokenRequireUser, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
