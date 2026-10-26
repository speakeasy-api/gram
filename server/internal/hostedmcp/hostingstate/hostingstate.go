// Package hostingstate derives a toolset's MCP hosting state from its canonical wrapper, the server row holding its
// id; a toolset without one is not hosted.
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

// Derive is a toolset's hosting state and preferred address given the server row holding its id and the toolset's
// legacy alias. Only a canonical wrapper hosts the toolset; without a servable endpoint its alias, which still
// serves, is its address.
func Derive(alias Address, externalOAuthServerID uuid.NullUUID, row *Row) (Hosting, Address) {
	if row == nil || !row.Canonical {
		return NotHosted(), Address{Slug: pgtype.Text{String: "", Valid: false}, CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
	}
	hosting := FromServer(externalOAuthServerID, row.Visibility, row.UserSessionIssuerID)
	if !row.HasEndpoint {
		return hosting, alias
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
