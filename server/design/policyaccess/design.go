package policyaccess

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("policyaccess", func() {
	Description("Risk policy access request workflow.")

	Security(security.ByKey, func() {
		Scope("consumer")
	})
	Security(security.Session)

	shared.DeclareErrorResponses()

	Method("listRequests", func() {
		Description("List risk-policy access requests produced when a policy blocks a caller.")
		Payload(func() {
			Attribute("status", String, func() {
				Description("Filter by request status. Omit for all.")
				Enum("requested", "approved", "denied")
			})
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListPolicyAccessRequestsResult)

		HTTP(func() {
			GET("/rpc/policyaccess.listRequests")
			Param("status")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listPolicyAccessRequests")
		Meta("openapi:extension:x-speakeasy-name-override", "listRequests")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PolicyAccessRequests"}`)
	})

	Method("decideRequest", func() {
		Description("Approve or deny a risk-policy access request. Approving grants risk_policy:bypass to selected principals.")
		Payload(func() {
			Attribute("id", String, "ID of the request to decide.", func() {
				Format(FormatUUID)
			})
			Attribute("status", String, func() {
				Description("Decision: approved or denied.")
				Enum("approved", "denied")
			})
			Attribute("grant_type", String, func() {
				Description("Who receives the bypass when approving.")
				Enum("requester", "requester_roles", "roles")
			})
			Attribute("role_slugs", ArrayOf(String), "Roles to grant when grant_type=roles.")
			Required("id", "status")
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(PolicyAccessRequestModel)

		HTTP(func() {
			POST("/rpc/policyaccess.decideRequest")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "decidePolicyAccessRequest")
		Meta("openapi:extension:x-speakeasy-name-override", "decideRequest")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DecidePolicyAccessRequest"}`)
	})

	Method("listBypasses", func() {
		Description("List active risk-policy bypass grants.")
		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListPolicyBypassesResult)

		HTTP(func() {
			GET("/rpc/policyaccess.listBypasses")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listPolicyBypasses")
		Meta("openapi:extension:x-speakeasy-name-override", "listBypasses")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PolicyBypasses"}`)
	})

	Method("revokeBypass", func() {
		Description("Revoke one active risk-policy bypass grant.")
		Payload(func() {
			Attribute("grant_id", String, "ID of the principal grant to revoke.", func() {
				Format(FormatUUID)
			})
			Required("grant_id")
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(Empty)

		HTTP(func() {
			POST("/rpc/policyaccess.revokeBypass")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokePolicyBypass")
		Meta("openapi:extension:x-speakeasy-name-override", "revokeBypass")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokePolicyBypass"}`)
	})
})

var PolicyAccessRequestModel = Type("PolicyAccessRequest", func() {
	Description("A request for access through a risk policy that blocked a caller.")
	Required("id", "organization_id", "project_id", "policy_id", "target", "status", "created_at", "updated_at")

	Attribute("id", String, "Request ID.")
	Attribute("organization_id", String, "Organization the request belongs to.")
	Attribute("project_id", String, "Project the policy belongs to.")
	Attribute("policy_id", String, "The risk policy that blocked the caller.")
	Attribute("policy_name", String, "Policy display name.")
	Attribute("target", PolicyAccessTargetModel, "Optional target the bypass is narrowed to.")
	Attribute("requester_user_id", String, "Gram user that was blocked.")
	Attribute("requester_email", String, "Email of the blocked user.")
	Attribute("note", String, "Optional requester note.")
	Attribute("status", String, func() {
		Description("Request lifecycle status.")
		Enum("requested", "approved", "denied")
	})
	Attribute("decided_by", String, "URN of the admin who decided.")
	Attribute("granted_principal_urns", ArrayOf(String), "Principals the bypass was granted to when approved.")
	Attribute("decided_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
})

var ListPolicyAccessRequestsResult = Type("ListPolicyAccessRequestsResult", func() {
	Required("requests")
	Attribute("requests", ArrayOf(PolicyAccessRequestModel), "The policy access requests.")
})

var PolicyAccessTargetModel = Type("PolicyAccessTarget", func() {
	Description("A generic target narrowing for a policy bypass. Empty fields mean the whole policy.")
	Required("kind", "label", "key", "dimensions")

	Attribute("kind", String, "Target kind, such as shadow_mcp_server. Empty means whole policy.")
	Attribute("label", String, "Human-readable target label.")
	Attribute("key", String, "Stable canonical target key.")
	Attribute("dimensions", MapOf(String, String), "Selector dimensions used for the bypass grant.")
})

var PolicyBypassGrantModel = Type("PolicyBypassGrant", func() {
	Description("An active risk-policy bypass grant.")
	Required("id", "policy_id", "principal_urn", "principal_type", "target", "created_at", "updated_at")

	Attribute("id", String, "Principal grant ID.")
	Attribute("policy_id", String, "Risk policy ID.")
	Attribute("policy_name", String, "Policy display name.")
	Attribute("principal_urn", String, "Principal holding the bypass.")
	Attribute("principal_type", String, "Principal type, e.g. user or role.")
	Attribute("target", PolicyAccessTargetModel, "Optional target this bypass is narrowed to.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
})

var ListPolicyBypassesResult = Type("ListPolicyBypassesResult", func() {
	Required("bypasses")
	Attribute("bypasses", ArrayOf(PolicyBypassGrantModel), "Active policy bypass grants.")
})
