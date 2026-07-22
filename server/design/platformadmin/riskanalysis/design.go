// Package riskanalysis declares the adminRiskAnalysis Goa service: the
// platform-admin (Speakeasy-only) surface for triggering and monitoring
// ad-hoc risk analysis runs — operator-driven re-scans of a project's chat
// messages over an explicit time window, e.g. backfilling after a scanner
// fix. Runs execute on a dedicated Temporal task queue, isolated from the
// live risk-analysis pipeline. Implemented on the existing *risk.Service.
package riskanalysis

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var AdhocRiskAnalysisProgress = Type("AdhocRiskAnalysisProgress", func() {
	Description("Progress counters for an ad-hoc risk analysis run.")

	Attribute("total_messages", Int64, "Total messages in the requested time window.")
	Attribute("dispatched_messages", Int64, "Messages dispatched for scanning so far.")
	Attribute("processed_messages", Int64, "Messages scanned across all completed batches.")
	Attribute("findings", Int64, "Risk findings recorded so far.")
	Attribute("batches_completed", Int64, "Analysis batches that completed successfully.")
	Attribute("batches_failed", Int64, "Analysis batches that failed after retries.")
	Attribute("policies", Int, "Number of policies being scanned.")

	Required("total_messages", "dispatched_messages", "processed_messages", "findings", "batches_completed", "batches_failed", "policies")
})

var AdhocRiskAnalysisStatus = Type("AdhocRiskAnalysisStatus", func() {
	Description("Status of a project's most recent ad-hoc risk analysis run.")

	Attribute("workflow_id", String, "The Temporal workflow id of the run; empty when status is none.")
	Attribute("status", String, "One of: none (no run ever triggered), running, completed, failed, canceled, terminated, timed_out.")
	Attribute("started_at", String, "When the run started.", func() {
		Format(FormatDateTime)
	})
	Attribute("closed_at", String, "When the run finished, if it has.", func() {
		Format(FormatDateTime)
	})
	Attribute("progress", AdhocRiskAnalysisProgress, "Live progress counters; absent when unavailable.")

	Required("workflow_id", "status")
})

var _ = Service("adminRiskAnalysis", func() {
	Description("Platform-admin triggering and monitoring of ad-hoc risk analysis runs over an explicit time window. Speakeasy-staff only; every method requires the platform-admin flag.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("trigger", func() {
		Description("Start an ad-hoc risk analysis run re-scanning a project's chat messages against a policy over a time window. One run per project may be in flight at a time. Requires platform admin.")

		Payload(func() {
			Attribute("project_id", String, "The project whose messages to re-scan.", func() {
				Format(FormatUUID)
			})
			Attribute("risk_policy_id", String, "The risk policy to scan against. Must be enabled.", func() {
				Format(FormatUUID)
			})
			Attribute("from", String, "Start of the message window (inclusive).", func() {
				Format(FormatDateTime)
			})
			Attribute("to", String, "End of the message window (exclusive). Defaults to now.", func() {
				Format(FormatDateTime)
			})
			Required("project_id", "risk_policy_id", "from")
			security.SessionPayload()
		})

		Result(AdhocRiskAnalysisStatus)

		HTTP(func() {
			POST("/rpc/adminRiskAnalysis.trigger")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "triggerAdhocRiskAnalysis")
		Meta("openapi:extension:x-speakeasy-name-override", "trigger")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "TriggerAdhocRiskAnalysis"}`)
	})

	Method("status", func() {
		Description("Get the status and progress of a project's most recent ad-hoc risk analysis run. Requires platform admin.")

		Payload(func() {
			Attribute("project_id", String, "The project to report on.", func() {
				Format(FormatUUID)
			})
			Required("project_id")
			security.SessionPayload()
		})

		Result(AdhocRiskAnalysisStatus)

		HTTP(func() {
			GET("/rpc/adminRiskAnalysis.status")
			Param("project_id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAdhocRiskAnalysisStatus")
		Meta("openapi:extension:x-speakeasy-name-override", "status")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AdhocRiskAnalysisStatus"}`)
	})
})
