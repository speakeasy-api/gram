package admin

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var SupportFact = Type("SupportFact", func() {
	Attribute("status", String, func() {
		Meta("struct:tag:json", "status")
		Enum("supported", "partial", "unimplemented", "impossible", "na", "unknown")
	})
	Attribute("note", String, func() { Meta("struct:tag:json", "note"); MaxLength(10000) })
	Attribute("verify", Boolean, func() { Meta("struct:tag:json", "verify") })
	Required("status", "note", "verify")
})
var SupportMapping = Type("SupportMapping", func() {
	Attribute("applicability", String, func() { Meta("struct:tag:json", "applicability"); Enum("unknown", "applicable", "na") })
	Attribute("conditions", String, func() { Meta("struct:tag:json", "conditions"); MaxLength(10000) })
	Attribute("facts", MapOf(String, SupportFact), func() { Meta("struct:tag:json", "facts") })
	Required("applicability", "conditions", "facts")
})
var SupportDraft = Type("SupportDraft", func() {
	Attribute("mappings", MapOf(String, SupportMapping), func() { Meta("struct:tag:json", "mappings") })
	Attribute("references", MapOf(String, MapOf(String, SupportFact)), func() { Meta("struct:tag:json", "references") })
	Required("mappings", "references")
})
var SupportMethod = Type("SupportMethod", func() {
	Attribute("id", String, func() { Meta("struct:tag:json", "id") })
	Attribute("name", String, func() { Meta("struct:tag:json", "name") })
	Attribute("vendor", String, func() { Meta("struct:tag:json", "vendor") })
	Attribute("plans", String, func() { Meta("struct:tag:json", "plans") })
	Attribute("facts", MapOf(String, SupportFact), func() { Meta("struct:tag:json", "facts") })
	Required("id", "name", "vendor", "plans", "facts")
})
var SupportPlatform = Type("SupportPlatform", func() {
	Attribute("id", String, func() { Meta("struct:tag:json", "id") })
	Attribute("name", String, func() { Meta("struct:tag:json", "name") })
	Attribute("vendor", String, func() { Meta("struct:tag:json", "vendor") })
	Attribute("family", String, func() { Meta("struct:tag:json", "family") })
	Attribute("surface", String, func() { Meta("struct:tag:json", "surface") })
	Required("id", "name", "vendor", "family", "surface")
})
var SupportCapability = Type("SupportCapability", func() {
	Attribute("id", String, func() { Meta("struct:tag:json", "id") })
	Attribute("name", String, func() { Meta("struct:tag:json", "name") })
	Attribute("group", String, func() { Meta("struct:tag:json", "group") })
	Required("id", "name", "group")
})
var SupportMatrix = Type("SupportMatrix", func() {
	Attribute("methods", ArrayOf(SupportMethod), func() { Meta("struct:tag:json", "methods") })
	Attribute("products", ArrayOf(SupportPlatform), func() { Meta("struct:tag:json", "products") })
	Attribute("capabilities", ArrayOf(SupportCapability), func() { Meta("struct:tag:json", "capabilities") })
	Attribute("draft", SupportDraft, func() { Meta("struct:tag:json", "draft") })
	Attribute("revision", String, func() { Meta("struct:tag:json", "revision") })
	Required("methods", "products", "capabilities", "draft", "revision")
})

func supportMatrixMethods() {
	Method("getSupportMatrix", func() {
		Meta("openapi:operationId", "adminGetSupportMatrix")
		Meta("openapi:extension:x-speakeasy-name-override", "getSupportMatrix")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminGetSupportMatrix"}`)
		Description("Read the shared support catalog and product coverage.")
		Payload(func() { security.AdminAuthPayload() })
		Result(SupportMatrix)
		HTTP(func() { GET("/admin/supportMatrix.get"); Response(StatusOK) })
	})
	Method("updateSupportMatrix", func() {
		Meta("openapi:operationId", "adminUpdateSupportMatrix")
		Meta("openapi:extension:x-speakeasy-name-override", "updateSupportMatrix")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminUpdateSupportMatrix"}`)
		Description("Save coverage against the last read revision; rejects concurrent changes.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("revision", String, func() { MinLength(64); MaxLength(64) })
			Attribute("draft", SupportDraft)
			Required("revision", "draft")
		})
		Result(SupportMatrix)
		HTTP(func() { POST("/admin/supportMatrix.update"); Response(StatusOK) })
	})
}
