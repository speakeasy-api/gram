package mcpapproval

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("mcpApproval", func() {
	Description("Dashboard API for reviewing and deciding MCP server approval requests.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listRequests", func() {
		Description("List MCP approval requests for a project.")
		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("status", String, "Only return requests in this status.")
			Attribute("limit", Int32, "The number of requests to return per page")
		})

		Result(ListApprovalRequestsResult)

		HTTP(func() {
			GET("/rpc/mcpApproval.listRequests")
			Param("status")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMcpApprovalRequests")
		Meta("openapi:extension:x-speakeasy-name-override", "listRequests")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListMcpApprovalRequests"}`)
	})

	Method("getRequest", func() {
		Description("Fetch one MCP approval request with its evidence and decision history.")
		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("id", String, "The approval request ID.")
			Required("id")
		})

		Result(ApprovalRequestDetail)

		HTTP(func() {
			GET("/rpc/mcpApproval.getRequest")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getMcpApprovalRequest")
		Meta("openapi:extension:x-speakeasy-name-override", "getRequest")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetMcpApprovalRequest"}`)
	})

	Method("ensureServerReview", func() {
		Description("Resolve the evidence dossier for a server URL, opening one when none exists. Gathers evidence without recording any ask or decision, so a server can be inspected before — or without — anyone requesting it.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("target", String, "The server URL the dossier describes.", func() {
				MaxLength(2048)
			})
			Required("target")
		})

		Result(ApprovalRequestSummary)

		HTTP(func() {
			POST("/rpc/mcpApproval.ensureServerReview")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "ensureMcpServerReview")
		Meta("openapi:extension:x-speakeasy-name-override", "ensureServerReview")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "EnsureMcpServerReview"}`)
	})

	Method("createRequest", func() {
		Description("Ask for an MCP server to be reviewed. Repeat asks for the same server attach to the existing review rather than opening a second one.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("target_kind", String, func() {
				Description("The namespace of the reference.")
				Enum("server_url", "stdio_command")
			})
			Attribute("target", String, "The server reference: a URL, or the stdio command that launches it.", func() {
				MaxLength(2048)
			})
			Attribute("note", String, "The requester's justification for wanting access to this server. Must not be blank.", func() {
				MaxLength(4000)
			})
			Required("target_kind", "target", "note")
		})

		Result(ApprovalRequestSummary)

		HTTP(func() {
			POST("/rpc/mcpApproval.createRequest")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createMcpApprovalRequest")
		Meta("openapi:extension:x-speakeasy-name-override", "createRequest")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateMcpApprovalRequest"}`)
	})

	Method("promote", func() {
		Description("Promote a risk-policy bypass request into an approval request, carrying its requester and justification into the review queue.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("risk_policy_bypass_request_id", String, "The bypass request to promote.")
			Required("risk_policy_bypass_request_id")
		})

		Result(ApprovalRequestSummary)

		HTTP(func() {
			POST("/rpc/mcpApproval.promote")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "promoteMcpApprovalRequest")
		Meta("openapi:extension:x-speakeasy-name-override", "promote")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PromoteMcpApprovalRequest"}`)
	})

	Method("refreshEvidence", func() {
		Description("Re-run every evidence source for a request and replace its current evidence with the fresh gather. Frozen decision snapshots are never touched.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("id", String, "The approval request ID.")
			Required("id")
		})

		Result(ApprovalRequestDetail)

		HTTP(func() {
			POST("/rpc/mcpApproval.refreshEvidence")
			// The id travels as a query parameter, leaving the POST bodyless:
			// an id-only JSON body is structurally identical to other one-field
			// forms and the OpenAPI generator dedupes it into whichever named
			// type hashed first, which mislabels the SDK surface.
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "refreshMcpApprovalEvidence")
		Meta("openapi:extension:x-speakeasy-name-override", "refreshEvidence")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RefreshMcpApprovalEvidence"}`)
	})

	Method("recordDecision", func() {
		Description("Approve or deny an MCP approval request, recording the rationale and who it applies to.")
		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("id", String, "The approval request ID.")
			Attribute("decision", String, "Either approved or denied.", func() {
				Enum("approved", "denied")
			})
			Attribute("rationale", String, "Why the decision was made. This is the artifact cited when explaining the decision to the requester, so it cannot be blank.")
			Attribute("granted_principal_urns", ArrayOf(String), "Principals the approval covers. Empty for a denial.")
			Attribute("research_report_id", String, "A research report this decision cites. Must belong to the request being decided.")
			Required("id", "decision", "rationale")
		})

		Result(ApprovalDecision)

		HTTP(func() {
			POST("/rpc/mcpApproval.recordDecision")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "recordMcpApprovalDecision")
		Meta("openapi:extension:x-speakeasy-name-override", "recordDecision")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RecordMcpApprovalDecision"}`)
	})
})

var ApprovalRequestSummary = Type("ApprovalRequestSummary", func() {
	Description("One MCP server awaiting a decision.")

	Attribute("id", String, "The approval request ID.")
	Attribute("target_kind", String, "The namespace of the requested reference, such as server_url or stdio_command.")
	Attribute("target_raw", String, "The stored display form of the requested reference, with credential-shaped material (URL query strings and userinfo, secret-named flag and environment values in commands) redacted at intake.")
	Attribute("server_slug", String, "The Shadow MCP inventory page slug for a server_url target — the same identifier the inventory derives from the canonical URL, so a request links to the server page it describes. Absent for stdio targets.")
	Attribute("artifact_ref", String, "The resolved artifact identity. Absent when the server could not be identified, which must surface as unknown rather than as an absence of findings.")
	Attribute("version_pinned", Boolean, "Whether the reference names an exact version.")
	Attribute("status", String, "The request's current status.")
	Attribute("requester_count", Int, "How many people have asked for this server.")
	Attribute("created_at", String, "When the request was first raised.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "When the request last changed.", func() { Format(FormatDateTime) })

	Required("id", "target_kind", "target_raw", "version_pinned", "status", "requester_count", "created_at", "updated_at")
})

var ApprovalRequester = Type("ApprovalRequester", func() {
	Description("Someone who asked for a server, and why.")

	Attribute("user_id", String, "The requester.")
	Attribute("user_email", String, "The requester's email, when known.")
	Attribute("note", String, "The requester's justification. The one input no automated evidence supplies.")
	Attribute("requested_at", String, "When they asked.", func() { Format(FormatDateTime) })

	Required("user_id", "requested_at")
})

var ApprovalDecision = Type("ApprovalDecision", func() {
	Description("A recorded approve or deny, with the evidence it rested on.")

	Attribute("id", String, "The decision ID.")
	Attribute("decision", String, "Either approved or denied.")
	Attribute("decided_by", String, "Who decided.")
	Attribute("rationale", String, "Why. Written to be cited when explaining the decision to the requester.")
	Attribute("granted_principal_urns", ArrayOf(String), "Principals the approval covers. Empty for a denial.")
	Attribute("research_report_id", String, "The research report this decision cited, when one informed it.")
	Attribute("evidence", Any, "The evidence as it stood when this decision was made. Frozen at decision time: a later re-gather updates the request's evidence but never this snapshot, so what the reviewer actually saw stays inspectable.")
	Attribute("evidence_version", Int, "Shape version of the frozen evidence payload, copied from the request at decision time.")
	Attribute("decided_at", String, "When the decision was made.", func() { Format(FormatDateTime) })

	Required("id", "decision", "decided_by", "decided_at")
})

var ResearchReport = Type("ResearchReport", func() {
	Description("One research-agent run over a request's server. Findings are gathered and cited, never adjudicated — and web-sourced claims may be inaccurate, incomplete, or deliberately seeded.")

	Attribute("id", String, "The report ID.")
	Attribute("status", String, "The run's lifecycle state, such as running, completed, or failed.")
	Attribute("report", Any, "The structured findings. Every claim carries a provenance tier and its citations.")
	Attribute("report_version", Int, "Shape version of the report payload.")
	Attribute("model", String, "The model that produced the report.")
	Attribute("prompt_version", String, "The prompt version the run used, so reports stay distinguishable across prompt changes.")
	Attribute("requested_by", String, "Who asked for the research run.")
	Attribute("started_at", String, "When the run started.", func() { Format(FormatDateTime) })
	Attribute("completed_at", String, "When the run finished.", func() { Format(FormatDateTime) })
	Attribute("error", String, "Why the run failed, when it did.")
	Attribute("created_at", String, "When the run was requested.", func() { Format(FormatDateTime) })

	Required("id", "status", "report_version", "created_at")
})

var ApprovalRequestDetail = Type("ApprovalRequestDetail", func() {
	Description("An approval request with everything needed to decide it.")

	Attribute("request", ApprovalRequestSummary, "The request itself.")
	Attribute("requesters", ArrayOf(ApprovalRequester), "Everyone who asked.")
	Attribute("evidence", Any, "The deterministic signals gathered for this server, as they stood when last collected. Every item is a declaration by the server or its registry, never an observation of behaviour.")
	Attribute("evidence_version", Int, "Shape version of the evidence payload, so an older snapshot stays interpretable.")
	Attribute("evidence_collected_at", String, "When the evidence was last gathered.", func() { Format(FormatDateTime) })
	Attribute("decisions", ArrayOf(ApprovalDecision), "Every decision made on this server, newest first. A repeat request starts from the last rationale rather than from zero.")
	Attribute("research_reports", ArrayOf(ResearchReport), "Every research-agent run for this request, newest first.")

	Required("request", "requesters", "decisions", "research_reports")
})

var ListApprovalRequestsResult = Type("ListApprovalRequestsResult", func() {
	Description("A page of the approval queue.")

	Attribute("requests", ArrayOf(ApprovalRequestSummary), "The list of approval requests")
	Required("requests")
})
