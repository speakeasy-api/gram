package mcpriskscan_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNoop_DoesNotPromoteCredentialOwnerToPrincipal(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	hook := mcpriskscan.NewNoop(provider)
	var authCtx contextvalues.AuthContext
	authCtx.UserID = "credential-owner"
	ctx := contextvalues.SetAuthContext(t.Context(), &authCtx)
	event := mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceHostedMCP, OrganizationID: "", ProjectID: "", ServerID: "",
		ToolsetID: "", ToolName: "ping", ResourceURI: "", PromptName: "", Phase: mcpriskscan.PhaseBeforeExecution,
		Payload: nil,
	}
	hook.Scan(ctx, event)
	hook.Scan(mcpidentity.NewValidatorBoundary().StampAPIKey(ctx), event)
	hook.Scan(mcpidentity.NewValidatorBoundary().StampAssistant(ctx), event)

	spans := recorder.Ended()
	require.Len(t, spans, 3)
	for i, wantKind := range []string{"", "api_key", "assistant"} {
		require.Equal(t, "mcp.risk.scan", spans[i].Name())
		attrs := make(map[attribute.Key]attribute.Value)
		for _, kv := range spans[i].Attributes() {
			attrs[kv.Key] = kv.Value
		}
		require.Empty(t, attrs[attr.UserIDKey].AsString())
		require.Equal(t, wantKind, attrs["gram.mcp.risk.scan.principal_kind"].AsString())
		require.Equal(t, i != 0, attrs["gram.mcp.risk.scan.identity_stamped"].AsBool())
		require.Equal(t, "ping", attrs[attr.ToolNameKey].AsString())
		require.Equal(t, mcpriskscan.PhaseBeforeExecution, attrs["gram.mcp.risk.scan.phase"].AsString())
	}
	require.Equal(t, "credential-owner", authCtx.UserID)
}

func TestNoop_DoesNotExportRequestPayload(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	hook := mcpriskscan.NewNoop(provider)
	event := mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceHostedMCP, OrganizationID: "", ProjectID: "", ServerID: "",
		ToolsetID: "", ToolName: "ping", ResourceURI: "", PromptName: "", Phase: mcpriskscan.PhaseBeforeExecution,
		Payload: nil,
	}
	hook.Scan(t.Context(), event)
	for _, payload := range []any{
		json.RawMessage(`{"private_argument":"sensitive-tool-input"}`),
		map[string]string{"private_argument": "sensitive-prompt-input"},
	} {
		event.Payload = payload
		hook.Scan(t.Context(), event)
	}

	spans := recorder.Ended()
	require.Len(t, spans, 3)
	for _, span := range spans[1:] {
		// Even a serialized or transformed payload must not add or alter attributes.
		require.ElementsMatch(t, spans[0].Attributes(), span.Attributes())
	}
}
