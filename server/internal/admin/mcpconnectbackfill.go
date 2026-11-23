package admin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// BackfillMcpConnectGrants makes the mcp:connect access that mcp:read and
// mcp:write imply explicit, so removing that implication changes no one's
// access. It only adds and extends grants.
func (s *Service) BackfillMcpConnectGrants(ctx context.Context, payload *gen.BackfillMcpConnectGrantsPayload) (*gen.AdminBackfillMCPConnectGrantsResult, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	var createdBefore pgtype.Timestamptz
	if payload != nil && payload.CreatedBefore != nil {
		cutoff, err := time.Parse(time.RFC3339, *payload.CreatedBefore)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "created_before must be an RFC 3339 time")
		}
		createdBefore = pgtype.Timestamptz{Time: cutoff, InfinityModifier: pgtype.Finite, Valid: true}
	}
	row, err := repo.New(s.db).AdminBackfillMCPConnectFromReadWrite(ctx, createdBefore)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "backfill mcp:connect grants").LogError(ctx, s.logger)
	}
	s.logger.InfoContext(ctx, "mcp:connect grants backfilled from mcp:read and mcp:write",
		attr.SlogAuditAction("mcp_connect_grants:backfill"),
		attr.SlogAdminOIDCSubject(auth.OIDCSubject),
	)
	return &gen.AdminBackfillMCPConnectGrantsResult{Inserted: row.Inserted, Extended: row.Extended, TakenOver: row.TakenOver}, nil
}
