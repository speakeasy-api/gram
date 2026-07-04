package spendrules

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

// SpendRuleWindowKindEnum applies the allowed-values constraint to a
// window_kind attribute. Windows are UTC calendar-aligned: daily resets at
// midnight UTC, weekly on Monday 00:00 UTC, monthly on the 1st 00:00 UTC.
func SpendRuleWindowKindEnum() {
	Enum("daily", "weekly", "monthly")
}

// SpendRuleActionEnum applies the allowed-values constraint to a spend rule
// action attribute. flag records warning/breach events only; block
// additionally opens a circuit that denies the actor's agent traffic until
// the window resets.
func SpendRuleActionEnum() {
	Enum("flag", "block")
}

var _ = Service("spendRules", func() {
	Description("Manage spend control rules, view budget events, and preview actor targeting.")
	Meta("openapi:extension:x-speakeasy-group", "spendRules")

	Security(security.ByKey, security.ProjectSlug, func() { Scope("producer") })
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("createSpendRule", func() {
		Description("Create a new spend control rule for the current organization.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("name", String, "The rule name.")
			Attribute("description", String, "Optional description of what the rule covers.", func() {
				Default("")
			})
			Attribute("target_expr", String, "CEL boolean expression over actor directory attributes selecting who the rule applies to.")
			Attribute("limit_usd", Float64, "Per-person budget in USD for one window.", func() {
				Minimum(0)
			})
			Attribute("window_kind", String, "UTC calendar window the budget covers.", func() {
				SpendRuleWindowKindEnum()
			})
			Attribute("warn_at_pct", Int, "Percentage of the limit at which a warning event is emitted.", func() {
				Minimum(1)
				Maximum(100)
				Default(80)
			})
			Attribute("action", String, "Rule action: flag or block.", func() {
				SpendRuleActionEnum()
				Default("flag")
			})
			Attribute("enabled", Boolean, "Whether the rule is active.", func() {
				Default(true)
			})
			Required("name", "target_expr", "limit_usd", "window_kind")
		})

		Result(SpendRule)

		HTTP(func() {
			POST("/rpc/spendrules.createRule")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createSpendRule")
		Meta("openapi:extension:x-speakeasy-group", "spendRules.rules")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SpendRulesCreateRule", "type": "mutation"}`)
	})

	Method("listSpendRules", func() {
		Description("List all spend control rules for the current organization.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListSpendRulesResult)

		HTTP(func() {
			GET("/rpc/spendrules.listRules")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listSpendRules")
		Meta("openapi:extension:x-speakeasy-group", "spendRules.rules")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SpendRulesListRules"}`)
	})

	Method("getSpendRule", func() {
		Description("Get a spend control rule by ID.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The rule ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(SpendRule)

		HTTP(func() {
			GET("/rpc/spendrules.getRule")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getSpendRule")
		Meta("openapi:extension:x-speakeasy-group", "spendRules.rules")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SpendRulesGetRule"}`)
	})

	Method("updateSpendRule", func() {
		Description("Update a spend control rule. Material changes (target_expr, limit_usd, window_kind, warn_at_pct, action) bump the rule version and reset its evaluation state.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The rule ID.", func() {
				Format(FormatUUID)
			})
			Attribute("name", String, "The rule name. Omit to preserve the current name.")
			Attribute("description", String, "Description of what the rule covers. Omit to preserve the current description.")
			Attribute("target_expr", String, "CEL boolean expression over actor directory attributes. Omit to preserve the current expression.")
			Attribute("limit_usd", Float64, "Per-person budget in USD for one window. Omit to preserve the current limit.", func() {
				Minimum(0)
			})
			Attribute("window_kind", String, "UTC calendar window the budget covers. Omit to preserve the current window.", func() {
				SpendRuleWindowKindEnum()
			})
			Attribute("warn_at_pct", Int, "Percentage of the limit at which a warning event is emitted. Omit to preserve the current threshold.", func() {
				Minimum(1)
				Maximum(100)
			})
			Attribute("action", String, "Rule action: flag or block. Omit to preserve the current action.", func() {
				SpendRuleActionEnum()
			})
			Attribute("enabled", Boolean, "Whether the rule is active. Omit to preserve the current state.")
			Required("id")
		})

		Result(SpendRule)

		HTTP(func() {
			PUT("/rpc/spendrules.updateRule")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateSpendRule")
		Meta("openapi:extension:x-speakeasy-group", "spendRules.rules")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SpendRulesUpdateRule", "type": "mutation"}`)
	})

	Method("deleteSpendRule", func() {
		Description("Delete a spend control rule. Any open circuits for the rule close on the next evaluation cycle.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The rule ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			DELETE("/rpc/spendrules.deleteRule")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteSpendRule")
		Meta("openapi:extension:x-speakeasy-group", "spendRules.rules")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SpendRulesDeleteRule", "type": "mutation"}`)
	})

	Method("previewSpendRule", func() {
		Description("Preview which actors a target expression matches and their current spend against a proposed budget. Powers the live preview in the rule editor and the per-actor breakdown in the rule detail view.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("target_expr", String, "CEL boolean expression over actor directory attributes to preview.")
			Attribute("limit_usd", Float64, "Per-person budget in USD used to compute usage percentages.", func() {
				Minimum(0)
			})
			Attribute("window_kind", String, "UTC calendar window to compute spend over.", func() {
				SpendRuleWindowKindEnum()
			})
			Attribute("evaluated_from", String, "Ignore spend accrued before this instant. Pass an existing rule's evaluated_from to mirror the evaluator; omit for new rules.", func() {
				Format(FormatDateTime)
			})
			Required("target_expr", "limit_usd", "window_kind")
		})

		Result(PreviewSpendRuleResult)

		HTTP(func() {
			POST("/rpc/spendrules.previewRule")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "previewSpendRule")
		Meta("openapi:extension:x-speakeasy-group", "spendRules.rules")
		Meta("openapi:extension:x-speakeasy-name-override", "preview")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SpendRulesPreviewRule", "type": "mutation"}`)
	})

	Method("listSpendRuleEvents", func() {
		Description("List warning and breach events emitted by spend rule evaluation, most recent first.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("rule_id", String, "Optional rule ID to filter by.", func() {
				Format(FormatUUID)
			})
			Attribute("event_type", String, "Optional event type to filter by.", func() {
				Enum("warning", "breach")
			})
			Attribute("cursor", String, "Cursor to fetch the next page of events.")
			Attribute("limit", Int, "Maximum number of events to return per page.", func() {
				Minimum(1)
				Maximum(200)
			})
		})

		Result(ListSpendRuleEventsResult)

		HTTP(func() {
			GET("/rpc/spendrules.listEvents")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("rule_id")
			Param("event_type")
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listSpendRuleEvents")
		Meta("openapi:extension:x-speakeasy-group", "spendRules.events")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SpendRulesListEvents"}`)
	})

	Method("getSpendRulesOverview", func() {
		Description("Get spend control overview metrics: aggregate card numbers plus current-window usage per rule.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(SpendRulesOverviewResult)

		HTTP(func() {
			GET("/rpc/spendrules.getOverview")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getSpendRulesOverview")
		Meta("openapi:extension:x-speakeasy-group", "spendRules.overview")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SpendRulesOverview"}`)
	})
})

var SpendRule = Type("SpendRule", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The spend rule ID.", func() {
		Format(FormatUUID)
	})
	Attribute("urn", String, "Versioned rule URN, e.g. spend_rule:<uuid>:v3. Pins the exact rule configuration that produced an event.")
	Attribute("organization_id", String, "The organization ID.")
	Attribute("name", String, "The rule name.")
	Attribute("description", String, "Description of what the rule covers. Empty when unset.")
	Attribute("target_expr", String, "CEL boolean expression over actor directory attributes selecting who the rule applies to.")
	Attribute("limit_usd", Float64, "Per-person budget in USD for one window.")
	Attribute("window_kind", String, "UTC calendar window the budget covers.", func() {
		SpendRuleWindowKindEnum()
	})
	Attribute("warn_at_pct", Int, "Percentage of the limit at which a warning event is emitted.")
	Attribute("action", String, "Rule action: flag (record events only) or block (deny agent traffic on breach).", func() {
		SpendRuleActionEnum()
	})
	Attribute("enabled", Boolean, "Whether the rule is active.")
	Attribute("version", Int64, "Rule version, incremented on material config changes.")
	Attribute("evaluated_from", String, "Spend accrued before this instant is ignored by the evaluator.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, "When the rule was created.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "When the rule was last updated.", func() {
		Format(FormatDateTime)
	})

	Required("id", "urn", "organization_id", "name", "description", "target_expr", "limit_usd", "window_kind", "warn_at_pct", "action", "enabled", "version", "evaluated_from", "created_at", "updated_at")
})

var SpendRuleEvent = Type("SpendRuleEvent", func() {
	Attribute("id", String, "The event ID.", func() {
		Format(FormatUUID)
	})
	Attribute("rule_id", String, "The spend rule ID that produced the event.", func() {
		Format(FormatUUID)
	})
	Attribute("rule_urn", String, "Versioned rule URN pinning the config that produced the event.")
	Attribute("rule_name", String, "Current name of the rule, for display.")
	Attribute("event_type", String, "Event type.", func() {
		Enum("warning", "breach")
	})
	Attribute("user_id", String, "Gram user ID of the actor, when linked.")
	Attribute("email", String, "Actor email.")
	Attribute("display_name", String, "Actor display name, when known.")
	Attribute("spend_usd", Float64, "Actor spend in USD at evaluation time.")
	Attribute("limit_usd", Float64, "Per-person budget in USD the rule granted at the pinned version.")
	Attribute("window_start", String, "Inclusive start of the budget window.", func() {
		Format(FormatDateTime)
	})
	Attribute("window_end", String, "Exclusive end of the budget window; the budget resets at this instant.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, "When the event was recorded.", func() {
		Format(FormatDateTime)
	})

	Required("id", "rule_id", "rule_urn", "rule_name", "event_type", "email", "spend_usd", "limit_usd", "window_start", "window_end", "created_at")
})

var SpendRuleActorUsage = Type("SpendRuleActorUsage", func() {
	Attribute("email", String, "Actor email.")
	Attribute("display_name", String, "Actor display name, when known.")
	Attribute("user_id", String, "Gram user ID of the actor, when linked.")
	Attribute("spend_usd", Float64, "Actor spend in USD within the current window.")
	Attribute("limit_usd", Float64, "Per-person budget in USD.")
	Attribute("used_pct", Float64, "Spend as a percentage of the limit (may exceed 100).")

	Required("email", "spend_usd", "limit_usd", "used_pct")
})

var PreviewSpendRuleResult = Type("PreviewSpendRuleResult", func() {
	Attribute("matched_count", Int, "Total number of directory users the target expression matches.")
	Attribute("window_start", String, "Inclusive start of the current window used for spend.", func() {
		Format(FormatDateTime)
	})
	Attribute("window_end", String, "Exclusive end of the current window used for spend.", func() {
		Format(FormatDateTime)
	})
	Attribute("actors", ArrayOf(SpendRuleActorUsage), "Matched actors with their current-window spend, ordered by spend descending. Capped at 50 entries.")

	Required("matched_count", "window_start", "window_end", "actors")
})

var SpendRuleUsage = Type("SpendRuleUsage", func() {
	Attribute("rule_id", String, "The spend rule ID.", func() {
		Format(FormatUUID)
	})
	Attribute("matched_users", Int, "Number of directory users the rule currently matches.")
	Attribute("users_warned", Int, "Matched users at or past the warning threshold but under the limit.")
	Attribute("users_breached", Int, "Matched users at or past the limit.")
	Attribute("spend_usd", Float64, "Total spend in USD across matched users within the current window.")
	Attribute("budget_usd", Float64, "Total budgeted spend in USD (per-person limit times matched users).")
	Attribute("worst_used_pct", Float64, "Highest per-actor usage percentage across matched users (may exceed 100).")
	Attribute("status", String, "Derived rule status from the worst matched actor.", func() {
		Enum("healthy", "approaching", "flagging", "blocking")
	})

	Required("rule_id", "matched_users", "users_warned", "users_breached", "spend_usd", "budget_usd", "worst_used_pct", "status")
})

var SpendRulesOverviewResult = Type("SpendRulesOverviewResult", func() {
	Attribute("total_spend_usd", Float64, "Spend in USD across all users matched by enabled rules, in each rule's current window.")
	Attribute("total_budget_usd", Float64, "Total budgeted spend in USD across enabled rules (per-person limits times matched users).")
	Attribute("users_breached", Int, "Distinct users at or past the limit of at least one enabled rule.")
	Attribute("users_total", Int, "Distinct users matched by at least one enabled rule.")
	Attribute("rules_unhealthy", Int, "Enabled rules whose status is not healthy.")
	Attribute("rules_total", Int, "Total enabled rules.")
	Attribute("projected_overrun_usd", Float64, "Projected end-of-window spend beyond budget in USD, extrapolated linearly from spend so far across enabled rules.")
	Attribute("rules", ArrayOf(SpendRuleUsage), "Current-window usage per enabled rule.")

	Required("total_spend_usd", "total_budget_usd", "users_breached", "users_total", "rules_unhealthy", "rules_total", "projected_overrun_usd", "rules")
})

var ListSpendRulesResult = Type("ListSpendRulesResult", func() {
	Attribute("rules", ArrayOf(SpendRule), "The list of spend rules.")
	Required("rules")
})

var ListSpendRuleEventsResult = Type("ListSpendRuleEventsResult", func() {
	Attribute("events", ArrayOf(SpendRuleEvent), "The list of spend rule events, most recent first.")
	Attribute("next_cursor", String, "Cursor for the next page of events.")
	Required("events")
})
