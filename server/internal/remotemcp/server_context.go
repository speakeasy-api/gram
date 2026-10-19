package remotemcp

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// ServerContext identifies the resolved server's owner,
// without authenticating the caller or granting access.
type ServerContext struct {
	// OrganizationID is charged regardless of the caller's organization.
	OrganizationID string

	// OrganizationSlug labels billing events.
	OrganizationSlug string

	// ProjectID scopes telemetry to the server's project, not the caller's.
	ProjectID uuid.UUID

	// ProjectSlug labels billing events.
	ProjectSlug string

	// AccountType determines whether the owner's free-tier cap applies.
	AccountType string
}

type serverContextKey struct{}

// WithServerContext requires ownership resolved from trusted storage.
func WithServerContext(ctx context.Context, serverCtx ServerContext) context.Context {
	return context.WithValue(ctx, serverContextKey{}, serverCtx)
}

// GetServerContext resolves server ownership, falling back to caller auth for gateway dispatches.
func GetServerContext(ctx context.Context) (ServerContext, bool) {
	if serverCtx, ok := ctx.Value(serverContextKey{}).(ServerContext); ok {
		return serverCtx, true
	}

	// Gateway dispatches bypass standalone backend context preparation.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		var empty ServerContext
		return empty, false
	}

	return ServerContext{
		OrganizationID:   authCtx.ActiveOrganizationID,
		OrganizationSlug: authCtx.OrganizationSlug,
		ProjectID:        conv.PtrValOr(authCtx.ProjectID, uuid.Nil),
		ProjectSlug:      conv.PtrValOr(authCtx.ProjectSlug, ""),
		AccountType:      authCtx.AccountType,
	}, true
}
