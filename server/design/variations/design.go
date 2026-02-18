package toolsets

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("variations", func() {
	Description("Manage variations of tools.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("upsertGlobal", func() {
		Description("Create or update a globally defined tool variation.")

		Payload(func() {
			Extend(UpsertGlobalToolVariationForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UpsertGlobalToolVariationResult)

		HTTP(func() {
			POST("/rpc/variations.upsertGlobal")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertGlobalVariation")
		Meta("openapi:extension:x-speakeasy-name-override", "upsertGlobal")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertGlobalVariation"}`)
	})

	Method("deleteGlobal", func() {
		Description("Create or update a globally defined tool variation.")

		Payload(func() {
			Extend(DeleteGlobalToolVariationForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(DeleteGlobalToolVariationResult)

		HTTP(func() {
			DELETE("/rpc/variations.deleteGlobal")
			Param("variation_id", String, "The ID of the variation to delete")

			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteGlobalVariation")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteGlobal")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "deleteGlobalVariation"}`)
	})

	Method("listGlobal", func() {
		Description("List globally defined tool variations.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListVariationsResult)

		HTTP(func() {
			GET("/rpc/variations.listGlobal")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listGlobalVariations")
		Meta("openapi:extension:x-speakeasy-name-override", "listGlobal")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GlobalVariations"}`)
	})
})

var UpsertGlobalToolVariationForm = Type("UpsertGlobalToolVariationForm", func() {
	Required("src_tool_name", "src_tool_urn")

	Attribute("src_tool_urn", String, "The URN of the source tool")
	Attribute("src_tool_name", String, "The name of the source tool")
	Attribute("confirm", String, "The confirmation mode for the tool variation", func() {
		Enum(conv.AnySlice(mv.ConfirmValues)...)
	})
	Attribute("confirm_prompt", String, "The confirmation prompt for the tool variation")
	Attribute("name", String, "The name of the tool variation")
	Attribute("summary", String, "The summary of the tool variation")
	Attribute("description", String, "The description of the tool variation")
	Attribute("tags", ArrayOf(String), "The tags of the tool variation")
	Attribute("summarizer", String, "The summarizer of the tool variation")
	Attribute("title", String, "Display name override for the tool")
	Attribute("read_only_hint", Boolean, "Override: if true, the tool does not modify its environment")
	Attribute("destructive_hint", Boolean, "Override: if true, the tool may perform destructive updates")
	Attribute("idempotent_hint", Boolean, "Override: if true, repeated calls have no additional effect")
	Attribute("open_world_hint", Boolean, "Override: if true, the tool interacts with external entities")
})

var UpsertGlobalToolVariationResult = Type("UpsertGlobalToolVariationResult", func() {
	Required("variation")

	Attribute("variation", shared.ToolVariation)
})

var ListVariationsResult = Type("ListVariationsResult", func() {
	Required("variations")

	Attribute("variations", ArrayOf(shared.ToolVariation))
})

var DeleteGlobalToolVariationForm = Type("DeleteGlobalToolVariationForm", func() {
	Required("variation_id")

	Attribute("variation_id", String, "The ID of the variation to delete")
})

var DeleteGlobalToolVariationResult = Type("DeleteGlobalToolVariationResult", func() {
	Required("variation_id")

	Attribute("variation_id", String, "The ID of the variation that was deleted")
})
