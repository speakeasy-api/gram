package assistantidentity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
)

// SlackDelegation pins the trusted directory observation. Reassignment, unmapping
// and reconnect invalidate it.
type SlackDelegation struct {
	TeamID               string    `json:"team_id"`
	UserID               string    `json:"user_id"`
	MembershipID         uuid.UUID `json:"membership_id"`
	MappingID            uuid.UUID `json:"mapping_id"`
	MappingRevision      int64     `json:"mapping_revision"`
	ConnectionGeneration uuid.UUID `json:"connection_generation"`
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
