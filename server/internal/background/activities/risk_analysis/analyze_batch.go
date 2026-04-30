package risk_analysis

import (
	"context"
	"encoding/json"
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
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
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

	findings, err := a.scan(ctx, args, messages)
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
// shadow_mcp scanning runs serially after the parallel scans because it makes
// per-message DB calls.
func (a *AnalyzeBatch) scan(ctx context.Context, args AnalyzeBatchArgs, messages []repo.GetMessageContentBatchRow) ([][]Finding, error) {
	ctx, scanSpan := a.tracer.Start(ctx, "risk.scanMessages")
	defer scanSpan.End()
	activity.RecordHeartbeat(ctx, 0)

	n := len(messages)
	contents := make([]string, n)
	for i, msg := range messages {
		contents[i] = msg.Content
	}

	gitleaksFindings := make([][]Finding, n)
	presidioFindings := make([][]Finding, n)
	shadowMCPFindings := make([][]Finding, n)

	var wg sync.WaitGroup
	var gitleaksErr error

	if slices.Contains(args.Sources, "gitleaks") {
		wg.Go(func() {
			results, err := a.scanner.ScanBatchParallel(contents)
			if err != nil {
				gitleaksErr = err
				return
			}
			gitleaksFindings = results
		})
	}

	if slices.Contains(args.Sources, "presidio") {
		wg.Go(func() {
			results, err := a.piiScanner.AnalyzeBatch(ctx, contents, args.PresidioEntities, func() {
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

	if slices.Contains(args.Sources, shadowmcp.SourceShadowMCP) {
		shadowMCPFindings = a.scanShadowMCP(ctx, args.OrganizationID, messages)
		activity.RecordHeartbeat(ctx, "shadow_mcp")
	}

	merged := make([][]Finding, n)
	for i := range n {
		// Gitleaks findings come first so they take priority over presidio
		// when both scanners match the same text region. shadow_mcp findings
		// are non-overlapping with content scanners (they apply to tool_calls,
		// not content), so they always pass through dedup.
		combined := slices.Concat(gitleaksFindings[i], presidioFindings[i], shadowMCPFindings[i])
		merged[i] = dedup(combined)
	}
	return merged, nil
}

// scanShadowMCP validates each message's tool_calls against the shadow-MCP
// guard. Messages without tool_calls (user prompts, assistant text, tool
// results) are skipped. Each unsigned or mismatched call produces one Finding.
func (a *AnalyzeBatch) scanShadowMCP(ctx context.Context, orgID string, messages []repo.GetMessageContentBatchRow) [][]Finding {
	out := make([][]Finding, len(messages))
	for i, msg := range messages {
		if len(msg.ToolCalls) == 0 {
			continue
		}
		out[i] = a.scanMessageToolCalls(ctx, orgID, msg.ToolCalls)
	}
	return out
}

// scanMessageToolCalls iterates the tool_calls JSON array stored on a chat
// message and runs shadowmcp.ValidateGramToolsetCall against each call. The
// expected payload mirrors what hooks/session_capture.go writes:
// [{"id": "...", "type": "function", "function": {"name": "...", "arguments": "<json>"}}]
func (a *AnalyzeBatch) scanMessageToolCalls(ctx context.Context, orgID string, raw []byte) []Finding {
	var calls []struct {
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &calls); err != nil {
		a.logger.WarnContext(ctx, "shadow_mcp scan: failed to parse tool_calls", attr.SlogError(err))
		return nil
	}

	var findings []Finding
	for _, call := range calls {
		toolName := call.Function.Name
		if toolName == "" {
			continue
		}
		// Native (non-MCP) tools don't carry the x-gram-toolset-id property
		// and are out of scope for shadow-MCP enforcement.
		if !isMCPToolName(toolName) {
			continue
		}
		var toolInput any
		if call.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &toolInput); err != nil {
				// Treat unparseable args as a missing toolset id.
				toolInput = nil
			}
		}
		bareName := stripMCPToolPrefix(toolName)
		detail, denied := shadowmcp.ValidateGramToolsetCall(ctx, a.logger, a.db, nil, toolInput, bareName, orgID)
		if !denied {
			continue
		}
		findings = append(findings, Finding{
			Source:      shadowmcp.SourceShadowMCP,
			RuleID:      "shadow_mcp.unverified_call",
			Description: detail,
			Match:       toolName,
			StartPos:    0,
			EndPos:      0,
			Tags:        nil,
			Confidence:  1.0,
		})
	}
	return findings
}

// isMCPToolName reports whether a tool-call name follows either the
// "mcp__<server>__<tool>" convention used by Claude Code or the "MCP:..."
// prefix used by Cursor for MCP-routed tools.
func isMCPToolName(name string) bool {
	if len(name) >= 5 && name[:5] == "mcp__" {
		return true
	}
	if len(name) >= 4 && name[:4] == "MCP:" {
		return true
	}
	return false
}

// stripMCPToolPrefix returns the bare tool name with any MCP routing prefix
// removed so it can be compared against the toolset's tool list.
func stripMCPToolPrefix(name string) string {
	if len(name) >= 5 && name[:5] == "mcp__" {
		// mcp__<server>__<tool>
		rest := name[5:]
		for i := 0; i+1 < len(rest); i++ {
			if rest[i] == '_' && rest[i+1] == '_' {
				return rest[i+2:]
			}
		}
		return rest
	}
	if len(name) >= 4 && name[:4] == "MCP:" {
		return name[4:]
	}
	return name
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
