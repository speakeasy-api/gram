package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type emaClientManagementStub struct {
	list   func(context.Context, *orggen.ListIssuersPayload) (*orggen.ListOrganizationUserSessionIssuersResult, error)
	create func(context.Context, *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error)
}

func (s emaClientManagementStub) ListIssuers(ctx context.Context, p *orggen.ListIssuersPayload) (*orggen.ListOrganizationUserSessionIssuersResult, error) {
	return s.list(ctx, p)
}

func (s emaClientManagementStub) CreateJWTBearerClient(ctx context.Context, p *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error) {
	return s.create(ctx, p)
}

func emaClientTestContext(t *testing.T) context.Context {
	t.Helper()
	p := Principal{OrganizationID: "test-org", UserID: "test-user"}
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: p.OrganizationID, UserID: p.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, p.UserID))
	return contextWithPrincipal(ctx, p)
}

func invokeEMAClientTool(t *testing.T, service *EMAClientRegistrationService, arguments map[string]any) (any, error) {
	t.Helper()
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "ema", Version: "1"}, nil))
	registerEMAClientRegistrationTool(reg, service)
	raw, err := json.Marshal(arguments)
	require.NoError(t, err)
	return descriptorByName(t, reg, "register_ema_client").Invoke(emaClientTestContext(t), raw)
}

func requireEMAClientRefusal(t *testing.T, err error, code string) emaClientRefusal {
	t.Helper()
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	var payload emaClientRefusal
	require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &payload))
	require.Equal(t, code, payload.Code)
	return payload
}

func failingEMAClientCreate(t *testing.T) func(context.Context, *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error) {
	t.Helper()
	return func(context.Context, *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error) {
		t.Fatal("no client may be registered")
		return nil, nil
	}
}

func TestEMAClientRegistrationToolContract(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerEMAClientRegistrationTool(live, &EMAClientRegistrationService{management: emaClientManagementStub{}})
	registerEMAClientRegistrationTool(unavailable, nil)
	require.Empty(t, live.For(AudienceAssistant), "registering a client needs a live external administrator")

	a, b := descriptorByName(t, live, "register_ema_client"), descriptorByName(t, unavailable, "register_ema_client")
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, ExternalAuthorizationOrgAdmin, a.Meta.Authorization)
	require.Equal(t, externalOnly, a.Meta.Audiences)
	require.Equal(t, ProjectScopeNone, a.Meta.ProjectScope)
	require.False(t, a.Annotations.ReadOnlyHint)
	require.False(t, a.Annotations.IdempotentHint)
	require.False(t, *a.Annotations.DestructiveHint)
	require.False(t, *a.Annotations.OpenWorldHint)

	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(a.InputSchema, &schema))
	require.ElementsMatch(t, []string{"client_name", "token_endpoint_auth_method", "confirmed"}, schema.Required)
	require.Equal(t, "boolean", schema.Properties["confirmed"].Type)

	_, err := b.Invoke(emaClientTestContext(t), json.RawMessage(`{"issuer_id":"`+uuid.NewString()+`","client_name":"Codex","token_endpoint_auth_method":"none","confirmed":true}`))
	requireEMAClientRefusal(t, err, unavailableCode)
}

func TestEMAClientRegistrationListsIssuersWhenNoneNamed(t *testing.T) {
	t.Parallel()
	trusted := uuid.NewString()
	service := &EMAClientRegistrationService{management: emaClientManagementStub{
		list: func(_ context.Context, p *orggen.ListIssuersPayload) (*orggen.ListOrganizationUserSessionIssuersResult, error) {
			require.Equal(t, "organization", *p.Tier)
			return &orggen.ListOrganizationUserSessionIssuersResult{Items: []*types.UserSessionIssuer{
				{ID: "issuer-a", Slug: "corp", TrustedRemoteSessionIssuerID: &trusted},
				{ID: "issuer-b", Slug: "untrusted"},
				{ID: "issuer-c", Slug: "project", ProjectID: uuid.NewString()},
			}}, nil
		},
		create: failingEMAClientCreate(t),
	}}

	_, err := invokeEMAClientTool(t, service, map[string]any{"client_name": "Codex", "token_endpoint_auth_method": "none", "confirmed": true})
	refusal := requireEMAClientRefusal(t, err, "issuer_required")
	require.Equal(t, []emaIssuerCandidate{
		{IssuerID: "issuer-a", Slug: "corp", TrustedIdentityProviderConfigured: true},
		{IssuerID: "issuer-b", Slug: "untrusted", TrustedIdentityProviderConfigured: false},
	}, refusal.Issuers)
}

func TestEMAClientRegistrationRequiresConfirmationAndValidInput(t *testing.T) {
	t.Parallel()
	service := &EMAClientRegistrationService{management: emaClientManagementStub{create: failingEMAClientCreate(t)}}
	issuerID := uuid.NewString()

	_, err := invokeEMAClientTool(t, service, map[string]any{"issuer_id": issuerID, "client_name": "Codex", "token_endpoint_auth_method": "client_secret_basic", "confirmed": false})
	requireEMAClientRefusal(t, err, "confirmation_required")

	for _, arguments := range []map[string]any{
		{"issuer_id": "not-a-uuid", "client_name": "Codex", "token_endpoint_auth_method": "none", "confirmed": true},
		{"issuer_id": issuerID, "client_name": " ", "token_endpoint_auth_method": "none", "confirmed": true},
		{"issuer_id": issuerID, "client_name": "Codex", "token_endpoint_auth_method": "private_key_jwt", "confirmed": true},
	} {
		_, err := invokeEMAClientTool(t, service, arguments)
		requireEMAClientRefusal(t, err, "invalid_request")
	}
}

func TestEMAClientRegistrationReturnsSecretOnceAndNextStep(t *testing.T) {
	t.Parallel()
	issuerID := uuid.NewString()
	calls := 0
	service := &EMAClientRegistrationService{management: emaClientManagementStub{
		create: func(ctx context.Context, p *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error) {
			calls++
			require.Equal(t, issuerID, p.UserSessionIssuerID)
			require.Equal(t, "Codex", p.ClientName)
			require.Equal(t, "client_secret_post", p.TokenEndpointAuthMethod)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.Equal(t, "test-user", ac.UserID)
			secret := "generated-secret"
			issuerURL, tokenEndpoint := "https://auth.example.test/as/1", "https://auth.example.test/as/1/token"
			return &orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult{
				Client:       &types.UserSessionClient{ClientID: "client_abc", ClientName: "Codex"},
				ClientSecret: &secret,
				Issuer:       &types.UserSessionIssuer{ID: issuerID, Slug: "corp", IssuerURL: &issuerURL, TokenEndpoint: &tokenEndpoint},
			}, nil
		},
	}}

	result, err := invokeEMAClientTool(t, service, map[string]any{"issuer_id": issuerID, "client_name": " Codex ", "token_endpoint_auth_method": "client_secret_post", "confirmed": true})
	require.NoError(t, err)
	out, ok := result.(RegisterEMAClientOutput)
	require.True(t, ok)
	require.Equal(t, 1, calls)
	require.Equal(t, "client_abc", out.ClientID)
	require.Equal(t, "generated-secret", out.ClientSecret)
	require.Equal(t, "https://auth.example.test/as/1", out.IssuerURL)
	require.Equal(t, "https://auth.example.test/as/1/token", out.TokenEndpoint)
	require.False(t, out.TrustedIdentityProviderConfigured)
	require.Contains(t, out.NextStep, "shown only now")
	require.Contains(t, out.NextStep, "does not trust an identity provider")
}

func TestEMAClientRegistrationMapsManagementRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		code oops.Code
		want string
	}{
		{oops.CodeNotFound, "not_found"},
		{oops.CodeBadRequest, "invalid_request"},
		{oops.CodeForbidden, "permission_denied"},
	} {
		code, want := tc.code, tc.want
		service := &EMAClientRegistrationService{management: emaClientManagementStub{
			create: func(context.Context, *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error) {
				return nil, oops.C(code)
			},
		}}
		_, err := invokeEMAClientTool(t, service, map[string]any{"issuer_id": uuid.NewString(), "client_name": "Codex", "token_endpoint_auth_method": "none", "confirmed": true})
		requireEMAClientRefusal(t, err, want)
	}
}
