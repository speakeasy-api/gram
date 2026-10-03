package assistantidentity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
)

// SlackDelegation pins the trusted directory observation, not an email or a
// model-selected identity. Reassignment, unmapping and reconnect invalidate it.
type SlackDelegation struct {
	TeamID               string    `json:"team_id"`
	UserID               string    `json:"user_id"`
	MembershipID         uuid.UUID `json:"membership_id"`
	MappingID            uuid.UUID `json:"mapping_id"`
	MappingRevision      int64     `json:"mapping_revision"`
	ConnectionGeneration uuid.UUID `json:"connection_generation"`
}

// CaptureSlackDelegation selects a human once. Infrastructure failures are
// best-effort workload selection only here, before any human is selected.
func CaptureSlackDelegation(ctx context.Context, db repo.DBTX, org, team, sender string) (*SlackDelegation, string, string, error) {
	if team == "" || sender == "" {
		return nil, "", "slack_sender_absent", nil
	}
	row, err := repo.New(db).GetSlackExecutionMapping(ctx, repo.GetSlackExecutionMappingParams{OrganizationID: org, SlackTeamID: team, SlackUserID: sender})
	if errors.Is(err, pgx.ErrNoRows) {
		// Disconnect removes membership rows, but the workspace tombstone remains.
		disconnected, lookupErr := repo.New(db).SlackExecutionWorkspaceDisconnected(ctx, repo.SlackExecutionWorkspaceDisconnectedParams{OrganizationID: org, SlackTeamID: team})
		if lookupErr == nil && disconnected {
			return nil, "", "", ErrActorIneligible
		}
		if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			return nil, "", "slack_mapping_unavailable", nil
		}
		return nil, "", "slack_mapping_absent", nil
	}
	if err != nil {
		return nil, "", "slack_mapping_unavailable", nil
	}
	if !row.Eligible {
		return nil, "", "", ErrActorIneligible
	}
	return &SlackDelegation{TeamID: team, UserID: sender, MembershipID: row.MembershipID, MappingID: row.MappingID, MappingRevision: row.MappingRevision, ConnectionGeneration: row.ConnectionGeneration}, row.UserID, "", nil
}

// ValidateSlackDelegation never changes principals or falls back on lookup errors.
func ValidateSlackDelegation(ctx context.Context, db repo.DBTX, e Execution) error {
	d := e.Slack
	if d == nil {
		return nil
	}
	row, err := repo.New(db).GetSlackExecutionMapping(ctx, repo.GetSlackExecutionMappingParams{OrganizationID: e.Identity.OrganizationID, SlackTeamID: d.TeamID, SlackUserID: d.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrActorIneligible
	}
	if err != nil {
		return fmt.Errorf("revalidate Slack delegation: %w", err)
	}
	if !row.Eligible || row.UserID != e.HumanUserID || row.MappingID != d.MappingID || row.MembershipID != d.MembershipID || row.MappingRevision != d.MappingRevision || row.ConnectionGeneration != d.ConnectionGeneration {
		return ErrActorIneligible
	}
	return nil
}
