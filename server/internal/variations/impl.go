package variations

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/variations/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/variations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/variations/repo"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("variations"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/variations"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
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

func (s *Service) ListGlobal(ctx context.Context, payload *gen.ListGlobalPayload) (res *gen.ListVariationsResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	rows, err := s.repo.ListGlobalToolVariations(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing global tool variations").Log(ctx, s.logger)
	}

	variations := make([]*types.ToolVariation, 0, len(rows))
	for _, row := range rows {
		variations = append(variations, &types.ToolVariation{
			ID:              row.ToolVariation.ID.String(),
			GroupID:         row.ToolVariation.GroupID.String(),
			SrcToolUrn:      row.ToolVariation.SrcToolUrn.String(),
			SrcToolName:     row.ToolVariation.SrcToolName,
			Confirm:         conv.FromPGText[string](row.ToolVariation.Confirm),
			ConfirmPrompt:   conv.FromPGText[string](row.ToolVariation.ConfirmPrompt),
			Name:            conv.FromPGText[string](row.ToolVariation.Name),
			Description:     conv.FromPGText[string](row.ToolVariation.Description),
			Summarizer:      conv.FromPGText[string](row.ToolVariation.Summarizer),
			Title:           conv.FromPGText[string](row.ToolVariation.Title),
			ReadOnlyHint:    conv.FromPGBool[bool](row.ToolVariation.ReadOnlyHint),
			DestructiveHint: conv.FromPGBool[bool](row.ToolVariation.DestructiveHint),
			IdempotentHint:  conv.FromPGBool[bool](row.ToolVariation.IdempotentHint),
			OpenWorldHint:   conv.FromPGBool[bool](row.ToolVariation.OpenWorldHint),
			CreatedAt:       row.ToolVariation.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:       row.ToolVariation.UpdatedAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListVariationsResult{
		Variations: variations,
	}, nil
}

func (s *Service) UpsertGlobal(ctx context.Context, payload *gen.UpsertGlobalPayload) (res *gen.UpsertGlobalToolVariationResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger
	projectID := *authCtx.ProjectID

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error beginning transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)

	groupID, err := tx.PokeGlobalToolVariationsGroup(ctx, projectID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "error poking global tool variations group").Log(ctx, logger)
	}

	if errors.Is(err, pgx.ErrNoRows) || groupID == uuid.Nil {
		groupID, err = tx.InitGlobalToolVariationsGroup(ctx, repo.InitGlobalToolVariationsGroupParams{
			ProjectID:   projectID,
			Name:        "Global tool variations",
			Description: conv.ToPGTextEmpty(""),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error initializing global tool variations group").Log(ctx, logger)
		}
	}

	srcToolUrn, err := urn.ParseTool(payload.SrcToolUrn)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid source tool urn").Log(ctx, logger)
	}

	existingVariations, err := tx.FindGlobalVariationsByToolURNs(ctx, repo.FindGlobalVariationsByToolURNsParams{
		ProjectID: projectID,
		ToolUrns:  []string{srcToolUrn.String()},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error finding existing global tool variations").Log(ctx, logger)
	}

	var existing *types.ToolVariation
	switch len(existingVariations) {
	case 0:
		// No existing variation, will create new one.
	case 1:
		existing = toVariation(existingVariations[0])
	default:
		// Multiple existing variations with the same source tool urn is unexpected, will log a warning and update the first one.
		existing = toVariation(existingVariations[0])
		logger.WarnContext(
			ctx,
			"multiple active global tool variations found with same source tool urn",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogProjectID(projectID.String()),
			attr.SlogToolURN(srcToolUrn.String()),
		)
	}

	row, err := tx.UpsertToolVariation(ctx, repo.UpsertToolVariationParams{
		GroupID:         groupID,
		SrcToolUrn:      srcToolUrn,
		SrcToolName:     payload.SrcToolName,
		Confirm:         conv.PtrToPGText(payload.Confirm),
		ConfirmPrompt:   conv.PtrToPGText(payload.ConfirmPrompt),
		Name:            conv.PtrToPGText(payload.Name),
		Summary:         conv.PtrToPGText(payload.Summary),
		Description:     conv.PtrToPGText(payload.Description),
		Tags:            payload.Tags,
		Summarizer:      conv.PtrToPGText(payload.Summarizer),
		Title:           conv.PtrToPGText(payload.Title),
		ReadOnlyHint:    conv.PtrToPGBool(payload.ReadOnlyHint),
		DestructiveHint: conv.PtrToPGBool(payload.DestructiveHint),
		IdempotentHint:  conv.PtrToPGBool(payload.IdempotentHint),
		OpenWorldHint:   conv.PtrToPGBool(payload.OpenWorldHint),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error upserting global tool variation").Log(ctx, logger)
	}

	result := toVariation(row)

	if err := s.audit.LogVariationUpdateGlobal(ctx, dbtx, audit.LogVariationUpdateGlobalEvent{
		OrganizationID:          authCtx.ActiveOrganizationID,
		ProjectID:               uuid.NullUUID{UUID: projectID, Valid: true},
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:        authCtx.Email,
		ActorSlug:               nil,
		VariationURN:            urn.NewVariation(urn.VariationKindGlobal, row.ID),
		SourceToolURN:           srcToolUrn,
		VariationSnapshotBefore: existing,
		VariationSnapshotAfter:  result,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating global tool variation audit log").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing transaction").Log(ctx, logger)
	}

	return &gen.UpsertGlobalToolVariationResult{Variation: result}, nil
}

func (s *Service) DeleteGlobal(ctx context.Context, payload *gen.DeleteGlobalPayload) (*gen.DeleteGlobalToolVariationResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	variationID, err := uuid.Parse(payload.VariationID)
	if err != nil || variationID == uuid.Nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid variation ID").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing variations").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	vr := s.repo.WithTx(dbtx)

	row, err := vr.DeleteGlobalToolVariation(ctx, repo.DeleteGlobalToolVariationParams{
		ID:        variationID,
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "global tool variation not found").Log(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error deleting global tool variation").Log(ctx, s.logger)
	}

	if err := s.audit.LogVariationDeleteGlobal(ctx, dbtx, audit.LogVariationDeleteGlobalEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		VariationURN:     urn.NewVariation(urn.VariationKindGlobal, row.ID),
		SourceToolURN:    row.SrcToolUrn,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating global tool variation deletion audit log").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving variation deletion").Log(ctx, s.logger)
	}

	return &gen.DeleteGlobalToolVariationResult{
		VariationID: row.ID.String(),
	}, nil
}

func toVariation(row repo.ToolVariation) *types.ToolVariation {
	return &types.ToolVariation{
		ID:              row.ID.String(),
		GroupID:         row.GroupID.String(),
		SrcToolUrn:      row.SrcToolUrn.String(),
		SrcToolName:     row.SrcToolName,
		Confirm:         conv.FromPGText[string](row.Confirm),
		ConfirmPrompt:   conv.FromPGText[string](row.ConfirmPrompt),
		Name:            conv.FromPGText[string](row.Name),
		Description:     conv.FromPGText[string](row.Description),
		Summarizer:      conv.FromPGText[string](row.Summarizer),
		Title:           conv.FromPGText[string](row.Title),
		ReadOnlyHint:    conv.FromPGBool[bool](row.ReadOnlyHint),
		DestructiveHint: conv.FromPGBool[bool](row.DestructiveHint),
		IdempotentHint:  conv.FromPGBool[bool](row.IdempotentHint),
		OpenWorldHint:   conv.FromPGBool[bool](row.OpenWorldHint),
		CreatedAt:       row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
