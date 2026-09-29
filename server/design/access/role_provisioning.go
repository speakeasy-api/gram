package access

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var RoleProvisioningSelection = Type("RoleProvisioningSelection", func() {
	Attribute("role_urn", String)
	Attribute("enabled", Boolean)
	Attribute("project_id", String, "Destination override; zero UUID explicitly leaves pending.", func() { Format(FormatUUID) })
	Required("role_urn", "enabled")
})
var RoleProvisioningRoleStatus = Type("RoleProvisioningRoleStatus", func() {
	Attribute("role_urn", String)
	Attribute("name", String)
	Attribute("configured", Boolean, "Whether this role has saved intent, rather than an initial default selection.")
	Attribute("enabled", Boolean)
	Attribute("project_id", String, "Desired destination.", func() { Format(FormatUUID) })
	Attribute("applied_project_id", String, "Current live associated plugin project.", func() { Format(FormatUUID) })
	Attribute("plugin_id", String, func() { Format(FormatUUID) })
	Attribute("origin_audience", String, func() { Enum("assigned", "not_assigned", "not_provisioned") })
	Attribute("publication_status", String, "Existing publication evidence, independent of provisioning. published_before does not assert freshness.", func() { Enum("not_provisioned", "not_connected", "not_published", "published_before") })
	Attribute("pending_reason", String, "Pending reconciliation or admission reason. audience_approval_required requires the existing plugin audience approval workflow.")
	Required("role_urn", "name", "configured", "enabled", "origin_audience", "publication_status")
})
var RoleProvisioningProject = Type("RoleProvisioningProject", func() {
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("name", String)
	Required("id", "name")
})
var RoleProvisioningStatus = Type("RoleProvisioningStatus", func() {
	Attribute("enabled", Boolean)
	Attribute("version", Int64)
	Attribute("project_id", String, func() { Format(FormatUUID) })
	Attribute("roles", ArrayOf(RoleProvisioningRoleStatus))
	Attribute("projects", ArrayOf(RoleProvisioningProject), "Eligible destinations, ordered by live plugin count then oldest project.")
	Required("enabled", "version", "roles", "projects")
})

func roleProvisioningMethods() {
	Method("getRoleProvisioning", func() {
		Description("Read organization-admin role plugin provisioning settings and observed outcomes. Does not provision or publish.")
		Security(security.Session)
		Payload(func() { security.SessionPayload() })
		Result(RoleProvisioningStatus)
		HTTP(func() { GET("/rpc/access.getRoleProvisioning"); security.SessionHeader(); Response(StatusOK) })
		Meta("openapi:operationId", "getRoleProvisioning")
		Meta("openapi:extension:x-speakeasy-name-override", "getRoleProvisioning")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"RoleProvisioning"}`)
	})
	Method("configureRoleProvisioning", func() {
		Description("Save organization-admin role plugin provisioning intent with optimistic versioning. Shared by IdP onboarding and settings. Disabling preserves plugins and audiences. Reconciliation is asynchronous.")
		Security(security.Session)
		Payload(func() {
			security.SessionPayload()
			Attribute("expected_version", Int64, func() { Minimum(0) })
			Attribute("enabled", Boolean)
			Attribute("project_id", String, "Omit to preserve the destination, or choose the best project on first save. Onboarding supplies its preferred project. Zero UUID explicitly leaves pending.", func() { Format(FormatUUID) })
			Attribute("roles", ArrayOf(RoleProvisioningSelection), "Omitted roles preserve saved exclusions; first save selects all live IdP roles.")
			Required("expected_version", "enabled")
		})
		Result(RoleProvisioningStatus)
		HTTP(func() { POST("/rpc/access.configureRoleProvisioning"); security.SessionHeader(); Response(StatusOK) })
		Meta("openapi:operationId", "configureRoleProvisioning")
		Meta("openapi:extension:x-speakeasy-name-override", "configureRoleProvisioning")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"ConfigureRoleProvisioning"}`)
	})
}
