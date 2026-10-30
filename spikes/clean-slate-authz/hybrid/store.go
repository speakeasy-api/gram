package hybrid

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/xrbac"
)

// raw is what a hybrid store reads from today's tables. Both stores feed the
// same decode path, so memory and Postgres cannot drift.
type raw interface {
	now() time.Time
	grantsFor(ctx context.Context, principals []string) ([]xrbac.StoredGrant, error)
	// userGrants resolves the user's principals and loads their grants together.
	userGrants(ctx context.Context, userID string) ([]xrbac.StoredGrant, error)
	rolesOf(ctx context.Context, userID string) ([]string, error)
	membersOf(ctx context.Context, roles []string) (map[string][]string, error) // role → users
	grantsWithScope(ctx context.Context, scopes []string) ([]xrbac.StoredGrant, error)
	apiKey(ctx context.Context, id string) (authz.APIKeyRow, []xrbac.StoredGrant, error)
	agent(ctx context.Context, id string) (authz.AgentRow, error)
	close(ctx context.Context) error
}

// tx adapts raw rows to the engine's authz.Tx. Principal resolution is
// Option B's (user, user:all, direct and directory roles); evaluation is A's.
type tx struct {
	r   raw
	m   *Mapping
	org string

	keyPolicy map[string][]xrbac.StoredGrant // read once per admission
}

func (t *tx) Revision() int64                 { return 0 } // no revision table yet: admission is uncached
func (t *tx) Now() time.Time                  { return t.r.now() }
func (t *tx) Close(ctx context.Context) error { return t.r.close(ctx) }

func (t *tx) APIKey(ctx context.Context, id string) (authz.APIKeyRow, error) {
	k, policy, err := t.r.apiKey(ctx, id)
	if err == nil {
		t.keyPolicy = map[string][]xrbac.StoredGrant{id: policy}
	}
	return k, err
}

func (t *tx) Agent(ctx context.Context, id string) (authz.AgentRow, error) { return t.r.agent(ctx, id) }

func (t *tx) decode(gs []xrbac.StoredGrant) ([]authz.Relationship, error) {
	out := make([]authz.Relationship, 0, len(gs))
	for _, g := range gs {
		r, err := t.m.Decode(t.org, g)
		if err != nil {
			return nil, err // a row the schema cannot express fails the load
		}
		out = append(out, r)
	}
	return out, nil
}

func (t *tx) SubjectRelationships(ctx context.Context, s authz.Subject) ([]authz.Relationship, error) {
	switch s.Type {
	case "user":
		gs, err := t.r.userGrants(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		return t.decode(gs)
	case "agent":
		gs, err := t.r.grantsFor(ctx, []string{"agent:" + s.ID}) // no owner, no user:all
		if err != nil {
			return nil, err
		}
		return t.decode(gs)
	case "api_key":
		policy, ok := t.keyPolicy[s.ID]
		if !ok {
			var err error
			if _, policy, err = t.r.apiKey(ctx, s.ID); err != nil {
				return nil, err
			}
		}
		return t.decode(policy)
	}
	return nil, errors.New("unsupported subject type " + s.Type)
}

func (t *tx) OrgFacts(ctx context.Context, _ []string, scopes []string) (memberships, marked []authz.Relationship, err error) {
	var roles []string
	for _, s := range scopes {
		roles = append(roles, s) // "role:R"
	}
	if len(roles) > 0 {
		members, err := t.r.membersOf(ctx, roles)
		if err != nil {
			return nil, nil, err
		}
		for role, users := range members {
			id := strings.TrimPrefix(role, "role:")
			for _, u := range users {
				memberships = append(memberships, authz.Relationship{Object: authz.ObjectID{Type: "role", ID: id}, Relation: "member", Subject: authz.User(u)})
			}
		}
	}
	gs, err := t.r.grantsWithScope(ctx, t.m.MarkerScopes())
	if err != nil {
		return nil, nil, err
	}
	if marked, err = t.decode(gs); err != nil {
		return nil, nil, err
	}
	return memberships, marked, nil
}

// MemStore serves an xrbac.World (Option B's data) to the Option A engine.
type MemStore struct {
	mu sync.RWMutex
	W  xrbac.World
	M  *Mapping
}

func (s *MemStore) RemoveMembership(userID, groupID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.W.DirectoryMembership[userID] = slices.DeleteFunc(slices.Clone(s.W.DirectoryMembership[userID]), func(g string) bool { return g == groupID })
}

func (s *MemStore) Revision(context.Context, string) (int64, error) { return 0, nil }

func (s *MemStore) Begin(_ context.Context, org string) (authz.Tx, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w := s.W
	w.DirectoryMembership = map[string][]string{}
	for k, v := range s.W.DirectoryMembership {
		w.DirectoryMembership[k] = slices.Clone(v)
	}
	return &tx{r: &memRaw{w: w, t: time.Now()}, m: s.M, org: org}, nil
}

type memRaw struct {
	w xrbac.World
	t time.Time
}

func (r *memRaw) now() time.Time              { return r.t }
func (r *memRaw) close(context.Context) error { return nil }

func (r *memRaw) roles(userID string) []string {
	roles := slices.Clone(r.w.RoleAssignments[userID])
	for _, g := range r.w.DirectoryMembership[userID] {
		roles = append(roles, r.w.DirectoryRoleMaps[g]...)
	}
	slices.Sort(roles)
	return slices.Compact(roles)
}

func (r *memRaw) rolesOf(_ context.Context, userID string) ([]string, error) {
	return r.roles(userID), nil
}

func (r *memRaw) userGrants(ctx context.Context, userID string) ([]xrbac.StoredGrant, error) {
	return r.grantsFor(ctx, append([]string{"user:" + userID, "user:all"}, r.roles(userID)...))
}

func (r *memRaw) membersOf(_ context.Context, roles []string) (map[string][]string, error) {
	out := map[string][]string{}
	users := map[string]bool{}
	for u := range r.w.RoleAssignments {
		users[u] = true
	}
	for u := range r.w.DirectoryMembership {
		users[u] = true
	}
	for u := range users {
		for _, role := range r.roles(u) {
			if slices.Contains(roles, role) {
				out[role] = append(out[role], u)
			}
		}
	}
	return out, nil
}

func (r *memRaw) grantsFor(_ context.Context, principals []string) ([]xrbac.StoredGrant, error) {
	var out []xrbac.StoredGrant
	for _, g := range r.w.Grants {
		if slices.Contains(principals, g.Principal) {
			out = append(out, g)
		}
	}
	return out, nil
}

func (r *memRaw) grantsWithScope(_ context.Context, scopes []string) ([]xrbac.StoredGrant, error) {
	var out []xrbac.StoredGrant
	for _, g := range r.w.Grants {
		if slices.Contains(scopes, string(g.Scope)) {
			out = append(out, g)
		}
	}
	return out, nil
}

func (r *memRaw) apiKey(_ context.Context, id string) (authz.APIKeyRow, []xrbac.StoredGrant, error) {
	c, ok := r.w.Credentials[id]
	if !ok {
		return authz.APIKeyRow{}, nil, errors.New("key not found")
	}
	var policy []xrbac.StoredGrant
	for i, rule := range c.Policy {
		policy = append(policy, xrbac.StoredGrant{ID: id + "-" + string(rune('a'+i)), Principal: "api_key:" + id, Scope: rule.Scope, Selectors: rule.Selector})
	}
	return authz.APIKeyRow{ID: id, AgentID: c.AgentID, RevokedAt: c.RevokedAt, ExpiresAt: c.ExpiresAt}, policy, nil
}

func (r *memRaw) agent(_ context.Context, id string) (authz.AgentRow, error) {
	a, ok := r.w.Agents[id]
	if !ok {
		return authz.AgentRow{}, errors.New("agent not found")
	}
	return authz.AgentRow{ID: a.ID, OwnerUserID: a.OwnerUserID, ProjectID: a.ProjectID, Status: a.Status}, nil
}

// PgStore reads Option B's x_ tables (seeded by xrbac.PgSource) inside one
// read-only repeatable-read transaction per admission.
type PgStore struct {
	Pool *pgxpool.Pool
	M    *Mapping
}

func (s *PgStore) Revision(context.Context, string) (int64, error) { return 0, nil }

func (s *PgStore) Begin(ctx context.Context, org string) (authz.Tx, error) {
	ptx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	r := &pgRaw{tx: ptx, org: org}
	if err := ptx.QueryRow(ctx, `SELECT now()`).Scan(&r.t); err != nil {
		_ = ptx.Rollback(ctx)
		return nil, err
	}
	return &tx{r: r, m: s.M, org: org}, nil
}

type pgRaw struct {
	tx  pgx.Tx
	org string
	t   time.Time
}

func (r *pgRaw) now() time.Time                  { return r.t }
func (r *pgRaw) close(ctx context.Context) error { return r.tx.Rollback(ctx) }

func (r *pgRaw) rolesOf(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.tx.Query(ctx, `
		SELECT role_urn FROM x_role_assignments WHERE organization_id = $1 AND user_id = $2
		UNION
		SELECT m.role_urn FROM x_directory_memberships d
		JOIN x_directory_role_mappings m ON m.organization_id = d.organization_id AND m.group_id = d.group_id
		WHERE d.organization_id = $1 AND d.user_id = $2`, r.org, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (r *pgRaw) membersOf(ctx context.Context, roles []string) (map[string][]string, error) {
	rows, err := r.tx.Query(ctx, `
		SELECT role_urn, user_id FROM x_role_assignments WHERE organization_id = $1 AND role_urn = ANY($2::text[])
		UNION
		SELECT m.role_urn, d.user_id FROM x_directory_memberships d
		JOIN x_directory_role_mappings m ON m.organization_id = d.organization_id AND m.group_id = d.group_id
		WHERE d.organization_id = $1 AND m.role_urn = ANY($2::text[])`, r.org, roles)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var role, user string
		if err := rows.Scan(&role, &user); err != nil {
			return nil, err
		}
		out[role] = append(out[role], user)
	}
	return out, rows.Err()
}

func (r *pgRaw) grants(ctx context.Context, where string, arg []string) ([]xrbac.StoredGrant, error) {
	rows, err := r.tx.Query(ctx, `SELECT id, principal_urn, scope, selectors, valid_from, valid_until
		FROM x_principal_grants WHERE organization_id = $1 AND `+where+` = ANY($2::text[])`, r.org, arg)
	if err != nil {
		return nil, err
	}
	return scanStored(rows)
}

func scanStored(rows pgx.Rows) ([]xrbac.StoredGrant, error) {
	defer rows.Close()
	var out []xrbac.StoredGrant
	for rows.Next() {
		var g xrbac.StoredGrant
		var scope string
		var sel []byte
		if err := rows.Scan(&g.ID, &g.Principal, &scope, &sel, &g.ValidFrom, &g.ValidUntil); err != nil {
			return nil, err
		}
		g.Scope = xrbac.Scope(scope)
		if err := json.Unmarshal(sel, &g.Selectors); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *pgRaw) userGrants(ctx context.Context, userID string) ([]xrbac.StoredGrant, error) {
	rows, err := r.tx.Query(ctx, `
		WITH principals AS (
		  SELECT 'user:' || $2::text AS urn UNION SELECT 'user:all'
		  UNION SELECT role_urn FROM x_role_assignments WHERE organization_id = $1 AND user_id = $2
		  UNION SELECT m.role_urn FROM x_directory_memberships d
		    JOIN x_directory_role_mappings m ON m.organization_id = d.organization_id AND m.group_id = d.group_id
		    WHERE d.organization_id = $1 AND d.user_id = $2
		)
		SELECT g.id, g.principal_urn, g.scope, g.selectors, g.valid_from, g.valid_until
		FROM x_principal_grants g JOIN principals p ON p.urn = g.principal_urn
		WHERE g.organization_id = $1`, r.org, userID)
	if err != nil {
		return nil, err
	}
	return scanStored(rows)
}

func (r *pgRaw) grantsFor(ctx context.Context, principals []string) ([]xrbac.StoredGrant, error) {
	return r.grants(ctx, "principal_urn", principals)
}

func (r *pgRaw) grantsWithScope(ctx context.Context, scopes []string) ([]xrbac.StoredGrant, error) {
	return r.grants(ctx, "scope", scopes)
}

func (r *pgRaw) apiKey(ctx context.Context, id string) (authz.APIKeyRow, []xrbac.StoredGrant, error) {
	k := authz.APIKeyRow{ID: id}
	var pol []byte
	err := r.tx.QueryRow(ctx, `SELECT agent_id, revoked_at, expires_at, delegated_grants FROM x_api_keys WHERE id = $1 AND organization_id = $2`, id, r.org).
		Scan(&k.AgentID, &k.RevokedAt, &k.ExpiresAt, &pol)
	if err != nil {
		return k, nil, err
	}
	var policy []xrbac.StoredGrant
	if err := json.Unmarshal(pol, &policy); err != nil {
		return k, nil, err
	}
	for i := range policy {
		policy[i].ID = id + "-" + string(rune('a'+i))
		policy[i].Principal = "api_key:" + id
	}
	return k, policy, nil
}

func (r *pgRaw) agent(ctx context.Context, id string) (authz.AgentRow, error) {
	a := authz.AgentRow{ID: id}
	err := r.tx.QueryRow(ctx, `SELECT project_id, owner_user_id, status FROM x_agents WHERE id = $1 AND organization_id = $2`, id, r.org).
		Scan(&a.ProjectID, &a.OwnerUserID, &a.Status)
	return a, err
}
