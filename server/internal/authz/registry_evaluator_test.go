package authz

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
)

// evaluatorSliceDeclarations extends the frozen F2 slice with the types the
// F1 decision corpus also exercises. Like frozenSliceDeclarations they exist
// only in tests; TestRegistryEvaluatorSliceMatchesFrozenTables keeps them
// honest against scope_tables.golden.
func evaluatorSliceDeclarations() []s.TypeDecl {
	access := string(GrantSurfaceAccess)
	riskSurface := string(GrantSurfaceRiskPolicy)
	return append(frozenSliceDeclarations(),
		s.NewType("environment", s.Parent("project"), s.WrittenBy(access),
			s.SelectorKeys(SelectorKeyProjectID),
			s.Grant("reader", s.StoredAs(string(ScopeEnvironmentRead)), s.AnchoredAt("organization", "project", s.Self),
				s.Describe("Read environments"), s.DefaultFor(SystemRoleAdmin), s.RuntimeSafeSince(1)),
			s.Grant("writer", s.StoredAs(string(ScopeEnvironmentWrite)), s.AnchoredAt("organization", "project", s.Self),
				s.Describe("Change environments"), s.DefaultFor(SystemRoleAdmin), s.RuntimeSafeSince(1)),
			s.Block("read_blocked", s.StoredAs(string(ScopeEnvironmentBlockedRead)), s.RuntimeSafeSince(4)),
			s.Block("write_blocked", s.StoredAs(string(ScopeEnvironmentBlockedWrite)), s.RuntimeSafeSince(4)),
			s.Permission("read", s.Union(s.Rel("reader"), s.Rel("writer")),
				s.Checks(string(ScopeEnvironmentRead)), s.ExcludedBy("read_blocked")),
			s.Permission("write", s.Rel("writer"),
				s.Checks(string(ScopeEnvironmentWrite)), s.ExcludedBy("write_blocked", "read_blocked")),
		),
		s.NewType("skill", s.Parent("project"), s.WrittenBy(access),
			s.Grant("reader", s.StoredAs(string(ScopeSkillRead)),
				s.Describe("Read skills"), s.DefaultFor(SystemRoleAdmin, SystemRoleMember), s.RuntimeSafeSince(1)),
			s.Grant("writer", s.StoredAs(string(ScopeSkillWrite)),
				s.Describe("Change skills"), s.DefaultFor(SystemRoleAdmin), s.RuntimeSafeSince(1)),
			s.Block("read_blocked", s.StoredAs(string(ScopeSkillBlockedRead)), s.RuntimeSafeSince(4)),
			s.Block("write_blocked", s.StoredAs(string(ScopeSkillBlockedWrite)), s.RuntimeSafeSince(4)),
			s.Permission("read", s.Union(s.Rel("reader"), s.Rel("writer")),
				s.Checks(string(ScopeSkillRead)), s.ExcludedBy("read_blocked")),
			s.Permission("write", s.Rel("writer"),
				s.Checks(string(ScopeSkillWrite)), s.ExcludedBy("write_blocked", "read_blocked")),
		),
		s.NewType("chat", s.Parent("project"), s.WrittenBy(access),
			s.Grant("reader", s.StoredAs(string(ScopeChatRead)), s.Sensitive(), s.Describe("Read session transcripts")),
			s.Grant("writer", s.StoredAs(string(ScopeChatWrite)), s.Describe("Change sessions")),
			s.Permission("read", s.Union(s.Rel("reader"), s.Rel("writer")), s.Checks(string(ScopeChatRead))),
			s.Permission("write", s.Rel("writer"), s.Checks(string(ScopeChatWrite))),
		),
		s.NewType("agent", s.Parent("organization"), s.WrittenBy(access),
			s.Grant("reader", s.StoredAs(string(ScopeAgentRead)), s.Describe("Read agents"), s.DefaultFor(SystemRoleAdmin)),
			s.Permission("read", s.Rel("reader"), s.Checks(string(ScopeAgentRead))),
		),
		s.NewType("risk_policy", s.WrittenBy(riskSurface),
			s.SelectorKeys(SelectorKeyServerIdentity, SelectorKeyServerURL),
			s.Grant("included", s.StoredAs(string(ScopeRiskPolicyEvaluate)), s.Describe("Apply the policy"), s.RuntimeSafeSince(1)),
			s.Grant("bypassed", s.StoredAs(string(ScopeRiskPolicyBypass)), s.Describe("Skip the policy")),
			s.Grant("blocker", s.StoredAs(string(ScopeRiskPolicyBlock)), s.Describe("Block with the policy")),
			s.Permission("evaluate", s.Minus(s.Rel("included"), s.Rel("bypassed")), s.Checks(string(ScopeRiskPolicyEvaluate))),
			s.Permission("bypass", s.Rel("bypassed"), s.Checks(string(ScopeRiskPolicyBypass))),
			s.Permission("block", s.Rel("blocker"), s.Checks(string(ScopeRiskPolicyBlock))),
		),
	)
}

func compileEvaluatorSlice(t *testing.T) *s.Registry {
	t.Helper()
	reg, err := s.Compile(s.Config{
		Roles:         []string{SystemRoleAdmin, SystemRoleMember},
		WriteSurfaces: []string{string(GrantSurfaceAccess), string(GrantSurfaceRiskPolicy)},
		Conditions: []s.Condition{
			{Name: "mcp_tools", SelectorKeys: []string{SelectorKeyTool, SelectorKeyDisposition}},
		},
		RetiredRuntimeScopes: nil,
	}, evaluatorSliceDeclarations()...)
	require.NoError(t, err)
	return reg
}

// TestRegistryEvaluatorSliceMatchesFrozenTables keeps the extra test
// declarations faithful to today's tables, so the corpus below runs against
// the same one-step expansions and exclusions Require uses.
func TestRegistryEvaluatorSliceMatchesFrozenTables(t *testing.T) {
	t.Parallel()

	reg := compileEvaluatorSlice(t)
	frozen := readGoldenRecords(t, filepath.Join(foundationCompatibilityDir, "scope_tables.golden"))
	for _, scope := range reg.Scopes() {
		record, ok := frozen[scope.Name]
		require.True(t, ok, "compiled scope %s is missing from the frozen scope tables", scope.Name)
		require.Equal(t, record, renderCompiledScope(scope), scope.Name)
	}
}

// corpusSkips lists every selected F1 fixture the grant evaluator does not
// run, and why. The test fails when a fixture is skipped without an entry or
// an entry no longer applies, so nothing drops out silently.
var corpusSkips = map[string]string{
	"allow_explicit_kind_override_must_match_grant_kind": "typed permissions take the resource kind from the declared type; a caller cannot override it",
	"no_checks_errors": "an empty check list is a client concern (F5), not a permission evaluation",
}

// TestRegistryEvaluatorFoundationCorpus runs the hand-written F1 decisions for
// ordinary human callers through the registry evaluator. The expectations
// come from the frozen fixtures, never from the old evaluator's output, so a
// broken compiled projection cannot make both sides agree.
//
// Require is every check passing, RequireAny one of them, and RiskPolicyApplies
// the raw evaluate − bypass difference. Other callers and the RequireAnyUnblocked
// veto compose several policies or named alternatives (F4).
func TestRegistryEvaluatorFoundationCorpus(t *testing.T) {
	t.Parallel()

	reg := compileEvaluatorSlice(t)
	evaluator, err := NewRegistryEvaluator(reg)
	require.NoError(t, err)

	paths, err := filepath.Glob(filepath.Join(foundationCompatibilityDir, "*.json"))
	require.NoError(t, err)

	selected := map[string]bool{"require": true, "require_any": true, "risk_policy_applies": true}
	var ran, skipped []string
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var file foundationFixtureFile
		require.NoError(t, json.Unmarshal(raw, &file), path)

		for _, fixture := range file.Cases {
			if fixture.Caller != "human" || !selected[fixture.Operation] {
				continue
			}
			if _, skip := corpusSkips[fixture.Name]; skip {
				skipped = append(skipped, fixture.Name)
				continue
			}
			ran = append(ran, fixture.Name)
			t.Run(fixture.Name, func(t *testing.T) {
				t.Parallel()
				runEvaluatorCorpusCase(t, reg, evaluator, fixture)
			})
		}
	}

	var stale []string
	for name := range corpusSkips {
		if !slices.Contains(skipped, name) {
			stale = append(stale, name)
		}
	}
	require.Empty(t, stale, "corpusSkips lists fixtures that are no longer selected")
	require.Len(t, ran, 71, "the selected F1 corpus changed; update the expected count after checking every new case runs")
}

func runEvaluatorCorpusCase(t *testing.T, reg *s.Registry, evaluator *RegistryEvaluator, f foundationFixture) {
	t.Helper()

	policy := foundationGrants(f.Grants)
	decisions := make([]bool, 0, len(f.Checks))
	var evalErr error
	for _, c := range f.Checks {
		require.Empty(t, c.ResourceKind, "kind overrides belong in corpusSkips")
		ref, ok := reg.PermissionForCheck(string(c.Scope))
		require.True(t, ok, "no compiled permission checks %s; declare its type in evaluatorSliceDeclarations", c.Scope)
		decision, err := evaluator.Evaluate(policy, PermissionRequest{
			Permission:      ref,
			ResourceID:      c.ResourceID,
			Dimensions:      c.Dimensions,
			StrictMatch:     c.Strict,
			RepresentedUser: fixtureUserID,
		})
		if err != nil {
			// RequireAny validates every check before deciding, so an invalid
			// later check errors even after an earlier one allows.
			evalErr = cmpErr(evalErr, err)
			continue
		}
		decisions = append(decisions, decision.Allowed)
	}

	var allowed bool
	switch f.Operation {
	case "require", "risk_policy_applies":
		allowed = !slices.Contains(decisions, false)
	case "require_any":
		allowed = slices.Contains(decisions, true)
	default:
		t.Fatalf("unexpected operation %q", f.Operation)
	}

	switch {
	case strings.HasPrefix(f.Want, "error"):
		require.Error(t, evalErr, "want %s", f.Want)
		require.ErrorIs(t, evalErr, ErrInvalidCheck)
	case evalErr != nil:
		t.Fatalf("unexpected error: %v", evalErr)
	case f.Want == "allow":
		require.True(t, allowed, "want allow")
	case f.Want == "deny":
		require.False(t, allowed, "want deny")
	default:
		t.Fatalf("unexpected want %q", f.Want)
	}
}

func cmpErr(first, next error) error {
	if first != nil {
		return first
	}
	return next
}

// The cases below pin evaluator behaviour the F1 corpus cannot express:
// nested subtraction, permission reuse, branch order and evidence.

const (
	testDirectUser = "user:" + fixtureUserID
	testRole       = "role:organization:role_fixture"
)

func grantOf(principal string, scope string, kind string, id string, extra ...string) Grant {
	selector := Selector{SelectorKeyResourceKind: kind, SelectorKeyResourceID: id}
	for i := 0; i+1 < len(extra); i += 2 {
		selector[extra[i]] = extra[i+1]
	}
	return Grant{PrincipalUrn: principal, Scope: Scope(scope), Selector: selector, Inferred: false}
}

// docDeclarations is a synthetic type exercising grammar no production slice
// uses yet: a permission reused through Perm with its own blocks, raw Minus
// around and inside it, and an ExcludedBy wrapping a subtraction.
func docDeclarations() []s.TypeDecl {
	return []s.TypeDecl{
		s.NewType("doc", s.WrittenBy("access"), s.SelectorKeys(SelectorKeyTool, SelectorKeyDisposition),
			s.Grant("viewer", s.StoredAs("doc:read"), s.Describe("Read")),
			s.Grant("editor", s.StoredAs("doc:write"), s.Describe("Write")),
			s.Grant("archived", s.StoredAs("doc:archived"), s.Describe("Archived")),
			s.Grant("hidden", s.StoredAs("doc:hidden"), s.Describe("Hidden")),
			s.Block("read_blocked", s.StoredAs("doc:blocked_read")),
			s.Block("write_blocked", s.StoredAs("doc:blocked_write")),
			s.Permission("read", s.Union(s.Rel("viewer"), s.Rel("editor")),
				s.Checks("doc:read"), s.ExcludedBy("read_blocked")),
			s.Permission("write", s.Rel("editor"), s.Checks("doc:write"), s.ExcludedBy("write_blocked")),
			s.Permission("is_archived", s.Rel("archived"), s.Checks("doc:archived")),
			s.Permission("is_hidden", s.Rel("hidden"), s.Checks("doc:hidden")),
			// An exception to the inner block cannot undo the outer subtraction.
			s.Permission("live_read", s.Minus(s.Perm("read"), s.Rel("archived"))),
			// An exception to the outer block cannot undo the inner subtraction.
			s.Permission("visible_read", s.Minus(s.Union(s.Rel("viewer"), s.Rel("editor")), s.Rel("hidden")),
				s.ExcludedBy("read_blocked")),
			// Nested raw subtraction: ((viewer − hidden) − archived).
			s.Permission("nested", s.Minus(s.Minus(s.Rel("viewer"), s.Rel("hidden")), s.Rel("archived"))),
			// The block of the reused permission applies to its branch only.
			s.Permission("read_or_write", s.Union(s.Perm("read"), s.Perm("write"))),
		),
		s.NewType("note", s.WrittenBy("access"),
			s.Attr("owner", "user"),
			s.Grant("viewer", s.StoredAs("note:read"), s.Describe("Read")),
			s.Permission("read", s.Rel("viewer"), s.Checks("note:read")),
			s.Permission("own_first", s.Union(s.Is("owner"), s.Rel("viewer"))),
			s.Permission("own_last", s.Union(s.Rel("viewer"), s.Is("owner"))),
			s.Permission("via_own", s.Perm("own_last")),
		),
	}
}

func docEvaluator(t *testing.T) *RegistryEvaluator {
	t.Helper()
	reg, err := s.Compile(s.Config{Roles: nil, WriteSurfaces: []string{"access"}, Conditions: nil, RetiredRuntimeScopes: nil}, docDeclarations()...)
	require.NoError(t, err)
	evaluator, err := NewRegistryEvaluator(reg)
	require.NoError(t, err)
	return evaluator
}

func docRequest(permission string, dims map[string]string) PermissionRequest {
	return PermissionRequest{
		Permission:      s.PermissionRef{Type: "doc", Permission: permission},
		ResourceID:      "doc_1",
		Dimensions:      dims,
		StrictMatch:     false,
		RepresentedUser: "",
	}
}

func TestRegistryEvaluatorPrecedence(t *testing.T) {
	t.Parallel()

	evaluator := docEvaluator(t)
	roleBlock := grantOf(testRole, "doc:blocked_read", "doc", "doc_1")
	roleRead := grantOf(testRole, "doc:read", "doc", "*")
	directRead := grantOf(testDirectUser, "doc:read", "doc", "doc_1")

	cases := []struct {
		name       string
		permission string
		dims       map[string]string
		policy     []Grant
		want       bool
		reason     PermissionReason
	}{
		{"inherited block removes access", "read", nil, []Grant{roleRead, roleBlock}, false, PermissionReasonExcluded},
		{"direct concrete grant restores", "read", nil, []Grant{roleRead, roleBlock, directRead}, true, PermissionReasonDirectException},
		{"own block beats own exception", "read", nil,
			[]Grant{directRead, grantOf(testDirectUser, "doc:blocked_read", "doc", "doc_1")}, false, PermissionReasonExcluded},
		{"agent grant never restores", "read", nil,
			[]Grant{roleBlock, grantOf("agent:"+fixtureAgentID, "doc:read", "doc", "doc_1")}, false, PermissionReasonExcluded},
		{"api key grant never restores", "read", nil,
			[]Grant{roleBlock, grantOf("api_key:key_1", "doc:read", "doc", "doc_1")}, false, PermissionReasonExcluded},
		{"inferred direct grant never restores", "read", nil,
			[]Grant{roleBlock, {PrincipalUrn: testDirectUser, Scope: "doc:read", Selector: Selector{SelectorKeyResourceKind: "doc", SelectorKeyResourceID: "doc_1"}, Inferred: true}},
			false, PermissionReasonExcluded},
		{"exception to inner block cannot undo outer minus", "live_read", nil,
			[]Grant{roleBlock, directRead, grantOf(testRole, "doc:archived", "doc", "*")}, false, PermissionReasonSubtracted},
		{"exception to inner block survives outer minus without match", "live_read", nil,
			[]Grant{roleBlock, directRead}, true, PermissionReasonDirectException},
		{"exception to outer block cannot undo inner minus", "visible_read", nil,
			[]Grant{roleBlock, directRead, grantOf(testRole, "doc:hidden", "doc", "*")}, false, PermissionReasonSubtracted},
		{"nested minus subtracts at every level", "nested", nil,
			[]Grant{roleRead, grantOf(testRole, "doc:archived", "doc", "doc_1")}, false, PermissionReasonSubtracted},
		{"nested minus with no subtraction allows", "nested", nil,
			[]Grant{roleRead, grantOf(testRole, "doc:archived", "doc", "doc_2")}, true, PermissionReasonMatched},
		{"raw minus is never restored by a direct grant", "nested", nil,
			[]Grant{directRead, grantOf(testRole, "doc:hidden", "doc", "doc_1")}, false, PermissionReasonSubtracted},
		{"reused permission block applies to its branch only", "read_or_write", nil,
			[]Grant{roleBlock, grantOf(testRole, "doc:write", "doc", "*")}, true, PermissionReasonMatched},
		{"subtraction matches the request instance only", "nested", map[string]string{SelectorKeyTool: "search"},
			[]Grant{roleRead, grantOf(testRole, "doc:hidden", "doc", "doc_1", SelectorKeyTool, "delete")}, true, PermissionReasonMatched},
		{"tool-scoped subtraction skips a dimensionless request", "nested", nil,
			[]Grant{roleRead, grantOf(testRole, "doc:hidden", "doc", "doc_1", SelectorKeyTool, "delete")}, true, PermissionReasonMatched},
		{"root allows", "read", nil, []Grant{grantOf(testRole, "root", "*", "*")}, true, PermissionReasonMatched},
		{"root never subtracts", "nested", nil,
			[]Grant{roleRead, grantOf(testRole, "root", "*", "*")}, true, PermissionReasonMatched},
		{"root never blocks", "read", nil,
			[]Grant{roleRead, grantOf(testDirectUser, "root", "*", "*")}, true, PermissionReasonMatched},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decision, err := evaluator.Evaluate(tc.policy, docRequest(tc.permission, tc.dims))
			require.NoError(t, err)
			require.Equal(t, tc.want, decision.Allowed)
			require.Equal(t, tc.reason, decision.Reason)
		})
	}
}

func TestRegistryEvaluatorEvidence(t *testing.T) {
	t.Parallel()

	evaluator := docEvaluator(t)
	roleBlock := grantOf(testRole, "doc:blocked_read", "doc", "doc_1")
	directRead := grantOf(testDirectUser, "doc:read", "doc", "doc_1")
	policy := []Grant{grantOf(testRole, "doc:read", "doc", "*"), roleBlock}

	decision, err := evaluator.Evaluate(policy, docRequest("read", nil))
	require.NoError(t, err)
	require.Equal(t, &policy[0], decision.Grant)
	require.Equal(t, &roleBlock, decision.Removed)

	decision, err = evaluator.Evaluate(append(policy, directRead), docRequest("read", nil))
	require.NoError(t, err)
	require.Equal(t, &directRead, decision.Grant)
	require.Nil(t, decision.Removed)

	// Evidence is a copy: changing it leaves the policy alone.
	decision.Grant.Selector[SelectorKeyResourceID] = "changed"
	require.Equal(t, "doc_1", directRead.Selector[SelectorKeyResourceID])
}

func TestRegistryEvaluatorErrors(t *testing.T) {
	t.Parallel()

	evaluator := docEvaluator(t)
	policy := []Grant{grantOf(testRole, "note:read", "note", "*"), grantOf(testRole, "doc:read", "doc", "*")}

	// A fact-reading node errors whichever branch it sits in, and through
	// Perm, even when a grant branch would allow.
	for _, permission := range []string{"own_first", "own_last", "via_own"} {
		_, err := evaluator.Evaluate(policy, PermissionRequest{
			Permission:      s.PermissionRef{Type: "note", Permission: permission},
			ResourceID:      "note_1",
			Dimensions:      nil,
			StrictMatch:     false,
			RepresentedUser: "",
		})
		require.ErrorIs(t, err, ErrUnsupportedExpression, permission)
	}

	_, err := evaluator.Evaluate(policy, docRequest("missing", nil))
	require.ErrorIs(t, err, ErrUnknownPermission)

	for _, id := range []string{"", WildcardResource} {
		req := docRequest("read", nil)
		req.ResourceID = id
		_, err := evaluator.Evaluate(policy, req)
		require.ErrorIs(t, err, ErrInvalidCheck, "resource id %q", id)
	}

	_, err = evaluator.Evaluate(policy, docRequest("read", map[string]string{SelectorKeyProjectID: "proj_1"}))
	require.ErrorIs(t, err, ErrInvalidCheck, "a dimension no scope accepts is rejected, not ignored")

	_, err = NewRegistryEvaluator(nil)
	require.Error(t, err)
}

func TestRegistryEvaluatorMatchesRequireForMCPToolCalls(t *testing.T) {
	t.Parallel()

	reg := compileEvaluatorSlice(t)
	evaluator, err := NewRegistryEvaluator(reg)
	require.NoError(t, err)
	connect := s.PermissionRef{Type: "mcp_server", Permission: "connect"}

	policies := [][]Grant{
		{grantOf(testRole, string(ScopeMCPRead), ResourceKindMCP, "*")},
		{grantOf(testRole, string(ScopeMCPWrite), ResourceKindMCP, "srv_1")},
		{grantOf(testRole, string(ScopeMCPConnect), ResourceKindMCP, "srv_1", SelectorKeyDisposition, DispositionReadOnly)},
		{
			grantOf(testRole, string(ScopeMCPConnect), ResourceKindMCP, "*"),
			grantOf(testRole, string(ScopeMCPBlockedConnect), ResourceKindMCP, "srv_1"),
			grantOf(testDirectUser, string(ScopeMCPConnect), ResourceKindMCP, "srv_1", SelectorKeyDisposition, DispositionReadOnly),
		},
		{
			grantOf(testRole, string(ScopeMCPConnect), ResourceKindMCP, "*", SelectorKeyProjectID, "proj_1"),
			grantOf(testRole, string(ScopeMCPBlockedConnect), ResourceKindMCP, "*", SelectorKeyTool, "delete"),
		},
		{grantOf("api_key:key_1", string(ScopeMCPConnect), ResourceKindMCP, "srv_1", SelectorKeyProjectID, "proj_1")},
	}
	requests := []MCPToolCallDimensions{
		{Tool: "", Disposition: "", ProjectID: ""},
		{Tool: "search", Disposition: "", ProjectID: ""},
		{Tool: "search", Disposition: DispositionReadOnly, ProjectID: ""},
		{Tool: "delete", Disposition: DispositionDestructive, ProjectID: "proj_1"},
		{Tool: "search", Disposition: "", ProjectID: "proj_2"},
		{Tool: "", Disposition: "", ProjectID: "proj_1"},
	}

	// Each pair is compared with the unchanged evaluator. This is a
	// consistency sweep over the MCP grammar; the authoritative expectations
	// stay in the F1 corpus above.
	for i, policy := range policies {
		for _, dims := range requests {
			check := MCPToolCallCheck("srv_1", dims)
			want, err := GrantsAuthorize(policy, check)
			require.NoError(t, err)
			decision, err := evaluator.Evaluate(policy, PermissionRequest{
				Permission:      connect,
				ResourceID:      "srv_1",
				Dimensions:      check.Dimensions,
				StrictMatch:     false,
				RepresentedUser: "",
			})
			require.NoError(t, err)
			require.Equal(t, want, decision.Allowed, "policy %d, request %+v", i, dims)
		}
	}
}
