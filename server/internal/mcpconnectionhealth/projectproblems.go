package mcpconnectionhealth

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"runtime/debug"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func (s *Service) ListProjectSuspectedProblems(ctx context.Context, payload *gen.ListProjectSuspectedProblemsPayload) (res *gen.ListProjectSuspectedProblemsResult, err error) {
	ctx, cancel := s.problemsContext(ctx)
	defer cancel()
	defer func() { err = s.answerProblems(ctx, err) }()
	c, err := s.beginLogs(ctx)
	if err != nil {
		return nil, err
	}
	result := &gen.ListProjectSuspectedProblemsResult{LoggingEnabled: false, ViewerScope: c.viewerScope(), Problems: []*gen.ProjectSuspectedProblem{}, ProblemsTotal: 0}
	if result.LoggingEnabled, err = s.loggingOn(ctx, c); err != nil || !result.LoggingEnabled {
		return result, err
	}
	// A problem names other people.
	if c.ownSubjects != nil {
		return result, nil
	}
	// The start is when the organization's logs were last turned on, since nothing is recorded while they are off.
	recordedSince, err := s.logsOnSince(ctx, c.organizationID)
	if err != nil {
		return nil, err
	}
	read, err := s.sharedProjectProblems(ctx, c, recordedSince)
	if err != nil {
		return nil, err
	}
	if !read.loggingEnabled {
		result.LoggingEnabled = false
		return result, nil
	}
	shown := read.problems[:min(len(read.problems), conv.PtrValOr(payload.Limit, maxProblems))]
	if result.Problems, err = s.linksForCaller(ctx, c, read, shown); err != nil {
		return nil, err
	}
	result.ProblemsTotal = int64(len(read.problems))
	return result, nil
}

// projectRead is the project's problems as every caller with log:read sees them, before their own permissions.
type projectRead struct {
	loggingEnabled bool
	problems       []*gen.ProjectSuspectedProblem

	// grants are the ids the linked servers' permissions name.
	grants map[string]uuid.UUID
}

// sharedProjectProblems runs one read per project and recording start for every concurrent caller, detached from the first caller's cancellation.
func (s *Service) sharedProjectProblems(ctx context.Context, c caller, recordedSince time.Time) (projectRead, error) {
	key := c.organizationID + "\x00" + c.projectID.String() + "\x00" + strconv.FormatInt(recordedSince.UnixNano(), 10)
	results := s.projectFlights.DoChan(key, func() (read any, err error) {
		flightCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.problemsDeadline)
		defer cancel()
		if s.flightDone != nil {
			defer s.flightDone()
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				cause := fmt.Errorf("project suspected problems read panicked: %v", recovered)
				s.logger.ErrorContext(flightCtx, "project suspected problems read panicked", attr.SlogError(cause), attr.SlogErrorStack(string(debug.Stack())))
				read, err = nil, oops.E(oops.CodeUnexpected, cause, "read project suspected problems")
			}
		}()
		if s.flightStarted != nil {
			s.flightStarted()
		}
		got, err := s.projectProblems(flightCtx, c, recordedSince)
		return got, s.answerProblems(flightCtx, err)
	})
	if s.flightJoined != nil {
		s.flightJoined()
	}
	var result singleflight.Result
	select {
	case result = <-results:
	case <-ctx.Done():
		// The shared read logs its own failure; a waiter only answers.
		return projectRead{}, oops.E(oops.CodeUnavailable, ctx.Err(), problemsUnavailable)
	}
	if result.Err != nil {
		return projectRead{}, result.Err
	}
	read, ok := result.Val.(projectRead)
	if !ok {
		return projectRead{}, s.unexpected(ctx, errors.New("unexpected shared result"), "read project suspected problems")
	}
	return read, nil
}

// projectProblems groups the project's failures across its MCP sign-ins, in the MCP sign-in list's order, with the pages that list each.
func (s *Service) projectProblems(ctx context.Context, c caller, recordedSince time.Time) (projectRead, error) {
	out := projectRead{loggingEnabled: true, problems: []*gen.ProjectSuspectedProblem{}, grants: map[string]uuid.UUID{}}
	g, err := s.groupProblems(ctx, problems.ReadScope{
		OrganizationID: c.organizationID, ProjectID: c.projectID, WholeOrganization: false,
		IssuerID: uuid.Nil, IssuerServerIDs: nil,
	}, recordedSince, s.now())
	switch {
	case errors.Is(err, mcpconnectionattempts.ErrLogsOff):
		out.loggingEnabled = false
		return out, nil
	case err != nil:
		return out, err
	}
	ps, _ := capProblems(slices.Clone(g.Problems), len(g.Problems))
	if len(ps) == 0 {
		return out, nil
	}
	ids := newCollector()
	for _, p := range ps {
		listedIDs(ids, p)
	}
	n, err := s.resolve(ctx, c, ids)
	if err != nil {
		return out, err
	}
	si, err := s.signIns(ctx, c, g.Episodes())
	if err != nil {
		return out, err
	}
	pages := pageLists{signIns: si, grouped: g, lists: map[uuid.UUID][]problems.Problem{}, listed: map[string]map[string]bool{}}
	for _, p := range ps {
		view, _ := problemView(n, p, "")
		item := &gen.ProjectSuspectedProblem{
			UserSessionIssuerID: nil,
			Server:              nil,
			ID:                  view.ID,
			Rule:                view.Rule,
			Part:                view.Part,
			WhoFixes:            view.WhoFixes,
			Scope:               view.Scope,
			Since:               view.Since,
			LastFailedAt:        view.LastFailedAt,
			PeopleCount:         view.PeopleCount,
			PeopleOnServerCount: nil,
			Headline:            view.Headline,
			Fact:                view.Fact,
			Filters:             view.Filters,
		}
		issuer, server := si.links(p)
		if issuer != uuid.Nil && pages.onSignIn(issuer, p) {
			item.UserSessionIssuerID = new(issuer.String())
			if row, ok := si.servers[server]; ok && row.IssuerID == issuer && row.SessionsTab && pages.onServer(issuer, server, p) {
				item.Server = &gen.SuspectedProblemServerLink{ID: server, Slug: conv.FromPGText[string](row.Slug), Name: n.serverName(server)}
				out.grants[server] = row.GrantID
			}
		}
		out.problems = append(out.problems, item)
	}
	return out, nil
}

// linksForCaller keeps the links whose pages the caller may open, copying a problem rather than changing the shared one.
func (s *Service) linksForCaller(ctx context.Context, c caller, read projectRead, shown []*gen.ProjectSuspectedProblem) ([]*gen.ProjectSuspectedProblem, error) {
	project := authz.ProjectIDOf(c.projectID)
	// The MCP sign-in pages and a server's Sessions tab need project:read.
	readsProject, err := s.authz.Can(ctx, authz.Project(project).Read())
	if err != nil {
		return nil, err //nolint:wrapcheck // authz errors carry the code the caller must see
	}
	var checks []authz.Check
	seen := map[uuid.UUID]bool{}
	for _, p := range shown {
		if p.Server == nil {
			continue
		}
		if grant := read.grants[p.Server.ID]; !seen[grant] {
			seen[grant] = true
			checks = append(checks, authz.MCPServer(authz.MCPServerIDOf(grant), project).Read())
		}
	}
	allowed := map[string]bool{}
	if readsProject && len(checks) > 0 {
		ids, err := s.authz.Filter(ctx, checks)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			allowed[id] = true
		}
	}
	out := make([]*gen.ProjectSuspectedProblem, len(shown))
	for i, p := range shown {
		keepSignIn := readsProject || p.UserSessionIssuerID == nil
		keepServer := p.Server == nil || (readsProject && allowed[read.grants[p.Server.ID].String()])
		if keepSignIn && keepServer {
			out[i] = p
			continue
		}
		cp := *p
		if !keepSignIn {
			cp.UserSessionIssuerID = nil
		}
		cp.Server = nil
		out[i] = &cp
	}
	return out, nil
}

// signInTable is which live MCP sign-in each server, gateway and recorded MCP sign-in stands for, as Connection Logs attributes failures.
type signInTable struct {
	// owner maps a server, gateway or MCP sign-in id to the live MCP sign-in it belongs to.
	owner map[string]uuid.UUID
	// servers are the live direct MCP servers by id.
	servers map[string]repo.ListServerSignInsRow
}

// signIns loads the MCP sign-ins of the episodes' servers and recorded MCP sign-ins.
func (s *Service) signIns(ctx context.Context, c caller, episodes []mcpconnectionattempts.FailingPerson) (signInTable, error) {
	out := signInTable{owner: map[string]uuid.UUID{}, servers: map[string]repo.ListServerSignInsRow{}}
	gateway := orggateway.ProjectGatewayID(c.organizationID, c.projectID).String()
	ids := map[uuid.UUID]struct{}{}
	onGateway := false
	for _, e := range episodes {
		onGateway = onGateway || e.ServerID == gateway
		for _, id := range append([]string{e.ServerID}, e.Issuers...) {
			if parsed, err := uuid.Parse(id); err == nil {
				ids[parsed] = struct{}{}
			}
		}
	}
	rows, err := repo.New(s.db).ListServerSignIns(ctx, repo.ListServerSignInsParams{ProjectID: uuid.NullUUID{UUID: c.projectID, Valid: true}, OrganizationID: c.organizationID, Ids: slices.Collect(maps.Keys(ids))})
	if err != nil {
		return out, s.unexpected(ctx, err, "load MCP sign-ins of servers")
	}
	for _, row := range rows {
		out.owner[row.ID.String()] = row.IssuerID
		if row.Kind == "mcp" && !row.Deleted {
			out.servers[row.ID.String()] = row
		}
	}
	// The organization gateway's view of the project belongs to the organization's MCP sign-in, as its own list attributes it.
	if onGateway {
		issuer, _, err := orggateway.Resolve(ctx, usersessionsrepo.New(s.db), c.organizationID)
		if err != nil {
			return out, s.unexpected(ctx, err, "resolve organization gateway issuer")
		}
		if issuer != uuid.Nil {
			out.owner[gateway] = issuer
		}
	}
	return out, nil
}

// of is the one live MCP sign-in every failure of the episode belongs to, as FailingNowFilter.keeps decides, or nil.
func (si signInTable) of(e mcpconnectionattempts.FailingPerson) uuid.UUID {
	owner := si.owner[e.ServerID]
	switch len(e.Issuers) {
	case 0:
		return owner
	case 1:
		recorded, ok := si.owner[e.Issuers[0]]
		if !ok || recorded.String() != e.Issuers[0] || (e.Unrecorded && owner != recorded) {
			return uuid.Nil
		}
		return recorded
	default:
		return uuid.Nil
	}
}

// partly reports that some, but not every, failure of the episode belongs to the MCP sign-in.
func (si signInTable) partly(e mcpconnectionattempts.FailingPerson, issuer uuid.UUID) bool {
	some := slices.Contains(e.Issuers, issuer.String()) || (e.Unrecorded && si.owner[e.ServerID] == issuer)
	return some && si.of(e) != issuer
}

// links are the MCP sign-in every episode belongs to, and the MCP server every episode reached directly, each only when there is exactly one.
func (si signInTable) links(p problems.Problem) (uuid.UUID, string) {
	issuer, server := uuid.Nil, ""
	for i, e := range p.Episodes {
		id := si.of(e)
		direct := ""
		if serverKinds[e.ServerKind] == "mcp" {
			direct = e.ServerID
		}
		switch {
		case id == uuid.Nil, i > 0 && id != issuer:
			return uuid.Nil, ""
		case i == 0:
			issuer, server = id, direct
		case direct != server:
			server = ""
		}
	}
	return issuer, server
}

// pageLists rebuilds, from the project's read, the list a linked page reads: the grouping
// narrowed to one MCP sign-in, with the page's filter and cap. Each MCP sign-in is grouped once.
type pageLists struct {
	signIns signInTable
	grouped problems.Grouped
	// lists are each MCP sign-in's problems, nil when its read cannot be rebuilt.
	lists map[uuid.UUID][]problems.Problem
	// listed are the ids each page list shows, by MCP sign-in and filter.
	listed map[string]map[string]bool
}

func (pl pageLists) signIn(issuer uuid.UUID) []problems.Problem {
	if ps, ok := pl.lists[issuer]; ok {
		return ps
	}
	var ps []problems.Problem
	// A failure the MCP sign-in's read keeps only part of would group differently there.
	if !slices.ContainsFunc(pl.grouped.Episodes(), func(e mcpconnectionattempts.FailingPerson) bool { return pl.signIns.partly(e, issuer) }) {
		ps = pl.grouped.Within(func(e mcpconnectionattempts.FailingPerson) bool { return pl.signIns.of(e) == issuer })
	}
	pl.lists[issuer] = ps
	return ps
}

// shows reports whether the MCP sign-in's list, filtered as a page filters it and capped, has the problem.
func (pl pageLists) shows(issuer uuid.UUID, filter string, keep func(problems.Problem) bool, id string) bool {
	key := issuer.String() + "\x00" + filter
	ids, ok := pl.listed[key]
	if !ok {
		kept := slices.DeleteFunc(slices.Clone(pl.signIn(issuer)), func(p problems.Problem) bool { return !keep(p) })
		kept, _ = capProblems(kept, maxProblems)
		ids = map[string]bool{}
		for _, p := range kept {
			ids[p.ID] = true
		}
		pl.listed[key] = ids
	}
	return ids[id]
}

// onSignIn checks the MCP sign-in's Overview for a sign-in problem, else its whole list.
func (pl pageLists) onSignIn(issuer uuid.UUID, p problems.Problem) bool {
	if partOf(p.Part) == partSpeakeasy {
		return pl.shows(issuer, string(partSpeakeasy), func(q problems.Problem) bool { return partOf(q.Part) == partSpeakeasy }, p.ID)
	}
	return pl.shows(issuer, "", func(problems.Problem) bool { return true }, p.ID)
}

// onServer checks the server's Sessions tab: the MCP sign-in's problems with people on it.
func (pl pageLists) onServer(issuer uuid.UUID, server string, p problems.Problem) bool {
	return pl.shows(issuer, "server:"+server, func(q problems.Problem) bool { return peopleOn(q, server) > 0 }, p.ID)
}
