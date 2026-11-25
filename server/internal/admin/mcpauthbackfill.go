package admin

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// BackfillMcpAuthGrants rejects all requests rather than granting broad access.
func (s *Service) BackfillMcpAuthGrants(ctx context.Context, _ *gen.BackfillMcpAuthGrantsPayload) (*gen.AdminBackfillMCPAuthGrantsResult, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	return nil, oops.E(oops.CodeNotImplemented, nil, "the upstream auth grant backfill has been retired").LogWarn(ctx, s.logger)
}
