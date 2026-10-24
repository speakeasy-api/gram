//nolint:exhaustruct // Test fixtures intentionally omit unrelated fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy"
)

type assertionReuseManagementStub struct {
	list   func(context.Context, *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error)
	update func(context.Context, *gen.UpdateIssuerPayload) (*gen.WorkloadIdentityPolicy, error)
}

func (s assertionReuseManagementStub) List(ctx context.Context, p *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error) {
	return s.list(ctx, p)
}
func (s assertionReuseManagementStub) UpdateIssuer(ctx context.Context, p *gen.UpdateIssuerPayload) (*gen.WorkloadIdentityPolicy, error) {
	return s.update(ctx, p)
}

func TestWorkloadAssertionReuseToolContracts(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerWorkloadAssertionReuseTools(live, &WorkloadAssertionReuseService{})
	registerWorkloadAssertionReuseTools(unavailable, nil)
	require.Empty(t, live.For(AudienceAssistant), "org-wide trust policy requires a live external user")
	for _, name := range []string{"get_workload_assertion_reuse", "set_workload_assertion_reuse"} {
		a, b := descriptorByName(t, live, name), descriptorByName(t, unavailable, name)
		require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
		require.Equal(t, a.Meta, b.Meta)
		require.Equal(t, ExternalAuthorizationMember, a.Meta.Authorization)
		require.Equal(t, ProjectScopeExplicit, a.Meta.ProjectScope)
		require.Equal(t, externalOnly, a.Meta.Audiences)
		var schema struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
			Required []string `json:"required"`
		}
		require.NoError(t, json.Unmarshal(a.InputSchema, &schema))
		for _, field := range []string{"project_id", "issuer_id"} {
			require.Contains(t, schema.Required, field)
		}
		if name == "set_workload_assertion_reuse" {
			for _, field := range []string{"confirmed", "allow_assertion_reuse"} {
				require.Equal(t, "boolean", schema.Properties[field].Type)
				require.Contains(t, schema.Required, field)
			}
			require.Equal(t, []authz.Scope{authz.ScopeWorkloadWrite}, a.Meta.DiscoveryScopes)
			require.True(t, a.Annotations.IdempotentHint)
			require.True(t, *a.Annotations.DestructiveHint)
		} else {
			require.Equal(t, []authz.Scope{authz.ScopeWorkloadRead}, a.Meta.DiscoveryScopes)
			require.True(t, a.Annotations.ReadOnlyHint)
		}
		ctx := contextWithPrincipal(t.Context(), Principal{OrganizationID: "test-org", UserID: "test-user"})
		arguments := map[string]any{"project_id": uuid.NewString(), "issuer_id": uuid.NewString()}
		if name == "set_workload_assertion_reuse" {
			arguments["confirmed"] = true
			arguments["allow_assertion_reuse"] = false
		}
		raw, marshalErr := json.Marshal(arguments)
		require.NoError(t, marshalErr)
		_, err := b.Invoke(ctx, raw)
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.Contains(t, refusal.Payload, unavailableCode)
	}
}

func assertionReuseTestContext(t *testing.T) (context.Context, Principal, uuid.UUID, uuid.UUID) {
	t.Helper()
	p := Principal{OrganizationID: "test-org", UserID: "test-user"}
	project, issuer := uuid.New(), uuid.New()
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: p.OrganizationID, UserID: p.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, p.UserID))
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeWorkloadRead, p.OrganizationID), authz.NewGrant(authz.ScopeWorkloadWrite, p.OrganizationID)})
	return ctx, p, project, issuer
}

func TestWorkloadAssertionReuseSetPreservesActorAndReturnsLiveSafeState(t *testing.T) {
	t.Parallel()
	ctx, p, project, issuer := assertionReuseTestContext(t)
	row := &types.WorkloadIssuer{ID: issuer.String(), OrganizationID: p.OrganizationID, Name: "Projected tokens", JwksURI: "private-key-location"}
	reads, writes := 0, 0
	service := &WorkloadAssertionReuseService{
		resolveProject: func(_ context.Context, org string, input FindMCPInput) (ResolvedProject, error) {
			require.Equal(t, p.OrganizationID, org)
			require.Equal(t, project.String(), input.ProjectID)
			return ResolvedProject{ID: project, Slug: "explicit"}, nil
		},
		management: assertionReuseManagementStub{
			list: func(context.Context, *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error) {
				reads++
				return &gen.WorkloadIdentityPolicy{Issuers: []*types.WorkloadIssuer{row}}, nil
			},
			update: func(scoped context.Context, input *gen.UpdateIssuerPayload) (*gen.WorkloadIdentityPolicy, error) {
				writes++
				require.Equal(t, issuer.String(), input.ID)
				require.Nil(t, input.Name)
				require.Nil(t, input.JwksURI)
				require.Nil(t, input.Tags)
				require.Nil(t, input.Description)
				ac, ok := contextvalues.GetAuthContext(scoped)
				require.True(t, ok)
				require.Equal(t, p.UserID, ac.UserID)
				require.Equal(t, p.OrganizationID, ac.ActiveOrganizationID)
				require.Equal(t, project, *ac.ProjectID)
				actor, _ := contextvalues.AuthenticatedActor(ctx)
				actual, _ := contextvalues.AuthenticatedActor(scoped)
				require.Equal(t, actor, actual)
				grants, _ := authz.GrantsFromContext(ctx)
				actualGrants, _ := authz.GrantsFromContext(scoped)
				require.Equal(t, grants, actualGrants)
				row.AllowAssertionReuse = *input.AllowAssertionReuse
				return nil, nil // Result must come from the post-commit read, not this response.
			},
		},
	}
	for _, enabled := range []bool{true, true, false, false} {
		result, err := service.set(ctx, p, SetWorkloadAssertionReuseInput{ProjectID: project.String(), IssuerID: issuer.String(), AllowAssertionReuse: enabled, Confirmed: true})
		require.NoError(t, err)
		require.Equal(t, enabled, result.AllowAssertionReuse)
		require.Equal(t, "organization", result.PolicyScope)
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private")
		require.Contains(t, string(encoded), `"allow_assertion_reuse":`)
	}
	require.Equal(t, 4, writes)
	require.Equal(t, 8, reads)
	ac, _ := contextvalues.GetAuthContext(ctx)
	require.Nil(t, ac.ProjectID)
}

func TestWorkloadAssertionReuseRejectsUnconfirmedAndHiddenTargets(t *testing.T) {
	t.Parallel()
	ctx, p, project, issuer := assertionReuseTestContext(t)
	row := &types.WorkloadIssuer{ID: issuer.String(), OrganizationID: p.OrganizationID}
	reads, writes := 0, 0
	service := &WorkloadAssertionReuseService{
		resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
			return ResolvedProject{ID: project}, nil
		},
		management: assertionReuseManagementStub{
			list: func(context.Context, *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error) {
				reads++
				return &gen.WorkloadIdentityPolicy{Issuers: []*types.WorkloadIssuer{row}}, nil
			},
			update: func(context.Context, *gen.UpdateIssuerPayload) (*gen.WorkloadIdentityPolicy, error) {
				writes++
				return nil, nil
			},
		},
	}
	input := SetWorkloadAssertionReuseInput{ProjectID: project.String(), IssuerID: issuer.String(), AllowAssertionReuse: true}
	_, err := service.set(ctx, p, input)
	require.Error(t, err)
	require.Zero(t, reads)
	input.Confirmed = true
	for _, tc := range []struct {
		name    string
		org     string
		project string
		id      string
	}{
		{"other org", "other-org", "", issuer.String()},
		{"project tier", p.OrganizationID, project.String(), issuer.String()},
		{"unknown issuer", p.OrganizationID, "", uuid.NewString()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row.OrganizationID, row.ProjectID, row.ID = tc.org, tc.project, tc.id
			_, err := service.set(ctx, p, input)
			var refusal *oops.ShareableError
			require.ErrorAs(t, err, &refusal)
			require.Equal(t, oops.CodeNotFound, refusal.Code)
		})
	}
	require.Zero(t, writes)
	for _, principal := range []Principal{{OrganizationID: "other-org", UserID: p.UserID}, {OrganizationID: p.OrganizationID, UserID: "other-user"}} {
		_, err = service.set(ctx, principal, input)
		var refusal *oops.ShareableError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, oops.CodeUnauthorized, refusal.Code)
	}
	service.resolveProject = func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
		return ResolvedProject{ID: uuid.New()}, nil
	}
	_, err = service.set(ctx, p, input)
	require.ErrorIs(t, err, ErrForbidden)
	require.Zero(t, writes)
}

func TestWorkloadAssertionReuseAPIAndMCPRequireSameReadScope(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	management := workloadpolicy.NewService(logger, testenv.NewTracerProvider(t), nil, &sessions.Manager{}, engine, nil, authserver.Hosts{}, nil)
	ctx, p, project, issuer := assertionReuseTestContext(t)
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	service := &WorkloadAssertionReuseService{management: management, resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
		return ResolvedProject{ID: project}, nil
	}}
	for _, grants := range [][]authz.Grant{nil, {authz.NewGrant(authz.ScopeWorkloadRead, "other-org")}, {authz.NewGrant(authz.ScopeWorkloadWrite, p.OrganizationID)}} {
		scoped := authz.GrantsToContext(ctx, grants)
		_, apiErr := management.List(scoped, &gen.ListPayload{})
		_, mcpErr := service.get(scoped, p, GetWorkloadAssertionReuseInput{ProjectID: project.String(), IssuerID: issuer.String()})
		var apiOops, mcpOops *oops.ShareableError
		require.ErrorAs(t, apiErr, &apiOops)
		require.ErrorAs(t, mcpErr, &mcpOops)
		require.Equal(t, oops.CodeForbidden, apiOops.Code)
		require.Equal(t, apiOops.Code, mcpOops.Code)
	}
}

func TestWorkloadAssertionReuseRefusalsAreBounded(t *testing.T) {
	t.Parallel()
	for _, err := range []error{errors.New("private token"), oops.E(oops.CodeForbidden, nil, "private grants"), oops.E(oops.CodeNotFound, nil, "private issuer")} {
		result, ok := workloadAssertionReuseToolResult(err)
		require.True(t, ok)
		require.True(t, result.IsError)
		require.NotContains(t, result.Content[0].(*mcp.TextContent).Text, "private")
	}
}

func TestWorkloadAssertionReuseAPIAndMCPRequireSameWriteScope(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	management := workloadpolicy.NewService(logger, testenv.NewTracerProvider(t), nil, &sessions.Manager{}, engine, nil, authserver.Hosts{}, nil)
	ctx, p, project, issuer := assertionReuseTestContext(t)
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	service := &WorkloadAssertionReuseService{
		resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
			return ResolvedProject{ID: project}, nil
		},
		management: assertionReuseManagementStub{
			list: func(context.Context, *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error) {
				return &gen.WorkloadIdentityPolicy{Issuers: []*types.WorkloadIssuer{{ID: issuer.String(), OrganizationID: p.OrganizationID}}}, nil
			},
			update: management.UpdateIssuer,
		},
	}
	for _, grants := range [][]authz.Grant{{authz.NewGrant(authz.ScopeWorkloadRead, p.OrganizationID)}, {authz.NewGrant(authz.ScopeWorkloadWrite, "other-org")}} {
		scoped := authz.GrantsToContext(ctx, grants)
		_, apiErr := management.UpdateIssuer(scoped, &gen.UpdateIssuerPayload{ID: issuer.String(), AllowAssertionReuse: new(true)})
		_, mcpErr := service.set(scoped, p, SetWorkloadAssertionReuseInput{ProjectID: project.String(), IssuerID: issuer.String(), AllowAssertionReuse: true, Confirmed: true})
		var apiOops, mcpOops *oops.ShareableError
		require.ErrorAs(t, apiErr, &apiOops)
		require.ErrorAs(t, mcpErr, &mcpOops)
		require.Equal(t, oops.CodeForbidden, apiOops.Code)
		require.Equal(t, apiOops.Code, mcpOops.Code)
	}
}

func TestWorkloadAssertionReuseDiscoveryUsesExistingScopes(t *testing.T) {
	t.Parallel()
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "policy", Version: "1"}, nil))
	registerWorkloadAssertionReuseTools(reg, nil)
	p := Principal{OrganizationID: "test-org", UserID: "test-user"}
	tools := []*mcp.Tool{{Name: "get_workload_assertion_reuse"}, {Name: "set_workload_assertion_reuse"}}
	for _, tc := range []struct {
		scope authz.Scope
		org   string
		want  string
	}{
		{authz.ScopeWorkloadRead, p.OrganizationID, "get_workload_assertion_reuse"},
		{authz.ScopeWorkloadWrite, p.OrganizationID, "set_workload_assertion_reuse"},
		{authz.ScopeWorkloadWrite, "other-org", ""},
	} {
		ctx := authz.GrantsToContext(t.Context(), []authz.Grant{authz.NewGrant(tc.scope, tc.org)})
		visible := reg.FilterExternalTools(ctx, p, tools)
		if tc.want == "" {
			require.Empty(t, visible)
		} else {
			require.Len(t, visible, 1)
			require.Equal(t, tc.want, visible[0].Name)
		}
	}
}

func TestWorkloadAssertionReuseSchemaRejectsMissingOrUntypedPolicy(t *testing.T) {
	t.Parallel()
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "policy", Version: "1"}, nil))
	registerWorkloadAssertionReuseTools(reg, nil)
	descriptor := descriptorByName(t, reg, "set_workload_assertion_reuse")
	for _, value := range []any{nil, "true", 1} {
		input := map[string]any{"project_id": uuid.NewString(), "issuer_id": uuid.NewString(), "confirmed": true}
		if value != nil {
			input["allow_assertion_reuse"] = value
		}
		raw, err := json.Marshal(input)
		require.NoError(t, err)
		_, err = descriptor.Invoke(t.Context(), raw)
		require.ErrorContains(t, err, "validate set_workload_assertion_reuse arguments")
	}
	output := inferOutputSchema[WorkloadAssertionReuseOutput]("get_workload_assertion_reuse")
	require.Equal(t, "boolean", output.Properties["allow_assertion_reuse"].Type)
	require.Contains(t, output.Required, "allow_assertion_reuse")
}
