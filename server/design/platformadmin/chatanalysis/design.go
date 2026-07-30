// Package chatanalysis declares the adminChatAnalysis Goa service: the
// platform-admin (Speakeasy-only) surface for switching an organization's chat
// session analysis judges on and off. Settings rows live in
// chat_analysis_settings, one per (organization, judge); today the only
// registered judge is work_units.
package chatanalysis

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var Settings = Type("ChatAnalysisSettings", func() {
	Description("Per-organization switches and budgets for chat session analysis judges.")
	Required("organization_id", "work_units_enabled", "work_units_daily_cap", "is_default")
	Attribute("organization_id", String, "Organization these settings apply to.")
	Attribute("work_units_enabled", Boolean, "Whether work-units chat analysis is enabled.")
	Attribute("work_units_daily_cap", Int, "Maximum work-units evaluations reserved across the organization each UTC day. 0 disables scoring as surely as the switch.", func() { Minimum(0); Maximum(10000) })
	Attribute("is_default", Boolean, "Whether these values are platform defaults rather than stored organization settings.")
})

var _ = Service("adminChatAnalysis", func() {
	Description("Platform-admin management of an organization's chat session analysis settings. Speakeasy-staff only; every method requires the platform-admin flag.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("getSettings", func() {
		Description("Get the active organization's chat analysis settings. Requires platform admin.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(Settings)

		HTTP(func() {
			GET("/rpc/adminChatAnalysis.getSettings")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getChatAnalysisSettings")
		Meta("openapi:extension:x-speakeasy-name-override", "getSettings")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ChatAnalysisSettings"}`)
	})

	// Named upsertWorkUnitsSettings rather than upsertSettings so the request
	// body schema never collides with skillEfficacy.upsertSettings' — Goa names
	// bodies after the method, and a collision silently renames the other
	// service's SDK types.
	Method("upsertWorkUnitsSettings", func() {
		Description("Create or replace the active organization's chat analysis settings. Requires platform admin.")

		Payload(func() {
			security.SessionPayload()
			Attribute("work_units_enabled", Boolean, "Whether work-units chat analysis is enabled.")
			Attribute("work_units_daily_cap", Int, "Maximum work-units evaluations reserved across the organization each UTC day. 0 disables scoring as surely as the switch.", func() { Minimum(0); Maximum(10000) })
			Required("work_units_enabled", "work_units_daily_cap")
		})

		Result(Settings)

		HTTP(func() {
			POST("/rpc/adminChatAnalysis.upsertWorkUnitsSettings")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertChatAnalysisSettings")
		Meta("openapi:extension:x-speakeasy-name-override", "upsertWorkUnitsSettings")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertChatAnalysisSettings"}`)
	})

	Method("triggerAnalysis", func() {
		Description("Wake the chat analysis coordinator for every project in the active organization, instead of waiting for the periodic sweep. Requires platform admin.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(func() {
			Description("How far the trigger reached.")
			Required("projects_signaled")
			Attribute("projects_signaled", Int, "Number of projects whose analysis coordinator was woken.")
		})

		HTTP(func() {
			POST("/rpc/adminChatAnalysis.triggerAnalysis")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "triggerChatAnalysis")
		Meta("openapi:extension:x-speakeasy-name-override", "triggerAnalysis")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "TriggerChatAnalysis"}`)
	})
})
