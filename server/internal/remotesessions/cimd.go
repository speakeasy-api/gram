// cimd.go implements outbound OAuth Client ID Metadata Document (CIMD,
// draft-ietf-oauth-client-id-metadata-document) support for remote-session
// clients. A CIMD-mode client publishes a JSON metadata document at a stable
// Gram URL and sends that URL as its client_id on every outbound OAuth call;
// the upstream Authorization Server dereferences the URL to fetch this
// document. The builder here renders that document; HandleClientMetadataDocument
// serves it at the public, unauthenticated endpoint.

package remotesessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/httpcache"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// clientMetadataDocumentMaxAgeSeconds is the Cache-Control max-age for served
// CIMD documents. Longer than the well-known metadata TTL: a document is keyed
// by an immutable client_id and changes only if the client's scope or callback
// does, so upstream Authorization Servers can safely cache it for an hour.
const clientMetadataDocumentMaxAgeSeconds = 3600

// cimdClientName is the client_name Gram publishes in every CIMD document. It
// is what the end user sees on the upstream's consent screen, so it carries the
// Speakeasy brand customers recognize. There is no per-client name column yet;
// a static value keeps the document free of any project/MCP-server-scoped state
// (clients are addressed by their globally unique id, and one client can back
// many MCP servers).
const cimdClientName = "Speakeasy"

// clientMetadataDocumentPath is the URL path prefix Gram serves CIMD documents
// under. Not an IANA-registered well-known location — the CIMD draft only
// requires an HTTPS URL with a path component — but placed under /.well-known
// for parity with the sibling OAuth metadata documents.
const clientMetadataDocumentPath = "/.well-known/oauth-client/"

// ClientMetadataDocumentURL builds the platform-canonical CIMD document URL for
// a client id. serverURL is the Gram deployment's public base URL; the path
// component is the client's globally unique primary key. This is the value
// stored as both client_id and client_id_metadata_uri on a CIMD-mode row and
// the URL Gram sends upstream as client_id.
func ClientMetadataDocumentURL(serverURL *url.URL, clientID uuid.UUID) string {
	return strings.TrimRight(serverURL.String(), "/") + clientMetadataDocumentPath + clientID.String()
}

// clientMetadataDocument is the JSON body served at the CIMD endpoint. Fields
// follow RFC 7591 client metadata as referenced by the CIMD draft. scope is a
// space-delimited string per RFC 7591 §2 (not an array).
type clientMetadataDocument struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope,omitempty"`
}

// BuildClientMetadataDocument renders the CIMD document for a client. clientID
// MUST equal the URL the document is served at (the CIMD invariant the upstream
// AS validates); redirectURI is Gram's outbound callback for this deployment;
// scope is the client's explicit upstream scopes, omitted when empty.
func BuildClientMetadataDocument(clientID, redirectURI string, scope []string) clientMetadataDocument {
	return clientMetadataDocument{
		ClientID:                clientID,
		ClientName:              cimdClientName,
		RedirectURIs:            []string{redirectURI},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: string(TokenEndpointAuthMethodNone),
		Scope:                   strings.Join(scope, " "),
	}
}

// preflightCIMDIssuer rejects opting a client into CIMD mode against an issuer
// that cannot support it. The issuer must advertise client_id_metadata_document
// support (the CIMD-draft capability flag, parsed during discovery). It must
// also accept the "none" token endpoint auth method that CIMD's public clients
// use — but only when the issuer enumerated its supported methods; an empty set
// means the issuer did not advertise them, so we do not second-guess it.
func preflightCIMDIssuer(issuer remotesessions_repo.RemoteSessionIssuer) error {
	if !issuer.ClientIDMetadataDocumentSupported {
		return fmt.Errorf("issuer %q does not advertise client_id_metadata_document_supported", issuer.Slug)
	}
	if methods := issuer.TokenEndpointAuthMethodsSupported; len(methods) > 0 && !slices.Contains(methods, string(TokenEndpointAuthMethodNone)) {
		return fmt.Errorf("issuer %q does not advertise the none token_endpoint_auth_method required for client id metadata documents", issuer.Slug)
	}
	return nil
}

// HandleClientMetadataDocument serves the public, unauthenticated CIMD document
// at GET /.well-known/oauth-client/{id}. The client is resolved by its globally
// unique primary key; only CIMD-mode rows (non-null client_id_metadata_uri) are
// served, everything else 404s.
func (m *ChallengeManager) HandleClientMetadataDocument(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// CIMD documents publish a platform-canonical client_id. This route is also
	// reachable on verified custom domains (the mux is host-agnostic), but a
	// document served there would advertise a custom-domain client_id that no
	// outbound /authorize ever sent, so a strict upstream AS would reject it.
	// Pin the document to the platform host: 404 when reached via a custom
	// domain.
	if customdomains.FromContext(ctx) != nil {
		return oops.E(oops.CodeNotFound, nil, "client metadata document not found")
	}

	clientID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		// An unparseable id is an unknown client, not a client error worth a 400.
		return oops.E(oops.CodeNotFound, err, "client metadata document not found")
	}

	row, err := remotesessions_repo.New(m.db).GetRemoteSessionClientForClientMetadataDocument(ctx, clientID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "client metadata document not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load client metadata document").LogError(ctx, m.logger)
	}

	// client_id is the stored canonical URL (== the value sent upstream), not a
	// host-derived one, so it always matches what the AS dereferenced.
	doc := BuildClientMetadataDocument(row.ClientIDMetadataUri.String, m.callbackURL(canonicalCallbackRouteBase), row.Scope)

	body, err := json.Marshal(doc)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "marshal client metadata document").LogError(ctx, m.logger)
	}

	//nolint:wrapcheck // helper writes the response; its error is already a contextual oops error.
	return httpcache.WriteCacheableJSON(ctx, w, r, m.logger, "application/json; charset=utf-8", clientMetadataDocumentMaxAgeSeconds, body)
}
