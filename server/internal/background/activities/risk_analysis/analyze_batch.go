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
	"golang.org/x/sync/errgroup"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/destructivetool"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// AnalyzeBatch scans a batch of messages against one risk policy and replaces
// that policy's stored results for the fetched message IDs.
type AnalyzeBatch struct {
	logger                 *slog.Logger
	tracer                 trace.Tracer
	metrics                *riskMetrics
	db                     *pgxpool.Pool
	assetStorage           contentPartAssetReader
	gitleaksScanner        *gitleaks.Scanner
	piiScanner             PIIScanner
	promptInjectionScanner *promptinjection.Scanner
	shadowMCPScanner       *shadowmcpscan.Scanner
	judge                  promptpolicy.Evaluator
	flags                  feature.Provider
	presidioPub            gcp.Publisher[*riskv1.PresidioAnalysis]
	gitleaksPub            gcp.Publisher[*riskv1.GitleaksAnalysis]
	promptInjectionPub     gcp.Publisher[*riskv1.PromptInjectionAnalysis]
	promptPolicyPub        gcp.Publisher[*riskv1.PromptPolicyAnalysis]
	customRulesPub         gcp.Publisher[*riskv1.CustomRulesAnalysis]
	findingsPub            gcp.Publisher[*riskv1.Finding]
	customRuleScanner      *customruleanalyzer.Scanner
	cliDestructiveScanner  *clidestructive.Scanner
	destructiveToolScanner *destructivetool.Scanner
	celEng                 *celenv.Engine
	builtinPresets         *presetlib.Library
	recommended            RecommendedSet
}

type contentPartAssetReader = blobio.Reader

func NewAnalyzeBatch(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	assetStorage contentPartAssetReader,
	piiScanner PIIScanner,
	promptInjectionScanner *promptinjection.Scanner,
	shadowMCPClient *shadowmcp.Client,
	mcpProvenanceLookup MCPProvenanceLookup,
	judge promptpolicy.Evaluator,
	flags feature.Provider,
	presidioPub gcp.Publisher[*riskv1.PresidioAnalysis],
	gitleaksPub gcp.Publisher[*riskv1.GitleaksAnalysis],
	promptInjectionPub gcp.Publisher[*riskv1.PromptInjectionAnalysis],
	promptPolicyPub gcp.Publisher[*riskv1.PromptPolicyAnalysis],
	customRulesPub gcp.Publisher[*riskv1.CustomRulesAnalysis],
	findingsPub gcp.Publisher[*riskv1.Finding],
	customRuleScanner *customruleanalyzer.Scanner,
	celEng *celenv.Engine,
	builtinPresets *presetlib.Library,
	shadowMCPBypass shadowmcpscan.BypassChecker,
) (*AnalyzeBatch, error) {
	logger = logger.With(attr.SlogComponent("risk-analysis-dispatcher"))

	if piiScanner == nil {
		piiScanner = &StubPIIScanner{}
	}
	if promptInjectionScanner == nil {
		promptInjectionScanner = promptinjection.NewScanner(logger, promptinjection.NoopClassifier)
	}
	if findingsPub == nil {
		findingsPub = gcp.NewNoopPublisher[*riskv1.Finding]()
	}
	recommended, err := CompileRecommended(celEng)
	if err != nil {
		return nil, fmt.Errorf("compile recommended scopes version %d: %w", recommendedscopes.Version, err)
	}

	metrics := newRiskMetrics(meterProvider, logger)

	return &AnalyzeBatch{
		logger:                 logger,
		tracer:                 tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"),
		metrics:                metrics,
		db:                     db,
		assetStorage:           assetStorage,
		gitleaksScanner:        gitleaks.NewScanner(),
		piiScanner:             piiScanner,
		promptInjectionScanner: promptInjectionScanner,
		shadowMCPScanner: shadowmcpscan.NewScanner(
			logger,
			shadowMCPClient,
			shadowMCPClient,
			mcpProvenanceLookup,
			metrics,
			shadowmcpscan.WithShadowMCPBypass(shadowMCPBypass),
		),
		judge:                  judge,
		flags:                  flags,
		presidioPub:            presidioPub,
		gitleaksPub:            gitleaksPub,
		promptInjectionPub:     promptInjectionPub,
		promptPolicyPub:        promptPolicyPub,
		customRulesPub:         customRulesPub,
		findingsPub:            findingsPub,
		customRuleScanner:      customRuleScanner,
		cliDestructiveScanner:  clidestructive.NewScanner(),
		destructiveToolScanner: destructivetool.NewScanner(shadowMCPClient),
		celEng:                 celEng,
		builtinPresets:         builtinPresets,
		recommended:            recommended,
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
	MessageTypes     []string
	PresidioEntities []string
	// PresidioScoreThreshold is the per-policy minimum recognizer confidence
	// (0.0-1.0). Do derives it from the refetched policy's analyzer_config, so it
	// is not a caller input; zero means unset and the scanner applies
	// DefaultPresidioScoreThreshold.
	PresidioScoreThreshold float64
	CustomRuleIds          []string
	// ApprovedEmailDomains is the account_identity corporate domain allowlist.
	// Like PresidioScoreThreshold, Do derives it from the refetched policy's
	// analyzer_config, so it is not a caller input.
	ApprovedEmailDomains []string
	// BuiltinPresetsEnabled toggles scan-time suppression of built-in catalog
	// false positives. Do derives it from the refetched policy's analyzer_config
	// (defaulting ON), so it is not a caller input.
	BuiltinPresetsEnabled bool
	// DetectionScopes are the policy's specified per-category detection scopes,
	// each replacing its registry recommendation. Do derives it from the
	// refetched policy's analyzer_config, so it is not a caller input.
	DetectionScopes []DetectionScopeConfig
}

type AnalyzeBatchResult struct {
	Processed int
	Findings  int
}

func (a *AnalyzeBatch) Do(ctx context.Context, args AnalyzeBatchArgs) (_ *AnalyzeBatchResult, err error) {
	if len(args.MessageIDs) == 0 && len(args.ContentPartIDs) == 0 {
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
		attribute.Int("risk.batch_size", len(args.MessageIDs)+len(args.ContentPartIDs)),
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
		span.SetAttributes(attribute.Bool("risk.policy_deleted", true))
		a.logger.InfoContext(ctx, "risk policy deleted, skipping batch", attr.SlogRiskPolicyID(args.RiskPolicyID.String()))
		return &AnalyzeBatchResult{Processed: 0, Findings: 0}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get risk policy: %w", err)
	}
	if !policy.Enabled {
		span.SetAttributes(attribute.Bool("risk.policy_disabled", true))
		a.logger.InfoContext(ctx, "risk policy disabled, skipping batch", attr.SlogRiskPolicyID(args.RiskPolicyID.String()))
		return &AnalyzeBatchResult{Processed: 0, Findings: 0}, nil
	}

	// Single source of truth: derive analyzer options from the policy we just
	// refetched, rather than trusting (possibly omitted) caller values.
	args.PresidioScoreThreshold = PresidioScoreThresholdFromConfig(policy.AnalyzerConfig)
	args.ApprovedEmailDomains = ApprovedEmailDomainsFromConfig(policy.AnalyzerConfig)
	args.BuiltinPresetsEnabled = BuiltinPresetsEnabledFromConfig(policy.AnalyzerConfig)
	args.DetectionScopes = DetectionScopesFromConfig(policy.AnalyzerConfig)

	messages, err := a.fetchContent(ctx, args)
	if err != nil {
		return nil, err
	}
	messages = filterBatchMessagesByMessageTypes(messages, args.MessageTypes)
	scannedCount = len(messages)

	exclusions := NewExclusionSet(nil)
	if policy.PolicyType != PolicyTypePromptBased {
		exclusions, err = a.policyExclusionSet(ctx, args)
		if err != nil {
			return nil, err
		}
	}

	// Session-scoped account_identity findings are computed over the batch's
	// full message-id set, before the message-type filter and CEL scope above
	// apply: the detector reads the session's account attribution, not message
	// content, so narrowing a policy's scope must not subset which sessions it
	// evaluates. Exclusions apply on entry into the pipeline (in
	// appendSessionFindings below) and disabled_rules at the shared filter
	// step, mirroring the content path.
	var session []sessionFinding
	if len(args.MessageIDs) > 0 && newSourceSet(args.Sources).Has(SourceAccountIdentity) {
		session, err = a.scanAccountIdentity(ctx, args)
		if err != nil {
			return nil, err
		}
	}

	if len(messages) == 0 && len(session) == 0 {
		if _, err := a.writeResults(ctx, args, nil); err != nil {
			return nil, err
		}
		return &AnalyzeBatchResult{Processed: 0, Findings: 0}, nil
	}

	findings := make([][]scanners.Finding, len(messages))
	if len(messages) > 0 {
		scope, err := CompileScope(a.celEng, policy.ScopeInclude.String, policy.ScopeExempt.String)
		if err != nil {
			return nil, fmt.Errorf("compile policy scope: %w", err)
		}
		specified, err := CompileDetectionScopes(a.celEng, args.DetectionScopes)
		if err != nil {
			return nil, fmt.Errorf("compile detection scopes: %w", err)
		}
		recommendedEnabled := a.projectFlagEnabled(ctx, args.OrganizationID, args.ProjectID, feature.FlagRiskRecommendedScopes)
		categoryScopes := NewCategoryScopes(scope, a.recommended, specified, recommendedEnabled, a.metrics)
		masks := categoryScopes.Masks(ctx, messages)

		switch policy.PolicyType {
		case PolicyTypePromptBased:
			findings = a.scanPromptPolicy(ctx, args, policy, messages, masks)
		default:
			findings, err = a.scanStandardPolicy(ctx, args, messages, policy.CustomRuleIds, exclusions, masks)
			if err != nil {
				return nil, err
			}
		}
	}

	ids, findings := mergeSessionFindings(messages, findings, session, exclusions)

	if disabled := NewDisabledRuleSet(policy.DisabledRules); !disabled.Empty() {
		for i, batch := range findings {
			findings[i] = disabled.FilterFindings(batch)
		}
	}

	rowsToWrite, findingsCount := a.buildRows(ctx, args, ids, findings)
	written, err := a.writeResults(ctx, args, rowsToWrite)
	if err != nil {
		return nil, err
	}
	// Mirror the stream scanners onto the shared findings topic for sources
	// that have no stream publisher (ClickHouse would otherwise never see
	// them). Only after a committed write: a batch dropped because its policy
	// was deleted mid-analysis must not leak findings into ClickHouse that
	// Postgres never stored. Best-effort — a publish failure logs and never
	// fails the activity.
	if written {
		a.publishBatchOnlyFindings(ctx, args, ids, findings)
	}

	span.SetAttributes(
		attribute.Int("risk.messages_processed", len(messages)),
		attribute.Int("risk.findings_count", findingsCount),
		attribute.Int("risk.rows_written", len(rowsToWrite)),
	)

	return &AnalyzeBatchResult{
		Processed: len(messages),
		Findings:  findingsCount,
	}, nil
}

// policyExclusionSet loads the enabled exclusions that apply to the policy
// (its own plus globals). Fetched once in Do and shared by the content
// scanners (via mergeFindings) and the session-scoped account_identity path.
func (a *AnalyzeBatch) policyExclusionSet(ctx context.Context, args AnalyzeBatchArgs) (ExclusionSet, error) {
	exclusions, err := repo.New(a.db).ListEnabledExclusionsForPolicy(ctx, repo.ListEnabledExclusionsForPolicyParams{
		ProjectID:    args.ProjectID,
		RiskPolicyID: uuid.NullUUID{UUID: args.RiskPolicyID, Valid: true},
	})
	if err != nil {
		return NewExclusionSet(nil), fmt.Errorf("list exclusions: %w", err)
	}
	return NewExclusionSet(exclusions), nil
}

// mergeSessionFindings folds session-scoped findings into the
// message-aligned findings set (ids and findings must be equal length),
// applying exclusions on the way in (the content path applies them inside
// mergeFindings, which session findings bypass). A session finding lands on
// its carrier message's existing slot when that message survived the
// policy's message-type filter — keeping the "message has findings → no
// sentinel row" invariant — and appends a new (id, findings) entry
// otherwise, since session findings deliberately bypass message scoping.
func mergeSessionFindings(ids []batchMessage, findings [][]scanners.Finding, session []sessionFinding, exclusions ExclusionSet) ([]batchMessage, [][]scanners.Finding) {
	if len(session) == 0 {
		return ids, findings
	}
	index := make(map[uuid.UUID]int, len(ids))
	for i, msg := range ids {
		if !msg.ContentPart {
			index[msg.ID] = i
		}
	}
	for _, sf := range session {
		kept := exclusions.FilterFindings(sf.findings)
		if len(kept) == 0 {
			continue
		}
		if i, ok := index[sf.messageID]; ok {
			findings[i] = append(findings[i], kept...)
		} else {
			ids = append(ids, batchMessage{
				ID:           sf.messageID,
				ContentPart:  false,
				Type:         "",
				Content:      "",
				RawToolCalls: nil,
				ToolCalls:    nil,
				UserID:       "",
				CreatedAt:    time.Time{},
				Source:       "",
			})
			findings = append(findings, kept)
		}
	}
	return ids, findings
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
	return messages, nil
}

const maxConcurrentContentPartAssetReads = 32
const maxContentPartAssetReadSize = 20 * 1024 * 1024 // 20 MiB

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
	content, err := blobio.ReadAllString(ctx, a.assetStorage, rawURL, maxContentPartAssetReadSize)
	if err != nil {
		return "", fmt.Errorf("read content part asset for %s: %w", contentPartID, err)
	}
	return content, nil
}
