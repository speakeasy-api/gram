package authz

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("authz", func() {
	Description("Query and resolve authorization challenge events.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("listChallenges", func() {
		Description("List authz challenge events from ClickHouse, enriched with resolution state from PostgreSQL.")
		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			Attribute("outcome", String, func() {
				Description("Filter by outcome.")
				Enum("allow", "deny")
			})
			Attribute("principal_urn", String, "Filter by principal URN.")
			Attribute("scope", String, "Filter by scope.")
			Attribute("project_id", String, "Filter to a specific project.")
			Attribute("resolved", Boolean, "Filter by resolution state. True = only resolved, false = only unresolved.")
			Attribute("limit", Int, func() {
				Description("Maximum number of results to return.")
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("offset", Int, func() {
				Description("Number of results to skip.")
				Default(0)
				Minimum(0)
			})
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListChallengesResult)

		HTTP(func() {
			GET("/rpc/authz.listChallenges")
			Param("outcome")
			Param("principal_urn")
			Param("scope")
			Param("project_id")
			Param("resolved")
			Param("limit")
			Param("offset")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listChallenges")
		Meta("openapi:extension:x-speakeasy-name-override", "listChallenges")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Challenges"}`)
	})

	Method("resolveChallenge", func() {
		Description("Record a resolution for a denied authz challenge. The caller is responsible for assigning the role first.")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Extend(ResolveChallengeForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ChallengeResolutionModel)

		HTTP(func() {
			POST("/rpc/authz.resolveChallenge")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "resolveChallenge")
		Meta("openapi:extension:x-speakeasy-name-override", "resolveChallenge")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ResolveChallenge"}`)
	})
})

var AuthzChallengeModel = Type("AuthzChallenge", func() {
	Required("id", "timestamp", "organization_id", "principal_urn", "principal_type",
		"operation", "outcome", "reason", "scope", "role_slugs",
		"evaluated_grant_count", "matched_grant_count")

	Attribute("id", String, "Unique challenge identifier.")
	Attribute("timestamp", String, func() {
		Description("When the authz decision was made.")
		Format(FormatDateTime)
	})
	Attribute("organization_id", String, "Organization the principal was acting in.")
	Attribute("project_id", String, "Project scope (empty for org-level checks).")
	Attribute("principal_urn", String, "Principal URN e.g. user:<uuid> or api_key:<id>.")
	Attribute("principal_type", String, func() {
		Description("Kind of principal.")
		Enum("user", "api_key", "assistant")
	})
	Attribute("user_email", String, "Email when available.")
	Attribute("photo_url", String, "User avatar URL when available.")
	Attribute("operation", String, func() {
		Enum("require", "require_any", "filter")
	})
	Attribute("outcome", String, func() {
		Enum("allow", "deny", "error")
	})
	Attribute("reason", String, func() {
		Enum("grant_matched", "no_grants", "scope_unsatisfied", "invalid_check", "rbac_skipped_apikey", "dev_override")
	})
	Attribute("scope", String, "Scope that was checked.")
	Attribute("resource_kind", String, "Resource kind of the check.")
	Attribute("resource_id", String, "Resource ID of the check.")
	Attribute("role_slugs", ArrayOf(String), "Roles the principal had loaded.")
	Attribute("evaluated_grant_count", Int, "Total grants evaluated.")
	Attribute("matched_grant_count", Int, "Number of grants that matched.")

	// Resolution fields — null when unresolved.
	Attribute("resolved_at", String, func() {
		Description("When the challenge was resolved by an admin.")
		Format(FormatDateTime)
	})
	Attribute("resolution_type", String, func() {
		Description("How the challenge was resolved.")
		Enum("role_assigned", "dismissed")
	})
	Attribute("resolved_by", String, "URN of the admin who resolved.")
	Attribute("resolution_role_slug", String, "Role slug assigned (when resolution_type=role_assigned).")
})

var ListChallengesResult = Type("ListChallengesResult", func() {
	Required("challenges", "total")
	Attribute("challenges", ArrayOf(AuthzChallengeModel), "The challenge events.")
	Attribute("total", Int, "Total number of matching challenges for pagination.")
})

var ResolveChallengeForm = Type("ResolveChallengeForm", func() {
	Required("challenge_id", "principal_urn", "scope", "resolution_type")

	Attribute("challenge_id", String, "ID of the challenge in ClickHouse.")
	Attribute("principal_urn", String, "Principal that was denied.")
	Attribute("scope", String, "Scope that was denied.")
	Attribute("resource_kind", String, "Resource kind from the challenge.")
	Attribute("resource_id", String, "Resource ID from the challenge.")
	Attribute("resolution_type", String, func() {
		Description("How the challenge is being resolved.")
		Enum("role_assigned", "dismissed")
	})
	Attribute("role_slug", String, "Role slug to assign (required when resolution_type=role_assigned).")
})

var ChallengeResolutionModel = Type("ChallengeResolution", func() {
	Required("id", "organization_id", "challenge_id", "principal_urn", "scope",
		"resolution_type", "resolved_by", "created_at")

	Attribute("id", String, "Resolution record ID.")
	Attribute("organization_id", String, "Organization ID.")
	Attribute("challenge_id", String, "ClickHouse challenge ID.")
	Attribute("principal_urn", String, "Denied principal.")
	Attribute("scope", String, "Denied scope.")
	Attribute("resource_kind", String, "Resource kind.")
	Attribute("resource_id", String, "Resource ID.")
	Attribute("resolution_type", String, func() {
		Enum("role_assigned", "dismissed")
	})
	Attribute("role_slug", String, "Assigned role slug.")
	Attribute("resolved_by", String, "Admin who resolved.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
})
