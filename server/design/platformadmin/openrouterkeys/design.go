// Package openrouterkeys declares the adminOpenRouterKeys Goa service: the
// platform-admin (Speakeasy-only) surface over openrouter_api_keys, the
// per-(organization, key type) platform OpenRouter keys that pay for
// completions. It exposes each key's credit usage alongside the
// enable/disable actions. Key material never appears in any payload or
// result.
package openrouterkeys

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var AdminKey = Type("AdminOpenRouterKey", func() {
	Description("One organization's platform OpenRouter key of a given type, without its key material.")
	Required("organization_id", "organization_name", "organization_slug", "gram_account_type", "key_type", "monthly_credits", "disabled", "created_at", "updated_at")
	Attribute("organization_id", String, "Organization that owns the key.")
	Attribute("organization_name", String, "Display name of the owning organization.")
	Attribute("organization_slug", String, "Slug of the owning organization.")
	Attribute("gram_account_type", String, "The organization's Gram account type (e.g. free, pro, enterprise).")
	Attribute("key_type", String, "Which upstream key this row provisions: 'chat' pays for customer-facing completions, 'internal' pays for platform-initiated LLM usage.")
	Attribute("monthly_credits", Int64, "Monthly credit ceiling last mirrored from OpenRouter.")
	Attribute("disabled", Boolean, "Whether the key is locked down (refused locally and disabled upstream).")
	Attribute("disable_causes", ArrayOf(String), "Administrative view of every reason currently disabling the key. Values are open-ended for forward compatibility.")
	Attribute("created_at", String, "When the key row was created.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "When the key row was last updated.", func() { Format(FormatDateTime) })
})

var _ = Service("adminOpenRouterKeys", func() {
	Description("Platform-admin management of per-organization platform OpenRouter keys. Speakeasy-staff only; every method requires the platform-admin flag.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("listKeys", func() {
		Description("List every organization's platform OpenRouter keys. Requires platform admin.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(func() {
			Required("keys")
			Attribute("keys", ArrayOf(AdminKey), "Every live platform OpenRouter key, ordered by organization slug then key type.")
		})

		HTTP(func() {
			GET("/rpc/adminOpenRouterKeys.listKeys")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listAdminOpenRouterKeys")
		Meta("openapi:extension:x-speakeasy-name-override", "listKeys")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AdminOpenRouterKeys"}`)
	})

	Method("getKeyUsage", func() {
		Description("Fetch an organization's live credit usage from OpenRouter for one key. Requires platform admin.")

		Payload(func() {
			security.SessionPayload()
			Attribute("organization_id", String, "Organization that owns the key.")
			Attribute("key_type", String, "Key type to inspect.", func() { Enum("chat", "internal") })
			Required("organization_id", "key_type")
		})

		Result(func() {
			Description("Live usage as reported by OpenRouter at request time.")
			Required("credits_used", "monthly_credits")
			Attribute("credits_used", Float64, "Credits spent this month, rounded to cents.")
			Attribute("monthly_credits", Int64, "Monthly credit ceiling recorded locally.")
			Attribute("upstream_limit", Int64, "Monthly limit as configured upstream; absent when OpenRouter reports an unlimited key.")
		})

		HTTP(func() {
			GET("/rpc/adminOpenRouterKeys.getKeyUsage")
			Param("organization_id")
			Param("key_type")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAdminOpenRouterKeyUsage")
		Meta("openapi:extension:x-speakeasy-name-override", "getKeyUsage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AdminOpenRouterKeyUsage"}`)
	})

	Method("disableKey", func() {
		Description("Lock down an organization's platform OpenRouter key, upstream and locally. Requires platform admin.")

		Payload(func() {
			security.SessionPayload()
			Attribute("organization_id", String, "Organization that owns the key.")
			Attribute("key_type", String, "Key type to disable.", func() { Enum("chat", "internal") })
			Required("organization_id", "key_type")
			// Named explicitly: the disable and enable payloads are
			// structurally identical and Goa deduplicates request bodies by
			// shape, which would give one method the other's SDK type name.
			Meta("openapi:typename", "DisableOpenRouterKeyRequestBody")
		})

		Result(AdminKey)

		HTTP(func() {
			POST("/rpc/adminOpenRouterKeys.disableKey")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "disableAdminOpenRouterKey")
		Meta("openapi:extension:x-speakeasy-name-override", "disableKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DisableAdminOpenRouterKey"}`)
	})

	Method("enableKey", func() {
		Description("Reinstate a disabled platform OpenRouter key, upstream and locally, keeping its recorded credit ceiling. Requires platform admin.")

		Payload(func() {
			security.SessionPayload()
			Attribute("organization_id", String, "Organization that owns the key.")
			Attribute("key_type", String, "Key type to enable.", func() { Enum("chat", "internal") })
			Required("organization_id", "key_type")
			// Named explicitly: the disable and enable payloads are
			// structurally identical and Goa deduplicates request bodies by
			// shape, which would give one method the other's SDK type name.
			Meta("openapi:typename", "EnableOpenRouterKeyRequestBody")
		})

		Result(AdminKey)

		HTTP(func() {
			POST("/rpc/adminOpenRouterKeys.enableKey")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "enableAdminOpenRouterKey")
		Meta("openapi:extension:x-speakeasy-name-override", "enableKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "EnableAdminOpenRouterKey"}`)
	})
})
