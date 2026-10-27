package platformmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
	get    func(context.Context, *orggen.GetIssuerPayload) (*types.UserSessionIssuer, error)
	create func(context.Context, *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error)
}

func (s emaClientManagementStub) ListIssuers(ctx context.Context, p *orggen.ListIssuersPayload) (*orggen.ListOrganizationUserSessionIssuersResult, error) {
	return s.list(ctx, p)
}

func (s emaClientManagementStub) GetIssuer(ctx context.Context, p *orggen.GetIssuerPayload) (*types.UserSessionIssuer, error) {
	return s.get(ctx, p)
}

func (s emaClientManagementStub) CreateJWTBearerClient(ctx context.Context, p *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error) {
	return s.create(ctx, p)
}

func emaPrincipalContext(t *testing.T, ctx context.Context, p Principal) context.Context {
	t.Helper()
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: p.OrganizationID, UserID: p.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, p.UserID))
	return contextWithPrincipal(ctx, p)
}

func emaClientTestContext(t *testing.T) context.Context {
	t.Helper()
	return emaPrincipalContext(t, t.Context(), Principal{OrganizationID: "test-org", UserID: "test-user"})
}

func untrustedEMAIssuer(context.Context, string, uuid.UUID) (bool, error) { return false, nil }

func orgIssuerGetter(issuerID string) func(context.Context, *orggen.GetIssuerPayload) (*types.UserSessionIssuer, error) {
	return func(_ context.Context, p *orggen.GetIssuerPayload) (*types.UserSessionIssuer, error) {
		if p.ID != issuerID {
			return nil, oops.C(oops.CodeNotFound)
		}
		return &types.UserSessionIssuer{ID: issuerID, Slug: "corp", AuthorizationServerMode: "shared"}, nil
	}
}

func invokeEMAClientTool(t *testing.T, ctx context.Context, service *EMAClientRegistrationService, arguments map[string]any) (any, error) {
	t.Helper()
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "ema", Version: "1"}, nil))
	registerEMAClientRegistrationTool(reg, service)
	raw, err := json.Marshal(arguments)
	require.NoError(t, err)
	return descriptorByName(t, reg, "register_ema_client").Invoke(ctx, raw)
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
		t.Error("no client may be registered")
		return nil, oops.C(oops.CodeUnexpected)
	}
}

func createdEMAClient(issuerID string, calls *int) func(context.Context, *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error) {
	return func(_ context.Context, p *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error) {
		*calls++
		secret := "generated-secret"
		issuerURL, tokenEndpoint := "https://auth.example.test/as/1", "https://auth.example.test/as/1/token"
		return &orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult{
			Client:       &types.UserSessionClient{ClientID: "client_" + uuid.NewString(), ClientName: p.ClientName},
			ClientSecret: &secret,
			Issuer:       &types.UserSessionIssuer{ID: issuerID, Slug: "corp", AuthorizationServerMode: "shared", IssuerURL: &issuerURL, TokenEndpoint: &tokenEndpoint},
		}, nil
	}
}

func TestEMAClientRegistrationToolContract(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerEMAClientRegistrationTool(live, &EMAClientRegistrationService{management: emaClientManagementStub{}, trusted: untrustedEMAIssuer})
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
	require.Equal(t, "string", schema.Properties["idempotency_key"].Type)

	_, err := b.Invoke(emaClientTestContext(t), json.RawMessage(`{"issuer_id":"`+uuid.NewString()+`","client_name":"Codex","token_endpoint_auth_method":"none","confirmed":true}`))
	requireEMAClientRefusal(t, err, unavailableCode)
}

func TestEMAClientRegistrationListsIssuersWhenNoneNamed(t *testing.T) {
	t.Parallel()
	trustedID, untrustedID := uuid.NewString(), uuid.NewString()
	for _, truncated := range []bool{false, true} {
		service := &EMAClientRegistrationService{
			management: emaClientManagementStub{
				list: func(_ context.Context, p *orggen.ListIssuersPayload) (*orggen.ListOrganizationUserSessionIssuersResult, error) {
					require.Equal(t, "organization", *p.Tier)
					require.Equal(t, emaIssuerCandidateLimit, *p.Limit)
					result := &orggen.ListOrganizationUserSessionIssuersResult{Items: []*types.UserSessionIssuer{
						{ID: trustedID, Slug: "corp"},
						{ID: untrustedID, Slug: "untrusted"},
						{ID: uuid.NewString(), Slug: "project", ProjectID: uuid.NewString()},
					}}
					if truncated {
						result.NextCursor = new(untrustedID)
					}
					return result, nil
				},
				create: failingEMAClientCreate(t),
			},
			trusted: func(_ context.Context, _ string, id uuid.UUID) (bool, error) { return id.String() == trustedID, nil },
		}

		_, err := invokeEMAClientTool(t, emaClientTestContext(t), service, map[string]any{"client_name": "Codex", "token_endpoint_auth_method": "none", "confirmed": true})
		refusal := requireEMAClientRefusal(t, err, "issuer_required")
		require.Equal(t, []emaIssuerCandidate{
			{IssuerID: trustedID, Slug: "corp", TrustedIdentityProviderConfigured: true},
			{IssuerID: untrustedID, Slug: "untrusted", TrustedIdentityProviderConfigured: false},
		}, refusal.Issuers)
		require.Equal(t, truncated, refusal.Truncated)
		require.Equal(t, truncated, strings.Contains(refusal.Message, "Only the first"))
	}
}

func TestEMAClientRegistrationPreviewEchoesIssuerWithoutRegistering(t *testing.T) {
	t.Parallel()
	issuerID := uuid.NewString()
	service := &EMAClientRegistrationService{management: emaClientManagementStub{get: orgIssuerGetter(issuerID), create: failingEMAClientCreate(t)}, trusted: untrustedEMAIssuer}

	result, err := invokeEMAClientTool(t, emaClientTestContext(t), service, map[string]any{"issuer_id": issuerID, "client_name": " Codex ", "token_endpoint_auth_method": "client_secret_basic", "confirmed": false})
	require.NoError(t, err)
	out, ok := result.(RegisterEMAClientOutput)
	require.True(t, ok)
	require.True(t, out.Preview)
	require.Empty(t, out.ClientID)
	require.Empty(t, out.ClientSecret)
	require.Equal(t, issuerID, out.IssuerID)
	require.Equal(t, "corp", out.IssuerSlug)
	require.Equal(t, "shared", out.AuthorizationServerMode)
	require.Equal(t, "Codex", out.ClientName)
	require.False(t, out.TrustedIdentityProviderConfigured)
	require.Contains(t, out.NextStep, "does not trust an identity provider")
}

func TestEMAClientRegistrationRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	issuerID := uuid.NewString()
	projectIssuerID := uuid.NewString()
	service := &EMAClientRegistrationService{
		management: emaClientManagementStub{
			get: func(ctx context.Context, p *orggen.GetIssuerPayload) (*types.UserSessionIssuer, error) {
				if p.ID == projectIssuerID {
					return &types.UserSessionIssuer{ID: projectIssuerID, ProjectID: uuid.NewString()}, nil
				}
				return orgIssuerGetter(issuerID)(ctx, p)
			},
			create: failingEMAClientCreate(t),
		},
		trusted: untrustedEMAIssuer,
	}

	for _, arguments := range []map[string]any{
		{"issuer_id": "not-a-uuid", "client_name": "Codex", "token_endpoint_auth_method": "none", "confirmed": true},
		{"issuer_id": issuerID, "client_name": " ", "token_endpoint_auth_method": "none", "confirmed": true},
		{"issuer_id": issuerID, "client_name": strings.Repeat("é", emaClientNameMaxLength+1), "token_endpoint_auth_method": "none", "confirmed": true},
		{"issuer_id": issuerID, "client_name": "Codex", "token_endpoint_auth_method": "private_key_jwt", "confirmed": true},
		{"issuer_id": issuerID, "client_name": "Codex", "token_endpoint_auth_method": "none", "confirmed": true, "idempotency_key": strings.Repeat("k", emaIdempotencyKeyMaxLength+1)},
		{"issuer_id": projectIssuerID, "client_name": "Codex", "token_endpoint_auth_method": "none", "confirmed": true},
	} {
		_, err := invokeEMAClientTool(t, emaClientTestContext(t), service, arguments)
		requireEMAClientRefusal(t, err, "invalid_request")
	}

	// Length is counted in characters: 255 two-byte characters are accepted.
	result, err := invokeEMAClientTool(t, emaClientTestContext(t), service, map[string]any{"issuer_id": issuerID, "client_name": strings.Repeat("é", emaClientNameMaxLength), "token_endpoint_auth_method": "none", "confirmed": false})
	require.NoError(t, err)
	previewOut, ok := result.(RegisterEMAClientOutput)
	require.True(t, ok)
	require.True(t, previewOut.Preview)
}

func TestEMAClientRegistrationReturnsSecretOnceAndNextStep(t *testing.T) {
	t.Parallel()
	issuerID := uuid.NewString()
	calls := 0
	create := createdEMAClient(issuerID, &calls)
	service := &EMAClientRegistrationService{management: emaClientManagementStub{
		get: orgIssuerGetter(issuerID),
		create: func(ctx context.Context, p *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error) {
			require.Equal(t, issuerID, p.UserSessionIssuerID)
			require.Equal(t, "Codex", p.ClientName)
			require.Equal(t, "client_secret_post", p.TokenEndpointAuthMethod)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.Equal(t, "test-user", ac.UserID)
			return create(ctx, p)
		},
	}, trusted: untrustedEMAIssuer}

	result, err := invokeEMAClientTool(t, emaClientTestContext(t), service, map[string]any{"issuer_id": issuerID, "client_name": " Codex ", "token_endpoint_auth_method": "client_secret_post", "confirmed": true})
	require.NoError(t, err)
	out, ok := result.(RegisterEMAClientOutput)
	require.True(t, ok)
	require.Equal(t, 1, calls)
	require.False(t, out.Preview)
	require.True(t, strings.HasPrefix(out.ClientID, "client_"))
	require.Equal(t, "generated-secret", out.ClientSecret)
	require.Equal(t, "https://auth.example.test/as/1", out.IssuerURL)
	require.Equal(t, "https://auth.example.test/as/1/token", out.TokenEndpoint)
	require.False(t, out.TrustedIdentityProviderConfigured)
	require.Contains(t, out.NextStep, "shown only now")
	require.Contains(t, out.NextStep, "does not trust an identity provider")
}

func TestEMAClientRegistrationIdempotencyKeyReplaysWithoutSecret(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_ema_client_idempotency")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	ctx = emaPrincipalContext(t, ctx, principal)

	issuerID := uuid.NewString()
	calls := 0
	service := &EMAClientRegistrationService{
		management:     emaClientManagementStub{get: orgIssuerGetter(issuerID), create: createdEMAClient(issuerID, &calls)},
		trusted:        untrustedEMAIssuer,
		db:             conn,
		now:            time.Now,
		resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) { return project, nil },
	}
	arguments := map[string]any{"issuer_id": issuerID, "client_name": "Codex", "token_endpoint_auth_method": "client_secret_basic", "confirmed": true, "idempotency_key": "codex-1"}

	first, err := invokeEMAClientTool(t, ctx, service, arguments)
	require.NoError(t, err)
	firstOut, ok := first.(RegisterEMAClientOutput)
	require.True(t, ok)
	require.Equal(t, "generated-secret", firstOut.ClientSecret)
	require.False(t, firstOut.Replayed)

	replay, err := invokeEMAClientTool(t, ctx, service, arguments)
	require.NoError(t, err)
	replayOut, ok := replay.(RegisterEMAClientOutput)
	require.True(t, ok)
	require.Equal(t, 1, calls)
	require.True(t, replayOut.Replayed)
	require.Equal(t, firstOut.ClientID, replayOut.ClientID)
	require.Empty(t, replayOut.ClientSecret)

	arguments["client_name"] = "Different"
	_, err = invokeEMAClientTool(t, ctx, service, arguments)
	requireEMAClientRefusal(t, err, "idempotency_key_reused")
	require.Equal(t, 1, calls)
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
		issuerID := uuid.NewString()
		service := &EMAClientRegistrationService{management: emaClientManagementStub{
			get: orgIssuerGetter(issuerID),
			create: func(context.Context, *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error) {
				return nil, oops.C(tc.code)
			},
		}, trusted: untrustedEMAIssuer}
		_, err := invokeEMAClientTool(t, emaClientTestContext(t), service, map[string]any{"issuer_id": issuerID, "client_name": "Codex", "token_endpoint_auth_method": "none", "confirmed": true})
		requireEMAClientRefusal(t, err, tc.want)
	}
}
