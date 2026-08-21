package authz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type InitialOrganizationAdmin struct {
	UserID             string
	WorkOSUserID       string
	WorkOSMembershipID string
}

var errInitialAdminWorkOSUserIDRequired = errors.New("assign initial organization admin: WorkOS user ID is required")

type Provisioner struct {
	db *pgxpool.Pool
}

func NewProvisioner(db *pgxpool.Pool) *Provisioner {
	return &Provisioner{db: db}
}

// ProvisionOrganizationAdmin seeds the built-in roles and grants, then assigns
// the organization creator to the admin role in the same transaction.
func (p *Provisioner) ProvisionOrganizationAdmin(ctx context.Context, organizationID string, admin InitialOrganizationAdmin) error {
	if admin.WorkOSUserID == "" {
		return errInitialAdminWorkOSUserIDRequired
	}

	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin organization access provisioning transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if err := p.ProvisionOrganizationAdminTx(ctx, tx, organizationID, admin); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit organization access provisioning transaction: %w", err)
	}

	return nil
}

// ProvisionOrganizationAdminTx seeds the built-in roles and grants, then
// assigns the organization creator using a caller-owned transaction.
func (p *Provisioner) ProvisionOrganizationAdminTx(ctx context.Context, tx pgx.Tx, organizationID string, admin InitialOrganizationAdmin) error {
	if admin.WorkOSUserID == "" {
		return errInitialAdminWorkOSUserIDRequired
	}

	if err := SeedSystemRoleGrantsTx(ctx, tx, organizationID); err != nil {
		return err
	}

	rows, err := repo.New(tx).UpsertOrganizationRoleAssignment(ctx, repo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     organizationID,
		WorkosUserID:       admin.WorkOSUserID,
		UserID:             conv.ToPGText(admin.UserID),
		WorkosMembershipID: conv.ToPGTextEmpty(admin.WorkOSMembershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
		WorkosRoleSlug:     SystemRoleAdmin,
	})
	if err != nil {
		return fmt.Errorf("assign initial organization admin: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("assign initial organization admin: admin role not found")
	}

	return nil
}
