// Package marketplacetokenbackfill moves plugin marketplace tokens stored in
// plaintext to their hash and sealed copies. The migrations CLI and the admin
// dashboard's temporary Secret backfills page run it; see
// server/cmd/tools/migrations/MARKETPLACE_TOKENS_BACKFILL.md.
package marketplacetokenbackfill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// Options bound one run.
type Options struct {
	// OrganizationID limits the run to one organization's connections; empty
	// covers all. A connection without an organization_id belongs to its
	// project's organization.
	OrganizationID string

	// Apply writes the backfill. Otherwise the run only counts.
	Apply bool

	// BatchSize is the most connections one statement or page covers.
	BatchSize int32
}

// Mover moves one connection's plaintext token. *marketplacetokens.Tokens
// implements it.
type Mover interface {
	MoveLegacy(ctx context.Context, db repo.DBTX, connectionID uuid.UUID, organizationID string, legacy string) (bool, error)
}

// Counts classifies every GitHub connection, without naming organizations.
type Counts struct {
	// Unattributed is the number of connections without an organization_id
	// whose project's organization has a metadata row. An apply attributes
	// them.
	Unattributed int64

	// Legacy is the number of those connections that still hold a plaintext
	// token. An apply moves them, unless their organization needs
	// organization encryption.
	Legacy int64

	// Hashed is the number of connections whose token is stored as a hash
	// and sealed copies.
	Hashed int64

	// MissingOrganization is the number of connections whose project's
	// organization has no metadata row. An apply can neither attribute them
	// nor move their plaintext token, so they stay as they are.
	MissingOrganization int64
}

// Organization is one organization the backfill has work left in.
type Organization struct {
	ID   string
	Name string
	Slug string

	// Unattributed is the number of its connections without an
	// organization_id.
	Unattributed int64

	// Legacy is the number of its connections that still hold a plaintext
	// token.
	Legacy int64
}

// Report summarizes one run.
type Report struct {
	// Attributed is the number of connections this run gave an
	// organization_id. Always zero in a dry run.
	Attributed int64

	// Moved is the number of plaintext tokens this run moved. Always zero in
	// a dry run.
	Moved int64

	// Skipped is the number of plaintext tokens that changed between the read
	// and the write, such as when another run moved them first.
	Skipped int64

	// NeedsOrganizationKey is the number of plaintext tokens left in place
	// because their organization's marketplace-tokens use case is past
	// platform_key, which this run cannot seal for.
	NeedsOrganizationKey int64

	// Counts is the state after the run: what remains in an apply, what an
	// apply would act on in a dry run.
	Counts Counts
}

// Run first copies each project's organization onto connections stored
// without one, then moves every plaintext token, one connection per
// statement, so an interrupted run continues where it stopped when run again.
// A dry run only counts. The counts cover only OrganizationID when it is set.
// It never logs or returns a token.
func Run(ctx context.Context, db *pgxpool.Pool, tokens Mover, opts Options) (Report, error) {
	q := repo.New(db)
	organizationID := conv.ToPGTextEmpty(opts.OrganizationID)
	report := Report{Attributed: 0, Moved: 0, Skipped: 0, NeedsOrganizationKey: 0, Counts: Counts{Unattributed: 0, Legacy: 0, Hashed: 0, MissingOrganization: 0}}

	if opts.Apply {
		for {
			if err := ctx.Err(); err != nil {
				return report, fmt.Errorf("attribute connections: %w", err)
			}

			n, err := q.AttributeGitHubConnections(ctx, repo.AttributeGitHubConnectionsParams{OrganizationID: organizationID, LimitValue: opts.BatchSize})
			if err != nil {
				return report, fmt.Errorf("attribute connections: %w", err)
			}
			report.Attributed += n
			if n == 0 {
				break
			}
		}

		after := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		for {
			rows, err := q.ListLegacyMarketplaceTokens(ctx, repo.ListLegacyMarketplaceTokensParams{OrganizationID: organizationID, After: after, LimitValue: opts.BatchSize})
			if err != nil {
				return report, fmt.Errorf("list plaintext marketplace tokens: %w", err)
			}
			if len(rows) == 0 {
				break
			}

			for _, row := range rows {
				if err := ctx.Err(); err != nil {
					return report, fmt.Errorf("move marketplace tokens: %w", err)
				}

				moved, err := tokens.MoveLegacy(ctx, db, row.ID, row.OrganizationID, row.LegacyToken)
				switch {
				case errors.Is(err, orgencryption.ErrOrganizationEncryptionUnavailable):
					report.NeedsOrganizationKey++
				case err != nil:
					return report, fmt.Errorf("move marketplace token of connection %s: %w", row.ID, err)
				case moved:
					report.Moved++
				default:
					report.Skipped++
				}
				after = uuid.NullUUID{UUID: row.ID, Valid: true}
			}
		}
	}

	counts, err := q.CountMarketplaceTokenBackfill(ctx, organizationID)
	if err != nil {
		return report, fmt.Errorf("count connections: %w", err)
	}
	report.Counts = Counts{Unattributed: counts.Unattributed, Legacy: counts.Legacy, Hashed: counts.Hashed, MissingOrganization: counts.MissingOrganization}

	return report, nil
}

// Organizations lists the organizations the backfill has work left in, by
// name. Connections whose project's organization has no metadata row belong
// to none of them.
func Organizations(ctx context.Context, db repo.DBTX) ([]Organization, error) {
	rows, err := repo.New(db).ListMarketplaceTokenBackfillOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organizations with marketplace tokens to move: %w", err)
	}

	organizations := make([]Organization, 0, len(rows))
	for _, row := range rows {
		organizations = append(organizations, Organization{ID: row.OrganizationID, Name: row.OrganizationName, Slug: row.OrganizationSlug, Unattributed: row.Unattributed, Legacy: row.Legacy})
	}
	return organizations, nil
}
