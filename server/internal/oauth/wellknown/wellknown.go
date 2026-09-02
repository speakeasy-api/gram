// Package wellknown resolves OAuth 2.1 well-known metadata for toolsets.
//
// It provides two capabilities:
//   - Determining whether a toolset is OAuth-protected (for oauth-protected-resource)
//   - Resolving OAuth server metadata (for oauth-authorization-server)
//
// Caveats:
//
// This implementation is tightly coupled to the MCP client authentication flow.
// The package's concerns are more broadly useful within Gram, but this revision
// only addresses the immediate client requirements rather than fully describing
// toolset authentication state.
//
// The methods here rely on reading the full toolset model view because OAuth
// state is currently inferred from tool definitions. Eventually, we'd prefer
// explicit user-assigned OAuth configuration on toolsets, and moving OAuth
// protectedness off of tools onto a separate abstraction. To mitigate the
// performance cost, we defer fetching the toolset model view until after
// exhausting other OAuth configuration sources.
package wellknown

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// OAuthAuthorizationServerPath is the well-known URI path for OAuth 2.0
// Authorization Server Metadata as defined by RFC 8414.
//
// https://datatracker.ietf.org/doc/html/rfc8414
const OAuthAuthorizationServerPath = "/.well-known/oauth-authorization-server"

// OAuthProtectedResourcePath is the well-known URI path for OAuth 2.0
// Protected Resource Metadata as defined by RFC 9728.
//
// https://datatracker.ietf.org/doc/html/rfc9728
const OAuthProtectedResourcePath = "/.well-known/oauth-protected-resource"

// OAuthProtectedResourceMetadata represents OAuth 2.0 Protected Resource Metadata (RFC 9728).
//
// Used for both serving Gram's own metadata documents and decoding metadata
// probed from upstream resource servers via [DiscoverProtectedResourceMetadata].
// Fields outside the minimum set required by the existing server-side callers
// are tagged omitempty so adding them does not change emitted documents.
type OAuthProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
	ResourceDocumentation  string   `json:"resource_documentation,omitempty"`
}

// OAuthServerMetadata represents OAuth 2.0 Authorization Server Metadata (RFC 8414).
//
// Every optional member is omitempty. A nil slice would otherwise marshal to
// `null`, which clients modelling these as optional arrays reject outright, and
// an empty slice asserts "supports none of these" rather than "not stated" —
// for response_types_supported that means a client refuses `code`, and for
// registration_endpoint an empty string is not the URL RFC 8414 promises. When
// a value is genuinely unknown, saying nothing lets the client apply the RFC's
// defaults; saying `[]` or `""` does not.
type OAuthServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint,omitempty"`
	RevocationEndpoint                string   `json:"revocation_endpoint,omitempty"`
	JwksURI                           string   `json:"jwks_uri,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	ServiceDocumentation              string   `json:"service_documentation,omitempty"`
	OpPolicyURI                       string   `json:"op_policy_uri,omitempty"`
	OpTosURI                          string   `json:"op_tos_uri,omitempty"`
	// Pointer so the CIMD draft's member is advertised only when the issuer
	// actually supports it; false and absent mean the same thing to a client,
	// and emitting false on every reconstruction would be noise.
	ClientIDMetadataDocumentSupported *bool `json:"client_id_metadata_document_supported,omitempty"`
}

type OAuthServerMetadataResultKind string

const (
	OAuthServerMetadataResultKindStatic OAuthServerMetadataResultKind = "static"
	OAuthServerMetadataResultKindRaw    OAuthServerMetadataResultKind = "raw"
	OAuthServerMetadataResultKindProxy  OAuthServerMetadataResultKind = "proxy"
)

type OAuthServerMetadataResult struct {
	Kind     OAuthServerMetadataResultKind
	Static   *OAuthServerMetadata
	Raw      json.RawMessage
	ProxyURL string
}

type OAuthRepo interface {
	GetExternalOAuthServerMetadata(ctx context.Context, arg repo.GetExternalOAuthServerMetadataParams) (repo.ExternalOauthServerMetadatum, error)
}

// ResolveOAuthServerMetadataFromToolset returns OAuth Authorization Server
// metadata for a toolset, or nil if the toolset is not OAuth-configured.
//
// oauthSlug is the slug used to address the Gram-hosted OAuth endpoints
// (`/oauth/{oauthSlug}/...`). Today the OAuth machinery is keyed by
// `toolsets.mcp_slug`, so callers should pass that value. The /x/mcp
// experimental endpoint uses the same OAuth flow under the hood, so it
// also passes `toolset.mcp_slug` here even though its protected-resource
// URL uses an `mcp_endpoints.slug` instead — see the companion
// resourceURL argument on [ResolveOAuthProtectedResourceFromToolset].
//
// resourceURL is the absolute URL of the protected resource — the same value
// [ResolveOAuthProtectedResourceFromToolset] emits as `resource` and
// `authorization_servers`. For the external-OAuth-server case it becomes the
// served document's `issuer` so the metadata satisfies RFC 8414 §3.3 (the
// served `issuer` must equal the issuer identifier the client fetched it
// under); the proxy case ignores it and keys its issuer off oauthSlug.
func ResolveOAuthServerMetadataFromToolset(
	ctx context.Context,
	logger *slog.Logger,
	db mv.DBTX,
	oauthRepo OAuthRepo,
	toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents],
	toolset *toolsets_repo.Toolset,
	baseURL string,
	oauthSlug string,
	resourceURL string,
) (*OAuthServerMetadataResult, error) {
	if toolset.ExternalOauthServerID.Valid {
		externalOAuthServer, err := oauthRepo.GetExternalOAuthServerMetadata(ctx, repo.GetExternalOAuthServerMetadataParams{
			ProjectID: toolset.ProjectID,
			ID:        toolset.ExternalOauthServerID.UUID,
		})
		if err != nil {
			return nil, fmt.Errorf("get external oauth server metadata: %w", err)
		}

		// The captured upstream document's `issuer` identifies the upstream
		// authorization server, but Gram re-serves that document from its own
		// `/.well-known/oauth-authorization-server/...` URL. RFC 8414 §3.3
		// requires the served `issuer` to equal the issuer identifier the
		// client used to fetch the document — here the Gram resource URL that
		// the protected-resource metadata advertises in
		// `authorization_servers` — so a spec-compliant MCP client does not
		// reject the metadata on a mismatch. The upstream's own
		// authorization/token/registration endpoints are preserved verbatim;
		// RFC 8414 does not require those to share the issuer's origin.
		rewritten, err := rewriteMetadataIssuer(externalOAuthServer.Metadata, resourceURL)
		if err != nil {
			return nil, fmt.Errorf("rewrite external oauth server issuer: %w", err)
		}

		return &OAuthServerMetadataResult{
			Kind:     OAuthServerMetadataResultKindRaw,
			Static:   nil,
			Raw:      rewritten,
			ProxyURL: "",
		}, nil
	}

	return nil, nil
}

// cimdSupported returns a pointer only when the issuer advertises CIMD, so the
// member is omitted rather than emitted as false.
func cimdSupported(supported bool) *bool {
	if !supported {
		return nil
	}
	return &supported
}

// ErrIncompleteIssuerMetadata is a reconstruction that cannot describe a usable
// authorization server, because the issuer has no captured snapshot and no
// stored authorization or token endpoint either.
var ErrIncompleteIssuerMetadata = errors.New("issuer has no authorization or token endpoint to advertise")

// RemoteSessionIssuerMetadata is the part of a remote_session_issuers row the
// well-known surface needs to describe an upstream authorization server. It is
// a plain struct rather than the repo row so this package stays independent of
// the remote sessions schema.
type RemoteSessionIssuerMetadata struct {
	AuthorizationEndpoint             string
	TokenEndpoint                     string
	RegistrationEndpoint              string
	RevocationEndpoint                string
	JwksURI                           string
	ScopesSupported                   []string
	ResponseTypesSupported            []string
	GrantTypesSupported               []string
	TokenEndpointAuthMethodsSupported []string
	CodeChallengeMethodsSupported     []string
	ServiceDocumentation              string
	OpPolicyURI                       string
	OpTosURI                          string
	ClientIDMetadataDocumentSupported bool

	// Snapshot is remote_session_issuers.metadata: the discovery document as
	// the issuer served it, filtered to what Gram will republish. Empty for a
	// row that predates capture or was configured by hand.
	Snapshot json.RawMessage
}

// ResolveOAuthServerMetadataFromRemoteSessionIssuer builds the RFC 8414
// authorization-server document an `upstream` MCP server serves, describing the
// issuer's own authorization server rather than Gram's.
//
// resourceURL is the Gram URL the document is fetched from, and becomes the
// served `issuer`. RFC 8414 §3.3 requires the two to be equal, so a
// spec-compliant MCP client does not reject the metadata; the upstream's own
// authorization, token, and registration endpoints are carried through
// unchanged, which the RFC does not require to share the issuer's origin.
//
// The second return value reports whether the document came from the captured
// snapshot. When it did not, the document was reconstructed from the typed
// columns and is missing whatever OIDC extension fields the upstream advertises
// that Gram does not model, which callers should log: it is serviceable but
// degraded, and it resolves itself the next time the issuer is refreshed.
func ResolveOAuthServerMetadataFromRemoteSessionIssuer(issuer RemoteSessionIssuerMetadata, resourceURL string) (*OAuthServerMetadataResult, bool, error) {
	if len(issuer.Snapshot) > 0 {
		rewritten, err := rewriteMetadataIssuer(issuer.Snapshot, resourceURL)
		if err != nil {
			return nil, false, fmt.Errorf("rewrite remote session issuer metadata: %w", err)
		}
		return &OAuthServerMetadataResult{
			Kind:     OAuthServerMetadataResultKindRaw,
			Static:   nil,
			Raw:      rewritten,
			ProxyURL: "",
		}, true, nil
	}

	// RFC 8414 makes both of these REQUIRED, and a client cannot start a flow
	// without them. An issuer that has neither a snapshot nor endpoints is a
	// row discovery never completed for, so serving 200 with empty strings
	// would advertise a broken authorization server rather than admit there is
	// nothing to advertise. The caller turns this into a not-found.
	if issuer.AuthorizationEndpoint == "" || issuer.TokenEndpoint == "" {
		return nil, false, ErrIncompleteIssuerMetadata
	}

	return &OAuthServerMetadataResult{
		Kind: OAuthServerMetadataResultKindStatic,
		Static: &OAuthServerMetadata{
			Issuer:                resourceURL,
			AuthorizationEndpoint: issuer.AuthorizationEndpoint,
			TokenEndpoint:         issuer.TokenEndpoint,
			RegistrationEndpoint:  issuer.RegistrationEndpoint,
			// Modelled by remote_session_issuers and therefore reconstructable.
			// Omitting token_endpoint_auth_methods_supported in particular is
			// not harmless: RFC 8414 makes its absence mean client_secret_basic,
			// so a public client issuer advertising `none` would fail token
			// exchange on this path while working on the snapshot path.
			RevocationEndpoint:                issuer.RevocationEndpoint,
			JwksURI:                           issuer.JwksURI,
			ScopesSupported:                   issuer.ScopesSupported,
			ResponseTypesSupported:            issuer.ResponseTypesSupported,
			GrantTypesSupported:               issuer.GrantTypesSupported,
			TokenEndpointAuthMethodsSupported: issuer.TokenEndpointAuthMethodsSupported,
			CodeChallengeMethodsSupported:     issuer.CodeChallengeMethodsSupported,
			ServiceDocumentation:              issuer.ServiceDocumentation,
			OpPolicyURI:                       issuer.OpPolicyURI,
			OpTosURI:                          issuer.OpTosURI,
			ClientIDMetadataDocumentSupported: cimdSupported(issuer.ClientIDMetadataDocumentSupported),
		},
		Raw:      nil,
		ProxyURL: "",
	}, false, nil
}

// rewriteMetadataIssuer returns raw with its top-level "issuer" field set to
// issuer, leaving every other field untouched. Used to reconcile a captured
// upstream OAuth authorization-server metadata document with the Gram URL it
// is re-served from (RFC 8414 §3.3). Preserving the raw form of every other
// field keeps upstream-specific extensions (e.g. userinfo/introspection
// endpoints, claims_supported) that Gram's typed structs do not model.
func rewriteMetadataIssuer(raw json.RawMessage, issuer string) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("decode oauth server metadata: %w", err)
	}
	// A JSON `null` payload unmarshals into a nil map without error; guard
	// against it (and any non-object that decoded to nil) so the issuer
	// assignment below does not panic on a nil map.
	if fields == nil {
		return nil, fmt.Errorf("decode oauth server metadata: expected a JSON object")
	}

	issuerJSON, err := json.Marshal(issuer)
	if err != nil {
		return nil, fmt.Errorf("encode issuer: %w", err)
	}
	fields["issuer"] = issuerJSON

	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode oauth server metadata: %w", err)
	}
	return out, nil
}

// ResolveOAuthProtectedResourceFromToolset returns OAuth Protected Resource
// Metadata for a toolset, or nil if the toolset is not OAuth-protected.
//
// resourceURL is the absolute URL of the protected resource (the runtime MCP
// endpoint). For /mcp callers this is `<baseURL>/mcp/<toolset.mcp_slug>`; for
// /x/mcp callers this is `<baseURL>/x/mcp/<mcp_endpoint.slug>`. It is used
// verbatim for both `resource` and `authorization_servers` so that the
// `/.well-known/...` discovery path on the protected resource resolves back
// to the Gram-hosted authorization server metadata.
func ResolveOAuthProtectedResourceFromToolset(
	ctx context.Context,
	logger *slog.Logger,
	db mv.DBTX,
	toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents],
	toolset *toolsets_repo.Toolset,
	resourceURL string,
) (*OAuthProtectedResourceMetadata, error) {
	// Check for external OAuth server configuration
	if toolset.ExternalOauthServerID.Valid {
		return &OAuthProtectedResourceMetadata{
			Resource:               resourceURL,
			AuthorizationServers:   []string{resourceURL},
			ScopesSupported:        nil,
			BearerMethodsSupported: nil,
			ResourceDocumentation:  "",
		}, nil
	}

	return nil, nil
}
