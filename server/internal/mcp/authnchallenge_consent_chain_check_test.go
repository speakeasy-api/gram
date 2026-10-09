package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
)

func postChainCheck(t *testing.T, fx consentActionFixture, edit func(url.Values)) (*httptest.ResponseRecorder, error) {
	t.Helper()

	form := url.Values{}
	form.Set("state", fx.stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "identity_chaining_check")
	form.Set("client_id", fx.clientA.String())
	if edit != nil {
		edit(form)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	if err := fx.ti.service.ServeConsentAction(w, req, fx.endpoint); err != nil {
		return w, fmt.Errorf("serve consent action: %w", err)
	}
	return w, nil
}

func chainCheckResult(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body, 2, "only status and message leave the server")
	return body
}

func TestConsentChainCheck_SuccessConnectsWithRuntimeRequest(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim480-check-ok")
	g := setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	var deadline time.Time
	g.acquire = func(ctx context.Context, _ identitychaining.Request) identitychaining.Outcome {
		deadline, _ = ctx.Deadline()
		return identitychaining.Outcome{Stage: identitychaining.StageComplete, Reason: identitychaining.ReasonSuccess, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false}
	}

	w, err := postChainCheck(t, fx, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"status": "connected", "message": "Connected through your identity provider"}, chainCheckResult(t, w))

	require.Equal(t, []identitychaining.Request{{
		OrganizationID:        fx.orgID,
		ProjectID:             fx.projectID,
		UserSessionIssuerID:   fx.shared,
		UserID:                fx.subject.ID,
		UpstreamResource:      consentChainedUpstream,
		RemoteSessionIssuerID: uuid.NullUUID{},
	}}, g.acquired, "the check acquires, and so publishes, exactly the runtime's credential")
	require.False(t, deadline.IsZero())
	require.LessOrEqual(t, time.Until(deadline), 5*time.Second, "the check is bounded well under the chainer's own attempt budget")
}

func TestConsentChainCheck_RejectionsAreGeneric(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim480-check-reject")
	g := setConsentGovernor(t, fx, issuer, consentChainedUpstream)

	for _, tc := range []struct {
		stage  identitychaining.Stage
		reason identitychaining.Reason
		want   string
	}{
		{identitychaining.StageRedemption, identitychaining.ReasonInvalidGrant, "didn't accept your identity provider sign-in"},
		{identitychaining.StageRedemption, identitychaining.ReasonInvalidClient, "didn't accept your identity provider sign-in"},
		{identitychaining.StageRedemption, identitychaining.ReasonInsufficientScope, "didn't accept your identity provider sign-in"},
		{identitychaining.StageExchange, identitychaining.ReasonAccessDenied, "Your identity provider didn't allow access to"},
		{identitychaining.StageExchange, identitychaining.ReasonScopePolicyDenied, "Your identity provider didn't allow access to"},
		{identitychaining.StageExchange, identitychaining.ReasonInvalidTarget, "Your identity provider didn't allow access to"},
		{identitychaining.StageSelection, identitychaining.ReasonConfigurationRequired, "isn't set up for identity provider sign-in"},
		{identitychaining.StageDelegation, identitychaining.ReasonReauthenticationRequired, "sign-in for"},
	} {
		outcome := identitychaining.Outcome{Stage: tc.stage, Reason: tc.reason, Confidence: identitychaining.ConfidenceInferred, Retryable: false, Cached: false}
		g.acquire = func(context.Context, identitychaining.Request) identitychaining.Outcome { return outcome }

		w, err := postChainCheck(t, fx, nil)
		require.NoError(t, err)
		body := chainCheckResult(t, w)
		require.Equal(t, "rejected", body["status"], tc.reason)
		require.Contains(t, body["message"], tc.want, tc.reason)
		require.Contains(t, body["message"], "use a separate sign-in", tc.reason)
		require.NotContains(t, body["message"], string(tc.reason), "provider codes stay in logs")
	}
}

func TestConsentChainCheck_TransientAndAmbiguousAreUnknown(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim480-check-unknown")
	g := setConsentGovernor(t, fx, issuer, consentChainedUpstream)

	for _, outcome := range []identitychaining.Outcome{
		{Stage: identitychaining.StagePersistence, Reason: identitychaining.ReasonTransientFailure, Confidence: identitychaining.ConfidenceVerified, Retryable: true, Cached: false},
		{Stage: identitychaining.StageRedemption, Reason: identitychaining.ReasonUnknownRejection, Confidence: identitychaining.ConfidenceUnknown, Retryable: false, Cached: false},
		{Stage: identitychaining.StageRedemption, Reason: identitychaining.ReasonStaleConfiguration, Confidence: identitychaining.ConfidenceVerified, Retryable: true, Cached: false},
		{Stage: identitychaining.StageSelection, Reason: identitychaining.ReasonNotApplicable, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false},
	} {
		g.acquire = func(context.Context, identitychaining.Request) identitychaining.Outcome { return outcome }
		w, err := postChainCheck(t, fx, nil)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"status": "unknown", "message": "Managed by your identity provider"}, chainCheckResult(t, w), outcome.Reason)
	}
}

func TestConsentChainCheck_TimeoutIsUnknown(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim480-check-timeout")
	g := setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	g.acquire = func(ctx context.Context, _ identitychaining.Request) identitychaining.Outcome {
		<-ctx.Done()
		return identitychaining.Outcome{Stage: identitychaining.StageExchange, Reason: identitychaining.ReasonTransientFailure, Confidence: identitychaining.ConfidenceUnknown, Retryable: true, Cached: false}
	}

	started := time.Now()
	w, err := postChainCheck(t, fx, nil)
	require.NoError(t, err)
	require.Less(t, time.Since(started), 10*time.Second)
	require.Equal(t, "unknown", chainCheckResult(t, w)["status"])
}

func TestConsentChainCheck_RejectsBadRequests(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim480-check-bad")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)

	for _, tc := range []struct {
		name string
		edit func(url.Values)
		code oops.Code
	}{
		{"bad csrf", func(f url.Values) { f.Set("csrf_token", "forged") }, oops.CodeUnauthorized},
		{"unknown state", func(f url.Values) { f.Set("state", uuid.NewString()) }, oops.CodeUnauthorized},
		{"malformed client", func(f url.Values) { f.Set("client_id", "nope") }, oops.CodeBadRequest},
		{"foreign client", func(f url.Values) { f.Set("client_id", uuid.NewString()) }, oops.CodeBadRequest},
	} {
		w, err := postChainCheck(t, fx, tc.edit)
		requireOopsCode(t, err, tc.code)
		require.NotContains(t, w.Body.String(), `"status"`, tc.name)
	}
}

func TestConsentChainCheck_UnresolvedSubjectIsRejected(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim480-check-anon")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	state.ID = uuid.NewString()
	state.Subject = nil
	require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))

	_, err = postChainCheck(t, fx, func(f url.Values) { f.Set("state", state.ID) })
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestConsentChainCheck_UnchainedServiceIsRejected(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim480-check-unchained")
	setConsentGovernor(t, fx, issuer)

	_, err := postChainCheck(t, fx, nil)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestConsentChainCheck_InteractivelyConnectedServiceIsRejected(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim480-check-linked")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	grant(t, ctx, fx, fx.clientA, consentChainedUpstream)

	_, err := postChainCheck(t, fx, nil)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestConsentPage_CachedChainedCredentialRendersConnected(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim480-check-cached")
	g := setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	g.usable = true

	code, page, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, page, "Connected through your identity provider")
	require.Contains(t, page, `data-chain-check="connected"`)
	require.Contains(t, page, "1 of 1 connected")
	require.Contains(t, page, "Use a separate sign-in instead")
	require.Empty(t, g.acquired, "rendering reads stored credentials only")
}
