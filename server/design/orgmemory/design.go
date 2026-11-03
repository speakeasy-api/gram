package orgmemory

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var Digest = Type("OrgMemoryDigest", func() {
	Description("A day, week or month digest of a project's Organizational Memory.")
	Required("id", "level", "period_start", "period_end", "content", "session_count", "memo_count", "child_count", "generated_at")
	Attribute("id", String, "Digest ID.", func() { Format(FormatUUID) })
	Attribute("level", String, "Period the digest covers.", func() { Enum("day", "week", "month") })
	Attribute("period_start", String, "First UTC day of the period.", func() { Format(FormatDate) })
	Attribute("period_end", String, "First UTC day after the period.", func() { Format(FormatDate) })
	Attribute("content", String, "Digest text, written only from masked summaries, memos and child digests.")
	Attribute("session_count", Int, "Sessions a day digest folded. Zero for weeks and months.")
	Attribute("memo_count", Int, "Memos a day digest folded. Zero for weeks and months.")
	Attribute("child_count", Int, "Digests one level down that a week or month digest folded.")
	Attribute("generated_at", String, "When the digest was written.", func() { Format(FormatDateTime) })
})

var Session = Type("OrgMemorySession", func() {
	Description("A session as Organizational Memory sees it: its masked summary, never the transcript.")
	Required("id", "title", "summary", "summarized_at")
	Attribute("id", String, "Chat ID.", func() { Format(FormatUUID) })
	Attribute("title", String, "Session title. Empty unless the caller also holds chat:read on the project.")
	Attribute("summary", String, "Masked session summary.")
	Attribute("summarized_at", String, "When the summary was written.", func() { Format(FormatDateTime) })
})

var Memo = Type("OrgMemoryMemo", func() {
	Description("A fact an agent saved to Organizational Memory with remember.")
	Required("id", "content", "author_kind", "created_at")
	Attribute("id", String, "Memo ID.", func() { Format(FormatUUID) })
	Attribute("content", String, "Masked memo text.")
	Attribute("chat_id", String, "Session the memo was saved from, when it names one.", func() { Format(FormatUUID) })
	Attribute("author_kind", String, "Who saved the memo.", func() { Enum("user", "agent", "system") })
	Attribute("author_name", String, "Display name of the person or agent that saved the memo, when known.")
	Attribute("created_at", String, "When the memo was saved.", func() { Format(FormatDateTime) })
})

var _ = Service("orgMemory", func() {
	Description("Browse a project's Organizational Memory: digests, the sessions and memos they fold, and recent activity. Requires memory:read on the project and Business Memory Extraction turned on for the organization.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("getOrgMemoryOverview", func() {
		Description("Summarize the project's Organizational Memory over a recent window.")
		Payload(func() {
			Attribute("days", Int, "Window size in days.", func() {
				Default(30)
				Minimum(1)
				Maximum(90)
			})
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(func() {
			Required("days", "sessions_remembered", "sessions_left_out", "memos_saved", "recalls")
			Attribute("days", Int, "Window size in days.")
			Attribute("sessions_remembered", Int64, "Sessions with a masked summary in memory.")
			Attribute("sessions_left_out", Int64, "Summarized sessions kept out of memory: excluded or from a personal account.")
			Attribute("memos_saved", Int64, "Memos saved to memory.")
			Attribute("recalls", Int64, "Times agents read memory through the Platform MCP.")
		})
		HTTP(func() {
			GET("/rpc/orgMemory.overview")
			Param("days")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getOrgMemoryOverview")
		Meta("openapi:extension:x-speakeasy-name-override", "overview")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrgMemoryOverview"}`)
	})

	Method("listOrgMemoryDigests", func() {
		Description("List the project's digests at one level, newest period first.")
		Payload(func() {
			Attribute("level", String, "Digest level to list.", func() {
				Enum("day", "week", "month")
				Default("week")
			})
			Attribute("before", String, "Only digests whose period starts before this UTC day.", func() { Format(FormatDate) })
			Attribute("limit", Int, "Number of digests to return.", func() {
				Default(12)
				Minimum(1)
				Maximum(62)
			})
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(func() {
			Required("digests")
			Attribute("digests", ArrayOf(Digest))
			Attribute("next_before", String, "Pass as before to load older digests.", func() { Format(FormatDate) })
		})
		HTTP(func() {
			GET("/rpc/orgMemory.listDigests")
			Param("level")
			Param("before")
			Param("limit")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listOrgMemoryDigests")
		Meta("openapi:extension:x-speakeasy-name-override", "listDigests")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrgMemoryDigests"}`)
	})

	Method("getOrgMemoryDigest", func() {
		Description("Load one digest with what it cites: child digests for a week or month, sessions and memos for a day. Sources that left memory since the digest was written are omitted.")
		Payload(func() {
			Required("id")
			Attribute("id", String, "Digest ID.", func() { Format(FormatUUID) })
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(func() {
			Required("digest", "children", "sessions", "memos")
			Attribute("digest", Digest)
			Attribute("children", ArrayOf(Digest), "Digests one level down, oldest first.")
			Attribute("sessions", ArrayOf(Session), "Sessions a day digest folded, oldest first.")
			Attribute("memos", ArrayOf(Memo), "Memos a day digest folded, oldest first.")
		})
		HTTP(func() {
			GET("/rpc/orgMemory.getDigest")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getOrgMemoryDigest")
		Meta("openapi:extension:x-speakeasy-name-override", "getDigest")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrgMemoryDigest"}`)
	})

	Method("listOrgMemoryRecent", func() {
		Description("List sessions and memos from the last week that no day digest has folded yet, newest first.")
		Payload(func() {
			Attribute("limit", Int, "Maximum sessions and maximum memos to return.", func() {
				Default(20)
				Minimum(1)
				Maximum(100)
			})
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(func() {
			Required("sessions", "memos")
			Attribute("sessions", ArrayOf(Session))
			Attribute("memos", ArrayOf(Memo))
		})
		HTTP(func() {
			GET("/rpc/orgMemory.recent")
			Param("limit")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listOrgMemoryRecent")
		Meta("openapi:extension:x-speakeasy-name-override", "recent")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrgMemoryRecent"}`)
	})
})
