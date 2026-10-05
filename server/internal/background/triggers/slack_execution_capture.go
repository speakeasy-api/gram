package triggers

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
)

// Slack execution fallback reasons are persisted with the execution envelope.
const (
	SlackExecutionFallbackNonUserEvent       = "slack_non_user_event"
	SlackExecutionFallbackSenderAbsent       = "slack_sender_absent"
	SlackExecutionFallbackMappingAbsent      = "slack_mapping_absent"
	SlackExecutionFallbackMappingUnavailable = "slack_mapping_unavailable"
)

// SlackExecutionSelection is trusted ingress state carried through task retries.
// Denial is deferred to configured execution capture so assistants with a
// NeverConfigured binding retain their existing behavior. It never permits
// actor reselection.
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

func selectSlackExecution(ctx context.Context, mappings slackExecutionMappings, org string, event any) *SlackExecutionSelection {
	slack, ok := event.(slackTriggerEvent)
	if !ok {
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: "", Denied: true, Delegation: nil}
	}
	if !slackExecutionHasActor(slack) || slack.BotID != "" || (slack.AppID != "" && slack.EventType != "block_actions") || slack.Subtype == "bot_message" {
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: SlackExecutionFallbackNonUserEvent, Denied: false, Delegation: nil}
	}
	if slack.TeamID == "" || slack.UserID == "" {
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: SlackExecutionFallbackSenderAbsent, Denied: false, Delegation: nil}
	}
	row, err := mappings.GetSlackExecutionMapping(ctx, identityrepo.GetSlackExecutionMappingParams{OrganizationID: org, SlackTeamID: slack.TeamID, SlackUserID: slack.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		// Disconnect deletes memberships but retains the workspace tombstone.
		disconnected, lookupErr := mappings.SlackExecutionWorkspaceDisconnected(ctx, identityrepo.SlackExecutionWorkspaceDisconnectedParams{OrganizationID: org, SlackTeamID: slack.TeamID})
		if lookupErr == nil && disconnected {
			return &SlackExecutionSelection{HumanUserID: "", FallbackReason: "", Denied: true, Delegation: nil}
		}
		if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			return &SlackExecutionSelection{HumanUserID: "", FallbackReason: SlackExecutionFallbackMappingUnavailable, Denied: false, Delegation: nil}
		}
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: SlackExecutionFallbackMappingAbsent, Denied: false, Delegation: nil}
	}
	if err != nil {
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: SlackExecutionFallbackMappingUnavailable, Denied: false, Delegation: nil}
	}
	if !row.Eligible {
		return &SlackExecutionSelection{HumanUserID: "", FallbackReason: "", Denied: true, Delegation: nil}
	}
	return &SlackExecutionSelection{HumanUserID: row.UserID, FallbackReason: "", Denied: false, Delegation: &SlackExecutionDelegation{TeamID: slack.TeamID, UserID: slack.UserID, MembershipID: row.MembershipID, MappingID: row.MappingID, MappingRevision: row.MappingRevision, ConnectionGeneration: row.ConnectionGeneration}}
}

// A normalized UserID is not always an acting sender. Profile/workspace and
// membership notifications identify the affected account (which may have been
// changed, invited or removed by someone else). File lifecycle notifications
// likewise do not establish an acting sender in our normalization contract.
// Only known actor-bearing events may delegate; other events remain workloads.
func slackExecutionHasActor(event slackTriggerEvent) bool {
	switch event.EventType {
	case "message":
		// System messages and edits/deletions can identify an affected user or the
		// original author rather than the actor responsible for this notification.
		switch event.Subtype {
		case "", "file_share", "thread_broadcast", "me_message":
			return true
		default:
			return false
		}
	case "app_home_opened", "app_mention", "block_actions",
		"channel_created", "channel_archive", "channel_unarchive", "channel_deleted",
		"group_archive", "group_unarchive", "group_deleted",
		"file_shared", "link_shared", "pin_added", "pin_removed", "reaction_added", "reaction_removed":
		return true
	default:
		return false
	}
}
