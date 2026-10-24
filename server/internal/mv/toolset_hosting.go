package mv

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostingstate"
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

// toolsetHostingColumns are the toolset columns the hosting derivation reads, including the OAuth attachments that
// decide how a disabled server re-enables.
type toolsetHostingColumns struct {
	McpSlug               pgtype.Text
	CustomDomainID        uuid.NullUUID
	McpEnabled            bool
	McpIsPublic           bool
	UserSessionIssuerID   uuid.NullUUID
	ExternalOauthServerID uuid.NullUUID
	OauthProxyServerID    uuid.NullUUID
}

// deriveToolsetHosting applies hostingstate.Apply to a toolset's hosting columns.
func deriveToolsetHosting(columns toolsetHostingColumns, row *hostingstate.Row) toolsetHosting {
	//nolint:exhaustruct // Only the hosting columns feed the derivation.
	derived := hostingstate.Apply(tsr.Toolset{
		McpSlug: columns.McpSlug, CustomDomainID: columns.CustomDomainID, McpEnabled: columns.McpEnabled, McpIsPublic: columns.McpIsPublic,
		UserSessionIssuerID: columns.UserSessionIssuerID, ExternalOauthServerID: columns.ExternalOauthServerID, OauthProxyServerID: columns.OauthProxyServerID,
	}, row)
	return toolsetHosting{
		McpSlug:             conv.FromPGText[types.Slug](derived.McpSlug),
		CustomDomainID:      conv.FromNullableUUID(derived.CustomDomainID),
		McpEnabled:          derived.McpEnabled,
		McpIsPublic:         derived.McpIsPublic,
		UserSessionIssuerID: derived.UserSessionIssuerID,
	}
}
