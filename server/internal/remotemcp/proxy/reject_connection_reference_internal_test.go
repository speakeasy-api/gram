package proxy

import (
	"encoding/json"
	"net/http/httptest"
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
