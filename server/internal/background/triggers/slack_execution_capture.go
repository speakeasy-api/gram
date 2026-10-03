package triggers

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
)

// SlackExecutionSelection is trusted ingress state carried through task retries.
// Denial is deferred to configured execution capture so never-configured legacy
// assistants retain their existing behavior. It never permits actor reselection.
type SlackExecutionSelection struct {
	HumanUserID    string
	FallbackReason string
	Denied         bool
	Delegation     *SlackExecutionDelegation
}

// SlackExecutionDelegation pins source mapping authority, not business policy.
// Keep triggers independent of the assistantidentity service package: assistants
// translates these pins into its execution envelope at persistence.
type SlackExecutionDelegation struct {
	TeamID               string
	UserID               string
	MembershipID         uuid.UUID
	MappingID            uuid.UUID
	MappingRevision      int64
	ConnectionGeneration uuid.UUID
}

type slackExecutionMappings interface {
	GetSlackExecutionMapping(context.Context, identityrepo.GetSlackExecutionMappingParams) (identityrepo.GetSlackExecutionMappingRow, error)
	SlackExecutionWorkspaceDisconnected(context.Context, identityrepo.SlackExecutionWorkspaceDisconnectedParams) (bool, error)
}

func captureSlackExecution(ctx context.Context, db identityrepo.DBTX, org string, event any) *SlackExecutionSelection {
	return selectSlackExecution(ctx, identityrepo.New(db), org, event)
}

func selectSlackExecution(ctx context.Context, mappings slackExecutionMappings, org string, event any) *SlackExecutionSelection {
	slack, ok := event.(slackTriggerEvent)
	if !ok {
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: "", Denied: true, Delegation: nil}
	}
	if slack.BotID != "" || (slack.AppID != "" && slack.EventType != "block_actions") || slack.Subtype == "bot_message" {
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: "slack_non_user_event", Denied: false, Delegation: nil}
	}
	if slack.TeamID == "" || slack.UserID == "" {
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: "slack_sender_absent", Denied: false, Delegation: nil}
	}
	row, err := mappings.GetSlackExecutionMapping(ctx, identityrepo.GetSlackExecutionMappingParams{OrganizationID: org, SlackTeamID: slack.TeamID, SlackUserID: slack.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		// Disconnect deletes memberships but retains the workspace tombstone.
		disconnected, lookupErr := mappings.SlackExecutionWorkspaceDisconnected(ctx, identityrepo.SlackExecutionWorkspaceDisconnectedParams{OrganizationID: org, SlackTeamID: slack.TeamID})
		if lookupErr == nil && disconnected {
			return &SlackExecutionSelection{HumanUserID: "", FallbackReason: "", Denied: true, Delegation: nil}
		}
		if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			return &SlackExecutionSelection{HumanUserID: "", FallbackReason: "slack_mapping_unavailable", Denied: false, Delegation: nil}
		}
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: "slack_mapping_absent", Denied: false, Delegation: nil}
	}
	if err != nil {
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: "slack_mapping_unavailable", Denied: false, Delegation: nil}
	}
	if !row.Eligible {
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: "", Denied: true, Delegation: nil}
	}
	return &SlackExecutionSelection{HumanUserID: row.UserID, FallbackReason: "", Denied: false, Delegation: &SlackExecutionDelegation{TeamID: slack.TeamID, UserID: slack.UserID, MembershipID: row.MembershipID, MappingID: row.MappingID, MappingRevision: row.MappingRevision, ConnectionGeneration: row.ConnectionGeneration}}
}
