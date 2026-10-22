package usersessions

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/gateway_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/sessionlist"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type gatewaySessionScope struct {
	organizationID string
	gatewayID      uuid.NullUUID
	projects       []uuid.UUID
	issuers        []uuid.UUID
	resources      []string
}

func (s *Service) gatewaySessionScope(ctx context.Context, gatewayID *string) (*gatewaySessionScope, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	id, err := conv.PtrToNullUUID(gatewayID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid gateway id")
	}
	scope := &gatewaySessionScope{organizationID: authCtx.ActiveOrganizationID, gatewayID: id, projects: []uuid.UUID{}, issuers: []uuid.UUID{}, resources: []string{}}
	q := repo.New(s.db)
	if id.Valid {
		gateway, err := q.GetGatewaySessionScope(ctx, repo.GetGatewaySessionScopeParams{GatewayID: id.UUID, OrganizationID: scope.organizationID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load gateway session scope")
		}
		if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: gateway.ProjectID.String(), Dimensions: nil}); err != nil {
			return nil, err
		}
		scope.projects = []uuid.UUID{gateway.ProjectID}
		if gateway.UserSessionIssuerID.Valid {
			scope.issuers = []uuid.UUID{gateway.UserSessionIssuerID.UUID}
		}
		endpoints, err := q.ListGatewaySessionEndpoints(ctx, repo.ListGatewaySessionEndpointsParams{GatewayID: id, ProjectID: gateway.ProjectID, OrganizationID: scope.organizationID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list gateway endpoint resources")
		}
		for _, endpoint := range endpoints {
			bases := s.gatewayPlatformURLs()
			if endpoint.Domain.Valid {
				bases = []string{"https://" + endpoint.Domain.String}
			}
			for _, base := range bases {
				parsed, err := url.Parse(base)
				if err != nil {
					return nil, oops.E(oops.CodeUnexpected, err, "parse gateway base URL")
				}
				if !endpoint.IsDomainRoot.Bool {
					parsed = parsed.JoinPath("mcp", endpoint.Slug)
				}
				scope.resources = append(scope.resources, strings.TrimRight(parsed.String(), "/"))
			}
		}
		return scope, nil
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: scope.organizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	// Turning off the gateway must not hide existing authorizations.
	scope.issuers, err = q.ListGatewaySessionIssuers(ctx, conv.ToPGText(scope.organizationID))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization gateway issuers")
	}
	scope.projects, err = q.ListGatewaySessionProjects(ctx, scope.organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization gateway projects")
	}
	organization, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, scope.organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load organization gateway")
	}
	for _, platformURL := range s.gatewayPlatformURLs() {
		base, err := url.Parse(platformURL)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "parse gateway base URL")
		}
		scope.resources = append(scope.resources, strings.TrimRight(orggateway.URL(base, organization.Slug), "/"))
	}
	return scope, nil
}

// gatewayPlatformURLs mirrors the platform origins accepted by shared OAuth.
func (s *Service) gatewayPlatformURLs() []string {
	bases := []string{s.serverURL}
	for _, base := range s.sharedHosts.PlatformHosts {
		bases = append(bases, base)
	}
	slices.Sort(bases)
	return slices.Compact(bases)
}

// ListGatewaySessions applies all filters before stable keyset pagination.
func (s *Service) ListGatewaySessions(ctx context.Context, payload *gen.ListGatewaySessionsPayload) (*gen.GatewaySessionsResult, error) {
	scope, err := s.gatewaySessionScope(ctx, payload.GatewayID)
	if err != nil {
		return nil, err
	}
	serverID, err := conv.PtrToNullUUID(payload.ServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid member server id")
	}
	providerID, err := conv.PtrToNullUUID(payload.ProviderID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid provider id")
	}
	after, err := sessionlist.Date(payload.CreatedAfter)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid created_after")
	}
	before, err := sessionlist.Date(payload.CreatedBefore)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid created_before")
	}
	if after.Valid && before.Valid && !after.Time.Before(before.Time) {
		return nil, oops.E(oops.CodeBadRequest, nil, "created_after must precede created_before")
	}
	sort := conv.Default(payload.Sort, "newest")
	status := conv.Default(payload.Status, "active")
	switch sort {
	case "newest", "oldest", "last_used", "expiry":
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid session ordering")
	}
	switch status {
	case "active", "expired", "revoked", "all":
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid session status")
	}
	search := strings.TrimSpace(conv.PtrValOr(payload.Search, ""))
	fingerprint := sessionlist.Fingerprint(scope.organizationID, conv.PtrValOr(payload.GatewayID, ""), search, conv.PtrValOr(payload.ServerID, ""), conv.PtrValOr(payload.ProviderID, ""), status, sort, conv.PtrValOr(payload.CreatedAfter, ""), conv.PtrValOr(payload.CreatedBefore, ""))
	cursorTime, cursorID, err := sessionlist.Decode(payload.Cursor, fingerprint)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
	}
	limit := pageLimit(&payload.Limit)
	rows, err := repo.New(s.db).ListGatewaySessions(ctx, repo.ListGatewaySessionsParams{
		GatewayID: scope.gatewayID, OrganizationID: scope.organizationID, ProjectIds: scope.projects,
		IssuerIds: scope.issuers, Resources: scope.resources, Search: search, Status: status, Sort: sort,
		ServerID: serverID, ProviderID: providerID, CreatedAfter: after, CreatedBefore: before,
		CursorTime: cursorTime, CursorID: cursorID, LimitValue: limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list gateway sessions").LogError(ctx, s.logger)
	}
	var nextCursor *string
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		at := last.CreatedAt.Time
		switch sort {
		case "last_used":
			at = time.Time{}
			if last.LastUsedAt.Valid {
				at = last.LastUsedAt.Time
			}
		case "expiry":
			at = last.RefreshExpiresAt.Time
		}
		nextCursor = new(sessionlist.Encode(at, last.ID, fingerprint))
	}
	viewRows := make([]repo.ListUserSessionsByProjectIDRow, len(rows))
	for i, row := range rows {
		viewRows[i] = repo.ListUserSessionsByProjectIDRow(row)
	}
	upstreams, err := s.loadUpstreamsForSessions(ctx, scope.projects, scope.organizationID, viewRows)
	if err != nil {
		return nil, err
	}
	workloads, err := s.loadWorkloadLabelsForSessions(ctx, scope.projects, scope.organizationID, viewRows)
	if err != nil {
		return nil, err
	}
	return &gen.GatewaySessionsResult{Items: mv.BuildUserSessionListView(viewRows, upstreams, workloads), NextCursor: nextCursor}, nil
}

// ListGatewaySessionFacets lists configured associations, not observed usage.
func (s *Service) ListGatewaySessionFacets(ctx context.Context, payload *gen.ListGatewaySessionFacetsPayload) (*gen.GatewaySessionFacets, error) {
	scope, err := s.gatewaySessionScope(ctx, payload.GatewayID)
	if err != nil {
		return nil, err
	}
	rows, err := repo.New(s.db).ListGatewaySessionFacets(ctx, repo.ListGatewaySessionFacetsParams{GatewayID: scope.gatewayID, OrganizationID: conv.ToPGText(scope.organizationID), ProjectIds: scope.projects})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list gateway session facets").LogError(ctx, s.logger)
	}
	result := &gen.GatewaySessionFacets{Servers: []*gen.GatewaySessionFacet{}, Providers: []*gen.GatewaySessionFacet{}}
	servers, providers := make(map[uuid.UUID]bool), make(map[uuid.UUID]bool)
	for _, row := range rows {
		if !servers[row.ServerID] {
			result.Servers = append(result.Servers, &gen.GatewaySessionFacet{Value: row.ServerID.String(), Label: conv.PtrValOr(conv.FromPGText[string](row.ServerName), row.ServerID.String())})
			servers[row.ServerID] = true
		}
		if !providers[row.ProviderID] {
			result.Providers = append(result.Providers, &gen.GatewaySessionFacet{Value: row.ProviderID.String(), Label: row.ProviderName})
			providers[row.ProviderID] = true
		}
	}
	return result, nil
}
