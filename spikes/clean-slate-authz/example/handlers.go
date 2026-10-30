// Package example shows how handlers use the engine. It compiles against the
// spike packages but is not wired to an HTTP server.
package example

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/model"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/objects"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/perm"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/pgstore"
)

type Service struct {
	authz *authz.Engine
	store *pgstore.Store
}

// Middleware admits the caller once per request. Handlers never load grants.
func (s *Service) Middleware(next http.Handler, authenticate func(*http.Request) (authz.Credential, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cred, err := authenticate(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		p, err := s.authz.Admit(r.Context(), cred)
		if err != nil {
			http.Error(w, "authorization unavailable", http.StatusServiceUnavailable) // fail closed
			return
		}
		next.ServeHTTP(w, r.WithContext(authz.WithPrincipal(r.Context(), p)))
	})
}

type toolsetRow struct{ ID, ProjectID, OrganizationID string }

// mcpServerObject is the one-line adapter each domain writes next to its queries.
func mcpServerObject(t toolsetRow) objects.MCPServer {
	return objects.MCPServer{ID: t.ID, ProjectID: t.ProjectID, OrganizationID: t.OrganizationID}
}

// UpdateToolset: load the row (tenant-scoped), then one line of authorization.
func (s *Service) UpdateToolset(ctx context.Context, ts toolsetRow) error {
	if err := authz.Require(ctx, s.authz, perm.MCPServerEdit, mcpServerObject(ts)); err != nil {
		return err
	}
	// ... mutate
	return nil
}

// CallTool: the same server check, plus the facts its conditions need.
func (s *Service) CallTool(ctx context.Context, ts toolsetRow, tool, disposition string) error {
	return authz.Require(ctx, s.authz, perm.MCPServerConnect, mcpServerObject(ts),
		authz.With(model.MCPToolContext{Tool: tool, Disposition: disposition}))
}

// ListChats: authorization becomes part of the query, so pagination and counts
// only ever see authorized rows. The same Filter would feed ClickHouse.
func (s *Service) ListChats(ctx context.Context, projectID string, limit int) ([]string, error) {
	f, err := authz.List(ctx, s.authz, perm.ChatRead)
	if err != nil {
		return nil, err
	}
	args := []any{projectID, limit}
	where, err := f.SQL(map[string]string{
		"id": "c.id", "project": "c.project_id", "author": "c.user_id", "agent": "c.agent_id", "agent.owner": "a.owner_user_id",
	}, &args)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.Pool.Query(ctx, `
		SELECT c.id FROM spike_chats c LEFT JOIN spike_agents a ON a.id = c.agent_id
		WHERE c.project_id = $1 AND `+where+` ORDER BY c.id LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CreateAgent: self-service creation, delegation containment, then one
// transaction for the agent row and its policy relationships.
func (s *Service) CreateAgent(ctx context.Context, org objects.Organization, project objects.Project, server objects.MCPServer, tools []string) (string, error) {
	if err := authz.Require(ctx, s.authz, perm.OrganizationCreateAgent, org); err != nil {
		return "", err
	}
	p, _ := authz.PrincipalFrom(ctx)
	params, _ := json.Marshal(model.MCPToolsParams{Tools: tools})
	proposal := authz.Proposal{ForType: model.MCPServer, Grant: "connector", Anchor: server.AuthzRef(), Params: params}
	if err := s.authz.CanGrant(ctx, p.Actor.ID, []authz.Proposal{proposal}); err != nil {
		return "", err
	}

	agentID := "agt_" + uuid.NewString()[:8]
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO spike_agents VALUES ($1,$2,$3,$4,'active')`, agentID, org.ID, project.ID, p.Actor.ID); err != nil {
		return "", err
	}
	if err := s.store.Write(ctx, tx, org.ID, authz.Relationship{
		ID: uuid.Must(uuid.NewV7()), Object: authz.ObjectID{Type: model.MCPServer, ID: server.ID}, Relation: "connector",
		Subject: authz.Agent(agentID), ConditionName: model.CondMCPTools, ConditionParams: params, Source: "self_service",
	}); err != nil {
		return "", err
	}
	return agentID, tx.Commit(ctx) // the trigger bumps the revision in this same commit
}

// StreamChat: a long-lived read re-checks as it goes; expiry or revocation
// ends delivery without the client's cooperation.
func (s *Service) StreamChat(ctx context.Context, cred authz.Credential, c objects.Chat, events <-chan string, send func(string) error) error {
	g := authz.NewGuard(ctx, s.authz, cred, perm.ChatRead, c, 15*time.Second)
	for ev := range events {
		if err := g.Still(); err != nil {
			return fmt.Errorf("stream ended: %w", err)
		}
		if err := send(ev); err != nil {
			return err
		}
	}
	return nil
}
