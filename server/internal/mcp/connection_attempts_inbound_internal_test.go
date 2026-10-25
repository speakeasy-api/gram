package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

func gatewayEndpoint() *ResolvedMcpEndpoint {
	return &ResolvedMcpEndpoint{
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		ProjectID:       uuid.New(),
		OrganizationID:  "org",
		RouteBase:       mcproute.MCP,
	}
}

func newObservation(t *testing.T) *inboundObservation {
	t.Helper()
	_, obs := observeInbound(httptest.NewRequest(http.MethodPost, "/token", nil), gatewayEndpoint(), attemptStageInboundToken)
	require.NotNil(t, obs)
	return obs
}

func TestObserveInboundOnlyConfiguredGateways(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/authorize", nil)

	observed, obs := observeInbound(request, gatewayEndpoint(), attemptStageInboundAuthorize)
	require.NotNil(t, obs)
	require.Same(t, obs, inboundObservationFrom(observed.Context()))

	server := gatewayEndpoint()
	server.MetaMcpServerID = uuid.NullUUID{}
	server.McpServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	organization := gatewayEndpoint()
	organization.RouteBase = mcproute.OrganizationGateway
	for _, endpoint := range []*ResolvedMcpEndpoint{nil, server, organization} {
		unobserved, obs := observeInbound(request, endpoint, attemptStageInboundAuthorize)
		require.Nil(t, obs)
		require.Nil(t, inboundObservationFrom(unobserved.Context()))
	}
	// Annotations without an observation do nothing.
	inboundObservationFrom(t.Context()).refuse(classConfiguration, reasonUnknownClientID)
}

func TestInboundResult(t *testing.T) {
	t.Parallel()
	person := urn.NewUserSubject("user_1")

	cases := []struct {
		name     string
		annotate func(*inboundObservation)
		canceled bool
		err      error
		want     inboundResult
	}{
		{"issued and written", func(o *inboundObservation) { o.issue(outcomeCodeIssued, person); o.responseWritten(nil) }, false, nil,
			inboundResult{outcome: outcomeCodeIssued, class: classNone, reason: reasonNone}},
		{"issued but the write failed", func(o *inboundObservation) {
			o.issue(outcomeRefreshed, person)
			o.responseWritten(errors.New("broken pipe"))
		}, false, errors.New("write token response"),
			inboundResult{outcome: outcomeFailed, class: classInternal, reason: reasonResponseWriteFailed}},
		{"issued but the client left", func(o *inboundObservation) {
			o.issue(outcomeRefreshed, person)
			o.responseWritten(errors.New("broken pipe"))
		}, true, nil,
			inboundResult{outcome: outcomeFailed, class: classCanceled, reason: reasonCanceled}},
		{"a written success nothing named", func(o *inboundObservation) { o.responseWritten(nil) }, false, nil,
			inboundResult{outcome: outcomeUnknown, class: classInternal, reason: reasonUnobservedExit}},
		{"returned nothing and wrote nothing", func(*inboundObservation) {}, false, nil,
			inboundResult{outcome: outcomeUnknown, class: classInternal, reason: reasonUnobservedExit}},
		{"a typed refusal wins over the writer", func(o *inboundObservation) {
			o.refuse(classEndpoint, reasonCIMDFetchFailed)
			o.answer(http.StatusServiceUnavailable, "temporarily_unavailable")
		}, false, nil,
			inboundResult{outcome: outcomeFailed, class: classEndpoint, reason: reasonCIMDFetchFailed}},
		{"a logged policy reason blocks", func(o *inboundObservation) {
			o.note("ai_tool_blocked")
			o.answer(http.StatusBadRequest, "unauthorized_client")
		}, false, nil,
			inboundResult{outcome: outcomePolicyBlocked, class: classConfiguration, reason: "ai_tool_blocked"}},
		{"a logged reason refines the writer's", func(o *inboundObservation) {
			o.note("refresh_token_expired")
			o.answer(http.StatusBadRequest, "invalid_grant")
		}, false, nil,
			inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "refresh_token_expired"}},
		{"an unlisted logged value is ignored", func(o *inboundObservation) {
			o.note("https://client.example/metadata")
			o.answer(http.StatusTooManyRequests, "temporarily_unavailable")
		}, false, nil,
			inboundResult{outcome: outcomeFailed, class: classInternal, reason: "temporarily_unavailable"}},
		{"an unknown OAuth code is bounded", func(o *inboundObservation) { o.answer(http.StatusBadRequest, "made_up") }, false, nil,
			inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: reasonOAuthError}},
		{"an OAuth error returned", func(*inboundObservation) {}, false,
			oops.NewOAuthError(http.StatusBadRequest, "invalid_grant", "issuer gone", nil),
			inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "invalid_grant"}},
		{"a 4xx error returned", func(*inboundObservation) {}, false,
			oops.E(oops.CodeBadRequest, nil, "capture OAuth endpoint authority"),
			inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "invalid_request"}},
		{"an unknown error type", func(*inboundObservation) {}, false, errors.New("boom"),
			inboundResult{outcome: outcomeFailed, class: classInternal, reason: reasonUnexpected}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			obs := newObservation(t)
			tc.annotate(obs)
			ctx := t.Context()
			if tc.canceled {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			require.Equal(t, tc.want, obs.result(ctx, tc.err))
		})
	}
}

func TestShareableResult(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code oops.Code
		want inboundResult
	}{
		{oops.CodeCanceled, inboundResult{outcome: outcomeFailed, class: classCanceled, reason: reasonCanceled}},
		{oops.CodeUnavailable, inboundResult{outcome: outcomeFailed, class: classInternal, reason: reasonUnavailable}},
		{oops.CodeGatewayError, inboundResult{outcome: outcomeFailed, class: classInternal, reason: reasonUnavailable}},
		{oops.CodeBadRequest, inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "invalid_request"}},
		{oops.CodeInvalid, inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "invalid_request"}},
		{oops.CodeNotFound, inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "not_found"}},
		{oops.CodeUnauthorized, inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "unauthorized"}},
		{oops.CodeForbidden, inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "forbidden"}},
		{oops.CodeConflict, inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "conflict"}},
		{oops.CodeUnexpected, inboundResult{outcome: outcomeFailed, class: classInternal, reason: reasonUnexpected}},
		{oops.CodeInvariantViolation, inboundResult{outcome: outcomeFailed, class: classInternal, reason: reasonUnexpected}},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, shareableResult(tc.code), tc.code)
	}
}

func TestInboundTypedRefusals(t *testing.T) {
	t.Parallel()
	require.Equal(t, attemptReason("cimd_denied_not_listed"), cimdDenialReason(admission.DenialNotListed))
	require.Equal(t, attemptReason("cimd_admission_denied"), cimdDenialReason(admission.DenialReason("denied_future")))

	cases := []struct {
		refusal *workloadGrantError
		want    inboundResult
	}{
		{&workloadGrantError{reason: "subject_not_admitted", outcome: workloadGrantRefused, retryAfter: 0, err: nil}, inboundResult{outcome: outcomePolicyBlocked, class: classConfiguration, reason: "subject_not_admitted"}},
		{&workloadGrantError{reason: "endpoint_not_agent_addressable", outcome: workloadGrantRefused, retryAfter: 0, err: nil}, inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: reasonEndpointNotAgentAddressable}},
		{&workloadGrantError{reason: "issuer_lookup_unavailable", outcome: workloadGrantUnavailable, retryAfter: 0, err: nil}, inboundResult{outcome: outcomeFailed, class: classInternal, reason: "issuer_lookup_unavailable"}},
		{&workloadGrantError{reason: "signature_invalid_at_some_stage", outcome: workloadGrantRefused, retryAfter: 0, err: nil}, inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "workload_assertion_refused"}},
	}
	for _, tc := range cases {
		obs := newObservation(t)
		obs.refuseWorkload(tc.refusal)
		require.Equal(t, tc.want, obs.result(t.Context(), nil), tc.refusal.reason)
	}
}

// warnings hears the inbound write's warning, so a test waits for the
// asynchronous write instead of sleeping.
type warnings struct{ seen chan string }

func (w warnings) Enabled(context.Context, slog.Level) bool { return true }

func (w warnings) Handle(_ context.Context, record slog.Record) error {
	if record.Message == "record inbound attempt" {
		w.seen <- record.Message
	}
	return nil
}

func (w warnings) WithAttrs([]slog.Attr) slog.Handler { return w }
func (w warnings) WithGroup(string) slog.Handler      { return w }

// A telemetry write that fails after the response never changes it: the
// refusal reads exactly as it does on an endpoint that is not observed.
func TestInboundWriteFailureLeavesTheResponse(t *testing.T) {
	t.Parallel()
	heard := warnings{seen: make(chan string, 1)}
	logger := slog.New(heard)
	// Nothing listens on port 1, so every insert fails.
	conn, err := clickhouse.Open(&clickhouse.Options{Addr: []string{"127.0.0.1:1"}, DialTimeout: 100 * time.Millisecond})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	enabled := func(context.Context, string) (bool, error) { return true, nil }
	s := &Service{logger: logger, telemLogger: tm.NewLogger(t.Context(), logger, nil, nil, conn, enabled, enabled, nil, tm.NewNoopLogPublisher(logger))}

	serve := func(endpoint *ResolvedMcpEndpoint) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		require.NoError(t, s.ServeToken(w, req.WithContext(t.Context()), endpoint))
		return w
	}
	unobserved := gatewayEndpoint()
	unobserved.MetaMcpServerID = uuid.NullUUID{}
	want := serve(unobserved)
	got := serve(gatewayEndpoint())

	select {
	case <-heard.seen:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the inbound write never finished")
	}
	require.Equal(t, http.StatusBadRequest, got.Code)
	require.Equal(t, want.Code, got.Code)
	require.Equal(t, want.Header(), got.Header())
	require.JSONEq(t, want.Body.String(), got.Body.String())
}
