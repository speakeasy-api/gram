package spike_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/fixtures"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/objects"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/xrbac"
)

// Option B: the Extensible RBAC plan, on the same scenarios.

func xSubject(c authz.Credential) xrbac.Subject {
	if c.KeyID != "" {
		return xrbac.Subject{Org: c.Org, Actor: "agent:" + fixtures.AgentID, Method: xrbac.AuthAgentKey, CredentialID: c.KeyID}
	}
	return xrbac.Subject{Org: c.Org, Actor: "user:" + c.UserID, Method: xrbac.AuthSession}
}

// xRequest translates a shared scenario into Option B's check builders.
func xRequest(s scenario) xrbac.Request {
	switch o := s.obj.(type) {
	case objects.MCPServer:
		switch s.perm {
		case "connect":
			return xrbac.ToolCallCheck(xrbac.ScopeMCPConnect, fixtures.XServerTarget, xrbac.ToolFacts{Name: s.tool.Tool, Classification: s.tool.Disposition})
		case "edit":
			return xrbac.ResourceCheck(xrbac.ScopeMCPWrite, fixtures.XServerTarget)
		case "read":
			return xrbac.ResourceCheck(xrbac.ScopeMCPRead, fixtures.XServerTarget)
		}
	case objects.Chat:
		return xrbac.ResourceCheck(xrbac.ScopeChatRead, fixtures.XChatTarget(o.ID))
	case objects.Organization:
		return xrbac.ResourceCheck(xrbac.ScopeAgentCreate, fixtures.XOrgTarget)
	}
	panic("unmapped scenario " + s.name)
}

func xMem() (*xrbac.Client, *xrbac.StoreBackend, *xrbac.MemSource) {
	src := &xrbac.MemSource{W: fixtures.XWorld()}
	be := &xrbac.StoreBackend{Src: src}
	return xrbac.NewClient(be, 100), be, src
}

func xPg(t testing.TB) (*xrbac.Client, *xrbac.StoreBackend, *xrbac.PgSource) {
	t.Helper()
	url := os.Getenv("SPIKE_DATABASE_URL")
	if url == "" {
		t.Skip("SPIKE_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	must(t, err)
	t.Cleanup(pool.Close)
	src := &xrbac.PgSource{Pool: pool}
	must(t, src.Reset(ctx))
	must(t, src.Seed(ctx, fixtures.Org, fixtures.XWorld()))
	be := &xrbac.StoreBackend{Src: src}
	return xrbac.NewClient(be, 100), be, src
}

func xDecide(t testing.TB, c *xrbac.Client, be *xrbac.StoreBackend, s scenario) xrbac.Decision {
	t.Helper()
	be.Offset = s.advance
	defer func() { be.Offset = 0 }()
	d, err := c.Authorize(context.Background(), xSubject(s.cred), xRequest(s))
	must(t, err)
	return d
}

func TestExtensibleScenarios(t *testing.T) {
	mc, mb, _ := xMem()
	backends := map[string]func(scenario) xrbac.Decision{
		"extensible/memory": func(s scenario) xrbac.Decision { return xDecide(t, mc, mb, s) },
	}
	if os.Getenv("SPIKE_DATABASE_URL") != "" {
		pc, pb, _ := xPg(t)
		backends["extensible/postgres"] = func(s scenario) xrbac.Decision { return xDecide(t, pc, pb, s) }
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			for name, decide := range backends {
				d := decide(s)
				if got := d.Outcome == xrbac.Allowed; got != s.want {
					t.Errorf("%s: got %v (%s, limit %s, by %s), want %v", name, got, d.Reason, d.Limit, d.Source, s.want)
				}
			}
		})
	}
}

func TestExtensibleDenyWinsAndProvenance(t *testing.T) {
	c, be, _ := xMem()
	d := xDecide(t, c, be, scenario{cred: agent, perm: "connect", obj: fixtures.ServerObj, tool: del})
	if d.Outcome != xrbac.Denied || d.Limit != xrbac.LimitAgent || d.Reason != xrbac.ReasonDenied {
		t.Fatalf("want the agent's carried denial to decide, got %+v", d)
	}
	t.Logf("delete_issue: %s by %s at %s limit (%s)", d.Outcome, d.Source, d.Limit, d.Reason)
	d = xDecide(t, c, be, scenario{cred: frank, perm: "read", obj: chat("chat_carol")})
	if d.Reason != xrbac.ReasonDenied || d.Source != "user:frank" {
		t.Fatalf("want frank's reviewer-side block, got %+v", d)
	}
	ex, err := c.Explain(context.Background(), xSubject(alice), xrbac.ResourceCheck(xrbac.ScopeChatRead, fixtures.XChatTarget("chat_agent")))
	must(t, err)
	t.Logf("why can alice read chat_agent? %v", ex.Detail)
}

func TestExtensibleUnknownClassificationIsUnresolved(t *testing.T) {
	c, _, _ := xMem()
	d, err := c.Authorize(context.Background(), xSubject(alice), xrbac.ToolCallCheck(xrbac.ScopeMCPConnect, fixtures.XServerTarget,
		xrbac.ToolFacts{Name: "update_issue", Classification: xrbac.ClassificationUnknown}))
	must(t, err)
	if d.Outcome != xrbac.Unresolved {
		t.Fatalf("an unknown classification cannot prove the destructive block does not apply: %+v", d)
	}
	_, err = c.Authorize(context.Background(), xSubject(alice), xrbac.ResourceCheck(xrbac.ScopeMCPConnect, xrbac.Target{Resource: fixtures.XRef("mcp", fixtures.Server)}))
	if !errors.Is(err, xrbac.ErrInvalidRequest) {
		t.Fatalf("a target without its ancestors must be rejected by the client, got %v", err)
	}
}

func TestExtensibleDiscoverAccess(t *testing.T) {
	ctx := context.Background()
	c, _, src := xMem()
	q := xrbac.AccessQuery{Scope: xrbac.ScopeMCPConnect, Target: fixtures.XServerTarget}
	d, err := c.DiscoverAccess(ctx, xSubject(agent), q)
	must(t, err)
	if d.Outcome != xrbac.Allowed {
		t.Fatalf("agent should be able to call some tool: %+v", d)
	}
	// Disjoint limits: the credential now allows only delete_issue, the agent only its three tools.
	cred := src.W.Credentials[fixtures.KeyID]
	cred.Policy = []xrbac.Rule{{Scope: xrbac.ScopeMCPConnect, Selector: map[string]string{"resource_kind": "mcp", "resource_id": fixtures.Server, "tool": "delete_issue"}}}
	src.W.Credentials[fixtures.KeyID] = cred
	d, err = c.DiscoverAccess(ctx, xSubject(agent), q)
	must(t, err)
	if d.Outcome != xrbac.Denied {
		t.Fatalf("no tool passes both credential and agent limits: %+v", d)
	}
}

func TestExtensibleRevocation(t *testing.T) {
	s := scenario{cred: agent, perm: "connect", obj: fixtures.ServerObj, tool: update}
	t.Run("memory", func(t *testing.T) {
		c, be, src := xMem()
		if xDecide(t, c, be, s).Outcome != xrbac.Allowed {
			t.Fatal("precondition")
		}
		src.RemoveMembership("alice", "grp_eng")
		if d := xDecide(t, c, be, s); d.Outcome == xrbac.Allowed || d.Limit != xrbac.LimitOwner {
			t.Fatalf("want denied at owner limit, got %+v", d)
		}
	})
	t.Run("postgres", func(t *testing.T) {
		c, be, src := xPg(t)
		if xDecide(t, c, be, s).Outcome != xrbac.Allowed {
			t.Fatal("precondition")
		}
		// One write to the domain table; nothing else to update or invalidate.
		must(t, src.RemoveMembership(context.Background(), fixtures.Org, "alice", "grp_eng"))
		if d := xDecide(t, c, be, s); d.Outcome == xrbac.Allowed || d.Limit != xrbac.LimitOwner {
			t.Fatalf("want denied at owner limit, got %+v", d)
		}
	})
}

// Option B lists by Filter over already-loaded candidates (PlanQuery is
// deferred). The set matches, but only after loading every candidate row.
func TestExtensibleFilterAgreesWithCheck(t *testing.T) {
	c, _, _ := xMem()
	a, _ := newMemEngine()
	ctx := context.Background()
	for _, cred := range []authz.Credential{alice, bob, dave, erin, frank} {
		p, err := a.Admit(ctx, cred)
		must(t, err)
		var optionA []string
		for _, ch := range fixtures.Chats {
			if authz.Can(authz.WithPrincipal(ctx, p), a, permChatRead, fixtures.ChatObj(ch)) {
				optionA = append(optionA, ch.ID)
			}
		}
		var reqs []xrbac.Request
		var want []string
		for _, ch := range fixtures.Chats {
			r := xrbac.ResourceCheck(xrbac.ScopeChatRead, fixtures.XChatTarget(ch.ID))
			reqs = append(reqs, r)
			if d, _ := c.Authorize(ctx, xSubject(cred), r); d.Outcome == xrbac.Allowed {
				want = append(want, ch.ID)
			}
		}
		got, err := c.Filter(ctx, xSubject(cred), reqs)
		must(t, err)
		var ids []string
		for _, tg := range got {
			ids = append(ids, tg.Resource.ID)
		}
		if !slices.Equal(ids, want) {
			t.Fatalf("%s: filter %v != checks %v", cred.UserID, ids, want)
		}
		if !slices.Equal(ids, optionA) {
			t.Fatalf("%s: option B %v != option A %v", cred.UserID, ids, optionA)
		}
		t.Logf("%s sees %v", cred.UserID, ids)
	}
}

func TestExtensibleDelegation(t *testing.T) {
	ctx := context.Background()
	c, _, _ := xMem()
	conn := func(kv ...string) xrbac.Rule {
		sel := map[string]string{"resource_kind": "mcp", "resource_id": fixtures.Server}
		for i := 0; i < len(kv); i += 2 {
			sel[kv[i]] = kv[i+1]
		}
		return xrbac.Rule{Scope: xrbac.ScopeMCPConnect, Selector: sel}
	}
	carried := conn("disposition", "destructive")
	carried.Effect = xrbac.EffectDeny

	cases := []struct {
		name     string
		cred     authz.Credential
		proposed []xrbac.Rule
		want     xrbac.Reason
	}{
		{"alice: three tools, carrying her destructive denial", alice,
			[]xrbac.Rule{conn("tool", "list_issues"), conn("tool", "get_issue"), conn("tool", "update_issue"), carried}, xrbac.ReasonAllowed},
		{"alice: same tools without the denial is rejected", alice,
			[]xrbac.Rule{conn("tool", "list_issues")}, xrbac.ReasonDenied},
		{"alice: project-wide connect exceeds her grant", alice,
			[]xrbac.Rule{{Scope: xrbac.ScopeMCPConnect, Selector: map[string]string{"resource_kind": "mcp", "resource_id": "*", "project_id": fixtures.Project}}, carried}, xrbac.ReasonExceedsParent},
		{"dave: management does not include connect", dave, []xrbac.Rule{conn("tool", "list_issues")}, xrbac.ReasonExceedsParent},
		{"mcp:write is not agent-runtime eligible", alice,
			[]xrbac.Rule{{Scope: xrbac.ScopeMCPWrite, Selector: map[string]string{"resource_kind": "mcp", "resource_id": fixtures.Server}}}, xrbac.ReasonNotDelegable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := c.CheckDelegation(ctx, xSubject(tc.cred), xrbac.Delegation{Agent: fixtures.XAgentTarget, Proposed: tc.proposed})
			must(t, err)
			if d.Reason != tc.want {
				t.Fatalf("got %+v, want %s", d, tc.want)
			}
		})
	}
}
