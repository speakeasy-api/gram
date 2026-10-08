package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var ErrOrganizationWhitelistNotFound = errors.New("organization whitelist target not found")

// OrganizationWhitelistState pins the target and access context under a metadata row lock.
type OrganizationWhitelistState struct {
	// OrganizationID is the canonical target.
	OrganizationID string `json:"organization_id"`

	// Name identifies the organization on the approval page.
	Name string `json:"name"`

	// Slug identifies the organization on the approval page.
	Slug string `json:"slug"`

	// Whitelisted bypasses the dashboard's demo-access gate.
	Whitelisted bool `json:"whitelisted"`

	// AccountType is context only and is never changed by this operation.
	AccountType string `json:"account_type"`

	// DisabledAt is context only; whitelisting cannot enable a disabled organization.
	DisabledAt *time.Time `json:"disabled_at"`

	// UpdatedAt invalidates a proposal after intervening account changes.
	UpdatedAt time.Time `json:"updated_at"`
}

func LockOrganizationWhitelistTx(ctx context.Context, tx pgx.Tx, organizationID string) (OrganizationWhitelistState, error) {
	row, err := repo.New(tx).LockOrganizationWhitelist(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.ID != organizationID) {
		return OrganizationWhitelistState{}, ErrOrganizationWhitelistNotFound
	}
	if err != nil {
		return OrganizationWhitelistState{}, fmt.Errorf("lock organization whitelist: %w", err)
	}
	state := OrganizationWhitelistState{OrganizationID: row.ID, Name: row.Name, Slug: row.Slug, Whitelisted: row.Whitelisted, AccountType: row.GramAccountType, DisabledAt: nil, UpdatedAt: row.UpdatedAt.Time}
	if row.DisabledAt.Valid {
		state.DisabledAt = &row.DisabledAt.Time
	}
	return state, nil
}

// SetOrganizationWhitelistTx changes only the demo-access gate and records its
// tenant audit event in the caller's transaction. It performs no provider calls.
func SetOrganizationWhitelistTx(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, organizationID string, whitelisted bool, actor urn.Principal, actorDisplayName *string) (bool, error) {
	before, err := LockOrganizationWhitelistTx(ctx, tx, organizationID)
	if err != nil {
		return false, err
	}
	if before.Whitelisted == whitelisted {
		return before.Whitelisted, nil
	}
	after, err := repo.New(tx).SetOrganizationWhitelist(ctx, repo.SetOrganizationWhitelistParams{ID: organizationID, Whitelisted: whitelisted, ExpectedWhitelisted: before.Whitelisted})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrOrganizationWhitelistNotFound
	}
	if err != nil {
		return false, fmt.Errorf("set organization whitelist: %w", err)
	}
	if err := auditLogger.LogOrganizationWhitelistUpdated(ctx, tx, audit.LogOrganizationWhitelistUpdatedEvent{
		OrganizationID: organizationID, Actor: actor, ActorDisplayName: actorDisplayName,
		OrganizationName: before.Name, OrganizationSlug: before.Slug,
		OrganizationSnapshotBefore: &audit.OrganizationWhitelistSnapshot{Whitelisted: before.Whitelisted},
		OrganizationSnapshotAfter:  &audit.OrganizationWhitelistSnapshot{Whitelisted: after},
	}); err != nil {
		return false, fmt.Errorf("log organization whitelist change: %w", err)
	}
	return after, nil
}
