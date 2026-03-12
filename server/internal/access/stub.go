// Package access provides a stub implementation of the access service.
// All methods return mock data until the real RBAC backend is built.
package access

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	srv "github.com/speakeasy-api/gram/server/gen/http/access/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("access"))
	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/access"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions),
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

// APIKeyAuth implements the session auth security scheme.
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// ---------------------------------------------------------------------------
// Stub implementations — return mock data
// ---------------------------------------------------------------------------

var now = time.Now().Format(time.RFC3339)

func (s *Service) ListRoles(_ context.Context, _ *gen.ListRolesPayload) (*gen.ListRolesResult, error) {
	return &gen.ListRolesResult{
		Roles: []*gen.Role{
			{ID: "role_admin", Name: "Admin", Description: "Full access to all resources and settings", IsSystem: true, Grants: []*gen.RoleGrant{
				{Scope: "org:read"}, {Scope: "org:admin"}, {Scope: "build:read"}, {Scope: "build:write"}, {Scope: "mcp:read"}, {Scope: "mcp:write"}, {Scope: "mcp:connect"},
			}, MemberCount: 1, CreatedAt: now, UpdatedAt: now},
			{ID: "role_member", Name: "Member", Description: "Read access and MCP connectivity", IsSystem: true, Grants: []*gen.RoleGrant{
				{Scope: "org:read"}, {Scope: "build:read"}, {Scope: "mcp:read"}, {Scope: "mcp:connect"},
			}, MemberCount: 3, CreatedAt: now, UpdatedAt: now},
		},
	}, nil
}

func (s *Service) GetRole(_ context.Context, p *gen.GetRolePayload) (*gen.Role, error) {
	return &gen.Role{
		ID: p.ID, Name: "Admin", Description: "Full access to all resources and settings",
		IsSystem: true, Grants: []*gen.RoleGrant{}, MemberCount: 1,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Service) CreateRole(_ context.Context, p *gen.CreateRolePayload) (*gen.Role, error) {
	return &gen.Role{
		ID: "role_new", Name: p.Name, Description: p.Description,
		IsSystem: false, Grants: p.Grants, MemberCount: len(p.MemberIds),
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Service) UpdateRole(_ context.Context, p *gen.UpdateRolePayload) (*gen.Role, error) {
	name := "Updated Role"
	if p.Name != nil {
		name = *p.Name
	}
	desc := "Updated"
	if p.Description != nil {
		desc = *p.Description
	}
	return &gen.Role{
		ID: p.ID, Name: name, Description: desc,
		IsSystem: false, Grants: p.Grants, MemberCount: 0,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Service) DeleteRole(_ context.Context, _ *gen.DeleteRolePayload) error {
	return nil
}

func (s *Service) ListScopes(_ context.Context, _ *gen.ListScopesPayload) (*gen.ListScopesResult, error) {
	return &gen.ListScopesResult{
		Scopes: []*gen.ScopeDefinition{
			{Slug: "org:read", Description: "View organization settings and metadata", ResourceType: "org"},
			{Slug: "org:admin", Description: "Manage organization settings, billing, and team", ResourceType: "org"},
			{Slug: "build:read", Description: "View projects, deployments, and build logs", ResourceType: "project"},
			{Slug: "build:write", Description: "Create and manage projects and deployments", ResourceType: "project"},
			{Slug: "mcp:read", Description: "View MCP server configurations", ResourceType: "mcp"},
			{Slug: "mcp:write", Description: "Create and manage MCP server configurations", ResourceType: "mcp"},
			{Slug: "mcp:connect", Description: "Connect to and use MCP servers", ResourceType: "mcp"},
		},
	}, nil
}

func (s *Service) ListMembers(_ context.Context, _ *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	return &gen.ListMembersResult{
		Members: []*gen.AccessMember{
			{ID: "usr_1", Name: "Sarah Chen", Email: "sarah@company.com", RoleID: "role_admin", JoinedAt: "2023-01-15T00:00:00Z"},
			{ID: "usr_2", Name: "Alex Johnson", Email: "alex@company.com", RoleID: "role_member", JoinedAt: "2023-03-22T00:00:00Z"},
			{ID: "usr_3", Name: "Maya Patel", Email: "maya@company.com", RoleID: "role_member", JoinedAt: "2023-05-10T00:00:00Z"},
			{ID: "usr_4", Name: "James Wilson", Email: "james@company.com", RoleID: "role_member", JoinedAt: "2023-06-01T00:00:00Z"},
		},
	}, nil
}

func (s *Service) UpdateMemberRole(_ context.Context, p *gen.UpdateMemberRolePayload) (*gen.AccessMember, error) {
	return &gen.AccessMember{
		ID: p.UserID, Name: "Updated User", Email: "user@company.com",
		RoleID: p.RoleID, JoinedAt: now,
	}, nil
}
