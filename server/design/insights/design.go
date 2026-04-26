package insights

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("insights", func() {
	Description("Manage AI Insights proposals and workspace memory.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("proposeToolVariation", func() {
		Description("Agent proposes an edit to a tool variation. Inserts a pending proposal row.")

		Payload(func() {
			Extend(ProposeToolVariationForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ProposalResult)

		HTTP(func() {
			POST("/rpc/insights.proposeToolVariation")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "proposeToolVariation")
		Meta("openapi:extension:x-speakeasy-name-override", "proposeToolVariation")
	})

	Method("proposeToolsetChange", func() {
		Description("Agent proposes a change to a toolset (add/remove tools, rename).")

		Payload(func() {
			Extend(ProposeToolsetChangeForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ProposalResult)

		HTTP(func() {
			POST("/rpc/insights.proposeToolsetChange")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "proposeToolsetChange")
		Meta("openapi:extension:x-speakeasy-name-override", "proposeToolsetChange")
	})

	Method("listProposals", func() {
		Description("List proposals for the active project.")

		Payload(func() {
			Attribute("status", String, "Optional status filter (pending, applied, dismissed, superseded, rolled_back)")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListProposalsResult)

		HTTP(func() {
			GET("/rpc/insights.listProposals")
			Param("status")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listProposals")
		Meta("openapi:extension:x-speakeasy-name-override", "listProposals")
	})

	Method("applyProposal", func() {
		Description("Apply a pending proposal to the underlying resource.")

		Payload(func() {
			Extend(ApplyProposalForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ProposalResult)

		HTTP(func() {
			POST("/rpc/insights.applyProposal")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "applyProposal")
		Meta("openapi:extension:x-speakeasy-name-override", "applyProposal")
	})

	Method("rollbackProposal", func() {
		Description("Roll back an applied proposal by writing the snapshotted current_value back.")

		Payload(func() {
			Extend(RollbackProposalForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ProposalResult)

		HTTP(func() {
			POST("/rpc/insights.rollbackProposal")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "rollbackProposal")
		Meta("openapi:extension:x-speakeasy-name-override", "rollbackProposal")
	})

	Method("dismissProposal", func() {
		Description("Mark a proposal as dismissed.")

		Payload(func() {
			Extend(DismissProposalForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ProposalResult)

		HTTP(func() {
			POST("/rpc/insights.dismissProposal")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "dismissProposal")
		Meta("openapi:extension:x-speakeasy-name-override", "dismissProposal")
	})

	Method("forgetMemoryById", func() {
		Description("UI-driven delete of a memory the agent created. Audit-logged.")

		Payload(func() {
			Extend(ForgetMemoryForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MemoryResult)

		HTTP(func() {
			POST("/rpc/insights.forgetMemoryById")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "forgetMemoryById")
		Meta("openapi:extension:x-speakeasy-name-override", "forgetMemoryById")
	})

	Method("rememberFact", func() {
		Description("Agent writes a memory (fact, playbook, glossary) into workspace memory.")

		Payload(func() {
			Extend(RememberFactForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MemoryResult)

		HTTP(func() {
			POST("/rpc/insights.rememberFact")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "rememberFact")
		Meta("openapi:extension:x-speakeasy-name-override", "rememberFact")
	})

	Method("forgetMemory", func() {
		Description("Agent or user deletes a memory by id (agent-side).")

		Payload(func() {
			Extend(ForgetMemoryForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MemoryResult)

		HTTP(func() {
			POST("/rpc/insights.forgetMemory")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "forgetMemory")
		Meta("openapi:extension:x-speakeasy-name-override", "forgetMemory")
	})

	Method("listMemories", func() {
		Description("List memories for the active project. Supports recall ranking by tags + recency.")

		Payload(func() {
			Attribute("kind", String, "Optional memory kind filter (fact, playbook, glossary, finding)")
			Attribute("tags", ArrayOf(String), "Optional tags to rank/filter by")
			Attribute("limit", Int, "Max number of memories to return (default 50, max 200)")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListMemoriesResult)

		HTTP(func() {
			GET("/rpc/insights.listMemories")
			Param("kind")
			Param("tags")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMemories")
		Meta("openapi:extension:x-speakeasy-name-override", "listMemories")
	})

	Method("recordFinding", func() {
		Description("Agent records an investigation finding. Sugar over rememberFact with kind=finding and short TTL.")

		Payload(func() {
			Extend(RecordFindingForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MemoryResult)

		HTTP(func() {
			POST("/rpc/insights.recordFinding")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "recordFinding")
		Meta("openapi:extension:x-speakeasy-name-override", "recordFinding")
	})
})

// ---- Forms ----

var ProposeToolVariationForm = Type("ProposeToolVariationForm", func() {
	Required("tool_name", "proposed_value")

	Attribute("tool_name", String, "The source tool name to vary")
	Attribute("proposed_value", String, "JSON-encoded proposed variation override fields")
	Attribute("current_value", String, "Optional JSON-encoded snapshot of the current variation; server falls back to live read if missing")
	Attribute("reasoning", String, "The agent's justification for the proposal")
	Attribute("source_chat_id", String, "Optional chat ID that produced the proposal")
})

var ProposeToolsetChangeForm = Type("ProposeToolsetChangeForm", func() {
	Required("toolset_slug", "proposed_value")

	Attribute("toolset_slug", String, "The slug of the toolset to change")
	Attribute("proposed_value", String, "JSON-encoded proposed toolset change (add_tools, remove_tools, new_name)")
	Attribute("current_value", String, "Optional JSON-encoded snapshot of the current toolset state")
	Attribute("reasoning", String, "The agent's justification for the change")
	Attribute("source_chat_id", String, "Optional chat ID that produced the proposal")
})

var ApplyProposalForm = Type("ApplyProposalForm", func() {
	Required("proposal_id")

	Attribute("proposal_id", String, "The ID of the proposal to apply")
	Attribute("force", Boolean, "If true, override staleness checks and apply anyway")
})

var RollbackProposalForm = Type("RollbackProposalForm", func() {
	Required("proposal_id")

	Attribute("proposal_id", String, "The ID of the applied proposal to roll back")
	Attribute("force", Boolean, "If true, override drift checks and roll back anyway")
})

var DismissProposalForm = Type("DismissProposalForm", func() {
	Required("proposal_id")

	Attribute("proposal_id", String, "The ID of the proposal to dismiss")
})

var RememberFactForm = Type("RememberFactForm", func() {
	Required("kind", "content")

	Attribute("kind", String, "The memory kind: fact, playbook, glossary, or finding", func() {
		Enum("fact", "playbook", "glossary", "finding")
	})
	Attribute("content", String, "The memory content (max 2000 chars)")
	Attribute("tags", ArrayOf(String), "Tags for recall")
	Attribute("source_chat_id", String, "Optional chat ID that produced the memory")
})

var ForgetMemoryForm = Type("ForgetMemoryForm", func() {
	Required("memory_id")

	Attribute("memory_id", String, "The ID of the memory to forget")
})

var RecordFindingForm = Type("RecordFindingForm", func() {
	Required("content")

	Attribute("content", String, "The investigation finding (max 2000 chars)")
	Attribute("tags", ArrayOf(String), "Tags for recall")
	Attribute("source_chat_id", String, "Optional chat ID that produced the finding")
})

// ---- Result types ----

var Proposal = Type("Proposal", func() {
	Meta("struct:pkg:path", "types")

	Required(
		"id", "kind", "target_ref", "current_value", "proposed_value",
		"status", "created_at",
	)

	Attribute("id", String, "Proposal ID")
	Attribute("kind", String, "Proposal kind: tool_variation or toolset_change")
	Attribute("target_ref", String, "Target tool name or toolset slug")
	Attribute("current_value", String, "JSON-encoded snapshot of the resource at proposal time")
	Attribute("proposed_value", String, "JSON-encoded proposed value")
	Attribute("applied_value", String, "JSON-encoded value actually written at apply time")
	Attribute("reasoning", String, "Agent reasoning")
	Attribute("source_chat_id", String, "Source chat ID, if any")
	Attribute("status", String, "pending, applied, dismissed, superseded, rolled_back")
	Attribute("created_at", String, "Creation timestamp (RFC3339)")
	Attribute("applied_at", String, "Applied timestamp")
	Attribute("dismissed_at", String, "Dismissed timestamp")
	Attribute("rolled_back_at", String, "Rolled back timestamp")
	Attribute("applied_by_user_id", String, "User that applied")
	Attribute("dismissed_by_user_id", String, "User that dismissed")
	Attribute("rolled_back_by_user_id", String, "User that rolled back")
})

var Memory = Type("Memory", func() {
	Meta("struct:pkg:path", "types")

	Required("id", "kind", "content", "tags", "usefulness_score", "last_used_at", "created_at")

	Attribute("id", String, "Memory ID")
	Attribute("kind", String, "Memory kind: fact, playbook, glossary, finding")
	Attribute("content", String, "Memory content")
	Attribute("tags", ArrayOf(String), "Memory tags")
	Attribute("source_chat_id", String, "Source chat ID, if any")
	Attribute("usefulness_score", Int, "Recall score")
	Attribute("expires_at", String, "Expiry timestamp")
	Attribute("last_used_at", String, "Last used timestamp")
	Attribute("created_at", String, "Created timestamp")
})

var ProposalResult = Type("ProposalResult", func() {
	Required("proposal")

	Attribute("proposal", Proposal)
})

var ListProposalsResult = Type("ListProposalsResult", func() {
	Required("proposals")

	Attribute("proposals", ArrayOf(Proposal))
})

var MemoryResult = Type("MemoryResult", func() {
	Required("memory")

	Attribute("memory", Memory)
})

var ListMemoriesResult = Type("ListMemoriesResult", func() {
	Required("memories")

	Attribute("memories", ArrayOf(Memory))
})
