package anthropicinference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type testProcessor struct {
	calls   int
	verdict Verdict
	err     error
}

func (p *testProcessor) Process(_ context.Context, _ Config, _ Frame) (Verdict, error) {
	p.calls++
	return p.verdict, p.err
}

func newTestHandler(t *testing.T, processor Processor) *handler {
	t.Helper()
	return &handler{
		config: Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil},
		keys:   [][]byte{[]byte("EXAMPLE-signing-secret")}, processor: processor, logger: testenv.NewLogger(t),
	}
}

func serveSigned(h *handler, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header = signedHeaders([]byte(body), h.keys[0], time.Now())
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}

func TestHandlerReturnsPolicyDenialWithHTTP200(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{calls: 0, verdict: Verdict{Action: "deny", DenyReason: "Remove restricted content.", ReferenceID: ""}, err: nil}
	response := serveSigned(newTestHandler(t, processor), `{"type":"prompt","request_id":"request-example","tenant_id":"tenant-example"}`)
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"action":"deny","deny_reason":"Remove restricted content."}`, response.Body.String())
	require.Equal(t, 1, processor.calls)
}

func TestHandlerFailsClosedOnProcessingError(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{calls: 0, verdict: Verdict{Action: "", DenyReason: "", ReferenceID: ""}, err: errors.New("database unavailable")}
	response := serveSigned(newTestHandler(t, processor), `{"type":"prompt","request_id":"request-example","tenant_id":"tenant-example"}`)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"action":"deny"`)
	require.NotContains(t, response.Body.String(), "database")
}

func TestHandlerRejectsCrossTenantDelivery(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{calls: 0, verdict: Verdict{Action: "allow", DenyReason: "", ReferenceID: ""}, err: nil}
	response := serveSigned(newTestHandler(t, processor), `{"type":"prompt","request_id":"request-example","tenant_id":"other-tenant"}`)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Zero(t, processor.calls)
}

func TestHandlerRejectsMismatchedRequestID(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{calls: 0, verdict: Verdict{Action: "allow", DenyReason: "", ReferenceID: ""}, err: nil}
	response := serveSigned(newTestHandler(t, processor), `{"type":"prompt","request_id":"other-request","tenant_id":"tenant-example"}`)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Zero(t, processor.calls)
}

func TestHandlerAcceptsFutureEventsWithoutProcessing(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{calls: 0, verdict: Verdict{Action: "", DenyReason: "", ReferenceID: ""}, err: nil}
	response := serveSigned(newTestHandler(t, processor), `{"type":"future-event","request_id":"request-example","tenant_id":"tenant-example","new_field":true}`)
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"action":"allow"}`, response.Body.String())
	require.Zero(t, processor.calls)
}

func TestHandlerDoesNotTrustConfigTestSourceToBypassPolicies(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{calls: 0, verdict: Verdict{Action: "deny", DenyReason: "blocked", ReferenceID: ""}, err: nil}
	response := serveSigned(newTestHandler(t, processor), `{"type":"prompt","request_id":"request-example","tenant_id":"tenant-example","source":{"application":"config-test"}}`)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, processor.calls)
	require.Contains(t, response.Body.String(), `"action":"deny"`)
}

func TestHandlerRejectsUnsignedBody(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{calls: 0, verdict: Verdict{Action: "allow", DenyReason: "", ReferenceID: ""}, err: nil}
	h := newTestHandler(t, processor)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Zero(t, processor.calls)
}

func TestHandlerBoundsRequestBody(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{calls: 0, verdict: Verdict{Action: "allow", DenyReason: "", ReferenceID: ""}, err: nil}
	h := newTestHandler(t, processor)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(make([]byte, maxRequestBytes+1))))
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	require.Zero(t, processor.calls)
}

func TestHandlerTruncatesDenialByCharacters(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{calls: 0, verdict: Verdict{Action: "deny", DenyReason: strings.Repeat("é", 501), ReferenceID: ""}, err: nil}
	response := serveSigned(newTestHandler(t, processor), `{"type":"prompt","request_id":"request-example","tenant_id":"tenant-example"}`)
	var verdict Verdict
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &verdict))
	require.Len(t, []rune(verdict.DenyReason), 500)
}

type testResolver struct {
	config Config
	err    error
}

func (r *testResolver) Resolve(context.Context, string) (Config, error) { return r.config, r.err }

func TestAttachRejectsInvalidSecretWithoutExposingIt(t *testing.T) {
	t.Parallel()
	mux := goahttp.NewMuxer()
	Attach(mux, testenv.NewLogger(t), nil, &testResolver{config: Config{SigningSecrets: []string{"EXAMPLE-invalid-secret"}}, err: nil})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/hooks/anthropic-inference/example", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.NotContains(t, response.Body.String(), "EXAMPLE-invalid-secret")
}

func TestPendingSetupOnlyAcceptsSyntheticProbe(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{}
	h := newTestHandler(t, processor)
	h.keys = nil
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"type":"prompt","source":{"application":"config-test"}}`)))
	require.Equal(t, http.StatusOK, response.Code)
	require.Zero(t, processor.calls)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"type":"prompt","source":{"application":"claude-code"}}`)))
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Zero(t, processor.calls)
}

func TestConfiguredHookRejectsUnsignedProbe(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{}
	h := newTestHandler(t, processor)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"type":"prompt","source":{"application":"config-test"}}`)))
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Zero(t, processor.calls)
}

func TestHandlerAcceptsSignedNullTenant(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{calls: 0, verdict: Verdict{Action: "allow", DenyReason: "", ReferenceID: ""}, err: nil}
	response := serveSigned(newTestHandler(t, processor), `{"type":"prompt","request_id":"request-example","tenant_id":null}`)
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"action":"allow"}`, response.Body.String())
	require.Equal(t, 1, processor.calls)
}
