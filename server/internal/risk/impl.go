package risk

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/risk/server"
	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

// RiskAnalysisSignaler starts or signals the drain workflow for a risk policy.
type RiskAnalysisSignaler interface {
	SignalNewMessages(ctx context.Context, params background.DrainRiskAnalysisParams) error
}

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	repo     *repo.Queries
	auth     *auth.Auth
	access   *access.Manager
	signaler RiskAnalysisSignaler
}

var _ chat.MessageObserver = (*Service)(nil)

// NewObserver creates a lightweight chat.MessageObserver that signals the risk
// drain workflow when new messages are stored. Use this in contexts (e.g. the
// worker process) where the full risk Service is not needed.
func NewObserver(logger *slog.Logger, db *pgxpool.Pool, signaler RiskAnalysisSignaler) chat.MessageObserver {
	return &Service{
		tracer:   tracenoop.NewTracerProvider().Tracer(""),
		logger:   logger.With(attr.SlogComponent("risk")),
		db:       db,
		repo:     repo.New(db),
		auth:     nil,
		access:   nil,
		signaler: signaler,
	}
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	accessManager *access.Manager,
	signaler RiskAnalysisSignaler,
) *Service {
	logger = logger.With(attr.SlogComponent("risk"))

	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/risk"),
		logger:   logger,
		db:       db,
		repo:     repo.New(db),
		auth:     auth.New(logger, db, sessions, accessManager),
		access:   accessManager,
		signaler: signaler,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// OnMessagesStored implements chat.MessageObserver. The caller
// (notifyObservers) already dispatches this in a goroutine with a
// detached context, so this method can safely perform I/O.
func (s *Service) OnMessagesStored(ctx context.Context, projectID uuid.UUID) {
	policies, err := s.repo.ListEnabledRiskPoliciesByProject(ctx, projectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "list enabled risk policies for observer", attr.SlogError(err))
		return
	}

	for _, p := range policies {
		if err := s.signaler.SignalNewMessages(ctx, background.DrainRiskAnalysisParams{
			ProjectID:    p.ProjectID,
			RiskPolicyID: p.ID,
		}); err != nil {
			s.logger.ErrorContext(ctx, "signal risk drain workflow", attr.SlogError(err))
		}
	}
}

func (s *Service) CreateRiskPolicy(ctx context.Context, payload *gen.CreateRiskPolicyPayload) (*types.RiskPolicy, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildWrite, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}

	if err := validatePolicyName(payload.Name); err != nil {
		return nil, err
	}

	sources := payload.Sources
	if len(sources) == 0 {
		sources = []string{"gitleaks"}
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate policy id").Log(ctx, s.logger)
	}

	row, err := s.repo.CreateRiskPolicy(ctx, repo.CreateRiskPolicyParams{
		ID:             id,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           payload.Name,
		Sources:        sources,
		Enabled:        enabled,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create risk policy").Log(ctx, s.logger)
	}

	// Trigger the drain workflow for the new policy.
	if enabled {
		_ = s.signaler.SignalNewMessages(ctx, background.DrainRiskAnalysisParams{
			ProjectID:    row.ProjectID,
			RiskPolicyID: row.ID,
		})
	}

	return s.policyToType(ctx, row)
}

func (s *Service) ListRiskPolicies(ctx context.Context, payload *gen.ListRiskPoliciesPayload) (*gen.ListRiskPoliciesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}

	rows, err := s.repo.ListRiskPolicies(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk policies").Log(ctx, s.logger)
	}

	policies := make([]*types.RiskPolicy, 0, len(rows))
	for _, row := range rows {
		p, err := s.policyToType(ctx, row)
		if err != nil {
			return nil, err
		}
		policies = append(policies, p)
	}

	return &gen.ListRiskPoliciesResult{Policies: policies}, nil
}

func (s *Service) GetRiskPolicy(ctx context.Context, payload *gen.GetRiskPolicyPayload) (*types.RiskPolicy, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	row, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}

	return s.policyToType(ctx, row)
}

func (s *Service) UpdateRiskPolicy(ctx context.Context, payload *gen.UpdateRiskPolicyPayload) (*types.RiskPolicy, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildWrite, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	if err := validatePolicyName(payload.Name); err != nil {
		return nil, err
	}

	// Fetch the current policy so we can preserve fields not provided in the payload.
	current, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}

	sources := current.Sources
	if len(payload.Sources) > 0 {
		sources = payload.Sources
	}

	enabled := current.Enabled
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	row, err := s.repo.UpdateRiskPolicy(ctx, repo.UpdateRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
		Name:      payload.Name,
		Sources:   sources,
		Enabled:   enabled,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update risk policy").Log(ctx, s.logger)
	}

	// Signal the drain workflow — it reads the current enabled/version
	// from the DB, so it will clean up results if the policy was disabled.
	_ = s.signaler.SignalNewMessages(ctx, background.DrainRiskAnalysisParams{
		ProjectID:    row.ProjectID,
		RiskPolicyID: row.ID,
	})

	return s.policyToType(ctx, row)
}

func (s *Service) DeleteRiskPolicy(ctx context.Context, payload *gen.DeleteRiskPolicyPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildWrite, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.C(oops.CodeInvalid)
	}

	// Soft-delete only — list queries already filter out results for deleted
	// policies via the risk_policies join, so orphaned rows are harmless.
	if err := s.repo.DeleteRiskPolicy(ctx, repo.DeleteRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete risk policy").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) ListRiskResults(ctx context.Context, payload *gen.ListRiskResultsPayload) (*gen.ListRiskResultsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}

	rawLimit := payload.Limit
	if rawLimit <= 0 || rawLimit > 500 {
		rawLimit = 100
	}
	limit := int32(rawLimit)

	if payload.ChatID != nil && *payload.ChatID != "" {
		return s.listResultsByChat(ctx, *authCtx.ProjectID, *payload.ChatID, limit)
	}
	if payload.PolicyID != nil && *payload.PolicyID != "" {
		return s.listResultsByPolicy(ctx, *authCtx.ProjectID, *payload.PolicyID, limit)
	}
	return s.listResultsByProject(ctx, *authCtx.ProjectID, limit)
}

func (s *Service) listResultsByChat(ctx context.Context, projectID uuid.UUID, rawChatID string, limit int32) (*gen.ListRiskResultsResult, error) {
	chatID, err := uuid.Parse(rawChatID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}
	rows, err := s.repo.ListRiskResultsByChatFound(ctx, repo.ListRiskResultsByChatFoundParams{
		ChatID:      chatID,
		ProjectID:   projectID,
		ResultLimit: limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by chat").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	for _, row := range rows {
		cid := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &cid, row.ChatTitle, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.CreatedAt))
	}
	return &gen.ListRiskResultsResult{Results: results}, nil
}

func (s *Service) listResultsByPolicy(ctx context.Context, projectID uuid.UUID, rawPolicyID string, limit int32) (*gen.ListRiskResultsResult, error) {
	policyID, err := uuid.Parse(rawPolicyID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}
	rows, err := s.repo.ListRiskResultsByProjectAndPolicy(ctx, repo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    projectID,
		RiskPolicyID: policyID,
		ResultLimit:  limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by policy").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	for _, row := range rows {
		chatID := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &chatID, row.ChatTitle, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.CreatedAt))
	}
	return &gen.ListRiskResultsResult{Results: results}, nil
}

func (s *Service) listResultsByProject(ctx context.Context, projectID uuid.UUID, limit int32) (*gen.ListRiskResultsResult, error) {
	rows, err := s.repo.ListRiskResultsByProjectFound(ctx, repo.ListRiskResultsByProjectFoundParams{
		ProjectID:   projectID,
		ResultLimit: limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	for _, row := range rows {
		chatID := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &chatID, row.ChatTitle, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.CreatedAt))
	}
	return &gen.ListRiskResultsResult{Results: results}, nil
}

func (s *Service) GetRiskPolicyStatus(ctx context.Context, payload *gen.GetRiskPolicyStatusPayload) (*types.RiskPolicyStatus, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	policy, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}

	totalMessages, err := s.repo.CountTotalMessages(ctx, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count total messages").Log(ctx, s.logger)
	}

	analyzedMessages, err := s.repo.CountAnalyzedMessages(ctx, repo.CountAnalyzedMessagesParams{
		ProjectID:         *authCtx.ProjectID,
		RiskPolicyID:      id,
		RiskPolicyVersion: policy.Version,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count analyzed messages").Log(ctx, s.logger)
	}

	findingsCount, err := s.repo.CountFindingsByPolicy(ctx, repo.CountFindingsByPolicyParams{
		ProjectID:         *authCtx.ProjectID,
		RiskPolicyID:      id,
		RiskPolicyVersion: policy.Version,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count findings").Log(ctx, s.logger)
	}

	pending := max(totalMessages-analyzedMessages, 0)

	// We report a simplified workflow status based on pending work.
	workflowStatus := "not_started"
	if pending > 0 && policy.Enabled {
		workflowStatus = "running"
	} else if pending == 0 && policy.Enabled {
		workflowStatus = "sleeping"
	}

	return &types.RiskPolicyStatus{
		PolicyID:         id.String(),
		PolicyVersion:    policy.Version,
		TotalMessages:    totalMessages,
		AnalyzedMessages: analyzedMessages,
		PendingMessages:  pending,
		FindingsCount:    findingsCount,
		WorkflowStatus:   workflowStatus,
	}, nil
}

func (s *Service) TriggerRiskAnalysis(ctx context.Context, payload *gen.TriggerRiskAnalysisPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildWrite, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.C(oops.CodeInvalid)
	}

	policy, err := s.repo.BumpRiskPolicyVersion(ctx, repo.BumpRiskPolicyVersionParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "bump policy version").Log(ctx, s.logger)
	}

	if err := s.signaler.SignalNewMessages(ctx, background.DrainRiskAnalysisParams{
		ProjectID:    policy.ProjectID,
		RiskPolicyID: policy.ID,
	}); err != nil {
		return fmt.Errorf("signal risk analysis workflow: %w", err)
	}
	return nil
}

// policyToType converts a database row to the API type, enriching it with
// message counts.
func (s *Service) policyToType(ctx context.Context, row repo.RiskPolicy) (*types.RiskPolicy, error) {
	totalMessages, err := s.repo.CountTotalMessages(ctx, uuid.NullUUID{UUID: row.ProjectID, Valid: true})
	if err != nil {
		totalMessages = 0
	}

	analyzedMessages, err := s.repo.CountAnalyzedMessages(ctx, repo.CountAnalyzedMessagesParams{
		ProjectID:         row.ProjectID,
		RiskPolicyID:      row.ID,
		RiskPolicyVersion: row.Version,
	})
	if err != nil {
		analyzedMessages = 0
	}
	pendingMessages := max(totalMessages-analyzedMessages, 0)

	return &types.RiskPolicy{
		ID:              row.ID.String(),
		ProjectID:       row.ProjectID.String(),
		Name:            row.Name,
		Sources:         row.Sources,
		Enabled:         row.Enabled,
		Version:         row.Version,
		CreatedAt:       row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.Format(time.RFC3339),
		PendingMessages: pendingMessages,
		TotalMessages:   totalMessages,
	}, nil
}

func validatePolicyName(name string) error {
	if name == "" {
		return oops.E(oops.CodeInvalid, nil, "name must not be empty")
	}
	if len([]rune(name)) > 100 {
		return oops.E(oops.CodeInvalid, nil, "name must be at most 100 characters")
	}
	return nil
}

func foundRowToResult(
	id, policyID uuid.UUID, policyVersion int64, chatMessageID uuid.UUID, chatID *string, chatTitle pgtype.Text,
	source string, ruleID, description, match pgtype.Text,
	startPos, endPos pgtype.Int4,
	confidence pgtype.Float8, tags []string, createdAt pgtype.Timestamptz,
) *types.RiskResult {
	return &types.RiskResult{
		ID:            id.String(),
		PolicyID:      policyID.String(),
		PolicyVersion: policyVersion,
		ChatMessageID: chatMessageID.String(),
		ChatID:        chatID,
		ChatTitle:     conv.FromPGText[string](chatTitle),
		Source:        source,
		RuleID:        conv.FromPGText[string](ruleID),
		Description:   conv.FromPGText[string](description),
		Match:         conv.FromPGText[string](match),
		StartPos:      conv.FromPGInt4(startPos),
		EndPos:        conv.FromPGInt4(endPos),
		Confidence:    conv.FromPGFloat8(confidence),
		Tags:          tags,
		CreatedAt:     createdAt.Time.Format(time.RFC3339),
	}
}
