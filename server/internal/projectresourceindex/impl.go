package projectresourceindex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/project_resource_index/server"
	gen "github.com/speakeasy-api/gram/server/gen/project_resource_index"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/projectresourceindex/repo"
)

var allKinds = []string{"assistant", "environment", "gateway", "mcp", "plugin", "rule", "skill"}

type queryStore interface {
	ListProjectResourceIndexAssistants(context.Context, uuid.UUID) ([]repo.ListProjectResourceIndexAssistantsRow, error)
	ListProjectResourceIndexMCPServers(context.Context, uuid.UUID) ([]repo.ListProjectResourceIndexMCPServersRow, error)
	ListProjectResourceIndexSkills(context.Context, uuid.UUID) ([]repo.ListProjectResourceIndexSkillsRow, error)
	ListProjectResourceIndexCustomRules(context.Context, repo.ListProjectResourceIndexCustomRulesParams) ([]repo.ListProjectResourceIndexCustomRulesRow, error)
	ListProjectResourceIndexEnvironments(context.Context, uuid.UUID) ([]repo.ListProjectResourceIndexEnvironmentsRow, error)
	ListProjectResourceIndexGateways(context.Context, repo.ListProjectResourceIndexGatewaysParams) ([]repo.ListProjectResourceIndexGatewaysRow, error)
	ListProjectResourceIndexPlugins(context.Context, repo.ListProjectResourceIndexPluginsParams) ([]repo.ListProjectResourceIndexPluginsRow, error)
}

type featureChecker interface {
	IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error)
}

type authorizer interface {
	Evaluate(context.Context, ...authz.Check) (bool, error)
	RequirePluginWrite(context.Context, string, string) error
}

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	auth     *auth.Auth
	repo     queryStore
	authz    authorizer
	features featureChecker
}

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, authzEngine *authz.Engine, features featureChecker) *Service {
	return &Service{tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/projectresourceindex"), logger: logger, auth: auth.New(logger, db, sessionManager, authzEngine), repo: repo.New(db), authz: authzEngine, features: features}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

type adapterResult struct {
	kind     string
	items    []*gen.Projectresourceindexitem
	err      error
	duration time.Duration
}
type adapter struct {
	kind string
	run  func(context.Context) ([]*gen.Projectresourceindexitem, error)
}

// Get returns one complete snapshot. Authorized set queries run concurrently;
// any query error fails the request, so successful responses are never partial.
func (s *Service) Get(ctx context.Context, _ *gen.GetPayload) (*gen.Projectresourceindex, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	projectID, organizationID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	skillsEnabled, err := s.features.IsFeatureEnabled(ctx, organizationID, productfeatures.FeatureSkills)
	if err != nil {
		return nil, fmt.Errorf("check skills feature for project resource index: %w", err)
	}
	authorized, err := s.authorizedKinds(ctx, organizationID, projectID)
	if err != nil {
		return nil, err
	}
	authorized["skill"] = authorized["skill"] && skillsEnabled
	adapters := s.adapters(organizationID, projectID, authorized)
	results := make(chan adapterResult, len(adapters))
	queryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, a := range adapters {
		go func(a adapter) {
			started := time.Now()
			items, err := a.run(queryCtx)
			results <- adapterResult{a.kind, items, err, time.Since(started)}
		}(a)
	}
	items := make([]*gen.Projectresourceindexitem, 0)
	counts := make(map[string]int, len(allKinds))
	for range adapters {
		result := <-results
		s.logger.InfoContext(ctx, "project resource index adapter completed",
			attr.SlogProjectResourceIndexKind(result.kind),
			attr.SlogProjectResourceIndexDurationMS(result.duration.Milliseconds()),
			attr.SlogProjectResourceIndexItemCount(len(result.items)),
			attr.SlogProjectResourceIndexFailed(result.err != nil),
		)
		if result.err != nil {
			cancel()
			return nil, oops.E(oops.CodeUnavailable, result.err, "project resource index query failed")
		}
		counts[result.kind] = len(result.items)
		items = append(items, result.items...)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].ID < items[j].ID
	})
	kinds := make([]*gen.Projectresourceindexkind, 0, len(allKinds))
	for _, kind := range allKinds {
		kinds = append(kinds, &gen.Projectresourceindexkind{Kind: kind, Complete: true, Overflow: false, ItemCount: counts[kind]})
	}
	return &gen.Projectresourceindex{SchemaVersion: "1", GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Items: items, Kinds: kinds}, nil
}

func (s *Service) adapters(org string, project uuid.UUID, allowed map[string]bool) []adapter {
	out := make([]adapter, 0, len(allKinds))
	add := func(kind string, fn func(context.Context) ([]*gen.Projectresourceindexitem, error)) {
		if allowed[kind] {
			out = append(out, adapter{kind, fn})
		}
	}
	add("assistant", func(ctx context.Context) ([]*gen.Projectresourceindexitem, error) {
		rows, e := s.repo.ListProjectResourceIndexAssistants(ctx, project)
		if e != nil {
			return nil, fmt.Errorf("list assistants: %w", e)
		}
		x := make([]*gen.Projectresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Projectresourceindexitem{Kind: "assistant", ID: r.ID.String(), Name: r.Name, RouteSelector: r.ID.String(), Keywords: nil})
		}
		return x, nil
	})
	add("environment", func(ctx context.Context) ([]*gen.Projectresourceindexitem, error) {
		rows, e := s.repo.ListProjectResourceIndexEnvironments(ctx, project)
		if e != nil {
			return nil, fmt.Errorf("list environments: %w", e)
		}
		x := make([]*gen.Projectresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Projectresourceindexitem{Kind: "environment", ID: r.ID.String(), Name: r.Name, RouteSelector: r.Slug, Keywords: compactKeywords(r.Name, r.Slug)})
		}
		return x, nil
	})
	add("gateway", func(ctx context.Context) ([]*gen.Projectresourceindexitem, error) {
		rows, e := s.repo.ListProjectResourceIndexGateways(ctx, repo.ListProjectResourceIndexGatewaysParams{OrganizationID: org, ProjectID: project})
		if e != nil {
			return nil, fmt.Errorf("list gateways: %w", e)
		}
		x := make([]*gen.Projectresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Projectresourceindexitem{Kind: "gateway", ID: r.ID.String(), Name: r.Name, RouteSelector: r.ID.String(), Keywords: nil})
		}
		return x, nil
	})
	add("mcp", func(ctx context.Context) ([]*gen.Projectresourceindexitem, error) {
		rows, e := s.repo.ListProjectResourceIndexMCPServers(ctx, project)
		if e != nil {
			return nil, fmt.Errorf("list MCP servers: %w", e)
		}
		x := make([]*gen.Projectresourceindexitem, 0, len(rows))
		for _, r := range rows {
			name, slug := r.Name.String, r.Slug.String
			if name == "" {
				name = slug
			}
			selector := slug
			if isReservedMCPRoute(selector) || selector == "" {
				selector = r.RouteID.String()
			}
			x = append(x, &gen.Projectresourceindexitem{Kind: "mcp", ID: r.ID.String(), Name: name, RouteSelector: selector, Keywords: compactKeywords(name, slug)})
		}
		return x, nil
	})
	add("plugin", func(ctx context.Context) ([]*gen.Projectresourceindexitem, error) {
		rows, e := s.repo.ListProjectResourceIndexPlugins(ctx, repo.ListProjectResourceIndexPluginsParams{OrganizationID: org, ProjectID: project})
		if e != nil {
			return nil, fmt.Errorf("list plugins: %w", e)
		}
		x := make([]*gen.Projectresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Projectresourceindexitem{Kind: "plugin", ID: r.ID.String(), Name: r.Name, RouteSelector: r.ID.String(), Keywords: compactKeywords(r.Name, r.Slug)})
		}
		return x, nil
	})
	add("rule", func(ctx context.Context) ([]*gen.Projectresourceindexitem, error) {
		rows, e := s.repo.ListProjectResourceIndexCustomRules(ctx, repo.ListProjectResourceIndexCustomRulesParams{OrganizationID: org, ProjectID: project})
		if e != nil {
			return nil, fmt.Errorf("list custom rules: %w", e)
		}
		x := make([]*gen.Projectresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Projectresourceindexitem{Kind: "rule", ID: r.ID.String(), Name: r.Title, RouteSelector: r.RuleID, Keywords: compactKeywords(r.Title, r.RuleID, r.Severity)})
		}
		return x, nil
	})
	add("skill", func(ctx context.Context) ([]*gen.Projectresourceindexitem, error) {
		rows, e := s.repo.ListProjectResourceIndexSkills(ctx, project)
		if e != nil {
			return nil, fmt.Errorf("list skills: %w", e)
		}
		x := make([]*gen.Projectresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Projectresourceindexitem{Kind: "skill", ID: r.ID.String(), Name: r.DisplayName, RouteSelector: r.ID.String(), Keywords: compactKeywords(r.DisplayName, r.Name)})
		}
		return x, nil
	})
	return out
}

func isReservedMCPRoute(slug string) bool {
	switch slug {
	case "add", "catalog", "deployments", "sources", "organization-gateway", "gateway", "x":
		return true
	default:
		return false
	}
}

func (s *Service) authorizedKinds(ctx context.Context, organizationID string, projectID uuid.UUID) (map[string]bool, error) {
	mcpCollectionCheck := authz.MCPCheck(authz.ScopeMCPRead, authz.WildcardResource, projectID.String())
	checks := []struct {
		kinds []string
		check authz.Check
	}{
		{[]string{"assistant"}, authz.AssistantCheck(authz.ScopeAssistantRead, authz.WildcardResource, projectID.String())},
		{[]string{"environment"}, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}},
		{[]string{"gateway", "mcp"}, mcpCollectionCheck},
		{[]string{"plugin"}, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: authz.ResourceKindOrg, ResourceID: organizationID, Dimensions: nil}},
		{[]string{"rule"}, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: organizationID, Dimensions: nil}},
		{[]string{"skill"}, authz.Check{Scope: authz.ScopeSkillRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}},
	}
	result := make(map[string]bool, len(allKinds))
	for _, candidate := range checks {
		ok, err := s.authz.Evaluate(ctx, candidate.check)
		if err != nil {
			return nil, fmt.Errorf("authorize project resource index kind %s: %w", strings.Join(candidate.kinds, "/"), err)
		}
		for _, kind := range candidate.kinds {
			result[kind] = ok
		}
	}
	if !result["plugin"] {
		err := s.authz.RequirePluginWrite(ctx, organizationID, projectID.String())
		if err != nil {
			err = fmt.Errorf("authorize project resource index plugin write: %w", err)
		}
		if err == nil {
			result["plugin"] = true
		} else {
			var denied *oops.ShareableError
			if !errors.As(err, &denied) || denied.Code != oops.CodeForbidden {
				return nil, err
			}
		}
	}
	return result, nil
}

func compactKeywords(name string, aliases ...string) []string {
	keywords := make([]string, 0, len(aliases))
	seen := map[string]struct{}{name: {}}
	for _, alias := range aliases {
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		keywords = append(keywords, alias)
	}
	if len(keywords) == 0 {
		return nil
	}
	return keywords
}
