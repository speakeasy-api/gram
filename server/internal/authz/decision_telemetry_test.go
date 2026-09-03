package authz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRecordAuthorizationDecisionEmitsBoundedAttribution(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	email := "must-not-appear@example.com"
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001")
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_123",
		UserID:               "user_authorizer",
		APIKeyID:             "key_123",
		APIKeyName:           "must-not-appear",
		Email:                &email,
	}, agent)
	ctx, span := provider.Tracer("test").Start(ctx, "request")
	RecordAuthorizationDecision(ctx, repo.OperationRequire, repo.OutcomeDeny, repo.ReasonScopeUnsatisfied)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1)
	event := spans[0].Events[0]
	require.Equal(t, authorizationDecisionEvent, event.Name)
	attrs := decisionAttributeMap(event.Attributes)
	require.Equal(t, "require", attrs["gram.authorization.operation"])
	require.Equal(t, "deny", attrs["gram.authorization.result"])
	require.Equal(t, "scope_unsatisfied", attrs["gram.authorization.reason"])
	require.Equal(t, "org_123", attrs["gram.authorization.organization_id"])
	require.Equal(t, "agent", attrs["gram.authorization.actor.type"])
	require.Equal(t, agent.ID, attrs["gram.authorization.actor.id"])
	require.Equal(t, "key_123", attrs["gram.authorization.api_key_id"])
	require.NotContains(t, attrs, "gram.authorization.session_id")
	require.NotContains(t, attrs, "gram.authorization.oauth_client_id")
	require.NotContains(t, attrs, "gram.authorization.actor.name")
	require.NotContains(t, attrs, "gram.authorization.email")
	require.NotContains(t, attrs, "gram.authorization.selector")
	require.NotContains(t, attrs, "gram.authorization.policy")
}

func TestRecordAuthorizationDecisionOmitsSessionCredentials(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	sessionToken := "must-not-appear-in-telemetry"
	ctx := contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_123",
		UserID:               "user_123",
		SessionID:            &sessionToken,
	}, false)
	ctx, span := provider.Tracer("test").Start(ctx, "request")
	RecordAuthorizationDecision(ctx, repo.OperationRequire, repo.OutcomeAllow, repo.ReasonGrantMatched)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1)
	attrs := decisionAttributeMap(spans[0].Events[0].Attributes)
	require.NotContains(t, attrs, "gram.authorization.session_id")
	for _, value := range attrs {
		require.NotEqual(t, sessionToken, value)
	}
}

func TestRecordAuthorizationDecisionBoundsUnknownValues(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	ctx, span := provider.Tracer("test").Start(t.Context(), "request")
	RecordAuthorizationDecision(ctx, repo.Operation("attacker-operation"), repo.Outcome("attacker-outcome"), repo.Reason("attacker-reason"))
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	attrs := decisionAttributeMap(spans[0].Events[0].Attributes)
	require.Equal(t, boundedUnknownValue, attrs["gram.authorization.operation"])
	require.Equal(t, boundedUnknownValue, attrs["gram.authorization.result"])
	require.Equal(t, boundedUnknownValue, attrs["gram.authorization.reason"])
}

func decisionAttributeMap(attrs []attribute.KeyValue) map[string]string {
	result := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		result[string(attr.Key)] = attr.Value.AsString()
	}
	return result
}
