package mcptoolexecution

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestHostedCheckpoint_ReevaluatesAndFailsClosed verifies that each hosted call
// uses current prescription state and rejects evaluator failures.
func TestHostedCheckpoint_ReevaluatesAndFailsClosed(t *testing.T) {
	t.Parallel()

	conn, orgID := newTestDatabase(t, "ks_hosted_checkpoint")
	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, nil)
	insertMembership(t, conn, orgID, userID, nil)
	projectID := insertProject(t, conn, orgID, "hosted-checkpoint", nil)
	serverID := insertMCPServer(t, conn, orgID, projectID, nil)
	source := ServerSource{FrontingServerID: uuid.NullUUID{UUID: serverID, Valid: true}}
	recorder := &coverageRecorder{}
	checkpoint, err := NewHostedCheckpoint(conn, testenv.NewMeterProvider(t), nil, recorder)
	require.NoError(t, err)
	ctx := mcpidentity.WithIdentity(t.Context(), mcpidentity.AuthenticatedUser(userID))

	disposition, err := checkpoint.Evaluate(ctx, orgID, source)
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())

	note := "Exact operator note."
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:           uuid.New(),
		PrincipalKey: userID,
		Scope:        "selected",
		Resources:    []string{serverID.String()},
		ExternalNote: note,
	})

	disposition, err = checkpoint.Evaluate(ctx, orgID, source)
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionMatchedDenial, disposition.Kind())
	gotNote, ok := disposition.ExternalNote()
	require.True(t, ok)
	require.Equal(t, note, gotNote)
	require.Len(t, recorder.observations, 2)
	for _, observation := range recorder.observations {
		require.Equal(t, coverageObservation{
			surface:  mcpmetrics.KillswitchSurfaceHosted,
			identity: mcpmetrics.KillswitchIdentityActiveUser,
			resource: mcpmetrics.KillswitchResourceCanonicalServer,
		}, observation)
	}

	_, err = conn.Exec(t.Context(), "DROP TABLE killswitch_prescriptions CASCADE") //nolint:glint // notestingrawsql: deterministic DDL breakage in this test's isolated database forces an evaluator failure
	require.NoError(t, err)

	disposition, err = checkpoint.Evaluate(ctx, orgID, source)
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
	_, hasNote := disposition.ExternalNote()
	require.False(t, hasNote)

	unsupportedCtx := mcpidentity.WithIdentity(t.Context(), mcpidentity.Identity{Kind: mcpidentity.KindAnonymous})
	disposition, err = checkpoint.Evaluate(unsupportedCtx, orgID, source)
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())
}

// TestHostedCheckpoint_DerivationErrorsTakePrecedenceOverUnsupportedInputs verifies
// that derivation failures reject calls even when another input is unsupported.
func TestHostedCheckpoint_DerivationErrorsTakePrecedenceOverUnsupportedInputs(t *testing.T) {
	t.Parallel()

	conn, orgID := newTestDatabase(t, "ks_hosted_checkpoint_derivation_errors")
	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, nil)
	insertMembership(t, conn, orgID, userID, nil)
	projectID := insertProject(t, conn, orgID, "hosted-checkpoint-errors", nil)
	serverID := insertMCPServer(t, conn, orgID, projectID, nil)
	serverSource := ServerSource{FrontingServerID: uuid.NullUUID{UUID: serverID, Valid: true}}
	recorder := &coverageRecorder{}
	checkpoint, err := NewHostedCheckpoint(conn, testenv.NewMeterProvider(t), nil, recorder)
	require.NoError(t, err)

	unsupportedIdentityCtx := mcpidentity.WithIdentity(t.Context(), mcpidentity.Identity{Kind: mcpidentity.KindAnonymous})
	disposition, err := checkpoint.Evaluate(unsupportedIdentityCtx, orgID, serverSource)
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())

	unsupportedResourceCtx := mcpidentity.WithIdentity(t.Context(), mcpidentity.AuthenticatedUser(userID))
	disposition, err = checkpoint.Evaluate(unsupportedResourceCtx, orgID, ServerSource{})
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())

	resourceFailureCtx, cancelResourceFailure := context.WithCancel(unsupportedIdentityCtx)
	cancelResourceFailure()
	disposition, err = checkpoint.Evaluate(resourceFailureCtx, orgID, serverSource)
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())

	principalFailureCtx, cancelPrincipalFailure := context.WithCancel(unsupportedResourceCtx)
	cancelPrincipalFailure()
	disposition, err = checkpoint.Evaluate(principalFailureCtx, orgID, ServerSource{})
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())

	require.Equal(t, []coverageObservation{
		{surface: mcpmetrics.KillswitchSurfaceHosted, identity: mcpmetrics.KillswitchIdentityAnonymous, resource: mcpmetrics.KillswitchResourceCanonicalServer},
		{surface: mcpmetrics.KillswitchSurfaceHosted, identity: mcpmetrics.KillswitchIdentityActiveUser, resource: mcpmetrics.KillswitchResourceLegacyNoServer},
		{surface: mcpmetrics.KillswitchSurfaceHosted, identity: mcpmetrics.KillswitchIdentityAnonymous, resource: mcpmetrics.KillswitchResourceUnavailable},
		{surface: mcpmetrics.KillswitchSurfaceHosted, identity: mcpmetrics.KillswitchIdentityUnavailable, resource: mcpmetrics.KillswitchResourceLegacyNoServer},
	}, recorder.observations)
}

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
