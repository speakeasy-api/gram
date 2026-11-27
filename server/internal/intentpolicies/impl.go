// Package intentpolicies implements the staff-only intentPolicies service:
// retroactive intent-guardrail analysis of stored sessions and transcript upload.
//
// There is deliberately no Platform MCP or Admin MCP tool for this service. It
// is a read-only exploratory surface for Speakeasy staff with no customer-facing
// workflow; the decisions it renders are never enforced.
package intentpolicies

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	intentpolicyv1 "github.com/speakeasy-api/gram/infra/gen/gram/intentpolicy/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	srv "github.com/speakeasy-api/gram/server/gen/http/intent_policies/server"
	gen "github.com/speakeasy-api/gram/server/gen/intent_policies"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// Service implements the intentPolicies Goa service.
type Service struct {
	tracer     trace.Tracer
	logger     *slog.Logger
	db         *pgxpool.Pool
	auth       *auth.Auth
	authz      *authz.Engine
	features   feature.Provider
	chatWriter *chat.ChatMessageWriter
	// transcripts reads chat message bodies, opening encrypted shadows for
	// the projects that keep them. Nil reads plaintext.
	transcripts *transcriptstore.Transcripts
	completion  openrouter.CompletionClient
	jev         *intentpolicy.JevEvaluator
	guardrail   intentpolicy.Guardrail
	engine      *intentpolicy.Engine
	results     gcp.Publisher[*intentpolicyv1.Assessment]
	now         func() time.Time
}

// WithTranscripts makes session analysis read message bodies from encrypted
// shadows for the projects that keep them.
func (s *Service) WithTranscripts(transcripts *transcriptstore.Transcripts) *Service {
	s.transcripts = transcripts
	return s
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

// NewService builds the service. jevClassifier should be pinned to
// intentpolicy.JevModel, or classifier.Noop when no key is configured. results
// receives one event per assessed call; nil publishes nothing.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	features feature.Provider,
	chatWriter *chat.ChatMessageWriter,
	completion openrouter.CompletionClient,
	jevClassifier classifier.Classifier,
	results gcp.Publisher[*intentpolicyv1.Assessment],
) (*Service, error) {
	logger = logger.With(attr.SlogComponent("intentpolicies.api"))
	guardrail := intentpolicy.DefaultGuardrail()
	engine, err := intentpolicy.NewEngine(guardrail)
	if err != nil {
		return nil, fmt.Errorf("compile intent guardrail: %w", err)
	}
	return &Service{
		transcripts: nil,
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/intentpolicies"),
		logger:      logger,
		db:          db,
		auth:        auth.New(logger, db, sessions, authzEngine),
		authz:       authzEngine,
		features:    features,
		chatWriter:  chatWriter,
		completion:  completion,
		jev:         intentpolicy.NewJevEvaluator(jevClassifier),
		guardrail:   guardrail,
		engine:      engine,
		results:     results,
		now:         time.Now,
	}, nil
}

// Attach mounts the service endpoints.
func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

// APIKeyAuth validates the session and project security schemes.
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// authorize admits a caller who is in the intent-policies rollout and passes
// check for the active project.
func (s *Service) authorize(ctx context.Context, check func(authz.ProjectID) authz.Check) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	// The rollout targets signed-in people by email, as the dashboard
	// identifies them; API keys and sessions without an email never match.
	email := ""
	if contextvalues.HasValidatedGramSession(ctx) && authCtx.Email != nil {
		email = strings.TrimSpace(*authCtx.Email)
	}
	if email == "" {
		return nil, logger, oops.C(oops.CodeNotFound)
	}
	enabled, err := s.features.IsFlagEnabledLocal(ctx, feature.FlagIntentPolicies, email, feature.OrgProjectGroups(authCtx.OrganizationSlug, ""), map[string]string{"email": strings.ToLower(email)})
	if err != nil {
		logger.WarnContext(ctx, "evaluate intent-policies flag", attr.SlogError(err))
	}
	if !enabled {
		return nil, logger, oops.C(oops.CodeNotFound)
	}

	if err := s.authz.Require(ctx, check(authz.ProjectIDOf(*authCtx.ProjectID))); err != nil {
		return nil, logger, err
	}
	return authCtx, logger, nil
}

// GetGuardrail returns the built-in guardrail.
func (s *Service) GetGuardrail(ctx context.Context, _ *gen.GetGuardrailPayload) (*gen.IntentGuardrail, error) {
	if _, _, err := s.authorize(ctx, func(p authz.ProjectID) authz.Check { return authz.Project(p).Read() }); err != nil {
		return nil, err
	}
	return buildGuardrailView(s.guardrail), nil
}

// AnalyzeSession labels and decides every tool call of one session.
func (s *Service) AnalyzeSession(ctx context.Context, payload *gen.AnalyzeSessionPayload) (*gen.IntentSessionAnalysis, error) {
	authCtx, logger, err := s.authorize(ctx, func(p authz.ProjectID) authz.Check { return authz.Chats(p).Read() })
	if err != nil {
		return nil, err
	}
	projectID := *authCtx.ProjectID
	requestedAt := s.now()

	chatID, err := uuid.Parse(payload.ChatID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid chat_id")
	}

	var evaluator intentpolicy.Evaluator
	switch payload.Evaluator {
	case "jev":
		evaluator = s.jev
	case "gemini":
		evaluator = intentpolicy.NewGeminiEvaluator(s.completion, authCtx.ActiveOrganizationID, projectID.String(), authCtx.UserID)
	default:
		return nil, oops.E(oops.CodeInvalid, nil, "unknown evaluator")
	}

	q := chatrepo.New(s.db)
	chatRow, err := q.GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "chat not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load chat").LogError(ctx, logger)
	}

	generation := payload.Generation
	if generation == nil {
		latest, err := q.GetMaxGenerationForChat(ctx, chatrepo.GetMaxGenerationForChatParams{ChatID: chatID, ProjectID: projectID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load chat generation").LogError(ctx, logger)
		}
		generation = &latest
	}
	rows, err := s.transcripts.ListChatMessagesByGeneration(ctx, s.db, projectID, chatID, *generation)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load chat messages").LogError(ctx, logger)
	}

	messages := make([]intentpolicy.MessageRow, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, intentpolicy.MessageRow{
			ID:         row.ID,
			Seq:        row.Seq,
			Generation: row.Generation,
			Role:       row.Role,
			ToolCalls:  row.ToolCalls,
			CreatedAt:  row.CreatedAt.Time,
		})
	}
	actions, err := intentpolicy.ExtractActions(chatID, conv.FromPGTextOrEmpty[string](chatRow.Cwd), messages)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "parse recorded tool calls").LogError(ctx, logger)
	}

	limit := 0
	if payload.Limit != nil {
		limit = int(*payload.Limit)
	}
	analysis, err := intentpolicy.Analyze(ctx, s.guardrail, s.engine, evaluator, actions, int(payload.Offset), limit)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "analyze session").LogError(ctx, logger)
	}
	s.publishResults(ctx, logger, authCtx, *generation, requestedAt, analysis)
	return buildAnalysisView(chatID, *generation, analysis), nil
}

// publishTimeout bounds how long published results may wait for Pub/Sub.
const publishTimeout = 30 * time.Second

// publishResults publishes one event per assessment without delaying the
// response. Failures are logged and never fail the analysis.
func (s *Service) publishResults(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, generation int32, requestedAt time.Time, analysis intentpolicy.Analysis) {
	if s.results == nil || len(analysis.Assessments) == 0 {
		return
	}
	analysisID, err := uuid.NewV7()
	if err != nil {
		logger.WarnContext(ctx, "generate intent analysis id", attr.SlogError(err))
		return
	}
	rc := intentpolicy.RecordContext{
		AnalysisID:     analysisID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		UserID:         authCtx.UserID,
		UserEmail:      conv.PtrValOr(authCtx.Email, ""),
		Generation:     generation,
		TriggerKind:    intentpolicy.TriggerDashboard,
		RequestedAt:    requestedAt,
		EvaluatedAt:    s.now(),
	}

	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	pending := make([]gcp.PublishResult, 0, len(analysis.Assessments))
	for _, a := range analysis.Assessments {
		pending = append(pending, s.results.Publish(pubCtx, intentpolicy.Record(rc, analysis.Guardrail, a)))
	}
	go func() {
		defer cancel()
		failed, unsettled := 0, 0
		var firstErr error
		for _, p := range pending {
			_, err := p.Get(pubCtx)
			switch {
			case err == nil:
			case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
				// The message may still be delivered; only its outcome is unknown.
				unsettled++
			default:
				failed++
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		if failed > 0 {
			logger.ErrorContext(pubCtx, "publish intent assessments", attr.SlogError(firstErr), attr.SlogValueInt(failed))
		}
		if unsettled > 0 {
			logger.WarnContext(pubCtx, "intent assessment publishes unsettled before timeout", attr.SlogValueInt(unsettled))
		}
	}()
}

// UploadTranscript stores a transcript as a chat in the caller's project.
func (s *Service) UploadTranscript(ctx context.Context, payload *gen.UploadTranscriptPayload) (*gen.UploadTranscriptResult, error) {
	authCtx, logger, err := s.authorize(ctx, func(p authz.ProjectID) authz.Check { return authz.Chats(p).Write() })
	if err != nil {
		return nil, err
	}
	if len(payload.Content) > intentpolicy.MaxTranscriptBytes {
		return nil, oops.E(oops.CodeInvalid, nil, "transcript exceeds 20 MB")
	}

	transcript, err := intentpolicy.ParseTranscript([]byte(payload.Content))
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "unrecognized transcript: expected messages JSON, Claude Code JSONL, or Pi JSONL").LogWarn(ctx, logger)
	}

	chatID, err := intentpolicy.StoreTranscript(ctx, s.chatWriter, intentpolicy.StoreParams{
		ChatID:         uuid.Nil,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
		Title:          payload.Name,
		Source:         intentpolicy.SourceUpload,
	}, transcript, s.now())
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "store transcript").LogError(ctx, logger)
	}
	return &gen.UploadTranscriptResult{ChatID: chatID.String(), Format: string(transcript.Format), MessageCount: len(transcript.Messages)}, nil
}

func buildGuardrailView(g intentpolicy.Guardrail) *gen.IntentGuardrail {
	qs := make([]*gen.IntentLabelQuestion, 0, len(g.Questions))
	for _, q := range g.Questions {
		opts := make([]*gen.IntentLabelOption, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, &gen.IntentLabelOption{Key: o.Key, Description: o.Description, Ordinal: o.Ordinal, Uncertain: o.Uncertain})
		}
		qs = append(qs, &gen.IntentLabelQuestion{Key: q.Key, Kind: string(q.Kind), Instructions: q.Instructions, Options: opts})
	}
	cs := make([]*gen.IntentContract, 0, len(g.Contracts))
	for _, c := range g.Contracts {
		conds := make([]*gen.IntentCondition, 0, len(c.Conditions))
		for _, cond := range c.Conditions {
			conds = append(conds, &gen.IntentCondition{Join: cond.Join, Label: cond.Label, Op: cond.Op, Values: append([]string{}, cond.Values...)})
		}
		cs = append(cs, &gen.IntentContract{ID: c.ID, Name: c.Name, Statement: c.Statement, Outcome: string(c.Outcome), Conditions: conds, Expression: c.Expression})
	}
	return &gen.IntentGuardrail{ID: g.ID, Version: g.Version, Statements: g.Statements, Questions: qs, Contracts: cs}
}

func buildAnalysisView(chatID uuid.UUID, generation int32, a intentpolicy.Analysis) *gen.IntentSessionAnalysis {
	assessments := make([]*gen.IntentCallAssessment, 0, len(a.Assessments))
	for _, as := range a.Assessments {
		labels := make([]*gen.IntentLabelValue, 0, len(as.Labels))
		for _, q := range a.Guardrail.Questions {
			v, ok := as.Labels[q.Key]
			if !ok {
				continue
			}
			labels = append(labels, &gen.IntentLabelValue{
				Key:          q.Key,
				Value:        v.Value,
				Rank:         v.Rank,
				Probability:  v.Probability,
				Distribution: v.Distribution,
				Rationale:    conv.PtrEmpty(v.Rationale),
			})
		}
		outcomes := make([]*gen.IntentContractOutcome, 0, len(as.Outcomes))
		for _, o := range as.Outcomes {
			outcomes = append(outcomes, &gen.IntentContractOutcome{
				ContractID: o.ContractID,
				Result:     conv.PtrEmpty(string(o.Result)),
				Error:      conv.PtrEmpty(o.Error),
				Inputs:     o.Inputs,
			})
		}
		decidedBy := as.DecidedBy
		if decidedBy == nil {
			decidedBy = []string{}
		}
		assessments = append(assessments, &gen.IntentCallAssessment{
			MessageID:      as.Action.MessageID.String(),
			Seq:            as.Action.Seq,
			ToolCallID:     as.Action.ToolCallID,
			ToolName:       as.Action.Tool.Name,
			McpServer:      conv.PtrEmpty(as.Action.Tool.MCPServer),
			McpFunction:    conv.PtrEmpty(as.Action.Tool.MCPFunction),
			Arguments:      rawArguments(as.Action.Arguments),
			OccurredAt:     as.Action.OccurredAt.UTC().Format(time.RFC3339Nano),
			Labels:         labels,
			Decision:       string(as.Decision),
			DecidedBy:      decidedBy,
			Outcomes:       outcomes,
			EvaluatorError: conv.PtrEmpty(as.EvaluatorError),
			Usage: &gen.IntentCallUsage{
				InputTokens:  as.Usage.InputTokens,
				OutputTokens: as.Usage.OutputTokens,
				CostUsd:      as.Usage.CostUSD,
				LatencyMs:    as.Usage.LatencyMs,
			},
		})
	}
	t := a.Totals
	return &gen.IntentSessionAnalysis{
		ChatID:           chatID.String(),
		Generation:       generation,
		GuardrailVersion: a.Guardrail.Version,
		Evaluator: &gen.IntentEvaluatorDescriptor{
			Evaluator:     a.Descriptor.Evaluator,
			Provider:      a.Descriptor.Provider,
			Model:         a.Descriptor.Model,
			PromptVersion: a.Descriptor.PromptVersion,
		},
		Assessments: assessments,
		Totals: &gen.IntentAnalysisTotals{
			Calls:     t.Calls,
			Judged:    t.Judged,
			Blocked:   t.Blocked,
			Clarified: t.Clarified,
			Permitted: t.Permitted,
			CostUsd:   t.CostUSD,
			LatencyMs: t.LatencyMs,
		},
		LimitHit: a.LimitHit,
	}
}

// rawArguments returns the recorded argument string; non-object payloads were
// stored as a JSON string and are unwrapped.
func rawArguments(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
