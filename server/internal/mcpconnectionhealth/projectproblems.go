package mcpconnectionhealth

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func (s *Service) ListProjectSuspectedProblems(ctx context.Context, payload *gen.ListProjectSuspectedProblemsPayload) (res *gen.ListProjectSuspectedProblemsResult, err error) {
	ctx, cancel := context.WithTimeout(ctx, s.problemsDeadline)
	defer cancel()
	defer func() { err = s.pastDeadline(ctx, err) }()
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
	all, err := s.sharedProjectProblems(ctx, c)
	if err != nil {
		return nil, err
	}
	result.Problems = all[:min(len(all), conv.PtrValOr(payload.Limit, maxProblems))]
	result.ProblemsTotal = int64(len(all))
	return result, nil
}

// sharedProjectProblems runs one read of the project's problems for every concurrent caller, detached from the first caller's cancellation.
func (s *Service) sharedProjectProblems(ctx context.Context, c caller) ([]*gen.ProjectSuspectedProblem, error) {
	results := s.projectFlights.DoChan(c.organizationID+"\x00"+c.projectID.String(), func() (any, error) {
		flightCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.problemsDeadline)
		defer cancel()
		if s.flightStarted != nil {
			s.flightStarted()
		}
		all, err := s.projectProblems(flightCtx, c)
		return all, s.pastDeadline(flightCtx, err)
	})
	if s.flightJoined != nil {
		s.flightJoined()
	}
	var result singleflight.Result
	select {
	case result = <-results:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for project suspected problems: %w", ctx.Err())
	}
	if result.Err != nil {
		return nil, result.Err
	}
	all, ok := result.Val.([]*gen.ProjectSuspectedProblem)
	if !ok {
		return nil, s.unexpected(ctx, errors.New("unexpected shared result"), "read project suspected problems")
	}
	return all, nil
}

// projectProblems groups the project's failures across its MCP sign-ins, in the MCP sign-in list's order, with what each links to.
func (s *Service) projectProblems(ctx context.Context, c caller) ([]*gen.ProjectSuspectedProblem, error) {
	ps, err := s.groupProblems(ctx, problems.ReadScope{
		OrganizationID: c.organizationID, ProjectID: c.projectID, WholeOrganization: false,
		IssuerID: uuid.Nil, IssuerServerIDs: nil,
	}, s.now())
	if err != nil {
		return nil, err
	}
	ps, _ = capProblems(ps, len(ps))
	out := make([]*gen.ProjectSuspectedProblem, 0, len(ps))
	if len(ps) == 0 {
		return out, nil
	}
	ids := newCollector()
	for _, p := range ps {
		listedIDs(ids, p)
	}
	n, err := s.resolve(ctx, c, ids)
	if err != nil {
		return nil, err
	}
	si, err := s.signIns(ctx, c, ps)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		view, _ := problemView(n, p, "")
		issuer, server := si.links(n, p)
		out = append(out, &gen.ProjectSuspectedProblem{
			UserSessionIssuerID: issuer,
			Server:              server,
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
		})
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

// signIns loads the MCP sign-ins of the problems' servers and recorded MCP sign-ins.
func (s *Service) signIns(ctx context.Context, c caller, ps []problems.Problem) (signInTable, error) {
	out := signInTable{owner: map[string]uuid.UUID{}, servers: map[string]repo.ListServerSignInsRow{}}
	gateway := orggateway.ProjectGatewayID(c.organizationID, c.projectID).String()
	ids := map[uuid.UUID]struct{}{}
	onGateway := false
	for _, p := range ps {
		for _, e := range p.Episodes {
			onGateway = onGateway || e.ServerID == gateway
			for _, id := range append([]string{e.ServerID}, e.Issuers...) {
				if parsed, err := uuid.Parse(id); err == nil {
					ids[parsed] = struct{}{}
				}
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

// links are the MCP sign-in every episode belongs to, and the live MCP server every episode reached directly, each only when there is exactly one.
func (si signInTable) links(n names, p problems.Problem) (*string, *gen.SuspectedProblemServerLink) {
	issuer, server := uuid.Nil, ""
	for i, e := range p.Episodes {
		id := si.of(e)
		direct := ""
		if serverKinds[e.ServerKind] == "mcp" {
			direct = e.ServerID
		}
		switch {
		case id == uuid.Nil, i > 0 && id != issuer:
			return nil, nil
		case i == 0:
			issuer, server = id, direct
		case direct != server:
			server = ""
		}
	}
	if issuer == uuid.Nil {
		return nil, nil
	}
	row, ok := si.servers[server]
	if !ok || row.IssuerID != issuer {
		return new(issuer.String()), nil
	}
	return new(issuer.String()), &gen.SuspectedProblemServerLink{ID: server, Slug: conv.FromPGText[string](row.Slug), Name: n.serverName(server)}
}
