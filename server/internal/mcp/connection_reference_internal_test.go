package mcp

import (
	"context"
	"encoding/json"

	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func recordRefusal(t *testing.T, recorder *mcpconnectionattempts.Recorder) context.Context {
	t.Helper()
	ctx := contextvalues.WithConnectionReferenceSlot(t.Context())
	scope := mcpconnectionattempts.ServerScope("org", uuid.New(), uuid.New(), mcpconnectionattempts.AppDCR)
	(&Service{connections: recorder}).recordRequestOutcome(ctx, scope, mcpconnectionattempts.CodeInvalidRemoteSession, httptest.NewRequest(http.MethodPost, "/", nil))
	return ctx
}

func TestConnectionReference_ToolErrorShowsTheStoredRow(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	ctx := recordRefusal(t, rec.Recorder)
	row := rec.Rows()[0]

	raw, err := marshalMetaToolError(ctx, testenv.NewLogger(t), mcpjsonrpc.StringID("1"), "Ask your administrator to reconnect it")
	require.NoError(t, err)
	var body struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	require.True(t, body.Result.IsError)
	require.Equal(t, "Ask your administrator to reconnect it\nReference: "+row.AttemptID.String(), body.Result.Content[0].Text)
}

func TestConnectionReference_LogsOffLeavesTheToolErrorAlone(t *testing.T) {
	t.Parallel()

	ctx := recordRefusal(t, mcpconnectionattemptstest.NewLogsOff(t).Recorder)
	raw, err := marshalMetaToolError(ctx, testenv.NewLogger(t), mcpjsonrpc.StringID("1"), "Ask your administrator")
	require.NoError(t, err)
	require.NotContains(t, string(raw), "Reference")
}

func TestConnectionReference_StatusRowReportingAProblem(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.WithConnectionReferenceSlot(t.Context())
	contextvalues.SetConnectionReference(ctx, "ref-1")

	broken := metamcp.ConnectionStatusServer{Slug: "s", Status: metamcp.SignInUnavailable, Detail: "down", Outage: &metamcp.ConnectionStatusOutage{Message: "Try again.", ReconnectURL: ""}}
	withStatusReference(ctx, &broken)
	require.Equal(t, "down. Reference: ref-1", broken.Detail)
	require.Equal(t, "Try again. Reference: ref-1", broken.Outage.Message)

	connected := metamcp.ConnectionStatusServer{Slug: "s", Status: metamcp.SignInConnected}
	withStatusReference(ctx, &connected)
	require.Empty(t, connected.Detail, "a working row has no problem to refer to")

	noReference := metamcp.ConnectionStatusServer{Slug: "s", Status: metamcp.SignInError, Detail: "could not check"}
	withStatusReference(t.Context(), &noReference)
	require.Equal(t, "could not check", noReference.Detail)
}

func TestConnectionReference_RecoveryPage(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	require.NoError(t, renderConnectionRecoveryPage(w, http.StatusForbidden, connectionRecoveryNoAccessPage, "Reference: ref-1"))
	require.Contains(t, w.Body.String(), "Reference: ref-1")

	plain := httptest.NewRecorder()
	require.NoError(t, renderConnectionRecoveryPage(plain, http.StatusForbidden, connectionRecoveryNoAccessPage, ""))
	require.NotContains(t, plain.Body.String(), "Reference")
}

func TestConnectionReference_NotShownToSomeoneElsesLink(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.WithConnectionReferenceSlot(t.Context())
	contextvalues.SetConnectionReference(ctx, "ref-1")
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)

	require.NoError(t, (&Service{}).answerConnectionRecoveryRefusal(w, r, connectionRecovery{}, errRecoveryOtherUser))
	require.NotContains(t, w.Body.String(), "Reference")
}

// Member A's failed refresh, earlier in the request, must not label member B's error.
func TestConnectionReference_MemberDispatchDoesNotInheritAnEarlierRow(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.WithConnectionReferenceSlot(t.Context())
	contextvalues.SetConnectionReference(ctx, "ref-member-a")

	service := &Service{logger: testenv.NewLogger(t)}
	req := &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.StringID("1"), Method: "tools/call", Params: nil}
	_, err := service.executeMetaMemberTool(ctx, service.logger, &metaGateContext{}, nil, req, "member-b--tool", nil, nil)
	require.Error(t, err, "the unknown server is answered with no row of its own")
	require.Empty(t, contextvalues.ConnectionReference(ctx), "member B's error shows no reference")
}

func TestConnectionReference_AFailedRefreshDoesNotLabelAnUnrelatedError(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.WithConnectionReferenceSlot(t.Context())
	contextvalues.SetRefreshConnectionReference(ctx, "ref-refresh")

	raw, err := marshalMetaToolError(ctx, testenv.NewLogger(t), mcpjsonrpc.StringID("1"), "Ask your administrator")
	require.NoError(t, err)
	require.NotContains(t, string(raw), "ref-refresh")
}
