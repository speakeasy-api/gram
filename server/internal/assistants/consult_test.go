package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	hooksgen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type capturedConsultCall struct {
	auth    *contextvalues.AuthContext
	payload *hooksgen.IngestPayload
}

type captureConsultIngester struct {
	result *hooksgen.IngestHookResult
	err    error
	calls  []capturedConsultCall
}

func (c *captureConsultIngester) IngestAssistantToolCall(_ context.Context, authCtx *contextvalues.AuthContext, payload *hooksgen.IngestPayload) (*hooksgen.IngestHookResult, error) {
	c.calls = append(c.calls, capturedConsultCall{auth: authCtx, payload: payload})
	return c.result, c.err
}

func consultAuthContext(projectID uuid.UUID) *contextvalues.AuthContext {
	return &contextvalues.AuthContext{
		ActiveOrganizationID: "org-test",
		UserID:               "user-test",
		ProjectID:            &projectID,
	}
}

func consultRequest(threadID uuid.UUID) consultToolCallRequest {
	return consultToolCallRequest{
		ThreadID:   threadID.String(),
		ToolName:   "bun_run",
		ToolInput:  json.RawMessage(`{"code":"1"}`),
		ToolCallID: "call_1",
	}
}

func TestConsultToolCallAllowsWhenIngesterNil(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_consult_nil_ingester")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
	ctx := contextvalues.SetAuthContext(t.Context(), consultAuthContext(projectID))

	result, err := core.ConsultToolCall(ctx, projectID, assistantID, consultRequest(threadID))
	require.NoError(t, err)
	require.Equal(t, consultDecisionAllow, result.Decision)
}

func TestConsultToolCallAllowsAndDeniesViaIngester(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_consult_ingester")
	require.NoError(t, err)
	projectID, assistantID, chatID, threadID := insertAssistantFixture(t, conn)

	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
	ingester := &captureConsultIngester{
		result: &hooksgen.IngestHookResult{Decision: consultDecisionAllow},
	}
	core.SetHookIngester(ingester)
	ctx := contextvalues.SetAuthContext(t.Context(), consultAuthContext(projectID))
	req := consultRequest(threadID)

	result, err := core.ConsultToolCall(ctx, projectID, assistantID, req)
	require.NoError(t, err)
	require.Equal(t, consultDecisionAllow, result.Decision)
	require.Len(t, ingester.calls, 1)
	call := ingester.calls[0]
	require.Equal(t, assistantHookAdapter, call.payload.Source.Adapter)
	require.Equal(t, "tool.requested", call.payload.Event.Type)
	require.Equal(t, chatID.String(), *call.payload.Session.ID)
	require.Equal(t, "assistant:"+threadID.String()+":call_1", *call.payload.IdempotencyKey)
	require.Equal(t, "bun_run", *call.payload.Data.ToolCall.Name)
	require.Nil(t, call.payload.Data.Mcp)

	denyMessage := "Speakeasy blocked this tool call"
	ingester.result = &hooksgen.IngestHookResult{Decision: consultDecisionDeny, Message: &denyMessage}
	req.ToolCallID = "call_2"
	result, err = core.ConsultToolCall(ctx, projectID, assistantID, req)
	require.NoError(t, err)
	require.Equal(t, consultDecisionDeny, result.Decision)
	require.Equal(t, denyMessage, result.Message)
}

func TestConsultToolCallFailsOpenOnIngestError(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_consult_ingest_error")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
	core.SetHookIngester(&captureConsultIngester{err: errors.New("ingest unavailable")})
	ctx := contextvalues.SetAuthContext(t.Context(), consultAuthContext(projectID))

	result, err := core.ConsultToolCall(ctx, projectID, assistantID, consultRequest(threadID))
	require.NoError(t, err)
	require.Equal(t, consultDecisionAllow, result.Decision)
}

func TestConsultToolCallRejectsAssistantMismatch(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_consult_mismatch")
	require.NoError(t, err)
	projectID, _, _, threadID := insertAssistantFixture(t, conn)

	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
	core.SetHookIngester(&captureConsultIngester{
		result: &hooksgen.IngestHookResult{Decision: consultDecisionAllow},
	})
	ctx := contextvalues.SetAuthContext(t.Context(), consultAuthContext(projectID))

	_, err = core.ConsultToolCall(ctx, projectID, uuid.New(), consultRequest(threadID))
	require.Error(t, err)
	var se *oops.ShareableError
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeForbidden, se.Code)
}

func TestConsultToolCallUsesDefaultDenyMessage(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_consult_default_deny")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
	core.SetHookIngester(&captureConsultIngester{
		result: &hooksgen.IngestHookResult{Decision: consultDecisionDeny, Message: conv.PtrEmpty("")},
	})
	ctx := contextvalues.SetAuthContext(t.Context(), consultAuthContext(projectID))

	result, err := core.ConsultToolCall(ctx, projectID, assistantID, consultRequest(threadID))
	require.NoError(t, err)
	require.Equal(t, consultDecisionDeny, result.Decision)
	require.Equal(t, consultDefaultDenyMessage, result.Message)
}
