package authz

import (
	"context"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var planTenant = PlanTenant{OrganizationID: fixtureOrgID, ProjectID: "proj_1"}

// planChatAdapter maps the ownership chat model onto a chats table aliased c.
func planChatAdapter() PlanAdapter {
	return PlanAdapter{
		Name: "chats.test",
		Table: PlanTable{
			ID: "c.id::text",
			Facts: map[string]PlanColumn{
				"internal_user_id": {SQL: "c.user_id", Nullable: true},
				"external_user_id": {SQL: "c.external_user_id", Nullable: true},
			},
			Relations: map[string]PlanRelation{
				"agent": {
					From: "agents a",
					Join: "a.id = c.agent_id",
					Target: PlanTable{
						ID: "a.id::text",
						Facts: map[string]PlanColumn{
							"owner":                          {SQL: "a.owner_user_id", Nullable: false},
							"deleted":                        {SQL: "a.deleted", Nullable: false},
							"owner_reassignment_required_at": {SQL: "a.owner_reassignment_required_at", Nullable: true},
						},
						Relations: nil,
					},
				},
			},
		},
	}
}

func chatRef(permission string) s.PermissionRef {
	return s.PermissionRef{Type: "chat", Permission: permission}
}

func boundPredicate(t *testing.T, plan AccessPlan) (string, []any) {
	t.Helper()
	bound, err := plan.BindSnapshot("snapshot_1")
	require.NoError(t, err)
	sql, args, err := bound.Predicate(1)
	require.NoError(t, err)
	return sql, args
}

// ownedAgentSQL is the agent-only branch of the ownership read, with the
// actor bound to parameter n.
func ownedAgentSQL(n int) string {
	return "((c.user_id IS NULL) AND (c.external_user_id IS NULL OR c.external_user_id = '') AND EXISTS (SELECT 1 FROM agents a WHERE a.id = c.agent_id AND TRUE) AND EXISTS (SELECT 1 FROM agents a WHERE a.id = c.agent_id AND ((a.deleted IS FALSE) AND (a.owner_reassignment_required_at IS NULL) AND COALESCE(a.owner_user_id = $" + strconv.Itoa(n) + "::text, FALSE))))"
}

func TestAccessPlanRendersOwnershipRead(t *testing.T) {
	t.Parallel()

	evaluator := ownershipEvaluator(t)
	otherReader := grantOf("user:user_1", "chat:read", "chat", "chat_2")
	cases := []struct {
		name string
		set  PolicySet
		want string
		args []any
	}{
		{
			name: "no grants leave authorship and owned agents",
			set:  humanSet(),
			want: "(COALESCE(c.user_id = $1::text, FALSE) OR " + ownedAgentSQL(2) + ")",
			args: []any{"user_1", "user_1"},
		},
		{
			name: "a wildcard reader grant admits every row",
			set:  humanSet(grantOf("role:organization:role_1", "chat:read", "chat", "*")),
			want: "TRUE",
			args: nil,
		},
		{
			name: "a writer grant admits every row through the union",
			set:  humanSet(grantOf("role:organization:role_1", "chat:write", "chat", "*")),
			want: "TRUE",
			args: nil,
		},
		{
			name: "grants naming chats admit exactly those IDs",
			set:  humanSet(otherReader, grantOf("user:user_1", "chat:write", "chat", "chat_1"), otherReader),
			want: "(COALESCE(c.id::text = ANY($1::text[]), FALSE) OR COALESCE(c.user_id = $2::text, FALSE) OR " + ownedAgentSQL(3) + ")",
			args: []any{[]string{"chat_1", "chat_2"}, "user_1", "user_1"},
		},
		{
			name: "a grant for another kind admits nothing more",
			set:  humanSet(grantOf("user:user_1", "agent:read", "agent", "*")),
			want: "(COALESCE(c.user_id = $1::text, FALSE) OR " + ownedAgentSQL(2) + ")",
			args: []any{"user_1", "user_1"},
		},
		{
			name: "an agent actor is never an author or an agent owner",
			set: PolicySet{
				Actor:           agentActor,
				Policies:        []Policy{{Principal: agentActor.String(), Grants: nil}},
				Captured:        nil,
				RepresentedUser: "",
				Represented:     nil,
			},
			want: "FALSE",
			args: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, err := evaluator.Plan(tc.set, chatRef("read"), planChatAdapter(), planTenant)
			require.NoError(t, err)
			sql, args := boundPredicate(t, plan)
			require.Equal(t, tc.want, sql)
			require.Equal(t, tc.args, args)
		})
	}
}

func TestAccessPlanRequiresEveryPolicy(t *testing.T) {
	t.Parallel()

	evaluator := ownershipEvaluator(t)
	set := humanSet(grantOf("role:organization:role_1", "chat:read", "chat", "*"))
	set.Captured = []Policy{{Principal: "captured[0]", Grants: []Grant{grantOf("role:organization:role_1", "chat:read", "chat", "chat_9")}}}

	plan, err := evaluator.Plan(set, chatRef("read"), planChatAdapter(), planTenant)
	require.NoError(t, err)
	sql, args := boundPredicate(t, plan)
	require.Equal(t, "(COALESCE(c.id::text = ANY($1::text[]), FALSE) OR COALESCE(c.user_id = $2::text, FALSE) OR "+ownedAgentSQL(3)+")", sql)
	require.Equal(t, []any{[]string{"chat_9"}, "user_1", "user_1"}, args)

	set.Captured = []Policy{{Principal: "captured[0]", Grants: nil}}
	set.Policies[0].Grants = nil
	set.Actor = agentActor
	plan, err = evaluator.Plan(set, chatRef("read"), planChatAdapter(), planTenant)
	require.NoError(t, err)
	sql, _ = boundPredicate(t, plan)
	require.Equal(t, "FALSE", sql)
}

func differenceEvaluator(t *testing.T) *RegistryEvaluator {
	t.Helper()
	reg, err := s.Compile(s.Config{Roles: nil, WriteSurfaces: []string{"access"}, Conditions: nil, RetiredRuntimeScopes: nil},
		s.NewType("chat", s.WrittenBy("access"),
			s.Attr("internal_user_id", s.FactNullableString),
			s.Derived("author", s.NonEmptyPrincipal(s.FactUser, "internal_user_id")),
			s.Grant("reader", s.StoredAs("chat:read"), s.Describe("Read session transcripts")),
			s.Grant("writer", s.StoredAs("chat:write"), s.Describe("Change sessions")),
			s.Permission("read_grant", s.Rel("reader"), s.Checks("chat:read")),
			s.Permission("write_grant", s.Rel("writer"), s.Checks("chat:write")),
			s.Permission("readable", s.Union(s.Rel("reader"), s.Is("author"))),
			s.Permission("read_only", s.Minus(s.Perm("readable"), s.Rel("writer"))),
		),
	)
	require.NoError(t, err)
	evaluator, err := NewRegistryEvaluator(reg)
	require.NoError(t, err)
	return evaluator
}

func TestAccessPlanRendersDifference(t *testing.T) {
	t.Parallel()

	evaluator := differenceEvaluator(t)
	adapter := PlanAdapter{Name: "chats.test", Table: PlanTable{
		ID:        "c.id::text",
		Facts:     map[string]PlanColumn{"internal_user_id": {SQL: "c.user_id", Nullable: true}},
		Relations: nil,
	}}
	cases := []struct {
		name   string
		grants []Grant
		want   string
		args   []any
	}{
		{"subtracting nothing keeps the base", nil, "COALESCE(c.user_id = $1::text, FALSE)", []any{"user_1"}},
		{"a wildcard subtraction removes every row", []Grant{grantOf("user:user_1", "chat:write", "chat", "*")}, "FALSE", nil},
		{
			"a named subtraction removes only its IDs",
			[]Grant{grantOf("user:user_1", "chat:read", "chat", "*"), grantOf("user:user_1", "chat:write", "chat", "chat_3")},
			"(NOT COALESCE(c.id::text = ANY($1::text[]), FALSE))",
			[]any{[]string{"chat_3"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, err := evaluator.Plan(humanSet(tc.grants...), chatRef("read_only"), adapter, planTenant)
			require.NoError(t, err)
			sql, args := boundPredicate(t, plan)
			require.Equal(t, tc.want, sql)
			require.Equal(t, tc.args, args)
		})
	}
}

func TestAccessPlanRefusesWhatItCannotExpress(t *testing.T) {
	t.Parallel()

	ownership := ownershipEvaluator(t)
	registry, err := DefaultRegistry()
	require.NoError(t, err)
	production, err := NewRegistryEvaluator(registry)
	require.NoError(t, err)

	withoutAgent := planChatAdapter()
	withoutAgent.Table.Relations = nil
	withoutAuthor := planChatAdapter()
	withoutAuthor.Table.Facts = map[string]PlanColumn{"external_user_id": {SQL: "c.external_user_id", Nullable: true}}
	nullableDeleted := planChatAdapter()
	agent := nullableDeleted.Table.Relations["agent"]
	agent.Target.Facts = map[string]PlanColumn{
		"owner":                          {SQL: "a.owner_user_id", Nullable: false},
		"deleted":                        {SQL: "a.deleted", Nullable: true},
		"owner_reassignment_required_at": {SQL: "a.owner_reassignment_required_at", Nullable: true},
	}
	nullableDeleted.Table.Relations = map[string]PlanRelation{"agent": agent}
	noID := planChatAdapter()
	noID.Table.ID = ""
	unnamed := planChatAdapter()
	unnamed.Name = ""

	cases := []struct {
		name      string
		evaluator *RegistryEvaluator
		set       PolicySet
		ref       s.PermissionRef
		adapter   PlanAdapter
		tenant    PlanTenant
		want      error
	}{
		{"block precedence", production, humanSet(), s.PermissionRef{Type: "project", Permission: "read"}, planChatAdapter(), planTenant, ErrPlanUnsupported},
		{"dimensions", production, humanSet(), s.PermissionRef{Type: "inference", Permission: "execute"}, planChatAdapter(), planTenant, ErrPlanUnsupported},
		{"represented policies", production, humanSet(), s.PermissionRef{Type: "mcp_auth_client", Permission: "use"}, planChatAdapter(), planTenant, ErrPlanUnsupported},
		{"unknown permission", ownership, humanSet(), chatRef("missing"), planChatAdapter(), planTenant, ErrUnknownPermission},
		{"relationship the adapter cannot join", ownership, humanSet(), chatRef("read"), withoutAgent, planTenant, ErrPlanUnsupported},
		{"fact the adapter cannot read", ownership, humanSet(), chatRef("read"), withoutAuthor, planTenant, ErrPlanUnsupported},
		{"nullable boolean guard", ownership, humanSet(), chatRef("read"), nullableDeleted, planTenant, ErrPlanUnsupported},
		{"no ID column", ownership, humanSet(), chatRef("read"), noID, planTenant, ErrPlanUnsupported},
		{"no policies", ownership, PolicySet{Actor: humanActor, Policies: nil, Captured: nil, RepresentedUser: "", Represented: nil}, chatRef("read"), planChatAdapter(), planTenant, ErrNoPolicies},
		{"no actor", ownership, PolicySet{Actor: urn.Principal{}, Policies: []Policy{{Principal: "live[0]", Grants: nil}}, Captured: nil, RepresentedUser: "", Represented: nil}, chatRef("read"), planChatAdapter(), planTenant, ErrMissingActor},
		{"no project", ownership, humanSet(), chatRef("read"), planChatAdapter(), PlanTenant{OrganizationID: fixtureOrgID, ProjectID: ""}, ErrPlanTenant},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.evaluator.Plan(tc.set, tc.ref, tc.adapter, tc.tenant)
			require.ErrorIs(t, err, tc.want)
		})
	}

	_, err = ownership.Plan(humanSet(), chatRef("read"), unnamed, planTenant)
	require.Error(t, err)
}

func TestAccessPlanBinding(t *testing.T) {
	t.Parallel()

	evaluator := ownershipEvaluator(t)
	plan, err := evaluator.Plan(humanSet(), chatRef("read"), planChatAdapter(), planTenant)
	require.NoError(t, err)

	_, _, err = plan.Predicate(1)
	require.Error(t, err, "an unbound plan renders nothing")
	_, err = plan.BindSnapshot("")
	require.Error(t, err)

	bound, err := plan.BindSnapshot("snapshot_1")
	require.NoError(t, err)
	require.Empty(t, plan.Binding().Snapshot, "binding returns a copy")
	_, err = bound.BindSnapshot("snapshot_2")
	require.ErrorIs(t, err, ErrPlanStale)
	_, _, err = bound.Predicate(0)
	require.Error(t, err, "parameters are numbered from $1")

	require.NoError(t, bound.Reusable(bound.Binding()))
	mutations := map[string]func(*PlanBinding){
		"organization": func(b *PlanBinding) { b.OrganizationID = "org_other" },
		"project":      func(b *PlanBinding) { b.ProjectID = "proj_other" },
		"actor":        func(b *PlanBinding) { b.Actor = urn.NewPrincipal(urn.PrincipalTypeUser, "user_2") },
		"permission":   func(b *PlanBinding) { b.Permission = chatRef("write") },
		"registry":     func(b *PlanBinding) { b.Registry = "registry_other" },
		"policies":     func(b *PlanBinding) { b.Policies = "policies_other" },
		"adapter":      func(b *PlanBinding) { b.Adapter = "chats.other" },
		"snapshot":     func(b *PlanBinding) { b.Snapshot = "snapshot_2" },
	}
	for name, mutate := range mutations {
		current := bound.Binding()
		mutate(&current)
		require.ErrorIs(t, bound.Reusable(current), ErrPlanStale, name)
	}
	require.ErrorIs(t, AccessPlan{}.Reusable(PlanBinding{}), ErrPlanStale)

	// A changed grant changes the policies fingerprint.
	widened, err := evaluator.Plan(humanSet(grantOf("user:user_1", "chat:read", "chat", "chat_2")), chatRef("read"), planChatAdapter(), planTenant)
	require.NoError(t, err)
	require.NotEqual(t, plan.Binding().Policies, widened.Binding().Policies)
	require.ErrorIs(t, plan.Reusable(widened.Binding()), ErrPlanStale)
}

func TestClientPlanProductionChatGrants(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t)
	adapter := PlanAdapter{Name: "chats.grants", Table: PlanTable{ID: "c.id::text", Facts: nil, Relations: nil}}
	ref := chatRef("read_grant")
	projectID := uuid.New()
	withProject := func(t *testing.T, grants ...Grant) context.Context {
		t.Helper()
		ctx := humanClientContext(t, grants...)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		authCtx.ProjectID = &projectID
		return ctx
	}

	cases := []struct {
		name   string
		grants []Grant
		want   string
	}{
		{"no grants admit nothing", nil, "FALSE"},
		{"a project-wide reader admits everything", []Grant{grantOf("user:"+fixtureUserID, "chat:read", "chat", "*")}, "TRUE"},
		{"a named chat admits that chat", []Grant{grantOf("user:"+fixtureUserID, "chat:write", "chat", "chat_1")}, "COALESCE(c.id::text = ANY($1::text[]), FALSE)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := withProject(t, tc.grants...)
			plan, err := client.Plan(ctx, ref, adapter)
			require.NoError(t, err)
			binding, err := client.PlanBinding(ctx, ref, adapter)
			require.NoError(t, err)
			require.NoError(t, plan.Reusable(binding))
			require.Equal(t, fixtureOrgID, binding.OrganizationID)
			require.Equal(t, projectID.String(), binding.ProjectID)
			require.NotEmpty(t, binding.Registry)
			sql, _ := boundPredicate(t, plan)
			require.Equal(t, tc.want, sql)
		})
	}

	t.Run("a legacy API key is not enforced", func(t *testing.T) {
		t.Parallel()
		authCtx := &contextvalues.AuthContext{
			ActiveOrganizationID:  fixtureOrgID,
			UserID:                fixtureUserID,
			ExternalUserID:        "",
			APIKeyID:              "key_1",
			SessionID:             nil,
			ProjectID:             &projectID,
			OrganizationSlug:      "",
			Email:                 nil,
			AccountType:           "enterprise",
			HasActiveSubscription: false,
			Whitelisted:           false,
			ProjectSlug:           nil,
			APIKeyScopes:          nil,
		}
		ctx := contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), authCtx)
		_, err := client.Plan(ctx, ref, adapter)
		require.ErrorIs(t, err, ErrPlanNotEnforced)
	})

	t.Run("a request without a project is refused", func(t *testing.T) {
		t.Parallel()
		_, err := client.Plan(humanClientContext(t), ref, adapter)
		require.ErrorIs(t, err, ErrPlanTenant)
	})
}
