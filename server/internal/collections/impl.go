package collections

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"strings"

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
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/collections/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	toolsets  *toolsetsRepo.Queries
	orgRepo   *orgRepo.Queries
	auth      *auth.Auth
	sessions  *sessions.Manager
	serverURL *url.URL
}

const defaultCollectionSlug = "registry"

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, accessLoader auth.AccessLoader, serverURL *url.URL) *Service {
	logger = logger.With(attr.SlogComponent("collections"))

	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/collections"),
		logger:    logger,
		db:        db,
		repo:      repo.New(db),
		toolsets:  toolsetsRepo.New(db),
		orgRepo:   orgRepo.New(db),
		auth:      auth.New(logger, db, sessions, accessLoader),
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
		if err := s.attachServerToCollection(ctx, cr, collection.ID, toolsetID, authCtx.ActiveOrganizationID, authCtx.UserID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to attach toolset to collection").Log(ctx, s.logger)
		}
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

	orgSlug := authCtx.OrganizationSlug
	if orgSlug == "" {
		orgMeta, err := s.orgRepo.GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error accessing organization metadata").Log(ctx, s.logger)
		}
		orgSlug = orgMeta.Slug
	}

	if err := s.ensureDefaultRegistryCollection(ctx, authCtx.ActiveOrganizationID, orgSlug); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error ensuring default registry collection").Log(ctx, s.logger)
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

	collectionID, err := uuid.Parse(payload.CollectionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid collection_id").Log(ctx, s.logger)
	}

	updated, err := s.repo.UpdateOrganizationMcpCollection(ctx, repo.UpdateOrganizationMcpCollectionParams{
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

	reg, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, repo.GetOrganizationMcpCollectionRegistryByIDParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection registry").Log(ctx, s.logger)
	}

	return toMCPCollection(repo.CreateOrganizationMcpCollectionRow(updated), reg.Namespace), nil
}

func (s *Service) Delete(ctx context.Context, payload *gen.DeletePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
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

	collectionID, err := uuid.Parse(payload.CollectionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid collection_id").Log(ctx, s.logger)
	}

	toolsetID, err := uuid.Parse(payload.ToolsetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
	}

	collection, err := s.repo.GetOrganizationMcpCollectionByID(ctx, repo.GetOrganizationMcpCollectionByIDParams{
		ID:             collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection").Log(ctx, s.logger)
	}

	if err := s.attachServerToCollection(ctx, s.repo, collectionID, toolsetID, authCtx.ActiveOrganizationID, authCtx.UserID); err != nil {
		return nil, err
	}

	reg, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, repo.GetOrganizationMcpCollectionRegistryByIDParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing collection registry").Log(ctx, s.logger)
	}

	return toMCPCollection(repo.CreateOrganizationMcpCollectionRow(collection), reg.Namespace), nil
}

func (s *Service) DetachServer(ctx context.Context, payload *gen.DetachServerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	collectionID, err := uuid.Parse(payload.CollectionID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid collection_id").Log(ctx, s.logger)
	}

	toolsetID, err := uuid.Parse(payload.ToolsetID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
	}

	_, err = s.repo.GetOrganizationMcpCollectionByID(ctx, repo.GetOrganizationMcpCollectionByIDParams{
		ID:             collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "error accessing collection").Log(ctx, s.logger)
	}

	if err := s.repo.DetachServerFromOrganizationMcpCollection(ctx, repo.DetachServerFromOrganizationMcpCollectionParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ToolsetID:      toolsetID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to detach server from collection").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) ListServers(ctx context.Context, payload *gen.ListServersPayload) (*gen.ListServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
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

	servers := make([]*types.ExternalMCPServer, 0, len(toolsets))
	for _, t := range toolsets {
		if !t.McpSlug.Valid {
			continue
		}
		remoteURL := s.serverURL.JoinPath("mcp", t.McpSlug.String).String()
		desc := ""
		if t.Description.Valid {
			desc = t.Description.String
		}
		specifier := t.McpSlug.String
		if registry.Namespace != "" {
			specifier = path.Join(registry.Namespace, t.McpSlug.String)
		}
		collectionRegistryIDStr := registry.ID.String()
		servers = append(servers, &types.ExternalMCPServer{
			RegistrySpecifier:                   specifier,
			Version:                             "1.0.0",
			Description:                         desc,
			RegistryID:                          nil,
			OrganizationMcpCollectionRegistryID: &collectionRegistryIDStr,
			Title:                               &t.Name,
			IconURL:                             nil,
			Meta:                                nil,
			Tools:                               nil,
			Remotes: []*types.ExternalMCPRemote{{
				URL:           remoteURL,
				TransportType: "streamable-http",
			}},
		})
	}

	return &gen.ListServersResult{Servers: servers}, nil
}

func (s *Service) attachServerToCollection(ctx context.Context, queries *repo.Queries, collectionID, toolsetID uuid.UUID, organizationID, userID string) error {
	toolset, err := s.toolsets.GetToolsetByID(ctx, toolsetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "error accessing toolset").Log(ctx, s.logger)
	}
	if toolset.OrganizationID != organizationID {
		return oops.C(oops.CodeNotFound)
	}
	if !toolset.McpEnabled || !toolset.McpSlug.Valid {
		return oops.E(oops.CodeInvalid, nil, "cannot attach a toolset that is not enabled as an MCP server").Log(ctx, s.logger)
	}

	installed, checkErr := queries.IsToolsetInstalledFromCatalog(ctx, toolsetID)
	if checkErr != nil {
		return oops.E(oops.CodeUnexpected, checkErr, "error checking if toolset is installed from catalog").Log(ctx, s.logger)
	}

	if installed {
		return oops.E(oops.CodeInvalid, nil, "cannot attach a toolset installed from the catalog").Log(ctx, s.logger)
	}

	_, err = queries.AttachServerToOrganizationMcpCollection(ctx, repo.AttachServerToOrganizationMcpCollectionParams{
		CollectionID:   collectionID,
		OrganizationID: organizationID,
		ToolsetID:      toolsetID,
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
