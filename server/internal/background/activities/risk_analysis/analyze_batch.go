package risk_analysis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// AnalyzeBatch scans a batch of messages against enabled detection sources
// and writes the results back to the database.
type AnalyzeBatch struct {
	logger          *slog.Logger
	tracer          trace.Tracer
	metrics         *riskMetrics
	db              *pgxpool.Pool
	scanner         *gitleaks.Scanner
	piiScanner      PIIScanner
	piScanner       *PromptInjectionScanner
	shadowMCPClient *shadowmcp.Client
	mcpMatchLookup  MCPMatchLookup
	judge           PromptJudge
	flags           feature.Provider
	presidioPub     gcp.Publisher[*riskv1.PresidioAnalysis]
	gitleaksPub     gcp.Publisher[*riskv1.GitleaksAnalysis]
	celEng          *celenv.Engine
}

func NewAnalyzeBatch(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	piiScanner PIIScanner,
	piScanner *PromptInjectionScanner,
	shadowMCPClient *shadowmcp.Client,
	mcpMatchLookup MCPMatchLookup,
	judge PromptJudge,
	flags feature.Provider,
	presidioPub gcp.Publisher[*riskv1.PresidioAnalysis],
	gitleaksPub gcp.Publisher[*riskv1.GitleaksAnalysis],
	celEng *celenv.Engine,
) *AnalyzeBatch {
	if piiScanner == nil {
		piiScanner = &StubPIIScanner{}
	}
	if piScanner == nil {
		piScanner = NewPromptInjectionScanner(logger, nil)
	}
	return &AnalyzeBatch{
		logger:          logger.With(attr.SlogComponent("risk-analysis-dispatcher")),
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"),
		metrics:         newRiskMetrics(meterProvider, logger),
		db:              db,
		scanner:         gitleaks.NewScanner(),
		piiScanner:      piiScanner,
		piScanner:       piScanner,
		shadowMCPClient: shadowMCPClient,
		mcpMatchLookup:  mcpMatchLookup,
		judge:           judge,
		flags:           flags,
		presidioPub:     presidioPub,
		gitleaksPub:     gitleaksPub,
		celEng:          celEng,
	}
}

type AnalyzeBatchArgs struct {
	ProjectID            uuid.UUID
	OrganizationID       string
	RiskPolicyID         uuid.UUID
	PolicyVersion        int64
	MessageIDs           []uuid.UUID
	Sources              []string
	MessageTypes         []string
	PresidioEntities     []string
	PromptInjectionRules []string
	CustomRuleIds        []string
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
	scannedCount := 0
	defer func() {
		a.metrics.RecordScan(ctx, args.OrganizationID, o11y.OutcomeFromError(err), scannedCount, time.Since(start))
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

	policy, err := repo.New(a.db).GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        args.RiskPolicyID,
		ProjectID: args.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Policy was deleted (soft or hard) between FetchUnanalyzedMessages
		// returning IDs and this activity running. FetchUnanalyzed errors out
		// on the next drain cycle, so there is no infinite loop and no need
		// to write Found=false rows; the FK to risk_policies might also be
		// gone on hard-delete.
		span.SetAttributes(attribute.Bool("risk.policy_deleted", true))
		a.logger.InfoContext(ctx, "risk policy deleted, skipping batch",
			attr.SlogRiskPolicyID(args.RiskPolicyID.String()),
		)
		return &AnalyzeBatchResult{Processed: 0, Findings: 0}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get risk policy: %w", err)
	}
	if !policy.Enabled {
		// Policy was disabled mid-flight. FetchUnanalyzed returns no IDs while
		// disabled (no infinite loop), and a re-enable bumps the policy
		// version so FetchUnanalyzedMessageIDs picks these messages up again.
		span.SetAttributes(attribute.Bool("risk.policy_disabled", true))
		a.logger.InfoContext(ctx, "risk policy disabled, skipping batch",
			attr.SlogRiskPolicyID(args.RiskPolicyID.String()),
		)
		return &AnalyzeBatchResult{Processed: 0, Findings: 0}, nil
	}

	messages, err := a.fetchContent(ctx, args)
	if err != nil {
		return nil, err
	}
	// message_types is the coarse filter; scope_include narrows further on top of
	// it (not instead of it), scope_exempt takes a matched message out.
	eng := a.celEng
	scope, err := CompileScope(eng, policy.ScopeInclude.String, policy.ScopeExempt.String)
	if err != nil {
		return nil, fmt.Errorf("compile policy scope: %w", err)
	}
	messages = filterMessagesByMessageTypes(messages, args.MessageTypes)
	scannedCount = len(messages)
	if len(messages) == 0 {
		if err := a.writeResults(ctx, args, nil); err != nil {
			return nil, err
		}
		return &AnalyzeBatchResult{Processed: 0, Findings: 0}, nil
	}

	customRules, err := a.customRulesForPolicy(ctx, args.ProjectID, policy.CustomRuleIds)
	if err != nil {
		return nil, err
	}

	// Load the going-forward exclusion set (the policy's own exclusions plus any
	// global ones). It is applied inside scan BEFORE the overlap-dedup pass so
	// excluding one finding cannot erase an overlapping finding that should
	// still flag the region. The retroactive reconcile sweep flags
	// already-stored findings using the same criteria.
	exclusions, err := repo.New(a.db).ListEnabledExclusionsForPolicy(ctx, repo.ListEnabledExclusionsForPolicyParams{
		ProjectID:    args.ProjectID,
		RiskPolicyID: uuid.NullUUID{UUID: args.RiskPolicyID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list exclusions: %w", err)
	}

	scopeExcluded := a.scopeExclusions(ctx, scope, messages)

	findings, err := a.scan(ctx, args, messages, customRules, NewExclusionSet(exclusions), scopeExcluded)
	if err != nil {
		return nil, err
	}

	// prompt_based policies are evaluated by the LLM judge rather than the
	// source-based scanners above. The judge runs on every in-scope message left
	// after the policy's message_types filter and scope predicates, so it
	// covers whatever the policy declares.
	if policy.PolicyType == "prompt_based" {
		judgeFindings := a.scanPromptJudge(ctx, args, policy, messages, scopeExcluded)
		for i := range findings {
			findings[i] = append(findings[i], judgeFindings[i]...)
		}
	}

	// Drop findings whose canonical rule_id has been unchecked by the policy
	// author. Done after the dedup pass so an enabled secret finding still
	// suppresses an overlapping disabled presidio finding, instead of letting
	// the disabled rule win the overlap and then disappear (leaving the
	// region unflagged).
	if disabled := NewDisabledRuleSet(policy.DisabledRules); !disabled.Empty() {
		for i, batch := range findings {
			findings[i] = disabled.FilterFindings(batch)
		}
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
