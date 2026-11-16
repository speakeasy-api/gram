package remotesessions

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// EMAIssuerTokenPurger erases retained vendor tokens after issuer containment.
// It does not revoke tokens at the vendor's authorization server.
type EMAIssuerTokenPurger struct {
	db *pgxpool.Pool
}

func NewEMAIssuerTokenPurger(db *pgxpool.Pool) *EMAIssuerTokenPurger {
	return &EMAIssuerTokenPurger{db: db}
}

// PurgeEMAIssuerTokens retires this organization's Speakeasy-issued credentials
// across projects, including ciphertext retained by an already retired row.
// External-provider credentials belong to a separate trust path and are kept.
func (p *EMAIssuerTokenPurger) PurgeEMAIssuerTokens(ctx context.Context, organizationID string) error {
	if err := repo.New(p.db).PurgeSpeakeasyEMACredentials(ctx, organizationID); err != nil {
		return fmt.Errorf("purge organization issuer credentials: %w", err)
	}
	return nil
}
