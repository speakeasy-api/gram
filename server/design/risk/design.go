package risk

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("risk", func() {
	Description("Manage risk analysis policies and view scan results.")
	Meta("openapi:extension:x-speakeasy-group", "risk")

	Security(security.ByKey, security.ProjectSlug, func() { Scope("producer") })
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("createRiskPolicy", func() {
		Description("Create a new risk analysis policy for the current project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("name", String, "The policy name. If omitted, a name will be auto-generated.")
			Attribute("sources", ArrayOf(String), "Detection sources to enable.")
			Attribute("presidio_entities", ArrayOf(String), "Presidio entity types to detect.")
			Attribute("prompt_injection_rules", ArrayOf(String), "Prompt-injection detection rule ids to enable in addition to the heuristic baseline (e.g. 'deberta-v3-classifier').")
			Attribute("enabled", Boolean, "Whether the policy is active.")
			Attribute("action", String, "Policy action: flag or block.", func() {
				shared.RiskPolicyActionEnum()
				Default("flag")
			})
			Attribute("auto_name", Boolean, "Whether the policy name should be auto-generated.")
			Attribute("user_message", String, "Optional message shown to end users when this policy blocks an action or surfaces a flagged finding.")
		})

		Result(shared.RiskPolicy)

		HTTP(func() {
			POST("/rpc/risk.policies.create")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createRiskPolicy")
		Meta("openapi:extension:x-speakeasy-group", "risk.policies")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskCreatePolicy", "type": "mutation"}`)
	})

	Method("listRiskPolicies", func() {
		Description("List all risk analysis policies for the current project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListRiskPoliciesResult)

		HTTP(func() {
			GET("/rpc/risk.policies.list")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRiskPolicies")
		Meta("openapi:extension:x-speakeasy-group", "risk.policies")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskListPolicies"}`)
	})

	Method("getRiskCapabilities", func() {
		Description("Get server-side risk analysis capabilities for the current project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(RiskCapabilitiesResult)

		HTTP(func() {
			GET("/rpc/risk.capabilities.get")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRiskCapabilities")
		Meta("openapi:extension:x-speakeasy-group", "risk.capabilities")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskCapabilities"}`)
	})

	Method("getRiskPolicy", func() {
		Description("Get a risk analysis policy by ID.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The policy ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(shared.RiskPolicy)

		HTTP(func() {
			GET("/rpc/risk.policies.get")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRiskPolicy")
		Meta("openapi:extension:x-speakeasy-group", "risk.policies")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
	})

	Method("updateRiskPolicy", func() {
		Description("Update a risk analysis policy.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The policy ID.", func() {
				Format(FormatUUID)
			})
			Attribute("name", String, "The policy name.")
			Attribute("sources", ArrayOf(String), "Detection sources to enable.")
			Attribute("presidio_entities", ArrayOf(String), "Presidio entity types to detect.")
			Attribute("prompt_injection_rules", ArrayOf(String), "Prompt-injection detection rule ids to enable in addition to the heuristic baseline (e.g. 'deberta-v3-classifier').")
			Attribute("enabled", Boolean, "Whether the policy is active.")
			Attribute("action", String, "Policy action: flag or block.", func() {
				shared.RiskPolicyActionEnum()
			})
			Attribute("auto_name", Boolean, "Whether the policy name should be auto-generated.")
			Attribute("user_message", String, "Optional message shown to end users when this policy blocks an action or surfaces a flagged finding. Send an empty string to clear.")
			Required("id", "name")
		})

		Result(shared.RiskPolicy)

		HTTP(func() {
			PUT("/rpc/risk.policies.update")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateRiskPolicy")
		Meta("openapi:extension:x-speakeasy-group", "risk.policies")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
	})

	Method("deleteRiskPolicy", func() {
		Description("Delete a risk analysis policy.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The policy ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			DELETE("/rpc/risk.policies.delete")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteRiskPolicy")
		Meta("openapi:extension:x-speakeasy-group", "risk.policies")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
	})

	Method("listRiskResults", func() {
		Description("List risk analysis results for the current project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("policy_id", String, "Optional policy ID to filter by.", func() {
				Format(FormatUUID)
			})
			Attribute("chat_id", String, "Optional chat ID to filter by.", func() {
				Format(FormatUUID)
			})
			Attribute("cursor", String, "Cursor to fetch the next page of results.")
			Attribute("limit", Int, "Maximum number of results to return per page.", func() {
				Minimum(1)
				Maximum(200)
			})
		})

		Result(ListRiskResultsResult)

		HTTP(func() {
			GET("/rpc/risk.results.list")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("policy_id")
			Param("chat_id")
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRiskResults")
		Meta("openapi:extension:x-speakeasy-group", "risk.results")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskListResults"}`)
	})

	Method("listRiskResultsByChat", func() {
		Description("List risk results grouped by chat session for the current project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("cursor", String, "Cursor to fetch the next page of results.")
			Attribute("limit", Int, "Maximum number of results to return per page.", func() {
				Minimum(1)
				Maximum(200)
			})
		})

		Result(ListRiskResultsByChatResult)

		HTTP(func() {
			GET("/rpc/risk.results.byChat")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRiskResultsByChat")
		Meta("openapi:extension:x-speakeasy-group", "risk.results")
		Meta("openapi:extension:x-speakeasy-name-override", "byChat")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskListResultsByChat"}`)
	})

	Method("getRiskPolicyStatus", func() {
		Description("Get the analysis status of a risk policy including progress and workflow state.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The policy ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(shared.RiskPolicyStatus)

		HTTP(func() {
			GET("/rpc/risk.policies.status")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRiskPolicyStatus")
		Meta("openapi:extension:x-speakeasy-group", "risk.policies")
		Meta("openapi:extension:x-speakeasy-name-override", "status")
	})

	Method("listShadowMCPApprovals", func() {
		Description("List shadow-MCP URL approvals for a policy. Temporary Redis-backed storage; will move to a dedicated table once the feature graduates.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("policy_id", String, "The risk policy ID.", func() {
				Format(FormatUUID)
			})
			Required("policy_id")
		})

		Result(ListShadowMCPApprovalsResult)

		HTTP(func() {
			GET("/rpc/risk.approvals.list")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("policy_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listShadowMCPApprovals")
		Meta("openapi:extension:x-speakeasy-group", "risk.approvals")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskListShadowMCPApprovals"}`)
	})

	Method("approveShadowMCP", func() {
		Description("Approve a shadow-MCP URL so the named policy stops blocking calls to it.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("policy_id", String, "The risk policy ID.", func() {
				Format(FormatUUID)
			})
			Attribute("url", String, "The MCP server URL to approve.")
			Attribute("server_name", String, "Display name of the MCP server (optional, for UI).")
			Required("policy_id", "url")
		})

		Result(shared.ShadowMCPApproval)

		HTTP(func() {
			POST("/rpc/risk.approvals.create")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "approveShadowMCP")
		Meta("openapi:extension:x-speakeasy-group", "risk.approvals")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskApproveShadowMCP", "type": "mutation"}`)
	})

	Method("revokeShadowMCPApproval", func() {
		Description("Remove a previously-approved shadow-MCP URL for a policy.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("policy_id", String, "The risk policy ID.", func() {
				Format(FormatUUID)
			})
			Attribute("url", String, "The MCP server URL to revoke.")
			Required("policy_id", "url")
		})

		HTTP(func() {
			DELETE("/rpc/risk.approvals.delete")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("policy_id")
			Param("url")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeShadowMCPApproval")
		Meta("openapi:extension:x-speakeasy-group", "risk.approvals")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
	})

	Method("triggerRiskAnalysis", func() {
		Description("Manually trigger risk analysis for a policy, starting or signaling the drain workflow.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The policy ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			POST("/rpc/risk.policies.trigger")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "triggerRiskAnalysis")
		Meta("openapi:extension:x-speakeasy-group", "risk.policies")
		Meta("openapi:extension:x-speakeasy-name-override", "trigger")
	})
})

var ListRiskPoliciesResult = Type("ListRiskPoliciesResult", func() {
	Attribute("policies", ArrayOf(shared.RiskPolicy), "The list of risk policies.")
	Required("policies")
})

var RiskCapabilitiesResult = Type("RiskCapabilitiesResult", func() {
	Attribute("pi_classifier_enabled", Boolean, "Whether the prompt-injection ML classifier is configured on this server.")
	Required("pi_classifier_enabled")
})

var ListRiskResultsResult = Type("ListRiskResultsResult", func() {
	Attribute("results", ArrayOf(shared.RiskResult), "The list of risk results.")
	Attribute("total_count", Int64, "Total number of findings across all enabled policies.")
	Attribute("next_cursor", String, "Cursor for the next page of results.")
	Required("results", "total_count")
})

var ListRiskResultsByChatResult = Type("ListRiskResultsByChatResult", func() {
	Attribute("chats", ArrayOf(shared.RiskChatSummary), "Risk results grouped by chat.")
	Attribute("next_cursor", String, "Cursor for the next page of results.")
	Required("chats")
})

var ListShadowMCPApprovalsResult = Type("ListShadowMCPApprovalsResult", func() {
	Attribute("approvals", ArrayOf(shared.ShadowMCPApproval), "The approved shadow-MCP URLs for the policy.")
	Required("approvals")
})
