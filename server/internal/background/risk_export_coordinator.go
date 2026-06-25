package background

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	risk_export "github.com/speakeasy-api/gram/server/internal/background/activities/risk_export"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	riskExportPageSize            int32 = 500
	riskExportChunkSize           int   = 100
	riskExportPagesPerRun         int   = 20
	riskExportDefaultSignedTTL          = time.Hour
	riskExportWriteChunkTimeout         = 30 * time.Minute
	riskExportWriteChunkHeartbeat       = 60 * time.Second
	// riskExportMaxParts bounds a single export so a runaway fan-out cannot
	// produce an unbounded number of objects. Hit only by very large
	// unsampled exports; surfaced as an error so the operator re-runs with a
	// tighter filter or a sample.
	riskExportMaxParts = 50_000
)

// RiskExportParams is the single peer-reviewed parameterization of an export
// run. The fields below the divider are carry-over state populated by
// ContinueAsNew; callers leave them zero-valued.
type RiskExportParams struct {
	RequestID    uuid.UUID
	Operator     string
	Filters      risk_export.Filters
	Sampling     risk_export.Sampling
	Mode         risk_export.Mode
	ContextSize  int64
	UseReplica   bool
	OutputPrefix string // resolved base; defaults to risk-exports/<org>/<request_id>
	TargetKind   string // "gcs" | "local"
	SignedTTL    time.Duration
	DryRun       bool

	// ── carry-over state (set by ContinueAsNew) ──
	AfterID          *uuid.UUID
	NextPartIndex    int
	AccumulatedChats int64
	AccumulatedRows  int64
	Parts            []string
}

type RiskExportResult struct {
	DryRun             bool
	TotalChats         int64
	TotalRows          int64
	TotalParts         int
	ManifestObjectPath string
	ManifestSignedURL  string
}

func riskExportWorkflowID(requestID uuid.UUID) string {
	return fmt.Sprintf("v1:risk-export:%s", requestID.String())
}

// RiskExportWorkflow extracts chat transcripts and risk data to object storage
// for offline risk-clustering analysis. It counts the sampled population first
// (returning early on a dry run), then pages over sampled chats, fanning out a
// WriteExportChunk per chunk of chats and folding the results. History is kept
// bounded with ContinueAsNew; the manifest is written once on the terminal run.
func RiskExportWorkflow(ctx workflow.Context, params RiskExportParams) (*RiskExportResult, error) {
	logger := workflow.GetLogger(ctx)

	if params.OutputPrefix == "" {
		params.OutputPrefix = fmt.Sprintf("risk-exports/%s/%s", params.Filters.OrganizationID, params.RequestID.String())
	}
	if params.TargetKind == "" {
		params.TargetKind = "gcs"
	}
	if params.SignedTTL <= 0 {
		params.SignedTTL = riskExportDefaultSignedTTL
	}

	baseOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    60 * time.Second,
		},
	}
	baseCtx := workflow.WithActivityOptions(ctx, baseOpts)

	writeOpts := baseOpts
	writeOpts.StartToCloseTimeout = riskExportWriteChunkTimeout
	writeOpts.HeartbeatTimeout = riskExportWriteChunkHeartbeat
	writeCtx := workflow.WithActivityOptions(ctx, writeOpts)

	var a *Activities

	// Count + dry-run gate (first invocation only).
	if params.AfterID == nil && params.NextPartIndex == 0 {
		var count risk_export.CountExportRowsResult
		if err := workflow.ExecuteActivity(baseCtx, a.CountExportRows, risk_export.CountExportRowsArgs{
			Filters:  params.Filters,
			Sampling: params.Sampling,
		}).Get(ctx, &count); err != nil {
			return nil, fmt.Errorf("count export rows: %w", err)
		}
		logger.Info("risk export sampled population counted", "chat_count", count.ChatCount, "dry_run", params.DryRun)
		if params.DryRun {
			return &RiskExportResult{
				DryRun:             true,
				TotalChats:         count.ChatCount,
				TotalRows:          0,
				TotalParts:         0,
				ManifestObjectPath: "",
				ManifestSignedURL:  "",
			}, nil
		}
	}

	pagesThisRun := 0
	for {
		var page risk_export.FetchExportChatPageResult
		if err := workflow.ExecuteActivity(baseCtx, a.FetchExportChatPage, risk_export.FetchExportChatPageArgs{
			Filters:  params.Filters,
			Sampling: params.Sampling,
			AfterID:  params.AfterID,
			PageSize: riskExportPageSize,
		}).Get(ctx, &page); err != nil {
			return nil, fmt.Errorf("fetch export chat page: %w", err)
		}

		if len(page.ChatIDs) > 0 {
			chunks := chunkUUIDs(page.ChatIDs, riskExportChunkSize)
			if params.NextPartIndex+len(chunks) > riskExportMaxParts {
				return nil, fmt.Errorf("risk export exceeded max parts (%d); narrow the filters or lower the sample percent", riskExportMaxParts)
			}

			futures := make([]workflow.Future, len(chunks))
			for i, chunk := range chunks {
				futures[i] = workflow.ExecuteActivity(writeCtx, a.WriteExportChunk, risk_export.WriteExportChunkArgs{
					Mode:         params.Mode,
					Filters:      params.Filters,
					ContextSize:  params.ContextSize,
					ChatIDs:      chunk,
					OutputPrefix: params.OutputPrefix,
					PartIndex:    params.NextPartIndex + i,
					TargetKind:   params.TargetKind,
				})
			}

			for i, f := range futures {
				var res risk_export.WriteExportChunkResult
				if err := f.Get(ctx, &res); err != nil {
					// Record the failure but keep going: exports account for
					// gaps rather than aborting the whole run.
					logger.Error("write export chunk failed", "error", err.Error(), "part_index", params.NextPartIndex+i)
					continue
				}
				if res.ObjectPath != "" {
					params.Parts = append(params.Parts, res.ObjectPath)
					params.AccumulatedRows += res.RowCount
					params.AccumulatedChats += int64(res.ChatCount)
				}
			}
			params.NextPartIndex += len(chunks)
		}

		params.AfterID = page.LastID
		pagesThisRun++

		if !page.HasMore {
			break
		}
		if pagesThisRun >= riskExportPagesPerRun {
			return nil, workflow.NewContinueAsNewError(ctx, RiskExportWorkflow, params)
		}
	}

	manifest := risk_export.Manifest{
		RequestID:      params.RequestID.String(),
		Operator:       params.Operator,
		Mode:           params.Mode,
		ContextSize:    params.ContextSize,
		SamplePercent:  params.Sampling.Percent,
		SampleSeed:     params.Sampling.Seed,
		OrganizationID: params.Filters.OrganizationID,
		ProjectID:      uuidPtrString(params.Filters.ProjectID),
		UsedReplica:    params.UseReplica,
		ReplicaReadAt:  workflow.Now(ctx).UTC(),
		TotalChats:     params.AccumulatedChats,
		TotalRows:      params.AccumulatedRows,
		Parts:          params.Parts,
		SchemaVersion:  risk_export.ManifestSchemaVersion,
	}

	var finalize risk_export.FinalizeExportResult
	if err := workflow.ExecuteActivity(baseCtx, a.FinalizeExport, risk_export.FinalizeExportArgs{
		OutputPrefix: params.OutputPrefix,
		TargetKind:   params.TargetKind,
		SignedTTL:    params.SignedTTL,
		Manifest:     manifest,
	}).Get(ctx, &finalize); err != nil {
		return nil, fmt.Errorf("finalize export: %w", err)
	}

	logger.Info("risk export completed",
		"total_chats", params.AccumulatedChats,
		"total_rows", params.AccumulatedRows,
		"total_parts", len(params.Parts),
	)

	return &RiskExportResult{
		DryRun:             false,
		TotalChats:         params.AccumulatedChats,
		TotalRows:          params.AccumulatedRows,
		TotalParts:         len(params.Parts),
		ManifestObjectPath: finalize.ManifestObjectPath,
		ManifestSignedURL:  finalize.ManifestSignedURL,
	}, nil
}

func uuidPtrString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

// ── Client ────────────────────────────────────────────────────────────────

// RiskExportClient triggers an ad-hoc export run.
type RiskExportClient struct {
	TemporalEnv *tenv.Environment
}

func (c *RiskExportClient) StartExport(ctx context.Context, params RiskExportParams) (client.WorkflowRun, error) {
	id := riskExportWorkflowID(params.RequestID)
	run, err := c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, RiskExportWorkflow, params)
	if err != nil {
		return nil, fmt.Errorf("start risk export workflow: %w", err)
	}
	return run, nil
}
