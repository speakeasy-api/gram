package skillefficacy

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var Settings = Type("SkillEfficacySettings", func() {
	Description("Per-organization sampling settings for skill efficacy scoring.")
	Required("organization_id", "enabled", "per_skill_daily_cap", "org_daily_cap", "new_version_burst", "is_default")
	Attribute("organization_id", String, "Organization these settings apply to.")
	Attribute("enabled", Boolean, "Whether skill efficacy scoring is enabled.")
	Attribute("per_skill_daily_cap", Int, "Maximum evaluations reserved per skill each UTC day.", func() { Minimum(0); Maximum(10000) })
	Attribute("org_daily_cap", Int, "Maximum evaluations reserved across the organization each UTC day.", func() { Minimum(0); Maximum(10000) })
	Attribute("new_version_burst", Int, "Lifetime evaluations a new skill version may reserve before the per-skill daily cap applies.", func() { Minimum(0); Maximum(10000) })
	Attribute("is_default", Boolean, "Whether these values are platform defaults rather than stored organization settings.")
})

var _ = Service("skillEfficacy", func() {
	Description("Manage organization-wide skill efficacy sampling settings.")

	shared.DeclareErrorResponses()

	Method("getSettings", func() {
		Description("Get effective organization-wide skill efficacy sampling settings.")

		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(Settings)

		HTTP(func() {
			GET("/rpc/skillEfficacy.getSettings")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getSkillEfficacySettings")
		Meta("openapi:extension:x-speakeasy-name-override", "getSettings")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SkillEfficacySettings"}`)
	})

	Method("upsertSettings", func() {
		Description("Create or replace organization-wide skill efficacy sampling settings.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("enabled", Boolean, "Whether skill efficacy scoring is enabled.")
			Attribute("per_skill_daily_cap", Int, "Maximum evaluations reserved per skill each UTC day.", func() { Minimum(0); Maximum(10000) })
			Attribute("org_daily_cap", Int, "Maximum evaluations reserved across the organization each UTC day.", func() { Minimum(0); Maximum(10000) })
			Attribute("new_version_burst", Int, "Lifetime evaluations a new skill version may reserve before the per-skill daily cap applies.", func() { Minimum(0); Maximum(10000) })
			Required("enabled", "per_skill_daily_cap", "org_daily_cap", "new_version_burst")
		})

		Result(Settings)

		HTTP(func() {
			POST("/rpc/skillEfficacy.upsertSettings")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertSkillEfficacySettings")
		Meta("openapi:extension:x-speakeasy-name-override", "upsertSettings")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertSkillEfficacySettings"}`)
	})
})
