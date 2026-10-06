package remotemcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
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

// recordProtectedResource stores the document read for resourceURL. The
// resource identifier and canonical metadata location must both match. A
// mismatch records a failed fetch without replacing the last good document.
// Stored metadata_url is diagnostic only; validate the full URL before redacting.
func recordProtectedResource(ctx context.Context, db repo.DBTX, projectID uuid.UUID, orgID, resourceURL string, doc wellknown.OAuthProtectedResourceMetadata) error {
	// RFC 9728 §§3.3 and 6 require exact equality, not URL equivalence.
	// Keep this check at the write boundary so every discovery path rejects
	// mismatches, including identifiers differing only by a trailing slash.
	if !doc.ValidForResource(resourceURL) {
		return recordProtectedResourceError(ctx, db, projectID, orgID, resourceURL, doc.MetadataURL, protectedResourceMismatchMessage)
	}

	_, err := repo.New(db).UpsertRemoteProtectedResource(ctx, repo.UpsertRemoteProtectedResourceParams{
		ProjectID:                             projectID,
		OrganizationID:                        orgID,
		ResourceIdentifier:                    resourceURL,
		MetadataUrl:                           urls.DiagnosticURL(doc.MetadataURL),
		AuthorizationServers:                  doc.AuthorizationServers,
		ScopesSupported:                       doc.ScopesSupported,
		BearerMethodsSupported:                doc.BearerMethodsSupported,
		ResourceName:                          doc.ResourceName,
		ResourceDocumentation:                 doc.ResourceDocumentation,
		ResourcePolicyUri:                     doc.ResourcePolicyURI,
		ResourceTosUri:                        doc.ResourceTosURI,
		DpopBoundAccessTokensRequired:         conv.PtrToPGBool(doc.DPoPBoundAccessTokensRequired),
		DpopSigningAlgValuesSupported:         doc.DPoPSigningAlgValuesSupported,
		TlsClientCertificateBoundAccessTokens: conv.PtrToPGBool(doc.TLSClientCertificateBoundAccessTokens),
		Metadata:                              string(doc.Raw),
	})
	if err != nil {
		if isProtectedResourceDataError(err) {
			// Go accepts JSON values (including escaped NULs and large numbers)
			// that PostgreSQL cannot store in jsonb or extracted text columns.
			// Record a safe fetch failure so on-use probes keep their backoff.
			return recordProtectedResourceError(ctx, db, projectID, orgID, resourceURL, doc.MetadataURL, "The metadata document contains values that cannot be stored.")
		}
		return fmt.Errorf("upsert remote protected resource: %w", err)
	}
	return nil
}

func isProtectedResourceDataError(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return false
	}
	switch pgErr.Code {
	case pgerrcode.UntranslatableCharacter, pgerrcode.CharacterNotInRepertoire,
		pgerrcode.InvalidTextRepresentation, pgerrcode.NumericValueOutOfRange:
		return true
	default:
		return false
	}
}

// recordProtectedResourceFetchError stores the public-safe reason the last
// read of resourceURL failed.
func recordProtectedResourceFetchError(ctx context.Context, db repo.DBTX, projectID uuid.UUID, orgID, resourceURL string, probeErr *wellknown.ProtectedResourceDiscoveryError) error {
	return recordProtectedResourceError(ctx, db, projectID, orgID, resourceURL, probeErr.ProbeURL, probeErr.UserMessage())
}

func recordProtectedResourceError(ctx context.Context, db repo.DBTX, projectID uuid.UUID, orgID, resourceURL, metadataURL, message string) error {
	_, err := repo.New(db).RecordRemoteProtectedResourceFetchError(ctx, repo.RecordRemoteProtectedResourceFetchErrorParams{
		ProjectID:          projectID,
		OrganizationID:     orgID,
		ResourceIdentifier: resourceURL,
		MetadataUrl:        urls.DiagnosticURL(metadataURL),
		MetadataLastError:  message,
	})
	if err != nil {
		return fmt.Errorf("record remote protected resource fetch error: %w", err)
	}
	return nil
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
