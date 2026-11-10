package platformmcp

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest,tparallel // Subtests share a database and mutate organization-wide grants and audit state.
func TestRiskPolicyAudienceChangeTransaction(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_audience_change")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedAccessMember(t, ctx, conn, principal.OrganizationID, principal.UserID, "actor@example.test")
	otherID := "user_other_audience"
	seedAccessMember(t, ctx, conn, principal.OrganizationID, otherID, "other@example.test")
	self := urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID)
	other := urn.NewPrincipal(urn.PrincipalTypeUser, otherID)
	role := seedAccessRole(t, ctx, conn, principal.OrganizationID, "audience-role", "Audience role")
	ctx = ContextWithPrincipal(ctx, principal)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPRiskMutations, principal.OrganizationID, true)
	controls, err := NewRiskMutationControls(conn, flags, NewPostgresOrganizationSlugResolver(conn), testOperationBudget(), "audience-change-test-key")
	require.NoError(t, err)
	handlers, err := NewRiskPolicyMutationHandlers(conn, controls, risk.NewPolicyMutationCore(conn, audit.NewLogger(), nil, noopRiskPolicySignaler{}, nil))
	require.NoError(t, err)
	reads, err := newRiskReadService(conn, "audience-change-test-key")
	require.NoError(t, err)
	create := func(t *testing.T, targets []string) (string, string) {
		t.Helper()
		_, created, err := handlers.CreatePolicy(ctx, nil, map[string]any{"project_slug": project.Slug, "policy_type": "standard", "name": "Policy " + uuid.NewString(), "enabled": true, "sources": []string{"gitleaks"}, "idempotency_key": uuid.NewString()})
		require.NoError(t, err)
		if targets == nil {
			return created.Policy.ID, created.Version
		}
		_, updated, err := handlers.UpdatePolicy(ctx, nil, map[string]any{"project_slug": project.Slug, "policy_id": created.Policy.ID, "expected_version": created.Version, "idempotency_key": uuid.NewString(), "patch": map[string]any{"audience": map[string]any{"type": "targeted", "principal_urns": targets, "confirm": true}}})
		require.NoError(t, err)
		return created.Policy.ID, updated.Version
	}
	arguments := func(id, version string, add, remove []string) map[string]any {
		args := map[string]any{"project_slug": project.Slug, "policy_id": id, "expected_version": version, "idempotency_key": uuid.NewString(), "confirmed": true}
		if add == nil {
			add = []string{}
		}
		if remove == nil {
			remove = []string{}
		}
		args["add_principals"], args["remove_principals"] = add, remove
		return args
	}
	read := func(t *testing.T, id string) *RiskPolicyDetail {
		t.Helper()
		result, err := reads.GetPolicy(ctx, principal, GetRiskPolicyInput{ProjectSlug: project.Slug, PolicyID: id})
		require.NoError(t, err)
		return &result.Policy
	}
	for _, tc := range []struct {
		name                       string
		initial, add, remove, want []string
	}{
		{"add user", []string{self.String()}, []string{other.String()}, nil, []string{self.String(), other.String()}},
		{"add role", []string{self.String()}, []string{role.String()}, nil, []string{self.String(), role.String()}},
		{"remove user", []string{self.String(), other.String()}, nil, []string{other.String()}, []string{self.String()}},
		{"remove role", []string{self.String(), role.String()}, nil, []string{role.String()}, []string{self.String()}},
		{"mixed", []string{self.String(), other.String()}, []string{role.String()}, []string{other.String()}, []string{self.String(), role.String()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, version := create(t, tc.initial)
			_, changed, err := handlers.ChangeAudience(ctx, nil, arguments(id, version, tc.add, tc.remove))
			require.NoError(t, err)
			live := read(t, id)
			require.Equal(t, "targeted", live.Audience.Type)
			require.ElementsMatch(t, tc.want, live.Audience.PrincipalURNs)
			require.Equal(t, changed.Version, live.Version)
			require.NotEqual(t, version, live.Version)
			entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionRiskPolicyUpdate)
			require.NoError(t, err)
			require.Equal(t, id, entry.SubjectID)
			require.Equal(t, principal.UserID, entry.ActorID)
		})
	}
	t.Run("settings unrelated and surviving URL grants preserved", func(t *testing.T) {
		id, _ := create(t, []string{self.String(), other.String()})
		unrelated := authz.Resource{OrganizationID: principal.OrganizationID, Scope: authz.ScopeRiskPolicyEvaluate, ResourceID: uuid.NewString()}
		require.NoError(t, authz.ReplaceGrantAudience(ctx, conn, authz.ResourceGrant{Resource: unrelated, Principals: []urn.Principal{other}, Selector: authz.NewSelector(unrelated.Scope, unrelated.ResourceID)}))
		require.NoError(t, policybypass.ReplacePolicyURLAudience(ctx, conn, principal.OrganizationID, authz.ScopeRiskPolicyBypass, id, "https://audience.example.test/mcp", []urn.Principal{self, other}))
		resource := authz.Resource{OrganizationID: principal.OrganizationID, Scope: authz.ScopeRiskPolicyBypass, ResourceID: id}
		beforeGrants, err := authz.ListGrantsForResource(ctx, conn, resource)
		require.NoError(t, err)
		before := *read(t, id)
		_, _, err = handlers.ChangeAudience(ctx, nil, arguments(id, before.Version, []string{role.String()}, nil))
		require.NoError(t, err)
		after := *read(t, id)
		after.Audience, after.Version, after.UpdatedAt = before.Audience, before.Version, before.UpdatedAt
		require.Equal(t, before, after)
		grants, err := authz.ListGrantsForResource(ctx, conn, resource)
		require.NoError(t, err)
		require.ElementsMatch(t, beforeGrants, grants)
		_, _, err = handlers.ChangeAudience(ctx, nil, arguments(id, read(t, id).Version, nil, []string{other.String()}))
		require.NoError(t, err)
		grants, err = authz.ListGrantsForResource(ctx, conn, resource)
		require.NoError(t, err)
		// Evaluation deltas leave URL-specific bypass grants untouched, even
		// when the corresponding principal leaves the evaluation audience.
		require.ElementsMatch(t, beforeGrants, grants)
		grants, err = authz.ListGrantsForResource(ctx, conn, unrelated)
		require.NoError(t, err)
		require.Len(t, grants, 1)
		require.Equal(t, other.String(), grants[0].PrincipalUrn)
	})
	t.Run("stale replay and key conflict", func(t *testing.T) {
		id, version := create(t, []string{self.String()})
		args := arguments(id, version, []string{other.String()}, nil)
		_, changed, err := handlers.ChangeAudience(ctx, nil, args)
		require.NoError(t, err)
		_, replay, err := handlers.ChangeAudience(ctx, nil, args)
		require.NoError(t, err)
		require.True(t, replay.Receipt.Replayed)
		require.Equal(t, changed.Receipt.ID, replay.Receipt.ID)
		args["add_principals"] = []string{role.String()}
		_, _, err = handlers.ChangeAudience(ctx, nil, args)
		requireRiskMutationRefusal(t, err, "conflict")
		args["idempotency_key"] = uuid.NewString()
		_, _, err = handlers.ChangeAudience(ctx, nil, args)
		requireRiskMutationRefusal(t, err, "conflict")
		require.Equal(t, changed.Version, read(t, id).Version)
	})
	t.Run("concurrent same version has one winner", func(t *testing.T) {
		id, version := create(t, []string{self.String()})
		inputs := []map[string]any{arguments(id, version, []string{other.String()}, nil), arguments(id, version, []string{role.String()}, nil)}
		errors := make([]error, len(inputs))
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i, args := range inputs {
			wg.Go(func() { ; <-start; _, _, errors[i] = handlers.ChangeAudience(ctx, nil, args) })
		}
		close(start)
		wg.Wait()
		winners := 0
		for i, err := range errors {
			if err == nil {
				winners++
				added, ok := inputs[i]["add_principals"].([]string)
				require.True(t, ok)
				require.ElementsMatch(t, append([]string{self.String()}, added...), read(t, id).Audience.PrincipalURNs)
			} else {
				requireRiskMutationRefusal(t, err, "conflict")
			}
		}
		require.Equal(t, 1, winners)
	})
	t.Run("foreign principals rejected on add and removal", func(t *testing.T) {
		foreign, _ := seedRegistrationLifecycle(t, ctx, conn)
		seedAccessMember(t, ctx, conn, foreign.OrganizationID, foreign.UserID, "foreign@example.test")
		foreignRole := seedAccessRole(t, ctx, conn, foreign.OrganizationID, "foreign-role", "Foreign role")
		id, version := create(t, []string{self.String(), other.String()})
		for _, target := range []string{urn.NewPrincipal(urn.PrincipalTypeUser, foreign.UserID).String(), foreignRole.String()} {
			for _, field := range []string{"add_principals", "remove_principals"} {
				args := arguments(id, version, nil, nil)
				args[field] = []string{target}
				_, _, err := handlers.ChangeAudience(ctx, nil, args)
				require.Error(t, err)
				require.Equal(t, version, read(t, id).Version)
			}
		}
	})
	t.Run("refusals do not mutate", func(t *testing.T) {
		everyoneID, everyoneVersion := create(t, nil)
		_, _, err := handlers.ChangeAudience(ctx, nil, arguments(everyoneID, everyoneVersion, []string{other.String()}, nil))
		require.Error(t, err)
		require.Equal(t, "everyone", read(t, everyoneID).Audience.Type)
		require.Equal(t, everyoneVersion, read(t, everyoneID).Version)
		id, version := create(t, []string{self.String()})
		for _, args := range []map[string]any{
			arguments(id, version, nil, []string{self.String()}),
			arguments(id, version, []string{other.String()}, []string{other.String()}),
			arguments(id, version, nil, nil),
			arguments(id, version, []string{"not-a-principal"}, nil),
		} {
			_, _, err := handlers.ChangeAudience(ctx, nil, args)
			require.Error(t, err)
			require.Equal(t, version, read(t, id).Version)
		}
		for _, confirmation := range []any{false, "true", nil} {
			args := arguments(id, version, []string{other.String()}, nil)
			args["confirmed"] = confirmation
			if confirmation == nil {
				delete(args, "confirmed")
			}
			_, _, err := handlers.ChangeAudience(ctx, nil, args)
			require.Error(t, err)
			require.Equal(t, version, read(t, id).Version)
		}
		for _, field := range []string{"add_principals", "remove_principals"} {
			oversized := make([]string, 101)
			for i := range oversized {
				oversized[i] = urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString()).String()
			}
			args := arguments(id, version, nil, nil)
			args[field] = oversized
			_, _, err := handlers.ChangeAudience(ctx, nil, args)
			require.Error(t, err)
			require.Equal(t, version, read(t, id).Version)
		}
		args := arguments(id, version, []string{other.String()}, nil)
		args["unknown"] = true
		_, _, err = handlers.ChangeAudience(ctx, nil, args)
		require.Error(t, err)
		require.Equal(t, version, read(t, id).Version)
	})
	t.Run("assistant refused and audience remains private", func(t *testing.T) {
		id, version := create(t, []string{self.String(), other.String()})
		assistant := principal
		assistant.Surface, assistant.ConnectionID, assistant.Generation = SurfaceProjectAssistant, "", ""
		assistantCtx := ContextWithPrincipal(ctx, assistant)
		_, _, err := handlers.ChangeAudience(assistantCtx, nil, arguments(id, version, []string{role.String()}, nil))
		require.Error(t, err)
		hidden, err := reads.GetPolicy(assistantCtx, assistant, GetRiskPolicyInput{ProjectSlug: project.Slug, PolicyID: id})
		require.NoError(t, err)
		require.Nil(t, hidden.Policy.Audience)
		encoded, err := json.Marshal(hidden)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), self.String())
		require.NotContains(t, string(encoded), other.String())
		require.Equal(t, version, read(t, id).Version)
	})
}
