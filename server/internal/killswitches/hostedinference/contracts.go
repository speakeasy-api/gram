package hostedinference

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

const (
	DefinitionKeyAIAccess                                                = killswitches.DefinitionKeyAIAccess
	PrincipalKindUser                                                    = killswitches.PrincipalKindUser
	ResourceKindGramHostedInference     killswitches.ResourceKind        = "gram_hosted_inference"
	SurfaceGramHostedInference          killswitches.Surface             = "gram_hosted_inference"
	TransportAdapterGramHostedInference killswitches.TransportAdapterKey = "gram_hosted_inference"
)

// CallClass states why one provider call is governed or deliberately outside
// DNO-991. The zero value is intentionally invalid.
type CallClass string

const (
	CallClassGovernedUser CallClass = "governed_user"
	CallClassInternal     CallClass = "internal"
	CallClassBackground   CallClass = "background"
	CallClassUnsupported  CallClass = "unsupported"
)

// CallCategory is a static identity for a current production inference path.
// New paths must be added to the inventory below before they can egress through
// a production-injected ChatClient.
type CallCategory string

const (
	// Governed user paths. These are also canonical ai_access resources.
	CallCategoryUserChatCompletion            CallCategory = "user_chat_completion"
	CallCategoryChatSummary                   CallCategory = "chat_summary"
	CallCategoryToolCallSummary               CallCategory = "tool_call_summary"
	CallCategoryRiskAuthoring                 CallCategory = "risk_authoring"
	CallCategoryBusinessMemorySearchEmbedding CallCategory = "business_memory_search_embedding"

	// Unsupported request identities. Attribution fields on these requests
	// never become acting users.
	CallCategoryAPIKeyChat                      CallCategory = "api_key_chat" //nolint:gosec // This names an authentication category, not a credential.
	CallCategoryChatSessionChat                 CallCategory = "chat_session_chat"
	CallCategoryNonOrdinaryGramSessionChat      CallCategory = "non_ordinary_gram_session_chat"
	CallCategoryAPIKeyChatSummary               CallCategory = "api_key_chat_summary"      //nolint:gosec // This names an authentication category, not a credential.
	CallCategoryAPIKeyToolCallSummary           CallCategory = "api_key_tool_call_summary" //nolint:gosec // This names an authentication category, not a credential.
	CallCategoryAPIKeyRiskAuthoring             CallCategory = "api_key_risk_authoring"    //nolint:gosec // This names an authentication category, not a credential.
	CallCategoryAPIKeyBusinessMemorySearch      CallCategory = "api_key_business_memory_search"
	CallCategoryNonOrdinarySessionChatSummary   CallCategory = "non_ordinary_session_chat_summary"
	CallCategoryNonOrdinarySessionToolSummary   CallCategory = "non_ordinary_session_tool_call_summary"
	CallCategoryNonOrdinarySessionRiskAuthoring CallCategory = "non_ordinary_session_risk_authoring"
	CallCategoryNonOrdinarySessionMemorySearch  CallCategory = "non_ordinary_session_business_memory_search"

	// Explicit internal/background paths that preserve existing behavior.
	CallCategoryAutomaticChatTitle  CallCategory = "automatic_chat_title"
	CallCategoryChatResolution      CallCategory = "chat_resolution"
	CallCategoryChatAnalysis        CallCategory = "chat_analysis"
	CallCategoryPromptScanner       CallCategory = "prompt_scanner"
	CallCategorySkillJudge          CallCategory = "skill_judge"
	CallCategoryBusinessMemoryJudge CallCategory = "business_memory_judge"
	CallCategoryRAGIndexing         CallCategory = "rag_indexing"

	// Assistant-owned paths are explicitly deferred rather than attributed to
	// an assistant owner or another substitute identity.
	CallCategoryAssistantChat     CallCategory = "assistant_chat"
	CallCategoryAssistantMemory   CallCategory = "assistant_memory"
	CallCategoryAssistantResearch CallCategory = "assistant_research"
	CallCategoryAssistantRAG      CallCategory = "assistant_rag"
)

var categoryClasses = map[CallCategory]CallClass{
	CallCategoryUserChatCompletion:              CallClassGovernedUser,
	CallCategoryChatSummary:                     CallClassGovernedUser,
	CallCategoryToolCallSummary:                 CallClassGovernedUser,
	CallCategoryRiskAuthoring:                   CallClassGovernedUser,
	CallCategoryBusinessMemorySearchEmbedding:   CallClassGovernedUser,
	CallCategoryAPIKeyChat:                      CallClassUnsupported,
	CallCategoryChatSessionChat:                 CallClassUnsupported,
	CallCategoryNonOrdinaryGramSessionChat:      CallClassUnsupported,
	CallCategoryAPIKeyChatSummary:               CallClassUnsupported,
	CallCategoryAPIKeyToolCallSummary:           CallClassUnsupported,
	CallCategoryAPIKeyRiskAuthoring:             CallClassUnsupported,
	CallCategoryAPIKeyBusinessMemorySearch:      CallClassUnsupported,
	CallCategoryNonOrdinarySessionChatSummary:   CallClassUnsupported,
	CallCategoryNonOrdinarySessionToolSummary:   CallClassUnsupported,
	CallCategoryNonOrdinarySessionRiskAuthoring: CallClassUnsupported,
	CallCategoryNonOrdinarySessionMemorySearch:  CallClassUnsupported,
	CallCategoryAutomaticChatTitle:              CallClassBackground,
	CallCategoryChatResolution:                  CallClassBackground,
	CallCategoryChatAnalysis:                    CallClassInternal,
	CallCategoryPromptScanner:                   CallClassInternal,
	CallCategorySkillJudge:                      CallClassBackground,
	CallCategoryBusinessMemoryJudge:             CallClassBackground,
	CallCategoryRAGIndexing:                     CallClassBackground,
	CallCategoryAssistantChat:                   CallClassUnsupported,
	CallCategoryAssistantMemory:                 CallClassUnsupported,
	CallCategoryAssistantResearch:               CallClassUnsupported,
	CallCategoryAssistantRAG:                    CallClassUnsupported,
}

// Classification is an opaque, server-authored call classification.
type Classification struct {
	class      CallClass
	category   CallCategory
	actingUser contextvalues.ActingUserProvenance
}

type classificationContextKey struct{}

// WithGovernedUser classifies a governed call only when ctx carries validated
// ordinary Gram-session provenance directly or through a qualifying chat JWT.
func WithGovernedUser(ctx context.Context, category CallCategory) (context.Context, error) {
	if err := validateCategoryClass(category, CallClassGovernedUser); err != nil {
		return ctx, err
	}
	actingUser, ok := contextvalues.ValidatedHostedInferenceActingUser(ctx)
	if !ok {
		return ctx, fmt.Errorf("validated session-backed acting user is required")
	}
	return context.WithValue(ctx, classificationContextKey{}, Classification{class: CallClassGovernedUser, category: category, actingUser: actingUser}), nil
}

// WithGovernedUserOrUnsupported governs an ordinary validated Gram session or
// a chat JWT carrying matching ordinary-session provenance. API-key and other
// non-ordinary categories never use owner, creator, external, or absent fields.
func WithGovernedUserOrUnsupported(ctx context.Context, governed, apiKey, nonOrdinary CallCategory) (context.Context, error) {
	if _, ok := contextvalues.ValidatedHostedInferenceActingUser(ctx); ok {
		return WithGovernedUser(ctx, governed)
	}
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx != nil && authCtx.APIKeyID != "" {
		return WithUnsupported(ctx, apiKey)
	}
	return WithUnsupported(ctx, nonOrdinary)
}

func WithInternal(ctx context.Context, category CallCategory) (context.Context, error) {
	return withBypass(ctx, category, CallClassInternal)
}

func WithBackground(ctx context.Context, category CallCategory) (context.Context, error) {
	return withBypass(ctx, category, CallClassBackground)
}

func WithUnsupported(ctx context.Context, category CallCategory) (context.Context, error) {
	return withBypass(ctx, category, CallClassUnsupported)
}

func withBypass(ctx context.Context, category CallCategory, class CallClass) (context.Context, error) {
	if err := validateCategoryClass(category, class); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, classificationContextKey{}, Classification{class: class, category: category, actingUser: nil}), nil
}

func validateCategoryClass(category CallCategory, class CallClass) error {
	expected, ok := categoryClasses[category]
	if !ok {
		return fmt.Errorf("unknown hosted-inference category %q", category)
	}
	if expected != class {
		return fmt.Errorf("hosted-inference category %q requires class %q", category, expected)
	}
	return nil
}

func classificationFromContext(ctx context.Context) (Classification, bool) {
	classification, ok := ctx.Value(classificationContextKey{}).(Classification)
	return classification, ok
}

func isGovernedCategory(category CallCategory) bool {
	return categoryClasses[category] == CallClassGovernedUser
}
