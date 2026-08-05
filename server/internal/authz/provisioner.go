package authz

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
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

type Provisioner struct {
	db *pgxpool.Pool
}

func NewProvisioner(db *pgxpool.Pool) *Provisioner {
	return &Provisioner{db: db}
}

// ProvisionOrganizationAdmin seeds the built-in roles and grants, then assigns
// the organization creator to the admin role in the same transaction.
func (p *Provisioner) ProvisionOrganizationAdmin(ctx context.Context, organizationID string, admin InitialOrganizationAdmin) error {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin organization access provisioning transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if err := SeedSystemRoleGrantsTx(ctx, tx, organizationID); err != nil {
		return err
	}

	if admin.WorkOSUserID != "" {
		rows, err := repo.New(tx).UpsertOrganizationRoleAssignment(ctx, repo.UpsertOrganizationRoleAssignmentParams{
			OrganizationID:     organizationID,
			WorkosUserID:       admin.WorkOSUserID,
			UserID:             conv.ToPGText(admin.UserID),
			WorkosMembershipID: conv.ToPGTextEmpty(admin.WorkOSMembershipID),
			WorkosUpdatedAt:    pgtype.Timestamptz{Time: time.Now().UTC(), InfinityModifier: pgtype.Finite, Valid: true},
			WorkosLastEventID:  pgtype.Text{String: "", Valid: false},
			WorkosRoleSlug:     SystemRoleAdmin,
		})
		if err != nil {
			return fmt.Errorf("assign initial organization admin: %w", err)
		}
		if rows != 1 {
			return fmt.Errorf("assign initial organization admin: admin role not found")
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit organization access provisioning transaction: %w", err)
	}

	return nil
}
