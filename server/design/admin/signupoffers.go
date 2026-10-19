package admin

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var AdminSignupOffer = Type("AdminSignupOffer", func() {
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("name", String)
	Attribute("stripe_promotion_code_id", String)
	Attribute("signup_flow", String, func() { Enum("tailscale_partner_gateway") })
	Attribute("enabled", Boolean)
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
	Required("id", "name", "stripe_promotion_code_id", "signup_flow", "enabled", "created_at", "updated_at")
})

func signupOfferMethods() {
	Method("listSignupOffers", func() {
		Description("List staff-managed signup offers. Stripe owns all promotion terms.")
		Payload(func() { security.AdminAuthPayload() })
		Result(func() { Attribute("offers", ArrayOf(AdminSignupOffer)); Required("offers") })
		HTTP(func() { GET("/admin/signupOffers"); Response(StatusOK) })
		Meta("openapi:operationId", "adminListSignupOffers")
	})
	Method("createSignupOffer", func() {
		Description("Create a disabled signup offer mapped to an exact Stripe promotion code ID.")
		Payload(func() {
			security.AdminAuthPayload()
			signupOfferAttributes()
		})
		Result(AdminSignupOffer)
		HTTP(func() { POST("/admin/signupOffers.create"); Response(StatusOK) })
		Meta("openapi:operationId", "adminCreateSignupOffer")
	})
	Method("updateSignupOffer", func() {
		Description("Edit, enable or disable a signup offer. Existing accepted promotion applications are unchanged.")
		Payload(func() {
			security.AdminAuthPayload()
			signupOfferAttributes()
			Attribute("id", String, func() { Format(FormatUUID) })
			Attribute("enabled", Boolean)
			Required("id", "enabled")
		})
		Result(AdminSignupOffer)
		HTTP(func() { POST("/admin/signupOffers.update"); Response(StatusOK) })
		Meta("openapi:operationId", "adminUpdateSignupOffer")
	})
}

func signupOfferAttributes() {
	Attribute("name", String, func() { MinLength(1); MaxLength(200) })
	Attribute("stripe_promotion_code_id", String, func() { Pattern("^promo_[A-Za-z0-9_]+$"); MaxLength(255) })
	Attribute("signup_flow", String, func() { Enum("tailscale_partner_gateway") })
	Required("name", "stripe_promotion_code_id", "signup_flow")
}
