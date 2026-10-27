//nolint:exhaustruct // Optional MCP manifest and management payload fields use their zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// emaClientNameMaxLength matches the management API's client_name bound.
const emaClientNameMaxLength = 255

// emaIssuerCandidateLimit bounds the issuers listed when the caller names none.
const emaIssuerCandidateLimit = 50

var emaClientAuthMethods = []string{oauthwire.AuthMethodNone, oauthwire.AuthMethodClientSecretBasic, oauthwire.AuthMethodClientSecretPost}

// EMAClientRegistrationManagement preserves the management API's org:admin
// check, organization-tier scoping, secret hashing, and atomic audit.
type EMAClientRegistrationManagement interface {
	ListIssuers(context.Context, *orggen.ListIssuersPayload) (*orggen.ListOrganizationUserSessionIssuersResult, error)
	CreateJWTBearerClient(context.Context, *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error)
}

type RegisterEMAClientInput struct {
	IssuerID                string `json:"issuer_id,omitempty" jsonschema:"exact UUID of an organization-level sign-in issuer; omit to list the candidates instead of registering"`
	ClientName              string `json:"client_name" jsonschema:"display name for the client, such as the MCP client application being configured"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method" jsonschema:"none for a public client without a secret; client_secret_basic or client_secret_post for a confidential client whose secret is generated and returned once"`
	Confirmed               bool   `json:"confirmed" jsonschema:"true only after the user confirms the exact issuer, client name, and authentication method"`
}

type RegisterEMAClientOutput struct {
	ClientID                          string `json:"client_id" jsonschema:"the client ID the identity provider must put in the assertion's client_id claim and the MCP client must present"`
	ClientSecret                      string `json:"client_secret,omitempty" jsonschema:"generated client secret, returned only in this result; it cannot be read again"`
	ClientName                        string `json:"client_name"`
	TokenEndpointAuthMethod           string `json:"token_endpoint_auth_method"`
	IssuerID                          string `json:"issuer_id"`
	IssuerSlug                        string `json:"issuer_slug"`
	IssuerURL                         string `json:"issuer_url,omitempty" jsonschema:"assertion audience of a shared authorization server; absent when each MCP server announces its own"`
	TokenEndpoint                     string `json:"token_endpoint,omitempty" jsonschema:"token endpoint of a shared authorization server; absent when each MCP server announces its own"`
	TrustedIdentityProviderConfigured bool   `json:"trusted_identity_provider_configured" jsonschema:"whether the issuer trusts an identity provider's assertions; while false every assertion this client presents is rejected"`
	NextStep                          string `json:"next_step"`
}

type emaIssuerCandidate struct {
	IssuerID                          string `json:"issuer_id"`
	Slug                              string `json:"slug"`
	TrustedIdentityProviderConfigured bool   `json:"trusted_identity_provider_configured"`
}

type emaClientRefusal struct {
	Code    string               `json:"code"`
	Message string               `json:"message"`
	Issuers []emaIssuerCandidate `json:"issuers,omitempty"`
}

// emaIssuerRequiredError carries the candidates for a call that named no issuer.
type emaIssuerRequiredError struct {
	candidates []emaIssuerCandidate
}

func (e *emaIssuerRequiredError) Error() string { return "ema client issuer required" }

type EMAClientRegistrationService struct {
	management EMAClientRegistrationManagement
}

func (r *PostgresReader) WithEMAClientRegistration(management EMAClientRegistrationManagement) *PostgresReader {
	if management != nil {
		r.emaClientRegistration = &EMAClientRegistrationService{management: management}
	}
	return r
}

func (s *EMAClientRegistrationService) register(ctx context.Context, principal Principal, input RegisterEMAClientInput) (RegisterEMAClientOutput, error) {
	if s == nil || s.management == nil {
		return RegisterEMAClientOutput{}, ErrUnavailable
	}
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil || principal.UserID == "" || principal.OrganizationID == "" || ac.UserID != principal.UserID || ac.ActiveOrganizationID != principal.OrganizationID {
		return RegisterEMAClientOutput{}, oops.C(oops.CodeUnauthorized)
	}

	if strings.TrimSpace(input.IssuerID) == "" {
		tier := "organization"
		limit := emaIssuerCandidateLimit
		listed, err := s.management.ListIssuers(ctx, &orggen.ListIssuersPayload{Tier: &tier, Limit: &limit})
		if err != nil {
			return RegisterEMAClientOutput{}, fmt.Errorf("list ema client issuers: %w", err)
		}
		candidates := make([]emaIssuerCandidate, 0, len(listed.Items))
		for _, issuer := range listed.Items {
			if issuer == nil || issuer.ProjectID != "" {
				continue
			}
			candidates = append(candidates, emaIssuerCandidate{IssuerID: issuer.ID, Slug: issuer.Slug, TrustedIdentityProviderConfigured: issuer.TrustedRemoteSessionIssuerID != nil})
		}
		return RegisterEMAClientOutput{}, &emaIssuerRequiredError{candidates: candidates}
	}

	issuerID, err := uuid.Parse(strings.TrimSpace(input.IssuerID))
	name := strings.TrimSpace(input.ClientName)
	if err != nil || issuerID == uuid.Nil || name == "" || len(name) > emaClientNameMaxLength || !slices.Contains(emaClientAuthMethods, input.TokenEndpointAuthMethod) {
		return RegisterEMAClientOutput{}, oops.C(oops.CodeBadRequest)
	}
	if !input.Confirmed {
		return RegisterEMAClientOutput{}, errConfirmationRequired
	}

	created, err := s.management.CreateJWTBearerClient(ctx, &orggen.CreateJWTBearerClientPayload{
		UserSessionIssuerID:     issuerID.String(),
		ClientName:              name,
		TokenEndpointAuthMethod: input.TokenEndpointAuthMethod,
	})
	if err != nil {
		return RegisterEMAClientOutput{}, fmt.Errorf("register ema client: %w", err)
	}
	if created == nil || created.Client == nil || created.Issuer == nil {
		return RegisterEMAClientOutput{}, ErrUnavailable
	}

	out := RegisterEMAClientOutput{
		ClientID:                          created.Client.ClientID,
		ClientName:                        created.Client.ClientName,
		TokenEndpointAuthMethod:           input.TokenEndpointAuthMethod,
		IssuerID:                          created.Issuer.ID,
		IssuerSlug:                        created.Issuer.Slug,
		TrustedIdentityProviderConfigured: created.TrustedIdentityProviderConfigured,
	}
	if created.ClientSecret != nil {
		out.ClientSecret = *created.ClientSecret
	}
	if created.Issuer.IssuerURL != nil {
		out.IssuerURL = *created.Issuer.IssuerURL
	}
	if created.Issuer.TokenEndpoint != nil {
		out.TokenEndpoint = *created.Issuer.TokenEndpoint
	}
	out.NextStep = emaClientNextStep(out)
	return out, nil
}

func emaClientNextStep(out RegisterEMAClientOutput) string {
	step := "Configure the MCP client with this client ID"
	if out.ClientSecret != "" {
		step += " and client secret; the secret is shown only now, so the user must store it before continuing"
	}
	step += ". The identity provider must issue assertions for this client ID."
	if !out.TrustedIdentityProviderConfigured {
		step += " This issuer does not trust an identity provider yet, so every assertion is rejected until an organization administrator configures one."
	}
	return step
}

var errConfirmationRequired = errors.New("confirmation required")

func registerEMAClientRegistrationTool(reg *Registrar, service *EMAClientRegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "register_ema_client",
		Title:       "Register an Enterprise-Managed Authorization Client",
		Description: "Register an MCP client, such as one that needs a client ID configured before it can use enterprise-managed authorization, on one organization-level sign-in issuer. The client redeems identity assertions from the organization's identity provider and has no redirect URIs. Call without issuer_id to list the organization's issuers. Then show the user the exact issuer, client name, and authentication method, ask them to confirm, and call again with confirmed: true. A confidential method returns a generated client secret exactly once; give it to the user for their MCP client configuration and never repeat it. Each call registers a new client, so do not retry a successful call. Registering does not by itself make sign-in work: the issuer must also trust the identity provider issuing the assertions. Requires org:admin.",
		Annotations: writeAnnotations(false, false, false),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input RegisterEMAClientInput) (*mcp.CallToolResult, RegisterEMAClientOutput, error) {
		return principalToolCall(ctx, emaClientToolResult, func(p Principal) (RegisterEMAClientOutput, error) { return service.register(ctx, p, input) })
	})
}

func emaClientToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	refusal := emaClientRefusal{Code: unavailableCode, Message: "Registering an enterprise-managed authorization client is temporarily unavailable, and nothing was registered."}
	if required, ok := errors.AsType[*emaIssuerRequiredError](err); ok {
		refusal = emaClientRefusal{Code: "issuer_required", Message: "Nothing was registered. Ask the user which organization-level issuer the client belongs to, then call again with its exact issuer_id.", Issuers: required.candidates}
	}
	if errors.Is(err, errConfirmationRequired) {
		refusal.Code, refusal.Message = "confirmation_required", "Nothing was registered. Show the user the exact issuer, client name, and authentication method, ask them to confirm, then call again with confirmed: true."
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			refusal.Code, refusal.Message = "invalid_request", "Nothing was registered. Provide an exact organization-level issuer UUID, a client name of at most 255 characters, and one of none, client_secret_basic, or client_secret_post. Project-level issuers are not supported."
		case oops.CodeForbidden, oops.CodeUnauthorized:
			refusal.Code, refusal.Message = "permission_denied", "Registering a client requires a signed-in organization administrator (org:admin)."
		case oops.CodeNotFound:
			refusal.Code, refusal.Message = "not_found", "That issuer is not available to you. Call again without issuer_id to list the organization's issuers."
		default:
			// Unknown failures keep the bounded unavailable response.
		}
	}
	payload, marshalErr := json.Marshal(refusal)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}}, true
}
