package toolsets

import (
	"github.com/speakeasy-api/gram/design/security"
	"github.com/speakeasy-api/gram/design/shared"
	. "goa.design/goa/v3/dsl"
)

// CustomDomain represents a custom domain associated with a project.
var CustomDomain = Type("CustomDomain", func() {
	Attribute("id", String, "The ID of the custom domain")
	Attribute("organization_id", String, "The ID of the organization this domain belongs to")
	Attribute("domain", String, "The custom domain name")
	Attribute("verified", Boolean, "Whether the domain is verified")
	Attribute("activated", Boolean, "Whether the domain is activated in ingress")
	Attribute("created_at", String, func() {
		Description("When the custom domain was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the custom domain was last updated.")
		Format(FormatDateTime)
	})
	Attribute("is_updating", Boolean, "The custom domain is actively being registered")

	Required("id", "organization_id", "domain", "verified", "activated", "created_at", "updated_at", "is_updating")
})

var _ = Service("domains", func() {
	Description("Manage custom domains for gram.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("getDomain", func() {
		Description("Get the custom domain for a project")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(CustomDomain)

		HTTP(func() {
			GET("/rpc/domain.get")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getDomain")
		Meta("openapi:extension:x-speakeasy-name-override", "getDomain")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "getDomain"}`)
	})

	Method("createDomain", func() {
		Description("Create a custom domain for a organization")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("domain", String, "The custom domain")
			Required("domain")
		})

		HTTP(func() {
			POST("/rpc/domain.register")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "registerDomain")
		Meta("openapi:extension:x-speakeasy-name-override", "registerDomain")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "registerDomain"}`)
	})

	Method("deleteDomain", func() {
		Description("Delete a custom domain")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/domain.delete")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteDomain")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteDomain")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "deleteDomain"}`)
	})
})
