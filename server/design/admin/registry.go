package admin

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var AdminRegistryIssue = Type("AdminRegistryIssue", func() {
	Attribute("path", String)
	Attribute("message", String)
	Required("path", "message")
})
var AdminRegistryEntry = Type("AdminRegistryEntry", func() {
	Attribute("id", String)
	Attribute("data_json", String, "Complete lossless registry record JSON")
	Attribute("published", Boolean)
	Attribute("created_at", String)
	Attribute("updated_at", String, "Opaque write precondition; echo unchanged")
	Attribute("issues", ArrayOf(AdminRegistryIssue))
	Required("id", "data_json", "published", "created_at", "updated_at", "issues")
})
var AdminRegistrySummary = Type("AdminRegistrySummary", func() {
	Attribute("id", String)
	Attribute("name", String)
	Attribute("published", Boolean)
	Attribute("updated_at", String)
	Attribute("issues", ArrayOf(AdminRegistryIssue))
	Required("id", "name", "published", "updated_at", "issues")
})
var AdminRegistryPage = Type("AdminRegistryPage", func() {
	Attribute("entries", ArrayOf(AdminRegistrySummary))
	Attribute("next_cursor", String)
	Required("entries")
})

func registryDesign() {
	Method("listRegistryEntries", func() {
		Description("Staff-only registry administration.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("query", String, "Search query; at most 1024 UTF-8 bytes (enforced by the server).")
			Attribute("published", Boolean)
			Attribute("cursor", String, "Opaque continuation cursor from next_cursor; at most 8192 UTF-8 bytes (enforced by the server).")
			Attribute("limit", Int32, "Page size; zero uses the server default of 25.", func() { Minimum(0); Maximum(50) })
		})
		Result(AdminRegistryPage)
		HTTP(func() {
			GET("/admin/registry.list")
			Param("query")
			Param("published")
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminListRegistryEntries")
		Meta("openapi:extension:x-speakeasy-name-override", "listRegistryEntries")
	})
	Method("getRegistryEntry", func() {
		Description("Staff-only registry administration.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("id", String, func() { Format(FormatUUID) })
			Required("id")
		})
		Result(AdminRegistryEntry)
		HTTP(func() {
			GET("/admin/registry.get")
			Param("id")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminGetRegistryEntry")
		Meta("openapi:extension:x-speakeasy-name-override", "getRegistryEntry")
	})
	Method("createRegistryEntry", func() {
		Description("Staff-only registry administration.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("data_json", String, "Complete registry record JSON; at most 8388608 UTF-8 bytes (8 MiB), enforced by the server on incoming writes. Stored records remain readable for repair.")
			Required("data_json")
		})
		Result(AdminRegistryEntry)
		HTTP(func() {
			POST("/admin/registry.create")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminCreateRegistryEntry")
		Meta("openapi:extension:x-speakeasy-name-override", "createRegistryEntry")
	})
	Method("saveRegistryEntry", func() {
		Description("Staff-only registry administration.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("id", String, func() { Format(FormatUUID) })
			Attribute("updated_at", String)
			Attribute("data_json", String, "Complete registry record JSON; at most 8388608 UTF-8 bytes (8 MiB), enforced by the server on incoming writes. Stored records remain readable for repair.")
			Required("id", "updated_at", "data_json")
		})
		Result(AdminRegistryEntry)
		HTTP(func() {
			POST("/admin/registry.save")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminSaveRegistryEntry")
		Meta("openapi:extension:x-speakeasy-name-override", "saveRegistryEntry")
	})
	Method("setRegistryEntryPublished", func() {
		Description("Staff-only registry administration.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("id", String, func() { Format(FormatUUID) })
			Attribute("updated_at", String)
			Attribute("published", Boolean)
			Required("id", "updated_at", "published")
		})
		Result(AdminRegistryEntry)
		HTTP(func() {
			POST("/admin/registry.setPublished")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminSetRegistryEntryPublished")
		Meta("openapi:extension:x-speakeasy-name-override", "setRegistryEntryPublished")
	})
}
