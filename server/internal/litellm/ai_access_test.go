package litellm

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/delegation"
	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/litellmacting"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

type recordingLiteLLMEvaluator struct {
	result   killswitches.EvaluationResult
	requests []killswitches.EvaluationRequest
}

type failingDeriveResourceAdapter struct {
	killswitches.ResourceAdapter
	err error
}

func (a failingDeriveResourceAdapter) Derive(context.Context, killswitches.OrganizationID, any) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, a.err
}

func (e *recordingLiteLLMEvaluator) Evaluate(_ context.Context, request killswitches.EvaluationRequest) killswitches.EvaluationResult {
	e.requests = append(e.requests, request)
	return e.result
}

//nolint:containedctx // Test fixture owns the transaction-scoped context returned by newRealTestService.
type checkpointFixture struct {
	ctx        context.Context
	instance   *realTestInstance
	checkpoint *LiteLLMAIAccessCheckpoint
	evaluator  *recordingLiteLLMEvaluator
	auth       *contextvalues.AuthContext
	payload    *gen.IngestPayload
	instanceID string
	signer     *litellmacting.Signer
	userID     string
}

func cloneCheckpointPayload(payload *gen.IngestPayload) gen.IngestPayload {
	clone := *payload
	if payload.RequestData != nil {
		requestData := *payload.RequestData
		clone.RequestData = &requestData
	}
	clone.RequestHeaders = maps.Clone(payload.RequestHeaders)
	return clone
}

func newCheckpointFixture(t *testing.T, result killswitches.EvaluationResult) checkpointFixture {
	t.Helper()
	ctx, ti := newRealTestService(t, nil)
	sessionAuth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, sessionAuth.ProjectID)
	created, err := ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "ai-access-checkpoint", FailurePosture: "fail_closed"})
	require.NoError(t, err)
	hash, err := auth.GetAPIKeyHash(created.Key)
	require.NoError(t, err)
	key, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, hash)
	require.NoError(t, err)

	signer, err := litellmacting.NewSigner("litellm-checkpoint-test-secret")
	require.NoError(t, err)
	registry, err := mcptoolexecution.NewRegistry(ti.conn)
	require.NoError(t, err)
	evaluator := &recordingLiteLLMEvaluator{result: result, requests: nil}
	checkpoint, err := NewLiteLLMAIAccessCheckpoint(registry, evaluator, signer)
	require.NoError(t, err)
	invocationID, err := uuid.NewV7()
	require.NoError(t, err)
	assertion, err := signer.MintAssertion(sessionAuth.UserID, litellmacting.AssertionBinding{
		OrganizationID: sessionAuth.ActiveOrganizationID, ProjectID: sessionAuth.ProjectID.String(),
		InstanceID: created.Instance.ID, APIKeyID: key.ID.String(), InvocationID: invocationID.String(),
	})
	require.NoError(t, err)
	callbackAuth := *sessionAuth
	callbackAuth.UserID = "integration-key-creator-must-not-be-actor"
	callbackAuth.Email = new("creator@example.test")
	callbackAuth.APIKeyID = key.ID.String()
	callbackAuth.APIKeyName = key.Name
	callbackAuth.APIKeyScopes = []string{auth.APIKeyScopeHooks.String()}
	payload := testPayload()
	payload.RequestHeaders = map[string]string{
		actingPrincipalHeader: assertion, actingPrincipalContractHeader: litellmacting.ContractVersion, inferenceInvocationHeader: invocationID.String(),
	}
	return checkpointFixture{ctx: ctx, instance: ti, checkpoint: checkpoint, evaluator: evaluator, auth: &callbackAuth, payload: payload, instanceID: created.Instance.ID, signer: signer, userID: sessionAuth.UserID}
}

func TestLiteLLMAIAccessEvaluatesEveryRetryAndReturnsSelectedNote(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	fixture := newCheckpointFixture(t, noMatch)

	first := fixture.checkpoint.Evaluate(fixture.ctx, fixture.payload, fixture.auth)
	require.False(t, first.blocked)
	require.NotEqual(t, fixture.auth.UserID, first.userID)
	require.Len(t, fixture.evaluator.requests, 1)
	request := fixture.evaluator.requests[0]
	require.Equal(t, mcptoolexecution.ResourceKindLiteLLMInstance, request.ResourceKind)
	require.Equal(t, killswitches.ResourceKey(fixture.instanceID), request.ResourceKey)
	require.Equal(t, []killswitches.DefinitionKey{mcptoolexecution.DefinitionKeyAIAccess}, request.DefinitionKeys)
	require.Len(t, request.PrincipalCandidates, 1)
	require.Equal(t, mcptoolexecution.PrincipalKindUser, request.PrincipalCandidates[0].Kind)

	matched, err := killswitches.NewMatchResult(killswitches.PrescriptionID(uuid.NewString()), "Selected external note.")
	require.NoError(t, err)
	fixture.evaluator.result = matched
	second := fixture.checkpoint.Evaluate(fixture.ctx, fixture.payload, fixture.auth)
	require.True(t, second.blocked)
	require.Equal(t, "ai_access_denied", second.reason)
	require.Equal(t, "Selected external note.", second.message)
	require.Len(t, fixture.evaluator.requests, 2, "retry must revalidate and reevaluate instead of using an allow cache")
}

//nolint:paralleltest,tparallel // Subtests share the recording evaluator to assert every content shape was checked.
func TestLiteLLMAIAccessRunsBeforeTextImageAndToolExtraction(t *testing.T) {
	t.Parallel()
	matched, err := killswitches.NewMatchResult(killswitches.PrescriptionID(uuid.NewString()), "Selected external note.")
	require.NoError(t, err)
	fixture := newCheckpointFixture(t, matched)
	fixture.instance.service.aiAccess = fixture.checkpoint
	ctx := contextvalues.SetAuthContext(fixture.ctx, fixture.auth)

	cases := map[string]func(*gen.IngestPayload){
		"textless":   func(payload *gen.IngestPayload) {},
		"image only": func(payload *gen.IngestPayload) { payload.Images = []string{"image"} },
		"tool only":  func(payload *gen.IngestPayload) { payload.Tools = []any{map[string]any{"name": "fixture-tool"}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			payloadCopy := cloneCheckpointPayload(fixture.payload)
			payloadCopy.StructuredMessages = nil
			payloadCopy.Texts = nil
			payloadCopy.Images = nil
			payloadCopy.Tools = nil
			callID := "ai-access-content-" + uuid.NewString()
			payloadCopy.LitellmCallID = &callID
			mutate(&payloadCopy)

			result, ingestErr := fixture.instance.service.Ingest(ctx, &payloadCopy)
			require.NoError(t, ingestErr)
			require.Equal(t, gen.LiteLLMGuardrailAction("BLOCKED"), result.Action)
			require.Equal(t, "Selected external note.", *result.BlockedReason)
		})
	}
	require.Len(t, fixture.evaluator.requests, len(cases))
}

//nolint:paralleltest,tparallel // Subtests intentionally share a stateful evaluator to assert that no requests occurred.
func TestLiteLLMAIAccessRejectsUntrustedAndMismatchedIdentity(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	fixture := newCheckpointFixture(t, noMatch)

	cases := map[string]func(*gen.IngestPayload, *contextvalues.AuthContext){
		"missing assertion excludes API-key creator": func(payload *gen.IngestPayload, _ *contextvalues.AuthContext) { payload.RequestHeaders = nil },
		"malformed assertion": func(payload *gen.IngestPayload, _ *contextvalues.AuthContext) {
			payload.RequestHeaders[actingPrincipalHeader] = "not-a-jwt"
		},
		"spoofed LiteLLM identity metadata": func(payload *gen.IngestPayload, _ *contextvalues.AuthContext) {
			payload.RequestData.UserAPIKeyUserEmail = new("spoofed@example.test")
			payload.RequestData.UserAPIKeyUserID = new("spoofed-user")
			payload.RequestHeaders = nil
		},
		"wrong contract": func(payload *gen.IngestPayload, _ *contextvalues.AuthContext) {
			payload.RequestHeaders[actingPrincipalContractHeader] = "other.v1"
		},
		"wrong invocation": func(payload *gen.IngestPayload, _ *contextvalues.AuthContext) {
			id, idErr := uuid.NewV7()
			require.NoError(t, idErr)
			payload.RequestHeaders[inferenceInvocationHeader] = id.String()
		},
		"duplicate case-insensitive header": func(payload *gen.IngestPayload, _ *contextvalues.AuthContext) {
			payload.RequestHeaders["X-Gram-Acting-Principal"] = payload.RequestHeaders[actingPrincipalHeader]
		},
		"wrong project": func(_ *gen.IngestPayload, authCtx *contextvalues.AuthContext) {
			projectID := uuid.New()
			authCtx.ProjectID = &projectID
		},
		"cross tenant": func(_ *gen.IngestPayload, authCtx *contextvalues.AuthContext) {
			authCtx.ActiveOrganizationID = "org_other"
		},
		"unmanaged key": func(_ *gen.IngestPayload, authCtx *contextvalues.AuthContext) { authCtx.APIKeyID = uuid.NewString() },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			payloadCopy := cloneCheckpointPayload(fixture.payload)
			authCopy := *fixture.auth
			mutate(&payloadCopy, &authCopy)
			decision := fixture.checkpoint.Evaluate(fixture.ctx, &payloadCopy, &authCopy)
			require.True(t, decision.blocked)
			require.Equal(t, "ai_access_identity_unavailable", decision.reason)
			require.Equal(t, liteLLMIdentityFailureMessage, decision.message)
			require.NotEqual(t, "Selected external note.", decision.message)
		})
	}
	require.Empty(t, fixture.evaluator.requests)
}

func TestLiteLLMLifecycleAcceptsCurrentSelectedInstance(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	fixture := newCheckpointFixture(t, noMatch)
	registry, err := mcptoolexecution.NewRegistry(fixture.instance.conn)
	require.NoError(t, err)
	lifecycle, err := killswitches.NewLifecycleService(fixture.instance.conn, registry, mcptoolexecution.NewCustomerLifecycleValidator(), nil)
	require.NoError(t, err)
	operationID := uuid.New()
	request := killswitches.ActivatePrescriptionRequest{
		MutationContext: killswitches.MutationContext{
			OrganizationID: killswitches.OrganizationID(fixture.auth.ActiveOrganizationID),
			ActorUserID:    fixture.userID, ActorDisplayName: fixture.userID, OperationID: operationID,
		},
		Definition: mcptoolexecution.DefinitionKeyAIAccess, PrincipalKind: mcptoolexecution.PrincipalKindUser, PrincipalInput: fixture.userID,
		ResourceKind: mcptoolexecution.ResourceKindLiteLLMInstance,
		Desired: killswitches.DesiredVersionInput{
			ResourceScope: killswitches.ResourceScopeSelected, SelectedResourceInputs: []string{" " + strings.ToUpper(fixture.instanceID) + " "},
			StartMode: killswitches.StartModeNow, InternalNote: "selected LiteLLM instance", ExternalNote: "Access paused.",
		},
	}
	activated, err := lifecycle.ActivatePrescription(fixture.ctx, request)
	require.NoError(t, err)
	require.Equal(t, int64(1), activated.Version)

	replayed, err := lifecycle.ActivatePrescription(fixture.ctx, request)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, activated.PrescriptionID, replayed.PrescriptionID)

	deactivated, err := lifecycle.DeactivatePrescription(fixture.ctx, killswitches.DeactivatePrescriptionRequest{
		MutationContext: killswitches.MutationContext{
			OrganizationID: killswitches.OrganizationID(fixture.auth.ActiveOrganizationID),
			ActorUserID:    fixture.userID, ActorDisplayName: fixture.userID, OperationID: uuid.New(),
		},
		PrescriptionID: activated.PrescriptionID, ExpectedVersion: activated.Version,
	})
	require.NoError(t, err)
	require.NoError(t, fixture.instance.service.RevokeInstance(fixture.ctx, &gen.RevokeInstancePayload{ID: fixture.instanceID}))
	_, err = lifecycle.ReactivatePrescription(fixture.ctx, killswitches.ReactivatePrescriptionRequest{
		MutationContext: killswitches.MutationContext{
			OrganizationID: killswitches.OrganizationID(fixture.auth.ActiveOrganizationID),
			ActorUserID:    fixture.userID, ActorDisplayName: fixture.userID, OperationID: uuid.New(),
		},
		PrescriptionID: activated.PrescriptionID, ExpectedVersion: deactivated.Version, Desired: request.Desired,
	})
	require.ErrorIs(t, err, killswitches.ErrInvalidReference)
}

func TestLiteLLMInstanceResourceValidationRequiresCurrentOwnership(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	fixture := newCheckpointFixture(t, noMatch)
	adapter := mcptoolexecution.NewLiteLLMInstanceResourceAdapter(fixture.instance.conn)
	key := killswitches.ResourceKey(fixture.instanceID)

	current, err := adapter.ValidateCurrentOrganization(fixture.ctx, killswitches.OrganizationID(fixture.auth.ActiveOrganizationID), key)
	require.NoError(t, err)
	require.True(t, current)
	current, err = adapter.ValidateCurrentOrganization(fixture.ctx, killswitches.OrganizationID("org_other"), key)
	require.NoError(t, err)
	require.False(t, current)
	current, err = adapter.ValidateCurrentOrganization(fixture.ctx, killswitches.OrganizationID(fixture.auth.ActiveOrganizationID), killswitches.ResourceKey(uuid.NewString()))
	require.NoError(t, err)
	require.False(t, current)

	require.NoError(t, fixture.instance.service.RevokeInstance(fixture.ctx, &gen.RevokeInstancePayload{ID: fixture.instanceID}))
	current, err = adapter.ValidateCurrentOrganization(fixture.ctx, killswitches.OrganizationID(fixture.auth.ActiveOrganizationID), key)
	require.NoError(t, err)
	require.False(t, current)
}

func TestLiteLLMAIAccessClassifiesResourceLookupFailureAsEvaluatorUnavailable(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	fixture := newCheckpointFixture(t, noMatch)
	fixture.checkpoint.resource = failingDeriveResourceAdapter{ResourceAdapter: fixture.checkpoint.resource, err: errors.New("database unavailable")}

	decision := fixture.checkpoint.Evaluate(fixture.ctx, fixture.payload, fixture.auth)
	require.True(t, decision.blocked)
	require.Equal(t, "ai_access_evaluator_unavailable", decision.reason)
	require.Equal(t, delegation.EvaluatorFailureMessage, decision.message)
	require.Empty(t, fixture.evaluator.requests)
}

func TestLiteLLMAIAccessFailsClosedOnMembershipAndEvaluatorFailures(t *testing.T) {
	t.Parallel()
	failure, err := killswitches.NewInfrastructureFailureResult(errors.New("evaluator unavailable"))
	require.NoError(t, err)
	fixture := newCheckpointFixture(t, failure)
	decision := fixture.checkpoint.Evaluate(fixture.ctx, fixture.payload, fixture.auth)
	require.True(t, decision.blocked)
	require.Equal(t, "ai_access_evaluator_unavailable", decision.reason)
	require.NotEqual(t, "Selected external note.", decision.message)

	require.NoError(t, orgrepo.New(fixture.instance.conn).DeleteOrganizationUserRelationship(fixture.ctx, orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: fixture.auth.ActiveOrganizationID, UserID: conv.ToPGText(string(fixture.evaluator.requests[0].PrincipalCandidates[0].Key)),
	}))
	decision = fixture.checkpoint.Evaluate(fixture.ctx, fixture.payload, fixture.auth)
	require.True(t, decision.blocked)
	require.Equal(t, "ai_access_identity_unavailable", decision.reason)
	require.Len(t, fixture.evaluator.requests, 1, "removed membership must stop before evaluator")
}

func TestLiteLLMAIAccessRejectsRotatedAndRevokedKeys(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	fixture := newCheckpointFixture(t, noMatch)
	rotated, err := fixture.instance.service.RotateInstanceKey(fixture.ctx, &gen.RotateInstanceKeyPayload{ID: fixture.instanceID})
	require.NoError(t, err)
	decision := fixture.checkpoint.Evaluate(fixture.ctx, fixture.payload, fixture.auth)
	require.True(t, decision.blocked)
	require.Equal(t, "ai_access_identity_unavailable", decision.reason)
	require.Empty(t, fixture.evaluator.requests)

	rotatedHash, err := auth.GetAPIKeyHash(rotated.Key)
	require.NoError(t, err)
	rotatedKey, err := keysrepo.New(fixture.instance.conn).GetAPIKeyByKeyHash(fixture.ctx, rotatedHash)
	require.NoError(t, err)
	invocationID, err := uuid.NewV7()
	require.NoError(t, err)
	assertion, err := fixture.signer.MintAssertion(fixture.userID, litellmacting.AssertionBinding{
		OrganizationID: fixture.auth.ActiveOrganizationID, ProjectID: fixture.auth.ProjectID.String(),
		InstanceID: fixture.instanceID, APIKeyID: rotatedKey.ID.String(), InvocationID: invocationID.String(),
	})
	require.NoError(t, err)
	fixture.auth.APIKeyID = rotatedKey.ID.String()
	fixture.payload.RequestHeaders[actingPrincipalHeader] = assertion
	fixture.payload.RequestHeaders[inferenceInvocationHeader] = invocationID.String()
	require.NoError(t, fixture.instance.service.RevokeInstance(fixture.ctx, &gen.RevokeInstancePayload{ID: fixture.instanceID}))
	decision = fixture.checkpoint.Evaluate(fixture.ctx, fixture.payload, fixture.auth)
	require.True(t, decision.blocked)
	require.Equal(t, "ai_access_identity_unavailable", decision.reason)
	require.Empty(t, fixture.evaluator.requests)
}
