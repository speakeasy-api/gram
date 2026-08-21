package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/httpcache"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	// assistantClientMetadataDocumentMaxAgeSeconds is the Cache-Control max-age
	// for served assistant CIMD documents. Longer than well-known metadata:
	// the document is keyed by an immutable assistant id and changes only if
	// the assistant is renamed or its callback moves.
	assistantClientMetadataDocumentMaxAgeSeconds = 3600

	// assistantClientMetadataDocumentPath is the URL path prefix Gram serves
	// assistant CIMD documents under. Distinct from the remote-session path
	// at /.well-known/oauth-client/{id}. The CIMD draft only requires an
	// HTTPS URL with a path component.
	assistantClientMetadataDocumentPath = "/.well-known/oauth-client/assistants/"

	mcpOAuthTokenEndpointAuthNone = "none"
)

// AssistantClientMetadataDocumentURL builds the platform-canonical CIMD
// document URL for an assistant. serverURL is the Gram deployment's public
// API base; the path component is the assistant's globally unique id. This
// is the value stored as both client_id and client_id_metadata_uri on a
// CIMD-mode row and the URL Gram sends upstream as client_id.
func AssistantClientMetadataDocumentURL(serverURL *url.URL, assistantID uuid.UUID) string {
	return strings.TrimRight(serverURL.String(), "/") + assistantClientMetadataDocumentPath + assistantID.String()
}

// assistantClientMetadataDocument is the JSON body served at the assistant
// CIMD endpoint. Fields follow RFC 7591 client metadata as referenced by the
// CIMD draft. client_uri smart-links the consent screen back to the assistant
// in the Gram dashboard.
type assistantClientMetadataDocument struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

func assistantClientName(assistantName string) string {
	name := strings.TrimSpace(assistantName)
	if name == "" {
		return "Gram Assistant"
	}
	return "Gram Assistant: " + name
}

func assistantDashboardURI(siteURL *url.URL, orgSlug, projectSlug, assistantID string) string {
	if siteURL == nil || orgSlug == "" || projectSlug == "" || assistantID == "" {
		return ""
	}
	return siteURL.JoinPath(orgSlug, "projects", projectSlug, "assistants", assistantID).String()
}

func buildAssistantClientMetadataDocument(clientID, clientName, clientURI, redirectURI string) assistantClientMetadataDocument {
	return assistantClientMetadataDocument{
		ClientID:                clientID,
		ClientName:              clientName,
		ClientURI:               clientURI,
		RedirectURIs:            []string{redirectURI},
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: mcpOAuthTokenEndpointAuthNone,
	}
}

func issuerSupportsAssistantCIMD(metadata *externalmcp.OAuthDiscoveryResult) bool {
	if metadata == nil || !metadata.ClientIDMetadataDocumentSupported {
		return false
	}
	if methods := metadata.TokenEndpointAuthMethodsSupported; len(methods) > 0 && !slices.Contains(methods, mcpOAuthTokenEndpointAuthNone) {
		return false
	}
	return true
}

func (s *Service) assistantCIMDAllowed(ctx context.Context, orgID, orgSlug string) bool {
	if s.core.featureFlags == nil || orgID == "" {
		return false
	}
	on, err := s.core.featureFlags.IsFlagEnabled(ctx, feature.FlagAssistantOAuthCIMD, orgID, feature.OrgProjectGroups(orgSlug, ""))
	if err != nil {
		s.logger.WarnContext(ctx, "evaluate assistant oauth cimd flag", attr.SlogError(err))
		return false
	}
	return on
}

func assistantOrgSlug(ctx context.Context) string {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return ""
	}
	return authCtx.OrganizationSlug
}

// handleAssistantClientMetadataDocument serves the public, unauthenticated
// CIMD document at GET /.well-known/oauth-client/assistants/{id}. The
// assistant is resolved by its globally unique primary key.
func (s *Service) handleAssistantClientMetadataDocument(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Pin the document to the platform host: a custom-domain document would
	// advertise a client_id no outbound /authorize ever sent.
	if customdomains.FromContext(ctx) != nil {
		return oops.E(oops.CodeNotFound, nil, "client metadata document not found")
	}

	assistantID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "client metadata document not found")
	}

	if s.core.serverURL == nil {
		return oops.E(oops.CodeNotFound, nil, "client metadata document not found")
	}

	row, err := assistantrepo.New(s.core.db).GetAssistantForClientMetadataDocument(ctx, assistantID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "client metadata document not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load assistant client metadata document").LogError(ctx, s.logger)
	}

	clientID := AssistantClientMetadataDocumentURL(s.core.serverURL, assistantID)
	redirectURI := s.core.serverURL.JoinPath("rpc", "assistantMcpAuth", assistantID.String(), "oauth", "callback").String()
	doc := buildAssistantClientMetadataDocument(
		clientID,
		assistantClientName(row.Name),
		assistantDashboardURI(s.core.siteURL, row.OrganizationSlug, row.ProjectSlug, assistantID.String()),
		redirectURI,
	)

	body, err := json.Marshal(doc)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "marshal assistant client metadata document").LogError(ctx, s.logger)
	}

	return httpcache.WriteCacheableJSON(ctx, w, r, s.logger, "application/json; charset=utf-8", assistantClientMetadataDocumentMaxAgeSeconds, body)
}
