package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var AdminMemoryEvalRunConfig = Type("AdminMemoryEvalRunConfig", func() {
	Description("The models and repeats one team memory eval run compares.")
	Required("answer_models", "judge_model", "summary_model", "digest_model", "runs")
	Attribute("answer_models", ArrayOf(String), "Models that answer the eval questions; each is scored in every arm.", func() { MinLength(1); MaxLength(4) })
	Attribute("judge_model", String, "Model that grades answers. Pick a different family from the answer models.")
	Attribute("summary_model", String, "Model that writes the masked chat summaries team memory reads.")
	Attribute("digest_model", String, "Model that writes day, week and month digests.")
	Attribute("runs", Int, "Repeats per answer model and arm.", func() { Minimum(1); Maximum(3) })
})

var AdminMemoryEvalRun = Type("AdminMemoryEvalRun", func() {
	Description("One team memory eval run and, once it succeeded, its scored report.")
	Required("id", "status", "config", "requested_by", "created_at")
	Attribute("id", String, "Run ID.", func() { Format(FormatUUID) })
	Attribute("status", String, "Where the run is.", func() { Enum("queued", "running", "succeeded", "failed") })
	Attribute("config", AdminMemoryEvalRunConfig)
	Attribute("cost_usd", Float64, "Model spend the run reported across preparation, answers and grading.")
	Attribute("error", String, "Why a failed run stopped.")
	Attribute("requested_by", String, "Email of the staff member who started the run.")
	Attribute("report_json", String, "The scored report as JSON: scores per answer model and arm, preparation spend, judge spend. Set on get only, once the run succeeded.")
	Attribute("started_at", String, func() { Format(FormatDateTime) })
	Attribute("finished_at", String, func() { Format(FormatDateTime) })
	Attribute("created_at", String, func() { Format(FormatDateTime) })
})

var AdminMemoryEvalOptions = Type("AdminMemoryEvalOptions", func() {
	Description("What a new team memory eval run may use.")
	Required("defaults", "allowed_models", "daily_run_limit", "runs_today")
	Attribute("defaults", AdminMemoryEvalRunConfig)
	Attribute("allowed_models", ArrayOf(String), "Models a run may use.")
	Attribute("daily_run_limit", Int, "Runs allowed per rolling 24 hours, across all staff.")
	Attribute("runs_today", Int, "Runs started in the last 24 hours.")
})

// memoryEvalMethods are team memory eval runs over the synthetic corpus the
// demo seed loads into the demo organization. Runs never touch customer data.
func memoryEvalMethods() {
	Method("getMemoryEvalOptions", func() {
		Meta("openapi:operationId", "adminGetMemoryEvalOptions")
		Meta("openapi:extension:x-speakeasy-name-override", "getMemoryEvalOptions")
		Description("Default team memory eval config, the models a run may pick from, and the daily run budget.")
		Payload(func() {
			security.AdminAuthPayload()
		})
		Result(AdminMemoryEvalOptions)
		HTTP(func() {
			GET("/admin/memoryEval.options")
			Response(StatusOK)
		})
	})

	Method("listMemoryEvalRuns", func() {
		Meta("openapi:operationId", "adminListMemoryEvalRuns")
		Meta("openapi:extension:x-speakeasy-name-override", "listMemoryEvalRuns")
		Description("Team memory eval runs, newest first, without their reports.")
		shared.CursorPagination()
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("cursor", String, "ID of the last run on the previous page.", func() { Format(FormatUUID) })
			Attribute("limit", Int, "Runs per page.", func() { Minimum(1); Maximum(100); Default(25) })
		})
		Result(func() {
			Required("runs")
			Attribute("runs", ArrayOf(AdminMemoryEvalRun))
			Attribute("next_cursor", String, "Pass as cursor for the next page; absent on the last page.")
		})
		HTTP(func() {
			GET("/admin/memoryEval.runs")
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})
	})

	Method("getMemoryEvalRun", func() {
		Meta("openapi:operationId", "adminGetMemoryEvalRun")
		Meta("openapi:extension:x-speakeasy-name-override", "getMemoryEvalRun")
		Description("One team memory eval run with its report.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("id", String, "Run ID.", func() { Format(FormatUUID) })
			Required("id")
		})
		Result(AdminMemoryEvalRun)
		HTTP(func() {
			GET("/admin/memoryEval.run")
			Param("id")
			Response(StatusOK)
		})
	})

	Method("startMemoryEvalRun", func() {
		Meta("openapi:operationId", "adminStartMemoryEvalRun")
		Meta("openapi:extension:x-speakeasy-name-override", "startMemoryEvalRun")
		Description("Queue a team memory eval run against the demo organization's synthetic corpus. Spends model credits; one run at a time and a daily limit.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("config", AdminMemoryEvalRunConfig)
			Required("config")
		})
		Result(AdminMemoryEvalRun)
		HTTP(func() {
			POST("/admin/memoryEval.start")
			Response(StatusOK)
		})
	})
}
