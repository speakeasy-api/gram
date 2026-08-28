package mcptoolexecution

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type countingEvaluator struct {
	delegate evaluator
	calls    int
	requests []killswitches.EvaluationRequest
}

func (e *countingEvaluator) Evaluate(ctx context.Context, request killswitches.EvaluationRequest) killswitches.EvaluationResult {
	e.calls++
	e.requests = append(e.requests, request)
	return e.delegate.Evaluate(ctx, request)
}

type fixedEvaluator struct {
	result   killswitches.EvaluationResult
	calls    int
	requests []killswitches.EvaluationRequest
}

func (e *fixedEvaluator) Evaluate(_ context.Context, request killswitches.EvaluationRequest) killswitches.EvaluationResult {
	e.calls++
	e.requests = append(e.requests, request)
	return e.result
}

func enforcedRollout(organizationID string) *feature.InMemory {
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagMCPKillswitchEnforce, organizationID, true)
	return flags
}

func TestMCPEvaluationDefinitionCandidates(t *testing.T) {
	t.Parallel()
	require.Equal(t, []killswitches.DefinitionKey{DefinitionKeyMCPToolExecution, DefinitionKeyAIAccess}, mcpEvaluationDefinitionKeys())
}

func TestCheckpointEvaluatesEveryCoveredCallWithRealEvaluator(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_mcp_checkpoint")
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
	projectA := insertProject(t, conn, orgID, "checkpoint-a", nil)
	projectB := insertProject(t, conn, orgID, "checkpoint-b", nil)
	serverA := insertMCPServer(t, conn, orgID, projectA, nil)
	serverB := insertMCPServer(t, conn, orgID, projectB, nil)
	ctx := testIdentityContext(t, mcpidentity.KindUserSession, userID)

	evaluate := func(serverID uuid.UUID) killswitches.TransportDisposition {
		t.Helper()
		disposition, err := checkpoint.Evaluate(ctx, orgID, serverID.String())
		require.NoError(t, err)
		return disposition
	}
	requireMatch := func(disposition killswitches.TransportDisposition, note string) {
		t.Helper()
		require.Equal(t, killswitches.TransportDispositionMatchedDenial, disposition.Kind())
		got, ok := disposition.ExternalNote()
		require.True(t, ok)
		require.Equal(t, note, got)
	}

	// The first no-match is not cached. Activating a selected prescription is
	// authoritative on the immediately following call.
	require.Equal(t, killswitches.TransportDispositionContinue, evaluate(serverA).Kind())
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:           uuid.New(),
		PrincipalKey: userID,
		Scope:        "selected",
		Resources:    []string{serverA.String()},
		ExternalNote: "Selected server paused exactly.",
	})
	requireMatch(evaluate(serverA), "Selected server paused exactly.")
	require.Equal(t, killswitches.TransportDispositionContinue, evaluate(serverB).Kind())

	// Dynamic all scope covers both projects. A concurrent selected scope wins
	// note selection without any ordering logic in the checkpoint.
	clearPrescriptions(t, conn, orgID)
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:           uuid.New(),
		PrincipalKey: userID,
		Scope:        "all",
		ExternalNote: "All servers paused.",
	})
	requireMatch(evaluate(serverA), "All servers paused.")
	requireMatch(evaluate(serverB), "All servers paused.")
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID:           uuid.New(),
		PrincipalKey: userID,
		Scope:        "selected",
		Resources:    []string{serverA.String()},
		ExternalNote: "Selected wins.",
	})
	requireMatch(evaluate(serverA), "Selected wins.")
	require.Equal(t, 6, counted.calls)
}

func TestCheckpointPreservesUnsupportedIdentityAndFailsClosedOnCoverageFailure(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_mcp_checkpoint_failures")
	registry, err := NewRegistry(conn)
	require.NoError(t, err)
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	evaluation := &fixedEvaluator{result: noMatch}
	checkpoint, err := newCheckpoint(registry, evaluation, time.Second, enforcedRollout(orgID))
	require.NoError(t, err)

	projectID := insertProject(t, conn, orgID, "unsupported-identity", nil)
	serverID := insertMCPServer(t, conn, orgID, projectID, nil)

	// API-key provenance is deliberately unsupported and never becomes a
	// concrete user when the covered route has its canonical resource.
	apiKeyCtx := testIdentityContext(t, mcpidentity.KindAPIKey, "")
	disposition, err := checkpoint.Evaluate(apiKeyCtx, orgID, serverID.String())
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())
	require.Zero(t, evaluation.calls)

	// Missing canonical resources are coverage failures even when identity is
	// unsupported; a private serving path cannot use that to bypass checks.
	disposition, err = checkpoint.Evaluate(apiKeyCtx, orgID, "")
	require.Error(t, err)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
	require.Zero(t, evaluation.calls)

	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, nil)
	insertMembership(t, conn, orgID, userID, nil)
	userCtx := testIdentityContext(t, mcpidentity.KindUserSession, userID)
	disposition, err = checkpoint.Evaluate(userCtx, orgID, "")
	require.Error(t, err)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
	require.Zero(t, evaluation.calls)

	otherOrgID := "org_" + uuid.NewString()
	insertOrganization(t, conn, otherOrgID)
	otherProject := insertProject(t, conn, otherOrgID, "other-org", nil)
	otherServer := insertMCPServer(t, conn, otherOrgID, otherProject, nil)
	disposition, err = checkpoint.Evaluate(userCtx, orgID, otherServer.String())
	require.Error(t, err)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
	require.Zero(t, evaluation.calls)
}

type blockingPrincipalAdapter struct{}

func (blockingPrincipalAdapter) Kind() killswitches.PrincipalKind { return PrincipalKindUser }

func (blockingPrincipalAdapter) Canonicalize(killswitches.OrganizationID, string) (killswitches.CanonicalizationResult[killswitches.PrincipalKey], error) {
	return killswitches.CanonicalizationResult[killswitches.PrincipalKey]{}, errors.New("not used")
}

func (blockingPrincipalAdapter) ValidateCurrentOrganization(context.Context, killswitches.OrganizationID, killswitches.PrincipalKey) (bool, error) {
	return false, errors.New("not used")
}

func (blockingPrincipalAdapter) DeriveCandidates(ctx context.Context, _ killswitches.OrganizationID, _ any) (killswitches.PrincipalCandidateResult, error) {
	<-ctx.Done()
	return killswitches.PrincipalCandidateResult{}, fmt.Errorf("wait for principal derivation: %w", ctx.Err())
}

type blockingResourceAdapter struct{}

func (blockingResourceAdapter) Kind() killswitches.ResourceKind { return ResourceKindMCPServer }

func (blockingResourceAdapter) Canonicalize(killswitches.OrganizationID, string) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, errors.New("not used")
}

func (blockingResourceAdapter) ValidateCurrentOrganization(context.Context, killswitches.OrganizationID, killswitches.ResourceKey) (bool, error) {
	return false, errors.New("not used")
}

func (blockingResourceAdapter) Derive(ctx context.Context, _ killswitches.OrganizationID, _ any) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	<-ctx.Done()
	return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("wait for resource derivation: %w", ctx.Err())
}

func TestCheckpointBoundsAllIdentityAndResourceResolution(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	checkpoint := &Checkpoint{
		principal:     blockingPrincipalAdapter{},
		resource:      blockingResourceAdapter{},
		evaluator:     &fixedEvaluator{result: noMatch},
		transport:     killswitches.ResolveTransportDisposition,
		failurePolicy: killswitches.FailurePolicyFailClosed,
		timeout:       20 * time.Millisecond,
		flags:         enforcedRollout("org_example"),
	}
	ctx := testIdentityContext(t, mcpidentity.KindUserSession, "user_example")
	started := time.Now()
	disposition, err := checkpoint.Evaluate(ctx, "org_example", uuid.NewString())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
	require.Less(t, time.Since(started), time.Second)
}

func TestCheckpointReturnsEvaluatorInfrastructureFailureWithoutMatch(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_mcp_checkpoint_evaluator_failure")
	registry, err := NewRegistry(conn)
	require.NoError(t, err)
	cause := errors.New("database unavailable")
	failure, err := killswitches.NewInfrastructureFailureResult(cause)
	require.NoError(t, err)
	evaluation := &fixedEvaluator{result: failure}
	checkpoint, err := newCheckpoint(registry, evaluation, time.Second, enforcedRollout(orgID))
	require.NoError(t, err)

	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, nil)
	insertMembership(t, conn, orgID, userID, nil)
	projectID := insertProject(t, conn, orgID, "evaluator-failure", nil)
	serverID := insertMCPServer(t, conn, orgID, projectID, nil)
	ctx := testIdentityContext(t, mcpidentity.KindUserSession, userID)

	disposition, err := checkpoint.Evaluate(ctx, orgID, serverID.String())
	require.ErrorIs(t, err, cause)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
	_, hasNote := disposition.ExternalNote()
	require.False(t, hasNote)
	require.Equal(t, 1, evaluation.calls)
	require.Len(t, evaluation.requests, 1)
	require.Equal(t, []killswitches.DefinitionKey{DefinitionKeyMCPToolExecution, DefinitionKeyAIAccess}, evaluation.requests[0].DefinitionKeys)
}
