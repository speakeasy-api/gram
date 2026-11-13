package commandsearch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/command_search"
	srv "github.com/speakeasy-api/gram/server/gen/http/command_search/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/commandsearch/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	indexBudget       = 250 * time.Millisecond
	perKindLimit      = 20
	perKindQueryLimit = perKindLimit + 1
	globalLimit       = 40
)

var allKinds = []string{"environment", "gateway", "plugin", "rule"}

type queryStore interface {
	ListCommandIndexCustomRules(context.Context, repo.ListCommandIndexCustomRulesParams) ([]repo.ListCommandIndexCustomRulesRow, error)
	ListCommandIndexEnvironments(context.Context, repo.ListCommandIndexEnvironmentsParams) ([]repo.ListCommandIndexEnvironmentsRow, error)
	ListCommandIndexGateways(context.Context, repo.ListCommandIndexGatewaysParams) ([]repo.ListCommandIndexGatewaysRow, error)
	ListCommandIndexPlugins(context.Context, repo.ListCommandIndexPluginsParams) ([]repo.ListCommandIndexPluginsRow, error)
}

type authorizer interface {
	Evaluate(context.Context, ...authz.Check) (bool, error)
	RequirePluginWrite(context.Context, string, string) error
}

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	auth   *auth.Auth
	repo   queryStore
	authz  authorizer
	budget time.Duration
}

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, authzEngine *authz.Engine) *Service {
	return &Service{tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/commandsearch"), logger: logger, auth: auth.New(logger, db, sessionManager, authzEngine), repo: repo.New(db), authz: authzEngine, budget: indexBudget}
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
	kind      string
	items     []*gen.Commandresourceindexitem
	truncated bool
	err       error
	duration  time.Duration
}

type adapter struct {
	kind string
	run  func(context.Context) ([]*gen.Commandresourceindexitem, error)
}

// Search is reusable product-domain logic: HTTP and Platform MCP callers can use
// the same authorization, tenancy, lifecycle, completeness, and safety rules.
func (s *Service) Search(ctx context.Context, payload *gen.SearchPayload) (*gen.Commandresourceindex, error) {
	indexCtx, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()

	query := strings.TrimSpace(payload.Q)
	queryLength := utf8.RuneCountInString(query)
	if queryLength < 2 || queryLength > 128 {
		return nil, oops.E(oops.CodeBadRequest, nil, "resource search query must contain between 2 and 128 non-whitespace characters")
	}

	authCtx, ok := contextvalues.GetAuthContext(indexCtx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	projectID, organizationID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	authorized, err := s.authorizedKinds(indexCtx, organizationID, projectID)
	if err != nil {
		return nil, err
	}

	adapters := s.adapters(organizationID, projectID, query, authorized)
	results := make(chan adapterResult, len(adapters))
	for _, a := range adapters {
		go func() {
			started := time.Now()
			items, err := a.run(indexCtx)
			truncated := len(items) > perKindLimit
			if truncated {
				items = items[:perKindLimit]
			}
			results <- adapterResult{kind: a.kind, items: items, truncated: truncated, err: err, duration: time.Since(started)}
		}()
	}

	items := make([]*gen.Commandresourceindexitem, 0)
	truncatedSet := make(map[string]struct{}, len(adapters))
	pending := make(map[string]struct{}, len(adapters))
	for _, a := range adapters {
		pending[a.kind] = struct{}{}
	}
	consume := func(result adapterResult) error {
		s.logger.InfoContext(ctx, "command resource search adapter completed",
			attr.SlogCommandSearchKind(result.kind),
			attr.SlogCommandSearchDurationMS(result.duration.Milliseconds()),
			attr.SlogCommandSearchItemCount(len(result.items)),
			attr.SlogCommandSearchFailed(result.err != nil),
		)
		if result.err != nil {
			if deadlineInterruptedAdapter(indexCtx.Err(), result.err) {
				return nil
			}
			delete(pending, result.kind)
			return oops.E(oops.CodeUnavailable, nil, "resource search adapter failed")
		}
		delete(pending, result.kind)
		if result.truncated {
			truncatedSet[result.kind] = struct{}{}
		}
		items = append(items, result.items...)
		return nil
	}

collect:
	for len(pending) > 0 {
		select {
		case result := <-results:
			if err := consume(result); err != nil {
				return nil, err
			}
		case <-indexCtx.Done():
			for {
				select {
				case result := <-results:
					if err := consume(result); err != nil {
						return nil, err
					}
				default:
					break collect
				}
			}
		}
	}
	timedOut := errors.Is(indexCtx.Err(), context.DeadlineExceeded) && len(pending) > 0
	if len(pending) > 0 && !timedOut {
		return nil, oops.E(oops.CodeUnavailable, nil, "resource search canceled")
	}
	failedKinds := make([]string, 0, len(pending))
	for kind := range pending {
		failedKinds = append(failedKinds, kind)
	}
	sort.Strings(failedKinds)

	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		aRank, bRank := matchRank(a, query), matchRank(b, query)
		if aRank != bRank {
			return aRank < bRank
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	if len(items) > globalLimit {
		for _, item := range items[globalLimit:] {
			truncatedSet[item.Kind] = struct{}{}
		}
		items = items[:globalLimit]
	}
	truncatedKinds := make([]string, 0, len(truncatedSet))
	for kind := range truncatedSet {
		truncatedKinds = append(truncatedKinds, kind)
	}
	sort.Strings(truncatedKinds)
	truncated := len(truncatedKinds) > 0
	payloadEstimate := 0
	for _, item := range items {
		payloadEstimate += len(item.Kind) + len(item.ID) + len(item.Name) + len(item.RouteSelector) + 32
		for _, keyword := range item.Keywords {
			payloadEstimate += len(keyword) + 3
		}
	}
	partial := timedOut && len(failedKinds) > 0
	s.logger.InfoContext(ctx, "command resource search completed",
		attr.SlogCommandSearchItemCount(len(items)),
		attr.SlogCommandSearchPayloadEstimateBytes(payloadEstimate),
		attr.SlogCommandSearchAdapterCount(len(adapters)),
		attr.SlogCommandSearchPartial(partial),
		attr.SlogCommandSearchTimedOut(timedOut),
		attr.SlogCommandSearchFailedKindCount(len(failedKinds)),
	)
	return &gen.Commandresourceindex{SchemaVersion: "1", GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Items: items, Partial: partial, TimedOut: timedOut, FailedKinds: failedKinds, Truncated: truncated, TruncatedKinds: truncatedKinds}, nil
}

func (s *Service) adapters(org string, project uuid.UUID, searchQuery string, allowed map[string]bool) []adapter {
	limit := int32(perKindQueryLimit)
	bounds := makePrefixBounds(searchQuery)
	out := make([]adapter, 0, len(allKinds))
	add := func(kind string, fn func(context.Context) ([]*gen.Commandresourceindexitem, error)) {
		if allowed[kind] {
			out = append(out, adapter{kind: kind, run: fn})
		}
	}
	add("environment", func(ctx context.Context) ([]*gen.Commandresourceindexitem, error) {
		rows, e := s.repo.ListCommandIndexEnvironments(ctx, repo.ListCommandIndexEnvironmentsParams{LowerBound: bounds.lower, LowerCeiling: bounds.lowerCeiling, ProjectID: project, ResultLimit: limit})
		if e != nil {
			return nil, fmt.Errorf("list command search environments: %w", e)
		}
		x := make([]*gen.Commandresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Commandresourceindexitem{Kind: "environment", ID: r.ID.String(), Name: r.Name, RouteSelector: r.Slug, Keywords: compactKeywords(r.Name, r.Slug)})
		}
		return x, nil
	})
	add("gateway", func(ctx context.Context) ([]*gen.Commandresourceindexitem, error) {
		rows, e := s.repo.ListCommandIndexGateways(ctx, repo.ListCommandIndexGatewaysParams{LowerBound: bounds.lower, LowerCeiling: bounds.lowerCeiling, OrganizationID: org, ProjectID: project, ResultLimit: limit})
		if e != nil {
			return nil, fmt.Errorf("list command search gateways: %w", e)
		}
		x := make([]*gen.Commandresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Commandresourceindexitem{Kind: "gateway", ID: r.ID.String(), Name: r.Name, RouteSelector: r.ID.String(), Keywords: nil})
		}
		return x, nil
	})
	add("plugin", func(ctx context.Context) ([]*gen.Commandresourceindexitem, error) {
		rows, e := s.repo.ListCommandIndexPlugins(ctx, repo.ListCommandIndexPluginsParams{LowerBound: bounds.lower, LowerCeiling: bounds.lowerCeiling, OrganizationID: org, ProjectID: project, ResultLimit: limit})
		if e != nil {
			return nil, fmt.Errorf("list command search plugins: %w", e)
		}
		x := make([]*gen.Commandresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Commandresourceindexitem{Kind: "plugin", ID: r.ID.String(), Name: r.Name, RouteSelector: r.ID.String(), Keywords: compactKeywords(r.Name, r.Slug)})
		}
		return x, nil
	})
	add("rule", func(ctx context.Context) ([]*gen.Commandresourceindexitem, error) {
		rows, e := s.repo.ListCommandIndexCustomRules(ctx, repo.ListCommandIndexCustomRulesParams{LowerBound: bounds.lower, LowerCeiling: bounds.lowerCeiling, OrganizationID: org, ProjectID: project, ResultLimit: limit})
		if e != nil {
			return nil, fmt.Errorf("list command search custom rules: %w", e)
		}
		x := make([]*gen.Commandresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Commandresourceindexitem{Kind: "rule", ID: r.ID.String(), Name: r.Title, RouteSelector: r.RuleID, Keywords: compactKeywords(r.Title, r.RuleID, r.Severity)})
		}
		return x, nil
	})

	return out
}

func (s *Service) authorizedKinds(ctx context.Context, organizationID string, projectID uuid.UUID) (map[string]bool, error) {
	checks := []struct {
		kind  string
		check authz.Check
	}{
		{"environment", authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}},
		{"gateway", authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}},
		{"plugin", authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: authz.ResourceKindOrg, ResourceID: organizationID, Dimensions: nil}},
		{"rule", authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: organizationID, Dimensions: nil}},
	}
	result := make(map[string]bool, len(checks))
	for _, candidate := range checks {
		ok, err := s.authz.Evaluate(ctx, candidate.check)
		if err != nil {
			return nil, fmt.Errorf("authorize command search kind %s: %w", candidate.kind, err)
		}
		result[candidate.kind] = ok
	}
	if !result["plugin"] {
		err := s.authz.RequirePluginWrite(ctx, organizationID, projectID.String())
		if err != nil {
			err = fmt.Errorf("authorize command search plugin write: %w", err)
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

func deadlineInterruptedAdapter(ctxErr, adapterErr error) bool {
	return errors.Is(ctxErr, context.DeadlineExceeded) &&
		(errors.Is(adapterErr, context.Canceled) || errors.Is(adapterErr, context.DeadlineExceeded))
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

type prefixBounds struct {
	lower        string
	lowerCeiling string
}

func makePrefixBounds(query string) prefixBounds {
	lower := strings.ToLower(strings.TrimSpace(query))
	return prefixBounds{lower: lower, lowerCeiling: nextPrefix(lower)}
}

func nextPrefix(prefix string) string {
	runes := []rune(prefix)
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] < utf8.MaxRune {
			runes[i]++
			return string(runes[:i+1])
		}
	}
	return prefix + string(utf8.MaxRune)
}

func matchRank(item *gen.Commandresourceindexitem, query string) int {
	query = strings.ToLower(strings.TrimSpace(query))
	if strings.ToLower(item.Name) == query {
		return 0
	}
	for _, keyword := range item.Keywords {
		if strings.ToLower(keyword) == query {
			return 0
		}
	}
	for _, keyword := range item.Keywords {
		if strings.HasPrefix(strings.ToLower(keyword), query) {
			return 1
		}
	}
	if strings.HasPrefix(strings.ToLower(item.Name), query) {
		return 2
	}
	return 3
}
