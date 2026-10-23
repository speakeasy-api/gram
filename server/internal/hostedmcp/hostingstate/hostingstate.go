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
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// Row is the server row holding a toolset's id, as ListCanonicalHostingByToolsetIDs returns it.
type Row = toolsetsrepo.ListCanonicalHostingByToolsetIDsRow

// ApplyServer sets toolset's enabled, public and issuer state from its canonical wrapper's visibility and issuer.
// A disabled server reads as public when it would re-enable public: its frozen alias was public or it carries
// external OAuth, which a private server cannot.
func ApplyServer(toolset toolsetsrepo.Toolset, serverVisibility string, issuer uuid.NullUUID) toolsetsrepo.Toolset {
	toolset.McpEnabled = serverVisibility == visibility.Public || serverVisibility == visibility.Private
	if serverVisibility == visibility.Disabled {
		toolset.McpIsPublic = toolset.McpIsPublic || toolset.ExternalOauthServerID.Valid || toolset.OauthProxyServerID.Valid
	} else {
		toolset.McpIsPublic = serverVisibility == visibility.Public
	}
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
