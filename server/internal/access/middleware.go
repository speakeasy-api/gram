package access

import (
	"context"
	"log/slog"

	goa "goa.design/goa/v3/pkg"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const hardcodedRBACUserID = "rbac-test-user"

var hardcodedRolePrincipalsByUserID = map[string][]urn.Principal{
	hardcodedRBACUserID: {
		urn.NewPrincipal(urn.PrincipalTypeRole, "admin"),
	},
}

// Middleware is implemented as a Goa endpoint middleware rather than a plain
// HTTP middleware because grant loading depends on the authenticated
// AuthContext. That context is populated by Goa security handlers after the
// mux-level HTTP middleware has already run.
func Middleware(logger *slog.Logger, db accessrepo.DBTX) func(goa.Endpoint) goa.Endpoint {
	return func(next goa.Endpoint) goa.Endpoint {
		return func(ctx context.Context, req any) (any, error) {
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			if !ok || authCtx == nil || authCtx.SessionID == nil || authCtx.ActiveOrganizationID == "" || authCtx.UserID == "" {
				return next(ctx, req)
			}

			grants, err := LoadGrants(ctx, db, authCtx.ActiveOrganizationID, principalsForAuthContext(authCtx))
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to load access grants").Log(
					ctx,
					logger,
					attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
					attr.SlogUserID(authCtx.UserID),
				)
			}

			ctx = GrantsToContext(ctx, grants)

			return next(ctx, req)
		}
	}
}

func principalsForAuthContext(authCtx *contextvalues.AuthContext) []urn.Principal {
	principals := []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)}
	principals = append(principals, hardcodedRolePrincipalsByUserID[authCtx.UserID]...)
	return principals
}
