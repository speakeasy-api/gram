package spike_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/server"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/fga"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/fixtures"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/memstore"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/model"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/objects"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/pgstore"
)

var (
	alice = authz.Credential{Org: fixtures.Org, UserID: "alice"}
	bob   = authz.Credential{Org: fixtures.Org, UserID: "bob"}
	dave  = authz.Credential{Org: fixtures.Org, UserID: "dave"}
	erin  = authz.Credential{Org: fixtures.Org, UserID: "erin"}
	frank = authz.Credential{Org: fixtures.Org, UserID: "frank"}
	agent = authz.Credential{Org: fixtures.Org, KeyID: fixtures.KeyID}
)

// fgaUsers is the OpenFGA equivalent of a credential's layers.
func fgaUsers(c authz.Credential) []string {
	if c.KeyID != "" {
		return []string{"api_key:" + fixtures.KeyID, "agent:" + fixtures.AgentID, "user:alice"}
	}
	return []string{"user:" + c.UserID}
}

type scenario struct {
	name    string
	cred    authz.Credential
	perm    string
	obj     authz.Object
	tool    model.MCPToolContext
	advance time.Duration
	want    bool
}

func chat(id string) objects.Chat {
	for _, c := range fixtures.Chats {
		if c.ID == id {
			return fixtures.ChatObj(c)
		}
	}
	panic(id)
}

var update = model.MCPToolContext{Tool: "update_issue", Disposition: "idempotent"}
var del = model.MCPToolContext{Tool: "delete_issue", Disposition: "destructive"}

var scenarios = []scenario{
	{"alice connects: update_issue", alice, "connect", fixtures.ServerObj, update, 0, true},
	{"alice connects: delete_issue is blocked for Engineering", alice, "connect", fixtures.ServerObj, del, 0, false},
	{"dave edits the server", dave, "edit", fixtures.ServerObj, model.MCPToolContext{}, 0, true},
	{"dave cannot connect: management is not use", dave, "connect", fixtures.ServerObj, update, 0, false},
	{"bob reads the server through the member role", bob, "read", fixtures.ServerObj, model.MCPToolContext{}, 0, true},
	{"agent via key: update_issue passes all three layers", agent, "connect", fixtures.ServerObj, update, 0, true},
	{"agent via key: delete_issue fails its own policy", agent, "connect", fixtures.ServerObj, del, 0, false},
	{"agent via key: list_issues", agent, "connect", fixtures.ServerObj, model.MCPToolContext{Tool: "list_issues", Disposition: "read_only"}, 0, true},
	{"alice reads bob's chat as team reviewer", alice, "read", chat("chat_bob"), model.MCPToolContext{}, 0, true},
	{"alice cannot read carol's chat: protected", alice, "read", chat("chat_carol"), model.MCPToolContext{}, 0, false},
	{"alice cannot read dave's chat: not her team", alice, "read", chat("chat_dave"), model.MCPToolContext{}, 0, false},
	{"alice reads her agent's chat as owner", alice, "read", chat("chat_agent"), model.MCPToolContext{}, 0, true},
	{"bob reads his own chat", bob, "read", chat("chat_bob"), model.MCPToolContext{}, 0, true},
	{"bob cannot read alice's chat", bob, "read", chat("chat_alice"), model.MCPToolContext{}, 0, false},
	{"erin reads carol's chat with temporary access", erin, "read", chat("chat_carol"), model.MCPToolContext{}, 0, true},
	{"erin's access has expired 31 minutes later", erin, "read", chat("chat_carol"), model.MCPToolContext{}, 31 * time.Minute, false},
	{"frank (org-wide reviewer) reads bob's chat", frank, "read", chat("chat_bob"), model.MCPToolContext{}, 0, true},
	{"frank cannot read carol's chat: protected beats org-wide review", frank, "read", chat("chat_carol"), model.MCPToolContext{}, 0, false},
	{"bob can create agents (self-service via member role)", bob, "create_agent", fixtures.OrgObj, model.MCPToolContext{}, 0, true},
	{"dave can create agents too", dave, "create_agent", fixtures.OrgObj, model.MCPToolContext{}, 0, true},
}

func newMemEngine() (*authz.Engine, *memstore.Store) {
	st := memstore.New(fixtures.Relationships(), fixtures.Agents, fixtures.APIKeys)
	return authz.New(model.Schema, st, authz.NewCache(1024)), st
}

func pgEngine(t testing.TB, cache bool) (*authz.Engine, *pgstore.Store) {
	t.Helper()
	url := os.Getenv("SPIKE_DATABASE_URL")
	if url == "" {
		t.Skip("SPIKE_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	st := &pgstore.Store{Pool: pool}
	must(t, st.Reset(ctx))
	must(t, st.Seed(ctx, fixtures.Org, fixtures.Relationships(), fixtures.Agents, fixtures.APIKeys))
	var chats []struct{ ID, UserID, AgentID string }
	for _, c := range fixtures.Chats {
		chats = append(chats, struct{ ID, UserID, AgentID string }{c.ID, c.AuthorUserID, c.AgentID})
	}
	must(t, st.SeedChats(ctx, fixtures.Org, fixtures.Project, chats))
	var c *authz.Cache
	if cache {
		c = authz.NewCache(1024)
	}
	return authz.New(model.Schema, st, c), st
}

func fgaMemory(t testing.TB) *fga.Backend {
	t.Helper()
	b, err := fga.NewMemory(context.Background())
	must(t, err)
	t.Cleanup(b.Close)
	must(t, b.Load(context.Background(), fixtures.Relationships()))
	return b
}

func fgaPostgres(t testing.TB) *fga.Backend { return fgaPostgresWith(t) }

func fgaPostgresCached(t testing.TB) *fga.Backend {
	return fgaPostgresWith(t, fga.CachedOptions(10*time.Second)...)
}

func fgaPostgresWith(t testing.TB, opts ...server.OpenFGAServiceV1Option) *fga.Backend {
	t.Helper()
	url := os.Getenv("SPIKE_FGA_DATABASE_URL")
	if url == "" {
		t.Skip("SPIKE_FGA_DATABASE_URL not set")
	}
	// Each backend gets a fresh store, so earlier tests' tuples never leak in.
	b, err := fga.NewPostgres(context.Background(), url, opts...)
	must(t, err)
	t.Cleanup(b.Close)
	must(t, b.Load(context.Background(), fixtures.Relationships()))
	return b
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func oursDecide(t testing.TB, e *authz.Engine, s scenario) authz.Decision {
	t.Helper()
	ctx := context.Background()
	p, err := e.Admit(ctx, s.cred)
	must(t, err)
	p.Advance(s.advance)
	ref := s.obj.AuthzRef()
	return e.Check(authz.WithPrincipal(ctx, p), authz.Request{Type: ref.Type, Perm: s.perm, Object: ref, Context: s.tool})
}

func fgaDecide(t testing.TB, b *fga.Backend, s scenario) bool {
	t.Helper()
	ref := s.obj.AuthzRef()
	ok, err := b.CheckLayers(context.Background(), fgaUsers(s.cred), s.perm, ref.Type+":"+ref.ID,
		fga.Facts{Tool: s.tool.Tool, Disposition: s.tool.Disposition, Now: time.Now().Add(s.advance)})
	must(t, err)
	return ok
}

// TestScenarios runs every scenario on every available backend. Our engine
// and OpenFGA must agree with each other and with the expectation: OpenFGA
// acts as the differential oracle the design proposes for CI.
func TestScenarios(t *testing.T) {
	mem, _ := newMemEngine()
	fm := fgaMemory(t)
	backends := map[string]func(scenario) bool{
		"ours/memstore":  func(s scenario) bool { return oursDecide(t, mem, s).Allowed() },
		"openfga/memory": func(s scenario) bool { return fgaDecide(t, fm, s) },
	}
	if os.Getenv("SPIKE_DATABASE_URL") != "" {
		pg, _ := pgEngine(t, true)
		backends["ours/postgres"] = func(s scenario) bool { return oursDecide(t, pg, s).Allowed() }
	}
	if os.Getenv("SPIKE_FGA_DATABASE_URL") != "" {
		b := fgaPostgres(t)
		backends["openfga/postgres"] = func(s scenario) bool { return fgaDecide(t, b, s) }
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			for name, decide := range backends {
				if got := decide(s); got != s.want {
					t.Errorf("%s: got %v, want %v", name, got, s.want)
				}
			}
		})
	}
}

// TestDecisionExplains shows which layer refused and why.
func TestDecisionExplains(t *testing.T) {
	e, _ := newMemEngine()
	d := oursDecide(t, e, scenario{cred: agent, perm: "connect", obj: fixtures.ServerObj, tool: del})
	if d.Outcome != authz.Denied || d.Layer != authz.LayerSubject {
		t.Fatalf("want denied at subject layer, got %+v", d)
	}
	t.Logf("delete_issue: %s at %s layer: %s", d.Outcome, d.Layer, d.Reason)

	ctx := context.Background()
	p, _ := e.Admit(ctx, alice)
	ok := authz.Explain(authz.WithPrincipal(ctx, p), e, permChatRead, chat("chat_bob"))
	t.Logf("why can alice read chat_bob? %v", ok.Path)
	if !ok.Allowed() || len(ok.Path) == 0 || !strings.Contains(ok.Path[0], "user.activity_reviewer") {
		t.Fatalf("expected an explain path through the team-review grant, got %+v", ok)
	}
}

var permChatRead = authz.Permission[objects.Chat]{Type: "chat", Name: "read"}

// TestMissingFactIsUndetermined: a conditional grant cannot allow when the
// handler forgot to supply the tool, and Eligible answers the
// before-the-tool-is-known question explicitly.
func TestMissingFactIsUndetermined(t *testing.T) {
	e, _ := newMemEngine()
	ctx := context.Background()
	p, _ := e.Admit(ctx, agent)
	ctx = authz.WithPrincipal(ctx, p)
	ref := fixtures.ServerObj.AuthzRef()
	d := e.Check(ctx, authz.Request{Type: "mcp_server", Perm: "connect", Object: ref})
	if d.Outcome != authz.Undetermined {
		t.Fatalf("want undetermined without tool facts, got %v", d.Outcome)
	}
	if !authz.Eligible(ctx, e, authz.Permission[objects.MCPServer]{Type: "mcp_server", Name: "connect"}, fixtures.ServerObj) {
		t.Fatal("agent should be eligible to connect before choosing a tool")
	}
}

// TestRevocation: removing Alice from Engineering cuts off her agent on the
// next request, with no policy edit and no cache invalidation.
func TestRevocation(t *testing.T) {
	ctx := context.Background()
	s := scenario{cred: agent, perm: "connect", obj: fixtures.ServerObj, tool: update}

	t.Run("ours/memstore", func(t *testing.T) {
		e, st := newMemEngine()
		if !oursDecide(t, e, s).Allowed() {
			t.Fatal("precondition")
		}
		st.Delete(fixtures.RowID(5)) // group:grp_eng#member@user:alice
		d := oursDecide(t, e, s)
		if d.Allowed() || d.Layer != authz.LayerOwner {
			t.Fatalf("want denied at owner layer, got %+v", d)
		}
	})
	t.Run("ours/postgres", func(t *testing.T) {
		e, st := pgEngine(t, true)
		if !oursDecide(t, e, s).Allowed() {
			t.Fatal("precondition")
		}
		loads := e.Stats.SnapshotLoads.Load()
		oursDecide(t, e, s)
		t.Logf("second request: %d snapshot loads (all cache hits)", e.Stats.SnapshotLoads.Load()-loads)
		rev, _ := st.Revision(ctx, fixtures.Org)
		must(t, st.Delete(ctx, fixtures.RowID(5)))
		rev2, _ := st.Revision(ctx, fixtures.Org)
		t.Logf("revision %d → %d after the delete (trigger)", rev, rev2)
		if d := oursDecide(t, e, s); d.Allowed() || d.Layer != authz.LayerOwner {
			t.Fatalf("want denied at owner layer, got %+v", d)
		}
	})
	t.Run("openfga/memory", func(t *testing.T) {
		b := fgaMemory(t)
		if !fgaDecide(t, b, s) {
			t.Fatal("precondition")
		}
		// Deleting only the grant-side tuple is not enough here: our model also
		// needed the reverse pointer user→group, so both must go. In production
		// these are a dual write next to the WorkOS sync, outside its transaction.
		must(t, b.Delete(ctx,
			&openfgav1.TupleKeyWithoutCondition{Object: "group:grp_eng", Relation: "member", User: "user:alice"},
			&openfgav1.TupleKeyWithoutCondition{Object: "user:alice", Relation: "member_group", User: "group:grp_eng"}))
		if fgaDecide(t, b, s) {
			t.Fatal("want denied")
		}
	})
	t.Run("openfga/postgres+cache10s", func(t *testing.T) {
		b := fgaPostgresCached(t)
		if !fgaDecide(t, b, s) {
			t.Fatal("precondition")
		}
		must(t, b.Delete(ctx,
			&openfgav1.TupleKeyWithoutCondition{Object: "group:grp_eng", Relation: "member", User: "user:alice"},
			&openfgav1.TupleKeyWithoutCondition{Object: "user:alice", Relation: "member_group", User: "group:grp_eng"}))
		// Not a failure of OpenFGA: this is the documented trade-off of its
		// caches. Recorded so the comparison shows what the speedup costs.
		t.Logf("immediately after revocation, cached OpenFGA still allows: %v", fgaDecide(t, b, s))
	})
}

// TestLookupAgreesWithCheck: the list predicate matches exactly the chats
// Check allows, pushes down into SQL, and matches OpenFGA ListObjects.
func TestLookupAgreesWithCheck(t *testing.T) {
	ctx := context.Background()
	mem, _ := newMemEngine()
	pg, pgSt := (*authz.Engine)(nil), (*pgstore.Store)(nil)
	if os.Getenv("SPIKE_DATABASE_URL") != "" {
		pg, pgSt = pgEngine(t, false)
	}
	b := fgaMemory(t)
	cols := map[string]string{"id": "c.id", "project": "c.project_id", "author": "c.user_id", "agent": "c.agent_id", "agent.owner": "a.owner_user_id"}

	for _, cred := range []authz.Credential{alice, bob, dave, erin, frank} {
		t.Run(cred.UserID, func(t *testing.T) {
			p, err := mem.Admit(ctx, cred)
			must(t, err)
			pctx := authz.WithPrincipal(ctx, p)
			f, err := authz.List(pctx, mem, permChatRead)
			must(t, err)

			var viaCheck, viaFilter []string
			for _, c := range fixtures.Chats {
				o := fixtures.ChatObj(c)
				if authz.Can(pctx, mem, permChatRead, o) {
					viaCheck = append(viaCheck, c.ID)
				}
				if f.Matches(o.AuthzRef()) {
					viaFilter = append(viaFilter, c.ID)
				}
			}
			if !slices.Equal(viaCheck, viaFilter) {
				t.Fatalf("check %v != filter %v", viaCheck, viaFilter)
			}

			fgaIDs, err := b.ListObjects(ctx, "user:"+cred.UserID, "read", "chat", fga.Facts{})
			must(t, err)
			for i := range fgaIDs {
				fgaIDs[i] = strings.TrimPrefix(fgaIDs[i], "chat:")
			}
			slices.Sort(fgaIDs)
			sortedCheck := slices.Sorted(slices.Values(viaCheck))
			if !slices.Equal(sortedCheck, fgaIDs) {
				t.Fatalf("ours %v != openfga ListObjects %v", sortedCheck, fgaIDs)
			}

			if pg == nil {
				t.Logf("%s sees %v", cred.UserID, viaCheck)
				return
			}
			var args []any
			where, err := f.SQL(cols, &args)
			must(t, err)
			rows, err := pgSt.Pool.Query(ctx, "SELECT c.id FROM spike_chats c LEFT JOIN spike_agents a ON a.id = c.agent_id WHERE "+where+" ORDER BY c.id", args...)
			must(t, err)
			var viaSQL []string
			for rows.Next() {
				var id string
				must(t, rows.Scan(&id))
				viaSQL = append(viaSQL, id)
			}
			rows.Close()
			if !slices.Equal(sortedCheck, viaSQL) {
				t.Fatalf("check %v != sql %v", sortedCheck, viaSQL)
			}
			t.Logf("%s sees %v\n  WHERE %s\n  args %v", cred.UserID, viaSQL, where, args)
		})
	}
}

// TestDelegation: Alice may give her agent tools she holds; she may not give
// it a server she cannot reach, and Bob cannot widen Alice's agent past Alice.
func TestDelegation(t *testing.T) {
	ctx := context.Background()
	e, _ := newMemEngine()
	server := fixtures.ServerObj.AuthzRef()
	project := objects.Project{ID: fixtures.Project, OrganizationID: fixtures.Org}.AuthzRef()
	narrow := authz.Proposal{ForType: "mcp_server", Grant: "connector", Anchor: server, Params: []byte(`{"tools":["list_issues","get_issue","update_issue"]}`)}

	cases := []struct {
		name  string
		cred  authz.Credential
		owner string
		prop  authz.Proposal
		ok    bool
	}{
		{"alice delegates three tools on srv_linear (row 16)", alice, "alice", narrow, true},
		{"alice cannot delegate project-wide connect", alice, "alice", authz.Proposal{ForType: "mcp_server", Grant: "connector", Anchor: project}, false},
		{"dave cannot delegate connect he does not hold", dave, "dave", narrow, false},
		{"edit is not delegable at all", alice, "alice", authz.Proposal{ForType: "mcp_server", Grant: "editor", Anchor: server}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := e.Admit(ctx, c.cred)
			must(t, err)
			err = e.CanGrant(authz.WithPrincipal(ctx, p), c.owner, []authz.Proposal{c.prop})
			if (err == nil) != c.ok {
				t.Fatalf("got %v, want ok=%v", err, c.ok)
			}
			if err != nil {
				t.Log(err)
			}
		})
	}
}

// TestTranslationCost reports how many tuples OpenFGA needs for the same data.
func TestTranslationCost(t *testing.T) {
	b := fgaMemory(t)
	t.Logf("our rows: %d; OpenFGA tuples: %d mirrored + %d derived from domain rows (parents, owner, author, reverse group pointers, org membership)",
		len(fixtures.Relationships()), b.GrantTuples, b.DerivedTuples)

	ends := time.Now().Add(time.Hour)
	_, err := fga.TupleFor(authz.Relationship{
		Object: authz.ObjectID{Type: "mcp_server", ID: fixtures.Server}, Relation: "connector", Subject: authz.User("frank"),
		ConditionName: "mcp_tools", ConditionParams: []byte(`{"tools":["get_issue"]}`), ExpiresAt: &ends,
	})
	if err == nil {
		t.Fatal("expected the one-condition-per-tuple limitation")
	}
	t.Logf("temporary + tool-limited grant: %v", err)
}
