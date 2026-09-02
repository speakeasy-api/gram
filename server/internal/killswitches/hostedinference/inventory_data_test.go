package hostedinference

// CallSiteClaim is one statically classified production call site. Function
// names make repeated methods in the same file independently reviewable.
type CallSiteClaim struct {
	Path     string
	Function string
	Method   string
	Category CallCategory
}

var productionCallSiteInventory = []CallSiteClaim{
	{"background/activities/chat_resolutions/analyze_segment.go", "analyzeWithLLM", "GetObjectCompletion", CallCategoryChatResolution},
	{"background/activities/chat_resolutions/segment_chat.go", "segmentWithLLM", "GetObjectCompletion", CallCategoryChatResolution},
	{"background/activities/generate_chat_title.go", "generateTitle", "GetCompletion", CallCategoryAutomaticChatTitle},
	{"businessmemory/impl.go", "SearchBusinessMemories", "CreateEmbeddings", CallCategoryBusinessMemorySearchEmbedding},
	{"businessmemory/judge.go", "persist", "CreateEmbeddings", CallCategoryBusinessMemoryJudge},
	{"chat/agent_client.go", "AgentChat", "GetCompletion", CallCategoryAssistantChat},
	{"chat/analysis/judge.go", "CallStructured", "GetObjectCompletion", CallCategoryChatAnalysis},
	{"chat/impl.go", "HandleCompletion", "GetCompletionStream", CallCategoryUserChatCompletion},
	{"chat/impl.go", "HandleCompletion", "GetCompletion", CallCategoryUserChatCompletion},
	{"chat/impl.go", "Summarize", "GetCompletion", CallCategoryChatSummary},
	{"chat/impl.go", "SummarizeToolCall", "GetCompletion", CallCategoryToolCallSummary},
	{"chat/turnstream_tee.go", "teedCompletion", "GetCompletionStream", CallCategoryUserChatCompletion},
	{"chat/turnstream_tee.go", "teedCompletion", "GetCompletion", CallCategoryUserChatCompletion},
	{"mcpapproval/researchagent/researchagent.go", "Run", "GetCompletion", CallCategoryAssistantResearch},
	{"mcpapproval/researchagent/researchagent.go", "extract", "GetObjectCompletion", CallCategoryAssistantResearch},
	{"memory/contradiction.go", "detectContradiction", "GetObjectCompletion", CallCategoryAssistantMemory},
	{"memory/service.go", "Remember", "CreateEmbeddings", CallCategoryAssistantMemory},
	{"memory/service.go", "Recall", "CreateEmbeddings", CallCategoryAssistantMemory},
	{"memory/service.go", "Forget", "CreateEmbeddings", CallCategoryAssistantMemory},
	{"platformtools/research/research.go", "Search", "GetCompletion", CallCategoryAssistantResearch},
	{"rag/search_tools.go", "SearchToolsetTools", "CreateEmbeddings", CallCategoryAssistantRAG},
	{"rag/search_tools.go", "generateEmbeddings", "CreateEmbeddings", CallCategoryRAGIndexing},
	{"risk/impl.go", "suggestCustomRuleViaLLM", "GetObjectCompletion", CallCategoryRiskAuthoring},
	{"risk/impl.go", "requestExclusionSuggestion", "GetObjectCompletion", CallCategoryRiskAuthoring},
	{"risk/impl.go", "generatePolicyName", "GetCompletion", CallCategoryRiskAuthoring},
	{"risk/impl.go", "generatePromptPolicyName", "GetCompletion", CallCategoryRiskAuthoring},
	{"scanners/promptinjection/openrouter/judge.go", "call", "GetCompletion", CallCategoryPromptScanner},
	{"scanners/promptpolicy/openrouter/judge.go", "call", "GetObjectCompletion", CallCategoryPromptScanner},
	{"skills/efficacy/judge.go", "call", "GetObjectCompletion", CallCategorySkillJudge},
	{"skills/suggest/judge.go", "Generate", "GetObjectCompletion", CallCategorySkillJudge},
}

// ConstructorKind identifies one supported ChatClient construction mode.
type ConstructorKind string

const (
	ConstructorProduction ConstructorKind = "NewUnifiedClient"
	ConstructorUnchecked  ConstructorKind = "NewUncheckedUnifiedClient"
)

// ConstructorClaim is an allowed repository construction site. The unchecked
// constructor is restricted to the explicit standalone commands above.
type ConstructorClaim struct {
	Path string
	Kind ConstructorKind
}

var repositoryConstructorInventory = []ConstructorClaim{
	{"cmd/gram/start.go", ConstructorProduction},
	{"cmd/gram/streams.go", ConstructorProduction},
	{"cmd/gram/worker.go", ConstructorProduction},
	{"cmd/risk-pi-report/main.go", ConstructorUnchecked},
	{"cmd/riskjudgebench/main.go", ConstructorUnchecked},
	{"cmd/skillefficacybench/main.go", ConstructorUnchecked},
	{"cmd/skillsuggestbench/main.go", ConstructorUnchecked},
}

type ClientConstructionClaim struct {
	Path             string
	Function         string
	Allocations      int
	CheckpointWrites int
}

var clientConstructionInventory = []ClientConstructionClaim{
	{"internal/thirdparty/openrouter/unified_client.go", "NewUnifiedClient", 0, 0},
	{"internal/thirdparty/openrouter/unified_client.go", "NewUncheckedUnifiedClient", 1, 0},
	{"internal/thirdparty/openrouter/unified_client.go", "WithHostedInferenceCheckpoint", 1, 1},
}

const (
	ProviderOperationHTTPDo        = "HTTPClient.Do"
	ProviderOperationHTTPRoundTrip = "HTTP.RoundTrip"
	ProviderOperationHTTPPackage   = "HTTP.PackageFunction"
	ProviderOperationOpenRouterSDK = "OpenRouterSDK.Operation"
)

// ProviderOperationClaim identifies one typed network operation implemented
// by the OpenRouter provider package. Calls in other packages are forbidden.
type ProviderOperationClaim struct {
	Path      string
	Function  string
	Operation string
}

// governedProviderOperationInventory contains model inference attempts that are
// covered by the hosted-inference checkpoint.
var governedProviderOperationInventory = []ProviderOperationClaim{
	{"internal/thirdparty/openrouter/unified_client.go", "Do", ProviderOperationHTTPDo},
	{"internal/thirdparty/openrouter/unified_client.go", "createEmbeddings", ProviderOperationOpenRouterSDK},
	{"internal/thirdparty/openrouter/unified_client.go", "makeHTTPRequest", ProviderOperationHTTPDo},
}

// excludedProviderOperationInventory contains reviewed metadata, control-plane,
// and background-accounting operations that do not perform hosted inference.
var excludedProviderOperationInventory = []ProviderOperationClaim{
	{"internal/thirdparty/openrouter/context_window.go", "fetchMin", ProviderOperationHTTPDo},
	{"internal/thirdparty/openrouter/openrouter.go", "GetKeyUsage", ProviderOperationHTTPDo},
	{"internal/thirdparty/openrouter/openrouter.go", "createOpenRouterAPIKey", ProviderOperationHTTPDo},
	{"internal/thirdparty/openrouter/openrouter.go", "getGenerationDetails", ProviderOperationHTTPDo},
	{"internal/thirdparty/openrouter/openrouter.go", "patchOpenRouterAPIKey", ProviderOperationHTTPDo},
	{"internal/thirdparty/openrouter/spend.go", "doSpendRequest", ProviderOperationHTTPDo},
}
