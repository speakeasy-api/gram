package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

func dnsRoutingMethodEnum() {
	Enum("per_domain_cname", "shared_cname", "a_record", "unknown")
}

var AdminCustomDomain = Type("AdminCustomDomain", func() {
	Description("A live custom domain with its designated CNAME target and the DNS routing method health checks last observed.")
	Required("id", "organization_id", "organization_name", "organization_slug", "domain", "verified", "activated", "created_at")

	Attribute("id", String, "The custom domain ID.", func() { Format(FormatUUID) })
	Attribute("organization_id", String, "The organization that owns the domain.")
	Attribute("organization_name", String, "The organization name.")
	Attribute("organization_slug", String, "The organization slug.")
	Attribute("domain", String, "The custom domain name.")
	Attribute("verified", Boolean, "Whether the domain passed DNS verification.")
	Attribute("activated", Boolean, "Whether the domain is provisioned in ingress.")
	Attribute("dns_target_token", String, "The random DNS label of the domain's designated CNAME target. Absent until assigned.")
	Attribute("dns_target", String, "The domain's designated CNAME target (<dns_target_token>.<suffix>). Absent when the admin server has no target suffix configured or the token is not yet assigned.")
	Attribute("dns_routing_method", String, "How the latest health check observed the domain's DNS routing traffic. One of: per_domain_cname, shared_cname, a_record (also covers flattened CNAME, ALIAS and ANAME records, which resolve to addresses), unknown. Absent until the first health check.")
	Attribute("dns_configuration_status", String, "Advisory public DNS observation: matched, wrong_target, unconfirmed, not_found, check_failed, or not_configured. Does not establish endpoint availability.")
	Attribute("dns_checked_at", String, "When DNS configuration was observed. Absent until a DNS observation has run.", func() { Format(FormatDateTime) })
	Attribute("health_status", String, "Infrastructure and certificate status: unknown, healthy, or unhealthy. Unknown until a complete observation has run, or when an older worker has overwritten the health result.")
	Attribute("health_issue", String, "Why the domain was last observed as unhealthy.")
	Attribute("health_checked_at", String, "When the domain health was last checked.", func() { Format(FormatDateTime) })
	Attribute("created_at", String, "When the domain was registered.", func() { Format(FormatDateTime) })
})

var AdminListCustomDomainsResult = Type("AdminListCustomDomainsResult", func() {
	Required("custom_domains")

	Attribute("custom_domains", ArrayOf(AdminCustomDomain), "Every matching live custom domain, ordered by domain name. Organizations have at most one live custom domain, so the list is not paginated.")
})

// MCP parity: exposed through Staff Admin MCP as list_custom_domains. Not a
// Platform MCP tool: it spans every organization.
func customDomainMethods() {
	Method("listCustomDomains", func() {
		Description("Lists live custom domains across organizations with their designated CNAME targets and observed DNS routing methods.")

		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String, "Only the domain owned by this organization.")
			Attribute("dns_routing_method", String, "Only domains last observed using this routing method. unknown also matches domains not yet checked.", dnsRoutingMethodEnum)
		})

		Result(AdminListCustomDomainsResult)

		HTTP(func() {
			GET("/admin/customDomains.list")
			Param("organization_id")
			Param("dns_routing_method")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminListCustomDomains")
		Meta("openapi:extension:x-speakeasy-name-override", "listCustomDomains")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminListCustomDomains"}`)
	})
}
