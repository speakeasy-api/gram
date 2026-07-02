package tokenexchange

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// --- Service ---

// tokenExchange is the device-agent token surface (DNO-383). The
// Speakeasy device agent holds an org-scoped API key with the `agent` scope. It
// exchanges a vouched user email for a long-lived, per-user API key carrying the
// `agent` and `hooks` scopes. The minted key is the credential the agent
// presents to the downstream user-scoped endpoints (agent.getPlugins,
// hooks.dispatch). The key is long-lived (api_keys has no TTL); its lifecycle
// lever is revocation, and a fresh exchange rotates (revokes + re-mints) the
// user's prior device-agent key.
var _ = Service("tokenExchange", func() {
	Description("Device-agent token exchange: trade an org-scoped agent API key plus a vouched user email for a long-lived, per-user API key scoped for the device agent.")

	shared.DeclareErrorResponses()

	Method("exchange", func() {
		Description("Exchange an org-scoped agent API key plus a vouched user email for a long-lived, per-user API key carrying the 'agent' and 'hooks' scopes. Authenticated with the org-scoped API key carrying the 'agent' scope, same scheme as agent.getPlugins. The raw key is returned exactly once.")

		Security(security.ByKey, func() {
			Scope("agent")
		})

		Payload(func() {
			security.ByKeyPayload()
			Attribute("email", String, "Email address of the enrolled user to mint a per-user key for. Resolved to a user within the authenticated org.", func() {
				Example("dev@acme.corp")
			})
			Required("email")
		})

		Result(TokenResult)

		HTTP(func() {
			// Path pinned explicitly, decoupled from the method name, so the
			// device-agent wire contract stays stable if the method is renamed.
			POST("/rpc/token-exchange")
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "tokenExchange")
		Meta("openapi:extension:x-speakeasy-name-override", "exchange")
	})
})

// --- Types ---

// TokenResult is the success shape for exchange. It preserves the RFC 6749 §5.1
// token-response shape the device agent already parses. Because the minted
// credential is a long-lived per-user API key rather than a refreshable JWT
// pair, `refresh_token` is empty and `expires_in` is zero — the device treats
// that as a mint-once, long-lived credential.
var TokenResult = Type("TokenResult", func() {
	Description("A minted per-user API key for the device agent.")
	Required("access_token", "refresh_token", "expires_in", "user_email")
	Attribute("access_token", String, "The raw per-user API key (carries the `agent` and `hooks` scopes). Returned exactly once; store it securely. Presented as the Gram-Key on downstream user-scoped endpoints.")
	Attribute("refresh_token", String, "Always empty. The minted key is long-lived and does not refresh; its lifecycle lever is revocation.")
	Attribute("expires_in", Int, "Always zero. The minted key has no expiry (api_keys has no TTL).")
	Attribute("user_email", String, "Email the key was minted for.")
})
