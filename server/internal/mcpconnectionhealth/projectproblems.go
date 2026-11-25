package mcpconnectionhealth

import (
	"context"
	"slices"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	l, err := s.readProblems(ctx, c, "", nil, s.now())
	if err != nil {
		return nil, err
	}
	result := &gen.ListProjectSuspectedProblemsResult{LoggingEnabled: l.loggingEnabled, ViewerScope: c.viewerScope(), Problems: []*gen.ProjectSuspectedProblem{}, ProblemsTotal: 0}
	limit := maxProblems
	if payload.Limit != nil {
		limit = min(max(*payload.Limit, 1), maxProblems)
	}
	l.problems, result.ProblemsTotal = capProblems(l.problems, limit)
	if len(l.problems) == 0 {
		return result, nil
	}
	ids := newCollector()
	for _, p := range l.problems {
		listedIDs(ids, p)
		for _, e := range p.Episodes {
			ids.server(e.ServerID)
		}
	}
	n, err := s.resolve(ctx, c, ids)
	if err != nil {
		return nil, err
	}
	gateway := orggateway.ProjectGatewayID(c.organizationID, c.projectID).String()
	gatewayIssuer, err := s.projectGatewayIssuer(ctx, c, l.problems, gateway)
	if err != nil {
		return nil, err
	}
	for _, p := range l.problems {
		view, _ := problemView(n, p, "")
		issuer, server := problemLinks(n, p, gateway, gatewayIssuer)
		result.Problems = append(result.Problems, &gen.ProjectSuspectedProblem{
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
			PeopleOnServerCount: view.PeopleOnServerCount,
			Headline:            view.Headline,
			Fact:                view.Fact,
			Filters:             view.Filters,
		})
	}
	return result, nil
}

// projectGatewayIssuer is the organization gateway's MCP sign-in when a problem is on the gateway's view of the project, else nil.
func (s *Service) projectGatewayIssuer(ctx context.Context, c caller, ps []problems.Problem, gateway string) (uuid.UUID, error) {
	onGateway := slices.ContainsFunc(ps, func(p problems.Problem) bool {
		return slices.ContainsFunc(p.Episodes, func(e mcpconnectionattempts.FailingPerson) bool {
			return serverKinds[e.ServerKind] == "gateway" && e.ServerID == gateway
		})
	})
	if !onGateway {
		return uuid.Nil, nil
	}
	issuer, _, err := orggateway.Resolve(ctx, usersessionsrepo.New(s.db), c.organizationID)
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "resolve organization gateway issuer").LogError(ctx, s.logger)
	}
	return issuer, nil
}

// problemLinks are the MCP sign-in whose pages list the problem, and the first MCP server on one that its people fail on directly.
func problemLinks(n names, p problems.Problem, gateway string, gatewayIssuer uuid.UUID) (*string, *gen.SuspectedProblemLinkServer) {
	issuer := uuid.Nil
	for _, e := range p.Episodes {
		if row, ok := n.lookup(e.ServerID, "mcp"); ok && row.Kind == "mcp" && serverKinds[e.ServerKind] == "mcp" && row.IssuerID.Valid {
			return new(row.IssuerID.UUID.String()), &gen.SuspectedProblemLinkServer{ID: row.ID.String(), Slug: conv.FromPGText[string](row.Slug), Name: &row.Name}
		}
		if issuer == uuid.Nil {
			issuer = frontIssuer(n, e, gateway, gatewayIssuer)
		}
	}
	if issuer == uuid.Nil {
		return nil, nil
	}
	return new(issuer.String()), nil
}

// frontIssuer is the MCP sign-in of where the episode's app connects, nil when unknown.
func frontIssuer(n names, e mcpconnectionattempts.FailingPerson, gateway string, gatewayIssuer uuid.UUID) uuid.UUID {
	kind := serverKinds[e.ServerKind]
	if kind == "gateway" && e.ServerID == gateway {
		return gatewayIssuer
	}
	if kind == "" || kind == "toolset" {
		return uuid.Nil
	}
	if row, ok := n.lookup(e.ServerID, kind); ok && row.Kind == kind && row.IssuerID.Valid {
		return row.IssuerID.UUID
	}
	return uuid.Nil
}
