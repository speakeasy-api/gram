package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminDefaultAuthPermissionKind = Type("AdminDefaultAuthPermissionKind", func() {
	Required("kind", "pending")

	Attribute("kind", String, "personal_default (everyone's and each agent's use of their own personal connections), shared_client (shared clients for whoever can connect to a server using them) or api_key (legacy API key policies).")
	Attribute("pending", Int64, "Permissions of this kind not yet granted.")
})

var AdminDefaultAuthPermissionStatus = Type("AdminDefaultAuthPermissionStatus", func() {
	Description("Where one organization stands in granting the default auth permissions the mandatory auth client check needs, from a dry run that writes nothing. Only clients already recorded are counted.")
	Required("complete", "api_keys_waiting", "pending_grants", "kinds")

	Attribute("complete", Boolean, "Whether every default permission is granted and the check succeeded.")
	Attribute("api_keys_waiting", Boolean, "Whether API key policies are held back because some of the organization's auth clients are not recorded yet: a key policy is written once, so it waits for them. Run the upstream OAuth migration first.")
	Attribute("pending_grants", Int64, "Default permissions not yet granted.")
	Attribute("kinds", ArrayOf(AdminDefaultAuthPermissionKind), "Pending permissions by kind.")
	Attribute("error", String, "Why the check failed, when it did.")
})

var AdminDefaultAuthPermissionRun = Type("AdminDefaultAuthPermissionRun", func() {
	Required("grants_written", "status")

	Attribute("grants_written", Int64, "Default permissions granted.")
	Attribute("status", AdminDefaultAuthPermissionStatus, "The organization's status after the run.")
})

func defaultAuthPermissionMethods() {
	Method("getDefaultAuthPermissionStatus", func() {
		Description("Reports which default auth permissions one organization is missing, from a dry run that writes nothing.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "The organization ID.")
		})

		Result(AdminDefaultAuthPermissionStatus)

		HTTP(func() {
			GET("/admin/defaultAuthPermissions.status")
			Param("organization_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetDefaultAuthPermissionStatus")
		Meta("openapi:extension:x-speakeasy-name-override", "getDefaultAuthPermissionStatus")
	})

	Method("runDefaultAuthPermissions", func() {
		Description("Grants one organization the default auth permissions it is missing: everyone's and each agent's use of their own personal connections, shared clients for whoever can connect to a server using them, and policies for legacy API keys. It only adds grants; rerunning restores any default since removed.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "The organization ID.")

			Meta("openapi:typename", "RunDefaultAuthPermissionsRequestBody")
		})

		Result(AdminDefaultAuthPermissionRun)

		HTTP(func() {
			POST("/admin/defaultAuthPermissions.run")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminRunDefaultAuthPermissions")
		Meta("openapi:extension:x-speakeasy-name-override", "runDefaultAuthPermissions")
	})
}
