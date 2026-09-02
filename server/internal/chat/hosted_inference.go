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

func classifyChatSummaryInference(ctx context.Context) (context.Context, error) {
	return wrapHostedInferenceClassification(hostedinference.WithGovernedUserOrUnsupported(
		ctx,
		hostedinference.CallCategoryChatSummary,
		hostedinference.CallCategoryAPIKeyChatSummary,
		hostedinference.CallCategoryNonOrdinarySessionChatSummary,
	))
}

func classifyToolCallSummaryInference(ctx context.Context) (context.Context, error) {
	return wrapHostedInferenceClassification(hostedinference.WithGovernedUserOrUnsupported(
		ctx,
		hostedinference.CallCategoryToolCallSummary,
		hostedinference.CallCategoryAPIKeyToolCallSummary,
		hostedinference.CallCategoryNonOrdinarySessionToolSummary,
	))
}

func wrapHostedInferenceClassification(classified context.Context, err error) (context.Context, error) {
	if err != nil {
		return classified, fmt.Errorf("classify hosted inference: %w", err)
	}
	return classified, nil
}
