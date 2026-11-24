package authz

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// A grant key the check does not carry never matches: absence is not
// permission.
func TestSelector_StrictMatches_missingKeyNeverMatches(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_id": "proj_123"}
	require.False(t, grant.StrictMatches(Selector{}))
	require.False(t, grant.StrictMatches(Selector{"other_key": "proj_123"}))
}

// A scope the registry does not declare, such as a retired one still stored
// in an old policy, keeps the kind its prefix has always meant. Declared
// scopes are pinned by the generated tables and the decision snapshot.
func TestResourceKindForScope_undeclaredScopesFallBackToTheirPrefix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		scope Scope
		want  string
	}{
		{"plugin:retired", ResourceKindProject},
		{"log:retired", ResourceKindProject},
		{"remote-mcp:read", ResourceKindMCP},
		{"skill:retired", ResourceKindSkill},
		{"mcp_auth_client:x", ResourceKindWildcard},
		{"unknown:read", ResourceKindWildcard},
	} {
		t.Run(string(tc.scope), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, ResourceKindForScope(tc.scope))
		})
	}
}

func TestSelector_ResourceID_present(t *testing.T) {
	t.Parallel()

	require.Equal(t, "proj_123", Selector{"resource_id": "proj_123"}.ResourceID())
}

func TestSelector_ResourceID_wildcard(t *testing.T) {
	t.Parallel()

	require.Equal(t, "*", Selector{"resource_id": "*"}.ResourceID())
}

func TestSelector_ResourceID_absent(t *testing.T) {
	t.Parallel()

	require.Equal(t, "*", Selector{}.ResourceID())
	require.Equal(t, "*", Selector(nil).ResourceID())
}

func TestSelector_CoversEveryResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		selector Selector
		covers   bool
	}{
		{name: "nil", selector: nil, covers: true},
		{name: "pure wildcard", selector: Selector{"resource_kind": "*", "resource_id": "*"}, covers: true},
		{name: "stored unrestricted grant", selector: NewSelector(ScopeLogRead, WildcardResource), covers: true},
		{name: "one resource", selector: NewSelector(ScopeLogRead, "project-1"), covers: false},
		{name: "one project of a kind", selector: Selector{"resource_kind": "mcp", "resource_id": "*", "project_id": "p1"}, covers: false},
		{name: "one tool", selector: Selector{"resource_kind": "mcp", "resource_id": "*", "tool": "search"}, covers: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.covers, tt.selector.CoversEveryResource())
		})
	}
}

func TestSelector_IsRestricted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		selector   Selector
		restricted bool
	}{
		{
			name:       "pure wildcard",
			selector:   Selector{"resource_kind": "*", "resource_id": "*"},
			restricted: false,
		},
		{
			name:       "scope wildcard is still scoped",
			selector:   Selector{"resource_kind": "mcp", "resource_id": "*"},
			restricted: true,
		},
		{
			name:       "extra dimension is scoped",
			selector:   Selector{"resource_kind": "*", "resource_id": "*", "tool": "dangerous"},
			restricted: true,
		},
		{
			name:       "legacy resource wildcard is unrestricted",
			selector:   Selector{"resource_id": "*"},
			restricted: false,
		},
		{
			name:       "missing resource id is restricted",
			selector:   Selector{"resource_kind": "*"},
			restricted: true,
		},
		{
			name:       "unknown one-key selector is restricted",
			selector:   Selector{"unknown": "*"},
			restricted: true,
		},
		{
			name:       "missing resource kind with extra key is restricted",
			selector:   Selector{"resource_id": "*", "unknown": "*"},
			restricted: true,
		},
		{
			name:       "nil is unrestricted wildcard",
			selector:   nil,
			restricted: false,
		},
		{
			name:       "empty is unrestricted wildcard",
			selector:   Selector{},
			restricted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.restricted, tt.selector.IsRestricted())
		})
	}
}

func TestSelector_MarshalJSON_nil(t *testing.T) {
	t.Parallel()

	var s Selector
	b, err := json.Marshal(s)
	require.NoError(t, err)
	require.JSONEq(t, `{"resource_kind":"*","resource_id":"*"}`, string(b))
}

func TestSelector_MarshalJSON_withKeys(t *testing.T) {
	t.Parallel()

	s := Selector{"resource_id": "proj_123"}
	b, err := json.Marshal(s)
	require.NoError(t, err)
	require.JSONEq(t, `{"resource_id":"proj_123"}`, string(b))
}

func TestSelector_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	var s Selector
	err := json.Unmarshal([]byte(`{"resource_id":"proj_123","tool_id":"t1"}`), &s)
	require.NoError(t, err)
	require.Equal(t, "proj_123", s["resource_id"])
	require.Equal(t, "t1", s["tool_id"])
}

func TestValidateSelector_agentRequiresAgentResourceKind(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateSelector(ScopeAgentRead, Selector{
		SelectorKeyResourceKind: ResourceKindAgent,
		SelectorKeyResourceID:   "0196cbd1-9328-74e7-b7bb-6e5357565573",
	}))
	require.ErrorContains(t, ValidateSelector(ScopeAgentWrite, Selector{
		SelectorKeyResourceKind: ResourceKindProject,
		SelectorKeyResourceID:   "0196cbd1-9328-74e7-b7bb-6e5357565573",
	}), `requires resource_kind="agent"`)
}

func TestValidateSelector_assistantUsesProjectIDWithAssistantResourceKind(t *testing.T) {
	t.Parallel()

	projectID := "0196cbd1-9328-74e7-b7bb-6e5357565573"
	selector := NewSelector(ScopeAssistantRead, projectID)
	require.Equal(t, ResourceKindAssistant, selector[SelectorKeyResourceKind])
	require.Equal(t, projectID, selector[SelectorKeyResourceID])
	require.NoError(t, ValidateSelector(ScopeAssistantRead, selector))
	require.ErrorContains(t, ValidateSelector(ScopeAssistantWrite, NewSelector(ScopeProjectWrite, projectID)), `requires resource_kind="assistant"`)
}

func TestValidateSelector_skillRequiresSkillResourceKind(t *testing.T) {
	t.Parallel()

	projectID := "0196cbd1-9328-74e7-b7bb-6e5357565573"
	require.NoError(t, ValidateSelector(ScopeSkillRead, Selector{
		SelectorKeyResourceKind: ResourceKindSkill,
		SelectorKeyResourceID:   projectID,
	}))
	require.ErrorContains(t, ValidateSelector(ScopeSkillWrite, Selector{
		SelectorKeyResourceKind: ResourceKindProject,
		SelectorKeyResourceID:   projectID,
	}), `requires resource_kind="skill"`)
}

func TestNewSelector_includesResourceKind(t *testing.T) {
	t.Parallel()

	s := NewSelector(ScopeProjectRead, "proj_123")
	require.Equal(t, Selector{"resource_kind": "project", "resource_id": "proj_123"}, s)
}

func TestNewSelector_wildcardResource(t *testing.T) {
	t.Parallel()

	s := NewSelector(ScopeOrgAdmin, WildcardResource)
	require.Equal(t, Selector{"resource_kind": "org", "resource_id": "*"}, s)
}

func TestNewGrant_combinesScopeAndSelector(t *testing.T) {
	t.Parallel()

	g := NewGrant(ScopeMCPConnect, "tool_a")
	require.Equal(t, ScopeMCPConnect, g.Scope)
	require.Equal(t, Selector{"resource_kind": "mcp", "resource_id": "tool_a"}, g.Selector)
}

func TestValidateSelector_mcpProjectIDAllowed(t *testing.T) {
	t.Parallel()

	sel := Selector{"resource_kind": "mcp", "resource_id": "*", "project_id": "proj_123"}
	require.NoError(t, ValidateSelector(ScopeMCPConnect, sel))
	require.NoError(t, ValidateSelector(ScopeMCPRead, sel))
	require.NoError(t, ValidateSelector(ScopeMCPWrite, sel))
}

func TestValidateSelector_riskPolicyAllowsServerURL(t *testing.T) {
	t.Parallel()

	valid := Selector{"resource_kind": ResourceKindRiskPolicy, "resource_id": "policy_123"}
	require.NoError(t, ValidateSelector(ScopeRiskPolicyEvaluate, valid))

	withServerURL := Selector{"resource_kind": ResourceKindRiskPolicy, "resource_id": "policy_123", "server_url": "https://api.example.com"}
	require.NoError(t, ValidateSelector(ScopeRiskPolicyBypass, withServerURL))
	withServerIdentity := Selector{"resource_kind": ResourceKindRiskPolicy, "resource_id": "policy_123", "server_identity": "github"}
	require.NoError(t, ValidateSelector(ScopeRiskPolicyBypass, withServerIdentity))
	withHostOnlyServerURL := Selector{"resource_kind": ResourceKindRiskPolicy, "resource_id": "policy_123", "server_url": "api.example.com"}
	require.ErrorContains(t, ValidateSelector(ScopeRiskPolicyBypass, withHostOnlyServerURL), "must include URI scheme and host")

	withExtraKey := Selector{"resource_kind": ResourceKindRiskPolicy, "resource_id": "policy_123", "tool": "search"}
	require.ErrorContains(t, ValidateSelector(ScopeRiskPolicyEvaluate, withExtraKey), "not allowed")
}

func TestRiskPolicyBypassCheck_injectsServerURL(t *testing.T) {
	t.Parallel()

	check := RiskPolicyBypassCheck("policy_123", RiskPolicyDimensions{ServerURL: "https://api.example.com", ServerIdentity: ""})
	require.Equal(t, ScopeRiskPolicyBypass, check.Scope)
	require.Equal(t, "policy_123", check.ResourceID)
	require.Equal(t, "https://api.example.com", check.Dimensions[SelectorKeyServerURL])
}

func TestRiskPolicyBypassCheck_injectsServerIdentity(t *testing.T) {
	t.Parallel()

	check := RiskPolicyBypassCheck("policy_123", RiskPolicyDimensions{ServerURL: "", ServerIdentity: "github"})
	require.Equal(t, ScopeRiskPolicyBypass, check.Scope)
	require.Equal(t, "policy_123", check.ResourceID)
	require.Equal(t, "github", check.Dimensions[SelectorKeyServerIdentity])
}

func TestRiskPolicyBypassCheck_emptyServerURLOmitsDimension(t *testing.T) {
	t.Parallel()

	check := RiskPolicyBypassCheck("policy_123", RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""})
	require.Nil(t, check.Dimensions)
}

func TestSelector_Matches_riskPolicyServerURL(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_kind": "risk_policy", "resource_id": "policy_123", "server_url": "https://api.example.com"}
	require.True(t, grant.StrictMatches(Selector{"resource_kind": "risk_policy", "resource_id": "policy_123", "server_url": "https://api.example.com"}))
	require.False(t, grant.StrictMatches(Selector{"resource_kind": "risk_policy", "resource_id": "policy_123", "server_url": "https://api.other.com"}))
}

func TestMCPCheck_injectsProjectID(t *testing.T) {
	t.Parallel()

	check := MCPServer(MCPServerIDFrom("server_456"), ProjectIDFrom("proj_123")).Read()
	require.Equal(t, ScopeMCPRead, check.Scope)
	require.Equal(t, "server_456", check.ResourceID)
	require.Equal(t, "proj_123", check.Dimensions[SelectorKeyProjectID])
}

func TestValidateSelector_ProjectBoundsHooksIngestOnly(t *testing.T) {
	t.Parallel()

	bounded := func(scope Scope) Selector {
		selector := NewSelector(scope, "org-123")
		selector[SelectorKeyProjectID] = "project-1"
		return selector
	}
	require.NoError(t, ValidateSelector(ScopeOrgHooksIngest, bounded(ScopeOrgHooksIngest)))
	require.Error(t, ValidateSelector(ScopeOrgDeviceAgentSync, bounded(ScopeOrgDeviceAgentSync)), "project_id must not read as a restriction on scopes not checked by project")
	require.Error(t, ValidateSelector(ScopeOrgRead, bounded(ScopeOrgRead)))
}

func TestHooksIngestCheck_MatchesOnlyItsProjectBound(t *testing.T) {
	t.Parallel()

	selector := NewSelector(ScopeOrgHooksIngest, "org-123")
	selector[SelectorKeyProjectID] = "project-1"
	bounded := []Grant{NewGrantWithSelector(ScopeOrgHooksIngest, selector)}
	unbounded := []Grant{NewGrant(ScopeOrgHooksIngest, "org-123")}

	require.True(t, GrantsSatisfy(bounded, Org(OrgIDFrom("org-123")).HooksIngest(ProjectIDFrom("project-1"))))
	require.False(t, GrantsSatisfy(bounded, Org(OrgIDFrom("org-123")).HooksIngest(ProjectIDFrom("project-2"))))
	require.True(t, GrantsSatisfy(unbounded, Org(OrgIDFrom("org-123")).HooksIngest(ProjectIDFrom("project-2"))), "a grant naming no project covers every project")
}

func TestGrantsContainSelectorWildcards(t *testing.T) {
	t.Parallel()
	broad := NewGrant(ScopeMCPConnect, "*")
	candidate := NewSelector(ScopeMCPConnect, "*")
	require.True(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	require.False(t, GrantsContainSelector([]Grant{NewGrant(ScopeMCPConnect, "one-server")}, ScopeMCPConnect, candidate))
	require.False(t, GrantsContainSelector([]Grant{NewGrant(ScopeMCPWrite, "*")}, ScopeMCPConnect, candidate), "mcp:write does not contain mcp:connect")
	broad.Selector[SelectorKeyTool] = "allowed-tool"
	require.False(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	candidate[SelectorKeyTool] = "allowed-tool"
	require.True(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	candidate[SelectorKeyTool] = "other-tool"
	require.False(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	// Delegation support must not relax runtime's concrete-resource requirement.
	_, err := GrantsAuthorize([]Grant{broad}, mcpCheck(ScopeMCPConnect, "*", "example-project"))
	require.ErrorIs(t, err, ErrInvalidCheck)
}

// A tool or disposition limit is saved on connect rules and connect blocks
// only. Viewing or managing a server names no tool.
func TestValidateSelector_LimitsAreForConnectOnly(t *testing.T) {
	t.Parallel()

	limits := map[string]string{SelectorKeyTool: "search", SelectorKeyDisposition: DispositionReadOnly}
	for key, value := range limits {
		for _, scope := range []Scope{ScopeMCPConnect, ScopeMCPBlockedConnect} {
			t.Run(fmt.Sprintf("%s limited by %s is allowed", scope, key), func(t *testing.T) {
				t.Parallel()
				require.NoError(t, ValidateSelector(scope, limitedSelector(scope, key, value)))
			})
		}
		for _, scope := range []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPBlockedRead, ScopeMCPBlockedWrite} {
			t.Run(fmt.Sprintf("%s limited by %s is refused", scope, key), func(t *testing.T) {
				t.Parallel()
				require.ErrorContains(t, ValidateSelector(scope, limitedSelector(scope, key, value)), "is not allowed for scope")
			})
		}
	}
}

// TestOpenKeys_Golden lists every grant key a check may leave out, by scope and
// kind of check. Opening a key loosens matching for every check of that kind,
// so a change to the registry's Open lists must change this list too, where a
// reviewer sees it.
func TestOpenKeys_Golden(t *testing.T) {
	t.Parallel()

	// No project_id: a type's parent key is never opened. org:hooks_ingest
	// declares project_id itself, and org has no parent.
	project := []string{SelectorKeyProjectID}
	server := []string{SelectorKeyServerURL, SelectorKeyServerIdentity}
	// A list of entries, not a map literal, so the list names only the
	// scopes that open a key.
	entries := []struct {
		scope Scope
		keys  openKeys
	}{
		{ScopeOrgHooksIngest, openKeys{Probe: project, Connection: nil}},
		{ScopeRiskPolicyBlock, openKeys{Probe: server, Connection: nil}},
		{ScopeRiskPolicyBypass, openKeys{Probe: server, Connection: nil}},
		{ScopeMCPConnect, openKeys{
			Probe:      []string{SelectorKeyTool, SelectorKeyDisposition},
			Connection: []string{SelectorKeyTool, SelectorKeyDisposition},
		}},
	}
	want := make(map[Scope]openKeys, len(entries))
	for _, e := range entries {
		want[e.scope] = e.keys
	}
	require.Equal(t, want, scopeOpenKeys)
}

// Auth clients, blocks and root open nothing: every check on them carries
// every key a grant names, probes included.
func TestOpenKeys_NoneOnAuthClientsBlocksOrRoot(t *testing.T) {
	t.Parallel()

	require.NotContains(t, scopeOpenKeys, ScopeMCPAuth)
	require.NotContains(t, scopeOpenKeys, ScopeRoot)
	for scope, exclusion := range scopeExclusions {
		if exclusion == "" || exclusion == ScopeRiskPolicyBypass {
			continue
		}
		require.NotContains(t, scopeOpenKeys, exclusion, "block of %s", scope)
	}
}

// An exact check opens nothing, even a probe of a scope that opens keys.
func TestOpenKeys_ExactCheckOpensNothing(t *testing.T) {
	t.Parallel()

	probe := Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: "server-1", Dimensions: nil, exact: false, representedUser: "", probe: true, anyOf: nil, operation: operationDefault}
	require.True(t, probe.mayOmit(SelectorKeyTool))
	require.False(t, probe.mayOmit(SelectorKeyProjectID))
	probe.exact = true
	require.False(t, probe.mayOmit(SelectorKeyTool))
}

func TestDispositionFromHints(t *testing.T) {
	t.Parallel()

	yes, no := new(bool), new(bool)
	*yes = true

	for name, tc := range map[string]struct {
		readOnly, destructive, idempotent, openWorld *bool
		want                                         string
		conflicting                                  bool
	}{
		"no annotations":            {nil, nil, nil, nil, DispositionUnannotated, false},
		"every hint false":          {no, no, no, no, DispositionUnannotated, false},
		"read-only":                 {yes, no, no, no, DispositionReadOnly, false},
		"destructive":               {nil, yes, nil, nil, DispositionDestructive, false},
		"read-only and destructive": {yes, yes, nil, nil, DispositionDestructive, true},
		"read-only and idempotent":  {yes, nil, yes, nil, DispositionReadOnly, false},
		"idempotent":                {nil, no, yes, yes, DispositionIdempotent, false},
		"open-world":                {nil, nil, nil, yes, DispositionOpenWorld, false},
	} {
		got, conflicting := DispositionFromHints(tc.readOnly, tc.destructive, tc.idempotent, tc.openWorld)
		require.Equal(t, tc.want, got, name)
		require.Equal(t, tc.conflicting, conflicting, name)
	}
}

// A grant key the check does not carry never matches, except where the
// constructor says the omission is meant: a connection, which names no tool,
// and a probe.
func TestMatching_MissingKeyNeverMatches(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	project := uuid.New()
	grants := searchOnlyConnect(server)
	ref := MCPServer(MCPServerIDOf(server), ProjectIDOf(project))

	for name, check := range map[string]Check{
		"generated connection":   ref.Connect(),
		"constructed connection": mcpCheck(ScopeMCPConnect, server.String(), project.String()),
		"probe of a tool call":   ref.ToolCall("", DispositionReadOnly).Probe(),
	} {
		allowed, err := GrantsAuthorize(grants, check)
		require.NoError(t, err, name)
		require.True(t, allowed, name)
	}

	for name, check := range map[string]Check{
		"tool call without a tool": ref.ToolCall("", DispositionReadOnly),
		"connect check not built as a connection": {
			Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: server.String(),
			Dimensions: map[string]string{SelectorKeyProjectID: project.String()},
		},
		"connection matched exactly": func() Check { c := ref.Connect(); c.exact = true; return c }(),
	} {
		allowed, err := GrantsAuthorize(grants, check)
		require.NoError(t, err, name)
		require.False(t, allowed, name)
	}
}

// Only the connect permission has connections.
func TestMatching_OmissionStampsBelongToTheirPermission(t *testing.T) {
	t.Parallel()

	read := MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Read()
	require.NoError(t, validateInput(read))
	require.ErrorIs(t, validateInput(read.asConnection()), ErrInvalidCheck)
}

// A policy's audience is the same on every server, so an evaluate grant
// cannot name one. A bypass limited to one server exempts only requests to
// that server.
func TestMatching_RiskPolicyAudienceHasNoServerLimit(t *testing.T) {
	t.Parallel()

	audience := NewSelector(ScopeRiskPolicyEvaluate, "policy-1")
	limited := maps.Clone(audience)
	limited[SelectorKeyServerURL] = "https://one.example.com"
	require.ErrorContains(t, ValidateSelector(ScopeRiskPolicyEvaluate, limited), "not allowed")
	bypass := NewSelector(ScopeRiskPolicyBypass, "policy-1")
	bypass[SelectorKeyServerURL] = "https://one.example.com"
	grants := []Grant{NewGrantWithSelector(ScopeRiskPolicyEvaluate, audience), NewGrantWithSelector(ScopeRiskPolicyBypass, bypass)}

	application, err := EvaluateRiskPolicy(grants, "policy-1", RiskPolicyDimensions{ServerURL: "https://two.example.com", ServerIdentity: ""})
	require.NoError(t, err)
	require.Equal(t, RiskPolicyGoverns, application)

	application, err = EvaluateRiskPolicy(grants, "policy-1", RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""})
	require.NoError(t, err)
	require.Equal(t, RiskPolicyGoverns, application)

	application, err = EvaluateRiskPolicy(grants, "policy-1", RiskPolicyDimensions{ServerURL: "https://one.example.com", ServerIdentity: ""})
	require.NoError(t, err)
	require.Equal(t, RiskPolicyExempt, application)
}

// A refusal caused only by a key the check does not carry is logged with the
// key, so a call site that should carry it shows up. Other refusals, and
// allowed checks, log nothing.
func TestMatching_MissingKeyRefusalIsLogged(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	project := uuid.New()
	ref := MCPServer(MCPServerIDOf(server), ProjectIDOf(project))

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), searchOnlyConnect(server))

	requireForbidden(t, engine.Require(ctx, ref.ToolCall("", DispositionReadOnly)))
	require.Contains(t, logs.String(), missingKeyLog)
	require.Contains(t, logs.String(), SelectorKeyTool)

	logs.Reset()
	requireForbidden(t, engine.RequireAny(ctx, ref.ToolCall("", DispositionReadOnly)))
	require.Contains(t, logs.String(), missingKeyLog)

	logs.Reset()
	require.NoError(t, engine.Require(ctx, ref.Connect()))
	require.NoError(t, engine.Require(ctx, ref.ToolCall("search", DispositionReadOnly)))
	requireForbidden(t, engine.Require(ctx, ref.ToolCall("delete_all", DispositionDestructive)))
	require.NotContains(t, logs.String(), missingKeyLog)
}

// Only a policy that refuses the check explains the refusal: a missing key on
// a grant in a policy that allows the check is not why it was refused.
func TestMatching_MissingKeysComeFromRefusingPolicies(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	project := uuid.New()
	call := MCPServer(MCPServerIDOf(server), ProjectIDOf(project)).ToolCall("", DispositionReadOnly)
	otherServer := []Grant{NewGrant(ScopeMCPConnect, uuid.NewString())}
	allowing := append([]Grant{NewGrant(ScopeMCPConnect, server.String())}, searchOnlyConnect(server)...)

	refusedElsewhere := grantAuthorization{policies: [][]Grant{otherServer, allowing}, representedUser: "", representedPolicies: nil, legacyAuthCeiling: false}
	require.Empty(t, refusedElsewhere.keysMissingFrom(call))

	refusedForTheKey := grantAuthorization{policies: [][]Grant{searchOnlyConnect(server), allowing}, representedUser: "", representedPolicies: nil, legacyAuthCeiling: false}
	require.Equal(t, []string{SelectorKeyTool}, refusedForTheKey.keysMissingFrom(call))
}

// RequireAny still reports a missing key on an alternative that no block
// refused, even when a block refused another one.
func TestMatching_RequireAnyLogsUnblockedAlternatives(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	blockedServer := uuid.New()
	project := uuid.New()
	grants := append(searchOnlyConnect(server),
		NewGrant(ScopeMCPConnect, blockedServer.String()),
		NewGrant(ScopeMCPBlockedConnect, blockedServer.String()),
	)

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), grants)

	requireForbidden(t, engine.RequireAny(ctx,
		MCPServer(MCPServerIDOf(blockedServer), ProjectIDOf(project)).Connect(),
		MCPServer(MCPServerIDOf(server), ProjectIDOf(project)).ToolCall("", DispositionReadOnly),
	))
	require.Contains(t, logs.String(), missingKeyLog)
}

func limitedSelector(scope Scope, key, value string) Selector {
	selector := NewSelector(scope, "server-one")
	selector[key] = value
	return selector
}

const missingKeyLog = "authz check refused: a grant names a key the check does not carry"

// searchOnlyConnect is a connect grant on server limited to the search tool.
func searchOnlyConnect(server uuid.UUID) []Grant {
	selector := NewSelector(ScopeMCPConnect, server.String())
	selector[SelectorKeyTool] = "search"
	return []Grant{NewGrantWithSelector(ScopeMCPConnect, selector)}
}

func TestValidateSelector_refusesNilResourceID(t *testing.T) {
	t.Parallel()

	require.ErrorContains(t, ValidateSelector(ScopeProjectRead, NewSelector(ScopeProjectRead, "00000000-0000-0000-0000-000000000000")), "nil UUID")
	require.ErrorContains(t, ValidateSelector(ScopeMCPConnect, NewSelector(ScopeMCPConnect, "00000000-0000-0000-0000-000000000000")), "nil UUID")
	require.NoError(t, ValidateSelector(ScopeProjectRead, NewSelector(ScopeProjectRead, WildcardResource)))
}
