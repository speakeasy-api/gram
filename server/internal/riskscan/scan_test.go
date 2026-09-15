package riskscan_test

import (
	"context"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/riskscan"
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
	hook := riskscan.NewNoop(provider)
	var authCtx contextvalues.AuthContext
	authCtx.UserID = "credential-owner"
	ctx := contextvalues.SetAuthContext(t.Context(), &authCtx)
	event := riskscan.Event{
		Surface: riskscan.SurfaceHostedMCP, OrganizationID: "", ProjectID: "", ServerID: "",
		ToolsetID: "", ToolName: "ping", ResourceURI: "", PromptName: "", Phase: riskscan.PhaseBeforeExecution,
	}
	hook.Scan(ctx, event)
	hook.Scan(mcpidentity.NewValidatorBoundary().StampAPIKey(ctx), event)
	hook.Scan(mcpidentity.NewValidatorBoundary().StampAssistant(ctx), event)

	spans := recorder.Ended()
	require.Len(t, spans, 3)
	for i, wantKind := range []string{"", "api_key", "assistant"} {
		require.Equal(t, "risk.scan", spans[i].Name())
		attrs := make(map[attribute.Key]attribute.Value)
		for _, kv := range spans[i].Attributes() {
			attrs[kv.Key] = kv.Value
		}
		require.Empty(t, attrs[attr.UserIDKey].AsString())
		require.Equal(t, wantKind, attrs["gram.risk.scan.principal_kind"].AsString())
		require.Equal(t, i != 0, attrs["gram.risk.scan.identity_stamped"].AsBool())
		require.Equal(t, "ping", attrs[attr.ToolNameKey].AsString())
		require.Equal(t, riskscan.PhaseBeforeExecution, attrs["gram.risk.scan.phase"].AsString())
	}
	require.Equal(t, "credential-owner", authCtx.UserID)
}
