package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
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
			require.Equal(t, ExternalAuthorizationOrgAdmin, a.Meta.Authorization)
			for _, field := range []string{"confirmed", "allow_assertion_reuse"} {
				require.Equal(t, "boolean", schema.Properties[field].Type)
				require.Contains(t, schema.Required, field)
			}
			require.Equal(t, []authz.Scope{authz.ScopeWorkloadWrite}, a.Meta.DiscoveryScopes)
			require.True(t, a.Annotations.IdempotentHint)
			require.True(t, *a.Annotations.DestructiveHint)
		} else {
			require.Equal(t, ExternalAuthorizationMember, a.Meta.Authorization)
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
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, p.OrganizationID), authz.NewGrant(authz.ScopeWorkloadRead, p.OrganizationID), authz.NewGrant(authz.ScopeWorkloadWrite, p.OrganizationID)})
	return ctx, p, project, issuer
}

func TestWorkloadAssertionReuseSetPreservesActorAndReturnsLiveSafeState(t *testing.T) {
	t.Parallel()
	ctx, p, project, issuer := assertionReuseTestContext(t)
	row := &types.WorkloadIssuer{ID: issuer.String(), OrganizationID: p.OrganizationID, Name: "Projected tokens", JwksURI: "private-key-location"}
	reads, writes := 0, 0
	service := &WorkloadAssertionReuseService{
		authorization: authz.NewEngine(testenv.NewLogger(t), nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
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
				administrator := admission.ActorFromContext(scoped)
				require.True(t, administrator.OrgAdmin)
				require.Equal(t, p.UserID, administrator.UserID)
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
		result, err := service.set(ctx, p, SetWorkloadAssertionReuseInput{ProjectID: project.String(), IssuerID: strings.ToUpper(issuer.String()), AllowAssertionReuse: enabled, Confirmed: true})
		require.NoError(t, err)
		require.Equal(t, enabled, result.AllowAssertionReuse)
		require.Equal(t, "organization", result.PolicyScope)
		require.Equal(t, issuer.String(), result.IssuerID)
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
		authorization: authz.NewEngine(testenv.NewLogger(t), nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
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
		row.OrganizationID, row.ProjectID, row.ID = tc.org, tc.project, tc.id
		_, err := service.set(ctx, p, input)
		var refusal *oops.ShareableError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, oops.CodeNotFound, refusal.Code, tc.name)
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
	management := workloadpolicy.NewService(logger, testenv.NewTracerProvider(t), nil, &sessions.Manager{}, engine, nil, authserver.Hosts{}, nil, cache.NoopCache, &feature.InMemory{})
	ctx, p, project, issuer := assertionReuseTestContext(t)
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	service := &WorkloadAssertionReuseService{management: management, resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
		return ResolvedProject{ID: project}, nil
	}}
	for _, grants := range [][]authz.Grant{nil, {authz.NewGrant(authz.ScopeWorkloadRead, "other-org")}, {authz.NewGrant(authz.ScopeProjectRead, project.String())}} {
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
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.NotContains(t, text.Text, "private")
	}
}

func TestWorkloadAssertionReuseAPIAndMCPRequireSameWriteScope(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	management := workloadpolicy.NewService(logger, testenv.NewTracerProvider(t), nil, &sessions.Manager{}, engine, nil, authserver.Hosts{}, nil, cache.NoopCache, &feature.InMemory{})
	ctx, p, project, issuer := assertionReuseTestContext(t)
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	service := &WorkloadAssertionReuseService{
		authorization: authz.NewEngine(testenv.NewLogger(t), nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
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
		scoped := authz.GrantsToContext(ctx, append(grants, authz.NewGrant(authz.ScopeOrgAdmin, p.OrganizationID)))
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
		admin bool
		want  []string
	}{
		{authz.ScopeWorkloadRead, p.OrganizationID, false, []string{"get_workload_assertion_reuse"}},
		// workload:write implies workload:read, but cannot admit organization mutations alone.
		{authz.ScopeWorkloadWrite, p.OrganizationID, false, []string{"get_workload_assertion_reuse"}},
		{authz.ScopeWorkloadWrite, p.OrganizationID, true, []string{"get_workload_assertion_reuse", "set_workload_assertion_reuse"}},
		// Discovery is coarse: a grant for any resource advertises the workflow.
		// The management service checks the exact organization at invocation.
		{authz.ScopeWorkloadWrite, "other-org", true, []string{"get_workload_assertion_reuse", "set_workload_assertion_reuse"}},
	} {
		grants := []authz.Grant{authz.NewGrant(tc.scope, tc.org)}
		if tc.admin {
			grants = append(grants, authz.NewGrant(authz.ScopeOrgAdmin, p.OrganizationID))
		}
		ctx := authz.GrantsToContext(t.Context(), grants)
		visible := reg.FilterExternalTools(ctx, p, tools)
		var names []string
		for _, tool := range visible {
			names = append(names, tool.Name)
		}
		require.Equal(t, tc.want, names)
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

func TestWorkloadAssertionReuseWriteRequiresAdministratorAdmission(t *testing.T) {
	t.Parallel()
	ctx, p, project, issuer := assertionReuseTestContext(t)
	calls := 0
	service := &WorkloadAssertionReuseService{
		authorization: authz.NewEngine(testenv.NewLogger(t), nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
			return ResolvedProject{ID: project}, nil
		},
		management: assertionReuseManagementStub{
			list: func(context.Context, *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error) { calls++; return nil, nil },
			update: func(context.Context, *gen.UpdateIssuerPayload) (*gen.WorkloadIdentityPolicy, error) {
				calls++
				return nil, nil
			},
		},
	}
	for _, adminOrg := range []string{"", "other-org"} {
		grants := []authz.Grant{authz.NewGrant(authz.ScopeWorkloadWrite, p.OrganizationID)}
		if adminOrg != "" {
			grants = append(grants, authz.NewGrant(authz.ScopeOrgAdmin, adminOrg))
		}
		scoped := authz.GrantsToContext(ctx, grants)
		_, err := service.set(scoped, p, SetWorkloadAssertionReuseInput{ProjectID: project.String(), IssuerID: issuer.String(), AllowAssertionReuse: true, Confirmed: true})
		var refusal *oops.ShareableError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, oops.CodeForbidden, refusal.Code)
	}
	require.Zero(t, calls, "administrator admission precedes policy reads and writes")
	service.authorization = nil
	_, err := service.set(ctx, p, SetWorkloadAssertionReuseInput{ProjectID: project.String(), IssuerID: issuer.String(), AllowAssertionReuse: true, Confirmed: true})
	require.ErrorIs(t, err, ErrUnavailable)
	require.Zero(t, calls)
}

func TestWorkloadAssertionReuseReadCanonicalizesIssuerAndAcceptsWriteScope(t *testing.T) {
	t.Parallel()
	ctx, p, project, issuer := assertionReuseTestContext(t)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeWorkloadWrite, p.OrganizationID)})
	service := &WorkloadAssertionReuseService{
		resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
			return ResolvedProject{ID: project}, nil
		},
		management: assertionReuseManagementStub{
			list: func(ctx context.Context, _ *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error) {
				grants, ok := authz.GrantsFromContext(ctx)
				require.True(t, ok)
				allowed, err := authz.GrantsAuthorize(grants, authz.Check{Scope: authz.ScopeWorkloadRead, ResourceKind: "", ResourceID: p.OrganizationID, Dimensions: nil})
				require.NoError(t, err)
				require.True(t, allowed, "workload:write includes workload:read")
				return &gen.WorkloadIdentityPolicy{Issuers: []*types.WorkloadIssuer{{ID: issuer.String(), OrganizationID: p.OrganizationID}}}, nil
			},
		},
	}
	output, err := service.get(ctx, p, GetWorkloadAssertionReuseInput{ProjectID: strings.ToUpper(project.String()), IssuerID: strings.ToUpper(issuer.String())})
	require.NoError(t, err)
	require.Equal(t, issuer.String(), output.IssuerID)
	require.Equal(t, project.String(), output.ProjectID)
}
