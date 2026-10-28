package spike_test

import (
	"context"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/fixtures"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/hmodel"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/hybrid"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/objects"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/xrbac"
)

// Option D: Option A's schema and engine over Option B's tables.

func dMapping(t testing.TB) *hybrid.Mapping {
	m, err := hybrid.NewMapping(hmodel.Schema)
	must(t, err)
	return m
}

func dMem(t testing.TB) (*authz.Engine, *hybrid.MemStore) {
	st := &hybrid.MemStore{W: fixtures.DWorld(), M: dMapping(t)}
	return authz.New(hmodel.Schema, st, nil), st
}

func dPg(t testing.TB) (*authz.Engine, *pgxpool.Pool, *xrbac.PgSource) {
	t.Helper()
	url := os.Getenv("SPIKE_DATABASE_URL")
	if url == "" {
		t.Skip("SPIKE_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	must(t, err)
	t.Cleanup(pool.Close)
	src := &xrbac.PgSource{Pool: pool} // the very same x_ tables Option B uses
	must(t, src.Reset(ctx))
	must(t, src.Seed(ctx, fixtures.Org, fixtures.DWorld()))
	return authz.New(hmodel.Schema, &hybrid.PgStore{Pool: pool, M: dMapping(t)}, nil), pool, src
}

func TestHybridScenarios(t *testing.T) {
	mem, _ := dMem(t)
	backends := map[string]*authz.Engine{"hybrid/memory": mem}
	if os.Getenv("SPIKE_DATABASE_URL") != "" {
		pg, _, _ := dPg(t)
		backends["hybrid/postgres"] = pg
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			for name, e := range backends {
				d := oursDecide(t, e, s)
				if d.Allowed() != s.want {
					t.Errorf("%s: got %s (layer %s: %s), want %v", name, d.Outcome, d.Layer, d.Reason, s.want)
				}
			}
		})
	}
}

// Every stored row decodes to a relationship and encodes back to the same
// row: the mapping is reversible, so write paths keep today's shape.
func TestHybridRoundTrip(t *testing.T) {
	m := dMapping(t)
	for _, g := range fixtures.DWorld().Grants {
		r, err := m.Decode(fixtures.Org, g)
		must(t, err)
		back, err := m.Encode(fixtures.Org, r)
		must(t, err)
		if back.Principal != g.Principal || back.Scope != g.Scope || !maps.Equal(back.Selectors, g.Selectors) || back.ValidUntil != g.ValidUntil {
			t.Fatalf("round trip changed %+v\n  → %s\n  → %+v", g, r, back)
		}
		t.Logf("%-18s %-20s %v\n      ⇄ %s", g.Principal, g.Scope, g.Selectors, r)
	}
}

func TestHybridRejectsUnmappableRows(t *testing.T) {
	m := dMapping(t)
	for _, g := range []xrbac.StoredGrant{
		{ID: "bad1", Principal: "user:x", Scope: "mcp:connect", Selectors: map[string]string{"resource_kind": "mcp", "resource_id": "*", "region": "eu"}},
		{ID: "bad2", Principal: "user:x", Scope: "mcp:write", Selectors: map[string]string{"resource_kind": "mcp", "resource_id": fixtures.Server, "tool": "x"}},
		{ID: "bad3", Principal: "user:x", Scope: "chat:read", Selectors: map[string]string{"resource_kind": "chat", "resource_id": "chat_bob"}},
		{ID: "bad4", Principal: "user:x", Scope: "risk_policy:bypass", Selectors: map[string]string{"resource_kind": "risk_policy", "resource_id": "*"}},
	} {
		if _, err := m.Decode(fixtures.Org, g); err == nil {
			t.Fatalf("%s should not decode", g.ID)
		} else {
			t.Log(err)
		}
	}
}

func TestHybridLookup(t *testing.T) {
	ctx := context.Background()
	d, _ := dMem(t)
	a, _ := newMemEngine()
	var pool *pgxpool.Pool
	if os.Getenv("SPIKE_DATABASE_URL") != "" {
		_, pool, _ = dPg(t)
	}
	cols := map[string]string{"id": "c.id", "project": "c.project_id", "author": "c.user_id", "agent": "c.agent_id", "agent.owner": "a.owner_user_id"}
	for _, cred := range []authz.Credential{alice, bob, dave, erin, frank} {
		t.Run(cred.UserID, func(t *testing.T) {
			pd, err := d.Admit(ctx, cred)
			must(t, err)
			pa, err := a.Admit(ctx, cred)
			must(t, err)
			dctx, actx := authz.WithPrincipal(ctx, pd), authz.WithPrincipal(ctx, pa)
			f, err := authz.List(dctx, d, permChatRead)
			must(t, err)
			var viaCheck, viaFilter, optionA []string
			for _, c := range fixtures.Chats {
				o := fixtures.ChatObj(c)
				if authz.Can(dctx, d, permChatRead, o) {
					viaCheck = append(viaCheck, c.ID)
				}
				if f.Matches(o.AuthzRef()) {
					viaFilter = append(viaFilter, c.ID)
				}
				if authz.Can(actx, a, permChatRead, o) {
					optionA = append(optionA, c.ID)
				}
			}
			if !slices.Equal(viaCheck, viaFilter) || !slices.Equal(viaCheck, optionA) {
				t.Fatalf("check %v, filter %v, option A %v", viaCheck, viaFilter, optionA)
			}
			if pool == nil {
				return
			}
			var args []any
			where, err := f.SQL(cols, &args)
			must(t, err)
			rows, err := pool.Query(ctx, "SELECT c.id FROM x_chats c LEFT JOIN x_agents a ON a.id = c.agent_id WHERE "+where+" ORDER BY c.id", args...)
			must(t, err)
			var viaSQL []string
			for rows.Next() {
				var id string
				must(t, rows.Scan(&id))
				viaSQL = append(viaSQL, id)
			}
			rows.Close()
			if !slices.Equal(slices.Sorted(slices.Values(viaCheck)), viaSQL) {
				t.Fatalf("check %v != sql %v", viaCheck, viaSQL)
			}
			t.Logf("%s sees %v\n  WHERE %s\n  args %v", cred.UserID, viaSQL, where, args)
		})
	}
}

func TestHybridRevocation(t *testing.T) {
	s := scenario{cred: agent, perm: "connect", obj: fixtures.ServerObj, tool: update}
	t.Run("memory", func(t *testing.T) {
		e, st := dMem(t)
		if !oursDecide(t, e, s).Allowed() {
			t.Fatal("precondition")
		}
		st.RemoveMembership("alice", "grp_eng")
		if d := oursDecide(t, e, s); d.Allowed() || d.Layer != authz.LayerOwner {
			t.Fatalf("want denied at owner layer, got %+v", d)
		}
	})
	t.Run("postgres", func(t *testing.T) {
		e, _, src := dPg(t)
		if !oursDecide(t, e, s).Allowed() {
			t.Fatal("precondition")
		}
		must(t, src.RemoveMembership(context.Background(), fixtures.Org, "alice", "grp_eng")) // one domain write
		if d := oursDecide(t, e, s); d.Allowed() || d.Layer != authz.LayerOwner {
			t.Fatalf("want denied at owner layer, got %+v", d)
		}
	})
}

// Delegation uses A's containment over B's rows, then Encode turns the
// accepted relationship into the principal_grants row to insert.
func TestHybridDelegationWritesTodaysRowShape(t *testing.T) {
	ctx := context.Background()
	e, _ := dMem(t)
	p, err := e.Admit(ctx, alice)
	must(t, err)
	prop := authz.Proposal{ForType: "mcp_server", Grant: "connector", Anchor: fixtures.ServerObj.AuthzRef(), Params: []byte(`{"tools":["get_issue"]}`)}
	must(t, e.CanGrant(authz.WithPrincipal(ctx, p), "alice", []authz.Proposal{prop}))
	row, err := dMapping(t).Encode(fixtures.Org, authz.Relationship{
		Object: authz.ObjectID{Type: "mcp_server", ID: fixtures.Server}, Relation: "connector", Subject: authz.Agent(fixtures.AgentID),
		ConditionName: "mcp_tools", ConditionParams: prop.Params, SourceRef: "g-new",
	})
	must(t, err)
	t.Logf("INSERT INTO principal_grants: %s %s %v", row.Principal, row.Scope, row.Selectors)
	if row.Scope != "mcp:connect" || row.Selectors["tool"] != "get_issue" || row.Selectors["resource_id"] != fixtures.Server {
		t.Fatalf("unexpected row %+v", row)
	}

	pd, err := e.Admit(ctx, dave)
	must(t, err)
	if err := e.CanGrant(authz.WithPrincipal(ctx, pd), "dave", []authz.Proposal{prop}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("dave holds no connect: %v", err)
	}
}

var _ = objects.Chat{}
