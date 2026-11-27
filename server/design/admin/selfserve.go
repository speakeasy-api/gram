package admin

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var AdminSelfServeStatus = Type("AdminSelfServeStatus", func() {
	Attribute("disabled", Boolean, "When true, no self-serve path can create an organization or arm a self-serve trial.")
	Attribute("updated_by", String, "Email of the staff member who last changed the switch. Absent if it was never changed.")
	Attribute("updated_at", String, func() {
		Description("When the switch last changed. Absent if it was never changed.")
		Format(FormatDateTime)
	})
	Required("disabled")
})

func selfServeMethods() {
	Method("getSelfServeStatus", func() {
		Description("Report whether self-serve signup is disabled across the platform.")
		Payload(func() { security.AdminAuthPayload() })
		Result(AdminSelfServeStatus)
		HTTP(func() { GET("/admin/selfServe"); Response(StatusOK) })
		Meta("openapi:operationId", "adminGetSelfServeStatus")
	})
	Method("setSelfServeDisabled", func() {
		Description("Turn self-serve signup off or on across the platform. Existing organizations are unaffected.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("disabled", Boolean)
			Required("disabled")
		})
		Result(AdminSelfServeStatus)
		HTTP(func() { POST("/admin/selfServe.set"); Response(StatusOK) })
		Meta("openapi:operationId", "adminSetSelfServeDisabled")
	})
}
