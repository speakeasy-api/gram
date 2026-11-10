package repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/roledistribution/requests"
)

// beginDistribution starts a transaction for pool callers, or a savepoint for
// caller-owned transactions. A publication failure rolls back the source too.
func (q *Queries) beginDistribution(ctx context.Context) (pgx.Tx, error) {
	db, ok := q.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return nil, fmt.Errorf("role distribution source requires a transaction-capable database")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin role distribution source transaction: %w", err)
	}
	return tx, nil
}

type UpsertGlobalRoleParams = UpsertGlobalRoleWithRequestsParams

func (q *Queries) UpsertGlobalRole(ctx context.Context, arg UpsertGlobalRoleParams) error {
	tx, err := q.beginDistribution(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	result, err := q.WithTx(tx).UpsertGlobalRoleWithRequests(ctx, arg)
	if err != nil {
		return err
	}
	if err := requests.PublishAll(ctx, tx, result); err != nil {
		return fmt.Errorf("publish role distribution source requests: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit role distribution source transaction: %w", err)
	}
	return nil
}

type CreateOrganizationRoleParams = CreateOrganizationRoleWithRequestsParams

// CreateOrganizationRoleRow excludes internal distribution requests from the source result.
type CreateOrganizationRoleRow = GetOrganizationRoleByIDRow

func (q *Queries) CreateOrganizationRole(ctx context.Context, arg CreateOrganizationRoleParams) (CreateOrganizationRoleRow, error) {
	tx, err := q.beginDistribution(ctx)
	if err != nil {
		return CreateOrganizationRoleRow{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	result, err := q.WithTx(tx).CreateOrganizationRoleWithRequests(ctx, arg)
	if err != nil {
		return CreateOrganizationRoleRow{}, err
	}
	if err := requests.PublishAll(ctx, tx, result.Requests); err != nil {
		return CreateOrganizationRoleRow{}, fmt.Errorf("publish role distribution source requests: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CreateOrganizationRoleRow{}, fmt.Errorf("commit role distribution source transaction: %w", err)
	}
	return organizationRole(result), nil
}

type UpsertOrganizationRoleParams = UpsertOrganizationRoleWithRequestsParams

// UpsertOrganizationRoleRow excludes internal distribution requests from the source result.
type UpsertOrganizationRoleRow = GetOrganizationRoleByIDRow

func (q *Queries) UpsertOrganizationRole(ctx context.Context, arg UpsertOrganizationRoleParams) (UpsertOrganizationRoleRow, error) {
	tx, err := q.beginDistribution(ctx)
	if err != nil {
		return UpsertOrganizationRoleRow{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	result, err := q.WithTx(tx).UpsertOrganizationRoleWithRequests(ctx, arg)
	if err != nil {
		return UpsertOrganizationRoleRow{}, err
	}
	if err := requests.PublishAll(ctx, tx, result.Requests); err != nil {
		return UpsertOrganizationRoleRow{}, fmt.Errorf("publish role distribution source requests: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return UpsertOrganizationRoleRow{}, fmt.Errorf("commit role distribution source transaction: %w", err)
	}
	return organizationRole(CreateOrganizationRoleWithRequestsRow(result)), nil
}

// organizationRole keeps distribution requests inside the publication boundary.
func organizationRole(result CreateOrganizationRoleWithRequestsRow) GetOrganizationRoleByIDRow {
	return GetOrganizationRoleByIDRow{
		ID:                result.ID,
		RoleUrn:           result.RoleUrn,
		WorkosSlug:        result.WorkosSlug,
		WorkosName:        result.WorkosName,
		WorkosDescription: result.WorkosDescription,
		WorkosCreatedAt:   result.WorkosCreatedAt,
		WorkosUpdatedAt:   result.WorkosUpdatedAt,
		MemberCount:       result.MemberCount,
	}
}
