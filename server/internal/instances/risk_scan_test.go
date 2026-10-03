package instances

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	templatesrepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

type scanObserverFunc func(context.Context, mcpriskscan.Subject)

func (f scanObserverFunc) Observe(ctx context.Context, subject mcpriskscan.Subject) {
	f(ctx, subject)
}

func TestExecuteInstanceTool_ScanCannotConsumeExecutionPayload(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestInstanceService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolURN := urn.NewTool(urn.ToolKindPrompt, "prompt", "scan-summary")
	_, err := templatesrepo.New(conn).CreateTemplate(ctx, templatesrepo.CreateTemplateParams{
		ProjectID: *authCtx.ProjectID,
		ToolUrn:   toolURN,
		Name:      "scan-summary", Prompt: "Summarize {{topic}}",
		Description: conv.ToPGText("Summary"),
		Arguments:   []byte(`{"type":"object","properties":{"topic":{"type":"string"}}}`),
		Engine:      conv.ToPGText("mustache"), Kind: conv.ToPGText("prompt"),
		ToolsHint: nil, ToolUrnsHint: nil,
	})
	require.NoError(t, err)

	const body = `{"arguments":{"topic":"independent readers"}}`
	recorder := httptest.NewRecorder()
	var events []mcpriskscan.Event
	var payloads [][]byte
	svc.scanEvaluator = mcpriskscan.PrependObserver(scanObserverFunc(func(_ context.Context, subject mcpriskscan.Subject) {
		require.Empty(t, recorder.Body.String(), "scan must precede response delivery")
		payloads = append(payloads, append([]byte(nil), subject.Payload.Bytes()...))
		events = append(events, subject.Event)
	}), svc.scanEvaluator)
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/rpc/instances.invoke/tool?tool_urn="+url.QueryEscape(toolURN.String()), iotest.OneByteReader(strings.NewReader(body)))
	request.Header.Set(constants.SessionHeader, *authCtx.SessionID)
	request.Header.Set(constants.ProjectHeader, *authCtx.ProjectSlug)
	require.NoError(t, svc.ExecuteInstanceTool(recorder, request))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "Summarize independent readers", recorder.Body.String())
	require.Equal(t, [][]byte{[]byte(body)}, payloads, "prompt results remain outside response scan scope")
	require.Len(t, events, 1)
	event := events[0]
	require.Equal(t, mcpriskscan.SurfaceInstances, event.Surface)
	require.Equal(t, mcpriskscan.MethodToolsCall, event.Method)
	require.Equal(t, authCtx.ActiveOrganizationID, event.OrganizationID)
	require.Equal(t, authCtx.ProjectID.String(), event.ProjectID)
	require.Empty(t, event.ServerID)
	require.Empty(t, event.MetaServerID)
	require.Empty(t, event.ToolsetID)
	require.Equal(t, "scan-summary", event.ToolName)
	require.Empty(t, event.ChatID)
	require.Equal(t, mcpriskscan.PhaseRequest, event.Phase())
	require.NotEmpty(t, event.ExecutionID())
	require.False(t, event.IdentityStamped(), "instances authentication does not stamp MCP principal provenance")
}

func TestInstanceResponsePayloadScansUTF8DespiteBinaryContentType(t *testing.T) {
	t.Parallel()

	payload := instanceResponsePayload(gateway.ToolKindHTTP, "image/png", []byte("person@example.com"))

	require.Equal(t, mcpriskscan.PayloadAvailable, payload.Availability())
	require.Equal(t, "person@example.com", string(payload.Bytes()))
}

func TestInstanceResponsePayloadScansTextAroundInvalidUTF8(t *testing.T) {
	t.Parallel()

	body := append([]byte("person@example.com"), 0xff)
	payload := instanceResponsePayload(gateway.ToolKindHTTP, "application/octet-stream", body)

	require.Equal(t, mcpriskscan.PayloadAvailable, payload.Availability())
	require.Equal(t, "person@example.com\uFFFD", string(payload.Bytes()))
}

func TestInstanceResponsePayloadRejectsEventStream(t *testing.T) {
	t.Parallel()

	payload := instanceResponsePayload(gateway.ToolKindHTTP, "text/event-stream; charset=utf-8", []byte("data: partial\n\n"))

	require.Equal(t, mcpriskscan.PayloadUnavailable, payload.Availability())
	require.Nil(t, payload.Bytes())
}
