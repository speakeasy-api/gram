package activities

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
)

// backfillDNSTargetToken returns the domain's DNS target token, assigning one
// first when the row was created before tokens existed. Its callers use the
// token only opportunistically, so a failure is logged and returns an invalid
// token instead of failing the caller.
func backfillDNSTargetToken(ctx context.Context, logger *slog.Logger, db customdomains.TxBeginner, organizationID string, id uuid.UUID, domain string, token pgtype.Text) pgtype.Text {
	if token.Valid {
		return token
	}

	value, err := customdomains.EnsureDNSTargetToken(ctx, db, organizationID, id)
	if err != nil {
		logger.WarnContext(ctx, "backfill custom domain dns target token", attr.SlogURLDomain(domain), attr.SlogError(err))
		return conv.ToPGTextEmpty("")
	}
	return conv.ToPGTextEmpty(value)
}
