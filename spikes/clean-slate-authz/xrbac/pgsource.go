package xrbac

import (
	"context"
	_ "embed"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed pg_schema.sql
var pgSchema string

// PgSource reads today's table shapes inside one read-only repeatable-read
// transaction per operation.
type PgSource struct {
	Pool *pgxpool.Pool
}

func (s *PgSource) Reset(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, pgSchema)
	return err
}

func (s *PgSource) Seed(ctx context.Context, org string, w World) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	b := &pgx.Batch{}
	for _, g := range w.Grants {
		sel, _ := json.Marshal(g.Selectors)
		b.Queue(`INSERT INTO x_principal_grants (id, organization_id, principal_urn, scope, selectors, valid_from, valid_until) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			g.ID, org, g.Principal, string(g.Scope), sel, g.ValidFrom, g.ValidUntil)
	}
	for u, roles := range w.RoleAssignments {
		for _, r := range roles {
			b.Queue(`INSERT INTO x_role_assignments VALUES ($1,$2,$3)`, org, u, r)
		}
	}
	for u, groups := range w.DirectoryMembership {
		for _, g := range groups {
			b.Queue(`INSERT INTO x_directory_memberships VALUES ($1,$2,$3)`, org, u, g)
		}
	}
	for g, roles := range w.DirectoryRoleMaps {
		for _, r := range roles {
			b.Queue(`INSERT INTO x_directory_role_mappings VALUES ($1,$2,$3)`, org, g, r)
		}
	}
	for _, a := range w.Agents {
		b.Queue(`INSERT INTO x_agents VALUES ($1,$2,$3,$4,$5)`, a.ID, org, a.ProjectID, a.OwnerUserID, a.Status)
	}
	for _, c := range w.Credentials {
		var stored []StoredGrant
		for _, r := range c.Policy {
			stored = append(stored, StoredGrant{Scope: r.Scope, Selectors: r.Selector})
		}
		pol, _ := json.Marshal(stored)
		b.Queue(`INSERT INTO x_api_keys VALUES ($1,$2,$3,$4,$5,$6)`, c.ID, org, c.AgentID, c.RevokedAt, c.ExpiresAt, pol)
	}
	for _, c := range w.Chats {
		b.Queue(`INSERT INTO x_chats VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''))`, c.ID, org, c.ProjectID, c.UserID, c.AgentID)
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PgSource) RemoveMembership(ctx context.Context, org, userID, groupID string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM x_directory_memberships WHERE organization_id=$1 AND user_id=$2 AND group_id=$3`, org, userID, groupID)
	return err
}

func (s *PgSource) Begin(ctx context.Context, org string) (SourceTx, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	t := &pgSourceTx{tx: tx, org: org}
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&t.now); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return t, nil
}

type pgSourceTx struct {
	tx  pgx.Tx
	org string
	now time.Time
}

func (t *pgSourceTx) Now() time.Time                  { return t.now }
func (t *pgSourceTx) Close(ctx context.Context) error { return t.tx.Rollback(ctx) }

const userRoles = `
  SELECT user_id, role_urn FROM x_role_assignments WHERE organization_id = $1 AND user_id = ANY($2::text[])
  UNION
  SELECT d.user_id, m.role_urn FROM x_directory_memberships d
  JOIN x_directory_role_mappings m ON m.organization_id = d.organization_id AND m.group_id = d.group_id
  WHERE d.organization_id = $1 AND d.user_id = ANY($2::text[])`

// UserPolicy: principals (user, user:all, direct and directory roles) and
// their grants in one query, plus currently owned agents.
func (t *pgSourceTx) UserPolicy(ctx context.Context, userID string) ([]Rule, []string, error) {
	rows, err := t.tx.Query(ctx, `
		WITH principals AS (
		  SELECT 'user:' || $2::text AS urn UNION SELECT 'user:all'
		  UNION SELECT role_urn FROM (`+strings.ReplaceAll(userRoles, "$2", "$3")+`) r
		)
		SELECT g.id, g.principal_urn, g.scope, g.selectors, g.valid_from, g.valid_until
		FROM x_principal_grants g JOIN principals p ON p.urn = g.principal_urn
		WHERE g.organization_id = $1`, t.org, userID, []string{userID})
	if err != nil {
		return nil, nil, err
	}
	grants, err := scanGrants(rows)
	if err != nil {
		return nil, nil, err
	}
	var owned []string
	rows, err = t.tx.Query(ctx, `SELECT id FROM x_agents WHERE organization_id = $1 AND owner_user_id = $2 AND status = 'active'`, t.org, userID)
	if err != nil {
		return nil, nil, err
	}
	owned, err = pgx.CollectRows(rows, pgx.RowTo[string])
	return DecodeAll(grants), owned, err
}

func (t *pgSourceTx) AgentPolicy(ctx context.Context, agentID string) ([]Rule, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT id, principal_urn, scope, selectors, valid_from, valid_until
		FROM x_principal_grants WHERE organization_id = $1 AND principal_urn = $2`, t.org, "agent:"+agentID)
	if err != nil {
		return nil, err
	}
	grants, err := scanGrants(rows)
	return DecodeAll(grants), err
}

func (t *pgSourceTx) Agent(ctx context.Context, id string) (AgentFacts, error) {
	a := AgentFacts{ID: id}
	err := t.tx.QueryRow(ctx, `SELECT project_id, owner_user_id, status FROM x_agents WHERE id = $1 AND organization_id = $2`, id, t.org).
		Scan(&a.ProjectID, &a.OwnerUserID, &a.Status)
	return a, err
}

func (t *pgSourceTx) Credential(ctx context.Context, id string) (CredentialFacts, error) {
	c := CredentialFacts{ID: id}
	var pol []byte
	err := t.tx.QueryRow(ctx, `SELECT agent_id, revoked_at, expires_at, delegated_grants FROM x_api_keys WHERE id = $1 AND organization_id = $2`, id, t.org).
		Scan(&c.AgentID, &c.RevokedAt, &c.ExpiresAt, &pol)
	if err != nil {
		return c, err
	}
	var stored []StoredGrant
	if err := json.Unmarshal(pol, &stored); err != nil {
		return c, err
	}
	for i := range stored {
		stored[i].Principal = "credential:" + id
	}
	c.Policy = DecodeAll(stored)
	return c, nil
}

func (t *pgSourceTx) SubjectPrincipals(ctx context.Context, targets []Ref) (map[Ref][]string, error) {
	ids := make([]string, len(targets))
	for i, r := range targets {
		ids[i] = r.ID
	}
	rows, err := t.tx.Query(ctx, `SELECT id, COALESCE(user_id,''), COALESCE(agent_id,'') FROM x_chats WHERE organization_id = $1 AND id = ANY($2::text[])`, t.org, ids)
	if err != nil {
		return nil, err
	}
	type chat struct{ id, user, agent string }
	var chats []chat
	var users []string
	for rows.Next() {
		var c chat
		if err := rows.Scan(&c.id, &c.user, &c.agent); err != nil {
			return nil, err
		}
		chats = append(chats, c)
		if c.user != "" {
			users = append(users, c.user)
		}
	}
	rows.Close()
	roles := map[string][]string{}
	if len(users) > 0 {
		rows, err := t.tx.Query(ctx, userRoles, t.org, users)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var u, r string
			if err := rows.Scan(&u, &r); err != nil {
				return nil, err
			}
			roles[u] = append(roles[u], r)
		}
		rows.Close()
	}
	byID := map[string]Ref{}
	for _, r := range targets {
		byID[r.ID] = r
	}
	out := map[Ref][]string{}
	for _, c := range chats {
		switch {
		case c.user != "":
			out[byID[c.id]] = append([]string{"user:" + c.user}, roles[c.user]...)
		case c.agent != "":
			out[byID[c.id]] = []string{"agent:" + c.agent}
		}
	}
	return out, nil
}

func scanGrants(rows pgx.Rows) ([]StoredGrant, error) {
	defer rows.Close()
	var out []StoredGrant
	for rows.Next() {
		var g StoredGrant
		var scope string
		var sel []byte
		if err := rows.Scan(&g.ID, &g.Principal, &scope, &sel, &g.ValidFrom, &g.ValidUntil); err != nil {
			return nil, err
		}
		g.Scope = Scope(scope)
		if err := json.Unmarshal(sel, &g.Selectors); err != nil {
			return nil, err // a malformed row fails the load (D2: never silently drop a denial)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
