package triggers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// NotifyAssistantExecutionDenied uses only the originating Slack event and the
// assistant's trigger-owned bot credential. Neither target nor message text can
// be supplied by the model. Human revocation does not prevent this private,
// constant-text refusal; it never authorizes business access or result disclosure.
func (a *App) NotifyAssistantExecutionDenied(ctx context.Context) error {
	ac, ok := contextvalues.GetAuthContext(ctx)
	principal, bound := contextvalues.GetAssistantPrincipal(ctx)
	event, origin := contextvalues.AssistantInvocationEvent(ctx)
	if !ok || ac == nil || ac.ProjectID == nil || !bound || !origin || event == "" || a.slackClient == nil {
		return fmt.Errorf("assistant reply context unavailable")
	}
	row, err := assistantrepo.New(a.db).GetAssistantExecutionReplyOrigin(ctx, assistantrepo.GetAssistantExecutionReplyOriginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, AssistantID: principal.AssistantID, ThreadID: principal.ThreadID, EventID: event})
	if err != nil {
		return fmt.Errorf("load assistant reply origin: %w", err)
	}
	// Read only the versioned routing projection. The platform entry point
	// already validated workload authority; this does not load or grant policy.
	var payload struct {
		Execution *struct {
			Version  int       `json:"version"`
			EventID  string    `json:"event_id"`
			ThreadID uuid.UUID `json:"thread_id"`
			Slack    *struct {
				TeamID string `json:"team_id"`
				UserID string `json:"user_id"`
			} `json:"slack"`
			Identity struct {
				OrganizationID string    `json:"organization_id"`
				ProjectID      uuid.UUID `json:"project_id"`
				AssistantID    uuid.UUID `json:"assistant_id"`
				TriggerID      uuid.UUID `json:"trigger_id"`
				IssuerID       uuid.UUID `json:"issuer_id"`
				Subject        string    `json:"subject"`
			} `json:"identity"`
		} `json:"_gram_execution"`
		TeamID    string `json:"team_id"`
		UserID    string `json:"user_id"`
		ChannelID string `json:"channel_id"`
		ThreadID  string `json:"thread_id"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(row.NormalizedPayloadJson, &payload); err != nil {
		return fmt.Errorf("decode assistant reply origin: %w", err)
	}
	e := payload.Execution
	if e == nil || e.Version != 2 || e.Slack == nil || e.EventID != event || e.ThreadID != principal.ThreadID || e.Identity.AssistantID != principal.AssistantID || e.Identity.ProjectID != *ac.ProjectID || e.Identity.OrganizationID != ac.ActiveOrganizationID || !row.TriggerInstanceID.Valid || row.TriggerInstanceID.UUID != e.Identity.TriggerID || payload.TeamID != e.Slack.TeamID || payload.UserID != e.Slack.UserID || payload.ChannelID == "" {
		return fmt.Errorf("invalid assistant reply origin")
	}
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok {
		return fmt.Errorf("assistant reply actor unavailable")
	}
	issuer, subject, err := actor.Workload()
	if err != nil || issuer != e.Identity.IssuerID || subject != e.Identity.Subject {
		return fmt.Errorf("assistant reply actor mismatch")
	}
	instance, err := a.repo.GetTriggerInstanceByIDPublic(ctx, e.Identity.TriggerID)
	if err != nil {
		return fmt.Errorf("load reply trigger: %w", err)
	}
	if instance.OrganizationID != ac.ActiveOrganizationID || instance.ProjectID != *ac.ProjectID || instance.TargetKind != TargetKindAssistant || instance.TargetRef != principal.AssistantID.String() || instance.DefinitionSlug != DefinitionSlugSlack || instance.Status != "active" || !instance.EnvironmentID.Valid {
		return fmt.Errorf("assistant reply trigger unavailable")
	}
	env, err := a.loadEnvironmentMap(ctx, instance.ProjectID, instance.EnvironmentID.UUID)
	if err != nil {
		return err
	}
	token := toolconfig.CIEnvFrom(env).Get("SLACK_BOT_TOKEN")
	if token == "" {
		return fmt.Errorf("assistant reply bot credential unavailable")
	}
	thread := payload.ThreadID
	if thread == "" {
		thread = payload.Timestamp
	}
	if err := a.slackClient.NotifyExecutionDenied(ctx, token, payload.ChannelID, thread, payload.UserID); err != nil {
		return fmt.Errorf("send assistant denial reply: %w", err)
	}
	return nil
}
