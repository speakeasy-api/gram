package mcptoolexecution

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

// ErrServerNotInOrganization reports that a resolved fronting server is not a
// live server in a live project of the organization. It is a data-integrity
// failure, not a deliberately unsupported resource: the route resolved a
// server that ownership validation then rejected, so the call must follow the
// fail-closed policy.
var ErrServerNotInOrganization = errors.New("mcp server is not a live resource of the organization")

// ServerSource is the authoritative resource input for one covered MCP call.
// It must be populated from the request's resolved mcp_endpoint route, never
// from a caller-provided identifier.
type ServerSource struct {
	// FrontingServerID is the fronting mcp_servers.id the route resolved for
	// this request — the same row whether the request arrived via /mcp/{slug}
	// or /x/mcp/{slug}, and regardless of the toolset, remote, or tunneled
	// backend behind it. Invalid marks a serving mode with no fronting server
	// (the legacy toolset-only fallback), which is deliberately unsupported.
	FrontingServerID uuid.NullUUID
}

// MCPServerResourceAdapter canonicalizes organization-owned MCP server
// resources. The canonical key is the fronting mcp_servers.id, giving hosted
// and private routes to one logical server a single identity.
type MCPServerResourceAdapter struct {
	db *pgxpool.Pool
}

// NewMCPServerResourceAdapter builds the canonical mcp_server resource
// adapter.
func NewMCPServerResourceAdapter(db *pgxpool.Pool) *MCPServerResourceAdapter {
	return &MCPServerResourceAdapter{db: db}
}

var _ killswitches.ResourceAdapter = (*MCPServerResourceAdapter)(nil)

// Kind returns the canonical MCP server resource namespace.
func (a *MCPServerResourceAdapter) Kind() killswitches.ResourceKind {
	return ResourceKindMCPServer
}

// Canonicalize parses the input as an mcp_servers UUID and normalizes it to
// its canonical lowercase form. Input that can never name a server is
// deliberately unsupported, not an error.
func (a *MCPServerResourceAdapter) Canonicalize(_ killswitches.OrganizationID, input string) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	id, err := uuid.Parse(strings.TrimSpace(input))
	if err != nil || id == uuid.Nil {
		return killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey](), nil
	}
	result, err := killswitches.NewCanonicalizationResult(killswitches.ResourceKey(id.String()))
	if err != nil {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("canonicalize mcp server key: %w", err)
	}
	return result, nil
}

// ValidateCurrentOrganization reports whether the key names a live server in
// a live project of the organization. A malformed key is not current; only
// query failures are errors.
func (a *MCPServerResourceAdapter) ValidateCurrentOrganization(ctx context.Context, organizationID killswitches.OrganizationID, key killswitches.ResourceKey) (bool, error) {
	id, err := uuid.Parse(string(key))
	if err != nil || id.String() != string(key) || organizationID == "" {
		return false, nil
	}
	live, err := ValidateLiveMCPServersInOrganization(ctx, a.db, organizationID, []killswitches.ResourceKey{key})
	if err != nil {
		return false, fmt.Errorf("check live mcp server ownership: %w", err)
	}
	return live, nil
}

// Derive accepts only a ServerSource built from the resolved route. A source
// with no fronting server is deliberately unsupported. A fronting server that
// fails live-ownership validation — deleted, in a deleted project, or in
// another organization — is an ErrServerNotInOrganization infrastructure
// failure and follows the fail-closed policy.
func (a *MCPServerResourceAdapter) Derive(ctx context.Context, organizationID killswitches.OrganizationID, source any) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	src, ok := source.(ServerSource)
	if !ok {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("unsupported resource source type %T", source)
	}
	if !src.FrontingServerID.Valid {
		return killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey](), nil
	}
	if organizationID == "" {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("organization ID is required")
	}

	live, err := ValidateLiveMCPServersInOrganization(ctx, a.db, organizationID, []killswitches.ResourceKey{killswitches.ResourceKey(src.FrontingServerID.UUID.String())})
	if err != nil {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("validate live mcp server ownership: %w", err)
	}
	if !live {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, ErrServerNotInOrganization
	}

	result, err := killswitches.NewCanonicalizationResult(killswitches.ResourceKey(src.FrontingServerID.UUID.String()))
	if err != nil {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("canonicalize mcp server key: %w", err)
	}
	return result, nil
}
