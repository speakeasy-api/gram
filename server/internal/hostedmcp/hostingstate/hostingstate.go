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

// Hosting is a toolset's MCP hosting state.
type Hosting struct {
	// Enabled reports whether the hosted server serves at all.
	Enabled bool

	// Public reports whether the hosted server serves without authentication.
	Public bool

	// UserSessionIssuerID is the issuer the hosted server authenticates against.
	UserSessionIssuerID uuid.NullUUID
}

// Address is a toolset's preferred MCP address; an invalid Slug means it has none.
type Address struct {
	Slug pgtype.Text

	// CustomDomainID is the custom domain Slug lives on; invalid means the platform domain.
	CustomDomainID uuid.NullUUID
}

// NotHosted is the hosting state of a toolset nothing serves.
func NotHosted() Hosting {
	return Hosting{Enabled: false, Public: false, UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
}

// LegacyAlias is the legacy /mcp/<slug> address a toolset created before hosted servers owned their endpoints still
// answers on (AIM-670). Hosting readers use it instead of reading the toolset's address columns directly.
func LegacyAlias(toolset toolsetsrepo.Toolset) Address {
	return Address{Slug: toolset.McpSlug, CustomDomainID: toolset.CustomDomainID}
}

// Columns are a toolset's own hosting columns. Hosting stands only without a canonical wrapper; Address also stands
// when the wrapper has no servable endpoint.
type Columns struct {
	Hosting
	Address
}

// ToolsetColumns reads toolset's own hosting columns.
func ToolsetColumns(toolset toolsetsrepo.Toolset) Columns {
	return Columns{
		Hosting: Hosting{Enabled: toolset.McpEnabled, Public: toolset.McpIsPublic, UserSessionIssuerID: toolset.UserSessionIssuerID},
		Address: LegacyAlias(toolset),
	}
}

// FromServer is the hosting state of a canonical wrapper with the given visibility and issuer. A disabled server
// reads as the visibility it re-enables to, which depends on the toolset's external OAuth server.
func FromServer(externalOAuthServerID uuid.NullUUID, serverVisibility string, issuer uuid.NullUUID) Hosting {
	public := serverVisibility == visibility.Public
	if serverVisibility == visibility.Disabled {
		public = reEnableVisibility(externalOAuthServerID) == visibility.Public
	}
	return Hosting{
		Enabled:             serverVisibility == visibility.Public || serverVisibility == visibility.Private,
		Public:              public,
		UserSessionIssuerID: issuer,
	}
}

// ReEnableVisibility is the visibility a disabled hosted server comes back with: public only when it carries
// external OAuth, which a private server cannot, and private otherwise. The frozen toolset public flag does not
// count, since nothing updates it, and neither does the retired OAuth proxy link.
func ReEnableVisibility(toolset toolsetsrepo.Toolset) string {
	return reEnableVisibility(toolset.ExternalOauthServerID)
}

func reEnableVisibility(externalOAuthServerID uuid.NullUUID) string {
	if externalOAuthServerID.Valid {
		return visibility.Public
	}
	return visibility.Private
}

// Kept is the hosting state of a toolset whose id a non-canonical server kept after leaving it: nothing serves it.
func Kept(issuer uuid.NullUUID) Hosting {
	return Hosting{Enabled: false, Public: false, UserSessionIssuerID: issuer}
}

// Derive is a toolset's hosting state and preferred address given the server row holding its id. A nil row leaves
// the toolset columns standing; a canonical wrapper without a servable endpoint keeps the toolset's address.
func Derive(columns Columns, externalOAuthServerID uuid.NullUUID, row *Row) (Hosting, Address) {
	switch {
	case row == nil:
		return columns.Hosting, columns.Address
	case !row.Canonical:
		return Kept(columns.UserSessionIssuerID), columns.Address
	}
	hosting := FromServer(externalOAuthServerID, row.Visibility, row.UserSessionIssuerID)
	if !row.HasEndpoint {
		return hosting, columns.Address
	}
	return hosting, Address{Slug: pgtype.Text{String: row.EndpointSlug, Valid: true}, CustomDomainID: row.EndpointCustomDomainID}
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
	alias := LegacyAlias(toolset)
	var preferred *mcpendpointsrepo.McpEndpoint
	for i := range endpoints {
		candidate := &endpoints[i]
		switch {
		case alias.Slug.Valid && candidate.Slug == alias.Slug.String && candidate.CustomDomainID == alias.CustomDomainID:
			return alias.Slug, alias.CustomDomainID
		case preferred == nil:
			preferred = candidate
		case !candidate.CustomDomainID.Valid && preferred.CustomDomainID.Valid:
			preferred = candidate
		case candidate.CustomDomainID.Valid == preferred.CustomDomainID.Valid && candidate.CreatedAt.Time.Before(preferred.CreatedAt.Time):
			preferred = candidate
		}
	}
	if preferred == nil {
		return alias.Slug, alias.CustomDomainID
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
	}
	alias := LegacyAlias(toolset)
	if !alias.Slug.Valid || alias.Slug.String == "" {
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
	if slug == alias.Slug && domainID == alias.CustomDomainID {
		return nil
	}
	if domainID.Valid && slug.Valid {
		// The platform host also serves custom-domain aliases, so the move must not contend with another one there.
		if err := mcpendpointsrepo.New(tx).LockSlugScope(ctx, mcpendpointsrepo.LockSlugScopeParams{CustomDomainID: domainID, Slug: slug.String}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "lock hosted MCP alias address")
		}
		served, err := toolsetsrepo.New(tx).OtherCustomDomainAliasServedOnPlatform(ctx, toolsetsrepo.OtherCustomDomainAliasServedOnPlatformParams{Slug: slug, ToolsetID: toolsetID})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "check hosted MCP alias address")
		}
		if served {
			return oops.E(oops.CodeConflict, nil, "this address is already taken by another toolset")
		}
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
