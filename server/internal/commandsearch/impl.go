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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/command_search"
	srv "github.com/speakeasy-api/gram/server/gen/http/command_search/server"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/commandsearch/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

const (
	indexBudget             = 250 * time.Millisecond
	perKindLimit            = 20
	perKindQueryLimit       = perKindLimit + 1
	rowAuthorizedQueryLimit = int32(1<<31 - 1)
	agentSearchBatchSize    = 200
	// Agent readability is evaluated in Go. Walk bounded keyset pages so denied
	// rows cannot hide later readable matches without materializing the tenant's
	// entire matching inventory in one query. Match the canonical agent roster's
	// hard scan ceiling and fail rather than silently report exhaustion.
	maxAgentSearchRowsScanned = 20_000
	globalLimit               = 40
)

var allKinds = []string{"agent_identity", "assistant", "environment", "gateway", "mcp", "person", "plugin", "rule", "skill"}

type queryStore interface {
	ListCommandIndexAssistants(context.Context, repo.ListCommandIndexAssistantsParams) ([]repo.ListCommandIndexAssistantsRow, error)
	ListCommandIndexMCPServers(context.Context, repo.ListCommandIndexMCPServersParams) ([]repo.ListCommandIndexMCPServersRow, error)
	ListCommandIndexSkills(context.Context, repo.ListCommandIndexSkillsParams) ([]repo.ListCommandIndexSkillsRow, error)
	ListCommandIndexCustomRules(context.Context, repo.ListCommandIndexCustomRulesParams) ([]repo.ListCommandIndexCustomRulesRow, error)
	ListCommandIndexEnvironments(context.Context, repo.ListCommandIndexEnvironmentsParams) ([]repo.ListCommandIndexEnvironmentsRow, error)
	ListCommandIndexGateways(context.Context, repo.ListCommandIndexGatewaysParams) ([]repo.ListCommandIndexGatewaysRow, error)
	ListCommandIndexPlugins(context.Context, repo.ListCommandIndexPluginsParams) ([]repo.ListCommandIndexPluginsRow, error)
	ListCommandIndexPeople(context.Context, repo.ListCommandIndexPeopleParams) ([]repo.ListCommandIndexPeopleRow, error)
	ListCommandIndexAgentIdentities(context.Context, repo.ListCommandIndexAgentIdentitiesParams) ([]repo.ListCommandIndexAgentIdentitiesRow, error)
}

type agentIdentityAuthorizer interface {
	RequireHuman(context.Context, agentsrepo.DBTX) (agentmanagement.HumanContext, error)
	Permissions(context.Context, agentmanagement.HumanContext, agentsrepo.Agent) (agentmanagement.AgentPermissions, error)
}

type featureChecker interface {
	IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error)
}

type authorizer interface {
	Evaluate(context.Context, ...authz.Check) (bool, error)
	Filter(context.Context, []authz.Check) ([]string, error)
	RequirePluginWrite(context.Context, string, string) error
}

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	auth      *auth.Auth
	repo      queryStore
	db        agentsrepo.DBTX
	agentAuth agentIdentityAuthorizer
	authz     authorizer
	features  featureChecker
	budget    time.Duration
}

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, authzEngine *authz.Engine, features featureChecker) *Service {
	return &Service{tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/commandsearch"), logger: logger, auth: auth.New(logger, db, sessionManager, authzEngine), repo: repo.New(db), db: db, agentAuth: agentmanagement.NewAuthorizer(authzEngine), authz: authzEngine, features: features, budget: indexBudget}
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
	kind     string
	run      func(context.Context) ([]*gen.Commandresourceindexitem, error)
	rowCheck func(string) authz.Check
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
	skillsEnabled, err := s.features.IsFeatureEnabled(indexCtx, organizationID, productfeatures.FeatureSkills)
	if err != nil {
		return nil, fmt.Errorf("check skills feature for command search: %w", err)
	}
	authorized, err := s.authorizedKinds(indexCtx, organizationID, projectID)
	if err != nil {
		return nil, err
	}

	authorized["skill"] = authorized["skill"] && skillsEnabled
	adapters := s.adapters(organizationID, projectID, query, authorized)
	results := make(chan adapterResult, len(adapters))
	for _, a := range adapters {
		go func() {
			started := time.Now()
			items, err := a.run(indexCtx)
			if err == nil && a.rowCheck != nil {
				items, err = s.filterRows(indexCtx, items, a.rowCheck)
			}
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
			out = append(out, adapter{kind: kind, run: fn, rowCheck: nil})
		}
	}
	add("agent_identity", func(ctx context.Context) ([]*gen.Commandresourceindexitem, error) {
		if s.agentAuth == nil {
			return nil, nil
		}
		human, e := s.agentAuth.RequireHuman(ctx, s.db)
		if e != nil {
			var denied *oops.ShareableError
			if errors.As(e, &denied) && (denied.Code == oops.CodeForbidden || denied.Code == oops.CodeUnauthorized) {
				return nil, nil
			}
			return nil, fmt.Errorf("authorize command search agent identities: %w", e)
		}
		x := make([]*gen.Commandresourceindexitem, 0, perKindQueryLimit)
		var cursorName pgtype.Text
		var cursorID uuid.NullUUID
		scanned := 0
		for len(x) < perKindQueryLimit {
			rows, e := s.repo.ListCommandIndexAgentIdentities(ctx, repo.ListCommandIndexAgentIdentitiesParams{
				OrganizationID: org, LowerBound: bounds.lower, LowerCeiling: bounds.lowerCeiling,
				CursorName: cursorName, CursorID: cursorID, ResultLimit: agentSearchBatchSize,
			})
			if e != nil {
				return nil, fmt.Errorf("list command search agent identities: %w", e)
			}
			if len(rows) == 0 {
				break
			}
			scanned += len(rows)
			for _, r := range rows {
				permissions, authErr := s.agentAuth.Permissions(ctx, human, agentsrepo.Agent{
					ID: r.ID, OrganizationID: r.OrganizationID, OwnerUserID: r.OwnerUserID,
					ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Name: r.Name,
					SuspendedAt:                 pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
					RevokedAt:                   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
					OwnerReassignmentRequiredAt: r.OwnerReassignmentRequiredAt,
					OwnerReassignmentReason:     pgtype.Text{String: "", Valid: false},
					CreatedAt:                   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
					UpdatedAt:                   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
					DeletedAt:                   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
					Deleted:                     false,
				})
				if authErr != nil {
					return nil, fmt.Errorf("authorize command search agent identity row: %w", authErr)
				}
				if permissions.Read {
					canWrite := permissions.Write
					x = append(x, &gen.Commandresourceindexitem{Kind: "agent_identity", ID: r.ID.String(), Name: r.Name, RouteSelector: r.ID.String(), Keywords: nil, CanWrite: &canWrite})
					if len(x) == perKindQueryLimit {
						break
					}
				}
			}
			if len(x) == perKindQueryLimit || len(rows) < agentSearchBatchSize {
				break
			}
			last := rows[len(rows)-1]
			cursorName = pgtype.Text{String: strings.ToLower(last.Name), Valid: true}
			cursorID = uuid.NullUUID{UUID: last.ID, Valid: true}
			if scanned >= maxAgentSearchRowsScanned {
				return nil, fmt.Errorf("agent identity search exceeded %d matching rows; narrow the query", maxAgentSearchRowsScanned)
			}
		}
		return x, nil
	})
	add("assistant", func(ctx context.Context) ([]*gen.Commandresourceindexitem, error) {
		rows, e := s.repo.ListCommandIndexAssistants(ctx, repo.ListCommandIndexAssistantsParams{LowerBound: bounds.lower, LowerCeiling: bounds.lowerCeiling, ProjectID: project, ResultLimit: rowAuthorizedQueryLimit})
		if e != nil {
			return nil, fmt.Errorf("list command search assistants: %w", e)
		}
		x := make([]*gen.Commandresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Commandresourceindexitem{Kind: "assistant", ID: r.ID.String(), Name: r.Name, RouteSelector: r.ID.String(), Keywords: nil, CanWrite: nil})
		}
		return x, nil
	})
	add("environment", func(ctx context.Context) ([]*gen.Commandresourceindexitem, error) {
		rows, e := s.repo.ListCommandIndexEnvironments(ctx, repo.ListCommandIndexEnvironmentsParams{LowerBound: bounds.lower, LowerCeiling: bounds.lowerCeiling, ProjectID: project, ResultLimit: limit})
		if e != nil {
			return nil, fmt.Errorf("list command search environments: %w", e)
		}
		x := make([]*gen.Commandresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Commandresourceindexitem{Kind: "environment", ID: r.ID.String(), Name: r.Name, RouteSelector: r.Slug, Keywords: compactKeywords(r.Name, r.Slug), CanWrite: nil})
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
			x = append(x, &gen.Commandresourceindexitem{Kind: "gateway", ID: r.ID.String(), Name: r.Name, RouteSelector: r.ID.String(), Keywords: nil, CanWrite: nil})
		}
		return x, nil
	})
	add("mcp", func(ctx context.Context) ([]*gen.Commandresourceindexitem, error) {
		rows, e := s.repo.ListCommandIndexMCPServers(ctx, repo.ListCommandIndexMCPServersParams{LowerBound: bounds.lower, LowerCeiling: bounds.lowerCeiling, ProjectID: project, ResultLimit: rowAuthorizedQueryLimit})
		if e != nil {
			return nil, fmt.Errorf("list command search MCP servers: %w", e)
		}
		x := make([]*gen.Commandresourceindexitem, 0, len(rows))
		for _, r := range rows {
			name, slug := r.Name.String, r.Slug.String
			if name == "" {
				name = slug
			}
			selector := slug
			if isReservedMCPRoute(selector) || selector == "" {
				selector = r.RouteID.String()
			}
			x = append(x, &gen.Commandresourceindexitem{Kind: "mcp", ID: r.ID.String(), Name: name, RouteSelector: selector, Keywords: compactKeywords(name, slug), CanWrite: nil})
		}
		return x, nil
	})
	add("person", func(ctx context.Context) ([]*gen.Commandresourceindexitem, error) {
		rows, e := s.repo.ListCommandIndexPeople(ctx, repo.ListCommandIndexPeopleParams{OrganizationID: org, LowerBound: bounds.lower, LowerCeiling: bounds.lowerCeiling, ResultLimit: limit})
		if e != nil {
			return nil, fmt.Errorf("list command search people: %w", e)
		}
		x := make([]*gen.Commandresourceindexitem, 0, len(rows))
		for _, r := range rows {
			name := r.DisplayName
			if name == "" {
				name = r.Email
			}
			x = append(x, &gen.Commandresourceindexitem{Kind: "person", ID: r.ID, Name: name, RouteSelector: r.ID, Keywords: compactKeywords(name, r.Email), CanWrite: nil})
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
			x = append(x, &gen.Commandresourceindexitem{Kind: "plugin", ID: r.ID.String(), Name: r.Name, RouteSelector: r.ID.String(), Keywords: compactKeywords(r.Name, r.Slug), CanWrite: nil})
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
			x = append(x, &gen.Commandresourceindexitem{Kind: "rule", ID: r.ID.String(), Name: r.Title, RouteSelector: r.RuleID, Keywords: compactKeywords(r.Title, r.RuleID, r.Severity), CanWrite: nil})
		}
		return x, nil
	})
	add("skill", func(ctx context.Context) ([]*gen.Commandresourceindexitem, error) {
		rows, e := s.repo.ListCommandIndexSkills(ctx, repo.ListCommandIndexSkillsParams{LowerBound: bounds.lower, LowerCeiling: bounds.lowerCeiling, ProjectID: project, ResultLimit: limit})
		if e != nil {
			return nil, fmt.Errorf("list command search skills: %w", e)
		}
		x := make([]*gen.Commandresourceindexitem, 0, len(rows))
		for _, r := range rows {
			x = append(x, &gen.Commandresourceindexitem{Kind: "skill", ID: r.ID.String(), Name: r.DisplayName, RouteSelector: r.ID.String(), Keywords: compactKeywords(r.DisplayName, r.Name), CanWrite: nil})
		}
		return x, nil
	})

	// Assistant and MCP grants can name one resource or exclude individual
	// resources. Fetch every prefix match for those kinds, authorize each row,
	// and only then apply the response cap so denied rows cannot hide a later
	// authorized match. The browser still receives at most perKindLimit rows.
	rowCheck := func(kind string, check func(string) authz.Check) {
		for i := range out {
			if out[i].kind == kind {
				out[i].rowCheck = check
			}
		}
	}
	rowCheck("assistant", func(id string) authz.Check {
		return authz.AssistantCheck(authz.ScopeAssistantRead, id, project.String())
	})
	rowCheck("mcp", func(id string) authz.Check {
		return authz.MCPCheck(authz.ScopeMCPRead, id, project.String())
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
	checks := []struct {
		kinds []string
		check authz.Check
	}{
		{[]string{"environment"}, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}},
		// Matches metamcp.ListMetaMcpServers. MCP checks must carry the project
		// dimension: the engine rejects one without it as malformed.
		{[]string{"gateway"}, authz.MCPCheck(authz.ScopeMCPRead, projectID.String(), projectID.String())},
		// Match access.listMembers: organization readers and project readers can
		// both use the active member roster. Duplicate kind checks are ORed below.
		{[]string{"person"}, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: authz.ResourceKindOrg, ResourceID: organizationID, Dimensions: nil}},
		{[]string{"person"}, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}},
		{[]string{"plugin"}, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: authz.ResourceKindOrg, ResourceID: organizationID, Dimensions: nil}},
		{[]string{"rule"}, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: organizationID, Dimensions: nil}},
		{[]string{"skill"}, authz.Check{Scope: authz.ScopeSkillRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}},
	}
	// Assistant and MCP rows are authorized after their bounded prefix queries,
	// matching their list endpoints and allowing resource-specific grants.
	result := map[string]bool{"agent_identity": true, "assistant": true, "mcp": true}
	for _, candidate := range checks {
		ok, err := s.authz.Evaluate(ctx, candidate.check)
		if err != nil {
			return nil, fmt.Errorf("authorize command search kind %s: %w", strings.Join(candidate.kinds, "/"), err)
		}
		for _, kind := range candidate.kinds {
			result[kind] = result[kind] || ok
		}
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

// filterRows keeps only rows the caller is individually authorized to read.
func (s *Service) filterRows(ctx context.Context, rows []*gen.Commandresourceindexitem, check func(string) authz.Check) ([]*gen.Commandresourceindexitem, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	checks := make([]authz.Check, len(rows))
	for i, row := range rows {
		checks[i] = check(row.ID)
	}
	allowedIDs, err := s.authz.Filter(ctx, checks)
	if err != nil {
		return nil, fmt.Errorf("authorize command search rows: %w", err)
	}
	allowed := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = struct{}{}
	}
	kept := make([]*gen.Commandresourceindexitem, 0, len(allowedIDs))
	for _, row := range rows {
		if _, ok := allowed[row.ID]; ok {
			kept = append(kept, row)
		}
	}
	return kept, nil
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
