package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminAIProductSummary = Type("AdminAIProductSummary", func() {
	Description("One product in the AI client catalog, as the catalog list shows it.")
	Required("id", "display_name", "vendor_key", "document_count", "deleted", "updated_at")

	Attribute("id", String, "Stable product identifier, e.g. claude-code.")
	Attribute("display_name", String)
	Attribute("category", String, "harness, assistant or local_model. Absent for a product known only as an OAuth client.")
	Attribute("vendor_key", String, "Groups the products a vendor ships.")
	Attribute("document_count", Int, "Documents not deleted, enabled or not.")
	Attribute("deleted", Boolean)
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
})

var AdminAIProductDocument = Type("AdminAIProductDocument", func() {
	Description("One Client ID Metadata Document a catalog product publishes.")
	Required("id", "url", "display_name", "enabled", "is_pattern", "deleted", "created_at", "updated_at")

	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("url", String, "The client_id, or a pattern where * stands for one path segment.")
	Attribute("display_name", String)
	Attribute("enabled", Boolean)
	Attribute("is_pattern", Boolean)
	Attribute("verified_at", String, "When the live document was last checked. Absent if never.", func() { Format(FormatDateTime) })
	Attribute("verification_error", String, "Reason code from the last check. Absent when it passed or never ran.")
	Attribute("created_by", String, "Staff subject who created the row. Absent for seeded rows.")
	Attribute("updated_by", String, "Staff subject who last changed the row. Absent for seeded rows.")
	Attribute("deleted", Boolean)
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
})

var AdminAIProduct = Type("AdminAIProduct", func() {
	Description("One product in the AI client catalog with its signatures and documents.")
	Required(
		"id", "display_name", "vendor_key", "bundle_ids", "binaries", "config_dirs", "process_names",
		"client_info_names", "deleted", "created_at", "updated_at", "blocked_organization_count", "documents",
	)

	Attribute("id", String)
	Attribute("display_name", String)
	Attribute("category", String)
	Attribute("vendor_key", String)
	Attribute("bundle_ids", ArrayOf(String), "macOS bundle identifiers.")
	Attribute("binaries", ArrayOf(String), "Command names resolved on the device PATH.")
	Attribute("config_dirs", ArrayOf(String), "Directories whose existence marks the product installed.")
	Attribute("process_names", ArrayOf(String), "Process names checked for the running signal.")
	Attribute("version_plist_key", String, "Info.plist key read for the version. Absent means CFBundleShortVersionString.")
	Attribute("client_info_names", ArrayOf(String), "Names the product reports at MCP initialize. Self-reported, attribution only.")
	Attribute("created_by", String)
	Attribute("updated_by", String)
	Attribute("deleted", Boolean)
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
	Attribute("blocked_organization_count", Int, "How many organizations block the product. A count only, never which ones.")
	Attribute("documents", ArrayOf(AdminAIProductDocument), "Every document, deleted ones included.")
})

// MCP parity: Staff Admin MCP tools for the AI client catalog are tracked
// separately (AIM-454). Not a Platform MCP tool: the catalog is global.
func aiCatalogMethods() {
	Method("listAIProducts", func() {
		Meta("openapi:operationId", "adminListAIProducts")
		Meta("openapi:extension:x-speakeasy-name-override", "listAIProducts")
		Description("Staff-only: list products in the AI client catalog.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("query", String, "Matches part of the product id or name, ignoring case.", func() { MaxLength(256) })
			Attribute("include_deleted", Boolean, "Include deleted products.", func() { Default(false) })
		})
		Result(func() {
			Attribute("products", ArrayOf(AdminAIProductSummary), "Live products first, then by name.")
			Attribute("truncated", Boolean, "Whether more products matched than the list returns.")
			Required("products", "truncated")
		})
		HTTP(func() {
			GET("/admin/aiProducts.list")
			Param("query")
			Param("include_deleted")
			Response(StatusOK)
		})
	})

	Method("getAIProduct", func() {
		Meta("openapi:operationId", "adminGetAIProduct")
		Meta("openapi:extension:x-speakeasy-name-override", "getAIProduct")
		Description("Staff-only: get one product in the AI client catalog, deleted or not.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("id", String, func() { MinLength(1); MaxLength(256) })
			Required("id")
		})
		Result(AdminAIProduct)
		HTTP(func() {
			GET("/admin/aiProducts.get")
			Param("id")
			Response(StatusOK)
		})
	})
}
