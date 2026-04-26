package insights

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/insights/server"
	gen "github.com/speakeasy-api/gram/server/gen/insights"
	toolsetsgen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	variationsgen "github.com/speakeasy-api/gram/server/gen/variations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/insights/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	kindToolVariation = "tool_variation"
	kindToolsetChange = "toolset_change"

	memoryKindFinding = "finding"

	defaultMemoryLimit = 50
	maxMemoryLimit     = 200

	findingTTL = 7 * 24 * time.Hour
)

// VariationsService is the narrow write API that the insights service needs
// from the variations service. The concrete *variations.Service satisfies it.
type VariationsService interface {
	UpsertGlobal(ctx context.Context, payload *variationsgen.UpsertGlobalPayload) (*variationsgen.UpsertGlobalToolVariationResult, error)
	ListGlobal(ctx context.Context, payload *variationsgen.ListGlobalPayload) (*variationsgen.ListVariationsResult, error)
}

// ToolsetsService is the narrow read+write API that the insights service needs
// from the toolsets service. The concrete *toolsets.Service satisfies it.
type ToolsetsService interface {
	GetToolset(ctx context.Context, payload *toolsetsgen.GetToolsetPayload) (*types.Toolset, error)
	UpdateToolset(ctx context.Context, payload *toolsetsgen.UpdateToolsetPayload) (*types.Toolset, error)
}

type Service struct {
	tracer     trace.Tracer
	logger     *slog.Logger
	db         *pgxpool.Pool
	repo       *repo.Queries
	auth       *auth.Auth
	authz      *authz.Engine
	variations VariationsService
	toolsets   ToolsetsService
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	variationsSvc VariationsService,
	toolsetsSvc ToolsetsService,
) *Service {
	logger = logger.With(attr.SlogComponent("insights"))

	return &Service{
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/insights"),
		logger:     logger,
		db:         db,
		repo:       repo.New(db),
		auth:       auth.New(logger, db, sessions, authzEngine),
		authz:      authzEngine,
		variations: variationsSvc,
		toolsets:   toolsetsSvc,
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

// requireAuth pulls the project-scoped auth context.
func (s *Service) requireAuth(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	return authCtx, nil
}

// requireScope enforces an insights scope directly against the authz engine.
// Chat sessions and API keys bypass RBAC in the engine (see
// authz.Engine.ShouldEnforce), which is the seed point for their access — they
// inherit the right to call propose/read from how they authenticate, not from
// a project-scope grant. Session users on enterprise accounts get the insights
// scopes via the SystemRoleGrants table in server/internal/authz/grants.go.
func (s *Service) requireScope(ctx context.Context, projectID string, scope authz.Scope) error {
	return s.authz.Require(ctx, authz.Check{Scope: scope, ResourceID: projectID})
}

// ---- Proposals ----

func (s *Service) ProposeToolVariation(ctx context.Context, payload *gen.ProposeToolVariationPayload) (*gen.ProposalResult, error) {
	authCtx, err := s.requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireScope(ctx, authCtx.ProjectID.String(), authz.ScopeInsightsPropose); err != nil {
		return nil, err
	}

	if payload.ToolName == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "tool_name is required").Log(ctx, s.logger)
	}
	if !json.Valid([]byte(payload.ProposedValue)) {
		return nil, oops.E(oops.CodeInvalid, nil, "proposed_value must be valid JSON").Log(ctx, s.logger)
	}

	// Always snapshot current_value via the SAME live-read path that apply
	// will use later. The agent-supplied current_value is ignored — it tends
	// to come from a different shape (e.g. the tool's effective description
	// from http_tool_definitions) than what variations.ListGlobal returns,
	// which would always-falsely trigger drift on apply ("snapshot of tool
	// description" vs "null variation"). Using liveReadResource here keeps
	// propose-time and apply-time snapshots structurally comparable.
	probe := repo.InsightsProposal{Kind: kindToolVariation, TargetRef: payload.ToolName}
	currentRaw, err := s.liveReadResource(ctx, probe)
	if err != nil {
		return nil, err
	}
	if len(currentRaw) == 0 {
		currentRaw = []byte("null")
	}

	row, err := s.repo.InsertProposal(ctx, repo.InsertProposalParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Kind:           kindToolVariation,
		TargetRef:      payload.ToolName,
		CurrentValue:   currentRaw,
		ProposedValue:  []byte(payload.ProposedValue),
		Reasoning:      conv.PtrToPGText(payload.Reasoning),
		SourceChatID:   parseNullUUID(payload.SourceChatID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error inserting tool variation proposal").Log(ctx, s.logger)
	}

	return &gen.ProposalResult{Proposal: toProposal(row)}, nil
}

func (s *Service) ProposeToolsetChange(ctx context.Context, payload *gen.ProposeToolsetChangePayload) (*gen.ProposalResult, error) {
	authCtx, err := s.requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireScope(ctx, authCtx.ProjectID.String(), authz.ScopeInsightsPropose); err != nil {
		return nil, err
	}

	if payload.ToolsetSlug == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "toolset_slug is required").Log(ctx, s.logger)
	}
	if !json.Valid([]byte(payload.ProposedValue)) {
		return nil, oops.E(oops.CodeInvalid, nil, "proposed_value must be valid JSON").Log(ctx, s.logger)
	}

	// Same propose-time live snapshot pattern as ProposeToolVariation — see
	// the comment there for why we ignore caller-supplied current_value.
	probe := repo.InsightsProposal{Kind: kindToolsetChange, TargetRef: payload.ToolsetSlug}
	currentRaw, err := s.liveReadResource(ctx, probe)
	if err != nil {
		return nil, err
	}
	if len(currentRaw) == 0 {
		currentRaw = []byte("null")
	}

	row, err := s.repo.InsertProposal(ctx, repo.InsertProposalParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Kind:           kindToolsetChange,
		TargetRef:      payload.ToolsetSlug,
		CurrentValue:   currentRaw,
		ProposedValue:  []byte(payload.ProposedValue),
		Reasoning:      conv.PtrToPGText(payload.Reasoning),
		SourceChatID:   parseNullUUID(payload.SourceChatID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error inserting toolset change proposal").Log(ctx, s.logger)
	}

	return &gen.ProposalResult{Proposal: toProposal(row)}, nil
}

func (s *Service) ListProposals(ctx context.Context, payload *gen.ListProposalsPayload) (*gen.ListProposalsResult, error) {
	authCtx, err := s.requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireScope(ctx, authCtx.ProjectID.String(), authz.ScopeInsightsRead); err != nil {
		return nil, err
	}

	statusFilter := ""
	if payload.Status != nil {
		statusFilter = *payload.Status
	}

	rows, err := s.repo.ListProposalsByStatus(ctx, repo.ListProposalsByStatusParams{
		ProjectID:    *authCtx.ProjectID,
		StatusFilter: statusFilter,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing proposals").Log(ctx, s.logger)
	}

	proposals := make([]*types.Proposal, 0, len(rows))
	for _, row := range rows {
		proposals = append(proposals, toProposal(row))
	}

	return &gen.ListProposalsResult{Proposals: proposals}, nil
}

// ApplyProposal applies a pending proposal.
//
// Ordering: we mutate the underlying resource first (via the injected
// variations/toolsets service) and THEN open our own transaction to mark the
// proposal row applied + write the audit log. This two-phase flow means if
// the audit-log/mark-applied step fails after the mutation succeeds, the
// mutation is live but the proposal row remains pending. That is recoverable
// (the user can retry apply, which will be a no-op from the resource's point
// of view or will update the audit trail). The alternative — marking applied
// first and then mutating — is worse because it reports success before the
// change is actually live.
//
// TODO(insights-tx): when variations/toolsets expose transaction-aware write
// methods we can bring both sides under one atomic unit.
func (s *Service) ApplyProposal(ctx context.Context, payload *gen.ApplyProposalPayload) (*gen.ProposalResult, error) {
	authCtx, err := s.requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireScope(ctx, authCtx.ProjectID.String(), authz.ScopeInsightsApply); err != nil {
		return nil, err
	}

	proposalID, err := uuid.Parse(payload.ProposalID)
	if err != nil || proposalID == uuid.Nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid proposal id").Log(ctx, s.logger)
	}

	existing, err := s.repo.GetProposalByID(ctx, repo.GetProposalByIDParams{
		ID:        proposalID,
		ProjectID: *authCtx.ProjectID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "proposal not found").Log(ctx, s.logger)
	} else if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading proposal").Log(ctx, s.logger)
	}

	if existing.Status != "pending" {
		return nil, oops.E(oops.CodeConflict, nil, "proposal is not pending").Log(ctx, s.logger)
	}

	force := payload.Force != nil && *payload.Force

	// Staleness check via live-read, unless force overrides.
	if !force {
		live, liveErr := s.liveReadResource(ctx, existing)
		if liveErr != nil {
			return nil, liveErr
		}
		if !jsonEqual(live, existing.CurrentValue) {
			if err := s.markProposalSuperseded(ctx, proposalID, *authCtx.ProjectID); err != nil {
				return nil, err
			}
			return nil, oops.E(oops.CodeConflict, nil, "proposal is stale; resource has changed since proposal was created").Log(ctx, s.logger)
		}
	}

	// Perform the underlying mutation first. If this fails, no proposal state
	// is changed.
	appliedValue, err := s.applyUnderlyingMutation(ctx, existing, existing.ProposedValue)
	if err != nil {
		return nil, err
	}

	// Then record the apply + audit log in a single tx.
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error beginning transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := s.repo.WithTx(dbtx)

	applied, err := tx.MarkProposalApplied(ctx, repo.MarkProposalAppliedParams{
		AppliedValue:    appliedValue,
		AppliedByUserID: conv.ToPGTextEmpty(authCtx.UserID),
		ID:              proposalID,
		ProjectID:       *authCtx.ProjectID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, oops.E(oops.CodeConflict, err, "proposal could not be applied (state changed)").Log(ctx, s.logger)
	} else if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error marking proposal applied").Log(ctx, s.logger)
	}

	if err := audit.LogInsightsProposalApplied(ctx, dbtx, audit.LogInsightsProposalEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ProposalID:       applied.ID.String(),
		ProposalKind:     applied.Kind,
		TargetRef:        applied.TargetRef,
		BeforeSnapshot:   json.RawMessage(applied.CurrentValue),
		AfterSnapshot:    json.RawMessage(applied.AppliedValue),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error writing apply audit log").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing apply").Log(ctx, s.logger)
	}

	return &gen.ProposalResult{Proposal: toProposal(applied)}, nil
}

// RollbackProposal reverts an applied proposal. Same two-phase ordering as
// ApplyProposal; see its docstring.
func (s *Service) RollbackProposal(ctx context.Context, payload *gen.RollbackProposalPayload) (*gen.ProposalResult, error) {
	authCtx, err := s.requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireScope(ctx, authCtx.ProjectID.String(), authz.ScopeInsightsApply); err != nil {
		return nil, err
	}

	proposalID, err := uuid.Parse(payload.ProposalID)
	if err != nil || proposalID == uuid.Nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid proposal id").Log(ctx, s.logger)
	}

	existing, err := s.repo.GetProposalByID(ctx, repo.GetProposalByIDParams{
		ID:        proposalID,
		ProjectID: *authCtx.ProjectID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "proposal not found").Log(ctx, s.logger)
	} else if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading proposal").Log(ctx, s.logger)
	}

	if existing.Status != "applied" {
		return nil, oops.E(oops.CodeConflict, nil, "proposal is not applied").Log(ctx, s.logger)
	}

	force := payload.Force != nil && *payload.Force

	// Drift check: live state vs applied_value, unless force overrides.
	if !force {
		live, liveErr := s.liveReadResource(ctx, existing)
		if liveErr != nil {
			return nil, liveErr
		}
		if !jsonEqual(live, existing.AppliedValue) {
			return nil, oops.E(oops.CodeConflict, nil, "resource has drifted from applied value; pass force=true to roll back anyway").Log(ctx, s.logger)
		}
	}

	// Perform the underlying revert (writing current_value back).
	if _, err := s.applyUnderlyingMutation(ctx, existing, existing.CurrentValue); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error beginning transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := s.repo.WithTx(dbtx)

	rolled, err := tx.MarkProposalRolledBack(ctx, repo.MarkProposalRolledBackParams{
		RolledBackByUserID: conv.ToPGTextEmpty(authCtx.UserID),
		ID:                 proposalID,
		ProjectID:          *authCtx.ProjectID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, oops.E(oops.CodeConflict, err, "proposal could not be rolled back (state changed)").Log(ctx, s.logger)
	} else if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error marking proposal rolled back").Log(ctx, s.logger)
	}

	if err := audit.LogInsightsProposalRolledBack(ctx, dbtx, audit.LogInsightsProposalEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ProposalID:       rolled.ID.String(),
		ProposalKind:     rolled.Kind,
		TargetRef:        rolled.TargetRef,
		BeforeSnapshot:   json.RawMessage(rolled.AppliedValue),
		AfterSnapshot:    json.RawMessage(rolled.CurrentValue),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error writing rollback audit log").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing rollback").Log(ctx, s.logger)
	}

	return &gen.ProposalResult{Proposal: toProposal(rolled)}, nil
}

func (s *Service) DismissProposal(ctx context.Context, payload *gen.DismissProposalPayload) (*gen.ProposalResult, error) {
	authCtx, err := s.requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireScope(ctx, authCtx.ProjectID.String(), authz.ScopeInsightsApply); err != nil {
		return nil, err
	}

	proposalID, err := uuid.Parse(payload.ProposalID)
	if err != nil || proposalID == uuid.Nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid proposal id").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error beginning transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := s.repo.WithTx(dbtx)

	row, err := tx.MarkProposalDismissed(ctx, repo.MarkProposalDismissedParams{
		DismissedByUserID: conv.ToPGTextEmpty(authCtx.UserID),
		ID:                proposalID,
		ProjectID:         *authCtx.ProjectID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "pending proposal not found").Log(ctx, s.logger)
	} else if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error dismissing proposal").Log(ctx, s.logger)
	}

	if err := audit.LogInsightsProposalDismissed(ctx, dbtx, audit.LogInsightsProposalEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ProposalID:       row.ID.String(),
		ProposalKind:     row.Kind,
		TargetRef:        row.TargetRef,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error writing dismiss audit log").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing dismissal").Log(ctx, s.logger)
	}

	return &gen.ProposalResult{Proposal: toProposal(row)}, nil
}

// markProposalSuperseded is a helper for the staleness-check path.
func (s *Service) markProposalSuperseded(ctx context.Context, proposalID, projectID uuid.UUID) error {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error beginning transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := s.repo.WithTx(dbtx)
	if _, err := tx.MarkProposalSuperseded(ctx, repo.MarkProposalSupersededParams{
		ID:        proposalID,
		ProjectID: projectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error marking proposal superseded").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error committing supersede").Log(ctx, s.logger)
	}
	return nil
}

// liveReadResource returns the current live state of the resource the
// proposal targets, serialized as JSON bytes matching the shape of
// current_value / applied_value. Returns nil bytes on "not found".
func (s *Service) liveReadResource(ctx context.Context, p repo.InsightsProposal) ([]byte, error) {
	switch p.Kind {
	case kindToolVariation:
		if s.variations == nil {
			return nil, oops.E(oops.CodeUnexpected, nil, "variations service not wired").Log(ctx, s.logger)
		}
		res, err := s.variations.ListGlobal(ctx, &variationsgen.ListGlobalPayload{})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error reading live variations").Log(ctx, s.logger)
		}
		var match *types.ToolVariation
		for _, v := range res.Variations {
			if v != nil && v.SrcToolName == p.TargetRef {
				match = v
				break
			}
		}
		return marshalVariationSnapshot(match)
	case kindToolsetChange:
		if s.toolsets == nil {
			return nil, oops.E(oops.CodeUnexpected, nil, "toolsets service not wired").Log(ctx, s.logger)
		}
		slug := types.Slug(p.TargetRef)
		ts, err := s.toolsets.GetToolset(ctx, &toolsetsgen.GetToolsetPayload{Slug: slug})
		if err != nil {
			// Treat not-found as an empty snapshot rather than a hard error;
			// the drift comparison will then flag it as changed.
			return []byte("null"), nil
		}
		return marshalToolsetSnapshot(ts)
	default:
		return nil, oops.E(oops.CodeUnexpected, nil, "unknown proposal kind: %s", p.Kind).Log(ctx, s.logger)
	}
}

// applyUnderlyingMutation performs the actual mutation against the
// variations/toolsets service. Returns the JSON-serialized value that was
// written (for storage in applied_value).
func (s *Service) applyUnderlyingMutation(ctx context.Context, p repo.InsightsProposal, value []byte) ([]byte, error) {
	switch p.Kind {
	case kindToolVariation:
		if s.variations == nil {
			return nil, oops.E(oops.CodeUnexpected, nil, "variations service not wired").Log(ctx, s.logger)
		}
		form, err := decodeVariationForm(value, p.TargetRef)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "could not decode variation form from proposal value").Log(ctx, s.logger)
		}
		res, err := s.variations.UpsertGlobal(ctx, form)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error applying tool variation").Log(ctx, s.logger)
		}
		return marshalVariationSnapshot(res.Variation)
	case kindToolsetChange:
		if s.toolsets == nil {
			return nil, oops.E(oops.CodeUnexpected, nil, "toolsets service not wired").Log(ctx, s.logger)
		}
		form, err := decodeToolsetUpdateForm(value, p.TargetRef)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "could not decode toolset form from proposal value").Log(ctx, s.logger)
		}
		res, err := s.toolsets.UpdateToolset(ctx, form)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error applying toolset change").Log(ctx, s.logger)
		}
		return marshalToolsetSnapshot(res)
	default:
		return nil, oops.E(oops.CodeUnexpected, nil, "unknown proposal kind: %s", p.Kind).Log(ctx, s.logger)
	}
}

// ---- Memories ----

func (s *Service) RememberFact(ctx context.Context, payload *gen.RememberFactPayload) (*gen.MemoryResult, error) {
	authCtx, err := s.requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireScope(ctx, authCtx.ProjectID.String(), authz.ScopeInsightsPropose); err != nil {
		return nil, err
	}

	if payload.Content == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "content is required").Log(ctx, s.logger)
	}
	if len(payload.Content) > 2000 {
		return nil, oops.E(oops.CodeInvalid, nil, "content exceeds 2000 chars").Log(ctx, s.logger)
	}

	tags := payload.Tags
	if tags == nil {
		tags = []string{}
	}

	expiresAt := pgtype.Timestamptz{}
	if payload.Kind == memoryKindFinding {
		expiresAt = pgtype.Timestamptz{Time: time.Now().Add(findingTTL), Valid: true}
	}

	row, err := s.repo.InsertMemory(ctx, repo.InsertMemoryParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Kind:           payload.Kind,
		Content:        payload.Content,
		Tags:           tags,
		SourceChatID:   parseNullUUID(payload.SourceChatID),
		ExpiresAt:      expiresAt,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error inserting memory").Log(ctx, s.logger)
	}

	return &gen.MemoryResult{Memory: toMemory(row)}, nil
}

func (s *Service) RecordFinding(ctx context.Context, payload *gen.RecordFindingPayload) (*gen.MemoryResult, error) {
	return s.RememberFact(ctx, &gen.RememberFactPayload{
		SessionToken:     payload.SessionToken,
		ApikeyToken:      payload.ApikeyToken,
		ProjectSlugInput: payload.ProjectSlugInput,
		Kind:             memoryKindFinding,
		Content:          payload.Content,
		Tags:             payload.Tags,
		SourceChatID:     payload.SourceChatID,
	})
}

func (s *Service) ForgetMemory(ctx context.Context, payload *gen.ForgetMemoryPayload) (*gen.MemoryResult, error) {
	return s.forgetMemoryCommon(ctx, payload.MemoryID, false)
}

func (s *Service) ForgetMemoryByID(ctx context.Context, payload *gen.ForgetMemoryByIDPayload) (*gen.MemoryResult, error) {
	return s.forgetMemoryCommon(ctx, payload.MemoryID, true)
}

func (s *Service) forgetMemoryCommon(ctx context.Context, memoryID string, requireApply bool) (*gen.MemoryResult, error) {
	authCtx, err := s.requireAuth(ctx)
	if err != nil {
		return nil, err
	}

	requiredScope := authz.ScopeInsightsPropose
	if requireApply {
		requiredScope = authz.ScopeInsightsApply
	}
	if err := s.requireScope(ctx, authCtx.ProjectID.String(), requiredScope); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(memoryID)
	if err != nil || id == uuid.Nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid memory id").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error beginning transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := s.repo.WithTx(dbtx)

	row, err := tx.SoftDeleteMemory(ctx, repo.SoftDeleteMemoryParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "memory not found").Log(ctx, s.logger)
	} else if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error deleting memory").Log(ctx, s.logger)
	}

	if err := audit.LogInsightsMemoryForgotten(ctx, dbtx, audit.LogInsightsMemoryForgottenEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		MemoryID:         row.ID.String(),
		MemoryKind:       row.Kind,
		MemoryContent:    row.Content,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error writing forget audit log").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing memory delete").Log(ctx, s.logger)
	}

	return &gen.MemoryResult{Memory: toMemory(row)}, nil
}

func (s *Service) ListMemories(ctx context.Context, payload *gen.ListMemoriesPayload) (*gen.ListMemoriesResult, error) {
	authCtx, err := s.requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireScope(ctx, authCtx.ProjectID.String(), authz.ScopeInsightsRead); err != nil {
		return nil, err
	}

	kindFilter := ""
	if payload.Kind != nil {
		kindFilter = *payload.Kind
	}

	tags := payload.Tags
	if tags == nil {
		tags = []string{}
	}

	limit := defaultMemoryLimit
	if payload.Limit != nil {
		limit = *payload.Limit
	}
	if limit <= 0 {
		limit = defaultMemoryLimit
	}
	if limit > maxMemoryLimit {
		limit = maxMemoryLimit
	}

	rows, err := s.repo.ListMemories(ctx, repo.ListMemoriesParams{
		ProjectID:   *authCtx.ProjectID,
		KindFilter:  kindFilter,
		TagFilter:   tags,
		ResultLimit: int32(limit), //nolint:gosec // bounded above
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing memories").Log(ctx, s.logger)
	}

	memories := make([]*types.Memory, 0, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		memories = append(memories, toMemory(row))
		ids = append(ids, row.ID)
	}

	if len(ids) > 0 {
		// Best-effort recall bump; failure is non-fatal.
		if err := s.repo.BumpMemoryUsage(ctx, repo.BumpMemoryUsageParams{
			Ids:       ids,
			ProjectID: *authCtx.ProjectID,
		}); err != nil {
			s.logger.WarnContext(ctx, "error bumping memory usage", attr.SlogError(err))
		}
	}

	return &gen.ListMemoriesResult{Memories: memories}, nil
}

// ---- Helpers ----

func parseNullUUID(s *string) uuid.NullUUID {
	if s == nil || *s == "" {
		return uuid.NullUUID{}
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: id, Valid: true}
}

// variationForm is the wire-format the agent uses to propose a tool variation
// edit. It is a JSON-serializable subset of variationsgen.UpsertGlobalPayload.
// Fields not set by the agent remain nil (inherit) in the variation row.
type variationForm struct {
	SrcToolURN      string   `json:"src_tool_urn,omitempty"`
	SrcToolName     string   `json:"src_tool_name,omitempty"`
	Confirm         *string  `json:"confirm,omitempty"`
	ConfirmPrompt   *string  `json:"confirm_prompt,omitempty"`
	Name            *string  `json:"name,omitempty"`
	Summary         *string  `json:"summary,omitempty"`
	Description     *string  `json:"description,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Summarizer      *string  `json:"summarizer,omitempty"`
	Title           *string  `json:"title,omitempty"`
	ReadOnlyHint    *bool    `json:"read_only_hint,omitempty"`
	DestructiveHint *bool    `json:"destructive_hint,omitempty"`
	IdempotentHint  *bool    `json:"idempotent_hint,omitempty"`
	OpenWorldHint   *bool    `json:"open_world_hint,omitempty"`
}

func decodeVariationForm(raw []byte, targetRef string) (*variationsgen.UpsertGlobalPayload, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("proposal value is empty")
	}
	var f variationForm
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("decode variation form: %w", err)
	}
	if f.SrcToolURN == "" {
		return nil, fmt.Errorf("src_tool_urn is required in proposed_value")
	}
	name := f.SrcToolName
	if name == "" {
		name = targetRef
	}
	return &variationsgen.UpsertGlobalPayload{
		SrcToolUrn:      f.SrcToolURN,
		SrcToolName:     name,
		Confirm:         f.Confirm,
		ConfirmPrompt:   f.ConfirmPrompt,
		Name:            f.Name,
		Summary:         f.Summary,
		Description:     f.Description,
		Tags:            f.Tags,
		Summarizer:      f.Summarizer,
		Title:           f.Title,
		ReadOnlyHint:    f.ReadOnlyHint,
		DestructiveHint: f.DestructiveHint,
		IdempotentHint:  f.IdempotentHint,
		OpenWorldHint:   f.OpenWorldHint,
	}, nil
}

// marshalVariationSnapshot builds a comparable JSON snapshot of a tool
// variation. Nil returns "null" so jsonEqual works both ways.
func marshalVariationSnapshot(v *types.ToolVariation) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	snap := variationForm{
		SrcToolURN:      v.SrcToolUrn,
		SrcToolName:     v.SrcToolName,
		Confirm:         v.Confirm,
		ConfirmPrompt:   v.ConfirmPrompt,
		Name:            v.Name,
		Description:     v.Description,
		Summarizer:      v.Summarizer,
		Title:           v.Title,
		ReadOnlyHint:    v.ReadOnlyHint,
		DestructiveHint: v.DestructiveHint,
		IdempotentHint:  v.IdempotentHint,
		OpenWorldHint:   v.OpenWorldHint,
	}
	return json.Marshal(snap)
}

// toolsetUpdateForm is the wire-format the agent uses to propose a toolset
// change. Mirrors the subset of toolsetsgen.UpdateToolsetPayload that the spec
// allows (add/remove/rename — no reorder).
type toolsetUpdateForm struct {
	Slug                string   `json:"slug,omitempty"`
	Name                *string  `json:"name,omitempty"`
	ToolURNs            []string `json:"tool_urns,omitempty"`
	ResourceURNs        []string `json:"resource_urns,omitempty"`
	PromptTemplateNames []string `json:"prompt_template_names,omitempty"`
}

func decodeToolsetUpdateForm(raw []byte, targetRef string) (*toolsetsgen.UpdateToolsetPayload, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("proposal value is empty")
	}
	var f toolsetUpdateForm
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("decode toolset form: %w", err)
	}
	slug := f.Slug
	if slug == "" {
		slug = targetRef
	}
	return &toolsetsgen.UpdateToolsetPayload{
		Slug:                types.Slug(slug),
		Name:                f.Name,
		ToolUrns:            f.ToolURNs,
		ResourceUrns:        f.ResourceURNs,
		PromptTemplateNames: f.PromptTemplateNames,
	}, nil
}

// marshalToolsetSnapshot builds a comparable JSON snapshot of a toolset.
func marshalToolsetSnapshot(ts *types.Toolset) ([]byte, error) {
	if ts == nil {
		return []byte("null"), nil
	}
	toolURNs := append([]string(nil), ts.ToolUrns...)
	resourceURNs := append([]string(nil), ts.ResourceUrns...)
	snap := toolsetUpdateForm{
		Slug:         string(ts.Slug),
		Name:         &ts.Name,
		ToolURNs:     toolURNs,
		ResourceURNs: resourceURNs,
	}
	return json.Marshal(snap)
}

// jsonEqual returns true if the two byte slices represent the same JSON value.
func jsonEqual(a, b []byte) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return false
	}
	aa, err := json.Marshal(va)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(vb)
	if err != nil {
		return false
	}
	return string(aa) == string(bb)
}

func toProposal(row repo.InsightsProposal) *types.Proposal {
	p := &types.Proposal{
		ID:                 row.ID.String(),
		Kind:               row.Kind,
		TargetRef:          row.TargetRef,
		CurrentValue:       string(row.CurrentValue),
		ProposedValue:      string(row.ProposedValue),
		Status:             row.Status,
		CreatedAt:          row.CreatedAt.Time.Format(time.RFC3339),
		Reasoning:          conv.FromPGText[string](row.Reasoning),
		AppliedByUserID:    conv.FromPGText[string](row.AppliedByUserID),
		DismissedByUserID:  conv.FromPGText[string](row.DismissedByUserID),
		RolledBackByUserID: conv.FromPGText[string](row.RolledBackByUserID),
	}
	if len(row.AppliedValue) > 0 {
		v := string(row.AppliedValue)
		p.AppliedValue = &v
	}
	if row.SourceChatID.Valid {
		v := row.SourceChatID.UUID.String()
		p.SourceChatID = &v
	}
	if row.AppliedAt.Valid {
		v := row.AppliedAt.Time.Format(time.RFC3339)
		p.AppliedAt = &v
	}
	if row.DismissedAt.Valid {
		v := row.DismissedAt.Time.Format(time.RFC3339)
		p.DismissedAt = &v
	}
	if row.RolledBackAt.Valid {
		v := row.RolledBackAt.Time.Format(time.RFC3339)
		p.RolledBackAt = &v
	}
	return p
}

func toMemory(row repo.InsightsMemory) *types.Memory {
	tags := row.Tags
	if tags == nil {
		tags = []string{}
	}
	m := &types.Memory{
		ID:              row.ID.String(),
		Kind:            row.Kind,
		Content:         row.Content,
		Tags:            tags,
		UsefulnessScore: int(row.UsefulnessScore),
		LastUsedAt:      row.LastUsedAt.Time.Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.Format(time.RFC3339),
	}
	if row.SourceChatID.Valid {
		v := row.SourceChatID.UUID.String()
		m.SourceChatID = &v
	}
	if row.ExpiresAt.Valid {
		v := row.ExpiresAt.Time.Format(time.RFC3339)
		m.ExpiresAt = &v
	}
	return m
}
