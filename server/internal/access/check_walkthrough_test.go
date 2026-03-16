package access

// ===========================================================================
// CHECK ALGORITHM WALKTHROUGH WITH IN-MEMORY DB
//
// This file is a runnable test suite that demonstrates exactly how the check
// algorithm works against realistic data. It builds an in-memory representation
// of the principal_grants table, resolves Grants the same way the middleware
// would, and then runs Can / Filter checks with step-by-step commentary.
//
// Run with: go test -v -run TestWalkthrough ./server/internal/access/
// ===========================================================================

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// In-memory DB: mirrors the principal_grants Postgres table
// ---------------------------------------------------------------------------

// principalGrantRow mirrors one row of the principal_grants table.
//
// CREATE TABLE principal_grants (
//
//	id              UUID PRIMARY KEY,
//	organization_id UUID NOT NULL,
//	principal_type  TEXT NOT NULL CHECK (principal_type IN ('user', 'role')),
//	principal_id    TEXT NOT NULL,  -- user UUID or role slug
//	scope_slug      TEXT NOT NULL,
//	resources       TEXT[],         -- NULL = unrestricted; ARRAY = allowlist
//	UNIQUE (organization_id, principal_type, principal_id, scope_slug)
//
// );
type principalGrantRow struct {
	OrganizationID string
	PrincipalType  string // "user" or "role"
	PrincipalID    string // user UUID or role slug
	ScopeSlug      Scope
	Resources      []string // nil = unrestricted
}

// memDB is the in-memory principal_grants table.
type memDB struct {
	rows []principalGrantRow
}

func newMemDB() *memDB {
	return &memDB{}
}

// insert adds a grant row. Mirrors INSERT INTO principal_grants.
func (db *memDB) insert(orgID, principalType, principalID string, scope Scope, resources []string) {
	db.rows = append(db.rows, principalGrantRow{
		OrganizationID: orgID,
		PrincipalType:  principalType,
		PrincipalID:    principalID,
		ScopeSlug:      scope,
		Resources:      resources,
	})
}

// queryForPrincipal mirrors the SQL query the middleware runs once per request:
//
//	SELECT scope_slug, resources FROM principal_grants
//	WHERE organization_id = $org
//	  AND (
//	    (principal_type = 'role' AND principal_id = $role_slug)
//	    OR
//	    (principal_type = 'user' AND principal_id = $user_id)
//	  )
//
// Returns the matching rows as grantRow values for the Grants object.
func (db *memDB) queryForPrincipal(orgID, roleSlug, userID string) []grantRow {
	var result []grantRow
	for _, row := range db.rows {
		if row.OrganizationID != orgID {
			continue
		}
		roleMatch := row.PrincipalType == "role" && row.PrincipalID == roleSlug
		userMatch := row.PrincipalType == "user" && row.PrincipalID == userID
		if !roleMatch && !userMatch {
			continue
		}
		result = append(result, grantRow{
			Scope:     row.ScopeSlug,
			Resources: row.Resources,
		})
	}
	return result
}

// resolveGrants simulates what the middleware does: query DB, build Grants.
func (db *memDB) resolveGrants(orgID, roleSlug, userID string) *Grants {
	return &Grants{
		orgID:    orgID,
		userID:   userID,
		roleSlug: roleSlug,
		rows:     db.queryForPrincipal(orgID, roleSlug, userID),
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func mustCan(t *testing.T, ctx context.Context, checks ...check) {
	t.Helper()
	if err := Can(ctx, checks...); err != nil {
		t.Fatalf("expected ALLOW, got DENY: %v", err)
	}
}

func mustDeny(t *testing.T, ctx context.Context, checks ...check) {
	t.Helper()
	if err := Can(ctx, checks...); err == nil {
		t.Fatalf("expected DENY, got ALLOW")
	}
}

func mustFilter(t *testing.T, ctx context.Context, scope Scope) []string {
	t.Helper()
	result, err := Filter(ctx, scope)
	if err != nil {
		t.Fatalf("expected filter result, got error: %v", err)
	}
	return result
}

func mustFilterDeny(t *testing.T, ctx context.Context, scope Scope) {
	t.Helper()
	_, err := Filter(ctx, scope)
	if err == nil {
		t.Fatal("expected filter DENY, got allow")
	}
}

func ctxWithGrants(grants *Grants) context.Context {
	return GrantsToContext(context.Background(), grants)
}

// ===========================================================================
// THE DATABASE: a realistic org with multiple users, roles, and resources
// ===========================================================================

func seedTestDB() *memDB {
	db := newMemDB()
	org := "org-acme"

	// -----------------------------------------------------------------
	// DEFAULT ROLE GRANTS (seeded at org creation — all unrestricted)
	// These are the rows every org gets when it's created.
	// -----------------------------------------------------------------

	// admin role: all 7 scopes, unrestricted
	for _, scope := range []Scope{ScopeOrgRead, ScopeOrgAdmin, ScopeBuildRead, ScopeBuildWrite, ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect} {
		db.insert(org, "role", "admin", scope, nil) // nil = unrestricted
	}

	// member role: 4 scopes, unrestricted
	for _, scope := range []Scope{ScopeOrgRead, ScopeBuildRead, ScopeMCPRead, ScopeMCPConnect} {
		db.insert(org, "role", "member", scope, nil)
	}

	// -----------------------------------------------------------------
	// USER-LEVEL GRANTS (set by admins for specific users)
	// -----------------------------------------------------------------

	// Bob the contractor: can ONLY connect to mcp-payments. No role grants.
	db.insert(org, "user", "bob", ScopeMCPConnect, []string{"mcp-payments"})

	// Grace the contractor: build:read on proj-xyz only.
	db.insert(org, "user", "grace", ScopeBuildRead, []string{"proj-xyz"})

	// Jack: build:read on proj-a and proj-b only.
	db.insert(org, "user", "jack", ScopeBuildRead, []string{"proj-a", "proj-b"})
	// Jack also gets mcp:connect on mcp-internal only.
	db.insert(org, "user", "jack", ScopeMCPConnect, []string{"mcp-internal"})

	// Irene (member role + extra user grant): her user grant is NARROWER
	// than her role grant, but grants are additive — role wins.
	db.insert(org, "user", "irene", ScopeBuildRead, []string{"proj-a"})

	return db
}

// ===========================================================================
// SCENARIO 1: Alice — a regular admin
//
//	Role: admin
//	User grants: none
//	Expected: can do everything, unrestricted
// ===========================================================================

func TestWalkthroughAliceAdmin(t *testing.T) {
	db := seedTestDB()

	// Middleware resolves grants for Alice (admin role, no user-level grants).
	// DB query returns all 7 role rows, each with resources = nil.
	grants := db.resolveGrants("org-acme", "admin", "alice")
	ctx := ctxWithGrants(grants)

	printGrants(t, "Alice (admin)", grants)

	// Alice can read any project — unrestricted build:read from admin role
	mustCan(t, ctx, Check(ScopeBuildRead, "proj-xyz"))
	mustCan(t, ctx, Check(ScopeBuildRead, "proj-abc"))
	mustCan(t, ctx, Check(ScopeBuildRead, "proj-brand-new")) // even a project created 5 seconds ago

	// Alice can write to any project
	mustCan(t, ctx, Check(ScopeBuildWrite, "proj-xyz"))

	// Alice can manage the org
	mustCan(t, ctx, Check(ScopeOrgAdmin, ""))

	// Alice can connect to any MCP
	mustCan(t, ctx, Check(ScopeMCPConnect, "mcp-payments"))
	mustCan(t, ctx, Check(ScopeMCPConnect, "mcp-analytics"))

	// Alice can do multi-scope operations (e.g., publish toolset)
	mustCan(t, ctx,
		Check(ScopeBuildWrite, "proj-xyz"),
		Check(ScopeMCPWrite, "mcp-payments"),
	)

	// Filter: Alice sees all projects (unrestricted)
	filter := mustFilter(t, ctx, ScopeBuildRead)
	if filter != nil {
		t.Fatalf("expected nil (unrestricted), got %v", filter)
	}
}

// ===========================================================================
// SCENARIO 2: Dave — a regular member
//
//	Role: member
//	User grants: none
//	Expected: read everything, connect to MCPs, but NO write access
// ===========================================================================

func TestWalkthroughDaveMember(t *testing.T) {
	db := seedTestDB()

	grants := db.resolveGrants("org-acme", "member", "dave")
	ctx := ctxWithGrants(grants)

	printGrants(t, "Dave (member)", grants)

	// Dave can read any project — unrestricted build:read from member role
	mustCan(t, ctx, Check(ScopeBuildRead, "proj-xyz"))

	// Dave CANNOT write — member role doesn't have build:write
	mustDeny(t, ctx, Check(ScopeBuildWrite, "proj-xyz"))

	// Dave CANNOT admin the org
	mustDeny(t, ctx, Check(ScopeOrgAdmin, ""))

	// Dave CAN connect to any MCP — member role has mcp:connect unrestricted
	mustCan(t, ctx, Check(ScopeMCPConnect, "mcp-payments"))

	// Dave CANNOT write MCP config
	mustDeny(t, ctx, Check(ScopeMCPWrite, "mcp-payments"))

	// Filter: Dave sees all projects (unrestricted build:read from role)
	filter := mustFilter(t, ctx, ScopeBuildRead)
	if filter != nil {
		t.Fatalf("expected nil (unrestricted), got %v", filter)
	}

	// Filter: Dave gets DENIED for build:write (no write scope at all)
	mustFilterDeny(t, ctx, ScopeBuildWrite)
}

// ===========================================================================
// SCENARIO 3: Bob — contractor with single MCP access, no role
//
//	Role: (none — or a role with zero default grants, e.g., "contractor")
//	User grants: mcp:connect on ["mcp-payments"]
//	Expected: can only connect to mcp-payments, nothing else
// ===========================================================================

func TestWalkthroughBobContractor(t *testing.T) {
	db := seedTestDB()

	// Bob has no role grants (role "contractor" has no default grants).
	// Only user-level: mcp:connect on ["mcp-payments"].
	grants := db.resolveGrants("org-acme", "contractor", "bob")
	ctx := ctxWithGrants(grants)

	printGrants(t, "Bob (contractor)", grants)

	// Bob CAN connect to mcp-payments
	mustCan(t, ctx, Check(ScopeMCPConnect, "mcp-payments"))

	// Bob CANNOT connect to mcp-analytics (not in his allowlist)
	mustDeny(t, ctx, Check(ScopeMCPConnect, "mcp-analytics"))

	// Bob CANNOT read projects (no build:read grant at all)
	mustDeny(t, ctx, Check(ScopeBuildRead, "proj-xyz"))

	// Bob CANNOT list projects (no build:read → Filter returns error)
	mustFilterDeny(t, ctx, ScopeBuildRead)

	// Bob CANNOT read the org
	mustDeny(t, ctx, Check(ScopeOrgRead, ""))

	// KEY POINT: Bob has mcp:connect on mcp-payments, but NO build:read.
	// The MCP server might "belong to" a project, but there is NO implicit
	// hierarchy. Bob can call tools on mcp-payments but cannot see the
	// project that contains it. (Invariant 1)
}

// ===========================================================================
// SCENARIO 4: Grace — contractor with one project
//
//	Role: (none)
//	User grants: build:read on ["proj-xyz"]
//	Expected: can read proj-xyz, denied on everything else
// ===========================================================================

func TestWalkthroughGraceContractor(t *testing.T) {
	db := seedTestDB()

	grants := db.resolveGrants("org-acme", "contractor", "grace")
	ctx := ctxWithGrants(grants)

	printGrants(t, "Grace (contractor)", grants)

	// Grace CAN read proj-xyz
	mustCan(t, ctx, Check(ScopeBuildRead, "proj-xyz"))

	// Grace CANNOT read proj-abc (not in her allowlist)
	mustDeny(t, ctx, Check(ScopeBuildRead, "proj-abc"))

	// Filter: Grace's list is restricted to ["proj-xyz"]
	filter := mustFilter(t, ctx, ScopeBuildRead)
	if len(filter) != 1 || filter[0] != "proj-xyz" {
		t.Fatalf("expected [proj-xyz], got %v", filter)
	}

	// Grace CANNOT write (no build:write grant)
	mustDeny(t, ctx, Check(ScopeBuildWrite, "proj-xyz"))

	// Grace CANNOT connect to MCPs (no mcp:connect grant)
	mustDeny(t, ctx, Check(ScopeMCPConnect, "mcp-payments"))
}

// ===========================================================================
// SCENARIO 5: Jack — contractor with scoped access to multiple resources
//
//	Role: (none)
//	User grants:
//	  - build:read on ["proj-a", "proj-b"]
//	  - mcp:connect on ["mcp-internal"]
//	Expected: can read 2 projects, connect to 1 MCP, nothing else
// ===========================================================================

func TestWalkthroughJackContractor(t *testing.T) {
	db := seedTestDB()

	grants := db.resolveGrants("org-acme", "contractor", "jack")
	ctx := ctxWithGrants(grants)

	printGrants(t, "Jack (contractor)", grants)

	// Jack CAN read proj-a and proj-b
	mustCan(t, ctx, Check(ScopeBuildRead, "proj-a"))
	mustCan(t, ctx, Check(ScopeBuildRead, "proj-b"))

	// Jack CANNOT read proj-c (not in allowlist)
	mustDeny(t, ctx, Check(ScopeBuildRead, "proj-c"))

	// Jack CAN connect to mcp-internal
	mustCan(t, ctx, Check(ScopeMCPConnect, "mcp-internal"))

	// Jack CANNOT connect to mcp-payments (not in allowlist)
	mustDeny(t, ctx, Check(ScopeMCPConnect, "mcp-payments"))

	// Filter for build:read: returns ["proj-a", "proj-b"]
	filter := mustFilter(t, ctx, ScopeBuildRead)
	if len(filter) != 2 {
		t.Fatalf("expected 2 projects, got %v", filter)
	}

	// Filter for mcp:connect: returns ["mcp-internal"]
	mcpFilter := mustFilter(t, ctx, ScopeMCPConnect)
	if len(mcpFilter) != 1 || mcpFilter[0] != "mcp-internal" {
		t.Fatalf("expected [mcp-internal], got %v", mcpFilter)
	}

	// Filter for build:write: DENIED (no grant at all)
	mustFilterDeny(t, ctx, ScopeBuildWrite)
}

// ===========================================================================
// SCENARIO 6: Irene — member role + narrower user grant (additive model)
//
//	Role: member (unrestricted build:read, org:read, mcp:read, mcp:connect)
//	User grants: build:read on ["proj-a"]
//	Expected: the user grant does NOT restrict her. Role's unrestricted
//	          build:read wins. She can read ALL projects.
//
//	This is the most important scenario for understanding the additive model.
// ===========================================================================

func TestWalkthroughIreneAdditiveModel(t *testing.T) {
	db := seedTestDB()

	// Irene is a "member" AND has user-level build:read on ["proj-a"].
	// The resolver picks up BOTH the role grant and the user grant.
	grants := db.resolveGrants("org-acme", "member", "irene")
	ctx := ctxWithGrants(grants)

	printGrants(t, "Irene (member + user grant)", grants)

	// The resolved rows for build:read are:
	//   row 1: (role, member, build:read, nil)       ← unrestricted (from role)
	//   row 2: (user, irene,  build:read, [proj-a])  ← scoped (from user grant)
	//
	// hasAccess("build:read", "proj-b") iterates:
	//   row 1: scope matches, resources is nil → return true (UNRESTRICTED WINS)
	//   (row 2 is never even checked)

	// Irene CAN read proj-b — the unrestricted role grant dominates
	mustCan(t, ctx, Check(ScopeBuildRead, "proj-b"))

	// Irene CAN read proj-a too (obviously)
	mustCan(t, ctx, Check(ScopeBuildRead, "proj-a"))

	// Irene CAN read proj-brand-new — unrestricted means ALL projects
	mustCan(t, ctx, Check(ScopeBuildRead, "proj-brand-new"))

	// Filter: unrestricted (nil), not ["proj-a"]
	filter := mustFilter(t, ctx, ScopeBuildRead)
	if filter != nil {
		t.Fatalf("expected nil (unrestricted), got %v — user grant should NOT restrict below role", filter)
	}

	// KEY POINT: Adding a user-level grant NEVER restricts access.
	// To restrict Irene to proj-a only, you would need to change her role
	// to one that doesn't have unrestricted build:read (e.g., "contractor").
}

// ===========================================================================
// SCENARIO 7: UpdateToolset field-level gating
//
//	This simulates the handler logic for UpdateToolset, showing how different
//	users get different levels of access within the SAME endpoint.
// ===========================================================================

func TestWalkthroughUpdateToolsetFieldGating(t *testing.T) {
	db := seedTestDB()
	org := "org-acme"

	// Give Eve build:write on proj-xyz but NO mcp:write
	db.insert(org, "user", "eve", ScopeBuildRead, []string{"proj-xyz"})
	db.insert(org, "user", "eve", ScopeBuildWrite, []string{"proj-xyz"})

	// Give Frank build:write AND mcp:write on proj-xyz / mcp-payments
	db.insert(org, "user", "frank-dev", ScopeBuildRead, []string{"proj-xyz"})
	db.insert(org, "user", "frank-dev", ScopeBuildWrite, []string{"proj-xyz"})
	db.insert(org, "user", "frank-dev", ScopeMCPWrite, []string{"mcp-payments"})

	// --- Eve: can update green fields, not red fields ---
	eveGrants := db.resolveGrants(org, "contractor", "eve")
	eveCtx := ctxWithGrants(eveGrants)

	printGrants(t, "Eve (contractor + build:write)", eveGrants)

	// Step 1: build:write check (gates green fields) — ALLOW
	mustCan(t, eveCtx, Check(ScopeBuildWrite, "proj-xyz"))

	// Step 2: mcp:write check (gates red fields) — DENY
	// This is what happens when Eve tries to set mcp_enabled=true
	mustDeny(t, eveCtx, Check(ScopeMCPWrite, "mcp-payments"))

	// So: Eve can update name, description, tools, templates (green)
	// but NOT mcp_enabled, mcp_slug, mcp_is_public (red).

	// --- Frank: can update everything ---
	frankGrants := db.resolveGrants(org, "contractor", "frank-dev")
	frankCtx := ctxWithGrants(frankGrants)

	printGrants(t, "Frank (contractor + build:write + mcp:write)", frankGrants)

	// Step 1: build:write — ALLOW
	mustCan(t, frankCtx, Check(ScopeBuildWrite, "proj-xyz"))

	// Step 2: mcp:write — ALLOW
	mustCan(t, frankCtx, Check(ScopeMCPWrite, "mcp-payments"))

	// Frank can update all fields — green AND red.

	// --- Alice (admin): can update everything (unrestricted) ---
	aliceGrants := db.resolveGrants(org, "admin", "alice")
	aliceCtx := ctxWithGrants(aliceGrants)

	mustCan(t, aliceCtx, Check(ScopeBuildWrite, "proj-xyz"))
	mustCan(t, aliceCtx, Check(ScopeMCPWrite, "mcp-payments"))
}

// ===========================================================================
// SCENARIO 8: API key (legacy scope translation)
//
//	API keys bypass the principal_grants table entirely. The middleware
//	translates legacy scopes to synthetic grant rows.
// ===========================================================================

func TestWalkthroughAPIKeyLegacy(t *testing.T) {
	// Simulate what the resolver does for API keys.
	// No DB query — purely in-code translation.

	legacyMap := map[string][]Scope{
		"producer": {ScopeOrgRead, ScopeBuildRead, ScopeBuildWrite, ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect},
		"consumer": {ScopeOrgRead, ScopeBuildRead, ScopeMCPRead, ScopeMCPConnect},
		"chat":     {ScopeMCPRead, ScopeMCPConnect},
	}

	buildSyntheticGrants := func(legacyScopes []string) *Grants {
		g := &Grants{}
		for _, ls := range legacyScopes {
			for _, scope := range legacyMap[ls] {
				g.rows = append(g.rows, grantRow{Scope: scope, Resources: nil}) // all unrestricted
			}
		}
		return g
	}

	// --- Producer key ---
	producerGrants := buildSyntheticGrants([]string{"producer"})
	producerCtx := ctxWithGrants(producerGrants)

	printGrants(t, "API key (producer)", producerGrants)

	mustCan(t, producerCtx, Check(ScopeBuildWrite, "proj-xyz"))     // can write
	mustCan(t, producerCtx, Check(ScopeMCPConnect, "mcp-payments")) // can connect
	mustDeny(t, producerCtx, Check(ScopeOrgAdmin, ""))              // CANNOT admin (intentional)

	// --- Consumer key ---
	consumerGrants := buildSyntheticGrants([]string{"consumer"})
	consumerCtx := ctxWithGrants(consumerGrants)

	mustCan(t, consumerCtx, Check(ScopeBuildRead, "proj-xyz"))      // can read
	mustCan(t, consumerCtx, Check(ScopeMCPConnect, "mcp-payments")) // can connect
	mustDeny(t, consumerCtx, Check(ScopeBuildWrite, "proj-xyz"))    // CANNOT write
	mustDeny(t, consumerCtx, Check(ScopeMCPWrite, "mcp-payments"))  // CANNOT write MCP

	// --- Chat key ---
	chatGrants := buildSyntheticGrants([]string{"chat"})
	chatCtx := ctxWithGrants(chatGrants)

	mustCan(t, chatCtx, Check(ScopeMCPConnect, "mcp-payments")) // can connect
	mustDeny(t, chatCtx, Check(ScopeBuildRead, "proj-xyz"))     // CANNOT read projects
	mustDeny(t, chatCtx, Check(ScopeOrgRead, ""))               // CANNOT read org
}

// ===========================================================================
// SCENARIO 9: New project — no seeding needed
//
//	When a new project is created, users with unrestricted build:read
//	can access it immediately. No rows need to be inserted.
//	Users with scoped grants do NOT get access automatically.
// ===========================================================================

func TestWalkthroughNewProjectNoSeeding(t *testing.T) {
	db := seedTestDB()

	brandNewProjectID := "proj-created-5-seconds-ago"

	// Dave (member) — unrestricted build:read from role
	daveGrants := db.resolveGrants("org-acme", "member", "dave")
	daveCtx := ctxWithGrants(daveGrants)

	// Dave CAN read the new project — no grant insertion needed
	mustCan(t, daveCtx, Check(ScopeBuildRead, brandNewProjectID))

	// Jack (contractor) — scoped build:read on [proj-a, proj-b]
	jackGrants := db.resolveGrants("org-acme", "contractor", "jack")
	jackCtx := ctxWithGrants(jackGrants)

	// Jack CANNOT read the new project — it's not in his allowlist
	mustDeny(t, jackCtx, Check(ScopeBuildRead, brandNewProjectID))

	// An admin would need to add brandNewProjectID to Jack's allowlist
	// via UPDATE principal_grants SET resources = array_append(resources, 'proj-created-5-seconds-ago')
}

// ===========================================================================
// Helper: print resolved grants for debugging
// ===========================================================================

func printGrants(t *testing.T, label string, g *Grants) {
	t.Helper()
	t.Logf("=== Resolved grants for %s ===", label)
	if len(g.rows) == 0 {
		t.Logf("  (no grants)")
		return
	}
	for _, row := range g.rows {
		if row.Resources == nil {
			t.Logf("  %-15s  resources: NULL (unrestricted)", row.Scope)
		} else {
			t.Logf("  %-15s  resources: [%s]", row.Scope, strings.Join(row.Resources, ", "))
		}
	}
	_ = fmt.Sprintf("") // suppress unused import
}
