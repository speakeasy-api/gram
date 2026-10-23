// Package hostingstate derives a toolset's MCP hosting state from the server row holding its id. The canonical
// wrapper owns that state; the toolset's own hosting columns are a frozen alias that stands only without one.
package hostingstate

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// Row is the server row holding a toolset's id, as ListCanonicalHostingByToolsetIDs returns it.
type Row = toolsetsrepo.ListCanonicalHostingByToolsetIDsRow

// ApplyServer sets toolset's enabled, public and issuer state from its canonical wrapper's visibility and issuer.
func ApplyServer(toolset toolsetsrepo.Toolset, serverVisibility string, issuer uuid.NullUUID) toolsetsrepo.Toolset {
	toolset.McpEnabled = serverVisibility == visibility.Public || serverVisibility == visibility.Private
	toolset.McpIsPublic = serverVisibility == visibility.Public
	toolset.UserSessionIssuerID = issuer
	return toolset
}

// ApplyKept marks a toolset whose id a non-canonical server kept after leaving it: nothing serves it.
func ApplyKept(toolset toolsetsrepo.Toolset) toolsetsrepo.Toolset {
	toolset.McpEnabled = false
	toolset.McpIsPublic = false
	return toolset
}

// Apply derives toolset's hosting state, and its address when the wrapper has a servable endpoint; a nil row
// leaves the toolset columns standing.
func Apply(toolset toolsetsrepo.Toolset, row *Row) toolsetsrepo.Toolset {
	switch {
	case row == nil:
		return toolset
	case !row.Canonical:
		return ApplyKept(toolset)
	}
	toolset = ApplyServer(toolset, row.Visibility, row.UserSessionIssuerID)
	if row.HasEndpoint {
		toolset.McpSlug = pgtype.Text{String: row.EndpointSlug, Valid: true}
		toolset.CustomDomainID = row.EndpointCustomDomainID
	}
	return toolset
}

// Load returns the server rows holding the given toolsets' ids in one query, keyed by toolset id.
func Load(ctx context.Context, db toolsetsrepo.DBTX, organizationID string, toolsetIDs []uuid.UUID) (map[uuid.UUID]Row, error) {
	if len(toolsetIDs) == 0 {
		return map[uuid.UUID]Row{}, nil
	}
	rows, err := toolsetsrepo.New(db).ListCanonicalHostingByToolsetIDs(ctx, toolsetsrepo.ListCanonicalHostingByToolsetIDsParams{OrganizationID: organizationID, ToolsetIds: toolsetIDs})
	if err != nil {
		return nil, fmt.Errorf("list canonical hosted MCP state: %w", err)
	}
	byID := make(map[uuid.UUID]Row, len(rows))
	for _, row := range rows {
		byID[row.ToolsetID] = row
	}
	return byID, nil
}

// For returns toolsetID's row from byID, or nil when no server holds its id.
func For(byID map[uuid.UUID]Row, toolsetID uuid.UUID) *Row {
	row, ok := byID[toolsetID]
	if !ok {
		return nil
	}
	return &row
}

// Ingress namespace kinds a private network ingress serves hosted endpoints under.
const (
	namespacePlatform     = "platform"
	namespaceCustomDomain = "custom_domain"
)

// CheckPrivateIngress requires an online private ingress whose namespace every given endpoint lives in.
func CheckPrivateIngress(ctx context.Context, db networkingressrepo.DBTX, organizationID string, endpoints []mcpendpointsrepo.McpEndpoint) error {
	ingress, err := networkingressrepo.New(db).GetNetworkIngressByOrganization(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeInvalid, nil, "online private network ingress is required")
	}
	if err != nil || !ingress.Enabled || ingress.Deleted || ingress.Status != "online" || !ingress.DnsName.Valid {
		return oops.E(oops.CodeInvalid, err, "online private network ingress is required")
	}
	switch ingress.EndpointNamespaceKind {
	case namespacePlatform, namespaceCustomDomain:
	default:
		return oops.E(oops.CodeInvalid, nil, "private ingress namespace is invalid")
	}
	for _, endpoint := range endpoints {
		if (ingress.EndpointNamespaceKind == namespacePlatform && endpoint.CustomDomainID.Valid) ||
			(ingress.EndpointNamespaceKind == namespaceCustomDomain && (!endpoint.CustomDomainID.Valid || endpoint.CustomDomainID != ingress.CustomDomainID)) {
			return oops.E(oops.CodeInvalid, nil, "hosted MCP address does not match the private ingress namespace")
		}
	}
	return nil
}

// MirrorToToolset writes a hosted toolset's hosting columns from its canonical server and endpoint, in the
// caller's transaction, so pods that still read the columns and run the toolset-to-server sync see the same state
// and that sync writes nothing. The caller holds the server's hosting locks. A toolset without a canonical server
// is left alone.
func MirrorToToolset(ctx context.Context, tx toolsetsrepo.DBTX, projectID, toolsetID uuid.UUID) error {
	toolset, err := toolsetsrepo.New(tx).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: toolsetID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("load hosted toolset: %w", err)
	}
	server, err := mcpserversrepo.New(tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: toolsetID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("load hosted MCP server: %w", err)
	case !server.ToolsetID.Valid || server.ToolsetID.UUID != server.ID:
		return nil
	}
	endpoints, err := mcpendpointsrepo.New(tx).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: toolsetID})
	if err != nil {
		return fmt.Errorf("list hosted MCP endpoints: %w", err)
	}
	mirrored := MirrorOf(toolset, server, endpoints)
	if _, err := toolsetsrepo.New(tx).MirrorToolsetHosting(ctx, toolsetsrepo.MirrorToolsetHostingParams{
		Name: mirrored.Name, McpSlug: mirrored.McpSlug, McpEnabled: mirrored.McpEnabled, McpIsPublic: mirrored.McpIsPublic,
		CustomDomainID: mirrored.CustomDomainID, UserSessionIssuerID: mirrored.UserSessionIssuerID,
		ToolVariationsGroupID: mirrored.ToolVariationsGroupID, ID: toolsetID, ProjectID: projectID,
	}); err != nil {
		return fmt.Errorf("mirror hosted MCP state to toolset: %w", err)
	}
	return nil
}

// MirrorOf is toolset with its hosting columns set from its canonical server and endpoints: the platform endpoint
// first, else the oldest one. A disabled server keeps the toolset's public flag, as the column pair encodes it, and
// a server with no endpoint keeps the toolset's address.
func MirrorOf(toolset toolsetsrepo.Toolset, server mcpserversrepo.McpServer, endpoints []mcpendpointsrepo.McpEndpoint) toolsetsrepo.Toolset {
	if server.Name.Valid && server.Name.String != "" {
		toolset.Name = server.Name.String
	}
	switch server.Visibility {
	case visibility.Public:
		toolset.McpEnabled, toolset.McpIsPublic = true, true
	case visibility.Private:
		toolset.McpEnabled, toolset.McpIsPublic = true, false
	default:
		toolset.McpEnabled = false
	}
	toolset.UserSessionIssuerID = server.UserSessionIssuerID
	toolset.ToolVariationsGroupID = server.ToolVariationsGroupID
	var preferred *mcpendpointsrepo.McpEndpoint
	for i := range endpoints {
		candidate := &endpoints[i]
		switch {
		case preferred == nil:
			preferred = candidate
		case !candidate.CustomDomainID.Valid && preferred.CustomDomainID.Valid:
			preferred = candidate
		case candidate.CustomDomainID.Valid == preferred.CustomDomainID.Valid && candidate.CreatedAt.Time.Before(preferred.CreatedAt.Time):
			preferred = candidate
		}
	}
	if preferred != nil {
		toolset.McpSlug = pgtype.Text{String: preferred.Slug, Valid: true}
		toolset.CustomDomainID = preferred.CustomDomainID
	}
	return toolset
}
