package platformmcp

import (
	"context"
	"testing"
	"time"

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

func TestSelfRemovalRiskPolicyTransaction(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_self_removal")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedAccessMember(t, ctx, conn, principal.OrganizationID, principal.UserID, "self@example.test")
	otherID := "user_other_audience"
	seedAccessMember(t, ctx, conn, principal.OrganizationID, otherID, "other@example.test")
	self := urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID)
	other := urn.NewPrincipal(urn.PrincipalTypeUser, otherID)
	role := seedAccessRole(t, ctx, conn, principal.OrganizationID, "audience-role", "Audience role")
	ctx = ContextWithPrincipal(ctx, principal)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPRiskMutations, principal.OrganizationID, true)
	controls, err := NewRiskMutationControls(conn, flags, NewPostgresOrganizationSlugResolver(conn), testOperationBudget(), "self-removal-test-key")
	require.NoError(t, err)
	handlers, err := NewRiskPolicyMutationHandlers(conn, controls, risk.NewPolicyMutationCore(conn, audit.NewLogger(), nil, noopRiskPolicySignaler{}, nil))
	require.NoError(t, err)
	reads, err := newRiskReadService(conn, "self-removal-test-key")
	require.NoError(t, err)
	create := func(t *testing.T, kind string, targets []string) (string, string) {
		t.Helper()
		_, created, err := handlers.CreatePolicy(ctx, nil, map[string]any{"project_slug": project.Slug, "policy_type": "standard", "name": "Policy " + uuid.NewString(), "enabled": true, "sources": []string{"gitleaks"}, "idempotency_key": uuid.NewString()})
		require.NoError(t, err)
		if kind == "everyone" {
			return created.Policy.ID, created.Version
		}
		_, updated, err := handlers.UpdatePolicy(ctx, nil, map[string]any{"project_slug": project.Slug, "policy_id": created.Policy.ID, "expected_version": created.Version, "idempotency_key": uuid.NewString(), "patch": map[string]any{"audience": map[string]any{"type": kind, "principal_urns": targets, "confirm": true}}})
		require.NoError(t, err)
		return created.Policy.ID, updated.Version
	}
	arguments := func(id, version string) map[string]any {
		return map[string]any{"project_slug": project.Slug, "policy_id": id, "expected_version": version, "idempotency_key": uuid.NewString(), "confirmed": true}
	}
	read := func(t *testing.T, id string) GetRiskPolicyOutput {
		t.Helper()
		result, err := reads.GetPolicy(ctx, principal, GetRiskPolicyInput{ProjectSlug: project.Slug, PolicyID: id})
		require.NoError(t, err)
		return result
	}
	t.Run("remove replay stale and preserve dependent URL grants", func(t *testing.T) {
		id, version := create(t, "targeted", []string{self.String(), other.String()})
		args := arguments(id, version)
		args["expected_version"] = "stale"
		_, _, err := handlers.RemoveSelf(ctx, nil, args)
		requireRiskMutationRefusal(t, err, "conflict")
		require.Equal(t, version, read(t, id).Policy.Version)
		serverURL := "https://audience.example.test/mcp"
		require.NoError(t, policybypass.ReplacePolicyURLAudience(ctx, conn, principal.OrganizationID, authz.ScopeRiskPolicyBypass, id, serverURL, []urn.Principal{self, other}))
		args["expected_version"] = read(t, id).Policy.Version
		_, removed, err := handlers.RemoveSelf(ctx, nil, args)
		require.NoError(t, err)
		after := read(t, id)
		require.Equal(t, []string{other.String()}, after.Policy.Audience.PrincipalURNs)
		require.Equal(t, removed.Version, after.Policy.Version)
		require.NotEqual(t, version, removed.Version)
		grants, err := authz.ListGrantsForResource(ctx, conn, authz.Resource{OrganizationID: principal.OrganizationID, Scope: authz.ScopeRiskPolicyBypass, ResourceID: id})
		require.NoError(t, err)
		require.Len(t, grants, 1)
		require.Equal(t, other.String(), grants[0].PrincipalUrn)
		entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionRiskPolicyUpdate)
		require.NoError(t, err)
		require.Equal(t, id, entry.SubjectID)
		require.Equal(t, principal.UserID, entry.ActorID)
		_, replay, err := handlers.RemoveSelf(ctx, nil, args)
		require.NoError(t, err)
		require.True(t, replay.Receipt.Replayed)
		require.Equal(t, removed.Receipt.ID, replay.Receipt.ID)
	})
	for _, test := range []struct {
		name, kind string
		targets    []string
		message    string
	}{
		{"everyone", "everyone", nil, "everyone-except-one"},
		{"role", "targeted", []string{self.String(), other.String(), role.String()}, "role-containing"},
		{"last", "targeted", []string{self.String()}, "empty"},
		{"missing", "targeted", []string{other.String()}, "no explicit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, version := create(t, test.kind, test.targets)
			_, _, err := handlers.RemoveSelf(ctx, nil, arguments(id, version))
			require.ErrorContains(t, err, test.message)
			require.Equal(t, version, read(t, id).Policy.Version)
		})
	}
	t.Run("unknown input confirmation assistant and cross project refuse", func(t *testing.T) {
		id, version := create(t, "targeted", []string{self.String(), other.String()})
		args := arguments(id, version)
		args["user_id"] = otherID
		_, _, err := handlers.RemoveSelf(ctx, nil, args)
		require.Error(t, err)
		delete(args, "user_id")
		args["confirmed"] = false
		_, _, err = handlers.RemoveSelf(ctx, nil, args)
		require.Error(t, err)
		args["confirmed"] = true
		assistant := principal
		assistant.Surface = SurfaceProjectAssistant
		assistant.ConnectionID = ""
		assistant.Generation = ""
		assistantCtx := ContextWithPrincipal(ctx, assistant)
		_, _, err = handlers.RemoveSelf(assistantCtx, nil, args)
		require.ErrorContains(t, err, "external OAuth")
		_, _, err = handlers.UpdatePolicy(assistantCtx, nil, map[string]any{"patch": map[string]any{"audience": map[string]any{"type": "everyone", "principal_urns": []string{}, "confirm": true}}})
		require.ErrorContains(t, err, "external administrator")
		hidden, err := reads.GetPolicy(assistantCtx, assistant, GetRiskPolicyInput{ProjectSlug: project.Slug, PolicyID: id})
		require.NoError(t, err)
		require.Nil(t, hidden.Policy.Audience)
		args["policy_id"] = uuid.NewString()
		_, _, err = handlers.RemoveSelf(ctx, nil, args)
		requireRiskMutationRefusal(t, err, "not_found")
		require.Equal(t, version, read(t, id).Policy.Version)
	})
	t.Run("foreign role replacement refuses", func(t *testing.T) {
		foreign, _ := seedRegistrationLifecycle(t, ctx, conn)
		foreignRole := seedAccessRole(t, ctx, conn, foreign.OrganizationID, "foreign-role", "Foreign role")
		id, version := create(t, "targeted", []string{self.String(), other.String()})
		_, _, err := handlers.UpdatePolicy(ctx, nil, map[string]any{"project_slug": project.Slug, "policy_id": id, "expected_version": version, "idempotency_key": uuid.NewString(), "patch": map[string]any{"audience": map[string]any{"type": "targeted", "principal_urns": []string{foreignRole.String()}, "confirm": true}}})
		require.Error(t, err)
		require.Equal(t, version, read(t, id).Policy.Version)
	})
	t.Run("existing grant writer refuses immediately without mutation", func(t *testing.T) {
		id, version := create(t, "targeted", []string{self.String(), other.String()})
		writer, err := conn.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = writer.Rollback(ctx) }()
		require.NoError(t, authz.ReplaceGrantAudience(ctx, writer, authz.ResourceGrant{Resource: authz.Resource{OrganizationID: principal.OrganizationID, Scope: authz.ScopeRiskPolicyEvaluate, ResourceID: "*"}, Principals: []urn.Principal{self}, Selector: authz.NewSelector(authz.ScopeRiskPolicyEvaluate, "*")}))
		// A deadline bounds a regression that accidentally removes NOWAIT. A
		// conflict (not cancellation/unavailability) proves immediate lock refusal.
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_, _, err = handlers.RemoveSelf(bounded, nil, arguments(id, version))
		requireRiskMutationRefusal(t, err, "conflict")
		require.ErrorContains(t, err, "Audience grants are being changed")
		require.NoError(t, bounded.Err())
		require.Equal(t, version, read(t, id).Policy.Version)
		require.ElementsMatch(t, []string{self.String(), other.String()}, read(t, id).Policy.Audience.PrincipalURNs)
		require.NoError(t, writer.Rollback(ctx))
	})
	// This grant is deliberately added after obtaining the token: broad grants
	// are not in the exact audience version and must be checked live under lock.
	t.Run("fresh wildcard grant refuses without version change", func(t *testing.T) {
		id, version := create(t, "targeted", []string{self.String(), other.String()})
		require.NoError(t, authz.ReplaceGrantAudience(ctx, conn, authz.ResourceGrant{Resource: authz.Resource{OrganizationID: principal.OrganizationID, Scope: authz.ScopeRiskPolicyEvaluate, ResourceID: "*"}, Principals: []urn.Principal{self}, Selector: authz.NewSelector(authz.ScopeRiskPolicyEvaluate, "*")}))
		require.Equal(t, version, read(t, id).Policy.Version)
		_, _, err := handlers.RemoveSelf(ctx, nil, arguments(id, version))
		require.ErrorContains(t, err, "broader evaluation")
		require.Equal(t, version, read(t, id).Policy.Version)
	})
}

func TestSelfRemovalAudienceLockSerializesGrantInsertion(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_self_removal_lock")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	self := urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID)
	other := urn.NewPrincipal(urn.PrincipalTypeUser, "other-user")
	policyID := uuid.NewString()
	resource := authz.Resource{OrganizationID: principal.OrganizationID, Scope: authz.ScopeRiskPolicyEvaluate, ResourceID: policyID}
	require.NoError(t, authz.ReplaceGrantAudience(ctx, conn, authz.ResourceGrant{Resource: resource, Principals: []urn.Principal{self, other}, Selector: authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)}))
	removal, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = removal.Rollback(ctx) }()
	require.NoError(t, lockSelfRemovalAudience(ctx, removal))
	require.NoError(t, refuseInheritedPolicyAudience(ctx, removal, principal.OrganizationID, policyID))

	writer, err := conn.Begin(ctx)
	require.NoError(t, err)
	writerPID := writer.Conn().PgConn().PID()
	done := make(chan error, 1)
	go func() {
		defer func() { _ = writer.Rollback(ctx) }()
		err := authz.ReplaceGrantAudience(ctx, writer, authz.ResourceGrant{Resource: authz.Resource{OrganizationID: principal.OrganizationID, Scope: authz.ScopeRiskPolicyEvaluate, ResourceID: "*"}, Principals: []urn.Principal{self}, Selector: authz.NewSelector(authz.ScopeRiskPolicyEvaluate, "*")})
		if err == nil {
			err = writer.Commit(ctx)
		}
		done <- err
	}()
	// Observe the actual database lock wait, not elapsed time or scheduling.
	require.Eventually(t, func() bool {
		var blocked bool
		err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND relation = 'principal_grants'::regclass AND mode = 'RowExclusiveLock' AND NOT granted)", writerPID).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("grant writer completed before self removal committed: %v", err)
	default:
	}
	require.NoError(t, authz.ReplaceGrantAudience(ctx, removal, authz.ResourceGrant{Resource: resource, Principals: []urn.Principal{other}, Selector: authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)}))
	require.NoError(t, refuseInheritedPolicyAudience(ctx, removal, principal.OrganizationID, policyID))
	require.NoError(t, removal.Commit(ctx))
	require.NoError(t, <-done)
	grants, err := authz.ListGrantsForResource(ctx, conn, resource)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, other.String(), grants[0].PrincipalUrn)
	// The writer is permitted after the removal transaction commits: this tool
	// changes the current positive audience, not a permanent exclusion.
	wildcard, err := authz.ListGrantsForResource(ctx, conn, authz.Resource{OrganizationID: principal.OrganizationID, Scope: authz.ScopeRiskPolicyEvaluate, ResourceID: "*"})
	require.NoError(t, err)
	require.Len(t, wildcard, 1)
	require.Equal(t, self.String(), wildcard[0].PrincipalUrn)
}
