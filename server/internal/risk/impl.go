package risk

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/risk/server"
	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

// RiskAnalysisSignaler starts or signals the drain workflow for a risk policy.
type RiskAnalysisSignaler interface {
	SignalNewMessages(ctx context.Context, params background.DrainRiskAnalysisParams) error
}

type Service struct {
	tracer           trace.Tracer
	logger           *slog.Logger
	db               *pgxpool.Pool
	repo             *repo.Queries
	auth             *auth.Auth
	authz            *authz.Engine
	signaler         RiskAnalysisSignaler
	completionClient openrouter.CompletionClient
	shadowMCPClient  *shadowmcp.Client
	audit            *audit.Logger
}

var _ chat.MessageObserver = (*Service)(nil)

// NewObserver creates a lightweight chat.MessageObserver that signals the risk
// drain workflow when new messages are stored. Use this in contexts (e.g. the
// worker process) where the full risk Service is not needed.
func NewObserver(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	signaler RiskAnalysisSignaler,
	auditLogger *audit.Logger,
) chat.MessageObserver {
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/risk"),

		logger:           logger.With(attr.SlogComponent("risk")),
		db:               db,
		repo:             repo.New(db),
		auth:             nil,
		authz:            nil,
		signaler:         signaler,
		completionClient: nil,
		shadowMCPClient:  nil,
		audit:            auditLogger,
	}
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	signaler RiskAnalysisSignaler,
	completionClient openrouter.CompletionClient,
	shadowMCPClient *shadowmcp.Client,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("risk"))

	return &Service{
		tracer:           tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/risk"),
		logger:           logger,
		db:               db,
		repo:             repo.New(db),
		auth:             auth.New(logger, db, sessions, authzEngine),
		authz:            authzEngine,
		signaler:         signaler,
		completionClient: completionClient,
		shadowMCPClient:  shadowMCPClient,
		audit:            auditLogger,
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

	s.logger.DebugContext(ctx, "risk observer signaling policies",
		attr.SlogProjectID(projectID.String()),
		attr.SlogRiskPolicyCount(len(policies)),
	)

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

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	name := ""
	if payload.Name != nil {
		name = *payload.Name
	}

	// Default auto_name to true only when the caller did not supply Name at
	// all (nil pointer). An explicitly empty Name (`Name: new("")`) is
	// treated as a validation error below — that path is exercised by
	// TestCreateRiskPolicy_EmptyName. An explicit auto_name in the payload
	// always wins over the default so callers can opt in/out without
	// ambiguity.
	autoName := payload.Name == nil
	if payload.AutoName != nil {
		autoName = *payload.AutoName
	}

	action := payload.Action
	if action == "" {
		action = "flag"
	}
	if err := validateAction(action); err != nil {
		return nil, err
	}

	sources := payload.Sources
	if sources == nil {
		sources = []string{"gitleaks"}
	}
	if err := validateSources(sources); err != nil {
		return nil, err
	}
	if err := validateSourceAction(sources, action); err != nil {
		return nil, err
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	// Auto-generate a name when the caller opted in (explicit auto_name=true
	// or omitted both auto_name and name). Setting auto_name=false with an
	// empty name surfaces a validation error below rather than silently
	// auto-generating.
	if autoName {
		existingPolicies, _ := s.repo.ListRiskPolicies(ctx, *authCtx.ProjectID)
		var existingNames []string
		for _, p := range existingPolicies {
			existingNames = append(existingNames, p.Name)
		}
		name = s.generatePolicyName(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), sources, payload.PresidioEntities, action, existingNames)
	}

	if err := validatePolicyName(name); err != nil {
		return nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate policy id").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateRiskPolicy(ctx, repo.CreateRiskPolicyParams{
		ID:               id,
		ProjectID:        *authCtx.ProjectID,
		OrganizationID:   authCtx.ActiveOrganizationID,
		Name:             name,
		Sources:          sources,
		PresidioEntities: payload.PresidioEntities,
		Enabled:          enabled,
		Action:           action,
		AutoName:         autoName,
		UserMessage:      conv.PtrToPGTextEmpty(payload.UserMessage),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create risk policy").Log(ctx, s.logger)
	}

	if err := s.audit.LogRiskPolicyCreate(ctx, dbtx, audit.LogRiskPolicyCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskPolicyID:     row.ID,
		RiskPolicyName:   row.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy create").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy create").Log(ctx, s.logger)
	}

	if s.shadowMCPClient != nil {
		s.shadowMCPClient.Invalidate(ctx, row.ProjectID)
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

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
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

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
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

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	// Name validation runs after the auto-name regeneration block below, so
	// callers can send auto_name=true with an empty name and have the server
	// generate one before validation rejects it.

	// Fetch the current policy so we can preserve fields not provided in the payload.
	current, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}

	sources := current.Sources
	if payload.Sources != nil {
		sources = payload.Sources
		if err := validateSources(sources); err != nil {
			return nil, err
		}
	}

	presidioEntities := current.PresidioEntities
	if payload.PresidioEntities != nil {
		presidioEntities = payload.PresidioEntities
	}

	enabled := current.Enabled
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	action := current.Action
	if payload.Action != nil {
		if err := validateAction(*payload.Action); err != nil {
			return nil, err
		}
		action = *payload.Action
	}
	if err := validateSourceAction(sources, action); err != nil {
		return nil, err
	}

	autoName := current.AutoName
	if payload.AutoName != nil {
		autoName = *payload.AutoName
	}

	// user_message is preserved when not in the payload; an explicit empty
	// string clears it (rendered as NULL).
	userMessage := current.UserMessage
	if payload.UserMessage != nil {
		userMessage = conv.PtrToPGTextEmpty(payload.UserMessage)
	}

	// Regenerate the name only when the caller explicitly opts in on this
	// update via auto_name=true. Toggling unrelated fields (e.g. enabled)
	// should not silently rename the policy.
	name := payload.Name
	if payload.AutoName != nil && *payload.AutoName {
		existingPolicies, _ := s.repo.ListRiskPolicies(ctx, *authCtx.ProjectID)
		var existingNames []string
		for _, p := range existingPolicies {
			if p.ID != id {
				existingNames = append(existingNames, p.Name)
			}
		}
		name = s.generatePolicyName(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), sources, presidioEntities, action, existingNames)
	}

	if err := validatePolicyName(name); err != nil {
		return nil, err
	}

	snapshotBefore := policyRowSnapshot(current)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).UpdateRiskPolicy(ctx, repo.UpdateRiskPolicyParams{
		ID:               id,
		ProjectID:        *authCtx.ProjectID,
		Name:             name,
		Sources:          sources,
		PresidioEntities: presidioEntities,
		Enabled:          enabled,
		Action:           action,
		AutoName:         autoName,
		UserMessage:      userMessage,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update risk policy").Log(ctx, s.logger)
	}

	if err := s.audit.LogRiskPolicyUpdate(ctx, dbtx, audit.LogRiskPolicyUpdateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskPolicyID:     row.ID,
		RiskPolicyName:   row.Name,
		SnapshotBefore:   snapshotBefore,
		SnapshotAfter:    policyRowSnapshot(row),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy update").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy update").Log(ctx, s.logger)
	}

	if s.shadowMCPClient != nil {
		s.shadowMCPClient.Invalidate(ctx, row.ProjectID)
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

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.C(oops.CodeInvalid)
	}

	// Fetch before delete so we can log the policy name.
	existing, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	// Soft-delete only — list queries already filter out results for deleted
	// policies via the risk_policies join, so orphaned rows are harmless.
	if err := repo.New(dbtx).DeleteRiskPolicy(ctx, repo.DeleteRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete risk policy").Log(ctx, s.logger)
	}

	if err := s.audit.LogRiskPolicyDelete(ctx, dbtx, audit.LogRiskPolicyDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskPolicyID:     id,
		RiskPolicyName:   existing.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log risk policy delete").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit risk policy delete").Log(ctx, s.logger)
	}

	if s.shadowMCPClient != nil {
		s.shadowMCPClient.Invalidate(ctx, *authCtx.ProjectID)
	}

	return nil
}

const riskDefaultPageSize = 50

func resolvePageSize(limit *int) int {
	if limit == nil || *limit <= 0 {
		return riskDefaultPageSize
	}
	if *limit > 200 {
		return 200
	}
	return *limit
}

func (s *Service) ListRiskResults(ctx context.Context, payload *gen.ListRiskResultsPayload) (*gen.ListRiskResultsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	cursor, err := conv.PtrToNullUUID(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid cursor").Log(ctx, s.logger)
	}

	pageSize := resolvePageSize(payload.Limit)

	totalCount, err := s.repo.CountAllFindings(ctx, *authCtx.ProjectID)
	if err != nil {
		totalCount = 0
	}

	if payload.ChatID != nil && *payload.ChatID != "" {
		return s.listResultsByChat(ctx, *authCtx.ProjectID, *payload.ChatID, cursor, pageSize, totalCount)
	}
	if payload.PolicyID != nil && *payload.PolicyID != "" {
		return s.listResultsByPolicy(ctx, *authCtx.ProjectID, *payload.PolicyID, cursor, pageSize, totalCount)
	}
	return s.listResultsByProject(ctx, *authCtx.ProjectID, cursor, pageSize, totalCount)
}

func (s *Service) ListRiskResultsByChat(ctx context.Context, payload *gen.ListRiskResultsByChatPayload) (*gen.ListRiskResultsByChatResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	cursor, err := conv.PtrToNullUUID(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid cursor").Log(ctx, s.logger)
	}

	pageSize := resolvePageSize(payload.Limit)

	rows, err := s.repo.ListRiskResultsGroupedByChat(ctx, repo.ListRiskResultsGroupedByChatParams{
		ProjectID: *authCtx.ProjectID,
		Cursor:    cursor,
		PageLimit: conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by chat").Log(ctx, s.logger)
	}

	chats := make([]*types.RiskChatSummary, 0, len(rows))
	for _, row := range rows {
		chats = append(chats, &types.RiskChatSummary{
			ChatID:         row.ChatID.String(),
			ChatTitle:      conv.FromPGText[string](row.ChatTitle),
			UserID:         conv.FromPGText[string](row.ChatUserID),
			FindingsCount:  row.FindingsCount,
			LatestDetected: row.LatestDetected.Time.Format(time.RFC3339),
		})
	}

	var nextCursor *string
	if len(chats) > pageSize {
		nextCursor = &chats[pageSize].ChatID
		chats = chats[:pageSize]
	}

	return &gen.ListRiskResultsByChatResult{Chats: chats, NextCursor: nextCursor}, nil
}

func (s *Service) paginateResults(results []*types.RiskResult, pageSize int, totalCount int64) *gen.ListRiskResultsResult {
	var nextCursor *string
	if len(results) > pageSize {
		nextCursor = &results[pageSize].ID
		results = results[:pageSize]
	}
	return &gen.ListRiskResultsResult{Results: results, TotalCount: totalCount, NextCursor: nextCursor}
}

func (s *Service) listResultsByChat(ctx context.Context, projectID uuid.UUID, rawChatID string, cursor uuid.NullUUID, pageSize int, totalCount int64) (*gen.ListRiskResultsResult, error) {
	chatID, err := uuid.Parse(rawChatID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}
	rows, err := s.repo.ListRiskResultsByChatFound(ctx, repo.ListRiskResultsByChatFoundParams{
		ChatID:    chatID,
		ProjectID: projectID,
		Cursor:    cursor,
		PageLimit: conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by chat").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	for _, row := range rows {
		cid := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &cid, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.CreatedAt))
	}
	return s.paginateResults(results, pageSize, totalCount), nil
}

func (s *Service) listResultsByPolicy(ctx context.Context, projectID uuid.UUID, rawPolicyID string, cursor uuid.NullUUID, pageSize int, totalCount int64) (*gen.ListRiskResultsResult, error) {
	policyID, err := uuid.Parse(rawPolicyID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}
	rows, err := s.repo.ListRiskResultsByProjectAndPolicy(ctx, repo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    projectID,
		RiskPolicyID: policyID,
		Cursor:       cursor,
		PageLimit:    conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by policy").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	for _, row := range rows {
		chatID := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &chatID, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.CreatedAt))
	}
	return s.paginateResults(results, pageSize, totalCount), nil
}

func (s *Service) listResultsByProject(ctx context.Context, projectID uuid.UUID, cursor uuid.NullUUID, pageSize int, totalCount int64) (*gen.ListRiskResultsResult, error) {
	rows, err := s.repo.ListRiskResultsByProjectFound(ctx, repo.ListRiskResultsByProjectFoundParams{
		ProjectID: projectID,
		Cursor:    cursor,
		PageLimit: conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	for _, row := range rows {
		chatID := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &chatID, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.CreatedAt))
	}
	return s.paginateResults(results, pageSize, totalCount), nil
}

func (s *Service) GetRiskPolicyStatus(ctx context.Context, payload *gen.GetRiskPolicyStatusPayload) (*types.RiskPolicyStatus, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
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

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
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

	if err := s.audit.LogRiskPolicyTrigger(ctx, s.db, audit.LogRiskPolicyTriggerEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskPolicyID:     policy.ID,
		RiskPolicyName:   policy.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log risk policy trigger").Log(ctx, s.logger)
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
		ID:               row.ID.String(),
		ProjectID:        row.ProjectID.String(),
		Name:             row.Name,
		Sources:          row.Sources,
		PresidioEntities: row.PresidioEntities,
		Enabled:          row.Enabled,
		Action:           row.Action,
		AutoName:         row.AutoName,
		UserMessage:      conv.FromPGText[string](row.UserMessage),
		Version:          row.Version,
		CreatedAt:        row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:        row.UpdatedAt.Time.Format(time.RFC3339),
		PendingMessages:  pendingMessages,
		TotalMessages:    totalMessages,
	}, nil
}

// policyRowSnapshot returns a *types.RiskPolicy suitable for audit log
// snapshots. Unlike policyToType it skips the extra DB queries for message
// counts, keeping transactions short. Count fields are set to -1 to indicate
// they were not computed.
func policyRowSnapshot(row repo.RiskPolicy) *types.RiskPolicy {
	return &types.RiskPolicy{
		ID:               row.ID.String(),
		ProjectID:        row.ProjectID.String(),
		Name:             row.Name,
		Sources:          row.Sources,
		PresidioEntities: row.PresidioEntities,
		Enabled:          row.Enabled,
		Action:           row.Action,
		AutoName:         row.AutoName,
		UserMessage:      conv.FromPGText[string](row.UserMessage),
		Version:          row.Version,
		CreatedAt:        row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:        row.UpdatedAt.Time.Format(time.RFC3339),
		PendingMessages:  -1,
		TotalMessages:    -1,
	}
}

func (s *Service) generatePolicyName(ctx context.Context, orgID, projectID string, sources, presidioEntities []string, action string, existingNames []string) string {
	if s.completionClient == nil {
		return s.fallbackPolicyName(sources, action)
	}

	prompt := fmt.Sprintf(
		"Generate a short, human-friendly name (2-5 words) for a security policy with these settings:\n"+
			"- Detection sources: %v\n"+
			"- PII entities: %v\n"+
			"- Action: %s\n"+
			"- Existing policy names to avoid: %v\n\n"+
			"Return ONLY the name, no quotes or explanation. Make it descriptive and distinct from existing names.",
		sources, presidioEntities, action, existingNames,
	)

	// Tight timeout: this runs synchronously in the API request path. If
	// OpenRouter is slow we fall back to fallbackPolicyName rather than
	// blocking the create/update RPC for long.
	nameCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	response, err := s.completionClient.GetCompletion(nameCtx, openrouter.CompletionRequest{
		OrgID:     orgID,
		ProjectID: projectID,
		ChatID:    uuid.Nil,
		Messages: []or.ChatMessages{
			openrouter.CreateMessageUser(prompt),
		},
		Tools:                     nil,
		Temperature:               nil,
		Model:                     "",
		Stream:                    false,
		UsageSource:               billing.ModelUsageSourceGram,
		UserID:                    "",
		ExternalUserID:            "",
		UserEmail:                 "",
		HTTPMetadata:              nil,
		APIKeyID:                  "",
		JSONSchema:                nil,
		NormalizeOutboundMessages: false,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate policy name via OpenRouter", attr.SlogError(err))
		return s.fallbackPolicyName(sources, action)
	}

	name := strings.TrimSpace(openrouter.GetText(*response.Message))
	if name == "" {
		return s.fallbackPolicyName(sources, action)
	}

	// Truncate to 100 chars
	runes := []rune(name)
	if len(runes) > 100 {
		name = string(runes[:100])
	}

	return name
}

func (s *Service) fallbackPolicyName(sources []string, action string) string {
	var parts []string
	for _, src := range sources {
		switch src {
		case "gitleaks":
			parts = append(parts, "Secret")
		case "presidio":
			parts = append(parts, "PII")
		case shadowmcp.SourceShadowMCP:
			parts = append(parts, "Shadow MCP")
		case shadowmcp.SourceDestructiveTool:
			parts = append(parts, "Destructive Tool")
		case ra.SourceCLIDestructive:
			parts = append(parts, "Destructive CLI Command")
		case ra.SourcePromptInjection:
			parts = append(parts, "Prompt Injection")
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "Risk")
	}

	actionLabel := "Scanner"
	if action == "block" {
		actionLabel = "Blocker"
	}

	return strings.Join(parts, " & ") + " " + actionLabel
}

func validateAction(action string) error {
	switch action {
	case "flag", "block":
		return nil
	default:
		return oops.E(oops.CodeInvalid, nil, "action must be one of: flag, block")
	}
}

func validateSources(sources []string) error {
	for _, src := range sources {
		switch src {
		case "gitleaks", "presidio", shadowmcp.SourceShadowMCP, shadowmcp.SourceDestructiveTool, ra.SourceCLIDestructive, ra.SourcePromptInjection:
		default:
			return oops.E(oops.CodeInvalid, nil, "source %q is not a recognized policy source", src)
		}
	}
	return nil
}

func validateSourceAction(sources []string, action string) error {
	if action != "block" {
		return nil
	}
	for _, src := range []string{shadowmcp.SourceDestructiveTool, ra.SourceCLIDestructive} {
		if slices.Contains(sources, src) {
			return oops.E(oops.CodeInvalid, nil, "source %q supports flagging only", src)
		}
	}
	return nil
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
	id, policyID uuid.UUID, policyVersion int64, chatMessageID uuid.UUID, chatID *string, chatTitle, chatUserID pgtype.Text,
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
		UserID:        conv.FromPGText[string](chatUserID),
		Source:        source,
		RuleID:        conv.FromPGText[string](ruleID),
		Description:   conv.FromPGText[string](description),
		Match:         conv.FromPGText[string](match),
		StartPos:      conv.PtrInt32ToInt(conv.FromPGInt4(startPos)),
		EndPos:        conv.PtrInt32ToInt(conv.FromPGInt4(endPos)),
		Confidence:    conv.FromPGFloat8(confidence),
		Tags:          tags,
		CreatedAt:     createdAt.Time.Format(time.RFC3339),
	}
}
