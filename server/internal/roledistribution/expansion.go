package roledistribution

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
	var active bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM global_roles WHERE id = $1 AND deleted IS FALSE AND workos_deleted IS FALSE)`, roleID).Scan(&active)
	if err != nil {
		return fmt.Errorf("validate global role distribution: %w", err)
	}
	if !active {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id FROM organization_metadata WHERE disabled_at IS NULL AND id > $1 ORDER BY id LIMIT $2`, cursor, expansionPageSize)
	if err != nil {
		return fmt.Errorf("list global role distribution organizations: %w", err)
	}
	organizations, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("read global role distribution organizations: %w", err)
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
	var locked string
	err = tx.QueryRow(ctx, `SELECT id FROM organization_metadata WHERE id = $1 AND disabled_at IS NULL FOR SHARE`, organizationID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("validate organization role distribution: %w", err)
	}
	rows, err := tx.Query(ctx, `WITH active_roles AS (
 SELECT 'role:global:' || id::text AS role_urn FROM global_roles WHERE deleted IS FALSE AND workos_deleted IS FALSE
 UNION ALL
 SELECT 'role:organization:' || id::text FROM organization_roles WHERE organization_id = $1 AND deleted IS FALSE AND workos_deleted IS FALSE
 ) SELECT role_urn FROM active_roles WHERE role_urn > $2 ORDER BY role_urn LIMIT $3`, organizationID, cursor, expansionPageSize)
	if err != nil {
		return fmt.Errorf("list organization role distribution roles: %w", err)
	}
	roles, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("read organization role distribution roles: %w", err)
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
