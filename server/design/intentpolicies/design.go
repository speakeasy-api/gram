package intentpolicies

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("intentPolicies", func() {
	Description("Staff-only retroactive analysis of recorded tool calls against the intent-based guardrail. Read-only apart from storing uploaded transcripts; nothing is enforced.")
	Meta("openapi:extension:x-speakeasy-group", "intentPolicies")

	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("getGuardrail", func() {
		Description("Get the built-in intent guardrail: grounding statements, label questions, and contracts. Requires the intent-policies rollout for the organization.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(IntentGuardrail)

		HTTP(func() {
			GET("/rpc/intentPolicies.getGuardrail")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getIntentGuardrail")
		Meta("openapi:extension:x-speakeasy-name-override", "getGuardrail")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "IntentGuardrail"}`)
	})

	Method("analyzeSession", func() {
		Description("Label every tool call of one chat session and evaluate the guardrail contracts. Synchronous, judges at most the first 200 calls of the latest generation by default, and persists nothing. Use offset and limit to analyze in batches. Requires the intent-policies rollout for the organization.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("chat_id", String, "The chat session to analyze.", func() {
				Format(FormatUUID)
			})
			Attribute("evaluator", String, "The label evaluator.", func() {
				Enum("jev", "gemini")
			})
			Attribute("generation", Int32, "Chat generation to analyze. Defaults to the latest.", func() {
				Minimum(0)
			})
			Attribute("offset", Int32, "Index of the first tool call to analyze, for analyzing a session in batches. Totals still count every call.", func() {
				Minimum(0)
				Default(0)
			})
			Attribute("limit", Int32, "Most tool calls to analyze from offset. Defaults to all, up to 200.", func() {
				Minimum(1)
				Maximum(200)
			})
			Required("chat_id", "evaluator")
		})

		Result(IntentSessionAnalysis)

		HTTP(func() {
			POST("/rpc/intentPolicies.analyzeSession")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "analyzeIntentSession")
		Meta("openapi:extension:x-speakeasy-name-override", "analyzeSession")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AnalyzeIntentSession", "type": "mutation"}`)
	})

	Method("uploadTranscript", func() {
		Description("Store an agent transcript (messages JSON, Claude Code JSONL, or Pi JSONL) as a chat session in the current project so it can be analyzed. Requires the intent-policies rollout for the organization.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("name", String, "Title for the stored session, usually the file name.", func() {
				MinLength(1)
				MaxLength(255)
			})
			Attribute("content", String, "The transcript file contents, at most 20 MiB.", func() {
				MinLength(1)
				// Characters, not bytes; the service also enforces intentpolicy.MaxTranscriptBytes.
				MaxLength(20 << 20)
			})
			Required("name", "content")
		})

		Result(UploadTranscriptResult)

		HTTP(func() {
			POST("/rpc/intentPolicies.uploadTranscript")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "uploadIntentTranscript")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadTranscript")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UploadIntentTranscript", "type": "mutation"}`)
	})
})

var IntentLabelOption = Type("IntentLabelOption", func() {
	Attribute("key", String, "Option value exposed to contracts.")
	Attribute("description", String, "What the option means.")
	Attribute("ordinal", Int, "Order of the option; higher is better understood.")
	Attribute("uncertain", Boolean, "True when the option reports rank -1.")
	Required("key", "description", "ordinal", "uncertain")
})

var IntentLabelQuestion = Type("IntentLabelQuestion", func() {
	Attribute("key", String, "Label key, read by contracts as label.<key>.")
	Attribute("kind", String, "Question shape.", func() {
		Enum("noul", "multi_noul", "choice")
	})
	Attribute("instructions", String, "The question the evaluator answers.")
	Attribute("options", ArrayOf(IntentLabelOption), "Values of a choice or multi_noul label.")
	Required("key", "kind", "instructions", "options")
})

var IntentCondition = Type("IntentCondition", func() {
	Attribute("join", String, "Links the clause to the previous one; empty on the first clause.", func() {
		Enum("", "and", "or")
	})
	Attribute("label", String, "Label key the clause reads. Empty for a shell command pattern.")
	Attribute("op", String, "How the label compares to values.", func() {
		Enum("", "is", "is not", "includes", "includes anything other than")
	})
	Attribute("values", ArrayOf(String), "Values compared: \"true\" for a yes/no label, else option keys. Several read as any of.")
	Attribute("pattern", String, "Regular expression a shell command must match, for a pattern clause.")
	Required("join", "label", "op", "values", "pattern")
})

var IntentContract = Type("IntentContract", func() {
	Attribute("id", String, "Stable contract id.")
	Attribute("name", String, "Display name.")
	Attribute("statement", String, "Grounding statement the contract enforces.")
	Attribute("outcome", String, "Decision when the contract fires.", func() {
		Enum("block", "clarify")
	})
	Attribute("conditions", ArrayOf(IntentCondition), "When the contract fires, for display.")
	Attribute("expression", String, "CEL expression returning PERMIT, CLARIFY, BLOCK, or ABSTAIN.")
	Required("id", "name", "statement", "outcome", "conditions", "expression")
})

var IntentGuardrail = Type("IntentGuardrail", func() {
	Attribute("id", String, "Guardrail id.")
	Attribute("version", String, "Guardrail version.")
	Attribute("statements", ArrayOf(String), "Grounding statements fed to the evaluator.")
	Attribute("questions", ArrayOf(IntentLabelQuestion), "Labels inferred for every call.")
	Attribute("contracts", ArrayOf(IntentContract), "Contracts evaluated over the labels.")
	Required("id", "version", "statements", "questions", "contracts")
})

var IntentEvaluatorDescriptor = Type("IntentEvaluatorDescriptor", func() {
	Attribute("evaluator", String, "Evaluator kind.")
	Attribute("provider", String, "Inference transport.")
	Attribute("model", String, "Model id.")
	Attribute("prompt_version", String, "Question compilation version.")
	Required("evaluator", "provider", "model", "prompt_version")
})

var IntentLabelValue = Type("IntentLabelValue", func() {
	Attribute("key", String, "Label key.")
	Attribute("value", Any, "Boolean, string, or list of strings, by label kind.")
	Attribute("rank", Int, "Ordinal rank; -1 when categorical or uncertain.")
	Attribute("probability", Float64, "Probability of the selected value.")
	Attribute("distribution", MapOf(String, Float64), "Per-option probabilities, when the evaluator gives them.")
	Attribute("rationale", String, "Evaluator explanation, when given.")
	Required("key", "value", "rank", "probability")
})

var IntentContractOutcome = Type("IntentContractOutcome", func() {
	Attribute("contract_id", String, "Contract id.")
	Attribute("result", String, "permit, clarify, block, or abstain; absent when the contract errored.")
	Attribute("error", String, "Evaluation error, when any.")
	Attribute("inputs", MapOf(String, Any), "Every label or tool path the contract reads, with its value (null when missing).")
	Required("contract_id", "inputs")
})

var IntentCallUsage = Type("IntentCallUsage", func() {
	Attribute("input_tokens", Int64, "Prompt tokens.")
	Attribute("output_tokens", Int64, "Completion tokens.")
	Attribute("cost_usd", Float64, "Provider-reported cost; zero when unreported.")
	Attribute("latency_ms", Int64, "Evaluator latency.")
	Required("input_tokens", "output_tokens", "cost_usd", "latency_ms")
})

var IntentCallAssessment = Type("IntentCallAssessment", func() {
	Attribute("message_id", String, "Assistant message that carried the call.", func() {
		Format(FormatUUID)
	})
	Attribute("seq", Int64, "Message sequence number.")
	Attribute("tool_call_id", String, "Provider call id.")
	Attribute("tool_name", String, "Raw tool name.")
	Attribute("mcp_server", String, "MCP server, for MCP tools.")
	Attribute("mcp_function", String, "MCP function, for MCP tools.")
	Attribute("arguments", String, "Arguments exactly as recorded.")
	Attribute("occurred_at", String, "When the call was recorded.", func() {
		Format(FormatDateTime)
	})
	Attribute("labels", ArrayOf(IntentLabelValue), "Inferred labels in guardrail order; missing labels are absent.")
	Attribute("decision", String, "Combined decision.", func() {
		Enum("permit", "clarify", "block")
	})
	Attribute("decided_by", ArrayOf(String), "Contracts that produced the decision.")
	Attribute("outcomes", ArrayOf(IntentContractOutcome), "Every contract's result, in guardrail order.")
	Attribute("evaluator_error", String, "Set when the evaluator failed to answer every label.")
	Attribute("usage", IntentCallUsage, "Evaluator cost for this call.")
	Required("message_id", "seq", "tool_call_id", "tool_name", "arguments", "occurred_at", "labels", "decision", "decided_by", "outcomes", "usage")
})

var IntentAnalysisTotals = Type("IntentAnalysisTotals", func() {
	Attribute("calls", Int, "Tool calls in the session.")
	Attribute("judged", Int, "Tool calls assessed.")
	Attribute("blocked", Int, "Block decisions, including evaluator failures.")
	Attribute("clarified", Int, "Clarify decisions.")
	Attribute("permitted", Int, "Permit decisions.")
	Attribute("cost_usd", Float64, "Summed evaluator cost.")
	Attribute("latency_ms", Int64, "Summed evaluator latency.")
	Required("calls", "judged", "blocked", "clarified", "permitted", "cost_usd", "latency_ms")
})

var IntentSessionAnalysis = Type("IntentSessionAnalysis", func() {
	Attribute("chat_id", String, "The analyzed session.", func() {
		Format(FormatUUID)
	})
	Attribute("generation", Int32, "The analyzed chat generation.")
	Attribute("guardrail_version", String, "Guardrail version applied.")
	Attribute("evaluator", IntentEvaluatorDescriptor, "Evaluator that produced the labels.")
	Attribute("assessments", ArrayOf(IntentCallAssessment), "One entry per judged call, in session order.")
	Attribute("totals", IntentAnalysisTotals, "Summary counts.")
	Attribute("limit_hit", Boolean, "True when the session had more calls than were judged.")
	Required("chat_id", "generation", "guardrail_version", "evaluator", "assessments", "totals", "limit_hit")
})

var UploadTranscriptResult = Type("UploadTranscriptResult", func() {
	Attribute("chat_id", String, "The stored session.", func() {
		Format(FormatUUID)
	})
	Attribute("format", String, "Detected transcript format.")
	Attribute("message_count", Int, "Messages stored.")
	Required("chat_id", "format", "message_count")
})
