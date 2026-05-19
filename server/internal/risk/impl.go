package risk

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
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
	"github.com/speakeasy-api/gram/server/internal/cache"
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
	cache            cache.Cache
	piClassifier     bool
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
		cache:            nil,
		piClassifier:     false,
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
	redisCache cache.Cache,
	piClassifier bool,
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
		cache:            redisCache,
		piClassifier:     piClassifier,
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

func (s *Service) GetRiskCapabilities(ctx context.Context, payload *gen.GetRiskCapabilitiesPayload) (*gen.RiskCapabilitiesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	return &gen.RiskCapabilitiesResult{
		PiClassifierEnabled: s.piClassifier,
	}, nil
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

	// Each SignalNewMessages call round-trips to Temporal (~20ms),
	// and this runs on the chat-message hot path. Fan out so total
	// latency is the slowest signal, not the sum of all signals.
	var wg sync.WaitGroup
	wg.Add(len(policies))
	for _, p := range policies {
		go func(p repo.RiskPolicy) {
			defer wg.Done()
			if err := s.signaler.SignalNewMessages(ctx, background.DrainRiskAnalysisParams{
				ProjectID:    p.ProjectID,
				RiskPolicyID: p.ID,
				MaxMessages:  background.DefaultRecentMessagesBudget,
			}); err != nil {
				s.logger.ErrorContext(ctx, "signal risk drain workflow",
					attr.SlogError(err),
					attr.SlogRiskPolicyID(p.ID.String()),
				)
			}
		}(p)
	}
	wg.Wait()
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
		ID:                   id,
		ProjectID:            *authCtx.ProjectID,
		OrganizationID:       authCtx.ActiveOrganizationID,
		Name:                 name,
		Sources:              sources,
		PresidioEntities:     payload.PresidioEntities,
		PromptInjectionRules: payload.PromptInjectionRules,
		Enabled:              enabled,
		Action:               action,
		AutoName:             autoName,
		UserMessage:          conv.PtrToPGTextEmpty(payload.UserMessage),
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

	// Trigger the drain workflow for the new policy. New policies only
	// scan the most recent slice by default; users can request a full
	// backfill explicitly via TriggerRiskAnalysis on the Progress tab.
	if enabled {
		_ = s.signaler.SignalNewMessages(ctx, background.DrainRiskAnalysisParams{
			ProjectID:    row.ProjectID,
			RiskPolicyID: row.ID,
			MaxMessages:  background.DefaultRecentMessagesBudget,
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

	promptInjectionRules := current.PromptInjectionRules
	if payload.PromptInjectionRules != nil {
		promptInjectionRules = payload.PromptInjectionRules
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
		ID:                   id,
		ProjectID:            *authCtx.ProjectID,
		Name:                 name,
		Sources:              sources,
		PresidioEntities:     presidioEntities,
		PromptInjectionRules: promptInjectionRules,
		Enabled:              enabled,
		Action:               action,
		AutoName:             autoName,
		UserMessage:          userMessage,
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
	// from the DB, so it will clean up results if the policy was
	// disabled. Policy edits default to the recent-N budget; full
	// backfill is opt-in via TriggerRiskAnalysis.
	_ = s.signaler.SignalNewMessages(ctx, background.DrainRiskAnalysisParams{
		ProjectID:    row.ProjectID,
		RiskPolicyID: row.ID,
		MaxMessages:  background.DefaultRecentMessagesBudget,
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

// riskResultsCursor is the composite cursor used for paginating risk
// result listings. We sort by (message_created_at, id) so the cursor
// carries both fields. Encoded as "<RFC3339Nano>|<uuid>" — a small,
// opaque-to-the-client string that survives URL/query-param round-trips.
// Legacy id-only cursors are silently dropped (treated as no cursor)
// rather than rejected, so an in-flight infinite-scroll session that
// upgrades doesn't error mid-scroll.
type riskResultsCursor struct {
	MessageCreatedAt time.Time
	ID               uuid.UUID
}

func parseRiskResultsCursor(raw *string) (*riskResultsCursor, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	parts := strings.SplitN(*raw, "|", 2)
	if len(parts) != 2 {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, fmt.Errorf("parse cursor timestamp: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, fmt.Errorf("parse cursor id: %w", err)
	}
	return &riskResultsCursor{MessageCreatedAt: t, ID: id}, nil
}

func encodeRiskResultsCursor(c riskResultsCursor) string {
	return c.MessageCreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
}

func cursorToParams(c *riskResultsCursor) (pgtype.Timestamptz, uuid.NullUUID) {
	if c == nil {
		return pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}, uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return pgtype.Timestamptz{Time: c.MessageCreatedAt, InfinityModifier: pgtype.Finite, Valid: true}, uuid.NullUUID{UUID: c.ID, Valid: true}
}

func (s *Service) ListRiskResults(ctx context.Context, payload *gen.ListRiskResultsPayload) (*gen.ListRiskResultsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	cursor, err := parseRiskResultsCursor(payload.Cursor)
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

func (s *Service) GetRiskOverview(ctx context.Context, payload *gen.GetRiskOverviewPayload) (*gen.RiskOverviewResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	from, to, err := resolveRiskOverviewWindow(payload.From, payload.To)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid overview window").Log(ctx, s.logger)
	}

	window := riskOverviewWindowParams(from, to)
	counts, err := s.repo.GetRiskOverviewCounts(ctx, repo.GetRiskOverviewCountsParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get risk overview counts").Log(ctx, s.logger)
	}

	userRows, err := s.repo.ListRiskOverviewTopUsers(ctx, repo.ListRiskOverviewTopUsersParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
		RowLimit:  10,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk overview top users").Log(ctx, s.logger)
	}

	timeSeriesRows, err := s.repo.ListRiskOverviewTimeSeriesFindings(ctx, repo.ListRiskOverviewTimeSeriesFindingsParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk overview time series findings").Log(ctx, s.logger)
	}

	topCategories := riskOverviewTopCategories(timeSeriesRows, 10)

	topUsers := make([]*gen.RiskOverviewUser, 0, len(userRows))
	for _, row := range userRows {
		topUsers = append(topUsers, &gen.RiskOverviewUser{
			Email:    row.Email,
			Findings: row.Findings,
		})
	}

	timeSeriesFindings := make([]*gen.RiskOverviewTimeSeriesFinding, 0, len(timeSeriesRows))
	for _, row := range timeSeriesRows {
		timeSeriesFindings = append(timeSeriesFindings, &gen.RiskOverviewTimeSeriesFinding{
			BucketStart: row.BucketStart.Time.UTC().Format(time.RFC3339),
			Category:    row.Category,
			Findings:    row.Findings,
		})
	}

	return &gen.RiskOverviewResult{
		From:               from.UTC().Format(time.RFC3339),
		To:                 to.UTC().Format(time.RFC3339),
		MessagesScanned:    counts.MessagesScanned,
		Findings:           counts.Findings,
		FlaggedSessions:    counts.FlaggedSessions,
		ActivePolicies:     counts.ActivePolicies,
		TopCategories:      topCategories,
		TopUsers:           topUsers,
		TimeSeriesFindings: timeSeriesFindings,
	}, nil
}

type riskOverviewWindow struct {
	from pgtype.Timestamptz
	to   pgtype.Timestamptz
}

const maxRiskOverviewWindow = 31 * 24 * time.Hour

func resolveRiskOverviewWindow(rawFrom, rawTo *string) (time.Time, time.Time, error) {
	to := time.Now().UTC()
	if rawTo != nil && strings.TrimSpace(*rawTo) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*rawTo))
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse to: %w", err)
		}
		to = parsed.UTC()
	}

	toYear, toMonth, toDay := to.Date()
	from := time.Date(toYear, toMonth, toDay, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -6)
	if rawFrom != nil && strings.TrimSpace(*rawFrom) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*rawFrom))
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse from: %w", err)
		}
		from = parsed.UTC()
	}

	if !from.Before(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("from must be before to")
	}
	if to.Sub(from) > maxRiskOverviewWindow {
		return time.Time{}, time.Time{}, fmt.Errorf("window must be %s or less", maxRiskOverviewWindow)
	}

	return from, to, nil
}

func riskOverviewWindowParams(from, to time.Time) riskOverviewWindow {
	return riskOverviewWindow{
		from: pgtype.Timestamptz{Time: from, InfinityModifier: pgtype.Finite, Valid: true},
		to:   pgtype.Timestamptz{Time: to, InfinityModifier: pgtype.Finite, Valid: true},
	}
}

func riskOverviewTopCategories(rows []repo.ListRiskOverviewTimeSeriesFindingsRow, limit int) []*gen.RiskOverviewCategory {
	if limit <= 0 {
		return nil
	}

	counts := make(map[string]int64)
	for _, row := range rows {
		counts[row.Category] += row.Findings
	}

	categories := make([]*gen.RiskOverviewCategory, 0, len(counts))
	for category, findings := range counts {
		if findings == 0 {
			continue
		}
		categories = append(categories, &gen.RiskOverviewCategory{
			Category: category,
			Findings: findings,
		})
	}

	slices.SortFunc(categories, func(a, b *gen.RiskOverviewCategory) int {
		if a.Findings != b.Findings {
			return cmp.Compare(b.Findings, a.Findings)
		}

		return cmp.Compare(a.Category, b.Category)
	})

	if len(categories) > limit {
		categories = categories[:limit]
	}

	return categories
}

// paginateResults trims the over-fetched extra row and, if present, encodes
// the (message_created_at, id) cursor from it so the next page starts
// strictly after the last returned row.
func (s *Service) paginateResults(results []*types.RiskResult, extraRow *riskResultsCursor, pageSize int, totalCount int64) *gen.ListRiskResultsResult {
	var nextCursor *string
	if len(results) > pageSize && extraRow != nil {
		encoded := encodeRiskResultsCursor(*extraRow)
		nextCursor = &encoded
		results = results[:pageSize]
	}
	return &gen.ListRiskResultsResult{Results: results, TotalCount: totalCount, NextCursor: nextCursor}
}

func (s *Service) listResultsByChat(ctx context.Context, projectID uuid.UUID, rawChatID string, cursor *riskResultsCursor, pageSize int, totalCount int64) (*gen.ListRiskResultsResult, error) {
	chatID, err := uuid.Parse(rawChatID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}
	cursorCreatedAt, cursorID := cursorToParams(cursor)
	rows, err := s.repo.ListRiskResultsByChatFound(ctx, repo.ListRiskResultsByChatFoundParams{
		ChatID:                 chatID,
		ProjectID:              projectID,
		CursorMessageCreatedAt: cursorCreatedAt,
		CursorID:               cursorID,
		PageLimit:              conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by chat").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		cid := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &cid, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.MessageCreatedAt))
		if i == pageSize {
			nextCursor = &riskResultsCursor{MessageCreatedAt: row.MessageCreatedAt.Time, ID: row.ID}
		}
	}
	return s.paginateResults(results, nextCursor, pageSize, totalCount), nil
}

func (s *Service) listResultsByPolicy(ctx context.Context, projectID uuid.UUID, rawPolicyID string, cursor *riskResultsCursor, pageSize int, totalCount int64) (*gen.ListRiskResultsResult, error) {
	policyID, err := uuid.Parse(rawPolicyID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}
	cursorCreatedAt, cursorID := cursorToParams(cursor)
	rows, err := s.repo.ListRiskResultsByProjectAndPolicy(ctx, repo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:              projectID,
		RiskPolicyID:           policyID,
		CursorMessageCreatedAt: cursorCreatedAt,
		CursorID:               cursorID,
		PageLimit:              conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by policy").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		chatID := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &chatID, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.MessageCreatedAt))
		if i == pageSize {
			nextCursor = &riskResultsCursor{MessageCreatedAt: row.MessageCreatedAt.Time, ID: row.ID}
		}
	}
	return s.paginateResults(results, nextCursor, pageSize, totalCount), nil
}

func (s *Service) listResultsByProject(ctx context.Context, projectID uuid.UUID, cursor *riskResultsCursor, pageSize int, totalCount int64) (*gen.ListRiskResultsResult, error) {
	cursorCreatedAt, cursorID := cursorToParams(cursor)
	rows, err := s.repo.ListRiskResultsByProjectFound(ctx, repo.ListRiskResultsByProjectFoundParams{
		ProjectID:              projectID,
		CursorMessageCreatedAt: cursorCreatedAt,
		CursorID:               cursorID,
		PageLimit:              conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		chatID := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &chatID, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.MessageCreatedAt))
		if i == pageSize {
			nextCursor = &riskResultsCursor{MessageCreatedAt: row.MessageCreatedAt.Time, ID: row.ID}
		}
	}
	return s.paginateResults(results, nextCursor, pageSize, totalCount), nil
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

	// payload.Limit defaults to 100 (Goa fills in the recent-N budget
	// when callers omit it). Explicit 0 requests a full backfill of
	// every unanalyzed message.
	maxMessages := payload.Limit

	if err := s.signaler.SignalNewMessages(ctx, background.DrainRiskAnalysisParams{
		ProjectID:    policy.ProjectID,
		RiskPolicyID: policy.ID,
		MaxMessages:  maxMessages,
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
		ID:                   row.ID.String(),
		ProjectID:            row.ProjectID.String(),
		Name:                 row.Name,
		Sources:              row.Sources,
		PresidioEntities:     row.PresidioEntities,
		PromptInjectionRules: row.PromptInjectionRules,
		Enabled:              row.Enabled,
		Action:               row.Action,
		AutoName:             row.AutoName,
		UserMessage:          conv.FromPGText[string](row.UserMessage),
		Version:              row.Version,
		CreatedAt:            row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            row.UpdatedAt.Time.Format(time.RFC3339),
		PendingMessages:      pendingMessages,
		TotalMessages:        totalMessages,
	}, nil
}

// policyRowSnapshot returns a *types.RiskPolicy suitable for audit log
// snapshots. Unlike policyToType it skips the extra DB queries for message
// counts, keeping transactions short. Count fields are set to -1 to indicate
// they were not computed.
func policyRowSnapshot(row repo.RiskPolicy) *types.RiskPolicy {
	return &types.RiskPolicy{
		ID:                   row.ID.String(),
		ProjectID:            row.ProjectID.String(),
		Name:                 row.Name,
		Sources:              row.Sources,
		PresidioEntities:     row.PresidioEntities,
		PromptInjectionRules: row.PromptInjectionRules,
		Enabled:              row.Enabled,
		Action:               row.Action,
		AutoName:             row.AutoName,
		UserMessage:          conv.FromPGText[string](row.UserMessage),
		Version:              row.Version,
		CreatedAt:            row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            row.UpdatedAt.Time.Format(time.RFC3339),
		PendingMessages:      -1,
		TotalMessages:        -1,
	}
}

func (s *Service) generatePolicyName(ctx context.Context, orgID, projectID string, sources, presidioEntities []string, action string, existingNames []string) string {
	if s.completionClient == nil {
		return s.fallbackPolicyName(sources, action)
	}

	// Policy authors think in *what* is detected, not *how* (gitleaks,
	// presidio). Translate sources to user-facing category labels and
	// scrub library names so the LLM cannot regurgitate them. See AGE-2378.
	categories := sourcesToCategoryLabels(sources)

	prompt := fmt.Sprintf(
		"Generate a short, human-friendly name (2-5 words) for a security policy with these settings:\n"+
			"- Detection categories: %v\n"+
			"- PII entity types: %v\n"+
			"- Action: %s\n"+
			"- Existing policy names to avoid: %v\n\n"+
			"Return ONLY the name, no quotes or explanation. Make it descriptive and distinct from existing names. "+
			"Do not mention internal tool or library names; describe what is detected.",
		categories, presidioEntities, action, existingNames,
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

// sourcesToCategoryLabels maps internal source identifiers (gitleaks,
// presidio, …) to the user-facing detection category they implement.
// Library names are an implementation detail; policy authors think in
// what is detected, not how. See AGE-2378.
func sourcesToCategoryLabels(sources []string) []string {
	out := make([]string, 0, len(sources))
	for _, src := range sources {
		switch src {
		case "gitleaks":
			out = append(out, "Secrets")
		case "presidio":
			out = append(out, "PII")
		case shadowmcp.SourceShadowMCP:
			out = append(out, "Shadow MCP")
		case shadowmcp.SourceDestructiveTool:
			out = append(out, "Destructive Tool")
		case ra.SourceCLIDestructive:
			out = append(out, "Destructive CLI Command")
		case ra.SourcePromptInjection:
			out = append(out, "Prompt Injection")
		}
	}
	return out
}

func (s *Service) fallbackPolicyName(sources []string, action string) string {
	parts := sourcesToCategoryLabels(sources)
	// Singularize the leading "Secrets" label when used in a name like
	// "Secret Blocker" — preserves the previous look of fallback names.
	for i, p := range parts {
		if p == "Secrets" {
			parts[i] = "Secret"
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

// ensureApprovalPolicy validates the policy ID for an approval endpoint and
// confirms the policy belongs to the active project. Returns the parsed
// UUID on success.
func (s *Service) ensureApprovalPolicy(ctx context.Context, authCtx *contextvalues.AuthContext, rawPolicyID string) (uuid.UUID, error) {
	policyID, err := uuid.Parse(rawPolicyID)
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeInvalid, err, "invalid policy id")
	}
	if _, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        policyID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		return uuid.Nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}
	return policyID, nil
}

func (s *Service) ListShadowMCPApprovals(ctx context.Context, payload *gen.ListShadowMCPApprovalsPayload) (*gen.ListShadowMCPApprovalsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	policyID, err := s.ensureApprovalPolicy(ctx, authCtx, payload.PolicyID)
	if err != nil {
		return nil, err
	}

	rows, err := shadowmcp.ListShadowMCPApprovals(ctx, s.cache, authCtx.ProjectID.String(), policyID.String())
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow-mcp approvals").Log(ctx, s.logger)
	}

	out := make([]*types.ShadowMCPApproval, 0, len(rows))
	for _, r := range rows {
		out = append(out, shadowMCPApprovalToType(policyID, r))
	}
	return &gen.ListShadowMCPApprovalsResult{Approvals: out}, nil
}

func (s *Service) ApproveShadowMCP(ctx context.Context, payload *gen.ApproveShadowMCPPayload) (*types.ShadowMCPApproval, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	policyID, err := s.ensureApprovalPolicy(ctx, authCtx, payload.PolicyID)
	if err != nil {
		return nil, err
	}

	canonical := shadowmcp.CanonicalizeMatch(payload.Match)
	if canonical == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "approval match is empty")
	}

	approval := shadowmcp.ShadowMCPApproval{
		Match:      canonical,
		ServerName: strings.TrimSpace(conv.PtrValOr(payload.ServerName, "")),
		ApprovedBy: conv.PtrValOr(authCtx.Email, ""),
		ApprovedAt: time.Now().UTC(),
	}
	if err := shadowmcp.AddShadowMCPApproval(ctx, s.cache, authCtx.ProjectID.String(), policyID.String(), approval); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "store shadow-mcp approval").Log(ctx, s.logger)
	}
	return shadowMCPApprovalToType(policyID, approval), nil
}

func (s *Service) RevokeShadowMCPApproval(ctx context.Context, payload *gen.RevokeShadowMCPApprovalPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	policyID, err := s.ensureApprovalPolicy(ctx, authCtx, payload.PolicyID)
	if err != nil {
		return err
	}

	if err := shadowmcp.RemoveShadowMCPApproval(ctx, s.cache, authCtx.ProjectID.String(), policyID.String(), payload.Match); err != nil {
		return oops.E(oops.CodeUnexpected, err, "remove shadow-mcp approval").Log(ctx, s.logger)
	}
	return nil
}

func shadowMCPApprovalToType(policyID uuid.UUID, a shadowmcp.ShadowMCPApproval) *types.ShadowMCPApproval {
	var serverName, approvedBy *string
	if a.ServerName != "" {
		s := a.ServerName
		serverName = &s
	}
	if a.ApprovedBy != "" {
		s := a.ApprovedBy
		approvedBy = &s
	}
	return &types.ShadowMCPApproval{
		PolicyID:   policyID.String(),
		Match:      a.Match,
		ServerName: serverName,
		ApprovedBy: approvedBy,
		ApprovedAt: a.ApprovedAt.UTC().Format(time.RFC3339),
	}
}
