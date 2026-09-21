package instances

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	templatesrepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

type scanEvaluatorFunc func(context.Context, io.Reader, mcpriskscan.Event)

func (f scanEvaluatorFunc) Scan(ctx context.Context, input io.Reader, event mcpriskscan.Event) {
	f(ctx, input, event)
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
	svc.scanEvaluator = scanEvaluatorFunc(func(_ context.Context, input io.Reader, event mcpriskscan.Event) {
		payload, err := io.ReadAll(input)
		require.NoError(t, err)
		require.JSONEq(t, body, string(payload))
		require.Empty(t, recorder.Body.String(), "scan must precede execution")
		events = append(events, event)
	})
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/rpc/instances.invoke/tool?tool_urn="+url.QueryEscape(toolURN.String()), iotest.OneByteReader(strings.NewReader(body)))
	request.Header.Set(constants.SessionHeader, *authCtx.SessionID)
	request.Header.Set(constants.ProjectHeader, *authCtx.ProjectSlug)
	require.NoError(t, svc.ExecuteInstanceTool(recorder, request))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "Summarize independent readers", recorder.Body.String())
	require.Equal(t, []mcpriskscan.Event{{
		Surface: mcpriskscan.SurfaceInstances, Method: mcpriskscan.MethodToolsCall,
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: authCtx.ProjectID.String(),
		ServerID: "", ToolsetID: "", ToolName: "scan-summary", ResourceURI: "", PromptName: "",
		Phase: mcpriskscan.PhaseBeforeExecution,
	}}, events)
}
