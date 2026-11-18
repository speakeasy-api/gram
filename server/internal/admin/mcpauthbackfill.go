package admin

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) BackfillMcpAuthGrants(ctx context.Context, _ *gen.BackfillMcpAuthGrantsPayload) (*gen.AdminBackfillMCPAuthGrantsResult, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	granted, err := repo.New(s.db).AdminBackfillMCPAuthFromConnect(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "backfill mcp:auth grants").LogError(ctx, s.logger)
	}
	s.logger.InfoContext(ctx, "mcp:auth grants backfilled from mcp:connect",
		attr.SlogAuditAction("mcp_auth_grants:backfill"),
		attr.SlogAdminOIDCSubject(auth.OIDCSubject),
	)
	return &gen.AdminBackfillMCPAuthGrantsResult{Granted: granted}, nil
}
