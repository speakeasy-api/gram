// Package protectedresource records what Gram learns about a remote MCP
// server's RFC 9728 protected resource metadata and keeps that record fresh,
// off the request path for proxied traffic and within a short budget for a
// login. It is the only writer of remote_protected_resources discovery
// columns, so every probe applies the same resource-identifier check.
package protectedresource

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

// Record stores the document read for resourceURL. The resource identifier
// and canonical metadata location must both match. A mismatch records a
// failed fetch without replacing the last good document. Stored metadata_url
// is diagnostic only; validate the full URL before redacting.
func Record(ctx context.Context, db repo.DBTX, projectID uuid.UUID, orgID, resourceURL string, doc wellknown.OAuthProtectedResourceMetadata) error {
	// RFC 9728 §§3.3 and 6 require exact equality, not URL equivalence.
	// Keep this check at the write boundary so every discovery path rejects
	// mismatches, including identifiers differing only by a trailing slash.
	if !doc.ValidForResource(resourceURL) {
		return recordError(ctx, db, projectID, orgID, resourceURL, doc.MetadataURL, mismatchMessage(resourceURL, doc))
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
		if isDataError(err) {
			// Go accepts JSON values (including escaped NULs and large numbers)
			// that PostgreSQL cannot store in jsonb or extracted text columns.
			// Record a safe fetch failure so on-use probes keep their backoff.
			return recordError(ctx, db, projectID, orgID, resourceURL, doc.MetadataURL, "The metadata document contains values that cannot be stored.")
		}
		return fmt.Errorf("upsert remote protected resource: %w", err)
	}
	return nil
}

func isDataError(err error) bool {
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

// RecordFetchError stores the public-safe reason the last read of resourceURL failed.
func RecordFetchError(ctx context.Context, db repo.DBTX, projectID uuid.UUID, orgID, resourceURL string, probeErr *wellknown.ProtectedResourceDiscoveryError) error {
	return recordError(ctx, db, projectID, orgID, resourceURL, probeErr.ProbeURL, probeErr.UserMessage())
}

// recordError stores message as the last fetch failure of resourceURL,
// creating the row when none exists.
func recordError(ctx context.Context, db repo.DBTX, projectID uuid.UUID, orgID, resourceURL, metadataURL, message string) error {
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

// mismatchMessage is recorded when the document's resource member or
// metadata location does not match the server's URL, naming which.
func mismatchMessage(resourceURL string, doc wellknown.OAuthProtectedResourceMetadata) string {
	if doc.Resource != resourceURL {
		return "The metadata document names the resource " + recordedURL(doc.Resource) + ", not the requested one."
	}
	return "The metadata document was read from " + recordedURL(doc.MetadataURL) + ", not the resource's well-known location."
}

// recordedURL redacts and bounds an upstream-supplied URL before it is stored.
func recordedURL(v string) string {
	const limit = 200
	v = urls.DiagnosticURL(v)
	if v == "" {
		return "(empty)"
	}
	if r := []rune(v); len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return v
}
