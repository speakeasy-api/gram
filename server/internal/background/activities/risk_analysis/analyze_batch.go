package risk_analysis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// AnalyzeBatch scans a batch of messages against enabled detection sources
// and writes the results back to the database.
type AnalyzeBatch struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	repo     *repo.Queries
	scanPool *DetectorPool
}

func NewAnalyzeBatch(logger *slog.Logger, db *pgxpool.Pool, pool *DetectorPool) *AnalyzeBatch {
	return &AnalyzeBatch{
		logger:   logger,
		db:       db,
		repo:     repo.New(db),
		scanPool: pool,
	}
}

type AnalyzeBatchArgs struct {
	ProjectID     uuid.UUID
	RiskPolicyID  uuid.UUID
	PolicyVersion int64
	MessageIDs    []uuid.UUID
	Sources       []string
}

type AnalyzeBatchResult struct {
	Processed int
	Findings  int
}

func (a *AnalyzeBatch) Do(ctx context.Context, args AnalyzeBatchArgs) (*AnalyzeBatchResult, error) {
	if len(args.MessageIDs) == 0 {
		return &AnalyzeBatchResult{Processed: 0, Findings: 0}, nil
	}

	start := time.Now()

	// Fetch message content for the batch.
	fetchStart := time.Now()
	messages, err := a.repo.GetMessageContentBatch(ctx, repo.GetMessageContentBatchParams{
		Ids:       args.MessageIDs,
		ProjectID: uuid.NullUUID{UUID: args.ProjectID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("get message content batch: %w", err)
	}
	fetchDuration := time.Since(fetchStart)

	// Build content slice for parallel scanning.
	contents := make([]string, len(messages))
	for i, msg := range messages {
		contents[i] = msg.Content
	}

	// Scan all messages in parallel using a goroutine worker pool.
	scanStart := time.Now()
	activity.RecordHeartbeat(ctx, 0)

	batchFindings := a.scanPool.ScanBatch(contents)

	// Convert scan results to DB rows.
	var rows []repo.InsertRiskResultsParams
	findingsCount := 0

	for i, msg := range messages {
		findings := batchFindings[i]

		if len(findings) == 0 {
			rows = append(rows, emptyResultRow(args, msg.ID))
			continue
		}

		for _, f := range findings {
			findingsCount++
			rows = append(rows, repo.InsertRiskResultsParams{
				ProjectID:     args.ProjectID,
				RiskPolicyID:  args.RiskPolicyID,
				PolicyVersion: args.PolicyVersion,
				ChatMessageID: msg.ID,
				Source:        "gitleaks",
				Found:         true,
				RuleID:        pgtype.Text{String: f.RuleID, Valid: true},
				Description:   pgtype.Text{String: f.Description, Valid: true},
				Match:         pgtype.Text{String: f.Match, Valid: true},
				StartLine:     pgtype.Int4{Int32: safeInt32(f.StartLine), Valid: true},
				StartColumn:   pgtype.Int4{Int32: safeInt32(f.StartColumn), Valid: true},
				EndLine:       pgtype.Int4{Int32: safeInt32(f.EndLine), Valid: true},
				EndColumn:     pgtype.Int4{Int32: safeInt32(f.EndColumn), Valid: true},
				Confidence:    pgtype.Float8{Float64: 1.0, Valid: true},
				Tags:          f.Tags,
			})
		}
	}
	scanDuration := time.Since(scanStart)

	// Atomically delete old results (any version) and insert new ones.
	writeStart := time.Now()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback is a no-op after commit

	txRepo := a.repo.WithTx(tx)

	if err := txRepo.DeleteRiskResultsForMessages(ctx, repo.DeleteRiskResultsForMessagesParams{
		RiskPolicyID: args.RiskPolicyID,
		MessageIds:   args.MessageIDs,
	}); err != nil {
		return nil, fmt.Errorf("delete old results: %w", err)
	}

	if len(rows) > 0 {
		if _, err := txRepo.InsertRiskResults(ctx, rows); err != nil {
			return nil, fmt.Errorf("insert risk results: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit results: %w", err)
	}
	writeDuration := time.Since(writeStart)

	a.logger.InfoContext(ctx, "analyzed message batch",
		slog.Int("messages", len(messages)),
		slog.Int("findings", findingsCount),
		slog.Int("rows_written", len(rows)),
		slog.Duration("fetch_content", fetchDuration),
		slog.Duration("scan", scanDuration),
		slog.Duration("db_write", writeDuration),
		slog.Duration("total", time.Since(start)),
	)

	return &AnalyzeBatchResult{
		Processed: len(messages),
		Findings:  findingsCount,
	}, nil
}

func emptyResultRow(args AnalyzeBatchArgs, messageID uuid.UUID) repo.InsertRiskResultsParams {
	return repo.InsertRiskResultsParams{
		ProjectID:     args.ProjectID,
		RiskPolicyID:  args.RiskPolicyID,
		PolicyVersion: args.PolicyVersion,
		ChatMessageID: messageID,
		Source:        "none",
		Found:         false,
		RuleID:        pgtype.Text{String: "", Valid: false},
		Description:   pgtype.Text{String: "", Valid: false},
		Match:         pgtype.Text{String: "", Valid: false},
		StartLine:     pgtype.Int4{Int32: 0, Valid: false},
		StartColumn:   pgtype.Int4{Int32: 0, Valid: false},
		EndLine:       pgtype.Int4{Int32: 0, Valid: false},
		EndColumn:     pgtype.Int4{Int32: 0, Valid: false},
		Confidence:    pgtype.Float8{Float64: 0, Valid: false},
		Tags:          nil,
	}
}

// safeInt32 converts int to int32, clamping at boundaries.
func safeInt32(v int) int32 {
	const maxInt32 = 1<<31 - 1
	const minInt32 = -(1 << 31)
	if v > maxInt32 {
		return maxInt32
	}
	if v < minInt32 {
		return minInt32
	}
	return int32(v)
}
