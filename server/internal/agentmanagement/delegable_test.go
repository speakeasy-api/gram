package agentmanagement

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Use the real loaded-grant engine: an exact agent:authorize grant must not
// accidentally stand in for agent:read, agent:write, or runtime resource rights.
type delegableFixture struct {
	db      *pgxpool.Pool
	service *Service
	agentID uuid.UUID
}

func newDelegableFixture(t *testing.T) delegableFixture {
	t.Helper()
	db := newTestDB(t)
	seedOrganization(t, db, "org-delegable")
	for _, userID := range []string{"owner", "caller"} {
		seedOrganizationUser(t, db, "org-delegable", userID)
	}
	agent := createAgent(t, db, "org-delegable", "owner", "Delegable agent")
	engine := authz.NewEngine(testenv.NewLogger(t), db, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := newTestService(db, engine)
	service.features = &recordingAgentManagementFeatures{evaluation: feature.EvaluationEnabled}
	seedGrant(t, t.Context(), db, "org-delegable", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), authz.ScopeAgentAuthorize, agent.ID.String())
	return delegableFixture{db: db, service: service, agentID: agent.ID}
}

func (f delegableFixture) principal(name string) urn.Principal {
	if name == "agent" {
		return urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String())
	}
	return urn.NewPrincipal(urn.PrincipalTypeUser, name)
}

func (f delegableFixture) grant(t *testing.T, principal string, scope authz.Scope, selector authz.Selector) uuid.UUID {
	t.Helper()
	raw, err := selector.MarshalJSON()
	require.NoError(t, err)
	row, err := accessrepo.New(f.db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: "org-delegable", PrincipalUrn: f.principal(principal), Scope: string(scope), Selectors: raw,
	})
	require.NoError(t, err)
	return row.ID
}

func (f delegableFixture) list(t *testing.T, user string) []*gen.AgentPolicyGrantForm {
	t.Helper()
	result, err := f.service.ListDelegableGrants(validatedHumanContext(t, "org-delegable", user), &gen.ListDelegableGrantsPayload{AgentID: f.agentID.String()})
	require.NoError(t, err)
	for _, grant := range result {
		require.NotNil(t, grant)
		require.Equal(t, "allow", grant.Effect)
		_, _, err := validatePolicyGrant(grant.Scope, grant.Effect, grant.Selector)
		require.NoError(t, err, "discovery must return valid runtime-safe grant forms")
	}
	return result
}

func delegableProjectForm(resource string) *gen.AgentPolicyGrantForm {
	return &gen.AgentPolicyGrantForm{Scope: string(authz.ScopeProjectRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: resource}}
}

func TestListDelegableGrantsOwnerAndExactAuthorizeIntersection(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	for principal, resources := range map[string][]string{
		"agent":  {"shared", "owner-only", "caller-only", "agent-only"},
		"owner":  {"shared", "owner-only", "not-agent"},
		"caller": {"shared", "caller-only", "not-agent"},
	} {
		for _, resource := range resources {
			f.grant(t, principal, authz.ScopeProjectRead, authz.NewSelector(authz.ScopeProjectRead, resource))
		}
	}
	require.ElementsMatch(t, []*gen.AgentPolicyGrantForm{delegableProjectForm("shared"), delegableProjectForm("owner-only")}, f.list(t, "owner"))
	require.ElementsMatch(t, []*gen.AgentPolicyGrantForm{delegableProjectForm("shared")}, f.list(t, "caller"))

	ctx := validatedHumanContext(t, "org-delegable", "caller")
	_, err := f.service.Get(ctx, &gen.GetPayload{ID: f.agentID.String()})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = f.service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: f.agentID.String()})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, _, err = f.service.authorizer.RequireAgent(ctx, f.db, f.agentID, OwnedAgentSetup)
	requireOopsCode(t, err, oops.CodeForbidden)

	other := createAgent(t, f.db, "org-delegable", "owner", "Not authorized")
	_, err = f.service.ListDelegableGrants(ctx, &gen.ListDelegableGrantsPayload{AgentID: other.ID.String()})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestListDelegableGrantsReloadsEveryPolicy(t *testing.T) {
	t.Parallel()
	for _, revoked := range []string{"agent", "owner", "caller"} {
		t.Run(revoked, func(t *testing.T) {
			t.Parallel()
			f := newDelegableFixture(t)
			ids := make(map[string]uuid.UUID)
			for _, principal := range []string{"agent", "owner", "caller"} {
				ids[principal] = f.grant(t, principal, authz.ScopeProjectRead, authz.NewSelector(authz.ScopeProjectRead, "shared"))
			}
			ctx := authz.GrantsToContext(validatedHumanContext(t, "org-delegable", "caller"), []authz.Grant{
				authz.NewGrant(authz.ScopeAgentAuthorize, f.agentID.String()), authz.NewGrant(authz.ScopeProjectRead, "shared"),
			})
			payload := &gen.ListDelegableGrantsPayload{AgentID: f.agentID.String()}
			before, err := f.service.ListDelegableGrants(ctx, payload)
			require.NoError(t, err)
			require.ElementsMatch(t, []*gen.AgentPolicyGrantForm{delegableProjectForm("shared")}, before)
			n, err := accessrepo.New(f.db).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{ID: ids[revoked], OrganizationID: "org-delegable"})
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
			after, err := f.service.ListDelegableGrants(ctx, payload)
			require.NoError(t, err)
			require.Empty(t, after, "prepared request grants must not resurrect revoked live policy")
		})
	}
}

func TestListDelegableGrantsPreservesCallerConstraints(t *testing.T) {
	t.Parallel()
	for _, incompatible := range []bool{false, true} {
		name := "pinned caller tool"
		if incompatible {
			name = "incompatible tools"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newDelegableFixture(t)
			for _, principal := range []string{"agent", "owner", "caller"} {
				selector := authz.NewSelector(authz.ScopeMCPRead, "server-one")
				if principal == "caller" {
					selector[authz.SelectorKeyTool] = "tool-one"
					selector[authz.SelectorKeyProjectID] = "project-one"
				}
				if incompatible && principal == "owner" {
					selector[authz.SelectorKeyTool] = "tool-two"
				}
				f.grant(t, principal, authz.ScopeMCPRead, selector)
			}
			result := f.list(t, "caller")
			if incompatible {
				require.Empty(t, result)
				return
			}
			require.NotEmpty(t, result)
			foundRead := false
			for _, grant := range result {
				require.Contains(t, []string{string(authz.ScopeMCPRead), string(authz.ScopeMCPConnect)}, grant.Scope)
				foundRead = foundRead || grant.Scope == string(authz.ScopeMCPRead)
				require.Equal(t, authz.ResourceKindMCP, grant.Selector.ResourceKind)
				require.Equal(t, "server-one", grant.Selector.ResourceID)
				require.NotNil(t, grant.Selector.Tool)
				require.Equal(t, "tool-one", *grant.Selector.Tool)
				require.NotNil(t, grant.Selector.ProjectID)
				require.Equal(t, "project-one", *grant.Selector.ProjectID)
			}
			require.True(t, foundRead)
		})
	}
}

func TestListDelegableGrantsWithoutResourceRightsIsEmpty(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	for _, principal := range []string{"agent", "owner"} {
		f.grant(t, principal, authz.ScopeProjectRead, authz.NewSelector(authz.ScopeProjectRead, "*"))
	}
	require.Empty(t, f.list(t, "caller"))
}

func TestListDelegableGrantsConservativelyOmitsOverlappingExclusions(t *testing.T) {
	t.Parallel()
	for _, excludedBy := range []string{"owner", "caller"} {
		t.Run(excludedBy, func(t *testing.T) {
			t.Parallel()
			f := newDelegableFixture(t)
			for _, principal := range []string{"agent", "owner", "caller"} {
				f.grant(t, principal, authz.ScopeProjectRead, authz.NewSelector(authz.ScopeProjectRead, "*"))
				f.grant(t, principal, authz.ScopeSkillRead, authz.NewSelector(authz.ScopeSkillRead, "safe-skill"))
			}
			f.grant(t, excludedBy, authz.ScopeProjectBlockedRead, authz.NewSelector(authz.ScopeProjectBlockedRead, "blocked-project"))
			result := f.list(t, "caller")
			require.ElementsMatch(t, []*gen.AgentPolicyGrantForm{{Scope: string(authz.ScopeSkillRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindSkill, ResourceID: "safe-skill"}}}, result,
				"an allow-only wildcard cannot represent subtracting an excluded project; unrelated candidates remain discoverable")
		})
	}
}

func TestListDelegableGrantsRejectsInactiveAgentOrOwner(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"suspended", "revoked", "deleted", "owner deleted", "owner membership removed"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			f := newDelegableFixture(t)
			ctx := validatedHumanContext(t, "org-delegable", "owner")
			switch state {
			case "suspended":
				_, err := f.service.Suspend(ctx, &gen.SuspendPayload{AgentID: f.agentID.String()})
				require.NoError(t, err)
			case "revoked":
				_, err := f.service.Revoke(ctx, &gen.RevokePayload{AgentID: f.agentID.String()})
				require.NoError(t, err)
			case "deleted":
				require.NoError(t, f.service.Delete(ctx, &gen.DeletePayload{AgentID: f.agentID.String()}))
			case "owner deleted":
				require.NoError(t, testrepo.New(f.db).ForceSoftDeleteUser(t.Context(), "owner"))
			case "owner membership removed":
				require.NoError(t, testrepo.New(f.db).ForceSoftDeleteOrganizationUserRelationship(t.Context(), testrepo.ForceSoftDeleteOrganizationUserRelationshipParams{OrganizationID: "org-delegable", UserID: conv.ToPGText("owner")}))
			}
			result, err := f.service.ListDelegableGrants(validatedHumanContext(t, "org-delegable", "caller"), &gen.ListDelegableGrantsPayload{AgentID: f.agentID.String()})
			requireOopsCode(t, err, oops.CodeForbidden)
			require.Empty(t, result)
		})
	}
}

func TestListDelegableGrantsRejectsCrossOrganizationAndNonhumanContexts(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	seedOrganization(t, f.db, "org-other")
	seedOrganizationUser(t, f.db, "org-other", "owner")
	valid := validatedHumanContext(t, "org-delegable", "owner")
	apiKey := *mustAuthContext(t, valid)
	apiKey.APIKeyID = "key-id"
	untrustedSession := "untrusted-session"
	contexts := map[string]context.Context{
		"cross organization":      validatedHumanContext(t, "org-other", "owner"),
		"missing member":          validatedHumanContext(t, "org-delegable", "absent"),
		"anonymous":               t.Context(),
		"unvalidated attribution": contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-delegable", UserID: "owner", SessionID: &untrustedSession}),
		"api key":                 contextvalues.WithValidatedGramSession(contextvalues.SetAuthContext(t.Context(), &apiKey), &apiKey, false),
		"assistant":               contextvalues.SetAssistantPrincipal(valid, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()}),
		"oauth":                   contextvalues.SetOAuthClientID(valid, "client-id"),
		"scope override":          contextvalues.SetRBACScopeOverride(valid, "root"),
		"legacy impersonation":    contextvalues.WithValidatedGramSession(t.Context(), mustAuthContext(t, valid), true),
	}
	for name, ctx := range contexts {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := f.service.ListDelegableGrants(ctx, &gen.ListDelegableGrantsPayload{AgentID: f.agentID.String()})
			require.Error(t, err)
			require.Empty(t, result)
		})
	}
}

func TestListDelegableGrantsCredentialGateFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		provider feature.Provider
	}{
		{name: "disabled", provider: &recordingAgentManagementFeatures{evaluation: feature.EvaluationDisabled}},
		{name: "indeterminate", provider: &recordingAgentManagementFeatures{evaluation: feature.EvaluationIndeterminate}},
		{name: "provider error", provider: &recordingAgentManagementFeatures{err: errors.New("feature provider unavailable")}},
		{name: "missing provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDelegableFixture(t)
			f.service.features = tc.provider
			var logs bytes.Buffer
			f.service.logger = slog.New(slog.NewTextHandler(&logs, nil))
			result, err := f.service.ListDelegableGrants(validatedHumanContext(t, "org-delegable", "owner"), &gen.ListDelegableGrantsPayload{AgentID: f.agentID.String()})
			require.Error(t, err)
			require.Empty(t, result)
			requireOopsCode(t, err, oops.CodeNotFound)
			if tc.name == "provider error" {
				require.Contains(t, logs.String(), "failed to evaluate agent credential rollout flag")
				require.Contains(t, logs.String(), "feature provider unavailable")
			}
			if provider, ok := tc.provider.(*recordingAgentManagementFeatures); ok {
				require.Equal(t, feature.FlagAgentIdentityCredentials, provider.flag)
			}
		})
	}
}

func TestListDelegableGrantsGeneratedEndpointCannotBypassManagementGate(t *testing.T) {
	t.Parallel()
	for _, evaluation := range []feature.Evaluation{feature.EvaluationDisabled, feature.EvaluationIndeterminate} {
		name := "disabled"
		if evaluation == feature.EvaluationIndeterminate {
			name = "indeterminate"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newDelegableFixture(t)
			ctx := validatedHumanContext(t, "org-delegable", "owner")
			flags := &recordingAgentManagementFeatures{evaluation: evaluation}
			f.service.features = flags
			f.service.auth = staticSessionAuthorizer{authCtx: mustAuthContext(t, ctx)}
			endpoint := gen.NewListDelegableGrantsEndpoint(f.service, f.service.APIKeyAuth)
			result, err := endpoint(ctx, &gen.ListDelegableGrantsPayload{AgentID: f.agentID.String()})
			requireOopsCode(t, err, oops.CodeNotFound)
			require.Nil(t, result)
			require.Equal(t, feature.FlagAgentManagement, flags.flag)
		})
	}
}

func TestListDelegableGrantsGatePrecedesAuthorizationPreparation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		provider feature.Provider
	}{
		{name: "disabled", provider: &recordingAgentManagementFeatures{evaluation: feature.EvaluationDisabled}},
		{name: "indeterminate", provider: &recordingAgentManagementFeatures{evaluation: feature.EvaluationIndeterminate}},
		{name: "provider error", provider: &recordingAgentManagementFeatures{err: errors.New("feature provider unavailable")}},
		{name: "missing provider", provider: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// No database or authorization engine: even an unknown agent and caller
			// must receive the cohort's 404 without opening a transaction or loading grants.
			service := newTestService(nil, nil)
			service.features = tc.provider
			var logs bytes.Buffer
			service.logger = slog.New(slog.NewTextHandler(&logs, nil))
			ctx := validatedHumanContext(t, "org-disabled", "unknown-member")
			for _, agentID := range []string{uuid.NewString(), "invalid-id"} {
				result, err := service.ListDelegableGrants(ctx, &gen.ListDelegableGrantsPayload{AgentID: agentID})
				requireOopsCode(t, err, oops.CodeNotFound)
				require.Nil(t, result)
			}
			if tc.name == "provider error" {
				require.Contains(t, logs.String(), "feature provider unavailable")
			}
		})
	}
}

func TestListDelegableGrantsAuthenticatesBeforeCredentialGate(t *testing.T) {
	t.Parallel()
	service := newTestService(nil, nil)
	flags := &recordingAgentManagementFeatures{evaluation: feature.EvaluationDisabled}
	service.features = flags
	result, err := service.ListDelegableGrants(t.Context(), &gen.ListDelegableGrantsPayload{AgentID: uuid.NewString()})
	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.Nil(t, result)
	require.Empty(t, flags.flag, "unauthenticated callers must not evaluate tenant flags")
}

func (f delegableFixture) toolset(t *testing.T, org, slug string) toolsetsrepo.Toolset {
	t.Helper()
	project, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		OrganizationID: org, Name: slug, Slug: slug,
	})
	require.NoError(t, err)
	toolset, err := toolsetsrepo.New(f.db).CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{
		OrganizationID: org, ProjectID: project.ID, Name: slug, Slug: slug, McpEnabled: true,
	})
	require.NoError(t, err)
	return toolset
}

func TestListDelegableGrantsScopedToolset(t *testing.T) {
	t.Parallel()
	for _, excludedBy := range []string{"owner", "caller"} {
		t.Run(excludedBy, func(t *testing.T) {
			t.Parallel()
			f := newDelegableFixture(t)
			selected := f.toolset(t, "org-delegable", "selected")
			other := f.toolset(t, "org-delegable", "other")
			for _, principal := range []string{"agent", "owner", "caller"} {
				f.grant(t, principal, authz.ScopeMCPWrite, authz.NewSelector(authz.ScopeMCPWrite, "*"))
			}
			// Both server and project exclusions on unrelated resources must be disjoint.
			f.grant(t, excludedBy, authz.ScopeMCPBlockedConnect, authz.NewSelector(authz.ScopeMCPBlockedConnect, other.ID.String()))
			exclusion := authz.NewSelector(authz.ScopeMCPBlockedConnect, "*")
			exclusion[authz.SelectorKeyProjectID] = other.ProjectID.String()
			f.grant(t, excludedBy, authz.ScopeMCPBlockedConnect, exclusion)
			require.Empty(t, f.list(t, "caller"), "unscoped broad grants still overlap exclusions")
			ctx := validatedHumanContext(t, "org-delegable", "caller")
			grants, err := f.service.ListDelegableGrants(ctx, &gen.ListDelegableGrantsPayload{
				AgentID: f.agentID.String(), ToolsetID: new(selected.ID.String()),
			})
			require.NoError(t, err)
			require.Len(t, grants, 3)
			for _, grant := range grants {
				require.Equal(t, "allow", grant.Effect)
				require.Equal(t, selected.ID.String(), grant.Selector.ResourceID)
				require.Equal(t, new(selected.ProjectID.String()), grant.Selector.ProjectID)
			}
			grants, err = f.service.ListDelegableGrants(ctx, &gen.ListDelegableGrantsPayload{
				AgentID: f.agentID.String(), ToolsetID: new(other.ID.String()),
			})
			require.NoError(t, err)
			require.Empty(t, grants)
		})
	}
}

func TestListDelegableGrantsBatchedToolsets(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	first := f.toolset(t, "org-delegable", "first")
	second := f.toolset(t, "org-delegable", "second")
	blocked := f.toolset(t, "org-delegable", "blocked")
	for _, principal := range []string{"agent", "owner", "caller"} {
		f.grant(t, principal, authz.ScopeMCPWrite, authz.NewSelector(authz.ScopeMCPWrite, "*"))
	}
	f.grant(t, "owner", authz.ScopeMCPBlockedConnect, authz.NewSelector(authz.ScopeMCPBlockedConnect, blocked.ID.String()))
	ctx := validatedHumanContext(t, "org-delegable", "caller")

	var want []*gen.AgentPolicyGrantForm
	for _, id := range []string{first.ID.String(), second.ID.String(), blocked.ID.String()} {
		grants, err := f.service.ListDelegableGrants(ctx, &gen.ListDelegableGrantsPayload{AgentID: f.agentID.String(), ToolsetID: new(id)})
		require.NoError(t, err)
		want = append(want, grants...)
	}
	require.NotEmpty(t, want)

	// The legacy single field and the batch field combine, and repeats collapse.
	grants, err := f.service.ListDelegableGrants(ctx, &gen.ListDelegableGrantsPayload{
		AgentID:    f.agentID.String(),
		ToolsetID:  new(first.ID.String()),
		ToolsetIds: []string{first.ID.String(), second.ID.String(), blocked.ID.String()},
	})
	require.NoError(t, err)
	require.ElementsMatch(t, want, grants)
	for _, grant := range grants {
		require.NotEqual(t, blocked.ID.String(), grant.Selector.ResourceID)
	}

	_, err = f.service.ListDelegableGrants(ctx, &gen.ListDelegableGrantsPayload{
		AgentID: f.agentID.String(), ToolsetIds: []string{first.ID.String(), uuid.NewString()},
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListDelegableGrantsScopedToolsetRejectsInvalidResources(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	seedOrganization(t, f.db, "org-other-delegable")
	foreign := f.toolset(t, "org-other-delegable", "foreign")
	for _, tc := range []struct {
		name string
		id   string
		code oops.Code
	}{
		{name: "malformed", id: "invalid", code: oops.CodeBadRequest},
		{name: "empty", id: "", code: oops.CodeBadRequest},
		{name: "missing", id: uuid.NewString(), code: oops.CodeNotFound},
		{name: "foreign organization", id: foreign.ID.String(), code: oops.CodeNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			grants, err := f.service.ListDelegableGrants(validatedHumanContext(t, "org-delegable", "caller"), &gen.ListDelegableGrantsPayload{
				AgentID: f.agentID.String(), ToolsetID: &tc.id,
			})
			requireOopsCode(t, err, tc.code)
			require.Nil(t, grants)
		})
	}
}

func (f delegableFixture) modernServer(t *testing.T, projectID uuid.UUID, toolsetID uuid.NullUUID) mcpserversrepo.McpServer {
	t.Helper()
	var remoteID uuid.NullUUID
	if !toolsetID.Valid {
		remote, err := remotemcprepo.New(f.db).CreateServer(t.Context(), remotemcprepo.CreateServerParams{
			ID: uuid.New(), ProjectID: projectID, TransportType: "streamable-http", Url: "https://mcp.example.test/mcp",
		})
		require.NoError(t, err)
		remoteID = uuid.NullUUID{UUID: remote.ID, Valid: true}
	}
	server, err := mcpserversrepo.New(f.db).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, ToolsetID: toolsetID, RemoteMcpServerID: remoteID, Visibility: "private",
	})
	require.NoError(t, err)
	return server
}

func TestListDelegableGrantsNormalizedMCPResourceIdentity(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"legacy toolset", "modern toolset backed", "modern remote"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := newDelegableFixture(t)
			toolset := f.toolset(t, "org-delegable", "selected")
			resourceID := toolset.ID
			switch kind {
			case "modern toolset backed":
				server := f.modernServer(t, toolset.ProjectID, uuid.NullUUID{UUID: toolset.ID, Valid: true})
				require.NotEqual(t, resourceID, server.ID, "inventory uses toolsetId, not the wrapper ID")
			case "modern remote":
				server := f.modernServer(t, toolset.ProjectID, uuid.NullUUID{})
				resourceID = server.ID
			}
			for _, principal := range []string{"agent", "owner", "caller"} {
				f.grant(t, principal, authz.ScopeMCPConnect, authz.NewSelector(authz.ScopeMCPConnect, resourceID.String()))
			}
			grants, err := f.service.ListDelegableGrants(validatedHumanContext(t, "org-delegable", "caller"), &gen.ListDelegableGrantsPayload{
				AgentID: f.agentID.String(), ToolsetID: new(resourceID.String()),
			})
			require.NoError(t, err)
			require.Len(t, grants, 1)
			require.Equal(t, resourceID.String(), grants[0].Selector.ResourceID)
			require.Equal(t, new(toolset.ProjectID.String()), grants[0].Selector.ProjectID)
		})
	}
}

func TestListDelegableGrantsModernResourceFailsClosed(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	seedOrganization(t, f.db, "org-foreign-discovery")
	foreign := f.toolset(t, "org-foreign-discovery", "foreign")
	remote := f.modernServer(t, foreign.ProjectID, uuid.NullUUID{})
	backed := f.modernServer(t, foreign.ProjectID, uuid.NullUUID{UUID: foreign.ID, Valid: true})
	for _, resourceID := range []uuid.UUID{remote.ID, backed.ID, foreign.ID, uuid.New()} {
		grants, err := f.service.ListDelegableGrants(validatedHumanContext(t, "org-delegable", "caller"), &gen.ListDelegableGrantsPayload{
			AgentID: f.agentID.String(), ToolsetID: new(resourceID.String()),
		})
		requireOopsCode(t, err, oops.CodeNotFound)
		require.Nil(t, grants)
	}
}
