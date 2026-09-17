package identityproviderconnections

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var ChecklistItem = Type("IdentityProviderConnectionChecklistItem", func() {
	Description("One step the organization administrator completes in the identity provider's console. Ordered; keys are stable across responses.")
	Required("key", "title", "description")
	Attribute("key", String, "Stable step identifier.")
	Attribute("title", String, "Short step title.")
	Attribute("description", String, "What to do in the console, including any value copied from this connection.")
})

var ActiveKey = Type("IdentityProviderConnectionActiveKey", func() {
	Description("The published signing key the identity provider currently trusts. Omitted after revocation.")
	Required("id", "kid", "activated_at")
	Attribute("id", String, "JSON Web Key row ID.", func() {
		Format(FormatUUID)
	})
	Attribute("kid", String, "Key ID as published in the JWKS document.")
	Attribute("activated_at", String, "ISO 8601 timestamp when the key became the signer.", func() {
		Format(FormatDateTime)
	})
})

var ApplicationsSync = Type("IdentityProviderConnectionApplicationsSync", func() {
	Description("Cadence and watermark of the scheduled applications snapshot.")
	Required("interval_seconds")
	Attribute("interval_seconds", Int, "How often the snapshot is reconciled, in seconds.")
	Attribute("synced_at", String, "ISO 8601 timestamp when the last completed run started. Omitted until the first run.", func() {
		Format(FormatDateTime)
	})
	Attribute("requested_at", String, "ISO 8601 timestamp of the last syncApplications call; a request newer than synced_at runs on the next coordinator pass.", func() {
		Format(FormatDateTime)
	})
})

var ReconcileRun = Type("IdentityProviderConnectionReconcileRun", func() {
	Description("One applications snapshot run and the changes it applied.")
	Required("id", "status", "started_at", "applications_seen", "applications_added", "applications_removed", "assignments_added", "assignments_removed", "skipped_app_ids", "truncated")
	Attribute("id", String, "Run ID.", func() {
		Format(FormatUUID)
	})
	Attribute("status", String, "running, succeeded, or failed.", func() {
		Enum("running", "succeeded", "failed")
	})
	Attribute("started_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("finished_at", String, "Omitted while running.", func() {
		Format(FormatDateTime)
	})
	Attribute("applications_seen", Int, "Applications in the snapshot after skipping Okta-internal ones.")
	Attribute("applications_added", Int)
	Attribute("applications_removed", Int)
	Attribute("assignments_added", Int)
	Attribute("assignments_removed", Int)
	Attribute("skipped_app_ids", ArrayOf(String), "Okta-internal application ids left out of the snapshot.")
	Attribute("truncated", Boolean, "Whether a listing hit the page or application cap; nothing missing from a truncated listing is removed.")
	Attribute("error", String, "Typed reason when the run failed. rate_limited and okta_unreachable are retried before being recorded; superseded means a newer run applied first; discarded means the connection stopped being verified during the run; interrupted means the worker died.", func() {
		Enum("rate_limited", "credential_rejected", "okta_unreachable", "client_unavailable", "superseded", "discarded", "interrupted")
	})
})

var Application = Type("IdentityProviderConnectionApplication", func() {
	Description("One Okta application in the snapshot, keyed by its Okta id.")
	Required("okta_app_id", "label", "name", "sign_on_mode", "status", "features", "user_assignments", "group_assignments", "first_seen_at", "last_seen_at")
	Attribute("okta_app_id", String, "Okta application id.")
	Attribute("label", String, "Admin-facing label. Admin-editable; never a key.")
	Attribute("name", String, "Okta application template name.")
	Attribute("sign_on_mode", String)
	Attribute("status", String, "ACTIVE or INACTIVE.")
	Attribute("features", ArrayOf(String))
	Attribute("user_assignments", Int, "Live direct and group-derived user assignments.")
	Attribute("group_assignments", Int, "Live group assignments.")
	Attribute("first_seen_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("last_seen_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("removed_at", String, "Set when the application disappeared from a run; only returned with include_removed.", func() {
		Format(FormatDateTime)
	})
})

var ListApplicationsResult = Type("ListIdentityProviderConnectionApplicationsResult", func() {
	Required("applications", "sync")
	Attribute("applications", ArrayOf(Application), "Applications ordered by label, live rows first. Capped at 2000 rows, the same cap a run applies.")
	Attribute("sync", ApplicationsSync)
	Attribute("last_run", ReconcileRun, "Omitted before the first run.")
})

var Connection = Type("OktaIdentityProviderConnection", func() {
	Description("An organization's Okta connection: the service application Gram authenticates to the Okta Management API with, its verification state, and the console checklist. Never carries key material or tokens.")
	Required("id", "organization_id", "provider", "status", "org_url", "issuer_url", "listing_mode", "jwks_url", "client_id_submitted", "dpop_required", "required_scopes", "granted_scopes", "missing_scopes", "verification_reasons", "checklist", "applications_sync", "created_at", "updated_at")
	Attribute("id", String, "Connection ID.", func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "Organization the connection belongs to.")
	Attribute("provider", String, "Identity provider; always okta.", func() {
		Enum("okta")
	})
	Attribute("status", String, "Connection state. pending until the client ID is submitted and verified; verified when every required scope is granted over DPoP; degraded when verification found gaps (see verification_reasons); revoked once the credential was withdrawn.", func() {
		Enum("pending", "verified", "degraded", "revoked")
	})
	Attribute("org_url", String, "Okta org URL, for example https://example.okta.com.")
	Attribute("issuer_url", String, "Discovered authorization server issuer. Equal to org_url by construction.")
	Attribute("listing_mode", String, "Which checklist template applies: custom_app when the admin creates the API Services app by hand, oin when the Speakeasy OIN listing is added from the catalog.", func() {
		Enum("custom_app", "oin")
	})
	Attribute("jwks_url", String, "Public JWKS URL the Okta app is configured to trust for private_key_jwt.")
	Attribute("client_id", String, "Okta application client ID. Omitted until submitted.")
	Attribute("client_id_submitted", Boolean, "Whether the real Okta client ID has replaced the provisioning placeholder.")
	Attribute("dpop_required", Boolean, "Whether Okta issued a DPoP-bound token at the last verification.")
	Attribute("required_scopes", ArrayOf(String), "Okta API scopes the integration needs.")
	Attribute("granted_scopes", ArrayOf(String), "Scopes Okta granted at the last verification.")
	Attribute("missing_scopes", ArrayOf(String), "Required scopes Okta did not grant at the last verification.")
	Attribute("verification_reasons", ArrayOf(String), "Typed reasons recorded by the last verification; empty when verified or not yet verified. missing_role is reserved for a later release.", func() {
		Elem(func() {
			Enum("missing_scope", "missing_role", "dpop_not_bound", "key_not_fetched", "read_failed:okta.apps.read", "read_failed:okta.users.read", "read_failed:okta.groups.read")
		})
	})
	Attribute("last_verified_at", String, "ISO 8601 timestamp of the last verification that found every required scope granted. Omitted until then.", func() {
		Format(FormatDateTime)
	})
	Attribute("last_error", String, "Why the last verification did not complete: Okta rejected the credential, or could not be reached. Omitted when it completed.", func() {
		Enum("credential_rejected", "okta_unreachable")
	})
	Attribute("agent_id", String, "Admin-entered Okta AI agent ID. Display only.")
	Attribute("agent_app_id", String, "Admin-entered Okta application ID the AI agent is bound to. Display only.")
	Attribute("active_key", ActiveKey)
	Attribute("checklist", ArrayOf(ChecklistItem), "Console steps for the connection's listing mode, in order.")
	Attribute("applications_sync", ApplicationsSync)
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
})

var GetResult = Type("GetIdentityProviderConnectionResult", func() {
	Description("The organization's Okta connection, or nothing when none is connected.")
	Attribute("connection", Connection, "Omitted when the organization has no live connection.")
})

var _ = Service("identityProviderConnections", func() {
	Description("Manage the organization's Okta connection: provision the private_key_jwt credential, submit the Okta client ID, verify granted scopes, and revoke.")

	shared.DeclareErrorResponses()
	Error(string(oops.CodeUnavailable), func() {
		Description(oops.CodeUnavailable.UserMessage())
		Fault()
	})
	HTTP(func() {
		shared.DeclareHTTPErrorResponses()
		Response(string(oops.CodeUnavailable), StatusServiceUnavailable, func() {
			ContentType("application/json")
		})
	})

	Method("create", func() {
		Description("Create the organization's Okta connection. Discovers the org's authorization server, provisions a signing key and JWKS URL, and returns the console checklist. Requires org:admin and the okta-connections rollout. One live connection per organization; creation is rate limited.")
		Error(string(oops.CodeFailedPrecondition), func() { Description(oops.CodeFailedPrecondition.UserMessage()) })
		Error(string(oops.CodeRateLimitExceeded), func() { Description(oops.CodeRateLimitExceeded.UserMessage()) })

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "CreateIdentityProviderConnectionRequestBody")
			Attribute("org_url", String, "Okta org URL, for example https://example.okta.com. Must be https with no path; the host must be an Okta-owned domain.")
			Attribute("listing_mode", String, "Checklist template. Defaults to custom_app.", func() {
				Enum("custom_app", "oin")
			})
			Required("org_url")
		})

		Result(Connection)

		HTTP(func() {
			POST("/rpc/identityProviderConnections.create")
			security.SessionHeader()
			Response(StatusOK)
			Response(string(oops.CodeFailedPrecondition), StatusPreconditionFailed, func() {
				ContentType("application/json")
			})
			Response(string(oops.CodeRateLimitExceeded), StatusTooManyRequests, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "createIdentityProviderConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateIdentityProviderConnection"}`)
	})

	Method("submitClientId", func() {
		Description("Record the client ID of the Okta API Services application and verify it. Allowed once, while the connection is pending; revoke and recreate to change it. Requires org:admin.")
		Error(string(oops.CodeFailedPrecondition), func() { Description(oops.CodeFailedPrecondition.UserMessage()) })
		Error(string(oops.CodeRateLimitExceeded), func() { Description(oops.CodeRateLimitExceeded.UserMessage()) })

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "SubmitIdentityProviderConnectionClientIdRequestBody")
			Attribute("id", String, "Connection ID.", func() {
				Format(FormatUUID)
			})
			Attribute("client_id", String, "Okta application client ID (0oa...).")
			Required("id", "client_id")
		})

		Result(Connection)

		HTTP(func() {
			POST("/rpc/identityProviderConnections.submitClientId")
			security.SessionHeader()
			Response(StatusOK)
			Response(string(oops.CodeFailedPrecondition), StatusPreconditionFailed, func() {
				ContentType("application/json")
			})
			Response(string(oops.CodeRateLimitExceeded), StatusTooManyRequests, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "submitIdentityProviderConnectionClientId")
		Meta("openapi:extension:x-speakeasy-name-override", "submitClientId")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SubmitIdentityProviderConnectionClientId"}`)
	})

	Method("verify", func() {
		Description("Re-verify the connection against Okta: mint a token, confirm each required scope with a read, and record the outcome. Rate limited per organization. Requires org:admin.")
		Error(string(oops.CodeFailedPrecondition), func() { Description(oops.CodeFailedPrecondition.UserMessage()) })
		Error(string(oops.CodeRateLimitExceeded), func() { Description(oops.CodeRateLimitExceeded.UserMessage()) })

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "VerifyIdentityProviderConnectionRequestBody")
			Attribute("id", String, "Connection ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(Connection)

		HTTP(func() {
			POST("/rpc/identityProviderConnections.verify")
			security.SessionHeader()
			Response(StatusOK)
			Response(string(oops.CodeFailedPrecondition), StatusPreconditionFailed, func() {
				ContentType("application/json")
			})
			Response(string(oops.CodeRateLimitExceeded), StatusTooManyRequests, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "verifyIdentityProviderConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "verify")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "VerifyIdentityProviderConnection"}`)
	})

	Method("get", func() {
		Description("Get a connection by ID, or the organization's live Okta connection when no ID is given. Session only; requires org:read.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, "Connection ID. Omit to fetch the organization's live connection.", func() {
				Format(FormatUUID)
			})
		})

		Result(GetResult)

		HTTP(func() {
			GET("/rpc/identityProviderConnections.get")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getIdentityProviderConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "IdentityProviderConnection"}`)
	})

	Method("recordAgent", func() {
		Description("Record the Okta AI agent ID and the application it is bound to, for display. Okta does not expose these through its API. Requires org:admin.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "RecordIdentityProviderConnectionAgentRequestBody")
			Attribute("id", String, "Connection ID.", func() {
				Format(FormatUUID)
			})
			Attribute("agent_id", String, "Okta AI agent ID. Empty clears it.")
			Attribute("agent_app_id", String, "Okta application ID the agent is bound to. Empty clears it.")
			Required("id")
		})

		Result(Connection)

		HTTP(func() {
			POST("/rpc/identityProviderConnections.recordAgent")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "recordIdentityProviderConnectionAgent")
		Meta("openapi:extension:x-speakeasy-name-override", "recordAgent")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RecordIdentityProviderConnectionAgent"}`)
	})

	Method("revoke", func() {
		Description("Revoke the connection: withdraw every signing key from the JWKS, disable the key material, and tombstone the connection so a new one can be created. Idempotent. Requires org:admin.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "RevokeIdentityProviderConnectionRequestBody")
			Attribute("id", String, "Connection ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(Connection)

		HTTP(func() {
			POST("/rpc/identityProviderConnections.revoke")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeIdentityProviderConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "revoke")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeIdentityProviderConnection"}`)
	})

	Method("syncApplications", func() {
		Description("Run the applications snapshot on the next coordinator pass, within minutes, instead of at the next scheduled interval. The connection must be verified. Rate limited per organization. Requires org:admin.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "SyncIdentityProviderConnectionApplicationsRequestBody")
			Attribute("id", String, "Connection ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(Connection)

		HTTP(func() {
			POST("/rpc/identityProviderConnections.syncApplications")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "syncIdentityProviderConnectionApplications")
		Meta("openapi:extension:x-speakeasy-name-override", "syncApplications")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SyncIdentityProviderConnectionApplications"}`)
	})

	Method("listApplications", func() {
		Description("List the applications snapshot for the connection with live assignment counts and the last reconcile run. The connection must be verified. Requires org:admin.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, "Connection ID.", func() {
				Format(FormatUUID)
			})
			Attribute("include_removed", Boolean, "Include applications that disappeared from a run.", func() {
				Default(false)
			})
			Required("id")
		})

		Result(ListApplicationsResult)

		HTTP(func() {
			GET("/rpc/identityProviderConnections.listApplications")
			security.SessionHeader()
			Param("id")
			Param("include_removed")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listIdentityProviderConnectionApplications")
		Meta("openapi:extension:x-speakeasy-name-override", "listApplications")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "IdentityProviderConnectionApplications"}`)
	})
})
