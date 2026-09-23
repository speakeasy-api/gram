// Package launcher ranks command palette candidates by the intent behind the
// text the user typed. The heavy lifting is delegated to TypeSafe's System
// One ("Jev") judge, reached through OpenRouter with the organization's
// provisioned internal key; this package only frames the questions and maps
// the answers back to the caller's candidate ids.
package launcher

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/launcher/server"
	gen "github.com/speakeasy-api/gram/server/gen/launcher"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
)

// maxCandidates is the hard cap on candidates per judgement. The Goa design
// enforces the same limit at decode time; the handler guards it again so a
// caller bypassing HTTP validation cannot inflate the outbound request.
const maxCandidates = 32

// unsetKeyPlaceholder is the value mise.toml declares for a dev key that has
// not been filled in. It is treated the same as no key at all.
const unsetKeyPlaceholder = "unset"

const meterJudgeOutcome = "gram.launcher.judge"

// keyProvisioner resolves the OpenRouter key that pays for an organization's
// judgements. openrouter.Provisioner satisfies it; the narrow interface keeps
// tests free of the full provisioning surface.
type keyProvisioner interface {
	ProvisionAPIKey(ctx context.Context, orgID string, keyType openrouter.KeyType) (string, error)
}

// Service implements the launcher management service.
type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	auth        *auth.Auth
	provisioner keyProvisioner
	client      *typesafe.Client
	metrics     *judgeMetrics
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

// NewService wires the launcher service. provisioner supplies each
// organization's internal OpenRouter key per request; client is the always
// constructed System One client that key is handed to.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	provisioner openrouter.Provisioner,
	client *typesafe.Client,
) *Service {
	logger = logger.With(attr.SlogComponent("launcher"))
	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/launcher"),
		logger:      logger,
		db:          db,
		auth:        auth.New(logger, db, sessions, authzEngine),
		provisioner: provisioner,
		client:      client,
		metrics:     newMetrics(logger, meterProvider),
	}
}

// Attach mounts the service's HTTP endpoints on mux.
func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

// APIKeyAuth implements the session and project-slug security schemes.
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// Judge asks the intent service which candidate the query refers to, what
// action it asks for and whether it is settled enough to act on. When the
// organization has no usable OpenRouter key the judgment is returned with
// Disabled set and no outbound call is made.
func (s *Service) Judge(ctx context.Context, payload *gen.JudgePayload) (*gen.LauncherJudgment, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if len(payload.Candidates) > maxCandidates {
		return nil, oops.E(oops.CodeBadRequest, nil, "at most %d candidates can be judged at once", maxCandidates)
	}

	apiKey, err := s.provisioner.ProvisionAPIKey(ctx, authCtx.ActiveOrganizationID, openrouter.KeyTypeInternal)
	if err != nil {
		s.logger.DebugContext(ctx, "launcher judge disabled: openrouter key unavailable", attr.SlogError(err))
		s.metrics.record(ctx, outcomeDisabled)
		return disabledJudgment(), nil
	}
	if apiKey == "" || apiKey == unsetKeyPlaceholder {
		s.metrics.record(ctx, outcomeDisabled)
		return disabledJudgment(), nil
	}

	cands := make([]Candidate, 0, len(payload.Candidates))
	for _, c := range payload.Candidates {
		cands = append(cands, Candidate{
			ID:     c.ID,
			Kind:   c.Kind,
			Title:  c.Title,
			Detail: conv.PtrValOr(c.Detail, ""),
			Verbs:  c.Verbs,
		})
	}
	route := ""
	if payload.Context != nil {
		route = conv.PtrValOr(payload.Context.Route, "")
	}

	req, callerIDs, err := BuildRequest(payload.Query, route, cands)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to build intent request").LogError(ctx, s.logger)
	}

	result, err := s.client.Ask(ctx, apiKey, req)
	if err != nil {
		s.metrics.record(ctx, o11y.OutcomeFromErrorWithTimeout(err))
		return nil, oops.E(oops.CodeGatewayError, err, "intent service unavailable").LogWarn(ctx, s.logger)
	}
	s.metrics.record(ctx, o11y.OutcomeSuccess)

	return mapJudgment(result, callerIDs), nil
}

func disabledJudgment() *gen.LauncherJudgment {
	return &gen.LauncherJudgment{
		Disabled:  true,
		Target:    nil,
		Action:    nil,
		Ready:     nil,
		LatencyMs: nil,
	}
}

// mapJudgment re-keys the target answer from the index ids c0..cN to the
// caller's ids, fills in the fallbacks for missing action and ready answers,
// and records the round trip latency.
func mapJudgment(result *typesafe.Result, callerIDs []string) *gen.LauncherJudgment {
	answers := result.Response.Answers

	target := make(map[string]float64, len(callerIDs)+1)
	for option, p := range answers[questionTarget].Probabilities {
		if option == targetNone {
			target[targetNone] = p
			continue
		}
		idx, ok := strings.CutPrefix(option, "c")
		if !ok {
			continue
		}
		i, err := strconv.Atoi(idx)
		if err != nil || i < 0 || i >= len(callerIDs) {
			continue
		}
		target[callerIDs[i]] = p
	}

	action := make(map[string]float64, len(actionVerbs))
	if probs := answers[questionAction].Probabilities; len(probs) > 0 {
		for _, verb := range actionVerbs {
			action[verb] = probs[verb]
		}
	} else {
		action[actionUnclear] = 1
	}

	ready := 0.0
	if noul := answers[questionReady].Noul; noul != nil {
		ready = *noul
	}

	latency := result.Latency.Milliseconds()

	return &gen.LauncherJudgment{
		Disabled:  false,
		Target:    target,
		Action:    action,
		Ready:     &ready,
		LatencyMs: &latency,
	}
}

// outcomeDisabled is recorded when a judgement is skipped for lack of a key.
const outcomeDisabled o11y.Outcome = "disabled"

// judgeMetrics holds the service's OpenTelemetry instruments. The counter is
// nil-guarded so a construction failure degrades to no metrics.
type judgeMetrics struct {
	outcome metric.Int64Counter
}

func newMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *judgeMetrics {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/launcher")
	outcome, err := meter.Int64Counter(
		meterJudgeOutcome,
		metric.WithDescription("Command palette intent judgements by outcome (success, failure, timeout, canceled or disabled)."),
		metric.WithUnit("{judgement}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterJudgeOutcome), attr.SlogError(err))
	}
	return &judgeMetrics{outcome: outcome}
}

func (m *judgeMetrics) record(ctx context.Context, outcome o11y.Outcome) {
	if m.outcome == nil {
		return
	}
	m.outcome.Add(ctx, 1, metric.WithAttributes(attr.Outcome(outcome)))
}
