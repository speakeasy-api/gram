package roledistribution

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/roledistribution/repo"
	"github.com/speakeasy-api/gram/server/internal/roledistribution/requests"
)

// expansionPageSize bounds each transaction's setup events and source rows.
const expansionPageSize = 100

// ProcessGlobalFanout expands one page of organizations for a global role.
// Setup events and the cursor continuation commit together without a job ledger.
func ProcessGlobalFanout(ctx context.Context, db *pgxpool.Pool, roleID uuid.UUID, cursor string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin global role distribution: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	queries := repo.New(tx)
	active, err := queries.IsGlobalRoleActive(ctx, roleID)
	if err != nil {
		return fmt.Errorf("validate global role distribution: %w", err)
	}
	if !active {
		return nil
	}
	organizations, err := queries.ListActiveOrganizations(ctx, repo.ListActiveOrganizationsParams{Cursor: cursor, PageSize: expansionPageSize})
	if err != nil {
		return fmt.Errorf("list global role distribution organizations: %w", err)
	}
	for _, organizationID := range organizations {
		if err := requests.Publish(ctx, tx, requests.Request{OrganizationID: organizationID, RoleURN: "role:global:" + roleID.String(), GlobalRoleID: "", BootstrapOrganizationID: "", Cursor: ""}); err != nil {
			return fmt.Errorf("publish global role setup: %w", err)
		}
	}
	if len(organizations) == expansionPageSize {
		if err := requests.Publish(ctx, tx, requests.Request{OrganizationID: "", RoleURN: "", GlobalRoleID: roleID.String(), BootstrapOrganizationID: "", Cursor: organizations[len(organizations)-1]}); err != nil {
			return fmt.Errorf("publish global role distribution continuation: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit global role distribution: %w", err)
	}
	return nil
}

// ProcessOrganizationBootstrap expands one page of live roles for an organization.
// The role URN cursor bounds work without recording completion state.
func ProcessOrganizationBootstrap(ctx context.Context, db *pgxpool.Pool, organizationID, cursor string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin organization role distribution: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := requests.LockOrganization(ctx, tx, organizationID); err != nil {
		return fmt.Errorf("lock organization role distribution: %w", err)
	}
	queries := repo.New(tx)
	enabled, err := queries.IsFeatureEnabled(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("read organization rollout: %w", err)
	}
	if !enabled {
		return nil // A later staff enable creates a fresh enumeration pass.
	}
	_, err = queries.LockActiveOrganization(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("validate organization role distribution: %w", err)
	}
	roles, err := queries.ListActiveRoles(ctx, repo.ListActiveRolesParams{OrganizationID: organizationID, Cursor: cursor, PageSize: expansionPageSize})
	if err != nil {
		return fmt.Errorf("list organization role distribution roles: %w", err)
	}
	for _, roleURN := range roles {
		if err := requests.Publish(ctx, tx, requests.Request{OrganizationID: organizationID, RoleURN: roleURN, GlobalRoleID: "", BootstrapOrganizationID: "", Cursor: ""}); err != nil {
			return fmt.Errorf("publish organization role setup: %w", err)
		}
	}
	if len(roles) == expansionPageSize {
		if err := requests.Publish(ctx, tx, requests.Request{OrganizationID: "", RoleURN: "", GlobalRoleID: "", BootstrapOrganizationID: organizationID, Cursor: roles[len(roles)-1]}); err != nil {
			return fmt.Errorf("publish organization role distribution continuation: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit organization role distribution: %w", err)
	}
	return nil
}
