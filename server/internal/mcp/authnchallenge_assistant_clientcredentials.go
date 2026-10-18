package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientcred"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// AssistantWorkloadTokenRequest identifies an authenticated registration, never
// an assistant selected by the caller. The issuer must resolve that registration
// to exactly one live assistant in this tenancy.
type AssistantWorkloadTokenRequest struct {
	ClientRegistrationID uuid.UUID
	ClientID             string
	IssuerID             uuid.UUID
	OrganizationID       string
	ProjectID            uuid.UUID
	Resource             string
	Scopes               []string
}

// AssistantWorkloadToken is an access-only, resource- and scope-bound credential.
// AssistantID is the independent workload identity; it is not an authorizing user.
type AssistantWorkloadToken struct {
	AssistantID uuid.UUID
	AccessToken string
	ExpiresAt   time.Time
}

// AssistantWorkloadTokenIssuer is the provisioning-neutral registration boundary.
// Implementations MUST resolve a durable client-to-assistant binding, reject
// deleted/revoked registrations and assistants, and require live AGENT identity
// policy (never HUMAN or delegated identity). They MUST enforce every requested
// scope against current assistant policy and the exact resource, persist issuance
// and its audit atomically, bind the persisted session's UserSessionClientID to
// ClientRegistrationID for the existing revocation ownership checks, and issue
// tokens whose resource server checks that
// same live policy and revocation on every use. Tokens must expire within 15
// minutes, carry exactly these scopes and resource, and have no refresh token.
//
// Do not implement this through mintSession: agent sessions there deliberately
// require a human authorizer and delegated grants. No default implementation is
// installed until the workload registration adapter is explicitly composed.
// Reuse assistantidentity.Resolve/Validate for assistant/trigger bindings,
// workloadidentity admission/assignment and existing workload session issuance
// and bearer validation; do not create another identity registry. OAuth scopes
// need explicit policy mapping: existing session JWTs do not persist scopes.
// Credential provisioning may use secrets or private_key_jwt; this boundary does
// not choose one. Both use the existing exact-method client authentication path.
type AssistantWorkloadTokenIssuer interface {
	IssueAssistantWorkloadToken(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error)
}

// SetAssistantWorkloadTokenIssuer installs the independently authorized workload
// issuer at service composition time. Nil disables the grant. It must not be
// changed concurrently with serving requests.
func (s *Service) SetAssistantWorkloadTokenIssuer(issuer AssistantWorkloadTokenIssuer) {
	s.assistantWorkloadTokens = issuer
}

func (s *Service) handleAssistantClientCredentialsGrant(ctx context.Context, w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, client *usersessions_repo.UserSessionClient, baseURL, presentedAuthMethod string, logger *slog.Logger) error {
	reject := func(code string) error {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth assistant workload token request rejected", client.ClientID, presentedAuthMethod, "client_credentials", code)
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, code, "assistant workload authorization refused")
	}
	if s.assistantWorkloadTokens == nil || endpoint.IsOrganizationGateway() {
		return reject("unauthorized_client")
	}
	kind, err := clientcred.KindOf(client.TokenEndpointAuthMethod, client.ClientSecretHash.Valid)
	if err != nil || (kind != clientcred.KindSecret && kind != clientcred.KindKey) {
		return reject("unauthorized_client")
	}
	if client.UserSessionIssuerID != endpoint.UserSessionIssuerID {
		return reject("unauthorized_client")
	}
	resource, err := endpoint.RootURL(baseURL)
	resources := r.PostForm["resource"]
	if err != nil || len(resources) != 1 || resources[0] != resource {
		return reject("invalid_target")
	}
	// Require an explicit scope set; there is no implicit all-tools default.
	rawScopes := r.PostForm["scope"]
	if len(rawScopes) != 1 || !validAssistantWorkloadScopes(rawScopes[0]) {
		return reject("invalid_scope")
	}
	scopes := strings.Split(rawScopes[0], " ")
	result, err := s.assistantWorkloadTokens.IssueAssistantWorkloadToken(ctx, AssistantWorkloadTokenRequest{
		ClientRegistrationID: client.ID, ClientID: client.ClientID,
		IssuerID: endpoint.UserSessionIssuerID, OrganizationID: endpoint.OrganizationID,
		ProjectID: endpoint.ProjectID, Resource: resource, Scopes: scopes,
	})
	if err != nil {
		return reject("unauthorized_client")
	}
	lifetime := time.Until(result.ExpiresAt)
	if result.AssistantID == uuid.Nil || result.AccessToken == "" || lifetime < time.Second || lifetime > 15*time.Minute {
		return writeTokenError(ctx, w, logger, http.StatusInternalServerError, "server_error", "workload token issuance unavailable")
	}
	body, err := json.Marshal(struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
	}{result.AccessToken, "Bearer", int64(lifetime / time.Second), rawScopes[0]})
	if err != nil {
		return err
	}
	logOAuthClientCredentialEvent(ctx, logger, r, "oauth assistant workload token issued", client.ClientID, presentedAuthMethod, "client_credentials", "issued")
	return writeTokenSuccess(ctx, w, logger, body)
}

func validAssistantWorkloadScopes(scope string) bool {
	if scope == "" {
		return false
	}
	seen := make(map[string]bool)
	for token := range strings.SplitSeq(scope, " ") {
		if token == "" || seen[token] {
			return false
		}
		seen[token] = true
		for _, c := range token {
			// RFC 6749 scope-token excludes double quote and backslash.
			if c < 0x21 || c > 0x7e || c == '"' || c == '\\' {
				return false
			}
		}
	}
	return true
}
