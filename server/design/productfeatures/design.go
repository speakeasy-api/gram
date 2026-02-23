package productfeatures

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("features", func() {
	Description("Manage product level feature controls.")

	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("setProductFeature", func() {
		Description("Enable or disable an organization feature flag.")

		Payload(func() {
			Attribute("feature_name", String, "Name of the feature to update", func() {
				MaxLength(60)
				Enum("logs", "tool_io_logs")
			})
			Attribute("enabled", Boolean, "Whether the feature should be enabled")
			Required("feature_name", "enabled")

			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/productFeatures.set")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setProductFeature")
		Meta("openapi:extension:x-speakeasy-name-override", "set")
	})
})
