package assistantmemories

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("assistantMemories", func() {
	Description("Manage assistant memory records.")

	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listAssistantMemories", func() {
		Description("List assistant memories for an assistant.")

		Payload(func() {
			Attribute("assistant_id", String, "The assistant ID.", func() {
				Format(FormatUUID)
			})
			Attribute("tags", ArrayOf(String), "Optional tags to filter memories by.")
			Attribute("include_deleted", Boolean, "Whether to include soft-deleted memories.", func() {
				Default(false)
			})
			Attribute("cursor", String, "The cursor to fetch results from.")
			Attribute("limit", Int, "The number of memories to return per page.", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Required("assistant_id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListAssistantMemoriesResult)

		HTTP(func() {
			GET("/rpc/assistantMemories.list")
			Param("assistant_id")
			Param("tags")
			Param("include_deleted")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listAssistantMemories")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListAssistantMemories"}`)
	})

	Method("getAssistantMemory", func() {
		Description("Get an assistant memory by ID.")

		Payload(func() {
			Attribute("id", String, "The assistant memory ID.", func() {
				Format(FormatUUID)
			})
			Required("id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.AssistantMemory)

		HTTP(func() {
			GET("/rpc/assistantMemories.get")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAssistantMemory")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetAssistantMemory"}`)
	})

	Method("deleteAssistantMemory", func() {
		Description("Delete an assistant memory by ID.")

		Payload(func() {
			Attribute("id", String, "The assistant memory ID.", func() {
				Format(FormatUUID)
			})
			Required("id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/assistantMemories.delete")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteAssistantMemory")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
	})
})

var ListAssistantMemoriesResult = Type("ListAssistantMemoriesResult", func() {
	Required("memories")
	Attribute("memories", ArrayOf(shared.AssistantMemory), "Assistant memories matching the query.")
	Attribute("next_cursor", String, func() {
		Description("The cursor to be used for the next page of results.")
	})
})
