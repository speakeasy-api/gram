package risk_analysis

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// AnalyzeBatch scans a batch of messages against enabled detection sources
// and writes the results back to the database.
type AnalyzeBatch struct {
	logger     *slog.Logger
	tracer     trace.Tracer
	metrics    *riskMetrics
	db         *pgxpool.Pool
	scanner    *Scanner
	piiScanner PIIScanner
}

func NewAnalyzeBatch(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, piiScanner PIIScanner) *AnalyzeBatch {
	if piiScanner == nil {
		piiScanner = &StubPIIScanner{}
	}
	return &AnalyzeBatch{
		logger:     logger,
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"),
		metrics:    newRiskMetrics(meterProvider, logger),
		db:         db,
		scanner:    NewScanner(),
		piiScanner: piiScanner,
	}
}

type AnalyzeBatchArgs struct {
	ProjectID        uuid.UUID
	OrganizationID   string
	RiskPolicyID     uuid.UUID
	PolicyVersion    int64
	MessageIDs       []uuid.UUID
	Sources          []string
	PresidioEntities []string
}

type AnalyzeBatchResult struct {
	Processed int
	Findings  int
}

func (a *AnalyzeBatch) Do(ctx context.Context, args AnalyzeBatchArgs) (_ *AnalyzeBatchResult, err error) {
	if len(args.MessageIDs) == 0 {
		return &AnalyzeBatchResult{Processed: 0, Findings: 0}, nil
	}

	start := time.Now()
	defer func() {
		a.metrics.RecordScan(ctx, args.OrganizationID, o11y.OutcomeFromError(err), len(args.MessageIDs), time.Since(start))
	}()

	ctx, span := a.tracer.Start(ctx, "risk.analyzeBatch", trace.WithAttributes(
		attribute.String("risk.project_id", args.ProjectID.String()),
		attribute.String("risk.policy_id", args.RiskPolicyID.String()),
		attribute.Int64("risk.policy_version", args.PolicyVersion),
		attribute.Int("risk.batch_size", len(args.MessageIDs)),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	messages, err := a.fetchContent(ctx, args)
	if err != nil {
		return nil, err
	}

	contents := make([]string, len(messages))
	for i, msg := range messages {
		contents[i] = msg.Content
	}

	findings, err := a.scan(ctx, args.Sources, args.PresidioEntities, contents)
	if err != nil {
		return nil, err
	}

	rows, findingsCount := a.buildRows(ctx, args, messages, findings)

	if err := a.writeResults(ctx, args, rows); err != nil {
		return nil, err
	}

	span.SetAttributes(
		attribute.Int("risk.messages_processed", len(messages)),
		attribute.Int("risk.findings_count", findingsCount),
		attribute.Int("risk.rows_written", len(rows)),
	)

	return &AnalyzeBatchResult{
		Processed: len(messages),
		Findings:  findingsCount,
	}, nil
}

func (a *AnalyzeBatch) fetchContent(ctx context.Context, args AnalyzeBatchArgs) ([]repo.GetMessageContentBatchRow, error) {
	ctx, fetchSpan := a.tracer.Start(ctx, "risk.fetchContent")
	defer fetchSpan.End()

	messages, err := repo.New(a.db).GetMessageContentBatch(ctx, repo.GetMessageContentBatchParams{
		Ids:       args.MessageIDs,
		ProjectID: uuid.NullUUID{UUID: args.ProjectID, Valid: true},
	})
	if err != nil {
		fetchSpan.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("get message content batch: %w", err)
	}
	return messages, nil
}

// scan runs enabled scanners concurrently. Gitleaks is CPU-bound, presidio is
// IO-bound, so they parallelize well and avoid exceeding the heartbeat timeout.
func (a *AnalyzeBatch) scan(ctx context.Context, sources []string, presidioEntities []string, contents []string) ([][]Finding, error) {
	ctx, scanSpan := a.tracer.Start(ctx, "risk.scanMessages")
	defer scanSpan.End()
	activity.RecordHeartbeat(ctx, 0)

	n := len(contents)
	gitleaksFindings := make([][]Finding, n)
	presidioFindings := make([][]Finding, n)

	var wg sync.WaitGroup
	var gitleaksErr error

	if slices.Contains(sources, "gitleaks") {
		wg.Go(func() {
			results, err := a.scanner.ScanBatchParallel(contents)
			if err != nil {
				gitleaksErr = err
				return
			}
			gitleaksFindings = results
		})
	}

	if slices.Contains(sources, "presidio") {
		wg.Go(func() {
			results, err := a.piiScanner.AnalyzeBatch(ctx, contents, presidioEntities, func() {
				activity.RecordHeartbeat(ctx, "presidio")
			})
			if err != nil {
				a.logger.WarnContext(ctx, "presidio scan failed, continuing with gitleaks only", attr.SlogError(err))
				if a.metrics.presidioScanSkipped != nil {
					a.metrics.presidioScanSkipped.Add(ctx, 1)
				}
				return
			}
			presidioFindings = results
		})
	}

	wg.Wait()

	if gitleaksErr != nil {
		scanSpan.SetStatus(codes.Error, gitleaksErr.Error())
		return nil, fmt.Errorf("gitleaks scan batch: %w", gitleaksErr)
	}

	merged := make([][]Finding, n)
	for i := range n {
		// Gitleaks findings come first so they take priority over presidio
		// when both scanners match the same text region.
		combined := slices.Concat(gitleaksFindings[i], presidioFindings[i])
		merged[i] = dedup(combined)
	}
	return merged, nil
}

// dedup removes findings that overlap the same text region. Earlier entries
// in the slice win (gitleaks before presidio), so secrets take priority over PII.
func dedup(findings []Finding) []Finding {
	if len(findings) <= 1 {
		return findings
	}
	var out []Finding
	for _, f := range findings {
		if overlapsAny(out, f) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func overlapsAny(kept []Finding, candidate Finding) bool {
	for _, k := range kept {
		if k.StartPos < candidate.EndPos && candidate.StartPos < k.EndPos {
			return true
		}
	}
	return false
}

func (a *AnalyzeBatch) buildRows(ctx context.Context, args AnalyzeBatchArgs, messages []repo.GetMessageContentBatchRow, batchFindings [][]Finding) ([]repo.InsertRiskResultsParams, int) {
	var rows []repo.InsertRiskResultsParams
	findingsCount := 0

	for i, msg := range messages {
		findings := batchFindings[i]

		if len(findings) == 0 {
			resultID, _ := uuid.NewV7()
			rows = append(rows, emptyResultRow(resultID, args, msg.ID))
			continue
		}

		for _, f := range findings {
			findingsCount++
			a.metrics.RecordFindingConfidence(ctx, args.OrganizationID, f.RuleID, f.Confidence)
			resultID, _ := uuid.NewV7()
			rows = append(rows, repo.InsertRiskResultsParams{
				ID:                resultID,
				ProjectID:         args.ProjectID,
				OrganizationID:    args.OrganizationID,
				RiskPolicyID:      args.RiskPolicyID,
				RiskPolicyVersion: args.PolicyVersion,
				ChatMessageID:     msg.ID,
				Source:            f.Source,
				Found:             true,
				RuleID:            pgtype.Text{String: f.RuleID, Valid: true},
				Description:       pgtype.Text{String: f.Description, Valid: true},
				Match:             pgtype.Text{String: f.Match, Valid: true},
				StartPos:          pgtype.Int4{Int32: conv.SafeInt32(f.StartPos), Valid: true},
				EndPos:            pgtype.Int4{Int32: conv.SafeInt32(f.EndPos), Valid: true},
				Confidence:        pgtype.Float8{Float64: f.Confidence, Valid: true},
				Tags:              f.Tags,
			})
		}
	}
	return rows, findingsCount
}

func (a *AnalyzeBatch) writeResults(ctx context.Context, args AnalyzeBatchArgs, rows []repo.InsertRiskResultsParams) error {
	ctx, writeSpan := a.tracer.Start(ctx, "risk.writeResults")
	defer writeSpan.End()

	tx, err := a.db.Begin(ctx)
	if err != nil {
		writeSpan.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txRepo := repo.New(a.db).WithTx(tx)

	if err := txRepo.DeleteRiskResultsForMessages(ctx, repo.DeleteRiskResultsForMessagesParams{
		RiskPolicyID: args.RiskPolicyID,
		ProjectID:    args.ProjectID,
		MessageIds:   args.MessageIDs,
	}); err != nil {
		writeSpan.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("delete old results: %w", err)
	}

	if len(rows) > 0 {
		if _, err := txRepo.InsertRiskResults(ctx, rows); err != nil {
			writeSpan.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("insert risk results: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeSpan.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("commit results: %w", err)
	}
	return nil
}

func emptyResultRow(id uuid.UUID, args AnalyzeBatchArgs, messageID uuid.UUID) repo.InsertRiskResultsParams {
	return repo.InsertRiskResultsParams{
		ID:                id,
		ProjectID:         args.ProjectID,
		OrganizationID:    args.OrganizationID,
		RiskPolicyID:      args.RiskPolicyID,
		RiskPolicyVersion: args.PolicyVersion,
		ChatMessageID:     messageID,
		Source:            "none",
		Found:             false,
		RuleID:            pgtype.Text{String: "", Valid: false},
		Description:       pgtype.Text{String: "", Valid: false},
		Match:             pgtype.Text{String: "", Valid: false},
		StartPos:          pgtype.Int4{Int32: 0, Valid: false},
		EndPos:            pgtype.Int4{Int32: 0, Valid: false},
		Confidence:        pgtype.Float8{Float64: 0, Valid: false},
		Tags:              nil,
	}
}
