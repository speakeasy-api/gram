package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// apiKeyAdmission is a live legacy key and its own policy. ProjectID and
// Scopes are the key's existing limits: its policy can narrow them, never
// widen them.
type apiKeyAdmission struct {
	ProjectID uuid.NullUUID
	Scopes    []string
	Grants    []Grant
}

// mcpTransportScopes are the key transport scopes that reach MCP servers,
// mirroring auth.APIKeyScope: producer implies consumer and chat.
var mcpTransportScopes = []string{"consumer", "producer", "chat"}

func (a apiKeyAdmission) reachesMCP() bool {
	return APIKeyReachesMCP(a.Scopes)
}

// APIKeyReachesMCP reports whether a key's transport scopes reach MCP servers.
func APIKeyReachesMCP(scopes []string) bool {
	return slices.ContainsFunc(scopes, func(scope string) bool { return slices.Contains(mcpTransportScopes, scope) })
}

// admitAPIKey admits a legacy API key as its own principal: its live row must
// be in the tenant, unrevoked and unexpired, and it holds only the grants
// written to api_key:<id>. It never inherits user:all, its creator's grants,
// or a direct-user override. An unknown or inactive key is ErrPrincipalNotFound.
func admitAPIKey(ctx context.Context, db repo.DBTX, organizationID string, apiKeyID uuid.UUID) (apiKeyAdmission, error) {
	key, err := keysrepo.New(db).GetActiveLegacyAPIKeyForAdmission(ctx, keysrepo.GetActiveLegacyAPIKeyForAdmissionParams{
		ID:             apiKeyID,
		OrganizationID: organizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return apiKeyAdmission{}, fmt.Errorf("admit api key %q: %w", apiKeyID, ErrPrincipalNotFound)
	}
	if err != nil {
		return apiKeyAdmission{}, fmt.Errorf("admit api key %q: %w", apiKeyID, err)
	}
	grants, err := LoadGrants(ctx, db, organizationID, []urn.Principal{urn.NewAPIKeyPrincipal(apiKeyID)})
	if err != nil {
		return apiKeyAdmission{}, fmt.Errorf("load api key policy: %w", err)
	}
	return apiKeyAdmission{ProjectID: key.ProjectID, Scopes: key.Scopes, Grants: grants}, nil
}

// isAPIKeyGrant reports whether grant is held by a legacy API key, whose grants
// always match strictly.
func isAPIKeyGrant(grant *Grant) bool {
	return strings.HasPrefix(grant.PrincipalUrn, string(urn.PrincipalTypeAPIKey)+":")
}

// apiKeyGrantMatches matches a legacy API key's grant. It is strict, and an
// MCP check must name its server's project: a key's server grants are scoped
// by project, so a check without one could pass an allow while missing a block.
func apiKeyGrantMatches(grant *Grant, check *Check) bool {
	kind := check.ResourceKind
	if kind == "" {
		kind = ResourceKindForScope(check.Scope)
	}
	if kind == ResourceKindMCP && check.Dimensions[SelectorKeyProjectID] == "" {
		return false
	}
	return grant.Selector.strictMatches(check.selector(), check.representedUser)
}
