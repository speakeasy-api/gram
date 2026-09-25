package launcher

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// --- Service ---

var _ = Service("launcher", func() {
	Description("Rank command palette candidates by the intent behind the text the user typed.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()
	HTTP(func() {
		shared.DeclareHTTPErrorResponses()
	})

	Method("judge", func() {
		Description("Judge which of the supplied command palette candidates the typed query refers to, what kind of action it asks for, and whether the intent is settled enough to act on Enter. Returns probability distributions rather than text. When no intent service is configured the response carries disabled=true and the palette falls back to fuzzy ordering.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("query", String, "Text typed into the command palette so far.", func() {
				MaxLength(200)
			})
			Attribute("context", LauncherContext, "Where in the dashboard the palette was opened.")
			Attribute("candidates", ArrayOf(LauncherCandidate), "Fuzzy-prefiltered candidates to rank.", func() {
				MaxLength(32)
			})
			Required("query", "candidates")
		})

		Result(LauncherJudgment)

		HTTP(func() {
			POST("/rpc/launcher.judge")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "launcherJudge")
		Meta("openapi:extension:x-speakeasy-name-override", "judge")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "LauncherJudge"}`)
	})
})

// --- Types ---

var LauncherContext = Type("LauncherContext", func() {
	Description("The dashboard location the palette was opened from.")

	Attribute("route", String, "The current dashboard route path.", func() {
		MaxLength(512)
	})
})

var LauncherCandidate = Type("LauncherCandidate", func() {
	Description("One item the palette could open or act on.")

	Attribute("id", String, "Caller-chosen stable identifier echoed back in the judgment.", func() {
		MaxLength(200)
	})
	Attribute("kind", String, "The candidate kind, such as page, mcp_server, plugin or marketplace.", func() {
		MaxLength(64)
	})
	Attribute("title", String, "Display title of the candidate.", func() {
		MaxLength(120)
	})
	Attribute("detail", String, "Short state description, such as 'MCP server · disabled'.", func() {
		MaxLength(160)
	})
	Attribute("verbs", ArrayOf(String, func() {
		MaxLength(32)
	}), "Verbs the caller may run on this candidate, such as open, enable, disable or publish.", func() {
		MaxLength(8)
	})
	Required("id", "kind", "title")
})

var LauncherJudgment = Type("LauncherJudgment", func() {
	Description("Probability distributions over the supplied candidates and verbs.")

	Attribute("disabled", Boolean, "True when no intent service is configured; the other fields are then absent. When false, target, action, ready and latency_ms are always present.")
	Attribute("target", MapOf(String, Float64), "Probability that each candidate id, or 'none', is the item the user means.")
	Attribute("action", MapOf(String, Float64), "Probability that the query asks for each of open, enable, disable, publish or unclear.")
	Attribute("ready", Float64, "Probability that the query is unambiguous enough to act on the best candidate on Enter.")
	Attribute("latency_ms", Int64, "Round trip time to the intent service in milliseconds.")
	Required("disabled")
})
