package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func writeRejected(t *testing.T, reference string, cause error) (int, string) {
	t.Helper()
	ctx := contextvalues.WithConnectionReferenceSlot(t.Context())
	if reference != "" {
		contextvalues.SetConnectionReference(ctx, reference)
	}
	p := &Proxy{Logger: testenv.NewLogger(t)}
	w := httptest.NewRecorder()
	_, span := testenv.NewTracerProvider(t).Tracer("").Start(ctx, "")
	defer span.End()
	id, err := jsonrpc.MakeID("1")
	require.NoError(t, err)
	p.writeRejection(ctx, w, span, id, cause)
	var body struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.Error.Code, body.Error.Message
}

func TestWriteRejection_AddsTheRecordedFailuresReference(t *testing.T) {
	t.Parallel()

	shared := &RejectError{Code: RejectCodeForbidden, Message: "Ask an admin", Data: nil}
	code, message := writeRejected(t, "ref-1", shared)
	require.Equal(t, RejectCodeForbidden, code)
	require.Equal(t, "Ask an admin\nReference: ref-1", message)
	require.Equal(t, "Ask an admin", shared.Message, "a shared rejection is not changed")
}

func TestWriteRejection_NoRowLeavesTheMessageAlone(t *testing.T) {
	t.Parallel()

	_, message := writeRejected(t, "", &RejectError{Code: RejectCodeForbidden, Message: "Ask an admin", Data: nil})
	require.Equal(t, "Ask an admin", message)
}

func TestWriteRejection_KillSwitchNoteStaysExact(t *testing.T) {
	t.Parallel()

	_, message := writeRejected(t, "ref-1", NewKillswitchMatchRejection("The exact note"))
	require.Equal(t, "The exact note", message)
}

func TestWriteToolResultError_AddsTheRecordedFailuresReference(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.WithConnectionReferenceSlot(t.Context())
	contextvalues.SetConnectionReference(ctx, "ref-1")
	p := &Proxy{Logger: testenv.NewLogger(t)}
	w := httptest.NewRecorder()
	id, err := jsonrpc.MakeID("1")
	require.NoError(t, err)
	p.writeToolResultError(ctx, w, id, &ToolResultError{Message: "Ask an admin"})
	require.Contains(t, w.Body.String(), "Reference: ref-1")
}

// refusingInterceptor refuses a tools/call, recording a row or not as a direct server would.
type refusingInterceptor struct {
	reference string
}

func (i *refusingInterceptor) Name() string { return "refusing" }

func (i *refusingInterceptor) InterceptToolsCallRequest(context.Context, *ToolsCallRequest) error {
	return &RejectError{Code: RejectCodeForbidden, Message: "Ask an admin", Data: nil}
}

func (i *refusingInterceptor) InterceptToolsCallRefusal(ctx context.Context, _ error) {
	if i.reference != "" {
		contextvalues.SetConnectionReference(ctx, i.reference)
	}
}

func postToolsCall(t *testing.T, interceptor *refusingInterceptor) string {
	t.Helper()
	ctx := contextvalues.WithConnectionReferenceSlot(t.Context())
	contextvalues.SetConnectionReference(ctx, "ref-earlier")
	p := &Proxy{
		Logger:                       testenv.NewLogger(t),
		Tracer:                       testenv.NewTracerProvider(t).Tracer("test"),
		MaxBufferedBodyBytes:         DefaultMaxBufferedBodyBytes,
		ToolsCallRequestInterceptors: []ToolsCallRequestInterceptor{interceptor},
		ToolsCallRefusalInterceptors: []ToolsCallRefusalInterceptor{interceptor},
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"tool","arguments":{}}}`
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	require.NoError(t, p.Post(w, r))
	return w.Body.String()
}

func TestPost_ARefusalWithNoRowDoesNotInheritAnEarlierFailure(t *testing.T) {
	t.Parallel()

	body := postToolsCall(t, &refusingInterceptor{reference: ""})
	require.Contains(t, body, "Ask an admin")
	require.NotContains(t, body, "ref-earlier")
}

func TestPost_ARefusalShowsItsOwnRow(t *testing.T) {
	t.Parallel()

	body := postToolsCall(t, &refusingInterceptor{reference: "ref-own"})
	require.Contains(t, body, "Reference: ref-own")
	require.NotContains(t, body, "ref-earlier")
}
