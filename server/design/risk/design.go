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
			Attribute("policy_type", String, "Policy type: standard (regex/presidio/custom detection) or prompt_based (LLM-judge). Defaults to standard.", func() {
				shared.RiskPolicyTypeEnum()
				Default("standard")
			})
			Attribute("sources", ArrayOf(String), "Detection sources to enable.")
			Attribute("presidio_entities", ArrayOf(String), "Presidio entity types to detect.")
			Attribute("prompt_injection_rules", ArrayOf(String), "Prompt-injection detection rule ids to enable in addition to the heuristic baseline (e.g. 'deberta-v3-classifier').")
			Attribute("disabled_rules", ArrayOf(String), "Canonical rule_ids the user has unchecked within otherwise-enabled categories. Matching findings are dropped at scan time.")
			Attribute("custom_rule_ids", ArrayOf(String), "Custom detection rule ids to enable for this policy.")
			Attribute("message_types", ArrayOf(String), "Message types this policy applies to. When empty or omitted, the policy scans all supported types.")
			Attribute("enabled", Boolean, "Whether the policy is active.")
			Attribute("action", String, "Policy action: flag or block.", func() {
				shared.RiskPolicyActionEnum()
				Default("flag")
			})
			Attribute("audience_type", String, "Policy audience type: everyone or targeted.", func() {
				shared.RiskPolicyAudienceTypeEnum()
				Default("everyone")
			})
			Attribute("audience_principal_urns", ArrayOf(String), "Principal URNs this policy applies to. For audience_type=everyone, the server stores user:all.")
			Attribute("auto_name", Boolean, "Whether the policy name should be auto-generated.")
			Attribute("user_message", String, "Optional message shown to end users when this policy blocks an action or surfaces a flagged finding.")
			Attribute("prompt", String, "For prompt_based policies: the guardrail prompt the LLM judge evaluates each in-scope message against. Required when policy_type is prompt_based.")
			Attribute("model_config", shared.RiskPolicyModelConfig, "For prompt_based policies: per-policy LLM-judge model configuration.")
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
			Attribute("disabled_rules", ArrayOf(String), "Canonical rule_ids the user has unchecked within otherwise-enabled categories. Matching findings are dropped at scan time.")
			Attribute("custom_rule_ids", ArrayOf(String), "Custom detection rule ids to enable for this policy. Omit to preserve the current selection.")
			Attribute("message_types", ArrayOf(String), "Message types this policy applies to. Omit to preserve the current selection; send an empty array to apply to all types.")
			Attribute("enabled", Boolean, "Whether the policy is active.")
			Attribute("action", String, "Policy action: flag or block.", func() {
				shared.RiskPolicyActionEnum()
			})
			Attribute("audience_type", String, "Policy audience type: everyone or targeted. Omit to preserve the current audience type.", func() {
				shared.RiskPolicyAudienceTypeEnum()
			})
			Attribute("audience_principal_urns", ArrayOf(String), "Principal URNs this policy applies to. Omit to preserve the current target principals.")
			Attribute("auto_name", Boolean, "Whether the policy name should be auto-generated.")
			Attribute("user_message", String, "Optional message shown to end users when this policy blocks an action or surfaces a flagged finding. Send an empty string to clear.")
			Attribute("prompt", String, "For prompt_based policies: the guardrail prompt the LLM judge evaluates each in-scope message against. Omit to preserve the current value.")
			Attribute("model_config", shared.RiskPolicyModelConfig, "For prompt_based policies: per-policy LLM-judge model configuration. Omit to preserve the current value.")
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
			Attribute("user_id", String, "Optional user identifier substring to filter by (case-insensitive, matched against the chat's external user id).")
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
			Param("user_id")
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
			Attribute("user_id", String, "Optional user identifier substring to filter by (case-insensitive, matched against the chat's external user id).")
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
			Param("user_id")
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

	Method("createRiskPolicyBypassRequest", func() {
		Description("Create or refresh a risk policy bypass request from a signed request URL token.")
		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Attribute("request_token", String, "Signed request token generated when a risk policy blocks an action.")
			Required("request_token")
		})

		Result(RiskPolicyBypassRequest)

		HTTP(func() {
			POST("/rpc/risk.createPolicyBypassRequest")
			security.SessionHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "createRiskPolicyBypassRequest")
		Meta("openapi:extension:x-speakeasy-group", "risk.policyBypassRequests")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskCreatePolicyBypassRequest", "type": "mutation"}`)
	})

	Method("listRiskPolicyBypassRequests", func() {
		Description("List current risk policy bypass request workflow records.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("policy_id", String, "Optional risk policy ID filter.", func() {
				Format(FormatUUID)
			})
			Attribute("status", String, "Optional request status filter.", func() {
				Enum("requested", "approved", "denied", "revoked")
			})
		})

		Result(ListRiskPolicyBypassRequestsResult)

		HTTP(func() {
			GET("/rpc/risk.listPolicyBypassRequests")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("policy_id")
			Param("status")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRiskPolicyBypassRequests")
		Meta("openapi:extension:x-speakeasy-group", "risk.policyBypassRequests")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskListPolicyBypassRequests"}`)
	})

	Method("approveRiskPolicyBypassRequest", func() {
		Description("Approve a risk policy bypass request for the requested policy target.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The bypass request ID.", func() {
				Format(FormatUUID)
			})
			Attribute("granted_principal_urns", ArrayOf(String), "Principal URNs to grant bypass access to. Defaults to the requester when omitted.")
			Required("id")
		})

		Result(RiskPolicyBypassRequest)

		HTTP(func() {
			POST("/rpc/risk.approvePolicyBypassRequest")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Body(RiskPolicyBypassApprovalRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "approveRiskPolicyBypassRequest")
		Meta("openapi:extension:x-speakeasy-group", "risk.policyBypassRequests")
		Meta("openapi:extension:x-speakeasy-name-override", "approve")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskApprovePolicyBypassRequest", "type": "mutation"}`)
	})

	Method("denyRiskPolicyBypassRequest", func() {
		Description("Deny a risk policy bypass request, updating workflow state.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The bypass request ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(RiskPolicyBypassRequest)

		HTTP(func() {
			POST("/rpc/risk.denyPolicyBypassRequest")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Body(RiskIDRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "denyRiskPolicyBypassRequest")
		Meta("openapi:extension:x-speakeasy-group", "risk.policyBypassRequests")
		Meta("openapi:extension:x-speakeasy-name-override", "deny")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskDenyPolicyBypassRequest", "type": "mutation"}`)
	})

	Method("revokeRiskPolicyBypassRequest", func() {
		Description("Revoke a previously approved risk policy bypass request.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The bypass request ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(RiskPolicyBypassRequest)

		HTTP(func() {
			POST("/rpc/risk.revokePolicyBypassRequest")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Body(RiskIDRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeRiskPolicyBypassRequest")
		Meta("openapi:extension:x-speakeasy-group", "risk.policyBypassRequests")
		Meta("openapi:extension:x-speakeasy-name-override", "revoke")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskRevokePolicyBypassRequest", "type": "mutation"}`)
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

	Method("createCustomDetectionRule", func() {
		Description("Create a custom regex-backed detection rule for the current project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("rule_id", String, "Stable rule identifier, prefixed with `custom.`.")
			Attribute("title", String, "Human-readable title for the rule.")
			Attribute("description", String, "Description of what the rule detects.")
			Attribute("regex", String, "RE2-compatible regex pattern.")
			Attribute("severity", String, "Severity level for findings produced by this rule.", func() {
				Enum("info", "low", "medium", "high", "critical")
				Default("medium")
			})
			Required("rule_id", "title", "regex")
		})

		Result(shared.RiskCustomDetectionRule)

		HTTP(func() {
			POST("/rpc/risk.customRules.create")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createCustomDetectionRule")
		Meta("openapi:extension:x-speakeasy-group", "risk.customRules")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskCreateCustomDetectionRule", "type": "mutation"}`)
	})

	Method("listCustomDetectionRules", func() {
		Description("List custom detection rules for the current project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListCustomDetectionRulesResult)

		HTTP(func() {
			GET("/rpc/risk.customRules.list")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listCustomDetectionRules")
		Meta("openapi:extension:x-speakeasy-group", "risk.customRules")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskListCustomDetectionRules", "type": "query"}`)
	})

	Method("getCustomDetectionRule", func() {
		Description("Get a custom detection rule by ID.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The custom detection rule ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(shared.RiskCustomDetectionRule)

		HTTP(func() {
			GET("/rpc/risk.customRules.get")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getCustomDetectionRule")
		Meta("openapi:extension:x-speakeasy-group", "risk.customRules")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskGetCustomDetectionRule", "type": "query"}`)
	})

	Method("updateCustomDetectionRule", func() {
		Description("Update a custom detection rule.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The custom detection rule ID.", func() {
				Format(FormatUUID)
			})
			Attribute("title", String, "Human-readable title for the rule.")
			Attribute("description", String, "Description of what the rule detects.")
			Attribute("regex", String, "RE2-compatible regex pattern.")
			Attribute("severity", String, "Severity level for findings produced by this rule.", func() {
				Enum("info", "low", "medium", "high", "critical")
			})
			Required("id", "title", "regex", "severity")
		})

		Result(shared.RiskCustomDetectionRule)

		HTTP(func() {
			POST("/rpc/risk.customRules.update")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateCustomDetectionRule")
		Meta("openapi:extension:x-speakeasy-group", "risk.customRules")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskUpdateCustomDetectionRule", "type": "mutation"}`)
	})

	Method("deleteCustomDetectionRule", func() {
		Description("Delete a custom detection rule.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The custom detection rule ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			POST("/rpc/risk.customRules.delete")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Body(RiskIDRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteCustomDetectionRule")
		Meta("openapi:extension:x-speakeasy-group", "risk.customRules")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskDeleteCustomDetectionRule", "type": "mutation"}`)
	})

	Method("listRiskExclusions", func() {
		Description("List risk exclusions for the current project. Optionally filter to a single policy.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("risk_policy_id", String, "Filter to exclusions bound to this policy. Omit to return all exclusions (global plus every policy).", func() {
				Format(FormatUUID)
			})
		})

		Result(ListRiskExclusionsResult)

		HTTP(func() {
			GET("/rpc/risk.listExclusions")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("risk_policy_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRiskExclusions")
		Meta("openapi:extension:x-speakeasy-group", "risk.exclusions")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskListExclusions", "type": "query"}`)
	})

	Method("createRiskExclusion", func() {
		Description("Create a risk exclusion. Omit risk_policy_id to create a global exclusion that applies to every policy in the project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("risk_policy_id", String, "Bind the exclusion to a single policy. Omit for a global (project-wide) exclusion.", func() {
				Format(FormatUUID)
			})
			Attribute("match_type", String, "How match_value is interpreted.", func() {
				shared.RiskExclusionMatchTypeEnum()
			})
			Attribute("match_value", String, "The value matched against findings, interpreted per match_type.")
			Attribute("rule_id_filter", String, "Optional: only apply within this rule_id. Empty means any.", func() {
				Default("")
			})
			Attribute("source_filter", String, "Optional: only apply within this source. Empty means any.", func() {
				Default("")
			})
			Attribute("enabled", Boolean, "Whether the exclusion is active.", func() {
				Default(true)
			})
			Required("match_type", "match_value")
		})

		Result(shared.RiskExclusion)

		HTTP(func() {
			POST("/rpc/risk.createExclusions")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createRiskExclusion")
		Meta("openapi:extension:x-speakeasy-group", "risk.exclusions")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskCreateExclusion", "type": "mutation"}`)
	})

	Method("updateRiskExclusion", func() {
		Description("Update a risk exclusion.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The exclusion ID.", func() {
				Format(FormatUUID)
			})
			Attribute("risk_policy_id", String, "Bind the exclusion to a single policy. Omit for a global (project-wide) exclusion.", func() {
				Format(FormatUUID)
			})
			Attribute("match_type", String, "How match_value is interpreted.", func() {
				shared.RiskExclusionMatchTypeEnum()
			})
			Attribute("match_value", String, "The value matched against findings, interpreted per match_type.")
			Attribute("rule_id_filter", String, "Optional: only apply within this rule_id. Empty means any.", func() {
				Default("")
			})
			Attribute("source_filter", String, "Optional: only apply within this source. Empty means any.", func() {
				Default("")
			})
			// No default: an omitted `enabled` must leave the exclusion's
			// current state untouched rather than silently re-enabling it.
			Attribute("enabled", Boolean, "Whether the exclusion is active. Omit to leave unchanged.")
			Required("id", "match_type", "match_value")
		})

		Result(shared.RiskExclusion)

		HTTP(func() {
			PUT("/rpc/risk.updateExclusions")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateRiskExclusion")
		Meta("openapi:extension:x-speakeasy-group", "risk.exclusions")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskUpdateExclusion", "type": "mutation"}`)
	})

	Method("deleteRiskExclusion", func() {
		Description("Delete a risk exclusion. Previously suppressed findings are restored.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The exclusion ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			DELETE("/rpc/risk.deleteExclusions")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteRiskExclusion")
		Meta("openapi:extension:x-speakeasy-group", "risk.exclusions")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskDeleteExclusion", "type": "mutation"}`)
	})

	Method("suggestCustomDetectionRule", func() {
		Description("Suggest a custom detection rule (rule_id, title, description, regex, severity) from a natural-language prompt. Calls the configured LLM with a JSON-schema constrained response so the dashboard can prefill the create form.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("prompt", String, "Natural-language description of what the rule should detect.", func() {
				MinLength(3)
				MaxLength(500)
			})
			Attribute("existing_rule_ids", ArrayOf(String), "Existing built-in and custom rule ids the suggested id must avoid colliding with.")
			Required("prompt")
		})

		Result(SuggestCustomDetectionRuleResult)

		HTTP(func() {
			POST("/rpc/risk.customRules.suggest")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "suggestCustomDetectionRule")
		Meta("openapi:extension:x-speakeasy-group", "risk.customRules")
		Meta("openapi:extension:x-speakeasy-name-override", "suggest")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskSuggestCustomRule", "type": "mutation"}`)
	})

	Method("testDetectionRule", func() {
		Description("Run a single detection rule against pasted sample text and return any matches. Reuses the same scanner code (gitleaks, Presidio, prompt-injection, custom regex) that the analyzer runs in production so the playground match shape mirrors the chat-message path.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("rule_id", String, "Rule identifier to evaluate (e.g. `secret.aws_access_token`, `pii.email_address`, `custom.acme_token`).", func() {
				MinLength(1)
				MaxLength(200)
			})
			Attribute("text", String, "Sample text to scan.", func() {
				MinLength(1)
				MaxLength(50000)
			})
			Attribute("regex", String, "Regex pattern. Required for `custom.*` rule ids since the server doesn't persist custom rules yet; ignored for built-in rules.")
			Required("rule_id", "text")
		})

		Result(TestDetectionRuleResult)

		HTTP(func() {
			POST("/rpc/risk.rules.test")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "testDetectionRule")
		Meta("openapi:extension:x-speakeasy-group", "risk.rules")
		Meta("openapi:extension:x-speakeasy-name-override", "test")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RiskTestDetectionRule", "type": "mutation"}`)
	})
})

var SuggestCustomDetectionRuleResult = Type("SuggestCustomDetectionRuleResult", func() {
	Attribute("rule_id", String, "Suggested stable identifier, prefixed with `custom.`.")
	Attribute("title", String, "Short, human-friendly title for the rule.")
	Attribute("description", String, "Description of what the rule detects and why it matters.")
	Attribute("regex", String, "RE2-compatible regex pattern the rule should match against.")
	Attribute("severity", String, "Suggested severity level.", func() {
		Enum("info", "low", "medium", "high", "critical")
	})
	Required("rule_id", "title", "description", "regex", "severity")
})

var TestDetectionRuleMatch = Type("TestDetectionRuleMatch", func() {
	Attribute("rule_id", String, "Canonical rule id of the match (may differ from the requested rule id when one input matches multiple rules).")
	Attribute("description", String, "Human-readable description of why this match was flagged.")
	Attribute("match", String, "Matched substring of the sample.")
	Attribute("start_pos", Int, "Inclusive start byte offset of the match in the sample.")
	Attribute("end_pos", Int, "Exclusive end byte offset of the match in the sample.")
	Attribute("source", String, "Detection source (e.g. `gitleaks`, `presidio`, `prompt_injection`, `custom`).")
	Attribute("confidence", Float64, "Confidence score in the range 0.0 to 1.0.")
	Attribute("tags", ArrayOf(String), "Tags from the underlying rule.")
	Required("rule_id", "match", "start_pos", "end_pos", "source", "confidence")
})

var TestDetectionRuleResult = Type("TestDetectionRuleResult", func() {
	Attribute("matches", ArrayOf(TestDetectionRuleMatch), "Matches the rule found in the sample.")
	Attribute("supported", Boolean, "False when the rule has no text-only detector (e.g. `shadow_mcp`, `destructive_tool`).")
	Attribute("reason", String, "Why the rule isn't supported when `supported` is false.")
	Required("matches", "supported")
})

var ListRiskPoliciesResult = Type("ListRiskPoliciesResult", func() {
	Attribute("policies", ArrayOf(shared.RiskPolicy), "The list of risk policies.")
	Required("policies")
})

var ListRiskExclusionsResult = Type("ListRiskExclusionsResult", func() {
	Attribute("exclusions", ArrayOf(shared.RiskExclusion), "The list of risk exclusions.")
	Required("exclusions")
})

var ListCustomDetectionRulesResult = Type("ListCustomDetectionRulesResult", func() {
	Attribute("rules", ArrayOf(shared.RiskCustomDetectionRule), "The list of custom detection rules.")
	Required("rules")
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

var RiskPolicyBypassRequest = Type("RiskPolicyBypassRequest", func() {
	Attribute("id", String, "The bypass request ID.", func() {
		Format(FormatUUID)
	})
	Attribute("policy_id", String, "The risk policy ID.", func() {
		Format(FormatUUID)
	})
	Attribute("target_kind", String, "Optional target namespace for the request, such as server_url.")
	Attribute("target_label", String, "Optional display label for the target.")
	Attribute("target_key", String, "Canonical key for the target.")
	Attribute("target_dimensions", MapOf(String, String), "Selector dimensions for the request target.")
	Attribute("requester_user_id", String, "Requester user ID.")
	Attribute("requester_email", String, "Requester email when known.")
	Attribute("note", String, "Requester note.")
	Attribute("status", String, "Current request status.", func() {
		Enum("requested", "approved", "denied", "revoked")
	})
	Attribute("decided_by", String, "User ID that approved, denied, or revoked the request.")
	Attribute("granted_principal_urns", ArrayOf(String), "Principal URNs granted when approved.")
	Attribute("decided_at", String, "Decision timestamp.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, "Creation timestamp.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "Last update timestamp.", func() {
		Format(FormatDateTime)
	})
	Required("id", "policy_id", "target_dimensions", "requester_user_id", "status", "granted_principal_urns", "created_at", "updated_at")
})

var RiskIDRequestBody = Type("RiskIDRequestBody", func() {
	Meta("openapi:typename", "RiskIDRequestBody")

	Attribute("id", String, "The resource ID.", func() {
		Format(FormatUUID)
	})
	Required("id")
})

var RiskPolicyBypassApprovalRequestBody = Type("RiskPolicyBypassApprovalRequestBody", func() {
	Meta("openapi:typename", "RiskPolicyBypassApprovalRequestBody")

	Attribute("id", String, "The bypass request ID.", func() {
		Format(FormatUUID)
	})
	Attribute("granted_principal_urns", ArrayOf(String), "Principal URNs to grant bypass access to. Use user:all for every user in the organization.")
	Required("id")
})

var ListRiskPolicyBypassRequestsResult = Type("ListRiskPolicyBypassRequestsResult", func() {
	Attribute("requests", ArrayOf(RiskPolicyBypassRequest), "Current risk policy bypass request records.")
	Required("requests")
})
