package productfeatures

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("features", func() {
	Description("Manage product level feature controls.")

	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("getProductFeatures", func() {
		Description("Get the current state of all product feature flags.")

		Payload(func() {
			Attribute("organization_id", String, "Organization whose product features to read.")
			Required("organization_id")
			security.SessionPayload()
		})

		Result(shared.ProductFeatures)

		HTTP(func() {
			GET("/rpc/productFeatures.get")
			Param("organization_id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getProductFeatures")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ProductFeatures"}`)
	})

	Method("setProductFeature", func() {
		Description("Enable or disable an organization feature flag. Staff-managed entitlements (such as sso and scim) additionally require a Speakeasy platform administrator; organization admins can set only the org-settable operational toggles.")

		Payload(func() {
			Attribute("organization_id", String, "Organization whose product feature to update.")
			Attribute("feature_name", shared.ProductFeatureName, "Name of the feature to update")
			Attribute("enabled", Boolean, "Whether the feature should be enabled")
			Required("organization_id", "feature_name", "enabled")

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

	Method("setRemoteSessionAutoRefreshPolicy", func() {
		Description("Set the organization policy for automatic remote-session refresh.")

		Payload(func() {
			Attribute("organization_id", String, "Organization whose automatic remote-session refresh policy to update.")
			Attribute("policy", String, "Organization policy for automatic remote-session refresh", func() {
				Enum("disabled", "user_controlled", "enforced")
			})
			Required("organization_id", "policy")

			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/productFeatures.setRemoteSessionAutoRefreshPolicy")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setRemoteSessionAutoRefreshPolicy")
		Meta("openapi:extension:x-speakeasy-name-override", "setRemoteSessionAutoRefreshPolicy")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetRemoteSessionAutoRefreshPolicy"}`)
	})
})
