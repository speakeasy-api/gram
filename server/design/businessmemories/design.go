package businessmemories

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var Memory = Type("BusinessMemory", func() {
	Description("Knowledge extracted from a completed business session.")
	Required(
		"id",
		"body",
		"memory_type",
		"structural_scope",
		"content_scope",
		"embedding_model",
		"extraction_model",
		"source_chat_id",
		"extracted_at",
		"lifecycle_state",
	)
	Attribute("id", String, "Memory ID.", func() { Format(FormatUUID) })
	Attribute("body", String, "Normalized knowledge body.")
	Attribute("memory_type", String, "Knowledge type.", func() {
		Enum("glossary", "procedure", "result")
	})
	Attribute("structural_scope", String, "Materialized structural scope path.")
	Attribute("content_scope", ArrayOf(String), "Canonical entity and topic labels.")
	Attribute("embedding_model", String, "Model used to create the semantic-search vector.")
	Attribute("extraction_model", String, "Model used to extract the memory.")
	Attribute("source_chat_id", String, "Chat the memory was extracted from, or unavailable if the source chat was deleted.")
	Attribute("source_turn", Int, "One-based transcript turn cited by the extractor.")
	Attribute("source_author_id", String, "Author associated with the source session.")
	Attribute("extracted_at", String, "Extraction timestamp.", func() { Format(FormatDateTime) })
	Attribute("lifecycle_state", String, "Current lifecycle state.")
	Attribute("similarity", Float64, "Cosine similarity for semantic-search results.")
})

var ContentScopeNode = Type("BusinessMemoryContentScopeNode", func() {
	Description("A content-scope namespace or exact tag and its distinct-memory count.")
	Required("scope", "memory_count")
	Attribute("scope", String, "Namespace or exact content-scope tag.")
	Attribute("parent_scope", String, "Parent namespace. Omitted for namespace nodes.")
	Attribute("memory_count", Int64, "Number of distinct memories assigned to this scope.")
})

func declareContentScopeFilters() {
	Attribute("content_scope", String, "Exact content-scope tag to match.", func() {
		Pattern(`^[a-z0-9][a-z0-9:_-]{0,127}$`)
	})
	Attribute("content_scope_namespace", String, "Content-scope namespace to match.", func() {
		Pattern(`^[a-z0-9][a-z0-9_-]{0,127}$`)
	})
}

var _ = Service("businessMemories", func() {
	Description("Inspect and semantically search business memories.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listBusinessMemories", func() {
		Description("List memories extracted for the active project. Requires organization admin.")
		Payload(func() {
			Attribute("cursor", String, "Cursor for the next result page.")
			declareContentScopeFilters()
			Attribute("limit", Int, "Number of memories to return.", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(func() {
			Required("memories")
			Attribute("memories", ArrayOf(Memory))
			Attribute("next_cursor", String)
		})
		HTTP(func() {
			GET("/rpc/businessMemories.list")
			Param("cursor")
			Param("content_scope")
			Param("content_scope_namespace")
			Param("limit")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		shared.CursorPagination()
		Meta("openapi:operationId", "listBusinessMemories")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListBusinessMemories"}`)
	})

	Method("listBusinessMemoryContentScopes", func() {
		Description("List the complete content-scope tree and distinct-memory counts for the active project. Requires organization admin.")
		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(func() {
			Required("nodes", "total_memories")
			Attribute("nodes", ArrayOf(ContentScopeNode), "Namespace and exact-tag nodes in lexical order.")
			Attribute("total_memories", Int64, "Total number of active memories, including memories without a content scope.")
		})
		HTTP(func() {
			GET("/rpc/businessMemories.contentScopes")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listBusinessMemoryContentScopes")
		Meta("openapi:extension:x-speakeasy-name-override", "listContentScopes")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListBusinessMemoryContentScopes"}`)
	})

	Method("searchBusinessMemories", func() {
		Description("Run semantic search over active memories in the active project. Requires organization admin.")
		shared.DeclareHostedInferenceErrors()
		Payload(func() {
			Attribute("query", String, "Natural-language semantic search query.", func() {
				MinLength(1)
				MaxLength(2000)
			})
			declareContentScopeFilters()
			Attribute("limit", Int, "Maximum search results.", func() {
				Default(20)
				Minimum(1)
				Maximum(100)
			})
			Required("query")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(func() {
			Required("memories")
			Attribute("memories", ArrayOf(Memory))
		})
		HTTP(func() {
			POST("/rpc/businessMemories.search")
			shared.DeclareHTTPHostedInferenceErrorResponses()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "searchBusinessMemories")
		Meta("openapi:extension:x-speakeasy-name-override", "search")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SearchBusinessMemories"}`)
	})
})
