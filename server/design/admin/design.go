package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("admin", func() {
	Description("Operational endpoints for administrative tasks.")
	shared.DeclareErrorResponses()

	Method("poke", func() {
		Result(func() {
			Required("ok")
			Attribute("ok", Boolean)
		})
		HTTP(func() {
			GET("/admin/diagnostics.poke")
			Response(StatusOK)
		})
	})
})
