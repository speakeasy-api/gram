package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameAddReminder = "platform_slack_add_reminder"

type addReminderRecurrence struct {
	Frequency string   `json:"frequency" jsonschema:"Recurrence frequency. Slack accepts daily, weekly, monthly, or yearly."`
	Weekdays  []string `json:"weekdays,omitempty" jsonschema:"Days of the week when frequency is weekly. Slack accepts lowercase names like monday, tuesday."`
}

type addReminderInput struct {
	Text       string                 `json:"text" jsonschema:"Reminder content to deliver to the user."`
	Time       string                 `json:"time" jsonschema:"When Slack should fire the reminder. Accepts a Unix timestamp (in seconds), a number of seconds from now, or a natural-language phrase like \"in 5 minutes\" or \"every Thursday\"."`
	User       *string                `json:"user,omitempty" jsonschema:"Slack user ID who receives the reminder. Defaults to the authenticated user. Slack has restricted setting reminders for other users via API."`
	TeamID     *string                `json:"team_id,omitempty" jsonschema:"Encoded team identifier, required only when calling with an org-level token."`
	Recurrence *addReminderRecurrence `json:"recurrence,omitempty" jsonschema:"Recurrence rule. Provide frequency (daily, weekly, monthly, yearly) and, for weekly frequency, weekdays."`
}

func NewAddReminderTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "add_reminder",
			Name:        toolNameAddReminder,
			Description: "Create a Slack reminder via reminders.add. Requires a user token with reminders:write (SLACK_USER_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[addReminderInput](
				core.WithPropertyEnum("frequency", "daily", "weekly", "monthly", "yearly"),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callAddReminder,
	}
}

func callAddReminder(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input addReminderInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	text, err := requireString("text", input.Text)
	if err != nil {
		return err
	}
	timeValue, err := requireString("time", input.Time)
	if err != nil {
		return err
	}

	request := map[string]any{
		"text": text,
		"time": timeValue,
	}
	setOptionalString(request, "user", input.User)
	setOptionalString(request, "team_id", input.TeamID)
	if input.Recurrence != nil {
		request["recurrence"] = input.Recurrence
	}

	body, err := client.call(ctx, "reminders.add", request, tokenRequireUser, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
