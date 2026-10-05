package cliauth

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// The cliAuth service backs the Speakeasy device agent's interactive
// enrollment (DNO-388). It is a PKCE one-time-code exchange:
//
//  1. The dashboard, on behalf of the signed-in user, calls authorize with a
//     PKCE code_challenge and receives a short-lived opaque code.
//  2. The device agent (which holds the matching code_verifier) calls redeem
//     with {code, code_verifier}. The verifier proving knowledge of the
//     challenge IS the credential, so redeem takes no session/api-key auth.
//     On success it mints a per-user [agent_user] API key, adding [hooks]
//     only for proof-bound relay enrollment, and returns the
//     raw key exactly once.
var _ = Service("cliAuth", func() {
	Description("Interactive device-agent enrollment via a PKCE one-time-code exchange. authorize (dashboard session) mints a short-lived code bound to a PKCE challenge; redeem (no auth — the code+verifier pair is the credential) exchanges it once for a per-user [agent_user] API key. Proof-bound hooks relay enrollment also grants [hooks].")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("authorize", func() {
		Description("Mint a short-lived one-time code bound to a PKCE code_challenge, on behalf of the authenticated dashboard user. Resolves the target project (given slug, else the org's default/first project) and records {user, org, project, scopes, challenge} against the code with a ~5 minute TTL. The ordinary scope is [agent_user]; proof-bound hooks relay enrollment adds [hooks]. Requires a member-available session (org:read); NOT org-admin. Refused (403) while impersonating an organization or user, or without membership in the active org.")

		Security(security.Session)

		Payload(func() {
			Attribute("code_challenge", String, "PKCE code challenge: base64url(sha256(code_verifier)).", func() {
				MinLength(43)
				MaxLength(128)
			})
			Attribute("code_challenge_method", String, "PKCE challenge method. Only S256 is supported.", func() {
				Enum("S256")
			})
			Attribute("project_slug", String, "Optional project slug to scope the minted key to. Defaults to the org's default (first) project when omitted.")
			Attribute("proof_public_key", String, "Optional base64url Ed25519 public key. When present, redeem also returns a proof-bound hooks delegation refresh credential.", func() {
				MinLength(43)
				MaxLength(43)
				Pattern(`^[A-Za-z0-9_-]{43}$`)
			})
			Attribute("delegation_contract_version", String, "Required hooks acting-user contract version when proof_public_key is present.", func() {
				Enum("hooks-acting-user.v1")
			})
			Required("code_challenge", "code_challenge_method")
			security.SessionPayload()
		})

		Result(func() {
			Attribute("code", String, "The opaque one-time code. Hand this to the device agent, which redeems it with its code_verifier.")
			Attribute("expires_in", Int, "Lifetime of the code in seconds.")
			Required("code", "expires_in")
		})

		HTTP(func() {
			POST("/rpc/cliAuth.authorize")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "cliAuthAuthorize")
		Meta("openapi:extension:x-speakeasy-name-override", "authorize")
	})

	Method("redeem", func() {
		Description("Exchange a one-time code plus its PKCE code_verifier for a freshly minted per-user [agent_user] API key, with [hooks] added for proof-bound relay enrollment. No session or API-key auth: proving knowledge of the code_verifier that matches the stored challenge IS the credential. The code is single-use — consumed atomically on lookup — so any missing/expired/already-consumed code or PKCE mismatch returns 401. The raw key is returned exactly once and never again.")

		NoSecurity()

		Payload(func() {
			Attribute("code", String, "The opaque one-time code issued by authorize.")
			Attribute("code_verifier", String, "The PKCE code verifier whose base64url(sha256(...)) equals the stored code_challenge.", func() {
				MinLength(43)
				MaxLength(128)
			})
			Required("code", "code_verifier")
		})

		Result(func() {
			Attribute("access_token", String, "The raw gram_ API key, carrying [agent_user] and, for proof-bound relay enrollment, [hooks]. Returned exactly once.")
			Attribute("user_email", String, "Email of the user the key was minted for.")
			Attribute("project_slug", String, "Slug of the project the key is scoped to.")
			Attribute("organization_id", String, "Organization bound by the authenticated enrollment session. Present for proof-bound hook enrollment.")
			Attribute("delegation_refresh_token", String, "Server-signed refresh credential bound to the enrolled Ed25519 public key. It cannot mint an assertion without proof of the private key.")
			Required("access_token", "user_email", "project_slug")
		})

		HTTP(func() {
			POST("/rpc/cliAuth.redeem")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "cliAuthRedeem")
		Meta("openapi:extension:x-speakeasy-name-override", "redeem")
	})

	Method("delegateHooksActingUser", func() {
		Description("Mint a very short-lived acting-user assertion for one approved live hook invocation. The proof-bound refresh credential and an Ed25519 signature over every invocation binding are both required. Current active organization membership is revalidated before minting.")
		NoSecurity()
		Payload(func() {
			Attribute("refresh_token", String, func() { MaxLength(4096) })
			Attribute("contract_version", String, func() { Enum("hooks-acting-user.v1") })
			Attribute("provider", String, func() { Enum("claude", "codex") })
			Attribute("event", String, func() { Enum("UserPromptSubmit", "PreToolUse") })
			Attribute("session_id", String, func() { MinLength(1); MaxLength(512) })
			Attribute("idempotency_key", String, func() { MinLength(1); MaxLength(512) })
			Attribute("observational", Boolean, "Binds an offline replay or synthetic backfill as observational rather than live governed work.")
			Attribute("signed_at", Int64)
			Attribute("nonce", String, "Cryptographically random, single-use base64url mint nonce and assertion JTI.", func() { MinLength(43); MaxLength(43); Pattern("^[A-Za-z0-9_-]{43}$") })
			Attribute("signature", String, func() { MinLength(86); MaxLength(86); Pattern("^[A-Za-z0-9_-]{86}$") })
			Required("refresh_token", "contract_version", "provider", "event", "session_id", "idempotency_key", "signed_at", "nonce", "signature")
		})
		Result(func() {
			Attribute("assertion", String)
			Attribute("expires_in", Int)
			Required("assertion", "expires_in")
		})
		HTTP(func() {
			POST("/rpc/cliAuth.delegateHooksActingUser")
			Response(StatusOK)
		})
		// Relay-only proof endpoint. Keep it in Goa HTTP/server generation but out
		// of the public dashboard OpenAPI and SDK surface.
		Meta("openapi:generate", "false")
	})
})
