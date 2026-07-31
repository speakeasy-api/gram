package collections

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/collections"
	srv "github.com/speakeasy-api/gram/server/gen/http/collections/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/collections/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	mcpmetadataRepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	toolsets  *toolsetsRepo.Queries
	orgRepo   *orgRepo.Queries
	auth      *auth.Auth
	authz     *authz.Engine
	audit     *audit.Logger
	sessions  *sessions.Manager
	serverURL *url.URL
}

const defaultCollectionSlug = "registry"

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine, auditLogger *audit.Logger, serverURL *url.URL) *Service {
	logger = logger.With(attr.SlogComponent("collections"))

	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/collections"),
		logger:    logger,
		db:        db,
		repo:      repo.New(db),
		toolsets:  toolsetsRepo.New(db),
		orgRepo:   orgRepo.New(db),
		auth:      auth.New(logger, db, sessions, authzEngine),
		authz:     authzEngine,
		audit:     auditLogger,
		sessions:  sessions,
		serverURL: serverURL,
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

func (s *Service) Create(ctx context.Context, payload *gen.CreatePayload) (*types.MCPCollection, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collections").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	cr := s.repo.WithTx(dbtx)

	collection, err := cr.CreateOrganizationMcpCollection(ctx, repo.CreateOrganizationMcpCollectionParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           payload.Name,
		Description:    conv.PtrToPGTextEmpty(payload.Description),
		Slug:           payload.Slug,
		Visibility:     payload.Visibility,
	})
	var pgErr *pgconn.PgError
	if err != nil {
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "collection slug already exists")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create collection").LogError(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	_, err = cr.CreateOrganizationMcpCollectionRegistry(ctx, repo.CreateOrganizationMcpCollectionRegistryParams{
		CollectionID:   collection.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Namespace:      payload.McpRegistryNamespace,
	})
	if err != nil {
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "registry namespace already exists")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create collection registry").LogError(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	for _, idStr := range payload.ToolsetIds {
		toolsetID, parseErr := uuid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid toolset_id").LogError(ctx, s.logger)
		}
		backend := serverBackend{toolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, mcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
		if err := s.attachServerToCollection(ctx, cr, collection.ID, backend, authCtx.ActiveOrganizationID, authCtx.UserID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to attach toolset to collection").LogError(ctx, s.logger)
		}
	}

	for _, idStr := range payload.McpServerIds {
		mcpServerID, parseErr := uuid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid mcp_server_id").LogError(ctx, s.logger)
		}
		backend := serverBackend{toolsetID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, mcpServerID: uuid.NullUUID{UUID: mcpServerID, Valid: true}}
		if err := s.attachServerToCollection(ctx, cr, collection.ID, backend, authCtx.ActiveOrganizationID, authCtx.UserID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to attach mcp server to collection").LogError(ctx, s.logger)
		}
	}

	if err := s.audit.LogMcpCollectionCreate(ctx, dbtx, audit.LogMcpCollectionCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		CollectionURN:    urn.NewMcpCollection(collection.ID),
		CollectionName:   collection.Name,
		CollectionSlug:   collection.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log collection creation").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving collection").LogError(ctx, s.logger)
	}

	return toMCPCollection(collection, payload.McpRegistryNamespace), nil
}

func (s *Service) List(ctx context.Context, payload *gen.ListPayload) (*gen.ListResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	orgSlug := authCtx.OrganizationSlug
	if orgSlug == "" {
		orgMeta, err := s.orgRepo.GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error accessing organization metadata").LogError(ctx, s.logger)
		}
		orgSlug = orgMeta.Slug
	}

	if err := s.ensureDefaultRegistryCollection(ctx, authCtx.ActiveOrganizationID, orgSlug); err != nil {
		s.logger.WarnContext(ctx, "failed to ensure default registry collection", attr.SlogError(err), attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	collections, err := s.repo.ListOrganizationMcpCollections(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &gen.ListResult{Collections: []*types.MCPCollection{}}, nil
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list collections").LogError(ctx, s.logger)
	}

	result := make([]*types.MCPCollection, 0, len(collections))
	for _, c := range collections {
		reg, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, repo.GetOrganizationMcpCollectionRegistryByIDParams{
			CollectionID:   c.ID,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection registry").LogError(ctx, s.logger)
		}
		result = append(result, toMCPCollection(repo.CreateOrganizationMcpCollectionRow(c), reg.Namespace))
	}

	return &gen.ListResult{Collections: result}, nil
}

func (s *Service) Update(ctx context.Context, payload *gen.UpdatePayload) (*types.MCPCollection, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	collectionID, err := uuid.Parse(payload.CollectionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid collection_id").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collections").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := s.repo.WithTx(dbtx)

	existing, err := tx.GetOrganizationMcpCollectionByID(ctx, repo.GetOrganizationMcpCollectionByIDParams{
		ID:             collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection").LogError(ctx, s.logger)
	}

	reg, err := tx.GetOrganizationMcpCollectionRegistryByID(ctx, repo.GetOrganizationMcpCollectionRegistryByIDParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection registry").LogError(ctx, s.logger)
	}

	before := toMCPCollection(repo.CreateOrganizationMcpCollectionRow(existing), reg.Namespace)

	updated, err := tx.UpdateOrganizationMcpCollection(ctx, repo.UpdateOrganizationMcpCollectionParams{
		ID:             collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           conv.PtrToPGTextEmpty(payload.Name),
		Description:    conv.PtrToPGTextEmpty(payload.Description),
		Visibility:     conv.PtrToPGTextEmpty(payload.Visibility),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update collection").LogError(ctx, s.logger)
	}

	after := toMCPCollection(repo.CreateOrganizationMcpCollectionRow(updated), reg.Namespace)

	if err := s.audit.LogMcpCollectionUpdate(ctx, dbtx, audit.LogMcpCollectionUpdateEvent{
		OrganizationID:           authCtx.ActiveOrganizationID,
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:         authCtx.Email,
		ActorSlug:                nil,
		CollectionURN:            urn.NewMcpCollection(updated.ID),
		CollectionName:           updated.Name,
		CollectionSlug:           updated.Slug,
		CollectionSnapshotBefore: before,
		CollectionSnapshotAfter:  after,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log collection update").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving collection update").LogError(ctx, s.logger)
	}

	return after, nil
}

func (s *Service) Delete(ctx context.Context, payload *gen.DeletePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	collectionID, err := uuid.Parse(payload.CollectionID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid collection_id").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error accessing collections").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := s.repo.WithTx(dbtx)

	collection, err := tx.GetOrganizationMcpCollectionByID(ctx, repo.GetOrganizationMcpCollectionByIDParams{
		ID:             collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "error accessing collection").LogError(ctx, s.logger)
	}

	if collection.Slug == defaultCollectionSlug {
		return oops.E(oops.CodeInvalid, nil, "cannot delete the default registry collection")
	}

	if err := tx.DeleteOrganizationMcpCollectionRegistriesByID(ctx, repo.DeleteOrganizationMcpCollectionRegistriesByIDParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete collection registries").LogError(ctx, s.logger)
	}
	if err := tx.DeleteOrganizationMcpCollectionServerAttachmentsByID(ctx, repo.DeleteOrganizationMcpCollectionServerAttachmentsByIDParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete collection server attachments").LogError(ctx, s.logger)
	}
	if err := tx.DeleteOrganizationMcpCollection(ctx, repo.DeleteOrganizationMcpCollectionParams{
		ID:             collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete collection").LogError(ctx, s.logger)
	}

	if err := s.audit.LogMcpCollectionDelete(ctx, dbtx, audit.LogMcpCollectionDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		CollectionURN:    urn.NewMcpCollection(collection.ID),
		CollectionName:   collection.Name,
		CollectionSlug:   collection.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log collection deletion").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error saving collection deletion").LogError(ctx, s.logger)
	}

	return nil
}

func (s *Service) AttachServer(ctx context.Context, payload *gen.AttachServerPayload) (*types.MCPCollection, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	collectionID, err := uuid.Parse(payload.CollectionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid collection_id").LogError(ctx, s.logger)
	}

	backend, err := parseServerBackend(payload.ToolsetID, payload.McpServerID)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collections").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := s.repo.WithTx(dbtx)

	collection, err := tx.GetOrganizationMcpCollectionByID(ctx, repo.GetOrganizationMcpCollectionByIDParams{
		ID:             collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection").LogError(ctx, s.logger)
	}

	if err := s.attachServerToCollection(ctx, tx, collectionID, backend, authCtx.ActiveOrganizationID, authCtx.UserID); err != nil {
		return nil, err
	}

	reg, err := tx.GetOrganizationMcpCollectionRegistryByID(ctx, repo.GetOrganizationMcpCollectionRegistryByIDParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection registry").LogError(ctx, s.logger)
	}

	toolsetURN, mcpServerURN := backend.auditURNs()
	if err := s.audit.LogMcpCollectionAttachServer(ctx, dbtx, audit.LogMcpCollectionAttachServerEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		CollectionURN:    urn.NewMcpCollection(collection.ID),
		CollectionName:   collection.Name,
		CollectionSlug:   collection.Slug,
		ToolsetURN:       toolsetURN,
		McpServerURN:     mcpServerURN,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log collection server attachment").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving collection server attachment").LogError(ctx, s.logger)
	}

	return toMCPCollection(repo.CreateOrganizationMcpCollectionRow(collection), reg.Namespace), nil
}

func (s *Service) DetachServer(ctx context.Context, payload *gen.DetachServerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	collectionID, err := uuid.Parse(payload.CollectionID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid collection_id").LogError(ctx, s.logger)
	}

	backend, err := parseServerBackend(payload.ToolsetID, payload.McpServerID)
	if err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error accessing collections").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := s.repo.WithTx(dbtx)

	collection, err := tx.GetOrganizationMcpCollectionByID(ctx, repo.GetOrganizationMcpCollectionByIDParams{
		ID:             collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "error accessing collection").LogError(ctx, s.logger)
	}

	// A toolset reference may be published under either key during the
	// expand/contract swap: as a toolset-keyed attachment (direct mcp_slug
	// publishing) or as a server-keyed attachment on its canonical wrapper.
	// Detach resolves both so callers holding either identity fully unpublish.
	detachToolsetID := backend.toolsetID
	detachMcpServerID := backend.mcpServerID
	if backend.toolsetID.Valid {
		toolset, toolsetErr := s.toolsets.GetToolsetByIDAndOrganization(ctx, toolsetsRepo.GetToolsetByIDAndOrganizationParams{
			ID:             backend.toolsetID.UUID,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		switch {
		case errors.Is(toolsetErr, pgx.ErrNoRows):
			// Toolset gone: only the toolset-keyed attachment can remain.
		case toolsetErr != nil:
			return oops.E(oops.CodeUnexpected, toolsetErr, "error accessing toolset").LogError(ctx, s.logger)
		default:
			detachMcpServerID, err = s.resolveToolsetWrapper(ctx, toolset.ID, toolset.ProjectID)
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "resolve toolset wrapper mcp server").LogError(ctx, s.logger)
			}
		}
	}

	var attached bool
	if detachToolsetID.Valid {
		toolsetAttached, checkErr := tx.IsServerAttachedToOrganizationMcpCollection(ctx, repo.IsServerAttachedToOrganizationMcpCollectionParams{
			CollectionID:   collectionID,
			OrganizationID: authCtx.ActiveOrganizationID,
			ToolsetID:      detachToolsetID,
		})
		if checkErr != nil {
			return oops.E(oops.CodeUnexpected, checkErr, "error checking collection server attachment").LogError(ctx, s.logger)
		}
		attached = attached || toolsetAttached
	}
	if detachMcpServerID.Valid {
		serverAttached, checkErr := tx.IsMcpServerAttachedToOrganizationMcpCollection(ctx, repo.IsMcpServerAttachedToOrganizationMcpCollectionParams{
			CollectionID:   collectionID,
			OrganizationID: authCtx.ActiveOrganizationID,
			McpServerID:    detachMcpServerID,
		})
		if checkErr != nil {
			return oops.E(oops.CodeUnexpected, checkErr, "error checking collection server attachment").LogError(ctx, s.logger)
		}
		attached = attached || serverAttached
	}
	if !attached {
		return nil
	}

	if detachToolsetID.Valid {
		if err := tx.DetachServerFromOrganizationMcpCollection(ctx, repo.DetachServerFromOrganizationMcpCollectionParams{
			CollectionID:   collectionID,
			OrganizationID: authCtx.ActiveOrganizationID,
			ToolsetID:      detachToolsetID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to detach server from collection").LogError(ctx, s.logger)
		}
	}
	if detachMcpServerID.Valid {
		if err := tx.DetachMcpServerFromOrganizationMcpCollection(ctx, repo.DetachMcpServerFromOrganizationMcpCollectionParams{
			CollectionID:   collectionID,
			OrganizationID: authCtx.ActiveOrganizationID,
			McpServerID:    detachMcpServerID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to detach server from collection").LogError(ctx, s.logger)
		}
	}

	toolsetURN, mcpServerURN := backend.auditURNs()
	if err := s.audit.LogMcpCollectionDetachServer(ctx, dbtx, audit.LogMcpCollectionDetachServerEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		CollectionURN:    urn.NewMcpCollection(collection.ID),
		CollectionName:   collection.Name,
		CollectionSlug:   collection.Slug,
		ToolsetURN:       toolsetURN,
		McpServerURN:     mcpServerURN,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log collection server detachment").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error saving collection server detachment").LogError(ctx, s.logger)
	}

	return nil
}

func (s *Service) ListServers(ctx context.Context, payload *gen.ListServersPayload) (*gen.ListServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	collection, err := s.repo.GetOrganizationMcpCollectionBySlugAndOrg(ctx, repo.GetOrganizationMcpCollectionBySlugAndOrgParams{
		Slug:           payload.CollectionSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection").LogError(ctx, s.logger)
	}

	registry, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, repo.GetOrganizationMcpCollectionRegistryByIDParams{
		CollectionID:   collection.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection registry").LogError(ctx, s.logger)
	}

	// Every published toolset resolves through its wrapper mcp_server, so
	// server-keyed attachments fully cover wrapped toolsets (the attach path
	// rekeys toolset-keyed rows in place on republish).
	mcpServerRows, err := s.repo.ListOrganizationMcpCollectionMcpServerAttachments(ctx, repo.ListOrganizationMcpCollectionMcpServerAttachmentsParams{
		CollectionID:   collection.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list collection mcp servers").LogError(ctx, s.logger)
	}

	collectionRegistryIDStr := registry.ID.String()
	mcpMetaRepo := mcpmetadataRepo.New(s.db)

	entries := make([]collectionServerEntry, 0, len(mcpServerRows))
	for _, m := range mcpServerRows {
		// Custom-domain endpoints are served from the domain host; platform
		// endpoints from the Gram server URL. The query already resolved the
		// single preferred endpoint per server.
		var remoteURL string
		if m.EndpointCustomDomain.Valid && m.EndpointCustomDomain.String != "" {
			remoteURL = (&url.URL{Scheme: "https", Host: m.EndpointCustomDomain.String}).JoinPath("mcp", m.EndpointSlug).String()
		} else {
			remoteURL = s.serverURL.JoinPath("mcp", m.EndpointSlug).String()
		}

		specifier := m.EndpointSlug
		if registry.Namespace != "" {
			specifier = path.Join(registry.Namespace, m.EndpointSlug)
		}

		title := m.McpServerSlug.String
		if m.McpServerName.Valid && m.McpServerName.String != "" {
			title = m.McpServerName.String
		}

		// Remote- and tunneled-backed servers authenticate via their user
		// session issuer (OAuth), so there are no static headers for the
		// client to collect. A toolset-backed server publishes with toolset
		// semantics instead: visibility drives the Gram key/environment
		// headers and user env configs come from its (server- or
		// toolset-keyed) metadata.
		description := ""
		var toolsetID *string
		remoteHeaders := []*types.ExternalMCPRemoteHeader{}
		if m.BackingToolsetID.Valid {
			description = conv.FromPGTextOrEmpty[string](m.BackingToolsetDescription)
			toolsetID = conv.PtrEmpty(m.BackingToolsetID.UUID.String())
			isPublic := m.McpServerVisibility == mcpservers.VisibilityPublic
			remoteHeaders, err = s.collectionRemoteHeaders(ctx, mcpMetaRepo, uuid.NullUUID{UUID: m.McpServerID, Valid: true}, m.BackingToolsetID, isPublic)
			if err != nil {
				return nil, err
			}
		}

		mcpServerID := m.McpServerID.String()
		entries = append(entries, collectionServerEntry{
			publishedAt: m.PublishedAt.Time,
			tiebreak:    mcpServerID,
			server: &types.ExternalMCPServer{
				RegistrySpecifier:                   specifier,
				Version:                             "1.0.0",
				Description:                         description,
				ToolsetID:                           toolsetID,
				McpServerID:                         &mcpServerID,
				RegistryID:                          nil,
				OrganizationMcpCollectionRegistryID: &collectionRegistryIDStr,
				Title:                               &title,
				IconURL:                             nil,
				Meta:                                nil,
				Tools:                               nil,
				Remotes: []*types.ExternalMCPRemote{{
					URL:           remoteURL,
					TransportType: "streamable-http",
					Headers:       remoteHeaders,
				}},
			},
		})
	}

	// Global ordering: newest published first, breaking ties on the backend id
	// descending so the order is deterministic across both sources.
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].publishedAt.Equal(entries[j].publishedAt) {
			return entries[i].publishedAt.After(entries[j].publishedAt)
		}
		return entries[i].tiebreak > entries[j].tiebreak
	})

	servers := make([]*types.ExternalMCPServer, 0, len(entries))
	for _, e := range entries {
		servers = append(servers, e.server)
	}

	return &gen.ListServersResult{Servers: servers}, nil
}

// collectionServerEntry pairs a built ExternalMCPServer with the sort keys used
// to merge toolset-backed and mcp_server-backed attachments into one stream.
type collectionServerEntry struct {
	server      *types.ExternalMCPServer
	publishedAt time.Time
	tiebreak    string
}

// collectionRemoteHeaders derives the client-facing headers for a published
// toolset (or its wrapper mcp_server). Metadata is read server-keyed first
// when a wrapper id is known, then toolset-keyed — both keys are live during
// the expand/contract publishing swap.
func (s *Service) collectionRemoteHeaders(ctx context.Context, mcpMetaRepo *mcpmetadataRepo.Queries, mcpServerID, toolsetID uuid.NullUUID, mcpIsPublic bool) ([]*types.ExternalMCPRemoteHeader, error) {
	headers := make([]*types.ExternalMCPRemoteHeader, 0)

	if !mcpIsPublic {
		headers = append(headers,
			collectionRemoteHeader("gram_environment", "gram-environment", false),
			collectionRemoteHeader("authorization", "gram-key", true),
		)
	}

	var metadataRecord mcpmetadataRepo.McpMetadatum
	found := false
	if mcpServerID.Valid {
		record, err := mcpMetaRepo.GetMetadataByMcpServerID(ctx, mcpServerID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Fall through to the toolset-keyed row.
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "load mcp metadata for collection server").LogError(ctx, s.logger)
		default:
			metadataRecord = record
			found = true
		}
	}
	if !found && toolsetID.Valid {
		record, err := mcpMetaRepo.GetMetadataForToolset(ctx, toolsetID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return headers, nil
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "load mcp metadata for collection server").LogError(ctx, s.logger)
		default:
			metadataRecord = record
			found = true
		}
	}
	if !found {
		return headers, nil
	}

	metadata, err := mcpmetadata.ToMCPMetadata(ctx, mcpMetaRepo, metadataRecord)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "convert mcp metadata for collection server").LogError(ctx, s.logger)
	}

	for _, config := range metadata.EnvironmentConfigs {
		if config.ProvidedBy != "user" {
			continue
		}

		displayName := fmt.Sprintf("MCP-%s", strings.ReplaceAll(config.VariableName, "_", "-"))
		if config.HeaderDisplayName != nil {
			if customDisplayName := strings.TrimSpace(*config.HeaderDisplayName); customDisplayName != "" {
				displayName = customDisplayName
			}
		}

		headers = append(headers, collectionRemoteHeader(fmt.Sprintf("MCP-%s", config.VariableName), displayName, true))
	}

	return headers, nil
}

func collectionRemoteHeader(systemName, displayName string, sensitive bool) *types.ExternalMCPRemoteHeader {
	placeholderName := toolconfig.ToPosixName(displayName)
	description := fmt.Sprintf("Set from %s", placeholderName)
	placeholder := fmt.Sprintf("${%s}", placeholderName)
	var isSecret *bool
	if sensitive {
		isSecret = conv.PtrEmpty(true)
	}

	return &types.ExternalMCPRemoteHeader{
		Name:        toolconfig.ToHTTPHeader(systemName),
		Description: &description,
		IsSecret:    isSecret,
		IsRequired:  conv.PtrEmpty(true),
		Placeholder: &placeholder,
	}
}

// serverBackend identifies which backend a collection attachment targets.
// Exactly one of toolsetID / mcpServerID is Valid, mirroring the
// toolset_id XOR mcp_server_id attachment row.
type serverBackend struct {
	toolsetID   uuid.NullUUID
	mcpServerID uuid.NullUUID
}

// parseServerBackend validates that exactly one of toolset_id / mcp_server_id
// was supplied and parses the provided id. The XOR is enforced here in the
// handler because the Goa payload accepts both as optional for wire-compat.
func parseServerBackend(toolsetID, mcpServerID *string) (serverBackend, error) {
	hasToolset := toolsetID != nil && *toolsetID != ""
	hasMcpServer := mcpServerID != nil && *mcpServerID != ""

	switch {
	case hasToolset == hasMcpServer:
		return serverBackend{}, oops.E(oops.CodeBadRequest, nil, "provide exactly one of toolset_id or mcp_server_id")
	case hasToolset:
		id, err := uuid.Parse(*toolsetID)
		if err != nil {
			return serverBackend{}, oops.E(oops.CodeBadRequest, err, "invalid toolset_id")
		}
		return serverBackend{toolsetID: uuid.NullUUID{UUID: id, Valid: true}, mcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	default:
		id, err := uuid.Parse(*mcpServerID)
		if err != nil {
			return serverBackend{}, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id")
		}
		return serverBackend{toolsetID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, mcpServerID: uuid.NullUUID{UUID: id, Valid: true}}, nil
	}
}

// auditURNs returns the subject URN for whichever backend is set, leaving the
// other nil, for the backend-aware collection attach/detach audit events.
func (b serverBackend) auditURNs() (*urn.Toolset, *urn.McpServer) {
	if b.mcpServerID.Valid {
		u := urn.NewMcpServer(b.mcpServerID.UUID)
		return nil, &u
	}
	u := urn.NewToolset(b.toolsetID.UUID)
	return &u, nil
}

func (s *Service) attachServerToCollection(ctx context.Context, queries *repo.Queries, collectionID uuid.UUID, backend serverBackend, organizationID, userID string) error {
	if backend.mcpServerID.Valid {
		return s.attachMcpServerToCollection(ctx, queries, collectionID, backend.mcpServerID.UUID, organizationID, userID)
	}

	toolsetID := backend.toolsetID.UUID
	toolset, err := s.toolsets.GetToolsetByIDAndOrganization(ctx, toolsetsRepo.GetToolsetByIDAndOrganizationParams{
		ID:             toolsetID,
		OrganizationID: organizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "error accessing toolset").LogError(ctx, s.logger)
	}

	// A toolset publishes through its wrapper mcp_server: resolve the
	// reference and write a server-keyed attachment. A toolset without a
	// wrapper has no serving address and cannot be attached.
	wrapperID, err := s.resolveToolsetWrapper(ctx, toolsetID, toolset.ProjectID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve toolset wrapper mcp server").LogError(ctx, s.logger)
	}
	if !wrapperID.Valid {
		return oops.E(oops.CodeInvalid, nil, "cannot attach a toolset that is not enabled as an MCP server").LogError(ctx, s.logger)
	}

	return s.attachWrappedToolsetToCollection(ctx, queries, collectionID, toolsetID, wrapperID.UUID, organizationID, userID)
}

// resolveToolsetWrapper returns the toolset's canonical wrapper: its single
// live toolset-backed mcp_servers row. Zero wrappers (not yet wrapped) or
// multiple wrappers (ambiguous — never guess which owns the published
// address) both resolve to an invalid id, keeping the toolset-keyed path.
func (s *Service) resolveToolsetWrapper(ctx context.Context, toolsetID, projectID uuid.UUID) (uuid.NullUUID, error) {
	wrappers, err := mcpserversRepo.New(s.db).GetMCPServersByToolsetID(ctx, mcpserversRepo.GetMCPServersByToolsetIDParams{
		ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true},
		ProjectID: projectID,
	})
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, fmt.Errorf("list toolset wrapper mcp servers: %w", err)
	}
	switch len(wrappers) {
	case 0:
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	case 1:
		return uuid.NullUUID{UUID: wrappers[0].ID, Valid: true}, nil
	default:
		s.logger.WarnContext(ctx, "toolset has multiple live wrapper mcp servers; keeping toolset-keyed collection publishing",
			attr.SlogToolsetID(toolsetID.String()),
		)
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
}

// attachWrappedToolsetToCollection publishes a wrapped toolset by its wrapper
// mcp_server. A live toolset-keyed attachment for the same collection is
// rekeyed in place first (preserving publish history) so the collection can
// never list the same toolset twice under both keys.
func (s *Service) attachWrappedToolsetToCollection(ctx context.Context, queries *repo.Queries, collectionID, toolsetID, mcpServerID uuid.UUID, organizationID, userID string) error {
	server, err := s.repo.GetMcpServerForOrganizationAttachment(ctx, repo.GetMcpServerForOrganizationAttachmentParams{
		McpServerID:    mcpServerID,
		OrganizationID: organizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "error accessing toolset wrapper mcp server").LogError(ctx, s.logger)
	}
	if server.Visibility == mcpservers.VisibilityDisabled || !server.HasEndpoint {
		return oops.E(oops.CodeInvalid, nil, "cannot attach a toolset that is not enabled as an MCP server").LogError(ctx, s.logger)
	}

	serverAttached, err := queries.IsMcpServerAttachedToOrganizationMcpCollection(ctx, repo.IsMcpServerAttachedToOrganizationMcpCollectionParams{
		CollectionID:   collectionID,
		OrganizationID: organizationID,
		McpServerID:    uuid.NullUUID{UUID: mcpServerID, Valid: true},
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error checking collection server attachment").LogError(ctx, s.logger)
	}
	if serverAttached {
		// The wrapper already holds the live attachment; tombstone any leftover
		// toolset-keyed duplicate before the republish upsert below.
		if err := queries.DetachServerFromOrganizationMcpCollection(ctx, repo.DetachServerFromOrganizationMcpCollectionParams{
			CollectionID:   collectionID,
			OrganizationID: organizationID,
			ToolsetID:      uuid.NullUUID{UUID: toolsetID, Valid: true},
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to detach toolset-keyed collection attachment").LogError(ctx, s.logger)
		}
	} else {
		if _, err := queries.MoveCollectionAttachmentToMcpServer(ctx, repo.MoveCollectionAttachmentToMcpServerParams{
			CollectionID:   collectionID,
			OrganizationID: organizationID,
			McpServerID:    uuid.NullUUID{UUID: mcpServerID, Valid: true},
			ToolsetID:      uuid.NullUUID{UUID: toolsetID, Valid: true},
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to move collection attachment to mcp server").LogError(ctx, s.logger)
		}
	}

	if _, err := queries.AttachMcpServerToOrganizationMcpCollection(ctx, repo.AttachMcpServerToOrganizationMcpCollectionParams{
		CollectionID:   collectionID,
		OrganizationID: organizationID,
		McpServerID:    uuid.NullUUID{UUID: mcpServerID, Valid: true},
		PublishedBy:    conv.PtrToPGTextEmpty(&userID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to attach mcp server to collection").LogError(ctx, s.logger)
	}

	return nil
}

func (s *Service) attachMcpServerToCollection(ctx context.Context, queries *repo.Queries, collectionID, mcpServerID uuid.UUID, organizationID, userID string) error {
	server, err := s.repo.GetMcpServerForOrganizationAttachment(ctx, repo.GetMcpServerForOrganizationAttachmentParams{
		McpServerID:    mcpServerID,
		OrganizationID: organizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "error accessing mcp server").LogError(ctx, s.logger)
	}
	if server.Visibility == mcpservers.VisibilityDisabled || !server.HasEndpoint {
		return oops.E(oops.CodeInvalid, nil, "cannot attach an mcp server that is disabled or has no endpoint").LogError(ctx, s.logger)
	}

	_, err = queries.AttachMcpServerToOrganizationMcpCollection(ctx, repo.AttachMcpServerToOrganizationMcpCollectionParams{
		CollectionID:   collectionID,
		OrganizationID: organizationID,
		McpServerID:    uuid.NullUUID{UUID: mcpServerID, Valid: true},
		PublishedBy:    conv.PtrToPGTextEmpty(&userID),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return oops.E(oops.CodeConflict, err, "mcp server already attached to collection").LogError(ctx, s.logger)
		}
		return oops.E(oops.CodeUnexpected, err, "failed to attach mcp server to collection").LogError(ctx, s.logger)
	}

	return nil
}

func (s *Service) ensureDefaultRegistryCollection(ctx context.Context, organizationID, organizationSlug string) error {
	const (
		defaultCollectionName = "Registry"
		defaultVisibility     = "private"
	)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error starting transaction for default collection").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tx := s.repo.WithTx(dbtx)
	collection, err := tx.EnsureOrganizationMcpCollection(ctx, repo.EnsureOrganizationMcpCollectionParams{
		OrganizationID: organizationID,
		Name:           defaultCollectionName,
		Description:    pgtype.Text{String: "", Valid: false},
		Slug:           defaultCollectionSlug,
		Visibility:     defaultVisibility,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error ensuring default registry collection").LogError(ctx, s.logger)
	}

	if _, err := tx.EnsureOrganizationMcpCollectionRegistry(ctx, repo.EnsureOrganizationMcpCollectionRegistryParams{
		CollectionID:   collection.ID,
		OrganizationID: organizationID,
		Namespace:      defaultRegistryNamespace(organizationSlug, organizationID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error ensuring default registry namespace").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error committing default registry collection").LogError(ctx, s.logger)
	}

	return nil
}

func defaultRegistryNamespace(organizationSlug, organizationID string) string {
	suffix := strings.ToLower(strings.TrimSpace(organizationSlug))
	if suffix == "" {
		suffix = strings.ReplaceAll(strings.ToLower(organizationID), "-", "")
	}

	return fmt.Sprintf("com.speakeasy.%s.registry", suffix)
}

func toMCPCollection(c repo.CreateOrganizationMcpCollectionRow, namespace string) *types.MCPCollection {
	return &types.MCPCollection{
		ID:                   c.ID.String(),
		Name:                 c.Name,
		Description:          conv.FromPGText[string](c.Description),
		Slug:                 c.Slug,
		McpRegistryNamespace: conv.PtrEmpty(namespace),
		Visibility:           c.Visibility,
	}
}
