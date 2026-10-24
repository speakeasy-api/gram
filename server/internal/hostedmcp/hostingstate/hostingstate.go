// Package hostingstate derives a toolset's MCP hosting state from the server row holding its id. The canonical
// wrapper owns that state; the toolset's own hosting columns stand only without one, apart from the legacy alias
// address, which FollowAlias moves onto the endpoint holding it.
package hostingstate

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
// A disabled server reads as the visibility it re-enables to.
func ApplyServer(toolset toolsetsrepo.Toolset, serverVisibility string, issuer uuid.NullUUID) toolsetsrepo.Toolset {
	toolset.McpEnabled = serverVisibility == visibility.Public || serverVisibility == visibility.Private
	if serverVisibility == visibility.Disabled {
		toolset.McpIsPublic = ReEnableVisibility(toolset) == visibility.Public
	} else {
		toolset.McpIsPublic = serverVisibility == visibility.Public
	}
	toolset.UserSessionIssuerID = issuer
	return toolset
}

// ReEnableVisibility is the visibility a disabled hosted server comes back with: public only when it carries
// external OAuth or an OAuth proxy, which a private server cannot, and private otherwise. The frozen toolset
// public flag does not count, since nothing updates it.
func ReEnableVisibility(toolset toolsetsrepo.Toolset) string {
	if toolset.ExternalOauthServerID.Valid || toolset.OauthProxyServerID.Valid {
		return visibility.Public
	}
	return visibility.Private
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

// AliasAddress is the address a legacy toolset's MCP alias follows among its hosted server's live endpoints: its
// current address while an endpoint still holds it, else the platform endpoint, else the oldest one. With no live
// endpoint the alias stays. This is the address rule pods that mirror the toolset columns apply, so both agree.
func AliasAddress(toolset toolsetsrepo.Toolset, endpoints []mcpendpointsrepo.McpEndpoint) (pgtype.Text, uuid.NullUUID) {
	var preferred *mcpendpointsrepo.McpEndpoint
	for i := range endpoints {
		candidate := &endpoints[i]
		switch {
		case toolset.McpSlug.Valid && candidate.Slug == toolset.McpSlug.String && candidate.CustomDomainID == toolset.CustomDomainID:
			return toolset.McpSlug, toolset.CustomDomainID
		case preferred == nil:
			preferred = candidate
		case !candidate.CustomDomainID.Valid && preferred.CustomDomainID.Valid:
			preferred = candidate
		case candidate.CustomDomainID.Valid == preferred.CustomDomainID.Valid && candidate.CreatedAt.Time.Before(preferred.CreatedAt.Time):
			preferred = candidate
		}
	}
	if preferred == nil {
		return toolset.McpSlug, toolset.CustomDomainID
	}
	return pgtype.Text{String: preferred.Slug, Valid: true}, preferred.CustomDomainID
}

// FollowAlias moves a legacy toolset's MCP alias to the endpoint AliasAddress picks, after a write to its hosted
// server's endpoints. Toolsets without an alias are left alone. The caller holds the toolset row lock.
func FollowAlias(ctx context.Context, tx toolsetsrepo.DBTX, organizationID string, projectID, toolsetID uuid.UUID) error {
	toolset, err := toolsetsrepo.New(tx).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: toolsetID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load hosted toolset")
	case !toolset.McpSlug.Valid || toolset.McpSlug.String == "":
		return nil
	}
	rows, err := mcpendpointsrepo.New(tx).ListMCPEndpointsOnLiveDomainsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsOnLiveDomainsByMCPServerIDParams{
		OrganizationID: organizationID, ProjectID: projectID, McpServerID: toolsetID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list hosted MCP endpoints")
	}
	live := make([]mcpendpointsrepo.McpEndpoint, 0, len(rows))
	for _, row := range rows {
		live = append(live, row.McpEndpoint)
	}
	slug, domainID := AliasAddress(toolset, live)
	if slug == toolset.McpSlug && domainID == toolset.CustomDomainID {
		return nil
	}
	if err := toolsetsrepo.New(tx).MoveToolsetMCPAlias(ctx, toolsetsrepo.MoveToolsetMCPAliasParams{McpSlug: slug, CustomDomainID: domainID, ID: toolsetID, ProjectID: projectID}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return oops.E(oops.CodeConflict, err, "this address is already taken by another toolset")
		}
		return oops.E(oops.CodeUnexpected, err, "move hosted MCP alias")
	}
	return nil
}
