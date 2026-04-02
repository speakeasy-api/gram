package access

import (
	"context"
	"fmt"
	"log/slog"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type contextKey string

const grantsContextKey contextKey = "access_grants"

// GrantsToContext stores resolved grants on the request context.
func GrantsToContext(ctx context.Context, grants *Grants) context.Context {
	return context.WithValue(ctx, grantsContextKey, grants)
}

// GrantsFromContext loads resolved grants from the request context.
func GrantsFromContext(ctx context.Context) (*Grants, bool) {
	grants, ok := ctx.Value(grantsContextKey).(*Grants)
	return grants, ok
}

// LoadIntoContext must be called after authentication has populated AuthContext.
// Goa endpoint middleware runs before security handlers, so it cannot see the
// authenticated user/session state needed to resolve grants.
func LoadIntoContext(ctx context.Context, logger *slog.Logger, db accessrepo.DBTX) (context.Context, error) {
	logger = logger.With(attr.SlogComponent("access"))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil || authCtx.ActiveOrganizationID == "" || authCtx.UserID == "" {
		return ctx, nil
	}

	if authCtx.AccountType != "enterprise" {
		return ctx, nil
	}

	principals := []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)}
	// TODO: once we have role mapping we need to also add grants for roles here
	// principals = append(principals, roleMapping[authCtx.UserID]...)

	grants, err := LoadGrants(ctx, db, authCtx.ActiveOrganizationID, principals)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"failed to load access grants",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogUserID(authCtx.UserID),
			attr.SlogError(err),
		)
		return ctx, fmt.Errorf("load access grants: %w", err)
	}

	return GrantsToContext(ctx, grants), nil
}
