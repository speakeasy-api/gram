package integrationcredentials

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/integrationcredentials/repo"
)

// BackfillResult reports one backfill run.
type BackfillResult struct {
	// Organizations is how many enrolled organizations had rows to seal.
	Organizations int

	// Sealed is how many rows gained an organization ciphertext.
	Sealed int

	// Unreadable is how many rows were skipped because their application-key
	// ciphertext does not decrypt.
	Unreadable int

	// Failed is how many organizations stopped early, typically because
	// their key was unavailable. The next run retries them.
	Failed int
}

// Backfiller seals, under each enrolled organization's key, the credentials
// it wrote before it was enrolled.
type Backfiller struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	cipher *Cipher
}

// NewBackfiller builds a Backfiller. Without organization encryption in the
// cipher no organization is enrolled and Run does nothing.
func NewBackfiller(logger *slog.Logger, db *pgxpool.Pool, cipher *Cipher) *Backfiller {
	return &Backfiller{logger: logger.With(attr.SlogComponent("integrationcredentials.backfill")), db: db, cipher: cipher}
}

type unsealedRow struct {
	id     uuid.UUID
	legacy string
}

// unsealedTable lists and seals the rows of one table that have no
// organization ciphertext yet.
type unsealedTable struct {
	column Column
	list   func(ctx context.Context, q *repo.Queries, organizationID string) ([]unsealedRow, error)
	seal   func(ctx context.Context, q *repo.Queries, organizationID string, row unsealedRow, org pgtype.Text) (int64, error)
}

var unsealedTables = []unsealedTable{
	{
		column: ColumnSlackDirectoryConnections,
		list: func(ctx context.Context, q *repo.Queries, organizationID string) ([]unsealedRow, error) {
			rows, err := q.ListUnsealedSlackDirectoryCredentials(ctx, organizationID)
			if err != nil {
				return nil, fmt.Errorf("list unsealed Slack workspace credentials: %w", err)
			}
			out := make([]unsealedRow, 0, len(rows))
			for _, row := range rows {
				out = append(out, unsealedRow{id: row.ID, legacy: row.CredentialsEncrypted})
			}
			return out, nil
		},
		seal: func(ctx context.Context, q *repo.Queries, organizationID string, row unsealedRow, org pgtype.Text) (int64, error) {
			affected, err := q.SealSlackDirectoryCredentials(ctx, repo.SealSlackDirectoryCredentialsParams{
				CredentialsOrgEncrypted: org,
				OrganizationID:          organizationID,
				ID:                      row.id,
				CredentialsEncrypted:    conv.ToPGText(row.legacy),
			})
			if err != nil {
				return 0, fmt.Errorf("seal Slack workspace credentials: %w", err)
			}
			return affected, nil
		},
	},
	{
		column: ColumnDeviceIntegrationConfigs,
		list: func(ctx context.Context, q *repo.Queries, organizationID string) ([]unsealedRow, error) {
			rows, err := q.ListUnsealedDeviceIntegrationCredentials(ctx, organizationID)
			if err != nil {
				return nil, fmt.Errorf("list unsealed device integration credentials: %w", err)
			}
			out := make([]unsealedRow, 0, len(rows))
			for _, row := range rows {
				out = append(out, unsealedRow{id: row.ID, legacy: row.CredentialsEncrypted})
			}
			return out, nil
		},
		seal: func(ctx context.Context, q *repo.Queries, organizationID string, row unsealedRow, org pgtype.Text) (int64, error) {
			affected, err := q.SealDeviceIntegrationCredentials(ctx, repo.SealDeviceIntegrationCredentialsParams{
				CredentialsOrgEncrypted: org,
				OrganizationID:          organizationID,
				ID:                      row.id,
				CredentialsEncrypted:    row.legacy,
			})
			if err != nil {
				return 0, fmt.Errorf("seal device integration credentials: %w", err)
			}
			return affected, nil
		},
	},
	{
		column: ColumnNetworkIngresses,
		list: func(ctx context.Context, q *repo.Queries, organizationID string) ([]unsealedRow, error) {
			rows, err := q.ListUnsealedNetworkIngressCredentials(ctx, organizationID)
			if err != nil {
				return nil, fmt.Errorf("list unsealed network ingress credentials: %w", err)
			}
			out := make([]unsealedRow, 0, len(rows))
			for _, row := range rows {
				out = append(out, unsealedRow{id: row.ID, legacy: row.CredentialsEncrypted})
			}
			return out, nil
		},
		seal: func(ctx context.Context, q *repo.Queries, organizationID string, row unsealedRow, org pgtype.Text) (int64, error) {
			affected, err := q.SealNetworkIngressCredentials(ctx, repo.SealNetworkIngressCredentialsParams{
				CredentialsOrgEncrypted: org,
				OrganizationID:          organizationID,
				ID:                      row.id,
				CredentialsEncrypted:    conv.ToPGText(row.legacy),
			})
			if err != nil {
				return 0, fmt.Errorf("seal network ingress credentials: %w", err)
			}
			return affected, nil
		},
	},
}

// Run seals the rows of every enrolled organization. An organization that
// fails is logged and counted, and the others still run.
func (b *Backfiller) Run(ctx context.Context) (BackfillResult, error) {
	result := BackfillResult{Organizations: 0, Sealed: 0, Unreadable: 0, Failed: 0}
	if b.cipher.org == nil {
		return result, nil
	}

	q := repo.New(b.db)
	organizationIDs, err := q.ListOrganizationsWithUnsealedCredentials(ctx)
	if err != nil {
		return result, fmt.Errorf("list organizations with unsealed integration credentials: %w", err)
	}

	for _, organizationID := range organizationIDs {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("backfill integration credentials: %w", err)
		}
		if !b.cipher.Enrolled(ctx, organizationID) {
			continue
		}

		result.Organizations++
		sealed, unreadable, err := b.sealOrganization(ctx, q, organizationID)
		result.Sealed += sealed
		result.Unreadable += unreadable
		if err != nil {
			result.Failed++
			b.logger.WarnContext(ctx, "backfill integration credentials for organization", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		}
	}

	return result, nil
}

func (b *Backfiller) sealOrganization(ctx context.Context, q *repo.Queries, organizationID string) (sealed int, unreadable int, err error) {
	for _, table := range unsealedTables {
		rows, err := table.list(ctx, q, organizationID)
		if err != nil {
			return sealed, unreadable, err
		}

		for _, row := range rows {
			org, err := b.reseal(ctx, organizationID, table.column, row.legacy)
			switch {
			case errors.Is(err, ErrUnreadable):
				unreadable++
				b.logger.WarnContext(ctx, "skip unreadable integration credentials", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
				continue
			case err != nil:
				return sealed, unreadable, err
			}

			affected, err := table.seal(ctx, q, organizationID, row, conv.ToPGText(org))
			if err != nil {
				return sealed, unreadable, err
			}
			sealed += int(affected)
		}
	}

	return sealed, unreadable, nil
}

func (b *Backfiller) reseal(ctx context.Context, organizationID string, column Column, legacy string) (string, error) {
	plaintext, err := b.cipher.legacy.Decrypt(legacy)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	return b.cipher.sealOrg(ctx, organizationID, column, []byte(plaintext))
}
