package collections

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	toolsets  *toolsetsRepo.Queries
	auth      *auth.Auth
	sessions  *sessions.Manager
	serverURL *url.URL
}

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

	collection, err := s.repo.CreateOrganizationMcpCollection(ctx, repo.CreateOrganizationMcpCollectionParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           payload.Name,
		Description:    pgText(payload.Description),
		Slug:           payload.Slug,
		Visibility:     payload.Visibility,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create collection").Log(ctx, s.logger)
	}

	_, err = s.repo.CreateOrganizationMcpCollectionRegistry(ctx, repo.CreateOrganizationMcpCollectionRegistryParams{
		CollectionID: collection.ID,
		Namespace:    payload.McpRegistryNamespace,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create collection registry").Log(ctx, s.logger)
	}

	if len(payload.ToolsetIds) > 0 {
		for _, idStr := range payload.ToolsetIds {
			toolsetID, parseErr := uuid.Parse(idStr)
			if parseErr != nil {
				continue
			}
			_, _ = s.repo.AttachServerToOrganizationMcpCollection(ctx, repo.AttachServerToOrganizationMcpCollectionParams{
				CollectionID: collection.ID,
				ToolsetID:    toolsetID,
				PublishedBy:  pgText(&authCtx.UserID),
			})
		}
	}

	return toMCPCollection(collection, payload.McpRegistryNamespace), nil
}

func (s *Service) List(ctx context.Context, payload *gen.ListPayload) (*gen.ListResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	collections, err := s.repo.ListOrganizationMcpCollections(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list collections").Log(ctx, s.logger)
	}

	result := make([]*types.MCPCollection, 0, len(collections))
	for _, c := range collections {
		namespace := ""
		reg, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, c.ID)
		if err == nil {
			namespace = reg.Namespace
		}
		result = append(result, toMCPCollection(repo.CreateOrganizationMcpCollectionRow(c), namespace))
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

	existing, err := s.repo.GetOrganizationMcpCollectionByID(ctx, collectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get collection").Log(ctx, s.logger)
	}
	if existing.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.C(oops.CodeForbidden)
	}

	updated, err := s.repo.UpdateOrganizationMcpCollection(ctx, repo.UpdateOrganizationMcpCollectionParams{
		ID:          collectionID,
		Name:        pgText(payload.Name),
		Description: pgText(payload.Description),
		Visibility:  pgText(payload.Visibility),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update collection").Log(ctx, s.logger)
	}

	namespace := ""
	reg, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, collectionID)
	if err == nil {
		namespace = reg.Namespace
	}

	return toMCPCollection(repo.CreateOrganizationMcpCollectionRow(updated), namespace), nil
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

	existing, err := s.repo.GetOrganizationMcpCollectionByID(ctx, collectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "get collection").Log(ctx, s.logger)
	}
	if existing.OrganizationID != authCtx.ActiveOrganizationID {
		return oops.C(oops.CodeForbidden)
	}

	_ = s.repo.DeleteOrganizationMcpCollectionRegistriesByID(ctx, collectionID)
	_ = s.repo.DeleteOrganizationMcpCollectionServerAttachmentsByID(ctx, collectionID)

	if err := s.repo.DeleteOrganizationMcpCollection(ctx, collectionID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete collection").Log(ctx, s.logger)
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

	collection, err := s.repo.GetOrganizationMcpCollectionByID(ctx, collectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get collection").Log(ctx, s.logger)
	}
	if collection.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.C(oops.CodeForbidden)
	}

	toolset, err := s.toolsets.GetToolsetByID(ctx, toolsetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get toolset").Log(ctx, s.logger)
	}
	if toolset.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.C(oops.CodeForbidden)
	}
	if !toolset.McpEnabled || !toolset.McpSlug.Valid {
		return nil, oops.E(oops.CodeInvalid, nil, "cannot attach a toolset that is not enabled as an MCP server").Log(ctx, s.logger)
	}

	if installed, checkErr := s.repo.IsToolsetInstalledFromCatalog(ctx, toolsetID); checkErr == nil && installed {
		return nil, oops.E(oops.CodeInvalid, nil, "cannot attach a toolset installed from the catalog").Log(ctx, s.logger)
	}

	_, err = s.repo.AttachServerToOrganizationMcpCollection(ctx, repo.AttachServerToOrganizationMcpCollectionParams{
		CollectionID: collectionID,
		ToolsetID:    toolsetID,
		PublishedBy:  pgText(&authCtx.UserID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "attach server").Log(ctx, s.logger)
	}

	namespace := ""
	reg, regErr := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, collectionID)
	if regErr == nil {
		namespace = reg.Namespace
	}

	return toMCPCollection(repo.CreateOrganizationMcpCollectionRow(collection), namespace), nil
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

	existing, err := s.repo.GetOrganizationMcpCollectionByID(ctx, collectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "get collection").Log(ctx, s.logger)
	}
	if existing.OrganizationID != authCtx.ActiveOrganizationID {
		return oops.C(oops.CodeForbidden)
	}

	if err := s.repo.DetachServerFromOrganizationMcpCollection(ctx, repo.DetachServerFromOrganizationMcpCollectionParams{
		CollectionID: collectionID,
		ToolsetID:    toolsetID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "detach server").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "get collection").Log(ctx, s.logger)
	}

	registry, err := s.repo.GetOrganizationMcpCollectionRegistryByID(ctx, collection.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "get collection registry").Log(ctx, s.logger)
	}

	toolsets, err := s.repo.ListOrganizationMcpCollectionServerAttachments(ctx, collection.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list server attachments").Log(ctx, s.logger)
	}

	servers := make([]*types.ExternalMCPServer, 0, len(toolsets))
	for _, t := range toolsets {
		if !t.McpSlug.Valid {
			continue
		}
		remoteURL := fmt.Sprintf("%s/mcp/%s", s.serverURL.String(), t.McpSlug.String)
		desc := ""
		if t.Description.Valid {
			desc = t.Description.String
		}
		specifier := t.McpSlug.String
		if registry.Namespace != "" {
			specifier = fmt.Sprintf("%s/%s", registry.Namespace, t.McpSlug.String)
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

func pgText(s *string) pgtype.Text {
	if s == nil || *s == "" {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func toMCPCollection(c repo.CreateOrganizationMcpCollectionRow, namespace string) *types.MCPCollection {
	var descPtr *string
	if c.Description.Valid {
		descPtr = &c.Description.String
	}
	var nsPtr *string
	if namespace != "" {
		nsPtr = &namespace
	}
	return &types.MCPCollection{
		ID:                   c.ID.String(),
		Name:                 c.Name,
		Description:          descPtr,
		Slug:                 c.Slug,
		McpRegistryNamespace: nsPtr,
		Visibility:           c.Visibility,
	}
}
