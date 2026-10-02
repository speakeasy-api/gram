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

var AdminRegistryOktaCandidate = Type("AdminRegistryOktaCandidate", func() {
	Description("An Okta application name observed across synced tenants that plausibly belongs to a catalog entry. A proposal for staff to confirm, never applied automatically.")
	Attribute("oin_name", String, "Okta application name, the OIN key.")
	Attribute("organizations", Int, "How many organizations have the application.")
	Attribute("sign_on_modes", ArrayOf(String), "Sign-on modes seen for the application.")
	Attribute("reason", String, "domain: the vendor token matches a remote or website host; title: it matches the entry title; label: a tenant's label equals the entry title.", func() {
		Enum("domain", "title", "label")
	})
	Attribute("integrator", Boolean, "Whether the name comes from an integrator listing rather than Okta's public catalog; any integrator account can publish under a vendor-like key, so confirm with care.")
	Attribute("mapped_by", String, "Name of the entry that already claims this key, when one does.")
	Required("oin_name", "organizations", "sign_on_modes", "reason", "integrator")
})
var AdminRegistryOktaCandidates = Type("AdminRegistryOktaCandidates", func() {
	Attribute("candidates", ArrayOf(AdminRegistryOktaCandidate), "Strongest reason first, then by organizations.")
	Required("candidates")
})
var AdminRegistryOktaUnmappedName = Type("AdminRegistryOktaUnmappedName", func() {
	Description("An observed Okta application name no catalog entry claims.")
	Attribute("oin_name", String)
	Attribute("organizations", Int)
	Attribute("sign_on_modes", ArrayOf(String))
	Attribute("integrator", Boolean, "Whether the name comes from an integrator listing rather than Okta's public catalog.")
	Attribute("suggested_entry_id", String, "The entry the heuristic would map it to, when one matches.")
	Attribute("suggested_entry_name", String)
	Attribute("reason", String, func() {
		Enum("domain", "title", "label")
	})
	Required("oin_name", "organizations", "sign_on_modes", "integrator")
})
var AdminRegistryOktaUnmapped = Type("AdminRegistryOktaUnmapped", func() {
	Attribute("names", ArrayOf(AdminRegistryOktaUnmappedName), "By organizations, most first. Okta's own applications are left out.")
	Required("names")
})

func registryDesign() {
	Method("getRegistryOktaCandidates", func() {
		Description("Staff-only registry administration: Okta application names observed across synced tenants that plausibly belong to the entry, for confirmation in the editor.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("id", String, func() { Format(FormatUUID) })
			Required("id")
		})
		Result(AdminRegistryOktaCandidates)
		HTTP(func() {
			GET("/admin/registry.oktaCandidates")
			Param("id")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminGetRegistryOktaCandidates")
		Meta("openapi:extension:x-speakeasy-name-override", "getRegistryOktaCandidates")
	})
	Method("listRegistryOktaUnmapped", func() {
		Description("Staff-only registry administration: observed Okta application names no entry claims yet, with the entry the heuristic would propose.")
		Payload(func() {
			security.AdminAuthPayload()
		})
		Result(AdminRegistryOktaUnmapped)
		HTTP(func() {
			GET("/admin/registry.oktaUnmapped")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminListRegistryOktaUnmapped")
		Meta("openapi:extension:x-speakeasy-name-override", "listRegistryOktaUnmapped")
	})
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
