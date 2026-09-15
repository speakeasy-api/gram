package design

import (
	. "goa.design/goa/v3/dsl"
)

var ListEmaAppsResult = Type("ListEmaAppsResult", func() {
	Attribute("items", ArrayOf(EmaApp), "Apps on this page.")
	PaginationResult()
	Required("items")
})

var ListEmaResourcesResult = Type("ListEmaResourcesResult", func() {
	Attribute("items", ArrayOf(EmaResource), "Resources on this page.")
	PaginationResult()
	Required("items")
})

var ListEmaAppAssignmentsResult = Type("ListEmaAppAssignmentsResult", func() {
	Attribute("items", ArrayOf(EmaAppAssignment), "Assignments on this page.")
	PaginationResult()
	Required("items")
})

var ListEmaTrustRulesResult = Type("ListEmaTrustRulesResult", func() {
	Attribute("items", ArrayOf(EmaTrustRule), "Trust rules on this page.")
	PaginationResult()
	Required("items")
})

var ListEmaIssuedJagsResult = Type("ListEmaIssuedJagsResult", func() {
	Attribute("items", ArrayOf(EmaIssuedJag), "Grants, newest first.")
	Required("items")
})

var _ = Service("emaApps", func() {
	Description("Enterprise-managed authorization requesting apps: the clients allowed to ask the IdP for an ID-JAG. Permanently unauthenticated.")

	Method("create", func() {
		Description("Register an app.")

		Payload(func() {
			Attribute("client_id", String, "Client id used at the token endpoint. Must be unique.")
			Attribute("client_secret", String, "Omit or leave empty to register a public client.")
			Attribute("jwks", String, "JWKS document holding the app's public key. Set this to require private_key_jwt.")
			Attribute("name", String, "Display name; defaults to the client id.")
			Attribute("enabled", Boolean, "Defaults to true.")
			Required("client_id")
		})

		Result(EmaApp)

		HTTP(func() {
			POST("/rpc/emaApps.create")
			Response(StatusOK)
		})
	})

	Method("update", func() {
		Description("Patch an app. Omitted string fields are left unchanged.")

		Payload(func() {
			Attribute("id", String, "App UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("client_id", String, "Client id.")
			Attribute("client_secret", String, "Client secret.")
			Attribute("jwks", String, "JWKS document holding the app's public key.")
			Attribute("name", String, "Display name.")
			Attribute("enabled", Boolean, "Enabled flag; always rewritten when supplied.")
			Required("id")
		})

		Result(EmaApp)

		HTTP(func() {
			POST("/rpc/emaApps.update")
			Response(StatusOK)
		})
	})

	Method("list", func() {
		Description("List apps.")

		Payload(func() {
			PaginationPayload()
		})

		Result(ListEmaAppsResult)

		HTTP(func() {
			POST("/rpc/emaApps.list")
			Response(StatusOK)
		})
	})

	Method("delete", func() {
		Description("Hard-delete an app. Cascades to its assignments and issued grants.")

		Payload(func() {
			Attribute("id", String, "App UUID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			POST("/rpc/emaApps.delete")
			Response(StatusNoContent)
		})
	})
})

var _ = Service("emaResources", func() {
	Description("Enterprise-managed authorization resource apps. Each is one resource authorization server mounted at /resource-as/<slug>. Permanently unauthenticated.")

	Method("create", func() {
		Description("Register a resource. Its authorization server becomes reachable immediately.")

		Payload(func() {
			Attribute("slug", String, "Path segment for the authorization server. Must be unique.")
			Attribute("name", String, "Display name; defaults to the slug.")
			Attribute("resource_identifier", String, "The MCP server URL this resource guards.")
			Required("slug", "resource_identifier")
		})

		Result(EmaResource)

		HTTP(func() {
			POST("/rpc/emaResources.create")
			Response(StatusOK)
		})
	})

	Method("update", func() {
		Description("Patch a resource. Changing the slug moves its authorization server, and invalidates any ID-JAG already minted for the old issuer.")

		Payload(func() {
			Attribute("id", String, "Resource UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "Path segment.")
			Attribute("name", String, "Display name.")
			Attribute("resource_identifier", String, "The MCP server URL this resource guards.")
			Required("id")
		})

		Result(EmaResource)

		HTTP(func() {
			POST("/rpc/emaResources.update")
			Response(StatusOK)
		})
	})

	Method("list", func() {
		Description("List resources.")

		Payload(func() {
			PaginationPayload()
		})

		Result(ListEmaResourcesResult)

		HTTP(func() {
			POST("/rpc/emaResources.list")
			Response(StatusOK)
		})
	})

	Method("delete", func() {
		Description("Hard-delete a resource. Cascades to its trust rules, assignments, and issued tokens.")

		Payload(func() {
			Attribute("id", String, "Resource UUID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			POST("/rpc/emaResources.delete")
			Response(StatusNoContent)
		})
	})
})

var _ = Service("emaAppAssignments", func() {
	Description("Assigns apps to users for a resource. The absence of an assignment is what denies a mint. Permanently unauthenticated.")

	Method("create", func() {
		Description("Assign an app to a user for a resource. Idempotent on the triple; re-assigning overwrites the scopes.")

		Payload(func() {
			Attribute("app_id", String, "App UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("user_id", String, "User UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("resource_id", String, "Resource UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("granted_scopes", String, "Space-delimited scopes. Empty grants none.")
			Required("app_id", "user_id", "resource_id")
		})

		Result(EmaAppAssignment)

		HTTP(func() {
			POST("/rpc/emaAppAssignments.create")
			Response(StatusOK)
		})
	})

	Method("update", func() {
		Description("Change an assignment's granted scopes.")

		Payload(func() {
			Attribute("id", String, "Assignment UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("granted_scopes", String, "Space-delimited scopes.")
			Required("id", "granted_scopes")
		})

		Result(EmaAppAssignment)

		HTTP(func() {
			POST("/rpc/emaAppAssignments.update")
			Response(StatusOK)
		})
	})

	Method("list", func() {
		Description("List assignments, optionally filtered by app, user, or resource.")

		Payload(func() {
			Attribute("app_id", String, "Filter by app UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("user_id", String, "Filter by user UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("resource_id", String, "Filter by resource UUID.", func() {
				Format(FormatUUID)
			})
			PaginationPayload()
		})

		Result(ListEmaAppAssignmentsResult)

		HTTP(func() {
			POST("/rpc/emaAppAssignments.list")
			Response(StatusOK)
		})
	})

	Method("delete", func() {
		Description("Revoke an assignment. The next mint for that app, user, and resource is denied.")

		Payload(func() {
			Attribute("id", String, "Assignment UUID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			POST("/rpc/emaAppAssignments.delete")
			Response(StatusNoContent)
		})
	})
})

var _ = Service("emaTrustRules", func() {
	Description("Trust domain rules: which issuer a resource authorization server accepts ID-JAGs from, and the ceiling it applies. Permanently unauthenticated.")

	Method("create", func() {
		Description("Trust an issuer for a resource. Idempotent on (resource, issuer).")

		Payload(func() {
			Attribute("resource_id", String, "Resource UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("trusted_issuer", String, "An ID-JAG `iss` value to accept. Pass a foreign issuer to model a second trust domain.")
			Attribute("allowed_client_ids", String, "JSON array of client ids; defaults to '[]', meaning any.")
			Attribute("allowed_scopes", String, "Space-delimited ceiling; empty means no ceiling.")
			Attribute("enabled", Boolean, "Defaults to true.")
			Required("resource_id", "trusted_issuer")
		})

		Result(EmaTrustRule)

		HTTP(func() {
			POST("/rpc/emaTrustRules.create")
			Response(StatusOK)
		})
	})

	Method("update", func() {
		Description("Patch a trust rule. Omitted string fields are left unchanged.")

		Payload(func() {
			Attribute("id", String, "Trust rule UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("trusted_issuer", String, "Issuer identifier.")
			Attribute("allowed_client_ids", String, "JSON array of client ids.")
			Attribute("allowed_scopes", String, "Space-delimited ceiling.")
			Attribute("enabled", Boolean, "Enabled flag; always rewritten when supplied.")
			Required("id")
		})

		Result(EmaTrustRule)

		HTTP(func() {
			POST("/rpc/emaTrustRules.update")
			Response(StatusOK)
		})
	})

	Method("list", func() {
		Description("List trust rules, optionally filtered by resource.")

		Payload(func() {
			Attribute("resource_id", String, "Filter by resource UUID.", func() {
				Format(FormatUUID)
			})
			PaginationPayload()
		})

		Result(ListEmaTrustRulesResult)

		HTTP(func() {
			POST("/rpc/emaTrustRules.list")
			Response(StatusOK)
		})
	})

	Method("delete", func() {
		Description("Remove a trust rule. The resource then refuses ID-JAGs from that issuer.")

		Payload(func() {
			Attribute("id", String, "Trust rule UUID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			POST("/rpc/emaTrustRules.delete")
			Response(StatusNoContent)
		})
	})

	Method("listIssuedGrants", func() {
		Description("Read the ledger of ID-JAGs this IdP minted, newest first. Inspection only.")

		Payload(func() {
			Attribute("user_id", String, "Filter by user UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("resource_id", String, "Filter by resource UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("limit", Int, "Maximum grants to return.", func() {
				Default(50)
				Minimum(1)
				Maximum(100)
			})
		})

		Result(ListEmaIssuedJagsResult)

		HTTP(func() {
			POST("/rpc/emaTrustRules.listIssuedGrants")
			Response(StatusOK)
		})
	})
})
