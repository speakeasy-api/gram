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
//     On success it mints a per-user [agent,hooks] API key and returns the
//     raw key exactly once.
var _ = Service("cliAuth", func() {
	Description("Interactive device-agent enrollment via a PKCE one-time-code exchange. authorize (dashboard session) mints a short-lived code bound to a PKCE challenge; redeem (no auth — the code+verifier pair is the credential) exchanges it once for a per-user [agent,hooks] API key.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("authorize", func() {
		Description("Mint a short-lived one-time code bound to a PKCE code_challenge, on behalf of the authenticated dashboard user. Resolves the target project (given slug, else the org's default/first project) and records {user, org, project, scopes:[agent,hooks], challenge} against the code with a ~5 minute TTL. Requires a member-available session (org:read); NOT org-admin.")

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
		Description("Exchange a one-time code plus its PKCE code_verifier for a freshly minted per-user [agent,hooks] API key. No session or API-key auth: proving knowledge of the code_verifier that matches the stored challenge IS the credential. The code is single-use — consumed atomically on lookup — so any missing/expired/already-consumed code or PKCE mismatch returns 401. The raw key is returned exactly once and never again.")

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
			Attribute("access_token", String, "The raw gram_ API key, carrying the [agent,hooks] scopes. Returned exactly once.")
			Attribute("user_email", String, "Email of the user the key was minted for.")
			Attribute("project_slug", String, "Slug of the project the key is scoped to.")
			Required("access_token", "user_email", "project_slug")
		})

		HTTP(func() {
			POST("/rpc/cliAuth.redeem")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "cliAuthRedeem")
		Meta("openapi:extension:x-speakeasy-name-override", "redeem")
	})
})
