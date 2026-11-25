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
		Description("Enable or disable signing with the organization's internal session issuer. Enabling provisions the issuer with a dedicated Speakeasy-managed signing key the first time; it does not configure trust at any service. Disabling keeps the issuer URI and its published keys. Requires org:admin; enabling also needs a verified email domain and the issuer rollout, while disabling never does.")
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
		Description("Rotate the issuer's signing key without changing its URI. A routine rotation takes two calls with the same active_kid: the first publishes a new key and returns rotation_ready_at, the second activates it after that time. The hourly sweep also activates a ready key on its own while the issuer is enabled and not paused, so a later second call may find the rotation already done and change nothing; a paused issuer can still be activated with the second call. With compromised set, every current key is revoked and withdrawn from the key set first, leaving the issuer keyless; the revoked keys' signing material is then disabled and, while the issuer is rolled out and the organization has a verified email domain, a fresh key activated. If either later step fails the issuer stays keyless or reports kms_cleanup_pending, and repeating the call retries it. Repeats naming a key that a compromised rotation already replaced change nothing; naming a key the sweep revoked after a routine rotation still contains its successor. Requires org:admin; a routine rotation also needs an enabled issuer, while a compromised rotation only needs an existing issuer and its revocation is never rate limited.")
		Payload(func() {
			Attribute("active_kid", String, "The key ID currently active, as last read. Required unless the issuer has no active key.")
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
	Attribute("state", String, "unavailable: not offered to this organization yet. verified_domain_required: needs at least one verified email domain. setup_required: not enabled yet. enabled: Speakeasy signs with it. disabled: an admin turned signing off. signing_paused: enabled, but Speakeasy has paused all signing. keyless: every key was revoked after a compromise and no replacement is active yet.", func() {
		Enum("unavailable", "verified_domain_required", "setup_required", "enabled", "disabled", "signing_paused", "keyless")
	})
	Attribute("can_enable", Boolean, "Whether deployment readiness, rollout and a verified email domain currently allow enabling this issuer. The caller still needs organization administrator access.")
	Attribute("issuer", String, "Issuer URI to register with supported services. It never changes.")
	Attribute("jwks_uri", String, "Public verification keys.")
	Attribute("keys", ArrayOf(InternalSessionIssuerKey), "Published keys.")
	Attribute("rotation_ready_at", String, "When a pending rotation can be activated.", func() {
		Format(FormatDateTime)
	})
	Attribute("kms_cleanup_pending", Int, "Revoked keys whose signing material is not yet disabled. Speakeasy retries until it is.")
	Attribute("replacement_error", String, "Set only by a compromised rotation whose replacement key could not be activated: the keys are contained, the issuer stays keyless, and repeating the call retries the replacement.")
	Required("state", "can_enable", "keys", "kms_cleanup_pending")
})

// InternalSessionIssuerKey is one published issuer key.
var InternalSessionIssuerKey = Type("InternalSessionIssuerKey", func() {
	Attribute("kid", String, "Key ID.")
	Attribute("state", String, "Lifecycle state.", func() {
		Enum("pending", "active", "retired")
	})
	Required("kid", "state")
})
