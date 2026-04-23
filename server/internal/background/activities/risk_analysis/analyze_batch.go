package risk_analysis

import (
	"context"
	"fmt"
	"log/slog"
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
	ProjectID      uuid.UUID
	OrganizationID string
	RiskPolicyID   uuid.UUID
	PolicyVersion  int64
	MessageIDs     []uuid.UUID
	Sources        []string
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
		outcome := o11y.OutcomeFromError(err)
		a.metrics.RecordScan(ctx, args.OrganizationID, outcome, len(args.MessageIDs), time.Since(start))
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

	// Fetch message content for the batch.
	queries := repo.New(a.db)
	var messages []repo.GetMessageContentBatchRow
	{
		ctx, fetchSpan := a.tracer.Start(ctx, "risk.fetchContent")
		var fetchErr error
		messages, fetchErr = queries.GetMessageContentBatch(ctx, repo.GetMessageContentBatchParams{
			Ids:       args.MessageIDs,
			ProjectID: uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		})
		if fetchErr != nil {
			fetchSpan.SetStatus(codes.Error, fetchErr.Error())
		}
		fetchSpan.End()
		if fetchErr != nil {
			return nil, fmt.Errorf("get message content batch: %w", fetchErr)
		}
	}

	// Build content slice for parallel scanning.
	contents := make([]string, len(messages))
	for i, msg := range messages {
		contents[i] = msg.Content
	}

	// Scan all messages with gitleaks (secrets) and presidio (PII) in parallel.
	var gitleaksFindings, presidioFindings [][]Finding
	{
		ctx, scanSpan := a.tracer.Start(ctx, "risk.scanMessages")
		activity.RecordHeartbeat(ctx, 0)

		var gitleaksErr, presidioErr error
		gitleaksFindings, gitleaksErr = a.scanner.ScanBatchParallel(contents)
		if gitleaksErr != nil {
			scanSpan.SetStatus(codes.Error, gitleaksErr.Error())
			scanSpan.End()
			return nil, fmt.Errorf("gitleaks scan batch: %w", gitleaksErr)
		}

		presidioFindings, presidioErr = a.piiScanner.AnalyzeBatch(ctx, contents)
		if presidioErr != nil {
			// PII scan failure is non-fatal; log and continue with gitleaks results only.
			a.logger.WarnContext(ctx, "presidio scan failed, continuing with gitleaks only", attr.SlogError(presidioErr))
			presidioFindings = make([][]Finding, len(contents))
		}
		scanSpan.End()
	}

	// Merge findings from both scanners.
	batchFindings := make([][]Finding, len(contents))
	for i := range contents {
		merged := make([]Finding, 0, len(gitleaksFindings[i])+len(presidioFindings[i]))
		merged = append(merged, gitleaksFindings[i]...)
		merged = append(merged, presidioFindings[i]...)
		batchFindings[i] = merged
	}

	// Convert scan results to DB rows.
	var rows []repo.InsertRiskResultsParams
	findingsCount := 0

	for i, msg := range messages {
		findings := batchFindings[i]

		if len(findings) == 0 {
			resultID, err := uuid.NewV7()
			if err != nil {
				return nil, fmt.Errorf("generate result id: %w", err)
			}
			rows = append(rows, emptyResultRow(resultID, args, msg.ID))
			continue
		}

		for _, f := range findings {
			findingsCount++
			a.metrics.RecordFindingConfidence(ctx, args.OrganizationID, f.RuleID, f.Confidence)
			resultID, err := uuid.NewV7()
			if err != nil {
				return nil, fmt.Errorf("generate result id: %w", err)
			}
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
	// Atomically delete old results (any version) and insert new ones.
	writeErr := func() (writeErr error) {
		ctx, writeSpan := a.tracer.Start(ctx, "risk.writeResults")
		defer func() {
			if writeErr != nil {
				writeSpan.SetStatus(codes.Error, writeErr.Error())
			}
			writeSpan.End()
		}()

		tx, err := a.db.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}
		defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

		txRepo := queries.WithTx(tx)

		if err := txRepo.DeleteRiskResultsForMessages(ctx, repo.DeleteRiskResultsForMessagesParams{
			RiskPolicyID: args.RiskPolicyID,
			ProjectID:    args.ProjectID,
			MessageIds:   args.MessageIDs,
		}); err != nil {
			return fmt.Errorf("delete old results: %w", err)
		}

		if len(rows) > 0 {
			if _, err := txRepo.InsertRiskResults(ctx, rows); err != nil {
				return fmt.Errorf("insert risk results: %w", err)
			}
		}

		return tx.Commit(ctx)
	}()
	if writeErr != nil {
		return nil, writeErr
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
