package chat

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
)

func classifyChatInference(ctx context.Context, keySlot billing.ModelUsageSource) (context.Context, error) {
	if _, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		return wrapHostedInferenceClassification(hostedinference.WithUnsupported(ctx, hostedinference.CallCategoryAssistantChat))
	}
	if keySlot == billing.ModelUsageSourceElements {
		if _, ok := contextvalues.ValidatedHostedInferenceActingUser(ctx); ok {
			return wrapHostedInferenceClassification(hostedinference.WithGovernedUser(ctx, hostedinference.CallCategoryUserChatCompletion))
		}
		return wrapHostedInferenceClassification(hostedinference.WithUnsupported(ctx, hostedinference.CallCategoryChatSessionChat))
	}
	return wrapHostedInferenceClassification(hostedinference.WithGovernedUserOrUnsupported(
		ctx,
		hostedinference.CallCategoryUserChatCompletion,
		hostedinference.CallCategoryAPIKeyChat,
		hostedinference.CallCategoryNonOrdinaryGramSessionChat,
	))
}

func classifySessionInference(ctx context.Context, governed, apiKey, nonOrdinary hostedinference.CallCategory) (context.Context, error) {
	if _, ok := contextvalues.ValidatedGramSessionActingUser(ctx); ok {
		return wrapHostedInferenceClassification(hostedinference.WithGovernedUser(ctx, governed))
	}
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx != nil && authCtx.APIKeyID != "" {
		return wrapHostedInferenceClassification(hostedinference.WithUnsupported(ctx, apiKey))
	}
	return wrapHostedInferenceClassification(hostedinference.WithUnsupported(ctx, nonOrdinary))
}

func wrapHostedInferenceClassification(classified context.Context, err error) (context.Context, error) {
	if err != nil {
		return classified, fmt.Errorf("classify hosted inference: %w", err)
	}
	return classified, nil
}
