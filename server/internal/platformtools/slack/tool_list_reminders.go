package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameListReminders = "platform_slack_list_reminders"

type listRemindersInput struct {
	TeamID *string `json:"team_id,omitempty" jsonschema:"Encoded team identifier, required only when calling with an org-level token."`
}

func NewListRemindersTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "list_reminders",
			Name:        toolNameListReminders,
			Description: "List reminders for the authenticated Slack user via reminders.list. Requires a user token with reminders:read (SLACK_USER_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[listRemindersInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callListReminders,
	}
}

func callListReminders(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input listRemindersInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "team_id", input.TeamID)

	body, err := client.call(ctx, "reminders.list", request, tokenRequireUser, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
