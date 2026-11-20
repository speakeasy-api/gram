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
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

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
	chatWriter *chat.ChatMessageWriter
	completion openrouter.CompletionClient
	jev        *intentpolicy.JevEvaluator
	guardrail  intentpolicy.Guardrail
	engine     *intentpolicy.Engine
	now        func() time.Time
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

// NewService builds the service. jevClassifier should be pinned to
// intentpolicy.JevModel, or classifier.Noop when no key is configured.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	chatWriter *chat.ChatMessageWriter,
	completion openrouter.CompletionClient,
	jevClassifier classifier.Classifier,
) (*Service, error) {
	logger = logger.With(attr.SlogComponent("intentpolicies.api"))
	guardrail := intentpolicy.DefaultGuardrail()
	engine, err := intentpolicy.NewEngine(guardrail)
	if err != nil {
		return nil, fmt.Errorf("compile intent guardrail: %w", err)
	}
	return &Service{
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/intentpolicies"),
		logger:     logger,
		db:         db,
		auth:       auth.New(logger, db, sessions, authzEngine),
		chatWriter: chatWriter,
		completion: completion,
		jev:        intentpolicy.NewJevEvaluator(jevClassifier),
		guardrail:  guardrail,
		engine:     engine,
		now:        time.Now,
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

func (s *Service) requireStaff(ctx context.Context) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, logger, err := auth.RequirePlatformAdmin(ctx, s.logger)
	if err != nil {
		return nil, logger, err
	}
	if authCtx.ProjectID == nil {
		return nil, logger, oops.C(oops.CodeUnauthorized)
	}
	return authCtx, logger.With(attr.SlogProjectID(authCtx.ProjectID.String())), nil
}

// GetGuardrail returns the built-in guardrail.
func (s *Service) GetGuardrail(ctx context.Context, _ *gen.GetGuardrailPayload) (*gen.IntentGuardrail, error) {
	if _, _, err := s.requireStaff(ctx); err != nil {
		return nil, err
	}
	return buildGuardrailView(s.guardrail), nil
}

// AnalyzeSession labels and decides every tool call of one session.
func (s *Service) AnalyzeSession(ctx context.Context, payload *gen.AnalyzeSessionPayload) (*gen.IntentSessionAnalysis, error) {
	authCtx, logger, err := s.requireStaff(ctx)
	if err != nil {
		return nil, err
	}
	projectID := *authCtx.ProjectID

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
	rows, err := q.ListChatMessagesByGeneration(ctx, chatrepo.ListChatMessagesByGenerationParams{ChatID: chatID, ProjectID: projectID, Generation: *generation})
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

	analysis, err := intentpolicy.Analyze(ctx, s.guardrail, s.engine, evaluator, actions)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "analyze session").LogError(ctx, logger)
	}
	return buildAnalysisView(chatID, *generation, analysis), nil
}

// UploadTranscript stores a transcript as a chat in the caller's project.
func (s *Service) UploadTranscript(ctx context.Context, payload *gen.UploadTranscriptPayload) (*gen.UploadTranscriptResult, error) {
	authCtx, logger, err := s.requireStaff(ctx)
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

	chatID, err := intentpolicy.StoreTranscript(ctx, s.db, s.chatWriter, intentpolicy.StoreParams{
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
		cs = append(cs, &gen.IntentContract{ID: c.ID, Name: c.Name, Expression: c.Expression})
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
