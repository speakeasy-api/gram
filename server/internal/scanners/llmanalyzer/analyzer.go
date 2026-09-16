package llmanalyzer

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/stokens"
)

// Dead-letter reasons written on the sentinel finding when the model could
// not be consulted. They are short classifications meant for metrics and the
// user-facing "risk analysis unavailable" copy, never upstream error text.
const (
	ReasonDisabled        = "disabled"
	ReasonTimeout         = "timeout"
	ReasonRateLimited     = "rate_limited"
	ReasonUpstream5xx     = "upstream_5xx"
	ReasonUpstream4xx     = "upstream_4xx"
	ReasonEmptyCompletion = "empty_completion"
	ReasonParseError      = "parse_error"
	ReasonRequestError    = "request_error"
)

// ErrDisabled is the Analysis.Err of an analyzer constructed without a
// completer: the deployment has no risk model URL configured.
var ErrDisabled = errors.New("risk llm: analyzer disabled")

// Completer is the model call the analyzer depends on. *Client satisfies it;
// tests substitute a StubCompleter.
type Completer interface {
	// Complete sends the chat turns to the model and returns its reply.
	Complete(ctx context.Context, info CallInfo, messages []Message) (Completion, error)

	// RecordParseFailure records that a completion could not be parsed as a
	// verdict.
	RecordParseFailure(ctx context.Context, info CallInfo)

	// ModelName is the served model name Complete requests. It keys the
	// verdict cache so a model swap never serves its predecessor's verdicts.
	ModelName() string
}

// Analyzer turns one message into analyzer findings: it renders the training
// prompt, calls the model, parses the verdict and maps each positive score to
// a finding. Model failures never surface as Go errors to the lanes; they
// become a dead-letter result the lanes fail closed on.
type Analyzer struct {
	logger        *slog.Logger
	tracer        trace.Tracer
	meterProvider metric.MeterProvider
	metrics       *analyzerMetrics
	completer     Completer
	cache         VerdictCache
	stokens       *stokens.Codec
}

// AnalyzerOption customizes an Analyzer.
type AnalyzerOption func(*Analyzer)

// WithVerdictCache stores model replies in cache and answers repeated prompts
// from it without calling the model. Without it every Analyze calls the
// model.
func WithVerdictCache(cache VerdictCache) AnalyzerOption {
	return func(a *Analyzer) {
		if cache != nil {
			a.cache = cache
		}
	}
}

// WithMeterProvider records the analyzer's own metrics (the verdict cache
// lookups) on meterProvider. Without it they go to a noop provider.
func WithMeterProvider(meterProvider metric.MeterProvider) AnalyzerOption {
	return func(a *Analyzer) {
		if meterProvider != nil {
			a.meterProvider = meterProvider
		}
	}
}

// NewAnalyzer builds an analyzer over completer. A nil completer yields a
// disabled analyzer whose Analyze always returns a dead-letter result, so the
// lanes need no nil checks of their own.
func NewAnalyzer(logger *slog.Logger, tracerProvider trace.TracerProvider, completer Completer, opts ...AnalyzerOption) *Analyzer {
	logger = logger.With(attr.SlogComponent("risk-llm-analyzer"))
	a := &Analyzer{
		logger:        logger,
		tracer:        tracerProvider.Tracer(tracerName),
		meterProvider: noop.NewMeterProvider(),
		metrics:       nil,
		completer:     completer,
		cache:         NoopVerdictCache{},
		stokens:       stokens.NewCodec(),
	}
	for _, opt := range opts {
		opt(a)
	}
	a.metrics = newAnalyzerMetrics(a.meterProvider, logger)
	return a
}

// Enabled reports whether a model is wired in. Disabled analyzers reply
// dead-letter on the sync lane and publish nothing on the async lane.
func (a *Analyzer) Enabled() bool {
	return a.completer != nil
}

// Request is one message to analyze together with its attribution.
type Request struct {
	// OrgID is the organization the message belongs to.
	OrgID string

	// OrgSlug is the organization's slug, for dashboards that slice by name.
	OrgSlug string

	// ProjectID is the project the message belongs to.
	ProjectID string

	// ScanMode is ScanModeSync for realtime enforcement and ScanModeAsync
	// for batch scans.
	ScanMode string

	// Message is the message under evaluation.
	Message judgemessage.Message

	// ToolCallIDs optionally carries the real tool call ids, aligned index for
	// index with Message.ToolCalls (or a single id for a Message rendered
	// from ToolName). When absent or misaligned the ids are synthesized.
	ToolCallIDs []string
}

// Analysis is the outcome of one Analyze call.
type Analysis struct {
	// Result carries the findings. On failure it is a dead-letter result.
	Result scanners.Result

	// Verdict is the parsed model reply. Zero on failure.
	Verdict Verdict

	// Completion is the raw model reply, populated whenever the model was
	// reached. Attempts and Model are set even when the call failed.
	Completion Completion

	// Truncated reports whether the rendered prompt dropped part of the
	// message to stay within the content caps.
	Truncated bool

	// Cached reports whether the verdict came from the verdict cache instead
	// of a model call. Completion then carries zero tokens and attempts.
	Cached bool

	// Err is the typed failure behind a dead-letter result: ErrDisabled,
	// ErrTimeout, ErrEmptyCompletion, *UpstreamError, an error wrapping
	// ErrParse, or a transport error. Nil on success.
	Err error
}

// Analyze evaluates the message and maps the verdict to findings. It never
// returns a Go error: any failure yields Analysis.Result as DeadLetterResult
// with the reason classified from Analysis.Err. Result.Completed is true only
// when a verdict parsed, and Result.STokens counts the rendered user prompt
// whether the verdict came from the model or the cache: metering measures the
// content scanned, not the model spend.
func (a *Analyzer) Analyze(ctx context.Context, req Request) Analysis {
	ctx, span := a.tracer.Start(ctx, "risk.llm.analyze", trace.WithAttributes(
		attr.OrganizationID(req.OrgID),
		attr.OrganizationSlug(req.OrgSlug),
		attr.ProjectID(req.ProjectID),
		attr.RiskScanMode(req.ScanMode),
	))
	defer span.End()

	info := CallInfo{OrgID: req.OrgID, OrgSlug: req.OrgSlug, ScanMode: req.ScanMode}

	if a.completer == nil {
		return a.fail(ctx, span, info, Completion{
			Content:          "",
			PromptTokens:     0,
			CompletionTokens: 0,
			Model:            "",
			Attempts:         0,
		}, false, ErrDisabled)
	}

	// The training set stripped surrounding whitespace from every content
	// block, so trim before the caps apply.
	req.Message.Body = strings.TrimSpace(req.Message.Body)
	in, truncated := PromptInputFromJudgeMessage(req.Message)
	applyToolCallIDs(in.ToolCalls, req.Message, req.ToolCallIDs)
	span.SetAttributes(attribute.Bool("gram.risk.llm.truncated", truncated))

	messages := BuildMessages(in)
	userPrompt := messages[len(messages)-1].Content
	cacheKey := VerdictCacheKey(a.completer.ModelName(), messages[0].Content, userPrompt)

	completion, verdict, cached := a.lookupVerdict(ctx, info, cacheKey)
	span.SetAttributes(attribute.Bool("gram.risk.llm.cached", cached))
	if !cached {
		var err error
		completion, err = a.completer.Complete(ctx, info, messages)
		span.SetAttributes(attr.RiskLLMModel(completion.Model))
		if err != nil {
			return a.fail(ctx, span, info, completion, truncated, err)
		}

		verdict, err = ParseVerdict(completion.Content)
		if err != nil {
			a.completer.RecordParseFailure(ctx, info)
			return a.fail(ctx, span, info, completion, truncated, err)
		}

		// Only a parsed verdict is worth replaying; failures always retry
		// the model.
		if err := a.cache.Set(ctx, cacheKey, CachedVerdict{
			Raw:              completion.Content,
			Model:            completion.Model,
			PromptTokens:     completion.PromptTokens,
			CompletionTokens: completion.CompletionTokens,
		}); err != nil {
			a.logger.WarnContext(ctx, "risk llm verdict cache write failed",
				attr.SlogError(err),
				attr.SlogOrganizationID(info.OrgID),
				attr.SlogRiskLane(info.Lane),
			)
		}
	} else {
		span.SetAttributes(attr.RiskLLMModel(completion.Model))
	}

	stokenCount, countErr := a.stokens.Count(ctx, userPrompt)
	if countErr != nil {
		a.logger.WarnContext(ctx, "risk llm stoken count failed",
			attr.SlogError(countErr),
			attr.SlogOrganizationID(req.OrgID),
			attr.SlogRiskScanMode(req.ScanMode),
		)
	}

	flagged := verdict.Flagged()
	findings := make([]scanners.Finding, 0, len(flagged))
	for _, key := range flagged {
		findings = append(findings, NewFinding(key, verdict.Risks[key].Reasoning))
	}
	span.SetAttributes(attribute.Int("gram.risk.llm.flagged_count", len(flagged)))

	return Analysis{
		Result: scanners.Result{
			Findings:  findings,
			STokens:   int64(stokenCount),
			Completed: countErr == nil,
		},
		Verdict:    verdict,
		Completion: completion,
		Truncated:  truncated,
		Cached:     cached,
		Err:        nil,
	}
}

// lookupVerdict consults the verdict cache. A hit yields the cached reply as
// a zero-token, zero-attempt completion together with its parsed verdict.
// Cache errors and unparsable entries are logged, counted as errors and then
// treated as misses, so the cache can only ever save a model call, never
// fail an analysis.
func (a *Analyzer) lookupVerdict(ctx context.Context, info CallInfo, key string) (Completion, Verdict, bool) {
	none := Completion{Content: "", PromptTokens: 0, CompletionTokens: 0, Model: "", Attempts: 0}

	hit, ok, err := a.cache.Get(ctx, key)
	if err != nil {
		a.logger.WarnContext(ctx, "risk llm verdict cache read failed; calling the model",
			attr.SlogError(err),
			attr.SlogOrganizationID(info.OrgID),
			attr.SlogRiskLane(info.Lane),
		)
		a.metrics.RecordCacheLookup(ctx, info, CacheResultError)
		return none, Verdict{Risks: nil, Raw: ""}, false
	}
	if !ok {
		a.metrics.RecordCacheLookup(ctx, info, CacheResultMiss)
		return none, Verdict{Risks: nil, Raw: ""}, false
	}

	verdict, err := ParseVerdict(hit.Raw)
	if err != nil {
		a.logger.WarnContext(ctx, "risk llm verdict cache entry unparsable; calling the model",
			attr.SlogError(err),
			attr.SlogOrganizationID(info.OrgID),
			attr.SlogRiskLane(info.Lane),
		)
		a.metrics.RecordCacheLookup(ctx, info, CacheResultError)
		return none, Verdict{Risks: nil, Raw: ""}, false
	}

	a.metrics.RecordCacheLookup(ctx, info, CacheResultHit)
	return Completion{
		Content:          hit.Raw,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            hit.Model,
		Attempts:         0,
	}, verdict, true
}

func (a *Analyzer) fail(ctx context.Context, span trace.Span, info CallInfo, completion Completion, truncated bool, err error) Analysis {
	reason := DeadLetterReason(err)
	span.RecordError(err)
	span.SetStatus(codes.Error, "risk llm analysis failed")
	span.SetAttributes(attribute.String("gram.risk.llm.dead_letter_reason", reason))
	a.logger.WarnContext(ctx, "risk llm analysis failed; returning dead-letter result",
		attr.SlogError(err),
		attr.SlogOrganizationID(info.OrgID),
		attr.SlogRiskScanMode(info.ScanMode),
	)
	return Analysis{
		Result:     DeadLetterResult(reason),
		Verdict:    Verdict{Risks: nil, Raw: ""},
		Completion: completion,
		Truncated:  truncated,
		Cached:     false,
		Err:        err,
	}
}

// applyToolCallIDs overwrites the synthetic ids on rendered with the real ids
// when they line up with the source message: one id per Message.ToolCalls
// entry, capped head and tail exactly like the rendered calls, or exactly
// one id for a message rendered from ToolName. Empty ids keep their
// synthetic value.
func applyToolCallIDs(rendered []ToolCall, m judgemessage.Message, ids []string) {
	if len(ids) == 0 || len(rendered) == 0 {
		return
	}
	switch {
	case len(m.ToolCalls) > 0:
		if len(ids) != len(m.ToolCalls) {
			return
		}
		ids, _ = capHeadTail(ids)
		if len(rendered) != len(ids) {
			return
		}
	case len(rendered) != 1 || len(ids) != 1:
		return
	}
	for i := range rendered {
		if ids[i] != "" {
			rendered[i].ID = ids[i]
		}
	}
}

// DeadLetterReason classifies an analyzer failure into the short reason
// written on the dead-letter sentinel.
func DeadLetterReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrDisabled):
		return ReasonDisabled
	case errors.Is(err, ErrTimeout):
		return ReasonTimeout
	case errors.Is(err, ErrEmptyCompletion):
		return ReasonEmptyCompletion
	case errors.Is(err, ErrParse):
		return ReasonParseError
	}
	if upstream, ok := errors.AsType[*UpstreamError](err); ok {
		switch {
		case upstream.Status == http.StatusTooManyRequests:
			return ReasonRateLimited
		case upstream.Status >= http.StatusInternalServerError:
			return ReasonUpstream5xx
		default:
			return ReasonUpstream4xx
		}
	}
	return ReasonRequestError
}

// NewFinding builds the finding for a positive score on key. The reasoning
// becomes the description (ParseVerdict already caps it at 500 runes), the
// match stays empty because the model reports no offsets, and confidence is
// 1 because the score is binary.
func NewFinding(key, reasoning string) scanners.Finding {
	return scanners.Finding{
		RuleID:              RuleIDForKey(key),
		Description:         reasoning,
		Match:               "",
		StartPos:            0,
		EndPos:              0,
		Tags:                []string{CategoryForKey(key)},
		Source:              Source,
		Confidence:          1,
		DeadLetterReason:    "",
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	}
}

// DeadLetterResult is the result the lanes fail closed on when the model
// could not be consulted: a single sentinel finding carrying the reason,
// with Completed false so it is never metered.
func DeadLetterResult(reason string) scanners.Result {
	return scanners.Result{
		Findings: []scanners.Finding{{
			RuleID:              RuleDeadLetter,
			Description:         "Risk analysis unavailable: " + reason,
			Match:               "",
			StartPos:            0,
			EndPos:              0,
			Tags:                []string{},
			Source:              Source,
			Confidence:          0,
			DeadLetterReason:    reason,
			McpLookupToolCallID: "",
			SpanGroupKey:        "",
			Field:               "",
			Path:                "",
		}},
		STokens:   0,
		Completed: false,
	}
}

// IsDeadLetter reports whether f is the analyzer's dead-letter sentinel.
func IsDeadLetter(f scanners.Finding) bool {
	return f.Source == Source && f.RuleID == RuleDeadLetter
}

// FindingsForSources fans the analyzer's findings out to a policy with the
// given sources. A finding is kept when its risk key covers one of the
// sources, and it is re-labelled for that source: the rule id becomes the
// source's own id (RuleIDForSource) and the category tag follows, so a
// destructive_tool_call verdict yields destructive_tool.llm for a
// destructive_tool policy and cli_destructive.llm for a cli_destructive
// policy. When several requested sources share a key the finding is emitted
// once per source. A dead-letter sentinel is kept unchanged whenever at least
// one source is covered, so a degraded lane still blocks every covered
// policy; policies with no covered source get nothing. Order follows the
// input findings, then the order of sources.
func FindingsForSources(findings []scanners.Finding, sources []string) []scanners.Finding {
	type target struct {
		source string
		key    string
	}
	targets := make([]target, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if _, dup := seen[source]; dup {
			continue
		}
		if key, ok := RiskKeyForSource(source); ok {
			seen[source] = struct{}{}
			targets = append(targets, target{source: source, key: key})
		}
	}

	kept := make([]scanners.Finding, 0, len(findings))
	if len(targets) == 0 {
		return kept
	}
	for _, f := range findings {
		if IsDeadLetter(f) {
			kept = append(kept, f)
			continue
		}
		if f.Source != Source {
			continue
		}
		for _, t := range targets {
			if f.RuleID != RuleIDForKey(t.key) {
				continue
			}
			relabelled := f
			relabelled.RuleID, _ = RuleIDForSource(t.source)
			relabelled.Tags = []string{CategoryForSource(t.source)}
			kept = append(kept, relabelled)
		}
	}
	return kept
}

// FindingsForSource is FindingsForSources for a single policy source.
func FindingsForSource(findings []scanners.Finding, source string) []scanners.Finding {
	return FindingsForSources(findings, []string{source})
}

// CategoryForSource returns the category name a covered policy source's
// findings are tagged with, or an empty string for an uncovered source.
func CategoryForSource(source string) string {
	switch source {
	case "gitleaks":
		return string(categories.CategorySecrets)
	case "presidio":
		return string(categories.CategoryPII)
	case "prompt_injection":
		return string(categories.CategoryPromptInjection)
	case "destructive_tool":
		return string(categories.CategoryDestructiveTool)
	case "cli_destructive":
		return string(categories.CategoryCLIDestructive)
	default:
		return ""
	}
}

// CategoryForKey returns the category name tagged on findings for a model
// risk key, or an empty string for an unknown key.
func CategoryForKey(key string) string {
	switch key {
	case KeySecretsLeak:
		return string(categories.CategorySecrets)
	case KeyPersonalDataLeak:
		return string(categories.CategoryPII)
	case KeyPromptInjection:
		return string(categories.CategoryPromptInjection)
	case KeyDestructiveToolCall:
		return string(categories.CategoryDestructiveTool)
	default:
		return ""
	}
}
