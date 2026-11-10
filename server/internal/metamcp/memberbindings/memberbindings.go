// Package memberbindings removes the provider client bindings gateway
// membership adds to a gateway's user session issuer, once no member needs
// them.
package memberbindings

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Gateway names the gateway whose issuer a binding is removed from.
type Gateway struct {
	// ID is the gateway, for logs.
	ID uuid.UUID

	// OrganizationID owns the gateway.
	OrganizationID string

	// ProjectID owns the gateway and its issuer.
	ProjectID uuid.UUID

	// UserSessionIssuerID is the issuer the binding is removed from.
	UserSessionIssuerID uuid.UUID
}

// DetachServerProvider unbinds the client of serverID's one provider from the
// gateway's issuer, unless another live member of a gateway on that issuer
// still fronts the provider or a server signs in directly through the issuer.
// A server with no provider, or several, has no binding to remove. Run it after
// the membership, server or gateway that needed the binding is soft-deleted,
// so the rule sees the state after the change.
func DetachServerProvider(ctx context.Context, tx pgx.Tx, logger *slog.Logger, gateway Gateway, serverID uuid.UUID) error {
	upstream, err := upstreamclients.ServerUpstreamIssuers(ctx, tx, gateway.ProjectID, serverID)
	if err != nil {
		return fmt.Errorf("read member upstream issuers: %w", err)
	}
	provider := upstream.Sole()
	if !provider.Valid {
		return nil
	}
	return DetachProvider(ctx, tx, logger, gateway, serverID, provider.UUID)
}

// DetachProvider unbinds provider's client from the gateway's issuer under the
// same rule as DetachServerProvider. serverID is the member whose removal
// ended the need, for logs.
func DetachProvider(ctx context.Context, tx pgx.Tx, logger *slog.Logger, gateway Gateway, serverID, provider uuid.UUID) error {
	if err := remotesessionsrepo.New(tx).LockRemoteSessionIssuerForClientBinding(ctx, provider); err != nil {
		return fmt.Errorf("lock remote session issuer for client binding: %w", err)
	}
	// TODO(AIM-925): THIS BETTER HECKING GET REMOVED once challenge state and
	// token resolution read from a server's associations, not its issuer's
	// bindings. Until then a server directly on a gateway's issuer depends on
	// these bindings, and nothing says which provider it uses, so keep them.
	outsiders, err := repo.New(tx).CountServersOnGatewayIssuer(ctx, repo.CountServersOnGatewayIssuerParams{
		OrganizationID:  gateway.OrganizationID,
		GatewayIssuerID: uuid.NullUUID{UUID: gateway.UserSessionIssuerID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("count servers on gateway issuer: %w", err)
	}
	if err := inv.Check("memberbindings.DetachProvider", "no MCP server signs in directly through a gateway's issuer", outsiders == 0); err != nil {
		// Logged rather than returned: the change that ended the need still
		// goes through, and keeping a binding costs a stale consent tile at worst.
		logger.ErrorContext(ctx, "kept provider client on a gateway issuer shared with an MCP server",
			attr.SlogError(err),
			attr.SlogMetaMcpServerID(gateway.ID.String()),
			attr.SlogMcpServerID(serverID.String()))
		return nil
	}
	detached, err := repo.New(tx).AutoDetachMemberProviderClient(ctx, repo.AutoDetachMemberProviderClientParams{
		GatewayIssuerID: gateway.UserSessionIssuerID,
		RemoteIssuerID:  provider,
		ProjectID:       gateway.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("detach member provider client: %w", err)
	}
	if detached > 0 {
		logger.InfoContext(ctx, "detached member provider client from meta mcp issuer",
			attr.SlogMetaMcpServerID(gateway.ID.String()),
			attr.SlogMcpServerID(serverID.String()))
	}
	return nil
}
