package hooks

import (
	"context"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/delegation"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooksacting"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
)

type failingDenialReadCache struct{ cache.Cache }

func (c failingDenialReadCache) Get(ctx context.Context, key string, value any) error {
	if strings.HasPrefix(key, "hook:ai-access:denial:v1:") {
		return errors.New("denial cache unavailable")
	}
	if err := c.Cache.Get(ctx, key, value); err != nil {
		return fmt.Errorf("get fallback cache entry: %w", err)
	}
	return nil
}

type blockingHookEvaluator struct {
	result  killswitches.EvaluationResult
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (e *blockingHookEvaluator) Evaluate(_ context.Context, _ killswitches.EvaluationRequest) killswitches.EvaluationResult {
	if e.calls.Add(1) == 1 {
		close(e.started)
	}
	<-e.release
	return e.result
}

type recordingHookEvaluator struct {
	result   killswitches.EvaluationResult
	requests []killswitches.EvaluationRequest
}

func (e *recordingHookEvaluator) Evaluate(_ context.Context, request killswitches.EvaluationRequest) killswitches.EvaluationResult {
	e.requests = append(e.requests, request)
	return e.result
}

func setupHookAIAccess(t *testing.T, result killswitches.EvaluationResult) (context.Context, *testInstance, *recordingHookEvaluator, *hooksacting.Signer, ed25519.PrivateKey) {
	t.Helper()
	ctx, ti := newTestHooksService(t)
	registry, err := mcptoolexecution.NewRegistry(ti.conn)
	require.NoError(t, err)
	evaluator := &recordingHookEvaluator{result: result}
	signer, err := hooksacting.NewSigner("test-hooks-acting-user-secret")
	require.NoError(t, err)
	checkpoint, err := NewHookAIAccessCheckpoint(registry, evaluator, signer)
	require.NoError(t, err)
	ti.service.aiAccess = checkpoint
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return ctx, ti, evaluator, signer, privateKey
}

func signedGovernedPayload(t *testing.T, ctx context.Context, signer *hooksacting.Signer, privateKey ed25519.PrivateKey, provider, event string) *gen.IngestPayload {
	t.Helper()
	return signedHookPayload(t, ctx, signer, privateKey, provider, event, false)
}

func signedHookPayload(t *testing.T, ctx context.Context, signer *hooksacting.Signer, privateKey ed25519.PrivateKey, provider, event string, observational bool) *gen.IngestPayload {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	idempotencyKey := uuid.NewString()
	sessionID := "session-" + uuid.NewString()
	eventType := "tool.requested"
	if event == delegation.EventUserPromptSubmit {
		eventType = "prompt.submitted"
	}
	payload := canonicalIngestPayload(provider, eventType, sessionID)
	payload.Source.RawEventName = &event
	payload.IdempotencyKey = &idempotencyKey
	payload.ActingUserContractVersion = new(delegation.ContractVersion)

	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	require.True(t, ok)
	refresh, err := signer.MintRefresh(authCtx.UserID, authCtx.ActiveOrganizationID, publicKey)
	require.NoError(t, err)
	identity, err := signer.VerifyRefresh(refresh)
	require.NoError(t, err)
	request := delegation.MintRequest{RefreshToken: refresh, ContractVersion: delegation.ContractVersion, Provider: provider, Event: event, SessionID: sessionID, IdempotencyKey: idempotencyKey, Observational: observational, SignedAt: time.Now().Unix(), Nonce: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	request.Signature, err = delegation.Sign(privateKey, request)
	require.NoError(t, err)
	assertion, err := signer.MintAssertion(identity, request)
	require.NoError(t, err)
	payload.ActingUserAssertion = &assertion
	return payload
}

func TestHookAIAccessApprovedNativeMatrixAndExactDenial(t *testing.T) {
	t.Parallel()
	match, err := killswitches.NewMatchResult(killswitches.PrescriptionID(uuid.NewString()), "Exact administrator note.")
	require.NoError(t, err)
	for _, provider := range []string{delegation.ProviderClaude, delegation.ProviderCodex} {
		for _, event := range []string{delegation.EventUserPromptSubmit, delegation.EventPreToolUse} {
			t.Run(provider+"/"+event, func(t *testing.T) {
				t.Parallel()
				ctx, ti, evaluator, signer, privateKey := setupHookAIAccess(t, match)
				payload := signedGovernedPayload(t, ctx, signer, privateKey, provider, event)
				result, err := ti.service.Ingest(ctx, payload)
				require.NoError(t, err)
				require.Equal(t, "deny", result.Decision)
				require.Equal(t, "ai_access_denied", *result.Reason)
				require.Equal(t, "Exact administrator note.", *result.Message)
				require.Len(t, evaluator.requests, 1)
				request := evaluator.requests[0]
				require.Equal(t, []killswitches.DefinitionKey{mcptoolexecution.DefinitionKeyAIAccess}, request.DefinitionKeys)
				require.Equal(t, mcptoolexecution.ResourceKindHookActivity, request.ResourceKind)
			})
		}
	}
}

func TestHookAIAccessIdentityAndEvaluatorFailuresAreNativeDenials(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	ctx, ti, evaluator, signer, privateKey := setupHookAIAccess(t, noMatch)
	payload := signedGovernedPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, delegation.EventPreToolUse)
	payload.ActingUserAssertion = nil
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
	require.Equal(t, "ai_access_identity_unavailable", *result.Reason)
	require.Equal(t, aiAccessIdentityFailureMessage, *result.Message)
	require.Empty(t, evaluator.requests, "missing identity never becomes an evaluator candidate")

	failure, err := killswitches.NewInfrastructureFailureResultWithPolicy(context.DeadlineExceeded, killswitches.FailurePolicyFailClosed, killswitches.InfrastructureFailureTimeout)
	require.NoError(t, err)
	ctx, ti, evaluator, signer, privateKey = setupHookAIAccess(t, failure)
	payload = signedGovernedPayload(t, ctx, signer, privateKey, delegation.ProviderCodex, delegation.EventUserPromptSubmit)
	result, err = ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
	require.Equal(t, "ai_access_evaluator_unavailable", *result.Reason)
	require.Equal(t, aiAccessEvaluatorFailureMessage, *result.Message)
	require.NotEqual(t, "Exact administrator note.", *result.Message)
	require.Len(t, evaluator.requests, 1)

	ctx, ti, _, signer, privateKey = setupHookAIAccess(t, noMatch)
	ti.service.aiAccess = nil
	payload = signedGovernedPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, delegation.EventPreToolUse)
	result, err = ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "ai_access_evaluator_unavailable", *result.Reason)
}

func TestHookAIAccessRejectsCrossTenantSpoofAndInactiveMembership(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	ctx, ti, evaluator, signer, privateKey := setupHookAIAccess(t, noMatch)
	payload := signedGovernedPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, delegation.EventPreToolUse)
	payload.ActingUserAssertion = new("spoofed")
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "ai_access_identity_unavailable", *result.Reason)
	require.Empty(t, evaluator.requests)

	payload = signedGovernedPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, delegation.EventPreToolUse)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	require.True(t, ok)
	refresh, err := signer.MintRefresh(authCtx.UserID, "other-organization", publicKey)
	require.NoError(t, err)
	identity, err := signer.VerifyRefresh(refresh)
	require.NoError(t, err)
	crossTenantRequest := delegation.MintRequest{RefreshToken: refresh, ContractVersion: delegation.ContractVersion, Provider: delegation.ProviderClaude, Event: delegation.EventPreToolUse, SessionID: canonicalSessionID(payload), IdempotencyKey: *payload.IdempotencyKey, SignedAt: time.Now().Unix(), Nonce: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	crossTenantRequest.Signature, err = delegation.Sign(privateKey, crossTenantRequest)
	require.NoError(t, err)
	crossTenantAssertion, err := signer.MintAssertion(identity, crossTenantRequest)
	require.NoError(t, err)
	payload.ActingUserAssertion = &crossTenantAssertion
	result, err = ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "ai_access_identity_unavailable", *result.Reason)
	require.Empty(t, evaluator.requests)

	payload = signedGovernedPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, delegation.EventPreToolUse)
	authCtx, ok = contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err = ti.conn.Exec(ctx, `UPDATE organization_user_relationships SET deleted_at = clock_timestamp() WHERE organization_id = $1 AND user_id = $2`, authCtx.ActiveOrganizationID, authCtx.UserID) //nolint:glint // notestingrawsql: isolated integration fixture must simulate membership removal
	require.NoError(t, err)
	t.Cleanup(func() {
		_, restoreErr := ti.conn.Exec(context.Background(), `UPDATE organization_user_relationships SET deleted_at = NULL WHERE organization_id = $1 AND user_id = $2`, authCtx.ActiveOrganizationID, authCtx.UserID) //nolint:glint // notestingrawsql: restore isolated membership fixture during cleanup
		require.NoError(t, restoreErr)
	})
	result, err = ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
	require.Equal(t, "ai_access_identity_unavailable", *result.Reason)
	require.Empty(t, evaluator.requests, "inactive membership never becomes an evaluator candidate")
}

func TestHookAIAccessExclusionsReplayAndBackfillRequireSignedObservationalBinding(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	ctx, ti, evaluator, signer, privateKey := setupHookAIAccess(t, noMatch)

	for _, test := range []struct{ provider, event, eventType string }{
		{delegation.ProviderCodex, "PermissionRequest", "tool.requested"},
		{"cursor", delegation.EventPreToolUse, "tool.requested"},
	} {
		payload := canonicalIngestPayload(test.provider, test.eventType, uuid.NewString())
		payload.Source.RawEventName = &test.event
		result, err := ti.service.Ingest(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, "allow", result.Decision)
	}

	for name, flags := range map[string]struct{ backfilled, replayed bool }{
		"backfilled": {backfilled: true},
		"replayed":   {replayed: true},
	} {
		unsigned := canonicalIngestPayload(delegation.ProviderClaude, "prompt.submitted", uuid.NewString())
		event := delegation.EventUserPromptSubmit
		unsigned.Source.RawEventName = &event
		unsigned.Backfilled = &flags.backfilled
		unsigned.Replayed = &flags.replayed
		result, err := ti.service.Ingest(ctx, unsigned)
		require.NoError(t, err, name)
		require.Equal(t, "ai_access_identity_unavailable", *result.Reason, name)

		signed := signedHookPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, event, true)
		signed.Backfilled = &flags.backfilled
		signed.Replayed = &flags.replayed
		result, err = ti.service.Ingest(ctx, signed)
		require.NoError(t, err, name)
		require.Equal(t, "deny", result.Decision, name)
		require.Equal(t, "ai_access_observational", *result.Reason, name)
	}
	require.Empty(t, evaluator.requests)
}

func TestHookAIAccessObservationalMarkerTamperingFailsClosed(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	ctx, ti, evaluator, signer, privateKey := setupHookAIAccess(t, noMatch)

	liveAssertion := signedHookPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, delegation.EventPreToolUse, false)
	replayed := true
	liveAssertion.Replayed = &replayed
	result, err := ti.service.Ingest(ctx, liveAssertion)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
	require.Equal(t, "ai_access_identity_unavailable", *result.Reason)

	observationalAssertion := signedHookPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, delegation.EventPreToolUse, true)
	result, err = ti.service.Ingest(ctx, observationalAssertion)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
	require.Equal(t, "ai_access_identity_unavailable", *result.Reason)

	observationalAssertion = signedHookPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, delegation.EventPreToolUse, true)
	observationalAssertion.Replayed = &replayed
	observationalAssertion.Source.RawEventName = nil
	result, err = ti.service.Ingest(ctx, observationalAssertion)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
	require.Equal(t, "ai_access_identity_unavailable", *result.Reason)

	observationalAssertion = signedHookPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, delegation.EventPreToolUse, true)
	observationalAssertion.Replayed = &replayed
	observationalAssertion.Source.Adapter = "cursor"
	result, err = ti.service.Ingest(ctx, observationalAssertion)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
	require.Equal(t, "ai_access_identity_unavailable", *result.Reason)
	require.Empty(t, evaluator.requests)
}

func TestHookAIAccessDuplicateVerifiesIdentityBeforeCachedDenial(t *testing.T) {
	t.Parallel()
	match, err := killswitches.NewMatchResult(killswitches.PrescriptionID(uuid.NewString()), "Stable denial.")
	require.NoError(t, err)
	ctx, ti, evaluator, signer, privateKey := setupHookAIAccess(t, match)
	payload := signedGovernedPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, delegation.EventPreToolUse)
	validAssertion := *payload.ActingUserAssertion
	first, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "Stable denial.", *first.Message)

	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	evaluator.result = noMatch
	second, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "Stable denial.", *second.Message)
	require.Len(t, evaluator.requests, 1, "a valid exact duplicate reuses the matched denial")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	stale := staleSignedAssertion(t, payload, authCtx)
	for name, assertion := range map[string]*string{
		"missing": nil,
		"stale":   &stale,
		"spoofed": new("not-a-signed-assertion"),
	} {
		payload.ActingUserAssertion = assertion
		result, err := ti.service.Ingest(ctx, payload)
		require.NoError(t, err, name)
		require.Equal(t, "ai_access_identity_unavailable", *result.Reason, name)
		require.NotEqual(t, "Stable denial.", *result.Message, name)
	}
	payload.ActingUserAssertion = &validAssertion

	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	require.True(t, ok)
	crossTenantRefresh, err := signer.MintRefresh(authCtx.UserID, "other-organization", publicKey)
	require.NoError(t, err)
	crossTenantIdentity, err := signer.VerifyRefresh(crossTenantRefresh)
	require.NoError(t, err)
	crossTenantNonce, err := delegation.NewNonce()
	require.NoError(t, err)
	crossTenantRequest := delegation.MintRequest{RefreshToken: crossTenantRefresh, ContractVersion: delegation.ContractVersion, Provider: delegation.ProviderClaude, Event: delegation.EventPreToolUse, SessionID: canonicalSessionID(payload), IdempotencyKey: *payload.IdempotencyKey, SignedAt: time.Now().Unix(), Nonce: crossTenantNonce}
	crossTenantRequest.Signature, err = delegation.Sign(privateKey, crossTenantRequest)
	require.NoError(t, err)
	crossTenantAssertion, err := signer.MintAssertion(crossTenantIdentity, crossTenantRequest)
	require.NoError(t, err)
	payload.ActingUserAssertion = &crossTenantAssertion
	crossTenant, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "ai_access_identity_unavailable", *crossTenant.Reason)
	require.NotEqual(t, "Stable denial.", *crossTenant.Message)

	otherUserID := "duplicate-user-" + uuid.NewString()
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, otherUserID, "duplicate-user@example.test")
	otherPublicKey, otherPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	otherRefresh, err := signer.MintRefresh(otherUserID, authCtx.ActiveOrganizationID, otherPublicKey)
	require.NoError(t, err)
	otherIdentity, err := signer.VerifyRefresh(otherRefresh)
	require.NoError(t, err)
	otherNonce, err := delegation.NewNonce()
	require.NoError(t, err)
	otherRequest := delegation.MintRequest{RefreshToken: otherRefresh, ContractVersion: delegation.ContractVersion, Provider: delegation.ProviderClaude, Event: delegation.EventPreToolUse, SessionID: canonicalSessionID(payload), IdempotencyKey: *payload.IdempotencyKey, SignedAt: time.Now().Unix(), Nonce: otherNonce}
	otherRequest.Signature, err = delegation.Sign(otherPrivateKey, otherRequest)
	require.NoError(t, err)
	otherAssertion, err := signer.MintAssertion(otherIdentity, otherRequest)
	require.NoError(t, err)
	payload.ActingUserAssertion = &otherAssertion
	crossUser, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", crossUser.Decision, "verified principal must scope the denial cache")
	require.Len(t, evaluator.requests, 2)

	payload.ActingUserAssertion = &validAssertion
	_, err = ti.conn.Exec(ctx, `UPDATE organization_user_relationships SET deleted_at = clock_timestamp() WHERE organization_id = $1 AND user_id = $2`, authCtx.ActiveOrganizationID, authCtx.UserID) //nolint:glint // notestingrawsql: isolated integration fixture must simulate membership removal
	require.NoError(t, err)
	t.Cleanup(func() {
		_, restoreErr := ti.conn.Exec(context.Background(), `UPDATE organization_user_relationships SET deleted_at = NULL WHERE organization_id = $1 AND user_id = $2`, authCtx.ActiveOrganizationID, authCtx.UserID) //nolint:glint // notestingrawsql: restore isolated membership fixture during cleanup
		require.NoError(t, restoreErr)
	})
	inactive, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "ai_access_identity_unavailable", *inactive.Reason)
	require.NotEqual(t, "Stable denial.", *inactive.Message)
}

func TestHookAIAccessCachesOnlyMatchedExternalNoteDenials(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	ctx, ti, evaluator, signer, privateKey := setupHookAIAccess(t, noMatch)
	payload := signedGovernedPayload(t, ctx, signer, privateKey, delegation.ProviderCodex, delegation.EventUserPromptSubmit)
	first, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", first.Decision)
	match, err := killswitches.NewMatchResult(killswitches.PrescriptionID(uuid.NewString()), "New denial.")
	require.NoError(t, err)
	evaluator.result = match
	second, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "New denial.", *second.Message)
	require.Len(t, evaluator.requests, 2, "allow must never be cached")

	failure, err := killswitches.NewInfrastructureFailureResultWithPolicy(context.DeadlineExceeded, killswitches.FailurePolicyFailClosed, killswitches.InfrastructureFailureTimeout)
	require.NoError(t, err)
	evaluator.result = failure
	payload = signedGovernedPayload(t, ctx, signer, privateKey, delegation.ProviderCodex, delegation.EventPreToolUse)
	failed, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "ai_access_evaluator_unavailable", *failed.Reason)
	evaluator.result = noMatch
	retried, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", retried.Decision, "evaluator failures must never be cached")
	require.Len(t, evaluator.requests, 4)
}

func TestHookAIAccessDuplicateFailsClosedWhenDenialCacheReadFails(t *testing.T) {
	t.Parallel()
	match, err := killswitches.NewMatchResult(killswitches.PrescriptionID(uuid.NewString()), "Stable denial.")
	require.NoError(t, err)
	ctx, ti, evaluator, signer, privateKey := setupHookAIAccess(t, match)
	payload := signedGovernedPayload(t, ctx, signer, privateKey, delegation.ProviderClaude, delegation.EventPreToolUse)
	first, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "Stable denial.", *first.Message)

	ti.service.cache = failingDenialReadCache{Cache: ti.service.cache}
	duplicate, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "ai_access_evaluator_unavailable", *duplicate.Reason)
	require.Len(t, evaluator.requests, 1, "a cache backend error must not re-evaluate into allow")
}

func TestHookAIAccessConcurrentDuplicateWaitsForFirstDenial(t *testing.T) {
	t.Parallel()
	match, err := killswitches.NewMatchResult(killswitches.PrescriptionID(uuid.NewString()), "Concurrent denial.")
	require.NoError(t, err)
	ctx, ti, _, signer, privateKey := setupHookAIAccess(t, match)
	blocking := &blockingHookEvaluator{result: match, started: make(chan struct{}), release: make(chan struct{})}
	ti.service.aiAccess.evaluator = blocking
	payload := signedGovernedPayload(t, ctx, signer, privateKey, delegation.ProviderCodex, delegation.EventPreToolUse)

	type outcome struct {
		result *gen.IngestHookResult
		err    error
	}
	results := make(chan outcome, 2)
	go func() {
		result, err := ti.service.Ingest(ctx, payload)
		results <- outcome{result: result, err: err}
	}()
	<-blocking.started
	go func() {
		result, err := ti.service.Ingest(ctx, payload)
		results <- outcome{result: result, err: err}
	}()
	select {
	case early := <-results:
		require.Failf(t, "duplicate returned before first decision", "result=%+v err=%v", early.result, early.err)
	case <-time.After(100 * time.Millisecond):
	}
	close(blocking.release)
	for range 2 {
		got := <-results
		require.NoError(t, got.err)
		require.Equal(t, "Concurrent denial.", *got.result.Message)
	}
	require.Equal(t, int32(1), blocking.calls.Load())
}

func TestHookDenialCacheKeyScopesTenantAndInvocationBindings(t *testing.T) {
	t.Parallel()
	payload := canonicalIngestPayload(delegation.ProviderClaude, "tool.requested", "session-one")
	event := delegation.EventPreToolUse
	idempotencyKey := "same-idempotency"
	payload.Source.RawEventName = &event
	payload.IdempotencyKey = &idempotencyKey
	payload.ActingUserContractVersion = new(delegation.ContractVersion)
	payload.ActingUserAssertion = new("assertion-one")
	baseline, ok := hookDenialCacheKey(payload, "org-one")
	require.True(t, ok)

	otherTenant, ok := hookDenialCacheKey(payload, "org-two")
	require.True(t, ok)
	require.NotEqual(t, baseline, otherTenant)

	otherSession := *payload
	otherSession.Session = &gen.HookIngestSession{ID: new("session-two")}
	otherSessionKey, ok := hookDenialCacheKey(&otherSession, "org-one")
	require.True(t, ok)
	require.NotEqual(t, baseline, otherSessionKey)

	otherProvider := *payload
	otherProviderSource := *payload.Source
	otherProvider.Source = &otherProviderSource
	otherProvider.Source.Adapter = delegation.ProviderCodex
	otherProviderKey, ok := hookDenialCacheKey(&otherProvider, "org-one")
	require.True(t, ok)
	require.NotEqual(t, baseline, otherProviderKey)

	otherAssertion := *payload
	otherAssertion.ActingUserAssertion = new("assertion-two")
	otherAssertionKey, ok := hookDenialCacheKey(&otherAssertion, "org-one")
	require.True(t, ok)
	require.NotEqual(t, baseline, otherAssertionKey)
}

func staleSignedAssertion(t *testing.T, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext) string {
	t.Helper()
	issuedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	claims := jwt.MapClaims{
		"iss": delegation.AssertionIssuer, "sub": "user:" + authCtx.UserID, "aud": []string{delegation.AssertionAudience}, "jti": "stale-jti",
		"iat": issuedAt.Unix(), "nbf": issuedAt.Unix(), "exp": issuedAt.Add(hooksacting.AssertionLifetime).Unix(),
		"ver": delegation.ContractVersion, "org": authCtx.ActiveOrganizationID, "provider": payload.Source.Adapter, "event": *payload.Source.RawEventName,
		"session_id": canonicalSessionID(payload), "idempotency_key": *payload.IdempotencyKey, "kid": "stale-key",
	}
	key, err := hkdf.Key(sha256.New, []byte("test-hooks-acting-user-secret"), nil, "hooks/acting-user-assertion/v1", sha256.Size)
	require.NoError(t, err)
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	require.NoError(t, err)
	return raw
}
