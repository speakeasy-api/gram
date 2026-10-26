package mv

import (
	"context"
	"fmt"
	"maps"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostingstate"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	tsr "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// toolsetHosting is the MCP hosting state a toolset view reports.
type toolsetHosting struct {
	// McpSlug is the toolset's preferred address slug.
	McpSlug *types.Slug

	// CustomDomainID is the custom domain McpSlug lives on; nil means the platform domain.
	CustomDomainID *string

	// McpEnabled reports whether the hosted server serves at all.
	McpEnabled bool

	// McpIsPublic reports whether the hosted server serves without authentication.
	McpIsPublic bool

	// UserSessionIssuerID is the issuer the hosted server authenticates against.
	UserSessionIssuerID uuid.NullUUID
}

// canonicalHosting loads the server rows holding the given toolsets' ids in one query, keyed by toolset id.
func canonicalHosting(ctx context.Context, db tsr.DBTX, organizationID string, toolsetIDs []uuid.UUID) (map[uuid.UUID]hostingstate.Row, error) {
	byID, err := hostingstate.Load(ctx, db, organizationID, toolsetIDs)
	if err != nil {
		return nil, fmt.Errorf("load hosted MCP state: %w", err)
	}
	return byID, nil
}

// deriveToolsetHosting applies hostingstate.Derive given the toolset's legacy alias and external OAuth server, which
// decides how a disabled server re-enables.
func deriveToolsetHosting(alias hostingstate.Address, externalOAuthServerID uuid.NullUUID, row *hostingstate.Row) toolsetHosting {
	hosting, address := hostingstate.Derive(alias, externalOAuthServerID, row)
	return toolsetHosting{
		McpSlug:             conv.FromPGText[types.Slug](address.Slug),
		CustomDomainID:      conv.FromNullableUUID(address.CustomDomainID),
		McpEnabled:          hosting.Enabled,
		McpIsPublic:         hosting.Public,
		UserSessionIssuerID: hosting.UserSessionIssuerID,
	}
}

// toolsetAliases loads the legacy alias of each canonically hosted toolset without a servable endpoint, the only
// toolsets whose view reports it; toolsetProjects maps toolset ids to their projects.
func toolsetAliases(ctx context.Context, db tsr.DBTX, hostingByID map[uuid.UUID]hostingstate.Row, toolsetProjects map[uuid.UUID]uuid.UUID) (map[uuid.UUID]hostingstate.Address, error) {
	byProject := make(map[uuid.UUID][]uuid.UUID)
	for id, projectID := range toolsetProjects {
		if row, ok := hostingByID[id]; ok && row.Canonical && !row.HasEndpoint {
			byProject[projectID] = append(byProject[projectID], id)
		}
	}
	aliases := make(map[uuid.UUID]hostingstate.Address)
	for projectID, ids := range byProject {
		listed, err := legacyalias.List(ctx, db, projectID, ids)
		if err != nil {
			return nil, fmt.Errorf("load legacy toolset aliases: %w", err)
		}
		maps.Copy(aliases, listed)
	}
	return aliases, nil
}
