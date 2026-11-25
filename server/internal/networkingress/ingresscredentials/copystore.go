package ingresscredentials

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// CopyStore is how the orgencryption backfill, verification and finalize
// reach the two copies of network ingress credentials. Row ids are the
// ingress's uuid formatted with uuid.UUID.String, which matches the id::text
// the count query joins organization_encryption_unreadable_values on.
type CopyStore struct {
	db *pgxpool.Pool
}

var _ orgencryption.CopyStore = (*CopyStore)(nil)

// NewCopyStore builds the network ingress credential CopyStore.
func NewCopyStore(db *pgxpool.Pool) *CopyStore {
	return &CopyStore{db: db}
}

// Columns names the credentials' two copies.
func (c *CopyStore) Columns() orgencryption.CopyColumns {
	return Columns
}

// ListWaiting returns the organization's next page of credentials with only a
// platform-key copy.
func (c *CopyStore) ListWaiting(ctx context.Context, organizationID string, after string, limit int32) ([]orgencryption.WaitingValue, error) {
	afterID, err := parseAfter(after)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(c.db).ListNetworkIngressCredentialsWaiting(ctx, repo.ListNetworkIngressCredentialsWaitingParams{
		OrganizationID: organizationID,
		After:          afterID,
		LimitValue:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list waiting network ingress credentials: %w", err)
	}

	values := make([]orgencryption.WaitingValue, 0, len(rows))
	for _, row := range rows {
		values = append(values, orgencryption.WaitingValue{RowID: row.ID.String(), Platform: row.PlatformCopy})
	}

	return values, nil
}

// SealWaiting stores the backfill's organization-key copy of one ingress's
// credentials.
func (c *CopyStore) SealWaiting(ctx context.Context, organizationID string, generation int64, value orgencryption.WaitingValue, orgCiphertext string) (bool, error) {
	id, err := uuid.Parse(value.RowID)
	if err != nil {
		return false, fmt.Errorf("parse network ingress id %q: %w", value.RowID, err)
	}

	landed, err := repo.New(c.db).SealNetworkIngressCredentialsWaiting(ctx, repo.SealNetworkIngressCredentialsWaitingParams{
		OrgCopy:           conv.ToPGText(orgCiphertext),
		ID:                id,
		OrganizationID:    organizationID,
		PlatformCopy:      conv.ToPGText(value.Platform),
		DecidedGeneration: generation,
	})
	if err != nil {
		return false, fmt.Errorf("seal waiting network ingress credentials: %w", err)
	}

	return landed == 1, nil
}

// ListSealed returns the organization's next page of credentials with both
// copies.
func (c *CopyStore) ListSealed(ctx context.Context, organizationID string, after string, limit int32) ([]orgencryption.SealedValue, error) {
	afterID, err := parseAfter(after)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(c.db).ListNetworkIngressCredentialsSealed(ctx, repo.ListNetworkIngressCredentialsSealedParams{
		OrganizationID: organizationID,
		After:          afterID,
		LimitValue:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list sealed network ingress credentials: %w", err)
	}

	values := make([]orgencryption.SealedValue, 0, len(rows))
	for _, row := range rows {
		values = append(values, orgencryption.SealedValue{RowID: row.ID.String(), Platform: row.PlatformCopy, Org: row.OrgCopy})
	}

	return values, nil
}

// Count returns the organization's credential counts. Every ingress row
// carries its organization, so none is unattributed.
func (c *CopyStore) Count(ctx context.Context, organizationID string) (orgencryption.CopyCounts, error) {
	row, err := repo.New(c.db).CountNetworkIngressCredentialCopies(ctx, organizationID)
	if err != nil {
		return orgencryption.CopyCounts{}, fmt.Errorf("count network ingress credential copies: %w", err)
	}

	return orgencryption.CopyCounts{
		Waiting:        row.Waiting,
		Unreadable:     row.Unreadable,
		Unattributed:   0,
		PlatformCopies: row.PlatformCopies,
		Values:         row.ValueCount,
		LastWrittenAt:  orgencryption.LastWrittenAt(row.LastWrittenAt),
	}, nil
}

// ListOrganizationsWithValues returns every organization storing a network ingress credential.
func (c *CopyStore) ListOrganizationsWithValues(ctx context.Context) ([]orgencryption.OrganizationValues, error) {
	rows, err := repo.New(c.db).ListOrganizationsWithNetworkIngressCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organizations with network ingress credentials: %w", err)
	}

	organizations := make([]orgencryption.OrganizationValues, 0, len(rows))
	for _, row := range rows {
		organizations = append(organizations, orgencryption.NewOrganizationValues(row.OrganizationID, row.ValueCount, row.LastWrittenAt))
	}

	return organizations, nil
}

// ClearPlatformCopies destroys up to limit of the organization's
// platform-key copies after finalize.
func (c *CopyStore) ClearPlatformCopies(ctx context.Context, organizationID string, generation int64, limit int32) (orgencryption.ClearedCopies, error) {
	cleared, err := repo.New(c.db).ClearNetworkIngressCredentialPlatformCopies(ctx, repo.ClearNetworkIngressCredentialPlatformCopiesParams{
		OrganizationID:    organizationID,
		LimitValue:        limit,
		DecidedGeneration: generation,
	})
	if err != nil {
		return orgencryption.ClearedCopies{}, fmt.Errorf("clear network ingress credential platform copies: %w", err)
	}

	return orgencryption.ClearedCopies{PlatformOnly: cleared.PlatformOnly, BesideOrgCopy: cleared.BesideOrgCopy, Empty: 0}, nil
}

// parseAfter parses a page cursor: "" for the first page, otherwise the RowID
// of the previous page's last value.
func parseAfter(after string) (uuid.NullUUID, error) {
	if after == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}

	id, err := uuid.Parse(after)
	if err != nil {
		return uuid.NullUUID{}, fmt.Errorf("parse page cursor %q: %w", after, err)
	}

	return uuid.NullUUID{UUID: id, Valid: true}, nil
}
