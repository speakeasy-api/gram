package remotemcp

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

// scopeOutcome is how a protected resource's scopes_supported relates to the
// scopes its authorization server advertises.
type scopeOutcome string

const (
	// scopeOutcomeUnknown: either metadata document omits scopes_supported.
	scopeOutcomeUnknown scopeOutcome = "unknown"
	// scopeOutcomeMatch: both advertise the same set.
	scopeOutcomeMatch scopeOutcome = "match"
	// scopeOutcomeResourceSubset: every resource scope is advertised by the issuer, which advertises more.
	scopeOutcomeResourceSubset scopeOutcome = "resource_subset"
	// scopeOutcomeResourceSuperset: every issuer scope is advertised by the resource, which advertises more.
	scopeOutcomeResourceSuperset scopeOutcome = "resource_superset"
	// scopeOutcomeDisjoint: the two sets share nothing.
	scopeOutcomeDisjoint scopeOutcome = "disjoint"
	// scopeOutcomePartialOverlap: the two sets share some scopes and each has its own.
	scopeOutcomePartialOverlap scopeOutcome = "partial_overlap"
)

// compareScopes relates a resource document's scopes_supported (nil when the
// member is omitted) to its issuer's. Both sides are treated as sets.
func compareScopes(resource, issuer []string) scopeOutcome {
	if resource == nil || issuer == nil {
		return scopeOutcomeUnknown
	}
	resourceSet := make(map[string]struct{}, len(resource))
	for _, s := range resource {
		resourceSet[s] = struct{}{}
	}
	issuerSet := make(map[string]struct{}, len(issuer))
	for _, s := range issuer {
		issuerSet[s] = struct{}{}
	}
	shared := 0
	for s := range resourceSet {
		if _, ok := issuerSet[s]; ok {
			shared++
		}
	}
	resourceOnly := len(resourceSet) - shared
	issuerOnly := len(issuerSet) - shared
	switch {
	case resourceOnly == 0 && issuerOnly == 0:
		return scopeOutcomeMatch
	case resourceOnly == 0:
		return scopeOutcomeResourceSubset
	case issuerOnly == 0:
		return scopeOutcomeResourceSuperset
	case shared == 0:
		return scopeOutcomeDisjoint
	default:
		return scopeOutcomePartialOverlap
	}
}

// logScopeComparison emits one line relating the document's scopes to the
// first claimed client's issuer, when that issuer's metadata has been read.
func logScopeComparison(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, serverID uuid.UUID, resourceURL string, doc wellknown.OAuthProtectedResourceMetadata, clients []resourceClient) {
	for _, rc := range clients {
		if !rc.client.MetadataFetchedAt.Valid {
			continue
		}
		logger.InfoContext(ctx, "protected resource scopes compared with issuer",
			attr.SlogProjectID(projectID.String()),
			attr.SlogRemoteMCPServerID(serverID.String()),
			attr.SlogURLFull(urls.DiagnosticURL(resourceURL)),
			attr.SlogOAuthIssuer(rc.client.IssuerUrl),
			attr.SlogOAuthScopeComparison(compareScopes(doc.ScopesSupported, rc.client.ScopesSupported)),
			attr.SlogOAuthResourceScopesSupported(doc.ScopesSupported),
			attr.SlogOAuthIssuerScopesSupported(rc.client.ScopesSupported),
		)
		return
	}
}
