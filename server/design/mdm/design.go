package mdm

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("mdm", func() {
	Description("MDM configuration management for Claude Code deployments.")
	shared.DeclareErrorResponses()

	Method("getInstallScript", func() {
		Description("Returns the shell script used to apply Gram settings to Claude Code. " +
			"Host this endpoint URL in your MDM policy — script updates automatically without touching Jamf/MDM.")
		NoSecurity()

		Result(Bytes)

		HTTP(func() {
			GET("/rpc/mdm.getInstallScript")
			Response(StatusOK, func() {
				ContentType("text/plain")
			})
		})

		Meta("openapi:operationId", "getInstallScript")
		Meta("openapi:extension:x-speakeasy-name-override", "getInstallScript")
	})

	Method("patchClaudeSettings", func() {
		Description("Accepts the current ~/.claude/settings.json as the request body and returns a patched " +
			"version with Gram observability configuration injected. All existing user settings are preserved. " +
			"Called by the MDM install script — requires a Hooks-scoped API key.")

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
