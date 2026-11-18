package mcpconnectionhealth

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var Outcome = Type("ConnectionOutcome", String, func() {
	Description("How an attempt ended. didnt_finish has no final step past the live window; in_progress is still inside it.")
	Enum("connected", "failed", "blocked", "declined", "didnt_finish", "in_progress")
	Meta("openapi:extension:x-speakeasy-name-override", "ConnectionOutcome")
})

var StepGroup = Type("ConnectionStepGroup", String, func() {
	Description("The plain step an attempt ended or stopped at.")
	Enum("connect", "sign_in", "service_sign_in", "sign_in_renewal", "service_renewal", "listing_tools", "request", "other")
	Meta("openapi:extension:x-speakeasy-name-override", "ConnectionStepGroup")
})

var FailureSource = Type("ConnectionFailureSource", String, func() {
	Description("The party that returned a failure.")
	Enum("service", "server", "app", "identity_provider", "speakeasy_setting", "speakeasy")
	Meta("openapi:extension:x-speakeasy-name-override", "ConnectionFailureSource")
})

var ViewerScope = Type("ConnectionViewerScope", String, func() {
	Description("all when the caller reads the whole project, own when only their own attempts.")
	Enum("all", "own")
	Meta("openapi:extension:x-speakeasy-name-override", "ConnectionViewerScope")
})

var _ = Service("mcpConnectionHealth", func() {
	Description("Read MCP connection attempts, sign-in activity and sessions for one MCP authentication provider in a project.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("listAttempts", func() {
		Description("List connection attempts for a provider, newest first. Repeats of the same failure fold into one row.")
		Payload(func() {
			Extend(AttemptQuery)
			Attribute("cursor", String, "Opaque cursor from next_cursor.")
			Attribute("limit", Int, "Page size, default 50, at most 100.", func() {
				Minimum(1)
				Maximum(100)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(ListAttemptsResult)
		HTTP(func() {
			POST("/rpc/mcpConnectionHealth.listAttempts")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listMcpConnectionAttempts")
		Meta("openapi:extension:x-speakeasy-name-override", "listAttempts")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpConnectionAttempts"}`)
	})

	Method("getAttemptCounts", func() {
		Description("Count connection attempts for a provider: the total and the count for each filter value.")
		Payload(func() {
			Extend(AttemptQuery)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(AttemptCountsResult)
		HTTP(func() {
			POST("/rpc/mcpConnectionHealth.getAttemptCounts")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getMcpConnectionAttemptCounts")
		Meta("openapi:extension:x-speakeasy-name-override", "getAttemptCounts")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpConnectionAttemptCounts"}`)
	})

	Method("getAttempt", func() {
		Description("Get one connection attempt by its reference.")
		Payload(func() {
			Attribute("reference", String, "The attempt reference a person was shown, or a row id from listAttempts.", func() {
				MaxLength(200)
			})
			Required("reference")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(GetAttemptResult)
		HTTP(func() {
			POST("/rpc/mcpConnectionHealth.getAttempt")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getMcpConnectionAttempt")
		Meta("openapi:extension:x-speakeasy-name-override", "getAttempt")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpConnectionAttempt"}`)
	})

	Method("getSignInSummary", func() {
		Description("Count sign-ins and renewals through a provider per time bucket, split by whether they succeeded.")
		Payload(func() {
			Extend(RangeQuery)
			Attribute("bucket", String, "Bucket size. Defaults to hour for ranges up to 48 hours, otherwise day.", func() {
				Enum("hour", "six_hours", "day")
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(SignInSummaryResult)
		HTTP(func() {
			POST("/rpc/mcpConnectionHealth.getSignInSummary")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getMcpSignInSummary")
		Meta("openapi:extension:x-speakeasy-name-override", "getSignInSummary")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpSignInSummary"}`)
	})

	Method("getSessionActivity", func() {
		Description("Count a provider's active sessions per day and report the average session length.")
		Payload(func() {
			Attribute("user_session_issuer_id", String, "The MCP authentication provider.", func() {
				Format(FormatUUID)
			})
			Attribute("from", String, "Inclusive start.", func() { Format(FormatDateTime) })
			Attribute("to", String, "Exclusive end.", func() { Format(FormatDateTime) })
			Required("user_session_issuer_id", "from", "to")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(SessionActivityResult)
		HTTP(func() {
			POST("/rpc/mcpConnectionHealth.getSessionActivity")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getMcpSessionActivity")
		Meta("openapi:extension:x-speakeasy-name-override", "getSessionActivity")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpSessionActivity"}`)
	})

	Method("listDeniedClients", func() {
		Description("List apps refused at sign-in through a provider, with the reason and the last time.")
		Payload(func() {
			Extend(RangeQuery)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(DeniedClientsResult)
		HTTP(func() {
			POST("/rpc/mcpConnectionHealth.listDeniedClients")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listMcpDeniedClients")
		Meta("openapi:extension:x-speakeasy-name-override", "listDeniedClients")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpDeniedClients"}`)
	})
})

var RangeQuery = Type("ConnectionHealthRangeQuery", func() {
	Attribute("user_session_issuer_id", String, "The MCP authentication provider.", func() {
		Format(FormatUUID)
	})
	Attribute("server_id", String, "Limit to one server, gateway or toolset.", func() {
		Format(FormatUUID)
	})
	Attribute("from", String, "Inclusive start of attempt start times, at most 30 days ago.", func() { Format(FormatDateTime) })
	Attribute("to", String, "Exclusive end of attempt start times.", func() { Format(FormatDateTime) })
	Required("user_session_issuer_id", "from", "to")
})

var AttemptQuery = Type("ConnectionAttemptQuery", func() {
	Extend(RangeQuery)
	Attribute("filters", AttemptFilters)
})

var AttemptFilters = Type("ConnectionAttemptFilters", func() {
	Description("Values within one filter are alternatives. Filters combine.")
	Attribute("outcomes", ArrayOf(Outcome), func() { MaxLength(50) })
	Attribute("steps", ArrayOf(StepGroup), func() { MaxLength(50) })
	Attribute("server_ids", ArrayOf(String), "Server, gateway or toolset ids.", func() { MaxLength(50) })
	Attribute("member_ids", ArrayOf(String), "Gateway member ids on the step that ended the attempt.", func() { MaxLength(50) })
	Attribute("people", ArrayOf(String), "Subject URNs.", func() { MaxLength(50) })
	Attribute("networks", ArrayOf(String), "Network keys.", func() { MaxLength(50) })
	Attribute("apps", ArrayOf(String), "App families. An empty string selects apps that are not on the list.", func() { MaxLength(50) })
	Attribute("app_versions", ArrayOf(String), "App family and version as family@version.", func() { MaxLength(50) })
	Attribute("reasons", ArrayOf(String), "Reason codes.", func() { MaxLength(50) })
	Attribute("services", ArrayOf(String), "Service hosts.", func() { MaxLength(50) })
})

var Person = Type("ConnectionPerson", func() {
	Attribute("subject_urn", String)
	Attribute("kind", String, func() { Enum("user", "agent", "apikey", "workload", "anonymous", "other") })
	Attribute("user_id", String)
	Attribute("name", String)
	Attribute("email", String, "Only on getAttempt.")
	Required("subject_urn", "kind")
})

var App = Type("ConnectionApp", func() {
	Attribute("family", String, "Client family, empty when the app is not on the list.")
	Attribute("name", String, "Display name, Unlisted app when the family is unknown.")
	Attribute("version", String)
	Attribute("app_key", String, "Catalog host or kind of app.")
	Required("family", "name")
})

var ServerRef = Type("ConnectionServer", func() {
	Attribute("id", String)
	Attribute("kind", String, func() { Enum("mcp", "gateway", "toolset", "authorization_server", "none") })
	Attribute("name", String)
	Required("id", "kind")
})

var Network = Type("ConnectionNetwork", func() {
	Attribute("key", String, "Opaque per-organization network key, the filter value.")
	Attribute("label", String, "Short label for display.")
	Required("key", "label")
})

var AttemptRow = Type("ConnectionAttemptRow", func() {
	Attribute("id", String, "Reference of the newest attempt in the row.")
	Attribute("time", String, "When the newest attempt ended or last moved.", func() { Format(FormatDateTime) })
	Attribute("first_started_at", String, "Earliest attempt start in the row.", func() { Format(FormatDateTime) })
	Attribute("count", Int64, "Attempts this row stands for.")
	Attribute("outcome", Outcome)
	Attribute("step", StepGroup)
	Attribute("reason", String)
	Attribute("message", String)
	Attribute("technical_message", String, "Reserved: a sentence for administrators about the reason. Not returned yet.")
	Attribute("source", FailureSource)
	Attribute("person", Person)
	Attribute("network", Network)
	Attribute("app", App)
	Attribute("server", ServerRef)
	Attribute("member", ServerRef)
	Attribute("service", String)
	Required("id", "time", "first_started_at", "count", "outcome", "step", "app")
})

var AttemptStep = Type("ConnectionAttemptStep", func() {
	Attribute("at", String, func() { Format(FormatDateTime) })
	Attribute("step", StepGroup)
	Attribute("stored_step", String)
	Attribute("result", String)
	Attribute("final", Boolean)
	Attribute("reason", String)
	Attribute("message", String)
	Attribute("technical_message", String, "Reserved: a sentence for administrators about the reason. Not returned yet.")
	Attribute("member", ServerRef)
	Attribute("service", String)
	Required("at", "step", "stored_step", "result", "final")
})

var AttemptContext = Type("ConnectionAttemptContext", func() {
	Attribute("grant_type", String)
	Attribute("sub_reason", String)
	Attribute("retries", Int64)
	Required("retries")
})

var AttemptDetail = Type("ConnectionAttemptDetail", func() {
	Extend(AttemptRow)
	Attribute("started_at", String, func() { Format(FormatDateTime) })
	Attribute("user_session_issuer_id", String)
	Attribute("trace_id", String)
	Attribute("steps", ArrayOf(AttemptStep))
	Attribute("context", AttemptContext)
	Required("started_at", "steps", "context")
})

var loggingEnabled = func() {
	Attribute("logging_enabled", Boolean, "False when the organization has logging off. Nothing else is returned then.")
	Attribute("viewer_scope", ViewerScope)
}

var ListAttemptsResult = Type("ListConnectionAttemptsResult", func() {
	loggingEnabled()
	Attribute("attempts", ArrayOf(AttemptRow))
	Attribute("next_cursor", String)
	Required("logging_enabled", "viewer_scope")
})

var FacetValue = Type("ConnectionFacetValue", func() {
	Attribute("value", String)
	Attribute("label", String)
	Attribute("attempts", Int64)
	Required("value", "attempts")
})

var Facets = Type("ConnectionAttemptFacets", func() {
	Attribute("outcomes", ArrayOf(FacetValue))
	Attribute("steps", ArrayOf(FacetValue))
	Attribute("servers", ArrayOf(FacetValue))
	Attribute("members", ArrayOf(FacetValue))
	Attribute("people", ArrayOf(FacetValue))
	Attribute("networks", ArrayOf(FacetValue))
	Attribute("apps", ArrayOf(FacetValue))
	Attribute("app_versions", ArrayOf(FacetValue))
	Attribute("reasons", ArrayOf(FacetValue))
	Attribute("services", ArrayOf(FacetValue))
	Required("outcomes", "steps", "servers", "members", "people", "networks", "apps", "app_versions", "reasons", "services")
})

var AttemptCountsResult = Type("ConnectionAttemptCountsResult", func() {
	loggingEnabled()
	Attribute("total_attempts", Int64)
	Attribute("facets", Facets)
	Attribute("truncated", Boolean, "True when counts were cut at the server's cap.")
	Required("logging_enabled", "viewer_scope")
})

var GetAttemptResult = Type("GetConnectionAttemptResult", func() {
	loggingEnabled()
	Attribute("found", Boolean)
	Attribute("attempt", AttemptDetail)
	Required("logging_enabled", "viewer_scope")
})

var SignInCounts = Type("ConnectionSignInCounts", func() {
	Attribute("succeeded", Int64)
	Attribute("did_not", Int64)
	Required("succeeded", "did_not")
})

var SignInBucket = Type("ConnectionSignInBucket", func() {
	Attribute("start", String, func() { Format(FormatDateTime) })
	Attribute("sign_ins", SignInCounts)
	Attribute("renewals", SignInCounts)
	Required("start", "sign_ins", "renewals")
})

var SignInSummaryResult = Type("ConnectionSignInSummaryResult", func() {
	loggingEnabled()
	Attribute("bucket", String, func() { Enum("hour", "six_hours", "day") })
	Attribute("buckets", ArrayOf(SignInBucket))
	Required("logging_enabled", "viewer_scope")
})

var SessionDay = Type("ConnectionSessionDay", func() {
	Attribute("day", String, "UTC day, YYYY-MM-DD.")
	Attribute("active_sessions", Int64)
	Required("day", "active_sessions")
})

var SessionActivityResult = Type("ConnectionSessionActivityResult", func() {
	Attribute("days", ArrayOf(SessionDay))
	Attribute("active_now", Int64)
	Attribute("average_session_seconds", Int64, "Mean length of sessions that ended in the range.")
	Attribute("configured_session_seconds", Int64)
	Required("days", "active_now", "configured_session_seconds")
})

var DeniedClient = Type("ConnectionDeniedClient", func() {
	Attribute("app", App)
	Attribute("reason", String)
	Attribute("message", String)
	Attribute("technical_message", String, "Reserved: a sentence for administrators about the reason. Not returned yet.")
	Attribute("attempts", Int64)
	Attribute("people", Int64)
	Attribute("networks", Int64)
	Attribute("last_at", String, func() { Format(FormatDateTime) })
	Required("app", "reason", "message", "attempts", "people", "networks", "last_at")
})

var DeniedClientsResult = Type("ConnectionDeniedClientsResult", func() {
	loggingEnabled()
	Attribute("clients", ArrayOf(DeniedClient))
	Required("logging_enabled", "viewer_scope")
})
