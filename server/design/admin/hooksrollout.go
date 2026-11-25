package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

// maxHooksRolloutVersion bounds a pin in the design. The handler also rejects
// a pin above the hooks generator version the server publishes, which the
// design cannot express.
const maxHooksRolloutVersion = 100000

var AdminHooksRolloutSource = Type("AdminHooksRolloutSource", String, func() {
	Description("What decides an organization's hooks version: canary organizations always get the current version, organization and default name the pin that applies, stage means a staged rollout has cleared the organization, and legacy_flag means no pin applies yet and the hooks-rollout PostHog flag decides.")
	Enum("canary", "organization", "stage", "default", "legacy_flag")
})

var AdminHooksRolloutPin = Type("AdminHooksRolloutPin", func() {
	Description("A hooks version rollout pin: the highest hooks generator version its scope is cleared to receive.")
	Required("version", "set_by", "set_at")

	Attribute("version", Int, "Highest hooks generator version the pin clears.")
	Attribute("set_by", String, "Email of the staff operator who set the pin.")
	Attribute("set_at", String, "When the pin was set.", func() { Format(FormatDateTime) })
})

var AdminHooksRolloutOverride = Type("AdminHooksRolloutOverride", func() {
	Description("An organization pinned to its own hooks version instead of the default pin.")
	Required("organization_id", "organization_name", "organization_slug", "pin")

	Attribute("organization_id", String)
	Attribute("organization_name", String)
	Attribute("organization_slug", String)
	Attribute("pin", AdminHooksRolloutPin)
})

var AdminHooksRolloutChange = Type("AdminHooksRolloutChange", func() {
	Description("One change to a hooks rollout pin.")
	Required("set_by", "set_at")

	Attribute("organization_id", String, "Organization whose override changed. Absent for a change to the default pin.")
	Attribute("organization_slug", String, "Slug of organization_id. Absent for a change to the default pin.")
	Attribute("version", Int, "Version the change set. Absent when the change cleared an organization override.")
	Attribute("set_by", String, "Email of the staff operator who made the change.")
	Attribute("set_at", String, "When the change was made.", func() { Format(FormatDateTime) })
})

var AdminHooksRolloutStage = Type("AdminHooksRolloutStage", func() {
	Description("The latest staged hooks rollout. A running rollout clears a growing share of the organizations that follow the default pin, moving to the next share each day, and moves the default pin when it reaches 100 percent.")
	Required("revision", "version", "percent", "state", "set_by", "set_at")

	Attribute("revision", Int64, "Increases with every change to the rollout. Send it back with a change so a rollout that moved on in the meantime is refused.")
	Attribute("version", Int, "Hooks generator version the rollout clears.")
	Attribute("percent", Int, "Share of organizations on the default pin that the rollout clears, beyond canary organizations.")
	Attribute("state", String, "One of running, paused, completed or cancelled. Running and paused rollouts clear organizations; completed and cancelled ones do not.")
	Attribute("reason", String, "Why the rollout was paused or cancelled.")
	Attribute("set_by", String, "Email of the staff operator who made the last change, or hooks-rollout-schedule when the schedule advanced it.")
	Attribute("set_at", String, "When the last change was made.", func() { Format(FormatDateTime) })
	Attribute("next_advance_at", String, "When a running rollout moves to its next share. Absent unless running.", func() { Format(FormatDateTime) })
})

var AdminHooksRollout = Type("AdminHooksRollout", func() {
	Description("Platform-wide hooks version rollout state.")
	Required("current_version", "canary_organization_slugs", "overrides", "recent_changes")

	Attribute("current_version", Int, "The hooks generator version this server build publishes to an organization once its pin reaches it.")
	Attribute("default_pin", AdminHooksRolloutPin, "The pin for every organization without an override. Absent until one is set; until then those organizations follow the legacy hooks-rollout PostHog flag.")
	Attribute("stage", AdminHooksRolloutStage, "The latest staged rollout. Absent when none was ever started.")
	Attribute("canary_organization_slugs", ArrayOf(String), "Organizations that always receive current_version and ignore pins.")
	Attribute("overrides", ArrayOf(AdminHooksRolloutOverride), "Organizations with an override, by slug.")
	Attribute("recent_changes", ArrayOf(AdminHooksRolloutChange), "The most recent pin changes, newest first.")
})

var AdminOrganizationHooksRollout = Type("AdminOrganizationHooksRollout", func() {
	Description("Hooks version rollout state for one organization.")
	Required("organization_id", "current_version", "source")

	Attribute("organization_id", String)
	Attribute("current_version", Int, "The hooks generator version this server build publishes to an organization once its pin reaches it.")
	Attribute("override", AdminHooksRolloutPin, "The organization's own pin. Absent when it follows the default pin.")
	Attribute("default_pin", AdminHooksRolloutPin, "The platform-wide default pin. Absent until one is set.")
	Attribute("source", AdminHooksRolloutSource)
	Attribute("effective_version", Int, "Version of the pin that applies. Absent for canary and legacy_flag.")
	Attribute("eligible", Boolean, "Whether the organization's next publish moves its hooks plugin to current_version. Absent for legacy_flag, which the admin server cannot read.")
})

var AdminHooksReleaseRepoVersion = Type("AdminHooksReleaseRepoVersion", func() {
	Description("Marketplace repositories whose last published hooks plugin carries one hooks generator version.")
	Required("version", "repos")

	Attribute("version", String, "The hooks generator version last published, or empty for repositories that have never published the hooks plugin.")
	Attribute("repos", Int64, "Repositories on this version.")
})

var AdminHooksReleaseInstallVersion = Type("AdminHooksReleaseInstallVersion", func() {
	Description("Hooks installs reporting one binary version.")
	Required("version", "status", "installs")

	Attribute("version", String, "The hooks binary version, or empty for installs that sent no version.")
	Attribute("status", String, "How the version compares to the release Speakeasy ships.", func() {
		Enum("current", "behind", "unsupported", "unknown")
	})
	Attribute("installs", Int64, "Installs whose latest version is this one.")
})

var AdminHooksReleaseFleet = Type("AdminHooksReleaseFleet", func() {
	Description("Hooks installs across every organization, by binary version.")
	Required("window_days", "installs", "installs_on_current", "versions")

	Attribute("window_days", Int, "Installs count when they sent a hook in this many full UTC days.")
	Attribute("installs", Int64, "Installs seen in the window.")
	Attribute("installs_on_current", Int64, "Installs reporting exactly the binary version this build ships, on any endpoint.")
	Attribute("versions", ArrayOf(AdminHooksReleaseInstallVersion), "Installs per binary version and status, newest first. A version can appear once per status, for example current and unsupported when some installs still send to a legacy endpoint.")
})

var AdminHooksReleaseProgress = Type("AdminHooksReleaseProgress", func() {
	Description("How far the current hooks release has reached: organizations cleared by the rollout pins, marketplace repositories republished with it, and installs running it.")
	Required("current_version", "organizations", "organizations_cleared", "organizations_held", "organizations_on_legacy_flag", "repos", "repos_on_current", "repo_versions")

	Attribute("current_version", Int, "The hooks generator version this server build publishes.")
	Attribute("current_binary_version", String, "The hooks binary version this server build ships. Absent when unknown.")
	Attribute("organizations", Int64, "Organizations with at least one marketplace repository.")
	Attribute("organizations_cleared", Int64, "Of those, organizations whose next publish moves to current_version: canaries and organizations pinned at or above it.")
	Attribute("organizations_held", Int64, "Organizations pinned below current_version.")
	Attribute("organizations_on_legacy_flag", Int64, "Organizations with no pin, which follow the legacy hooks-rollout PostHog flag the admin server cannot read.")
	Attribute("repos", Int64, "Marketplace repositories.")
	Attribute("repos_on_current", Int64, "Repositories whose last published hooks plugin carries current_version.")
	Attribute("repo_versions", ArrayOf(AdminHooksReleaseRepoVersion), "Repositories per published hooks generator version, newest first.")
	Attribute("fleet", AdminHooksReleaseFleet, "Installs running the release. Absent when the hooks fleet is unavailable.")
})

// MCP parity: get_hooks_rollout, get_hooks_release_progress and
// get_organization_hooks_rollout expose the reads to Staff Admin MCP,
// including the staged rollout. The writes are deliberately dashboard-only:
// moving the default pin or a staged rollout changes the hooks plugin of many
// customer organizations at once, which does not fit the Admin MCP proposal
// flow's single-organization targeting, and a release step this consequential
// stays a human action.
func hooksRolloutMethods() {
	Method("getHooksRollout", func() {
		Description("Returns the platform-wide hooks version rollout state: the version this build publishes, the default pin, canary organizations, organization overrides and recent changes.")
		Payload(func() {
			security.AdminAuthPayload()
		})
		Result(AdminHooksRollout)
		HTTP(func() {
			GET("/admin/hooksRollout.get")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminGetHooksRollout")
	})

	Method("getHooksReleaseProgress", func() {
		Description("Returns how far the current hooks release has reached: organizations the rollout pins clear for it, marketplace repositories republished with it, and installs reporting its binary version.")
		Payload(func() {
			security.AdminAuthPayload()
		})
		Result(AdminHooksReleaseProgress)
		HTTP(func() {
			GET("/admin/hooksRollout.progress")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminGetHooksReleaseProgress")
	})

	Method("setHooksRolloutDefault", func() {
		Description("Moves the default hooks rollout pin for every organization without an override. Customer organizations receive a hooks version on the next rollout sweep once the pin reaches it. Lowering the pin holds back later versions; it never downgrades a published hooks plugin.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "SetHooksRolloutDefaultRequestBody")
			Required("version")
			Attribute("version", Int, "Highest hooks generator version to clear. Must not exceed the version this build publishes.", func() {
				Minimum(1)
				Maximum(maxHooksRolloutVersion)
			})
		})
		Result(AdminHooksRollout)
		HTTP(func() {
			POST("/admin/hooksRollout.setDefault")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminSetHooksRolloutDefault")
	})

	Method("getOrganizationHooksRollout", func() {
		Description("Returns the hooks version rollout state for one organization.")
		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")
			Attribute("organization_id", String)
		})
		Result(AdminOrganizationHooksRollout)
		HTTP(func() {
			GET("/admin/organization.hooksRollout")
			Param("organization_id")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminGetOrganizationHooksRollout")
	})

	Method("setOrganizationHooksRollout", func() {
		Description("Pins one organization to its own hooks version, regardless of the default pin.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "SetOrganizationHooksRolloutRequestBody")
			Required("organization_id", "version")
			Attribute("organization_id", String)
			Attribute("version", Int, "Highest hooks generator version to clear for the organization. Must not exceed the version this build publishes.", func() {
				Minimum(1)
				Maximum(maxHooksRolloutVersion)
			})
		})
		Result(AdminOrganizationHooksRollout)
		HTTP(func() {
			POST("/admin/organization.setHooksRollout")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminSetOrganizationHooksRollout")
	})

	Method("clearOrganizationHooksRollout", func() {
		Description("Removes an organization's hooks version override so it follows the default pin again.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "ClearOrganizationHooksRolloutRequestBody")
			Required("organization_id")
			Attribute("organization_id", String)
		})
		Result(AdminOrganizationHooksRollout)
		HTTP(func() {
			POST("/admin/organization.clearHooksRollout")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminClearOrganizationHooksRollout")
	})
	Method("startHooksRolloutStage", func() {
		Description("Starts a staged rollout of a hooks version newer than the default pin. It clears canary organizations first, then 25 percent of organizations on the default pin a day later, then moves the default pin a day after that. Replaces an active rollout of another version.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "StartHooksRolloutStageRequestBody")
			Required("revision", "version")
			Attribute("revision", Int64, "The stage revision the operator was shown, or 0 when no rollout was ever staged. A different current revision returns 409 Conflict.", func() {
				Minimum(0)
			})
			Attribute("version", Int, "Hooks generator version to roll out. Must be above the default pin and not exceed the version this build publishes.", func() {
				Minimum(1)
				Maximum(maxHooksRolloutVersion)
			})
		})
		Result(AdminHooksRollout)
		HTTP(func() {
			POST("/admin/hooksRollout.startStage")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminStartHooksRolloutStage")
	})

	Method("pauseHooksRolloutStage", func() {
		Description("Holds a running staged rollout at its current share. Organizations it already cleared keep the new version.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "PauseHooksRolloutStageRequestBody")
			Required("revision", "reason")
			Attribute("revision", Int64, "The stage revision the operator was shown, or 0 when no rollout was ever staged. A different current revision returns 409 Conflict.", func() {
				Minimum(0)
			})
			Attribute("reason", String, "Why, recorded with the change.", func() {
				MinLength(1)
				MaxLength(500)
			})
		})
		Result(AdminHooksRollout)
		HTTP(func() {
			POST("/admin/hooksRollout.pauseStage")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminPauseHooksRolloutStage")
	})

	Method("resumeHooksRolloutStage", func() {
		Description("Restarts a paused staged rollout. The day at the current share starts over.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "ResumeHooksRolloutStageRequestBody")
			Required("revision")
			Attribute("revision", Int64, "The stage revision the operator was shown, or 0 when no rollout was ever staged. A different current revision returns 409 Conflict.", func() {
				Minimum(0)
			})
		})
		Result(AdminHooksRollout)
		HTTP(func() {
			POST("/admin/hooksRollout.resumeStage")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminResumeHooksRolloutStage")
	})

	Method("cancelHooksRolloutStage", func() {
		Description("Ends a staged rollout. Organizations it cleared go back to the default pin; hooks plugins already published are not downgraded.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "CancelHooksRolloutStageRequestBody")
			Required("revision", "reason")
			Attribute("revision", Int64, "The stage revision the operator was shown, or 0 when no rollout was ever staged. A different current revision returns 409 Conflict.", func() {
				Minimum(0)
			})
			Attribute("reason", String, "Why, recorded with the change.", func() {
				MinLength(1)
				MaxLength(500)
			})
		})
		Result(AdminHooksRollout)
		HTTP(func() {
			POST("/admin/hooksRollout.cancelStage")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminCancelHooksRolloutStage")
	})
}
