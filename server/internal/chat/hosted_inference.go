package chat

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type hostedInferencePreflighter interface {
	PreflightHostedInference(context.Context, string) error
}

func classifyChatInference(ctx context.Context, authCtx *contextvalues.AuthContext, keySlot billing.ModelUsageSource) (context.Context, error) {
	if _, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		return hostedinference.WithUnsupported(ctx, hostedinference.CallCategoryAssistantChat)
	}
	if _, ok := contextvalues.ValidatedHostedInferenceActingUser(ctx); ok {
		return hostedinference.WithGovernedUser(ctx, hostedinference.CallCategoryUserChatCompletion)
	}
	if keySlot == billing.ModelUsageSourceElements {
		return hostedinference.WithUnsupported(ctx, hostedinference.CallCategoryChatSessionChat)
	}
	if authCtx != nil && authCtx.APIKeyID != "" {
		return hostedinference.WithUnsupported(ctx, hostedinference.CallCategoryAPIKeyChat)
	}
	return hostedinference.WithUnsupported(ctx, hostedinference.CallCategoryNonOrdinaryGramSessionChat)
}

func classifySessionInference(ctx context.Context, governed, apiKey, nonOrdinary hostedinference.CallCategory) (context.Context, error) {
	if _, ok := contextvalues.ValidatedGramSessionActingUser(ctx); ok {
		return hostedinference.WithGovernedUser(ctx, governed)
	}
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx != nil && authCtx.APIKeyID != "" {
		return hostedinference.WithUnsupported(ctx, apiKey)
	}
	return hostedinference.WithUnsupported(ctx, nonOrdinary)
}

func mapHostedInferenceError(ctx context.Context, logger *slog.Logger, err error) (error, bool) {
	shareable, ok := hostedinference.AsShareableError(err)
	if !ok {
		return nil, false
	}
	if shareable.Code == oops.CodeAIAccessDenied {
		return shareable, true
	}
	return shareable.LogError(ctx, logger), true
}
