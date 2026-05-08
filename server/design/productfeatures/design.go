package productfeatures

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// ProductFeaturesResult is the result of getting product features.
var ProductFeaturesResult = ResultType("application/vnd.gram.product-features", func() {
	Description("Current state of product feature flags")
	Attributes(func() {
		Attribute("logs_enabled", Boolean, "Whether logging is enabled")
		Attribute("tool_io_logs_enabled", Boolean, "Whether tool I/O logging is enabled")
		Attribute("session_capture_enabled", Boolean, "Whether Claude Code session capture is enabled")
		Attribute("authz_challenge_logging_enabled", Boolean, "Whether authz challenge logging to ClickHouse is enabled")
		Required("logs_enabled", "tool_io_logs_enabled", "session_capture_enabled", "authz_challenge_logging_enabled")
	})
})

var _ = Service("features", func() {
	Description("Manage product level feature controls.")

	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("getProductFeatures", func() {
		Description("Get the current state of all product feature flags.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ProductFeaturesResult)

		HTTP(func() {
			GET("/rpc/productFeatures.get")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getProductFeatures")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
	})

	Method("setProductFeature", func() {
		Description("Enable or disable an organization feature flag.")

		Payload(func() {
			Attribute("feature_name", String, "Name of the feature to update", func() {
				MaxLength(60)
				Enum("logs", "tool_io_logs", "session_capture", "authz_challenge_logging")
			})
			Attribute("enabled", Boolean, "Whether the feature should be enabled")
			Required("feature_name", "enabled")

			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/productFeatures.set")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setProductFeature")
		Meta("openapi:extension:x-speakeasy-name-override", "set")
	})
})
