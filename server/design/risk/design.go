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
			Attribute("category", String, "Optional rule category key to filter by (e.g. secrets, pii, financial).")
			Attribute("rule_id", String, "Optional rule identifier substring to filter by (case-insensitive, e.g. 'secret' matches all 'secret.*' rules).")
			Attribute("unique_match", Boolean, "If true, collapse results to one row per (policy_id, rule_id, match), keeping the most recent occurrence. Useful when the same secret is detected many times within a single message body.")
			Attribute("from", String, "Filter results to messages created at or after this timestamp (ISO 8601).", func() {
				Format(FormatDateTime)
			})
			Attribute("to", String, "Filter results to messages created strictly before this timestamp (ISO 8601).", func() {
				Format(FormatDateTime)
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
			Param("category")
			Param("rule_id")
			Param("unique_match")
			Param("from")
			Param("to")
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRiskResults")
		Meta("openapi:extension:x-speakeasy-group", "risk.results")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskListResults"}`)
	})

	Method("listRiskResultsForAgent", func() {
		Description("List risk analysis results with the `match` field redacted to an opaque length+sha256-prefix fingerprint. Matches the payload and pagination semantics of listRiskResults. Designed for AI assistant / MCP consumption so secret content (gitleaks captures, presidio entities, prompt-injection payloads) never reaches the model context. For shadow_mcp findings the `match` value — a non-sensitive server URL or command identifier — is passed through verbatim.")

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
			Attribute("category", String, "Optional rule category key to filter by (e.g. secrets, pii, financial).")
			Attribute("rule_id", String, "Optional rule identifier substring to filter by (case-insensitive, e.g. 'secret' matches all 'secret.*' rules).")
			Attribute("unique_match", Boolean, "If true, collapse results to one row per (policy_id, rule_id, match), keeping the most recent occurrence. Useful when the same secret is detected many times within a single message body.")
			Attribute("from", String, "Filter results to messages created at or after this timestamp (ISO 8601).", func() {
				Format(FormatDateTime)
			})
			Attribute("to", String, "Filter results to messages created strictly before this timestamp (ISO 8601).", func() {
				Format(FormatDateTime)
			})
			Attribute("cursor", String, "Cursor to fetch the next page of results.")
			Attribute("limit", Int, "Maximum number of results to return per page.", func() {
				Minimum(1)
				Maximum(200)
			})
		})

		Result(ListRiskResultsForAgentResult)

		HTTP(func() {
			GET("/rpc/risk.results.listForAgent")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("policy_id")
			Param("chat_id")
			Param("category")
			Param("rule_id")
			Param("unique_match")
			Param("from")
			Param("to")
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRiskResultsForAgent")
		Meta("openapi:extension:x-speakeasy-group", "risk.results")
		Meta("openapi:extension:x-speakeasy-name-override", "listForAgent")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskListResultsForAgent"}`)
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

	Method("getRiskOverview", func() {
		Description("Get risk overview metrics and trend data for the current project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("from", String, "Inclusive start of the overview window. Defaults to the start of the 7-day calendar window ending at to.", func() {
				Format(FormatDateTime)
			})
			Attribute("to", String, "Exclusive end of the overview window. Defaults to now.", func() {
				Format(FormatDateTime)
			})
		})

		Result(RiskOverviewResult)

		HTTP(func() {
			GET("/rpc/risk.overview.get")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("from")
			Param("to")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRiskOverview")
		Meta("openapi:extension:x-speakeasy-group", "risk.overview")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskOverview"}`)
	})

	Method("listRiskCategories", func() {
		Description("Return the canonical risk category definitions: metadata (label/description/icon) plus the classification (source / rule_id list / rule_id prefix) used to bucket findings. Dashboards and CLIs should call this instead of maintaining their own copy of the mapping.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(RiskCategoriesResult)

		HTTP(func() {
			GET("/rpc/risk.categories")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRiskCategories")
		Meta("openapi:extension:x-speakeasy-group", "risk.categories")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskCategories"}`)
	})

	Method("getRiskUserBreakdown", func() {
		Description("Per-user breakdowns of findings by category and by rule_id within a time window. Powers the user drill-down on /risk-overview.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("external_user_id", String, "External user identifier to scope the breakdown to.")
			Attribute("from", String, "Inclusive start of the window. Defaults to the same 7-day window as the overview.", func() {
				Format(FormatDateTime)
			})
			Attribute("to", String, "Exclusive end of the window. Defaults to now.", func() {
				Format(FormatDateTime)
			})
			Required("external_user_id")
		})

		Result(RiskUserBreakdownResult)

		HTTP(func() {
			GET("/rpc/risk.overview.userBreakdown")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("external_user_id")
			Param("from")
			Param("to")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRiskUserBreakdown")
		Meta("openapi:extension:x-speakeasy-group", "risk.overview")
		Meta("openapi:extension:x-speakeasy-name-override", "userBreakdown")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskUserBreakdown"}`)
	})

	Method("getRiskRuleBreakdown", func() {
		Description("Get per-rule_id finding counts for a category within a time window. Powers the per-category drill-down chart on /risk-overview.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("category", String, "Required category key to break down by rule_id (e.g. secrets, pii).")
			Attribute("from", String, "Inclusive start of the window. Defaults to the same 7-day window as the overview.", func() {
				Format(FormatDateTime)
			})
			Attribute("to", String, "Exclusive end of the window. Defaults to now.", func() {
				Format(FormatDateTime)
			})
			Required("category")
		})

		Result(RiskRuleBreakdownResult)

		HTTP(func() {
			GET("/rpc/risk.overview.rules")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("category")
			Param("from")
			Param("to")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRiskRuleBreakdown")
		Meta("openapi:extension:x-speakeasy-group", "risk.overview")
		Meta("openapi:extension:x-speakeasy-name-override", "rules")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskRuleBreakdown"}`)
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
		Description("List shadow-MCP approvals (URL- or command-keyed) for a policy. Temporary Redis-backed storage; will move to a dedicated table once the feature graduates.")

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
		Description("Approve a shadow-MCP server so the named policy stops blocking calls to it. `match` is the same opaque server identifier surfaced in `RiskResult.match` — typically a server URL, stdio command, or `mcp__<server>__` prefix.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("policy_id", String, "The risk policy ID.", func() {
				Format(FormatUUID)
			})
			Attribute("match", String, "The MCP server identifier to approve.")
			Attribute("server_name", String, "Display name of the MCP server (optional, for UI).")
			Required("policy_id", "match")
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
		Description("Remove a previously-approved shadow-MCP server for a policy.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("policy_id", String, "The risk policy ID.", func() {
				Format(FormatUUID)
			})
			Attribute("match", String, "The MCP server identifier to revoke — exactly the value used to approve.")
			Required("policy_id", "match")
		})

		HTTP(func() {
			DELETE("/rpc/risk.approvals.delete")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("policy_id")
			Param("match")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeShadowMCPApproval")
		Meta("openapi:extension:x-speakeasy-group", "risk.approvals")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
	})

	Method("triggerRiskAnalysis", func() {
		Description("Manually trigger risk analysis for a policy, starting or signaling the drain workflow. Defaults to the most recent 100 unanalyzed messages; pass `limit=0` to backfill every unanalyzed message.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The policy ID.", func() {
				Format(FormatUUID)
			})
			Attribute("limit", Int32, "Cap the backfill at the most recent N unanalyzed messages. Defaults to 100 (the recent-N drain budget). Pass 0 to request a full backfill of every unanalyzed message.", func() {
				Minimum(0)
				Default(100)
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

var ListRiskResultsForAgentResult = Type("ListRiskResultsForAgentResult", func() {
	Attribute("results", ArrayOf(shared.RiskResultRedacted), "The list of risk results with match content redacted to opaque fingerprints.")
	Attribute("total_count", Int64, "Total number of findings across all enabled policies.")
	Attribute("next_cursor", String, "Cursor for the next page of results.")
	Required("results", "total_count")
})

var ListRiskResultsByChatResult = Type("ListRiskResultsByChatResult", func() {
	Attribute("chats", ArrayOf(shared.RiskChatSummary), "Risk results grouped by chat.")
	Attribute("next_cursor", String, "Cursor for the next page of results.")
	Required("chats")
})

var RiskOverviewResult = Type("RiskOverviewResult", func() {
	Attribute("from", String, "Inclusive start of the overview window.", func() {
		Format(FormatDateTime)
	})
	Attribute("to", String, "Exclusive end of the overview window.", func() {
		Format(FormatDateTime)
	})
	Attribute("messages_scanned", Int64, "Messages analyzed by risk policies in the window.")
	Attribute("findings", Int64, "Policy findings in the window.")
	Attribute("flagged_sessions", Int64, "Chat sessions with at least one finding in the window.")
	Attribute("active_policies", Int64, "Enabled risk policies for the current project.")
	Attribute("top_categories", ArrayOf(RiskOverviewCategory), "Top policy categories by finding count.")
	Attribute("top_users", ArrayOf(RiskOverviewUser), "Top users by finding count.")
	Attribute("top_rules", ArrayOf(RiskRuleBreakdownEntry), "Top rule_ids by finding count.")
	Attribute("time_series_findings", ArrayOf(RiskOverviewTimeSeriesFinding), "Time-series finding counts by category in the window.")

	Required("from", "to", "messages_scanned", "findings", "flagged_sessions", "active_policies", "top_categories", "top_users", "top_rules", "time_series_findings")
})

var RiskOverviewCategory = Type("RiskOverviewCategory", func() {
	Attribute("category", String, "Policy category key.")
	Attribute("findings", Int64, "Finding count for this category.")

	Required("category", "findings")
})

var RiskCategoryDefinition = Type("RiskCategoryDefinition", func() {
	Description("One canonical risk category and how findings are classified into it.")

	Attribute("key", String, "Canonical category key (e.g. 'secrets', 'pii', 'shadow_mcp').")
	Attribute("label", String, "Human-readable category label for UI rendering.")
	Attribute("description", String, "Plain-English description of what this category covers.")
	Attribute("icon", String, "Lucide icon name suggested for this category.")
	Attribute("source", String, "When non-empty, findings whose source equals this value belong to this category.")
	Attribute("rule_ids", ArrayOf(String), "When non-empty, findings whose rule_id is in this exact list belong to this category. Checked before rule_id_prefix.")
	Attribute("rule_id_prefix", String, "When non-empty, findings whose rule_id starts with this prefix belong to this category. The catch-all for a family (e.g. 'pii.').")

	Required("key", "label", "description", "icon", "source", "rule_ids", "rule_id_prefix")
})

var RiskCategoriesResult = Type("RiskCategoriesResult", func() {
	Description("Canonical risk category definitions used to classify findings, in classification-priority order. Consumers should iterate in order and pick the first match.")

	Attribute("categories", ArrayOf(RiskCategoryDefinition), "Categories in classification-priority order. The last entry is the 'custom' fallback for findings that match none of the others.")

	Required("categories")
})

var RiskRuleBreakdownEntry = Type("RiskRuleBreakdownEntry", func() {
	Attribute("rule_id", String, "Rule identifier (e.g. 'secret.aws-access-key'). Empty when the finding has no rule_id (treat as 'unspecified').")
	Attribute("source", String, "Source bucket the rule belongs to (gitleaks, presidio, etc.) for label/icon resolution on the dashboard.")
	Attribute("findings", Int64, "Finding count for this rule within the window.")

	Required("rule_id", "source", "findings")
})

var RiskUserBreakdownResult = Type("RiskUserBreakdownResult", func() {
	Attribute("from", String, "Inclusive start of the window used.", func() { Format(FormatDateTime) })
	Attribute("to", String, "Exclusive end of the window used.", func() { Format(FormatDateTime) })
	Attribute("external_user_id", String, "External user the breakdown is scoped to.")
	Attribute("findings", Int64, "Total findings for this user in the window.")
	Attribute("categories", ArrayOf(RiskOverviewCategory), "Category breakdown for this user, ordered by finding count descending.")
	Attribute("rules", ArrayOf(RiskRuleBreakdownEntry), "Rule_id breakdown for this user, ordered by finding count descending.")

	Required("from", "to", "external_user_id", "findings", "categories", "rules")
})

var RiskRuleBreakdownResult = Type("RiskRuleBreakdownResult", func() {
	Attribute("from", String, "Inclusive start of the window used.", func() { Format(FormatDateTime) })
	Attribute("to", String, "Exclusive end of the window used.", func() { Format(FormatDateTime) })
	Attribute("category", String, "Category the breakdown is scoped to.")
	Attribute("rules", ArrayOf(RiskRuleBreakdownEntry), "Rules in this category, ordered by finding count descending.")
	Attribute("total", Int64, "Total findings across all rules in this category and window.")

	Required("from", "to", "category", "rules", "total")
})

var RiskOverviewUser = Type("RiskOverviewUser", func() {
	Attribute("email", String, "User email, or Unknown user when unavailable.")
	Attribute("external_user_id", String, "External user identifier as recorded on chats, when known. Empty when the finding cannot be attributed to an external user.")
	Attribute("findings", Int64, "Finding count for this user.")

	Required("email", "external_user_id", "findings")
})

var RiskOverviewTimeSeriesFinding = Type("RiskOverviewTimeSeriesFinding", func() {
	Attribute("bucket_start", String, "Time bucket start.", func() {
		Format(FormatDateTime)
	})
	Attribute("category", String, "Policy category key.")
	Attribute("findings", Int64, "Finding count for this category and time bucket.")

	Required("bucket_start", "category", "findings")
})

var ListShadowMCPApprovalsResult = Type("ListShadowMCPApprovalsResult", func() {
	Attribute("approvals", ArrayOf(shared.ShadowMCPApproval), "The approved shadow-MCP servers for the policy (URL- or command-keyed).")
	Required("approvals")
})
