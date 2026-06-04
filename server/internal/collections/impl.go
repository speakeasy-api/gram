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
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collections").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create collection").Log(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create collection registry").Log(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	for _, idStr := range payload.ToolsetIds {
		toolsetID, parseErr := uuid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid toolset_id").Log(ctx, s.logger)
		}
		backend := serverBackend{toolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, mcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
		if err := s.attachServerToCollection(ctx, cr, collection.ID, backend, authCtx.ActiveOrganizationID, authCtx.UserID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to attach toolset to collection").Log(ctx, s.logger)
		}
	}

	for _, idStr := range payload.McpServerIds {
		mcpServerID, parseErr := uuid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid mcp_server_id").Log(ctx, s.logger)
		}
		backend := serverBackend{toolsetID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, mcpServerID: uuid.NullUUID{UUID: mcpServerID, Valid: true}}
		if err := s.attachServerToCollection(ctx, cr, collection.ID, backend, authCtx.ActiveOrganizationID, authCtx.UserID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to attach mcp server to collection").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "log collection creation").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving collection").Log(ctx, s.logger)
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
			return nil, oops.E(oops.CodeUnexpected, err, "error accessing organization metadata").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list collections").Log(ctx, s.logger)
	}

	result := make([]*types.MCPCollection, 0, len(collections))
	for _, c := range collections {
		reg, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, repo.GetOrganizationMcpCollectionRegistryByIDParams{
			CollectionID:   c.ID,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection registry").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid collection_id").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collections").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection").Log(ctx, s.logger)
	}

	reg, err := tx.GetOrganizationMcpCollectionRegistryByID(ctx, repo.GetOrganizationMcpCollectionRegistryByIDParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection registry").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update collection").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "log collection update").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving collection update").Log(ctx, s.logger)
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
		return oops.E(oops.CodeBadRequest, err, "invalid collection_id").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error accessing collections").Log(ctx, s.logger)
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
		return oops.E(oops.CodeUnexpected, err, "error accessing collection").Log(ctx, s.logger)
	}

	if collection.Slug == defaultCollectionSlug {
		return oops.E(oops.CodeInvalid, nil, "cannot delete the default registry collection")
	}

	if err := tx.DeleteOrganizationMcpCollectionRegistriesByID(ctx, repo.DeleteOrganizationMcpCollectionRegistriesByIDParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete collection registries").Log(ctx, s.logger)
	}
	if err := tx.DeleteOrganizationMcpCollectionServerAttachmentsByID(ctx, repo.DeleteOrganizationMcpCollectionServerAttachmentsByIDParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete collection server attachments").Log(ctx, s.logger)
	}
	if err := tx.DeleteOrganizationMcpCollection(ctx, repo.DeleteOrganizationMcpCollectionParams{
		ID:             collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete collection").Log(ctx, s.logger)
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
		return oops.E(oops.CodeUnexpected, err, "log collection deletion").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error saving collection deletion").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid collection_id").Log(ctx, s.logger)
	}

	backend, err := parseServerBackend(payload.ToolsetID, payload.McpServerID)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collections").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection").Log(ctx, s.logger)
	}

	if err := s.attachServerToCollection(ctx, tx, collectionID, backend, authCtx.ActiveOrganizationID, authCtx.UserID); err != nil {
		return nil, err
	}

	reg, err := tx.GetOrganizationMcpCollectionRegistryByID(ctx, repo.GetOrganizationMcpCollectionRegistryByIDParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection registry").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "log collection server attachment").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving collection server attachment").Log(ctx, s.logger)
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
		return oops.E(oops.CodeBadRequest, err, "invalid collection_id").Log(ctx, s.logger)
	}

	backend, err := parseServerBackend(payload.ToolsetID, payload.McpServerID)
	if err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error accessing collections").Log(ctx, s.logger)
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
		return oops.E(oops.CodeUnexpected, err, "error accessing collection").Log(ctx, s.logger)
	}

	var attached bool
	if backend.mcpServerID.Valid {
		attached, err = tx.IsMcpServerAttachedToOrganizationMcpCollection(ctx, repo.IsMcpServerAttachedToOrganizationMcpCollectionParams{
			CollectionID:   collectionID,
			OrganizationID: authCtx.ActiveOrganizationID,
			McpServerID:    backend.mcpServerID,
		})
	} else {
		attached, err = tx.IsServerAttachedToOrganizationMcpCollection(ctx, repo.IsServerAttachedToOrganizationMcpCollectionParams{
			CollectionID:   collectionID,
			OrganizationID: authCtx.ActiveOrganizationID,
			ToolsetID:      backend.toolsetID,
		})
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error checking collection server attachment").Log(ctx, s.logger)
	}
	if !attached {
		return nil
	}

	if backend.mcpServerID.Valid {
		err = tx.DetachMcpServerFromOrganizationMcpCollection(ctx, repo.DetachMcpServerFromOrganizationMcpCollectionParams{
			CollectionID:   collectionID,
			OrganizationID: authCtx.ActiveOrganizationID,
			McpServerID:    backend.mcpServerID,
		})
	} else {
		err = tx.DetachServerFromOrganizationMcpCollection(ctx, repo.DetachServerFromOrganizationMcpCollectionParams{
			CollectionID:   collectionID,
			OrganizationID: authCtx.ActiveOrganizationID,
			ToolsetID:      backend.toolsetID,
		})
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to detach server from collection").Log(ctx, s.logger)
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
		return oops.E(oops.CodeUnexpected, err, "log collection server detachment").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error saving collection server detachment").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection").Log(ctx, s.logger)
	}

	registry, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, repo.GetOrganizationMcpCollectionRegistryByIDParams{
		CollectionID:   collection.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection registry").Log(ctx, s.logger)
	}

	toolsets, err := s.repo.ListOrganizationMcpCollectionServerAttachments(ctx, repo.ListOrganizationMcpCollectionServerAttachmentsParams{
		CollectionID:   collection.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list collection servers").Log(ctx, s.logger)
	}

	mcpServerRows, err := s.repo.ListOrganizationMcpCollectionMcpServerAttachments(ctx, repo.ListOrganizationMcpCollectionMcpServerAttachmentsParams{
		CollectionID:   collection.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list collection mcp servers").Log(ctx, s.logger)
	}

	collectionRegistryIDStr := registry.ID.String()
	mcpMetaRepo := mcpmetadataRepo.New(s.db)

	// Toolset-backed and mcp_server-backed attachments are listed independently
	// (Option A) and merged into one published_at DESC stream so a collection
	// that mixes both backends presents a single stable ordering.
	entries := make([]collectionServerEntry, 0, len(toolsets)+len(mcpServerRows))
	for _, t := range toolsets {
		if !t.McpSlug.Valid {
			continue
		}

		remoteURL := s.serverURL.JoinPath("mcp", t.McpSlug.String).String()
		remoteHeaders, err := s.collectionRemoteHeaders(ctx, mcpMetaRepo, t.ID, t.McpIsPublic)
		if err != nil {
			return nil, err
		}
		desc := ""
		if t.Description.Valid {
			desc = t.Description.String
		}
		specifier := t.McpSlug.String
		if registry.Namespace != "" {
			specifier = path.Join(registry.Namespace, t.McpSlug.String)
		}
		toolsetID := t.ID.String()
		entries = append(entries, collectionServerEntry{
			publishedAt: t.PublishedAt.Time,
			tiebreak:    toolsetID,
			server: &types.ExternalMCPServer{
				RegistrySpecifier:                   specifier,
				Version:                             "1.0.0",
				Description:                         desc,
				ToolsetID:                           &toolsetID,
				McpServerID:                         nil,
				RegistryID:                          nil,
				OrganizationMcpCollectionRegistryID: &collectionRegistryIDStr,
				Title:                               &t.Name,
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

		mcpServerID := m.McpServerID.String()
		entries = append(entries, collectionServerEntry{
			publishedAt: m.PublishedAt.Time,
			tiebreak:    mcpServerID,
			server: &types.ExternalMCPServer{
				RegistrySpecifier:                   specifier,
				Version:                             "1.0.0",
				Description:                         "",
				ToolsetID:                           nil,
				McpServerID:                         &mcpServerID,
				RegistryID:                          nil,
				OrganizationMcpCollectionRegistryID: &collectionRegistryIDStr,
				Title:                               &title,
				IconURL:                             nil,
				Meta:                                nil,
				Tools:                               nil,
				// mcp_server-backed servers authenticate via their user session
				// issuer (OAuth), so there are no static headers for the client
				// to collect. Environments are not yet wired to mcp_servers; when
				// they are, per-variable headers would be derived here.
				Remotes: []*types.ExternalMCPRemote{{
					URL:           remoteURL,
					TransportType: "streamable-http",
					Headers:       []*types.ExternalMCPRemoteHeader{},
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

func (s *Service) collectionRemoteHeaders(ctx context.Context, mcpMetaRepo *mcpmetadataRepo.Queries, toolsetID uuid.UUID, mcpIsPublic bool) ([]*types.ExternalMCPRemoteHeader, error) {
	headers := make([]*types.ExternalMCPRemoteHeader, 0)

	if !mcpIsPublic {
		headers = append(headers,
			collectionRemoteHeader("gram_environment", "gram-environment", false),
			collectionRemoteHeader("authorization", "gram-key", true),
		)
	}

	metadataRecord, err := mcpMetaRepo.GetMetadataForToolset(ctx, uuid.NullUUID{UUID: toolsetID, Valid: true})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return headers, nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load mcp metadata for collection server").Log(ctx, s.logger)
	}

	metadata, err := mcpmetadata.ToMCPMetadata(ctx, mcpMetaRepo, metadataRecord)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "convert mcp metadata for collection server").Log(ctx, s.logger)
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
		return oops.E(oops.CodeUnexpected, err, "error accessing toolset").Log(ctx, s.logger)
	}
	if !toolset.McpEnabled || !toolset.McpSlug.Valid {
		return oops.E(oops.CodeInvalid, nil, "cannot attach a toolset that is not enabled as an MCP server").Log(ctx, s.logger)
	}

	_, err = queries.AttachServerToOrganizationMcpCollection(ctx, repo.AttachServerToOrganizationMcpCollectionParams{
		CollectionID:   collectionID,
		OrganizationID: organizationID,
		ToolsetID:      uuid.NullUUID{UUID: toolsetID, Valid: true},
		PublishedBy:    conv.PtrToPGTextEmpty(&userID),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return oops.E(oops.CodeConflict, err, "toolset already attached to collection").Log(ctx, s.logger)
		}
		return oops.E(oops.CodeUnexpected, err, "failed to attach server to collection").Log(ctx, s.logger)
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
		return oops.E(oops.CodeUnexpected, err, "error accessing mcp server").Log(ctx, s.logger)
	}
	if server.Visibility == mcpservers.VisibilityDisabled || !server.HasEndpoint {
		return oops.E(oops.CodeInvalid, nil, "cannot attach an mcp server that is disabled or has no endpoint").Log(ctx, s.logger)
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
			return oops.E(oops.CodeConflict, err, "mcp server already attached to collection").Log(ctx, s.logger)
		}
		return oops.E(oops.CodeUnexpected, err, "failed to attach mcp server to collection").Log(ctx, s.logger)
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
		return oops.E(oops.CodeUnexpected, err, "error starting transaction for default collection").Log(ctx, s.logger)
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
		return oops.E(oops.CodeUnexpected, err, "error ensuring default registry collection").Log(ctx, s.logger)
	}

	if _, err := tx.EnsureOrganizationMcpCollectionRegistry(ctx, repo.EnsureOrganizationMcpCollectionRegistryParams{
		CollectionID:   collection.ID,
		OrganizationID: organizationID,
		Namespace:      defaultRegistryNamespace(organizationSlug, organizationID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error ensuring default registry namespace").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error committing default registry collection").Log(ctx, s.logger)
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
