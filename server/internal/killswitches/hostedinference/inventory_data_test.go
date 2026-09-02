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

// standaloneCommandExclusions are report/benchmark binaries that deliberately
// construct an uninjected client. They are not production Gram compositions.
var standaloneCommandExclusions = []string{
	"risk-pi-report",
	"riskjudgebench",
	"skillefficacybench",
	"skillsuggestbench",
}
