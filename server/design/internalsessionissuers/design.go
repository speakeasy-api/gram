package internalsessionissuers

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// The internalSessionIssuers service manages the issuer Speakeasy operates for
// an organization's enterprise-managed authorization: a permanent issuer URI
// vendors trust, whose signing keys rotate behind it.
var _ = Service("internalSessionIssuers", func() {
	Description("Manage the organization's internal session issuer: the permanent issuer URI Speakeasy signs enterprise-managed authorization assertions with, and its signing keys.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("get", func() {
		Description("Read the organization's internal session issuer: its state, the issuer URI to register with supported services and its published keys. Requires org:read.")
		Payload(func() { security.SessionPayload() })
		Result(InternalSessionIssuer)
		HTTP(func() {
			GET("/rpc/internalSessionIssuers.get")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getInternalSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "InternalSessionIssuer"}`)
	})

	Method("setEnabled", func() {
		Description("Enable or disable signing with the organization's internal session issuer. Enabling provisions the issuer with a dedicated Speakeasy-managed signing key the first time; it does not configure trust at any service. Disabling keeps the issuer URI and its published keys. Requires org:admin; enabling also needs the organization's single sign-on entitlement and the issuer rollout, while disabling never does.")
		Payload(func() {
			Attribute("enabled", Boolean, "Whether Speakeasy may sign with the issuer.")
			Required("enabled")
			security.SessionPayload()
		})
		Result(InternalSessionIssuer)
		HTTP(func() {
			POST("/rpc/internalSessionIssuers.setEnabled")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "setInternalSessionIssuerEnabled")
		Meta("openapi:extension:x-speakeasy-name-override", "setEnabled")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetInternalSessionIssuerEnabled"}`)
	})

	Method("rotate", func() {
		Description("Rotate the issuer's signing key without changing its URI. A routine rotation takes two calls with the same active_kid: the first publishes a new key and returns rotation_ready_at, the second activates it after that time. With compromised set, every current key is revoked at once and a fresh key activated immediately. Requires org:admin; a routine rotation also needs an enabled issuer, while a compromised rotation only needs an existing issuer and is never rate limited.")
		Payload(func() {
			Attribute("active_kid", String, "The key ID currently active, as last read. Required unless compromised is set.")
			Attribute("compromised", Boolean, "Revoke every current key immediately instead of rotating gradually.", func() {
				Default(false)
			})
			security.SessionPayload()
		})
		Result(InternalSessionIssuer)
		HTTP(func() {
			POST("/rpc/internalSessionIssuers.rotate")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "rotateInternalSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "rotate")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RotateInternalSessionIssuer"}`)
	})
})

// InternalSessionIssuer is an organization's public trust anchor, never a signing capability.
var InternalSessionIssuer = Type("InternalSessionIssuer", func() {
	Attribute("state", String, "unavailable: not offered to this organization yet. entitlement_required: needs single sign-on. setup_required: not enabled yet. enabled: Speakeasy signs with it. disabled: an admin turned signing off. signing_paused: enabled, but Speakeasy has paused all signing.", func() {
		Enum("unavailable", "entitlement_required", "setup_required", "enabled", "disabled", "signing_paused")
	})
	Attribute("issuer", String, "Issuer URI to register with supported services. It never changes.")
	Attribute("jwks_uri", String, "Public verification keys.")
	Attribute("keys", ArrayOf(InternalSessionIssuerKey), "Published keys.")
	Attribute("rotation_ready_at", String, "When a pending rotation can be activated.", func() {
		Format(FormatDateTime)
	})
	Required("state", "keys")
})

// InternalSessionIssuerKey is one published issuer key.
var InternalSessionIssuerKey = Type("InternalSessionIssuerKey", func() {
	Attribute("kid", String, "Key ID.")
	Attribute("state", String, "Lifecycle state.", func() {
		Enum("pending", "active", "retired")
	})
	Required("kid", "state")
})
