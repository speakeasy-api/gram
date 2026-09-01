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

func TestPrivateCheckpointDualDefinitionMatrixAndNextCall(t *testing.T) {
	t.Parallel()

	conn, orgID := newTestDatabase(t, "ks_mcp_dual_matrix")
	registry, err := NewRegistry(conn)
	require.NoError(t, err)
	realEvaluator, err := killswitches.NewEvaluator(conn, registry, time.Second, nil, testenv.NewLogger(t))
	require.NoError(t, err)
	counted := &countingEvaluator{delegate: realEvaluator}
	checkpoint, err := newCheckpoint(registry, counted, time.Second, enforcedRollout(orgID))
	require.NoError(t, err)

	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, nil)
	insertMembership(t, conn, orgID, userID, nil)
	projectID := insertProject(t, conn, orgID, "dual-matrix", nil)
	serverID := insertMCPServer(t, conn, orgID, projectID, nil)
	ctx := testIdentityContext(t, mcpidentity.KindUserSession, userID)

	evaluate := func() killswitches.TransportDisposition {
		t.Helper()
		disposition, evalErr := checkpoint.Evaluate(ctx, orgID, serverID.String())
		require.NoError(t, evalErr)
		return disposition
	}
	requireNote := func(want string) {
		t.Helper()
		disposition := evaluate()
		require.Equal(t, killswitches.TransportDispositionMatchedDenial, disposition.Kind())
		note, ok := disposition.ExternalNote()
		require.True(t, ok)
		require.Equal(t, want, note)
	}

	// No match is not cached: the AI-only prescription is authoritative on the
	// immediately following call.
	require.Equal(t, killswitches.TransportDispositionContinue, evaluate().Kind())
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:            uuid.New(),
		DefinitionKey: DefinitionKeyAIAccess,
		PrincipalKey:  userID,
		Scope:         "selected",
		Resources:     []string{serverID.String()},
		ExternalNote:  "AI-only note.",
	})
	requireNote("AI-only note.")

	clearPrescriptions(t, conn, orgID)
	mcpID := uuid.New()
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:           mcpID,
		PrincipalKey: userID,
		Scope:        "all",
		ExternalNote: "MCP-specific note.",
	})
	requireNote("MCP-specific note.")

	aiID := uuid.New()
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:            aiID,
		DefinitionKey: DefinitionKeyAIAccess,
		PrincipalKey:  userID,
		Scope:         "selected",
		Resources:     []string{serverID.String()},
		ExternalNote:  "Newer selected AI note.",
	})
	// The MCP match was activated first and is less specific than the newer,
	// selected AI match. Definition candidate order must still choose MCP.
	requireNote("MCP-specific note.")

	deletePrescription(t, conn, orgID, mcpID)
	requireNote("Newer selected AI note.")
	require.Equal(t, 5, counted.calls)
	for _, request := range counted.requests {
		require.Equal(t, []killswitches.DefinitionKey{DefinitionKeyMCPToolExecution, DefinitionKeyAIAccess}, request.DefinitionKeys)
	}
}

func TestAIAccessLifecyclePersistsExactMCPIdentity(t *testing.T) {
	t.Parallel()

	conn, orgID := newTestDatabase(t, "ks_ai_access_lifecycle")
	registry, err := NewRegistry(conn)
	require.NoError(t, err)
	lifecycle, err := killswitches.NewLifecycleService(conn, registry, NewCustomerLifecycleValidator(), nil)
	require.NoError(t, err)
	facade, err := killswitches.NewFacade(lifecycle)
	require.NoError(t, err)

	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, nil)
	insertMembership(t, conn, orgID, userID, nil)
	projectID := insertProject(t, conn, orgID, "ai-lifecycle", nil)
	serverID := insertMCPServer(t, conn, orgID, projectID, nil)

	activated, err := lifecycle.ActivatePrescription(t.Context(), killswitches.ActivatePrescriptionRequest{
		MutationContext: killswitches.MutationContext{
			OrganizationID:   killswitches.OrganizationID(orgID),
			ActorUserID:      userID,
			ActorDisplayName: "Test operator",
			OperationID:      uuid.New(),
		},
		Definition:     DefinitionKeyAIAccess,
		PrincipalKind:  PrincipalKindUser,
		PrincipalInput: userID,
		ResourceKind:   ResourceKindMCPServer,
		Desired: killswitches.DesiredVersionInput{
			ResourceScope:          killswitches.ResourceScopeSelected,
			SelectedResourceInputs: []string{serverID.String()},
			StartMode:              killswitches.StartModeNow,
			InternalNote:           "test incident context",
			ExternalNote:           "AI access paused.",
		},
	})
	require.NoError(t, err)

	persisted, err := facade.GetPrescription(t.Context(), killswitches.GetPrescriptionRequest{
		OrganizationID: killswitches.OrganizationID(orgID),
		PrescriptionID: activated.PrescriptionID,
	})
	require.NoError(t, err)
	require.Equal(t, DefinitionKeyAIAccess, persisted.Definition)
	require.Equal(t, PrincipalKindUser, persisted.PrincipalKind)
	require.Equal(t, killswitches.PrincipalKey(userID), persisted.PrincipalKey)
	require.Equal(t, ResourceKindMCPServer, persisted.ResourceKind)
	require.Equal(t, []killswitches.ResourceKey{killswitches.ResourceKey(serverID.String())}, persisted.SelectedResourceKeys)
}

func TestEvaluatorHasNoImplicitCapabilityHierarchy(t *testing.T) {
	t.Parallel()

	conn, orgID := newTestDatabase(t, "ks_mcp_no_hierarchy")
	registry, err := NewRegistry(conn)
	require.NoError(t, err)
	evaluator, err := killswitches.NewEvaluator(conn, registry, time.Second, nil, testenv.NewLogger(t))
	require.NoError(t, err)

	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, nil)
	insertMembership(t, conn, orgID, userID, nil)
	projectID := insertProject(t, conn, orgID, "no-hierarchy", nil)
	serverID := insertMCPServer(t, conn, orgID, projectID, nil)
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:            uuid.New(),
		DefinitionKey: DefinitionKeyAIAccess,
		PrincipalKey:  userID,
		Scope:         "all",
		ExternalNote:  "AI only.",
	})

	request := killswitches.EvaluationRequest{
		OrganizationID:      killswitches.OrganizationID(orgID),
		DefinitionKeys:      []killswitches.DefinitionKey{DefinitionKeyMCPToolExecution},
		PrincipalCandidates: []killswitches.PrincipalCandidate{{Kind: PrincipalKindUser, Key: killswitches.PrincipalKey(userID)}},
		ResourceKind:        ResourceKindMCPServer,
		ResourceKey:         killswitches.ResourceKey(serverID.String()),
	}
	require.Equal(t, killswitches.EvaluationResultNoMatch, evaluator.Evaluate(t.Context(), request).Kind())
	request.DefinitionKeys = []killswitches.DefinitionKey{DefinitionKeyAIAccess}
	require.Equal(t, killswitches.EvaluationResultMatch, evaluator.Evaluate(t.Context(), request).Kind())

	clearPrescriptions(t, conn, orgID)
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:           uuid.New(),
		PrincipalKey: userID,
		Scope:        "all",
		ExternalNote: "MCP only.",
	})
	require.Equal(t, killswitches.EvaluationResultNoMatch, evaluator.Evaluate(t.Context(), request).Kind())
	request.DefinitionKeys = []killswitches.DefinitionKey{DefinitionKeyMCPToolExecution}
	require.Equal(t, killswitches.EvaluationResultMatch, evaluator.Evaluate(t.Context(), request).Kind())
}
