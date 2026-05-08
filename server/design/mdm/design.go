package mdm

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("mdm", func() {
	Description("MDM configuration management for Claude Code deployments.")
	shared.DeclareErrorResponses()

	Method("generateDeployScript", func() {
		Description("Generates a ready-to-use MDM deploy script with an embedded Hooks-scoped API key. " +
			"Download this script once and upload it to your MDM platform (Jamf, Kandji, Mosyle, etc.). " +
			"The embedded key is automatically provisioned with Hooks scope. Requires org admin access.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
		})

		Result(Bytes)

		HTTP(func() {
			POST("/rpc/mdm.generateDeployScript")
			security.SessionHeader()
			Response(StatusOK, func() {
				ContentType("text/plain")
			})
		})

		Meta("openapi:operationId", "generateDeployScript")
		Meta("openapi:extension:x-speakeasy-name-override", "generateDeployScript")
	})

	Method("getApplyScript", func() {
		Description("Returns the per-user apply script. The deploy script fetches and runs this on each " +
			"login — logic updates automatically without touching your MDM policy.")
		NoSecurity()

		Result(Bytes)

		HTTP(func() {
			GET("/rpc/mdm.getApplyScript")
			Response(StatusOK, func() {
				ContentType("text/plain")
			})
		})

		Meta("openapi:operationId", "getApplyScript")
		Meta("openapi:extension:x-speakeasy-name-override", "getApplyScript")
	})

	Method("patchClaudeSettings", func() {
		Description("Accepts the current ~/.claude/settings.json as the request body and returns a patched " +
			"version with Gram observability configuration injected. All existing user settings are preserved. " +
			"Called by the apply script — requires a Hooks-scoped API key.")

		Security(security.ByKey, func() {
			Scope("hooks")
		})

		Payload(func() {
			security.ByKeyPayload()
		})

		HTTP(func() {
			POST("/rpc/mdm.patchClaudeSettings")
			security.ByKeyHeader()
			SkipRequestBodyEncodeDecode()
			SkipResponseBodyEncodeDecode()
			Response(StatusOK, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "patchClaudeSettings")
		Meta("openapi:extension:x-speakeasy-name-override", "patchClaudeSettings")
	})
})
