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
			Attribute("status", String, "Only return requests in this status: unreviewed, requested, approved, denied, or superseded.")
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

	Method("startResearch", func() {
		Description("Start a research-agent run for an approval request. The agent searches the web and reads pages about the server's vendor, then files a cited report; it never decides. Runs are additive — a re-run adds a report rather than replacing one — and at most one run per request is in flight at a time: starting while one runs returns the running report.")
		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("id", String, "The approval request ID.")
			Required("id")
		})

		Result(ResearchReport)

		HTTP(func() {
			POST("/rpc/mcpApproval.startResearch")
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

		Meta("openapi:operationId", "startMcpResearch")
		Meta("openapi:extension:x-speakeasy-name-override", "startResearch")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "StartMcpResearch"}`)
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
	Attribute("status", String, "The request's current status: unreviewed, requested, approved, denied, or superseded (the latest decision was explicitly displaced by a policy URL-list edit; history preserved, no enforcement derives from it until re-decided).")
	Attribute("requester_count", Int, "How many people have asked for this server.")
	Attribute("evidence_changed_at", String, "When the daily recheck first found the permission-relevant evidence differing from what the latest approval rested on. Absent when nothing has drifted. Cleared only by recording a new decision.", func() { Format(FormatDateTime) })
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

	// Rationale stays optional because its column is nullable, and evidence
	// stays optional because an undecodable snapshot must surface as absent
	// rather than as an empty document. The version and principal set are NOT
	// NULL at the source and always written, so consumers can rely on them.
	Required("id", "decision", "decided_by", "granted_principal_urns", "evidence_version", "decided_at")
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
	Attribute("tool_calls", ArrayOf(ResearchToolCall), "The run's per-action trace — every search and page fetch, in order. The report above is a synthesis that drops most of what was read; this is what the agent actually did.")

	Required("id", "status", "report_version", "created_at")
})

var ResearchToolCall = Type("ResearchToolCall", func() {
	Description("One action a research run took, named by tool and carrying the payload for that tool. The report is a synthesis that drops most of what was read; this is what the agent actually did. Observability, never a claim about the server.")

	Attribute("sequence", Int, "Position in the run, from zero.")
	Attribute("tool", String, "The tool that ran: web_search or fetch_page. The discriminator for which payload below is present.")
	Attribute("error", String, "The tool's failure text, when the call did not succeed.")
	Attribute("search", ResearchWebSearchCall, "The web-search payload, present when tool is web_search.")
	Attribute("fetch", ResearchPageFetchCall, "The page-fetch payload, present when tool is fetch_page.")

	Required("sequence", "tool")
})

var ResearchWebSearchCall = Type("ResearchWebSearchCall", func() {
	Description("A web search the agent ran. A search runs a billed completion, so its token spend lives here.")

	Attribute("query", String, "What the agent searched for.")
	Attribute("result_count", Int, "How many citations the search returned.")
	Attribute("prompt_tokens", Int, "Prompt tokens this search spent.")
	Attribute("completion_tokens", Int, "Completion tokens this search spent.")
})

var ResearchPageFetchCall = Type("ResearchPageFetchCall", func() {
	Description("A page the agent fetched, with what came back and the injection judge's verdict on it. A fetch spends nothing.")

	Attribute("url", String, "The page the agent fetched.")
	Attribute("final_url", String, "Where the fetch landed after redirects, when it differed.")
	Attribute("content_type", String, "The fetched page's content type.")
	Attribute("content_bytes", Int, "The fetched page's extracted-text size.")
	Attribute("truncated", Boolean, "Whether the fetch hit its caps and the preview is of a prefix.")
	Attribute("judged", Boolean, "Whether the injection judge reached a verdict on this page.")
	Attribute("injection_flagged", Boolean, "Whether the judge found the page tried to instruct its reader.")
	Attribute("judge_rationale", String, "The judge's reasoning, when it flagged the page.")
	Attribute("content_preview", String, "A bounded preview of the extracted page text. Untrusted web content.")
	Attribute("cited_by_claims", ArrayOf(Int), "Indices of the report claims that cited this page — the link from a fetch to the evidence it became. Empty when the page was read but nothing in the final report rests on it.")
})

var EvidenceFieldChange = Type("EvidenceFieldChange", func() {
	Description("One scalar drift between the decision's evidence snapshot and the current gather.")

	Attribute("field", String, "Which fact moved: authority_mode, dynamic_registration, or known_advisories.")
	Attribute("before", String, "The value the decision rested on.")
	Attribute("after", String, "The value the latest gather found.")

	Required("field", "before", "after")
})

var EvidenceAdvisoryChange = Type("EvidenceAdvisoryChange", func() {
	Description("A published advisory the decision's snapshot did not carry.")

	Attribute("id", String, "The advisory identifier.")
	Attribute("summary", String, "The advisory's summary, when the database published one.")
	Attribute("severity", String, "The advisory's severity, when the database published one.")

	Required("id")
})

var EvidenceDiff = Type("EvidenceDiff", func() {
	Description("What moved between the latest decision's evidence snapshot and the current gather, restricted to the permission-relevant slice: OAuth scopes, authority mode, demanded credentials, and published advisories. A change here is a re-review trigger, never a verdict — an unchanged published interface says nothing about unchanged behavior.")

	Attribute("changed", Boolean, "Whether anything below is non-empty.")
	Attribute("scopes_added", ArrayOf(String), "OAuth scopes the server's published authority metadata gained since the decision.")
	Attribute("scopes_removed", ArrayOf(String), "OAuth scopes the published authority metadata lost since the decision.")
	Attribute("secrets_added", ArrayOf(String), "Credentials the server now demands that it did not at decision time.")
	Attribute("secrets_removed", ArrayOf(String), "Credentials the server demanded at decision time and no longer does.")
	Attribute("fields", ArrayOf(EvidenceFieldChange), "Scalar drifts: authority mode, dynamic client registration, published-advisory count.")
	Attribute("advisories_added", ArrayOf(EvidenceAdvisoryChange), "Advisories in the current gather's most-recent sample that the snapshot's sample did not carry.")

	Required("changed")
})

var ApprovalRequestDetail = Type("ApprovalRequestDetail", func() {
	Description("An approval request with everything needed to decide it.")

	Attribute("request", ApprovalRequestSummary, "The request itself.")
	Attribute("requesters", ArrayOf(ApprovalRequester), "Everyone who asked.")
	Attribute("evidence", Any, "The deterministic signals gathered for this server, as they stood when last collected. Every item is a declaration by the server or its registry, never an observation of behaviour.")
	Attribute("evidence_version", Int, "Shape version of the evidence payload, so an older snapshot stays interpretable.")
	Attribute("evidence_collected_at", String, "When the evidence was last gathered.", func() { Format(FormatDateTime) })
	Attribute("decisions", ArrayOf(ApprovalDecision), "Every decision made on this server, newest first. A repeat request starts from the last rationale rather than from zero.")
	Attribute("evidence_diff", EvidenceDiff, "What moved since the latest decision, compared on read between that decision's frozen snapshot and the current evidence. Absent when the request has no decisions or either side cannot be decoded.")
	Attribute("research_reports", ArrayOf(ResearchReport), "Every research-agent run for this request, newest first.")

	Required("request", "requesters", "decisions", "research_reports")
})

var ListApprovalRequestsResult = Type("ListApprovalRequestsResult", func() {
	Description("A page of the approval queue.")

	Attribute("requests", ArrayOf(ApprovalRequestSummary), "The list of approval requests")
	Required("requests")
})
