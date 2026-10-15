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

type UpsertOrganizationMetadataParams = UpsertOrganizationMetadataWithRequestsParams

// UpsertOrganizationMetadataRow excludes internal distribution requests from the source result.
type UpsertOrganizationMetadataRow = OrganizationMetadatum

func (q *Queries) UpsertOrganizationMetadata(ctx context.Context, arg UpsertOrganizationMetadataParams) (UpsertOrganizationMetadataRow, error) {
	tx, err := q.beginDistribution(ctx)
	if err != nil {
		return UpsertOrganizationMetadataRow{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	result, err := q.WithTx(tx).UpsertOrganizationMetadataWithRequests(ctx, arg)
	if err != nil {
		return UpsertOrganizationMetadataRow{}, err
	}
	if err := requests.PublishAll(ctx, tx, result.Requests); err != nil {
		return UpsertOrganizationMetadataRow{}, fmt.Errorf("publish role distribution source requests: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return UpsertOrganizationMetadataRow{}, fmt.Errorf("commit role distribution source transaction: %w", err)
	}
	return organizationMetadata(result), nil
}

type CreateOrganizationMetadataParams = CreateOrganizationMetadataWithRequestsParams

func (q *Queries) CreateOrganizationMetadata(ctx context.Context, arg CreateOrganizationMetadataParams) error {
	tx, err := q.beginDistribution(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	result, err := q.WithTx(tx).CreateOrganizationMetadataWithRequests(ctx, arg)
	if err != nil {
		return err
	}
	if err := requests.PublishAll(ctx, tx, result.Requests); err != nil {
		return fmt.Errorf("publish role distribution source requests: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit role distribution source transaction: %w", err)
	}
	return nil
}

type CreateOrganizationMetadataFromWorkOSParams = CreateOrganizationMetadataFromWorkOSWithRequestsParams

// CreateOrganizationMetadataFromWorkOSRow excludes internal distribution requests from the source result.
type CreateOrganizationMetadataFromWorkOSRow = OrganizationMetadatum

func (q *Queries) CreateOrganizationMetadataFromWorkOS(ctx context.Context, arg CreateOrganizationMetadataFromWorkOSParams) (CreateOrganizationMetadataFromWorkOSRow, error) {
	tx, err := q.beginDistribution(ctx)
	if err != nil {
		return CreateOrganizationMetadataFromWorkOSRow{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	result, err := q.WithTx(tx).CreateOrganizationMetadataFromWorkOSWithRequests(ctx, arg)
	if err != nil {
		return CreateOrganizationMetadataFromWorkOSRow{}, err
	}
	if err := requests.PublishAll(ctx, tx, result.Requests); err != nil {
		return CreateOrganizationMetadataFromWorkOSRow{}, fmt.Errorf("publish role distribution source requests: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CreateOrganizationMetadataFromWorkOSRow{}, fmt.Errorf("commit role distribution source transaction: %w", err)
	}
	return organizationMetadata(UpsertOrganizationMetadataWithRequestsRow(result)), nil
}

type UpsertOrganizationMetadataFromWorkOSParams = UpsertOrganizationMetadataFromWorkOSWithRequestsParams

// UpsertOrganizationMetadataFromWorkOSRow excludes internal distribution requests from the source result.
type UpsertOrganizationMetadataFromWorkOSRow = OrganizationMetadatum

func (q *Queries) UpsertOrganizationMetadataFromWorkOS(ctx context.Context, arg UpsertOrganizationMetadataFromWorkOSParams) (UpsertOrganizationMetadataFromWorkOSRow, error) {
	tx, err := q.beginDistribution(ctx)
	if err != nil {
		return UpsertOrganizationMetadataFromWorkOSRow{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	result, err := q.WithTx(tx).UpsertOrganizationMetadataFromWorkOSWithRequests(ctx, arg)
	if err != nil {
		return UpsertOrganizationMetadataFromWorkOSRow{}, err
	}
	if err := requests.PublishAll(ctx, tx, result.Requests); err != nil {
		return UpsertOrganizationMetadataFromWorkOSRow{}, fmt.Errorf("publish role distribution source requests: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return UpsertOrganizationMetadataFromWorkOSRow{}, fmt.Errorf("commit role distribution source transaction: %w", err)
	}
	return organizationMetadata(UpsertOrganizationMetadataWithRequestsRow(result)), nil
}

// organizationMetadata keeps distribution requests inside the publication boundary.
func organizationMetadata(result UpsertOrganizationMetadataWithRequestsRow) OrganizationMetadatum {
	return OrganizationMetadatum{
		ID:                 result.ID,
		Name:               result.Name,
		Slug:               result.Slug,
		GramAccountType:    result.GramAccountType,
		WorkosID:           result.WorkosID,
		WorkosUpdatedAt:    result.WorkosUpdatedAt,
		WorkosLastEventID:  result.WorkosLastEventID,
		SvixAppID:          result.SvixAppID,
		WebhooksEnabled:    result.WebhooksEnabled,
		Whitelisted:        result.Whitelisted,
		FreeTrialStartedAt: result.FreeTrialStartedAt,
		FreeTrialEndsAt:    result.FreeTrialEndsAt,
		ScimEnabled:        result.ScimEnabled,
		SsoEnabled:         result.SsoEnabled,
		VerifiedDomains:    result.VerifiedDomains,
		CreationSource:     result.CreationSource,
		DefaultHost:        result.DefaultHost,
		CreatedAt:          result.CreatedAt,
		UpdatedAt:          result.UpdatedAt,
		DisabledAt:         result.DisabledAt,
	}
}
