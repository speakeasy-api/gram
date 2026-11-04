package authz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ShadowOutcome is how the registry evaluator's answer compared with the
// engine's for one client call. Only match counts as parity. new_allow,
// new_deny and new_error are disagreements to investigate; incomparable,
// skipped and failed comparisons prove nothing either way and are counted
// apart from them.
type ShadowOutcome string

const (
	// ShadowMatch: both evaluators gave the same answer, or failed with the
	// same error category.
	ShadowMatch ShadowOutcome = "match"
	// ShadowNewAllow: the registry evaluator allowed something the engine
	// denied or could not decide. For batch matches, any such entry makes
	// the call new_allow.
	ShadowNewAllow ShadowOutcome = "new_allow"
	// ShadowNewDeny: the registry evaluator denied something the engine
	// allowed.
	ShadowNewDeny ShadowOutcome = "new_deny"
	// ShadowNewError: the error category changed. The registry evaluator
	// could not decide a request the engine decided, failed in another
	// category than the engine, or denied a request the engine could not
	// decide. Detail names the categories.
	ShadowNewError ShadowOutcome = "new_error"
	// ShadowIncomparable: the registry evaluator lacked facts equivalent to
	// what the engine read, so the two did not answer the same question.
	ShadowIncomparable ShadowOutcome = "incomparable"
	// ShadowSkipped: the comparison budget was exhausted and the registry
	// evaluator did not run.
	ShadowSkipped ShadowOutcome = "skipped"
	// ShadowFailed: the comparison did not finish, because it timed out or
	// the request was cancelled.
	ShadowFailed ShadowOutcome = "failed"
)

// Disagrees reports whether the outcome is a disagreement to investigate.
func (o ShadowOutcome) Disagrees() bool {
	return o == ShadowNewAllow || o == ShadowNewDeny || o == ShadowNewError
}

// ShadowComparison is one redacted comparison record. It names permissions
// and units but never resource IDs, principals or grants.
type ShadowComparison struct {
	OrganizationID string
	// Units are the migration units the call touched.
	Units []string
	// Entry is the client entry point: require, require_member, evaluate,
	// evaluate_grants, match_many or audience.
	Entry string
	// Permissions are the typed permissions the call asked for, such as
	// project.read, in call order.
	Permissions []string
	Outcome     ShadowOutcome
	// Detail says why a comparison was incomparable, skipped or failed, or
	// classifies a new_error, and is empty otherwise.
	Detail string
	// Duration is how long the registry evaluator ran, or how long the
	// comparison waited before it gave up. For an audience it is the sum of
	// the registry evaluator's per-member decisions, without the grant reads.
	Duration time.Duration
	// BuildVersion is the server build that ran the comparison.
	BuildVersion string
	// ModelVersion is the compiled registry's fingerprint.
	ModelVersion string
}

// ShadowRecorder receives every comparison the client runs. It is called on
// the request path after the comparison's wait, so an implementation must not
// block: it should only log, count, or hand the record to a buffered sink.
type ShadowRecorder interface {
	RecordShadowComparison(ctx context.Context, comparison ShadowComparison)
}

// ShadowBudget bounds the cost of shadow comparisons. A comparison waits at
// most Timeout for the registry evaluator, and at most MaxConcurrent
// comparisons run at once; a call beyond that skips its comparison. Neither
// limit ever changes a response: the engine has already decided.
type ShadowBudget struct {
	MaxConcurrent int
	Timeout       time.Duration
}

// DefaultShadowBudget suits in-process evaluation, which reads only the
// prepared policies and supplied facts and does no I/O.
var DefaultShadowBudget = ShadowBudget{MaxConcurrent: 64, Timeout: 50 * time.Millisecond}

// shadowAnswer is one evaluator's answer to a call: one result for require
// and evaluate, one per operation for match_many.
type shadowAnswer struct {
	allowed []bool
	err     error
}

type shadowComparer struct {
	budget       ShadowBudget
	slots        chan struct{}
	recorder     ShadowRecorder
	buildVersion string
	modelVersion string
}

func newShadowComparer(budget ShadowBudget, recorder ShadowRecorder, buildVersion, modelVersion string) (*shadowComparer, error) {
	if budget.MaxConcurrent <= 0 || budget.Timeout <= 0 {
		return nil, fmt.Errorf("shadow budget needs a positive concurrency and timeout, got %d and %s", budget.MaxConcurrent, budget.Timeout)
	}
	if recorder == nil {
		return nil, errors.New("shadow comparisons require a recorder")
	}
	return &shadowComparer{
		budget:       budget,
		slots:        make(chan struct{}, budget.MaxConcurrent),
		recorder:     recorder,
		buildVersion: buildVersion,
		modelVersion: modelVersion,
	}, nil
}

// compare runs evaluate, the registry evaluator's answer, beside the engine's
// answer old and records how they compare. It returns once the comparison is
// recorded, after at most the budget's timeout. Nothing it does reaches the
// caller's response.
func (s *shadowComparer) compare(ctx context.Context, entry string, rt route, ops []Operation, old shadowAnswer, evaluate func() shadowAnswer) {
	organizationID := rt.organizationID
	if organizationID == "" {
		organizationID = requestOrganizationID(ctx)
	}
	record := ShadowComparison{
		OrganizationID: organizationID,
		Units:          rt.units,
		Entry:          entry,
		Permissions:    operationPermissions(ops),
		Outcome:        ShadowSkipped,
		Detail:         "",
		Duration:       0,
		BuildVersion:   s.buildVersion,
		ModelVersion:   s.modelVersion,
	}
	if ctx.Err() != nil {
		record.Outcome, record.Detail = ShadowFailed, "canceled"
		s.recorder.RecordShadowComparison(ctx, record)
		return
	}
	select {
	case s.slots <- struct{}{}:
	default:
		record.Outcome, record.Detail = ShadowSkipped, "concurrency_budget"
		s.recorder.RecordShadowComparison(ctx, record)
		return
	}

	done := make(chan shadowAnswer, 1)
	start := time.Now()
	go func() {
		// The slot is released when the evaluator finishes, not when the
		// comparison gives up, so slow evaluations cannot pile up.
		defer func() { <-s.slots }()
		defer func() {
			if r := recover(); r != nil {
				done <- shadowAnswer{allowed: nil, err: fmt.Errorf("%w: registry evaluator panicked: %v", errShadowPanic, r)}
			}
		}()
		done <- evaluate()
	}()

	timer := time.NewTimer(s.budget.Timeout)
	defer timer.Stop()
	select {
	case got := <-done:
		record.Duration = time.Since(start)
		// An answer that lands with or after the deadline does not count as
		// a comparison, whichever select case won.
		switch {
		case record.Duration >= s.budget.Timeout:
			record.Outcome, record.Detail = ShadowFailed, "timeout"
		case ctx.Err() != nil:
			record.Outcome, record.Detail = ShadowFailed, "canceled"
		default:
			record.Outcome, record.Detail = classifyShadow(old, got)
		}
	case <-timer.C:
		record.Duration = time.Since(start)
		record.Outcome, record.Detail = ShadowFailed, "timeout"
	case <-ctx.Done():
		record.Duration = time.Since(start)
		record.Outcome, record.Detail = ShadowFailed, "canceled"
	}
	s.recorder.RecordShadowComparison(ctx, record)
}

// recordAudience records a comparison the caller already ran in full, such
// as an audience resolution that asks both evaluators about each member
// while it reads their grants once. It is not bounded by the budget: the
// registry evaluator does no I/O, and the caller owns the reads.
func (s *shadowComparer) recordAudience(ctx context.Context, organizationID string, rt route, ops []Operation, old, got shadowAnswer, duration time.Duration) {
	outcome, detail := classifyShadow(old, got)
	s.recorder.RecordShadowComparison(ctx, ShadowComparison{
		OrganizationID: organizationID,
		Units:          rt.units,
		Entry:          "audience",
		Permissions:    operationPermissions(ops),
		Outcome:        outcome,
		Detail:         detail,
		Duration:       duration,
		BuildVersion:   s.buildVersion,
		ModelVersion:   s.modelVersion,
	})
}

var errShadowPanic = errors.New("shadow evaluation panicked")

// classifyShadow compares two answers to the same call, and names the error
// categories involved in Detail.
func classifyShadow(old, got shadowAnswer) (ShadowOutcome, string) {
	if got.err != nil && errors.Is(got.err, ErrFactUnresolved) {
		return ShadowIncomparable, "facts_unavailable"
	}
	switch {
	case old.err != nil && got.err != nil:
		oldCategory, newCategory := shadowErrorCategory(old.err), shadowErrorCategory(got.err)
		if oldCategory == newCategory {
			return ShadowMatch, ""
		}
		return ShadowNewError, "engine_" + oldCategory + "_new_" + newCategory
	case old.err != nil:
		if slices.Contains(got.allowed, true) {
			return ShadowNewAllow, "engine_" + shadowErrorCategory(old.err)
		}
		return ShadowNewError, "engine_" + shadowErrorCategory(old.err) + "_new_forbidden"
	case got.err != nil:
		return ShadowNewError, "new_" + shadowErrorCategory(got.err)
	}
	if len(old.allowed) != len(got.allowed) {
		return ShadowNewError, "answer_shape"
	}
	outcome := ShadowMatch
	for i := range old.allowed {
		switch {
		case got.allowed[i] && !old.allowed[i]:
			return ShadowNewAllow, ""
		case !got.allowed[i] && old.allowed[i]:
			outcome = ShadowNewDeny
		}
	}
	return outcome, ""
}

// shadowErrorCategory classifies an error without its message, which can
// name resources. The engine's mapped errors and the evaluator's raw errors
// share categories because mapped errors wrap their cause.
func shadowErrorCategory(err error) string {
	switch {
	case errors.Is(err, errShadowPanic):
		return "panic"
	case errors.Is(err, ErrFactUnresolved):
		return "facts_unavailable"
	case errors.Is(err, ErrMissingGrants):
		return "missing_grants"
	case errors.Is(err, ErrInvalidCheck), errors.Is(err, ErrNoChecks):
		return "invalid_operation"
	case errors.Is(err, ErrUnknownPermission):
		return "unknown_permission"
	case errors.Is(err, ErrMissingRepresentedPolicy):
		return "missing_represented_policy"
	default:
		var shareable *oops.ShareableError
		if errors.As(err, &shareable) {
			return string(shareable.Code)
		}
		return "other"
	}
}

func operationPermissions(ops []Operation) []string {
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = op.permission.Type + "." + op.permission.Permission
	}
	return out
}

const (
	meterShadowComparisons = "gram.authz.shadow.comparisons"
	meterShadowDuration    = "gram.authz.shadow.duration"
)

// telemetryShadowRecorder logs disagreements and counts every comparison by
// unit, entry point, outcome and detail. The counters and the duration
// histogram are the inputs for sizing ShadowBudget: skipped and failed
// comparisons show the budget is too tight, durations show its headroom.
type telemetryShadowRecorder struct {
	logger      *slog.Logger
	comparisons metric.Int64Counter
	duration    metric.Float64Histogram
}

// NewShadowRecorder records comparisons as logs and metrics.
func NewShadowRecorder(logger *slog.Logger, meterProvider metric.MeterProvider) ShadowRecorder {
	logger = logger.With(attr.SlogComponent("authz-shadow"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/authz")
	comparisons, err := meter.Int64Counter(
		meterShadowComparisons,
		metric.WithDescription("Registry evaluator comparisons with the engine, by outcome"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterShadowComparisons), attr.SlogError(err))
	}
	duration, err := meter.Float64Histogram(
		meterShadowDuration,
		metric.WithDescription("Time the registry evaluator took in a shadow comparison"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterShadowDuration), attr.SlogError(err))
	}
	return &telemetryShadowRecorder{logger: logger, comparisons: comparisons, duration: duration}
}

func (r *telemetryShadowRecorder) RecordShadowComparison(ctx context.Context, c ShadowComparison) {
	units := strings.Join(c.Units, ",")
	attrs := metric.WithAttributes(
		attr.AuthzMigrationUnit(units),
		attr.AuthzShadowEntry(c.Entry),
		attr.AuthzShadowOutcome(string(c.Outcome)),
		attr.AuthzShadowDetail(c.Detail),
		attr.AuthzModelVersion(c.ModelVersion),
	)
	if r.comparisons != nil {
		r.comparisons.Add(ctx, 1, attrs)
	}
	if r.duration != nil && c.Duration > 0 {
		r.duration.Record(ctx, float64(c.Duration)/float64(time.Millisecond), attrs)
	}

	fields := []any{
		attr.SlogOrganizationID(c.OrganizationID),
		attr.SlogAuthzMigrationUnit(units),
		attr.SlogAuthzShadowEntry(c.Entry),
		attr.SlogAuthzShadowPermissions(strings.Join(c.Permissions, ",")),
		attr.SlogAuthzShadowOutcome(string(c.Outcome)),
		attr.SlogAuthzShadowDetail(c.Detail),
		attr.SlogServiceVersion(c.BuildVersion),
		attr.SlogAuthzModelVersion(c.ModelVersion),
	}
	switch {
	case c.Outcome.Disagrees():
		r.logger.WarnContext(ctx, "authz shadow disagreement", fields...)
	case c.Outcome == ShadowMatch:
		r.logger.DebugContext(ctx, "authz shadow agreement", fields...)
	default:
		r.logger.InfoContext(ctx, "authz shadow comparison inconclusive", fields...)
	}
}
