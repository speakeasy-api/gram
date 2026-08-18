package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/researchagent"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// McpResearchInput identifies one research run. Everything else is loaded
// from the report and request rows, so a stale queue item cannot carry stale
// evidence into the run.
type McpResearchInput struct {
	ReportID  uuid.UUID
	RequestID uuid.UUID
	ProjectID uuid.UUID
	OrgID     string
}

// McpResearch runs the research agent for approval requests and lands the
// result on the report row.
type McpResearch struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	runner *researchagent.Runner
	flags  feature.Provider
}

// NewMcpResearch builds the research activity over the supplied runner.
func NewMcpResearch(logger *slog.Logger, db *pgxpool.Pool, runner *researchagent.Runner, flags feature.Provider) *McpResearch {
	return &McpResearch{
		logger: logger.With(attr.SlogComponent("mcp-research")),
		db:     db,
		runner: runner,
		flags:  flags,
	}
}

// Run executes the research run named by input and records the outcome on
// the report row. The report row is the durable record: a failure lands
// there as status failed with the reason, so the workflow's single attempt
// never strands a report in running.
func (m *McpResearch) Run(ctx context.Context, input McpResearchInput) error {
	stopHeartbeat := startActivityHeartbeat(ctx)
	defer stopHeartbeat()

	queries := approvalrepo.New(m.db)

	// Re-checked here, not only at start: the flags can flip between the
	// admin's click and this activity being picked up, and the kill switch
	// only means anything if a queued run also honors it. A killed or
	// de-rolled run resolves its report as failed — silently skipping would
	// strand it in running.
	if reason, gated := m.researchGate(ctx, input.OrgID); gated {
		m.failReport(ctx, queries, input, reason)
		return nil
	}

	request, err := queries.GetApprovalRequest(ctx, approvalrepo.GetApprovalRequestParams{
		ID:        input.RequestID,
		ProjectID: input.ProjectID,
	})
	if err != nil {
		m.failReport(ctx, queries, input, "the approval request could not be loaded")
		return fmt.Errorf("load approval request for research: %w", err)
	}

	document, meta, err := m.runner.Run(ctx, researchagent.RunInput{
		OrgID:       input.OrgID,
		ProjectID:   input.ProjectID,
		ReportID:    input.ReportID,
		TargetKind:  request.TargetKind,
		TargetRaw:   request.TargetRaw,
		ArtifactRef: conv.PtrValOr(conv.FromPGText[string](request.ArtifactRef), ""),
		Evidence:    request.CurrentEvidence,
	})
	if err != nil {
		m.logger.ErrorContext(ctx, "mcp research run failed", attr.SlogError(err))
		m.failReport(ctx, queries, input, err.Error())
		return fmt.Errorf("run research agent: %w", err)
	}

	if _, err := queries.CompleteResearchReport(ctx, approvalrepo.CompleteResearchReportParams{
		ID:            input.ReportID,
		ProjectID:     input.ProjectID,
		Report:        document,
		ReportVersion: researchagent.ReportVersion,
		Model:         conv.ToPGText(meta.Model),
	}); err != nil {
		// The row is no longer running: a compensation already resolved it
		// while this activity was finishing. The late result is discarded on
		// purpose — the terminal state an admin has already been shown must
		// not flip back to completed underneath them — and the workflow ends
		// cleanly rather than firing a compensation that would no-op.
		if errors.Is(err, pgx.ErrNoRows) {
			m.logger.WarnContext(ctx, "discarding mcp research result for an already-resolved report")
			return nil
		}

		// The run itself succeeded and the spend already happened, but the
		// row still says running. Resolve it here with the real reason
		// rather than leaving it to the workflow's compensation, which can
		// only report that something was interrupted.
		m.logger.ErrorContext(ctx, "complete mcp research report failed", attr.SlogError(err))
		m.failReport(ctx, queries, input, "the research run finished but its report could not be stored")
		return fmt.Errorf("complete research report: %w", err)
	}

	m.logger.InfoContext(ctx, "mcp research run completed",
		attr.SlogGenAIRequestModel(meta.Model),
	)

	return nil
}

// MarkInterrupted resolves a report whose run died without reaching its own
// failure handling — a crashed worker or a heartbeat timeout. Marking a row
// that already resolved is a no-op: the failure update only touches rows
// still in running.
func (m *McpResearch) MarkInterrupted(ctx context.Context, input McpResearchInput) error {
	queries := approvalrepo.New(m.db)
	_, err := queries.FailResearchReport(ctx, approvalrepo.FailResearchReportParams{
		ID:        input.ReportID,
		ProjectID: input.ProjectID,
		Error:     conv.ToPGText("the research run was interrupted before it finished"),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Already resolved — completed, failed, or gone.
		return nil
	case err != nil:
		return fmt.Errorf("mark research report interrupted: %w", err)
	}

	return nil
}

// failReport lands the failure on the report row, best-effort: the returned
// activity error is what surfaces operationally, and the row is what the
// admin sees.
func (m *McpResearch) failReport(ctx context.Context, queries *approvalrepo.Queries, input McpResearchInput, reason string) {
	// The run's context may already be dead — that must not keep the failure
	// off the row.
	ctx = context.WithoutCancel(ctx)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if _, err := queries.FailResearchReport(ctx, approvalrepo.FailResearchReportParams{
		ID:        input.ReportID,
		ProjectID: input.ProjectID,
		Error:     conv.ToPGText(reason),
	}); err != nil {
		m.logger.ErrorContext(ctx, "record research failure", attr.SlogError(err))
	}
}

// researchGate mirrors the service's start-time gate for the execution side:
// the kill switch first (affirmatively on stops everything, failing open on
// evaluation errors), then the rollout flag (failing closed). Returns the
// reason to record when the run must not execute.
func (m *McpResearch) researchGate(ctx context.Context, organizationID string) (string, bool) {
	var groups map[string]string
	org, err := orgrepo.New(m.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		m.logger.WarnContext(ctx, "resolve organization slug for mcp research flag", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
	} else {
		groups = feature.OrgProjectGroups(org.Slug, "")
	}

	killed, err := m.flags.IsFlagEnabled(ctx, feature.FlagMCPResearchKill, organizationID, groups)
	if err != nil {
		m.logger.WarnContext(ctx, "mcp research kill-switch check failed; continuing to rollout check", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
	} else if killed {
		return "research was disabled before this run executed", true
	}

	enabled, err := m.flags.IsFlagEnabled(ctx, feature.FlagMCPResearch, organizationID, groups)
	if err != nil {
		m.logger.WarnContext(ctx, "mcp research flag check failed; treating as disabled", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return "research is not enabled for this organization", true
	}
	if !enabled {
		return "research is not enabled for this organization", true
	}

	return "", false
}
