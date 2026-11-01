// Package protectedresource records what Speakeasy learns about a remote MCP
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
	"github.com/jackc/pgx/v5"
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
	return recordDocument(ctx, db, projectID, orgID, resourceURL, doc, false)
}

// RecordIfChanged is Record for a login's read: the row is rewritten only
// when the document changed, a failure is on record, or the last good read
// is over a day old.
func RecordIfChanged(ctx context.Context, db repo.DBTX, projectID uuid.UUID, orgID, resourceURL string, doc wellknown.OAuthProtectedResourceMetadata) error {
	return recordDocument(ctx, db, projectID, orgID, resourceURL, doc, true)
}

func upsert(ctx context.Context, db repo.DBTX, projectID uuid.UUID, orgID, resourceURL string, doc wellknown.OAuthProtectedResourceMetadata, onlyIfChanged bool) error {
	params := repo.UpsertRemoteProtectedResourceParams{
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
	}
	var err error
	if onlyIfChanged {
		_, err = repo.New(db).UpsertRemoteProtectedResourceIfChanged(ctx, repo.UpsertRemoteProtectedResourceIfChangedParams(params))
	} else {
		_, err = repo.New(db).UpsertRemoteProtectedResource(ctx, params)
	}
	if err != nil {
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
	return recordWithEMASync(ctx, db, projectID, orgID, resourceURL, probeErr.ProbeURL, func(tx pgx.Tx) error {
		return recordError(ctx, tx, projectID, orgID, resourceURL, probeErr.ProbeURL, probeErr.UserMessage())
	})
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
