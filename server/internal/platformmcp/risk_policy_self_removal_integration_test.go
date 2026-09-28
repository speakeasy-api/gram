package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/feature"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest,tparallel // Subtests share one database and mutation fixtures.
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
	t.Run("self removal refuses without mutation", func(t *testing.T) {
		id, version := create(t, "targeted", []string{self.String(), other.String()})
		args := arguments(id, version)
		for range 2 {
			_, output, err := handlers.RemoveSelf(ctx, nil, args)
			requireRiskMutationRefusal(t, err, unavailableCode)
			require.Zero(t, output)
			require.Equal(t, version, read(t, id).Policy.Version)
		}
	})
	t.Run("general update refuses cross project policy", func(t *testing.T) {
		id, version := create(t, "targeted", []string{self.String(), other.String()})
		params := projectsrepo.CreateProjectParams{
			Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: principal.OrganizationID,
		}
		otherProject, err := projectsrepo.New(conn).CreateProject(ctx, params)
		require.NoError(t, err)
		seedRegistrationEligibleCohort(t, ctx, conn, otherProject.ID)
		_, _, err = handlers.UpdatePolicy(ctx, nil, map[string]any{"project_slug": otherProject.Slug, "policy_id": id, "expected_version": version, "idempotency_key": uuid.NewString(), "patch": map[string]any{"audience": map[string]any{"type": "targeted", "principal_urns": []string{other.String()}, "confirm": true}}})
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
}
