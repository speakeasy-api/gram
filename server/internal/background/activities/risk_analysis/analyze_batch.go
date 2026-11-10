package risk_analysis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"
	"golang.org/x/sync/errgroup"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/policyflags"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/stokens"
)

// AnalyzeBatch hands a batch of messages to the Pub/Sub analysis handlers for
// one risk policy. The handlers produce every finding and usage reading; the
// activity scans nothing itself and writes nothing to Postgres. It fails
// unless every enforcing scan request is acknowledged, and the coordinator
// marks the batch analyzed only after it succeeds, so a dropped request is
// redriven rather than lost. Shadow LLM analyzer requests are the one
// exception: they only feed engine comparison, so their failures are counted
// and dropped. Disabled rules, builtin presets and exclusions are applied by
// the ClickHouse findings writer.
type AnalyzeBatch struct {
	logger             *slog.Logger
	tracer             trace.Tracer
	metrics            *riskMetrics
	db                 *pgxpool.Pool
	assetStorage       contentPartAssetReader
	stokenCodec        *stokens.Codec
	flags              feature.Provider
	presidioPub        gcp.Publisher[*riskv1.PresidioAnalysis]
	gitleaksPub        gcp.Publisher[*riskv1.GitleaksAnalysis]
	promptInjectionPub gcp.Publisher[*riskv1.PromptInjectionAnalysis]
	promptPolicyPub    gcp.Publisher[*riskv1.PromptPolicyAnalysis]
	customRulesPub     gcp.Publisher[*riskv1.CustomRulesAnalysis]
	llmPub             gcp.Publisher[*riskv1.LLMAnalysis]
	toolCallPub        gcp.Publisher[*riskv1.ToolCallAnalysis]
	accountIdentityPub gcp.Publisher[*riskv1.AccountIdentityAnalysis]
	// llmAnalyzerEnabled prevents diverting work to an unconfigured consumer.
	llmAnalyzerEnabled bool
	llmFallbackOnce    sync.Once
	celEng             *celenv.Engine
	recommended        RecommendedSet
}

type contentPartAssetReader = blobio.Reader

func NewAnalyzeBatch(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	assetStorage contentPartAssetReader,
	flags feature.Provider,
	presidioPub gcp.Publisher[*riskv1.PresidioAnalysis],
	gitleaksPub gcp.Publisher[*riskv1.GitleaksAnalysis],
	promptInjectionPub gcp.Publisher[*riskv1.PromptInjectionAnalysis],
	promptPolicyPub gcp.Publisher[*riskv1.PromptPolicyAnalysis],
	customRulesPub gcp.Publisher[*riskv1.CustomRulesAnalysis],
	llmPub gcp.Publisher[*riskv1.LLMAnalysis],
	toolCallPub gcp.Publisher[*riskv1.ToolCallAnalysis],
	accountIdentityPub gcp.Publisher[*riskv1.AccountIdentityAnalysis],
	celEng *celenv.Engine,
	llmAnalyzerEnabled bool,
) (*AnalyzeBatch, error) {
	logger = logger.With(attr.SlogComponent("risk-analysis-dispatcher"))

	if toolCallPub == nil {
		return nil, errors.New("tool call analysis publisher is required")
	}
	if accountIdentityPub == nil {
		return nil, errors.New("account identity analysis publisher is required")
	}
	recommended, err := CompileRecommended(celEng)
	if err != nil {
		return nil, fmt.Errorf("compile recommended scopes version %d: %w", recommendedscopes.Version, err)
	}

	return &AnalyzeBatch{
		logger:             logger,
		tracer:             tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"),
		metrics:            newRiskMetrics(meterProvider, logger),
		db:                 db,
		assetStorage:       assetStorage,
		stokenCodec:        stokens.NewCodec(),
		flags:              flags,
		presidioPub:        presidioPub,
		gitleaksPub:        gitleaksPub,
		promptInjectionPub: promptInjectionPub,
		promptPolicyPub:    promptPolicyPub,
		customRulesPub:     customRulesPub,
		llmPub:             llmPub,
		toolCallPub:        toolCallPub,
		accountIdentityPub: accountIdentityPub,
		llmAnalyzerEnabled: llmAnalyzerEnabled,
		llmFallbackOnce:    sync.Once{},
		celEng:             celEng,
		recommended:        recommended,
	}, nil
}

type AnalyzeBatchArgs struct {
	ProjectID        uuid.UUID
	OrganizationID   string
	RiskPolicyID     uuid.UUID
	PolicyVersion    int64
	MessageIDs       []uuid.UUID
	ContentPartIDs   []uuid.UUID
	Sources          []string
	PresidioEntities []string
	// PresidioScoreThreshold is the per-policy minimum recognizer confidence
	// (0.0-1.0). Do derives it from the refetched policy's analyzer_config, so it
	// is not a caller input; zero means unset and the handler applies
	// DefaultPresidioScoreThreshold.
	PresidioScoreThreshold float64
	CustomRuleIds          []string
	// ApprovedEmailDomains is the account_identity corporate domain allowlist.
	// Like PresidioScoreThreshold, Do derives it from the refetched policy's
	// analyzer_config, so it is not a caller input.
	ApprovedEmailDomains []string
	// DetectionScopes are the policy's specified per-category detection scopes,
	// each replacing its registry recommendation. Do derives it from the
	// refetched policy's analyzer_config, so it is not a caller input.
	DetectionScopes []DetectionScopeConfig
}

type AnalyzeBatchResult struct {
	// Processed is the number of messages and content parts published.
	Processed int
}

func (a *AnalyzeBatch) Do(ctx context.Context, args AnalyzeBatchArgs) (_ *AnalyzeBatchResult, err error) {
	if len(args.MessageIDs) == 0 && len(args.ContentPartIDs) == 0 {
		return &AnalyzeBatchResult{Processed: 0}, nil
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
		attribute.Int("risk.batch_size", len(args.MessageIDs)+len(args.ContentPartIDs)),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	activity.RecordHeartbeat(ctx, 0)

	policy, err := repo.New(a.db).GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        args.RiskPolicyID,
		ProjectID: args.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		span.SetAttributes(attribute.Bool("risk.policy_deleted", true))
		a.logger.InfoContext(ctx, "risk policy deleted, skipping batch", attr.SlogRiskPolicyID(args.RiskPolicyID.String()))
		return &AnalyzeBatchResult{Processed: 0}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get risk policy: %w", err)
	}
	if !policy.Enabled {
		span.SetAttributes(attribute.Bool("risk.policy_disabled", true))
		a.logger.InfoContext(ctx, "risk policy disabled, skipping batch", attr.SlogRiskPolicyID(args.RiskPolicyID.String()))
		return &AnalyzeBatchResult{Processed: 0}, nil
	}

	// Single source of truth: derive analyzer options from the policy we just
	// refetched, rather than trusting (possibly omitted) caller values.
	args.PresidioScoreThreshold = PresidioScoreThresholdFromConfig(policy.AnalyzerConfig)
	args.ApprovedEmailDomains = ApprovedEmailDomainsFromConfig(policy.AnalyzerConfig)
	args.DetectionScopes = DetectionScopesFromConfig(policy.AnalyzerConfig)

	if len(args.MessageIDs) > 0 && newSourceSet(args.Sources).Has(SourceAccountIdentity) {
		if err := a.publishAccountIdentityScanRequests(ctx, args); err != nil {
			return nil, err
		}
	}

	messages, err := a.fetchContent(ctx, args)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return &AnalyzeBatchResult{Processed: 0}, nil
	}

	masks, err := a.categoryScopeMasks(ctx, args, messages)
	if err != nil {
		return nil, err
	}

	switch policy.PolicyType {
	case PolicyTypePromptBased:
		err = a.publishPromptPolicy(ctx, args, policy, messages, masks)
	default:
		err = a.publishStandardPolicy(ctx, args, messages, policy.CustomRuleIds, masks)
	}
	if err != nil {
		return nil, err
	}
	scannedCount = len(messages)
	span.SetAttributes(attribute.Int("risk.messages_processed", len(messages)))
	return &AnalyzeBatchResult{Processed: len(messages)}, nil
}

// publishPromptPolicy publishes the in-scope messages for the handler to judge.
// A policy without a prompt still publishes: the handler applies the policy's
// fail mode to it, flagging every message when the policy fails closed.
func (a *AnalyzeBatch) publishPromptPolicy(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy, messages []batchMessage, masks CategoryScopeMasks) error {
	enabled, err := policyflags.ProjectFlagChecked(ctx, a.logger, repo.New(a.db), a.flags, args.OrganizationID, args.ProjectID, feature.FlagPromptPolicies)
	if err != nil {
		return fmt.Errorf("check prompt policies flag: %w", err)
	}
	if !enabled {
		return nil
	}
	indices := promptPolicyIndices(messages, masks)
	if len(indices) == 0 {
		return nil
	}
	return a.publishPromptPolicyScanRequests(ctx, args, policy, messages, indices)
}

// publishStandardPolicy publishes each source's in-scope messages to its
// analysis handler. The LLM analyzer lane, when enforcing, stands in for the
// sources the model enforces.
func (a *AnalyzeBatch) publishStandardPolicy(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, customRuleIDs []string, masks CategoryScopeMasks) error {
	sources := newSourceSet(args.Sources)
	lane := a.resolveLLMLane(ctx, args)
	inScope := func(source string) []batchMessage {
		cats := sourceCategories[source]
		a.metrics.RecordRecommendedScopePrefiltered(ctx, args.OrganizationID, source, masks.RecommendedPrefilteredCount(cats))
		return masks.Admitted(messages, cats)
	}

	var group errgroup.Group
	group.Go(func() error {
		return a.publishLLMLane(ctx, args, messages, lane, sources, masks)
	})
	if sources.Has(shadowmcp.SourceShadowMCP) || lane.runs(sources, shadowmcp.SourceDestructiveTool) || lane.runs(sources, SourceCLIDestructive) {
		group.Go(func() error {
			return a.publishToolCallScanRequests(ctx, args, messages, sources, masks, lane.ownEngineRuns(shadowmcp.SourceDestructiveTool), lane.ownEngineRuns(SourceCLIDestructive))
		})
	}
	if lane.runs(sources, SourceGitleaks) {
		subMessages := inScope(SourceGitleaks)
		group.Go(func() error {
			return a.publishGitleaksScanRequests(ctx, args, subMessages)
		})
	}
	if lane.runs(sources, SourcePresidio) {
		subMessages := inScope(SourcePresidio)
		group.Go(func() error {
			return a.publishPresidioScanRequests(ctx, args, subMessages, resolvePresidioScoreThreshold(args.PresidioScoreThreshold))
		})
	}
	if lane.runs(sources, SourcePromptInjection) {
		subMessages := inScope(SourcePromptInjection)
		group.Go(func() error {
			return a.publishPromptInjectionScanRequests(ctx, args, subMessages)
		})
	}
	if len(customRuleIDs) > 0 {
		subMessages := inScope(SourceCustom)
		group.Go(func() error {
			return a.publishCustomRulesScanRequests(ctx, args, subMessages, customRuleIDs)
		})
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf("publish scan requests: %w", err)
	}
	return nil
}

// categoryScopeMasks compiles the policy's detection scopes against the batch.
func (a *AnalyzeBatch) categoryScopeMasks(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage) (CategoryScopeMasks, error) {
	specified, err := CompileDetectionScopes(a.celEng, args.DetectionScopes)
	if err != nil {
		return CategoryScopeMasks{}, fmt.Errorf("compile detection scopes: %w", err)
	}
	return NewCategoryScopes(a.recommended, specified).Masks(ctx, messages), nil
}

func (a *AnalyzeBatch) fetchContent(ctx context.Context, args AnalyzeBatchArgs) ([]batchMessage, error) {
	ctx, fetchSpan := a.tracer.Start(ctx, "risk.fetchContent")
	defer fetchSpan.End()

	queries := repo.New(a.db)
	messages := []batchMessage{}
	if len(args.MessageIDs) > 0 {
		rows, err := queries.GetMessageContentBatch(ctx, repo.GetMessageContentBatchParams{
			Ids:       args.MessageIDs,
			ProjectID: uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		})
		if err != nil {
			fetchSpan.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("get message content batch: %w", err)
		}
		messages = append(messages, newBatchMessages(ctx, a.logger, rows)...)
	}
	if len(args.ContentPartIDs) > 0 {
		rows, err := queries.GetContentPartBatch(ctx, repo.GetContentPartBatchParams{
			Ids:       args.ContentPartIDs,
			ProjectID: uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		})
		if err != nil {
			fetchSpan.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("get content part batch: %w", err)
		}
		contents, err := a.hydrateContentPartBatch(ctx, rows)
		if err != nil {
			fetchSpan.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		messages = append(messages, newContentPartBatchMessages(rows, contents)...)
	}
	for _, msg := range messages {
		if !msg.Truncated {
			continue
		}
		a.logger.WarnContext(ctx, "risk analysis: truncated oversized batch input",
			attr.SlogProjectID(args.ProjectID.String()),
			attr.SlogMessageID(msg.ID.String()),
		)
		a.metrics.RecordBatchInputTruncation(ctx, args.OrganizationID, msg.ContentPart)
	}
	return messages, nil
}

const maxConcurrentContentPartAssetReads = 32

// contentPartAssetReadLimit reads one byte past the bound so bound can tell a
// cut asset from one that fits exactly.
const contentPartAssetReadLimit = batchScanMaxContentBytes + 1

func (a *AnalyzeBatch) hydrateContentPartBatch(ctx context.Context, rows []repo.GetContentPartBatchRow) ([]string, error) {
	contents := make([]string, len(rows))
	var group errgroup.Group
	group.SetLimit(maxConcurrentContentPartAssetReads)
	for i := range rows {
		group.Go(func() error {
			content, err := a.readContentPartAsset(ctx, rows[i].ID, rows[i].ContentAssetUrl)
			if err != nil {
				return err
			}
			contents[i] = content
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("hydrate content part batch: %w", err)
	}
	return contents, nil
}

func (a *AnalyzeBatch) readContentPartAsset(ctx context.Context, contentPartID uuid.UUID, rawURL string) (string, error) {
	content, err := blobio.ReadAllString(ctx, a.assetStorage, rawURL, contentPartAssetReadLimit)
	if err != nil {
		return "", fmt.Errorf("read content part asset for %s: %w", contentPartID, err)
	}
	return content, nil
}
