package mcptoolexecution

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestMCPToolExecutionEvaluationAcrossProjects proves the end-to-end
// adapter-to-evaluator slice with real database state: authoritative
// candidates from provenance, canonical server keys from fronting IDs, and
// selected-one, selected-many (cross-project), and dynamic all-server
// matching — including a server created after the all-server activation,
// without materializing the organization's server list.
func TestMCPToolExecutionEvaluationAcrossProjects(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_mcp_evaluation")
	organization := killswitches.OrganizationID(orgID)

	registry, err := NewRegistry(conn)
	require.NoError(t, err)
	evaluator, err := killswitches.NewEvaluator(conn, registry, time.Second, nil, testenv.NewLogger(t))
	require.NoError(t, err)

	pausedUser := "user_" + uuid.NewString()
	insertUser(t, conn, pausedUser, nil)
	insertMembership(t, conn, orgID, pausedUser, nil)
	freeUser := "user_" + uuid.NewString()
	insertUser(t, conn, freeUser, nil)
	insertMembership(t, conn, orgID, freeUser, nil)

	projectOne := insertProject(t, conn, orgID, "proj-one", nil)
	projectTwo := insertProject(t, conn, orgID, "proj-two", nil)
	serverA := insertMCPServer(t, conn, orgID, projectOne, nil)
	serverB := insertMCPServer(t, conn, orgID, projectTwo, nil)

	principalAdapter, ok := registry.PrincipalAdapter(PrincipalKindUser)
	require.True(t, ok)
	resourceAdapter, ok := registry.ResourceAdapter(ResourceKindMCPServer)
	require.True(t, ok)

	evaluate := func(t *testing.T, userID string, serverID uuid.UUID) killswitches.EvaluationResult {
		t.Helper()
		candidates, err := principalAdapter.DeriveCandidates(t.Context(), organization, mcpidentity.AuthenticatedUser(userID))
		require.NoError(t, err)
		require.Equal(t, killswitches.PrincipalCandidateResultCandidates, candidates.Kind())
		resource, err := resourceAdapter.Derive(t.Context(), organization, ServerSource{FrontingServerID: uuid.NullUUID{UUID: serverID, Valid: true}})
		require.NoError(t, err)
		key, supported, err := resource.Key()
		require.NoError(t, err)
		require.True(t, supported)
		return evaluator.Evaluate(t.Context(), killswitches.EvaluationRequest{
			OrganizationID:      organization,
			DefinitionKeys:      []killswitches.DefinitionKey{DefinitionKeyMCPToolExecution},
			PrincipalCandidates: candidates.Candidates(),
			ResourceKind:        ResourceKindMCPServer,
			ResourceKey:         key,
		})
	}
	requireMatch := func(t *testing.T, result killswitches.EvaluationResult, note string) {
		t.Helper()
		require.Equal(t, killswitches.EvaluationResultMatch, result.Kind())
		gotNote, ok := result.ExternalNote()
		require.True(t, ok)
		require.Equal(t, note, gotNote)
	}
	requireNoMatch := func(t *testing.T, result killswitches.EvaluationResult) {
		t.Helper()
		require.Equal(t, killswitches.EvaluationResultNoMatch, result.Kind())
		reason, ok := result.NoMatchReason()
		require.True(t, ok)
		require.Equal(t, killswitches.NoMatchReasonNoPrescription, reason)
	}

	// No prescriptions yet: everything proceeds.
	requireNoMatch(t, evaluate(t, pausedUser, serverA))

	// Selected-one: only the selected server is blocked, only for the
	// prescribed user.
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:           uuid.New(),
		PrincipalKey: pausedUser,
		Scope:        "selected",
		Resources:    []string{serverA.String()},
		ExternalNote: "Server A paused.",
	})
	requireMatch(t, evaluate(t, pausedUser, serverA), "Server A paused.")
	requireNoMatch(t, evaluate(t, pausedUser, serverB))
	requireNoMatch(t, evaluate(t, freeUser, serverA))

	// Selected-many across projects: one prescription can select servers
	// living in different projects of the same organization.
	manyID := uuid.New()
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:           manyID,
		PrincipalKey: pausedUser,
		Scope:        "selected",
		Resources:    []string{serverA.String(), serverB.String()},
		ExternalNote: "Both servers paused.",
	})
	requireMatch(t, evaluate(t, pausedUser, serverB), "Both servers paused.")
	requireNoMatch(t, evaluate(t, freeUser, serverB))

	// Dynamic all-server scope covers a server created after activation: the
	// evaluator matches by scope, not by a materialized server list.
	clearPrescriptions(t, conn, orgID)

	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:           uuid.New(),
		PrincipalKey: pausedUser,
		Scope:        "all",
		Resources:    nil,
		ExternalNote: "All servers paused.",
	})
	requireMatch(t, evaluate(t, pausedUser, serverA), "All servers paused.")
	requireMatch(t, evaluate(t, pausedUser, serverB), "All servers paused.")

	projectThree := insertProject(t, conn, orgID, "proj-three", nil)
	serverC := insertMCPServer(t, conn, orgID, projectThree, nil)
	requireMatch(t, evaluate(t, pausedUser, serverC), "All servers paused.")
	requireNoMatch(t, evaluate(t, freeUser, serverC))
}
