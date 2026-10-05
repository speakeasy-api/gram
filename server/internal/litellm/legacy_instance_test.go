package litellm

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooks"
)

// blockingLiteLLMAIAccess records evaluations and blocks every request, so a
// test can prove a caller never reached the checkpoint.
type blockingLiteLLMAIAccess struct{ calls *int }

func (b blockingLiteLLMAIAccess) Evaluate(context.Context, *gen.IngestPayload, *contextvalues.AuthContext) liteLLMAIAccessDecision {
	*b.calls++
	return liteLLMIdentityFailureDecision()
}

// TestLegacyInstanceKeysSkipAIAccessAndKeepCallbackAttribution: instances
// created before the acting-principal contract cannot forward assertions, so
// they keep the pre-contract guardrail behavior until re-provisioned.
func TestLegacyInstanceKeysSkipAIAccessAndKeepCallbackAttribution(t *testing.T) {
	t.Parallel()
	authCtx := testAuthContext()
	authCtx.APIKeyScopes = slices.DeleteFunc(slices.Clone(authCtx.APIKeyScopes), func(scope string) bool {
		return scope == auth.APIKeyScopeLiteLLMActingPrincipal.String()
	})
	ingester := &captureIngester{result: allowResult(hooks.ResolvedActor{UserID: "resolved-user", Email: "member@example.test"}), err: nil, calls: nil}
	service := unitService(t, ingester, authCtx)
	evaluations := 0
	service.aiAccess = blockingLiteLLMAIAccess{calls: &evaluations}

	payload := testPayload()
	payload.Texts = []string{"legacy prompt"}
	payload.RequestData.UserAPIKeyUserEmail = new("  Member@Example.Test ")

	result, err := service.Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	require.Zero(t, evaluations, "legacy instance keys must not enter the ai_access checkpoint")
	require.Len(t, ingester.calls, 1)
	require.Equal(t, "member@example.test", *ingester.calls[0].payload.Source.UserEmail)
	require.Empty(t, ingester.calls[0].auth.UserID)
}

func TestGovernedInstanceKeysEnterAIAccess(t *testing.T) {
	t.Parallel()
	authCtx := testAuthContext()
	require.Contains(t, authCtx.APIKeyScopes, auth.APIKeyScopeLiteLLMActingPrincipal.String())
	ingester := &captureIngester{result: allowResult(hooks.ResolvedActor{UserID: "resolved-user", Email: "member@example.test"}), err: nil, calls: nil}
	service := unitService(t, ingester, authCtx)
	evaluations := 0
	service.aiAccess = blockingLiteLLMAIAccess{calls: &evaluations}

	payload := testPayload()
	payload.Texts = []string{"governed prompt"}

	result, err := service.Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("BLOCKED"), result.Action)
	require.Equal(t, 1, evaluations)
	require.Empty(t, ingester.calls)
}
