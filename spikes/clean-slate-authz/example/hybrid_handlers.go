package example

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/hmodel"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/hybrid"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/model"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/objects"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/perm"
)

// Option D: handlers are written exactly as in Option A (handlers.go). Only
// the wiring differs — the engine reads today's tables through the mapping.

func NewHybridEngine(pool *pgxpool.Pool) (*authz.Engine, *hybrid.Mapping, error) {
	m, err := hybrid.NewMapping(hmodel.Schema) // fails at startup if two relations claim one scope
	if err != nil {
		return nil, nil, err
	}
	return authz.New(hmodel.Schema, &hybrid.PgStore{Pool: pool, M: m}, nil), m, nil
}

type HybridService struct {
	authz   *authz.Engine
	mapping *hybrid.Mapping
	pool    *pgxpool.Pool
}

func (s *HybridService) CallTool(ctx context.Context, ts toolsetRow, tool, disposition string) error {
	return authz.Require(ctx, s.authz, perm.MCPServerConnect, mcpServerObject(ts),
		authz.With(model.MCPToolContext{Tool: tool, Disposition: disposition}))
}

// GrantAgentTool: containment in schema terms, then Encode writes the row in
// today's principal_grants shape — the role editor and APIs keep working.
func (s *HybridService) GrantAgentTool(ctx context.Context, org string, server objects.MCPServer, agentID, tool string) error {
	p, _ := authz.PrincipalFrom(ctx)
	params, _ := json.Marshal(model.MCPToolsParams{Tools: []string{tool}})
	if err := s.authz.CanGrant(ctx, p.Actor.ID, []authz.Proposal{{
		ForType: "mcp_server", Grant: "connector", Anchor: server.AuthzRef(), Params: params,
	}}); err != nil {
		return err
	}
	row, err := s.mapping.Encode(org, authz.Relationship{
		Object: authz.ObjectID{Type: "mcp_server", ID: server.ID}, Relation: "connector",
		Subject: authz.Agent(agentID), ConditionName: model.CondMCPTools, ConditionParams: params, SourceRef: "<NEW_GRANT_ID>",
	})
	if err != nil {
		return err
	}
	sel, _ := json.Marshal(row.Selectors)
	_, err = s.pool.Exec(ctx, `INSERT INTO x_principal_grants (id, organization_id, principal_urn, scope, selectors) VALUES ($1,$2,$3,$4,$5)`,
		row.ID, org, row.Principal, string(row.Scope), sel)
	return err
}
