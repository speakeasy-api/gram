package authz

import (
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ownershipDeclarations are the chat and agent types of the RFC, declared
// only for these tests. They exercise guarded authorship and one agent-owner
// hop without changing any production table.
func ownershipDeclarations() []s.TypeDecl {
	return []s.TypeDecl{
		s.NewType("agent", s.WrittenBy("access"),
			s.Attr("owner", s.FactUser),
			s.Attr("deleted", s.FactBool),
			s.Attr("owner_reassignment_required_at", s.FactNullableTimestamp),
			s.Guard("live", s.All(s.IsFalse("deleted"), s.IsNull("owner_reassignment_required_at"))),
			s.Grant("reader", s.StoredAs("agent:read"), s.Describe("Read agents")),
			s.Permission("read", s.Rel("reader"), s.Checks("agent:read")),
			s.Permission("is_owner", s.When("live", s.Is("owner"))),
		),
		s.NewType("chat", s.WrittenBy("access"),
			s.Attr("internal_user_id", s.FactNullableString),
			s.Attr("external_user_id", s.FactNullableString),
			s.Attr("agent", "agent"),
			s.Derived("author", s.NonEmptyPrincipal(s.FactUser, "internal_user_id")),
			s.Guard("agent_only", s.All(
				s.IsNull("internal_user_id"),
				s.NullOrEmpty("external_user_id"),
				s.Present("agent"),
			)),
			s.Grant("reader", s.StoredAs("chat:read"), s.Describe("Read session transcripts")),
			s.Grant("writer", s.StoredAs("chat:write"), s.Describe("Change sessions")),
			s.Permission("read_grant", s.Union(s.Rel("reader"), s.Rel("writer")), s.Checks("chat:read")),
			s.Permission("write_grant", s.Rel("writer"), s.Checks("chat:write")),
			s.Permission("read", s.Union(
				s.Rel("reader"), s.Rel("writer"), s.Is("author"),
				s.When("agent_only", s.Via("agent", "is_owner")),
			)),
			s.Permission("write", s.Union(s.Rel("writer"), s.Is("author"))),
			s.Permission("read_author_last", s.Union(s.Rel("reader"), s.Is("author"))),
			s.Permission("read_author_first", s.Union(s.Is("author"), s.Rel("reader"))),
		),
	}
}

func ownershipEvaluator(t *testing.T) *RegistryEvaluator {
	t.Helper()
	reg, err := s.Compile(s.Config{Roles: nil, WriteSurfaces: []string{"access"}, Conditions: nil, RetiredRuntimeScopes: nil}, ownershipDeclarations()...)
	require.NoError(t, err)
	evaluator, err := NewRegistryEvaluator(reg)
	require.NoError(t, err)
	return evaluator
}

var (
	humanActor = urn.NewPrincipal(urn.PrincipalTypeUser, "user_1")
	agentActor = urn.NewPrincipal(urn.PrincipalTypeAgent, fixtureAgentID)
)

// liveAgent is an agent owned by owner with no deletion or reassignment latch.
func liveAgent(owner string) Fact {
	return RelatedFact(RelatedResource{ID: "agent_1", Dimensions: nil, Facts: map[string]Fact{
		"owner":                          StringFact(owner),
		"deleted":                        BoolFact(false),
		"owner_reassignment_required_at": AbsentFact(),
	}})
}

func chatFacts(internal, external, agent Fact) map[string]Fact {
	return map[string]Fact{"internal_user_id": internal, "external_user_id": external, "agent": agent}
}

func chatRequest(permission string, facts map[string]Fact) PermissionRequest {
	return PermissionRequest{
		Permission:      s.PermissionRef{Type: "chat", Permission: permission},
		ResourceID:      "chat_1",
		Dimensions:      nil,
		StrictMatch:     false,
		RepresentedUser: "",
		Actor:           urn.Principal{},
		Facts:           facts,
		RequiredFacts:   nil,
	}
}

func humanSet(grants ...Grant) PolicySet {
	return PolicySet{
		Actor:           humanActor,
		Policies:        []Policy{{Principal: humanActor.String(), Grants: grants}},
		Captured:        nil,
		RepresentedUser: humanActor.ID,
		Represented:     nil,
	}
}

func TestChatOwnershipFacts(t *testing.T) {
	t.Parallel()

	evaluator := ownershipEvaluator(t)
	errLookup := errors.New("lookup failed")
	reader := grantOf("role:organization:role_1", "chat:read", "chat", "*")
	reassigned := RelatedFact(RelatedResource{ID: "agent_1", Dimensions: nil, Facts: map[string]Fact{
		"owner": StringFact("user_1"), "deleted": BoolFact(false), "owner_reassignment_required_at": PresentFact(),
	}})
	deleted := RelatedFact(RelatedResource{ID: "agent_1", Dimensions: nil, Facts: map[string]Fact{
		"owner": StringFact("user_1"), "deleted": BoolFact(true), "owner_reassignment_required_at": AbsentFact(),
	}})

	cases := []struct {
		name       string
		permission string
		facts      map[string]Fact
		grants     []Grant
		want       string
	}{
		{"author reads without a grant", "read", chatFacts(StringFact("user_1"), AbsentFact(), AbsentFact()), nil, "allow"},
		{"author writes without a grant", "write", chatFacts(StringFact("user_1"), AbsentFact(), AbsentFact()), nil, "allow"},
		{"other author denies", "read", chatFacts(StringFact("user_2"), AbsentFact(), AbsentFact()), nil, "deny"},
		{"empty internal user is no author and not agent-only", "read", chatFacts(StringFact(""), AbsentFact(), liveAgent("user_1")), nil, "deny"},
		{"agent-only chat reads through the agent owner", "read", chatFacts(AbsentFact(), AbsentFact(), liveAgent("user_1")), nil, "allow"},
		{"empty external user still counts as agent-only", "read", chatFacts(AbsentFact(), StringFact(""), liveAgent("user_1")), nil, "allow"},
		{"external attribution keeps the chat the external user's", "read", chatFacts(AbsentFact(), StringFact("ext_1"), liveAgent("user_1")), nil, "deny"},
		{"owning the agent permits no mutation", "write", chatFacts(AbsentFact(), AbsentFact(), liveAgent("user_1")), nil, "deny"},
		{"another owner's agent denies", "read", chatFacts(AbsentFact(), AbsentFact(), liveAgent("user_2")), nil, "deny"},
		{"reassignment latch denies", "read", chatFacts(AbsentFact(), AbsentFact(), reassigned), nil, "deny"},
		{"deleted agent denies", "read", chatFacts(AbsentFact(), AbsentFact(), deleted), nil, "deny"},
		{"known-absent agent denies", "read", chatFacts(AbsentFact(), AbsentFact(), AbsentFact()), nil, "deny"},
		{"false guard skips an unloaded agent", "read", chatFacts(StringFact("user_2"), AbsentFact(), Fact{}), nil, "deny"},
		{"unresolved guard is not absence", "read", chatFacts(Fact{}, AbsentFact(), AbsentFact()), nil, "error"},
		{"failed agent lookup errors without a grant", "read", chatFacts(AbsentFact(), AbsentFact(), LoadFailedFact(errLookup)), nil, "error"},
		{"failed agent lookup is skipped when a grant allows", "read", chatFacts(AbsentFact(), AbsentFact(), LoadFailedFact(errLookup)), []Grant{reader}, "allow"},
		{"cross-tenant agent errors", "read", chatFacts(AbsentFact(), AbsentFact(), InvalidFact(errors.New("agent belongs to another organization"))), nil, "error"},
		{"author before grant: unresolved fact errors without a grant", "read_author_first", chatFacts(Fact{}, AbsentFact(), AbsentFact()), nil, "error"},
		{"author after grant: unresolved fact errors without a grant", "read_author_last", chatFacts(Fact{}, AbsentFact(), AbsentFact()), nil, "error"},
		{"author before grant: grant allows past an unresolved fact", "read_author_first", chatFacts(Fact{}, AbsentFact(), AbsentFact()), []Grant{reader}, "allow"},
		{"author after grant: grant allows past an unresolved fact", "read_author_last", chatFacts(Fact{}, AbsentFact(), AbsentFact()), []Grant{reader}, "allow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decision, err := evaluator.Decide(humanSet(tc.grants...), chatRequest(tc.permission, tc.facts))
			switch tc.want {
			case "error":
				require.ErrorIs(t, err, ErrFactUnresolved)
			case "allow":
				require.NoError(t, err)
				require.True(t, decision.Allowed)
			default:
				require.NoError(t, err)
				require.False(t, decision.Allowed)
			}
		})
	}
}

func TestRequiredFactsKeepEagerErrors(t *testing.T) {
	t.Parallel()

	evaluator := ownershipEvaluator(t)
	req := chatRequest("read", chatFacts(AbsentFact(), AbsentFact(), LoadFailedFact(errors.New("lookup failed"))))
	req.RequiredFacts = []string{"agent"}
	_, err := evaluator.Decide(humanSet(grantOf("role:organization:role_1", "chat:read", "chat", "*")), req)
	require.ErrorIs(t, err, ErrFactUnresolved, "an eager lookup error survives a grant that would allow")
}

func TestMachineActorNeverUsesOwnerAuthorship(t *testing.T) {
	t.Parallel()

	evaluator := ownershipEvaluator(t)
	set := PolicySet{
		Actor: agentActor,
		Policies: []Policy{
			{Principal: "credential", Grants: []Grant{grantOf("role:organization:role_1", "agent:read", "agent", "*")}},
			{Principal: "agent", Grants: nil},
			{Principal: "owner:user_1", Grants: nil},
		},
		Captured:        nil,
		RepresentedUser: "user_1",
		Represented:     nil,
	}
	for name, facts := range map[string]map[string]Fact{
		"owner-authored chat":       chatFacts(StringFact("user_1"), AbsentFact(), AbsentFact()),
		"owner's agent-only chat":   chatFacts(AbsentFact(), AbsentFact(), liveAgent("user_1")),
		"chat attributed to itself": chatFacts(StringFact(fixtureAgentID), AbsentFact(), AbsentFact()),
	} {
		decision, err := evaluator.Decide(set, chatRequest("read", facts))
		require.NoError(t, err, name)
		require.False(t, decision.Allowed, name)
	}
}

func TestPolicyComposition(t *testing.T) {
	t.Parallel()

	evaluator := ownershipEvaluator(t)
	role := "role:organization:role_1"
	authored := chatFacts(StringFact("user_2"), AbsentFact(), AbsentFact())

	t.Run("different grants may satisfy the same complete permission", func(t *testing.T) {
		t.Parallel()
		set := humanSet(grantOf(role, "chat:read", "chat", "*"))
		set.Policies = append(set.Policies, Policy{Principal: "second", Grants: []Grant{grantOf(role, "chat:write", "chat", "*")}})
		decision, err := evaluator.Decide(set, chatRequest("read", authored))
		require.NoError(t, err)
		require.True(t, decision.Allowed)
	})

	t.Run("every policy must allow", func(t *testing.T) {
		t.Parallel()
		set := humanSet(grantOf(role, "chat:read", "chat", "*"))
		set.Policies = append(set.Policies, Policy{Principal: "empty", Grants: nil})
		decision, err := evaluator.Decide(set, chatRequest("read", authored))
		require.NoError(t, err)
		require.False(t, decision.Allowed)
		require.Equal(t, "empty", decision.Policy)
	})

	t.Run("captured boundary survives a broader refresh", func(t *testing.T) {
		t.Parallel()
		set := humanSet(grantOf(role, "chat:read", "chat", "*"))
		set.Captured = []Policy{{Principal: "captured", Grants: []Grant{grantOf(role, "chat:read", "chat", "chat_other")}}}
		decision, err := evaluator.Decide(set, chatRequest("read", authored))
		require.NoError(t, err)
		require.False(t, decision.Allowed)
		require.Equal(t, "captured", decision.Policy)
	})

	t.Run("no admitted policy errors", func(t *testing.T) {
		t.Parallel()
		set := humanSet()
		set.Policies = nil
		_, err := evaluator.Decide(set, chatRequest("read", authored))
		require.ErrorIs(t, err, ErrNoPolicies)
	})

	t.Run("missing actor errors", func(t *testing.T) {
		t.Parallel()
		set := humanSet(grantOf(role, "chat:read", "chat", "*"))
		set.Actor = urn.Principal{}
		_, err := evaluator.Decide(set, chatRequest("read", authored))
		require.ErrorIs(t, err, ErrMissingActor)
	})

	t.Run("the set's actor replaces a claimed one", func(t *testing.T) {
		t.Parallel()
		req := chatRequest("read", authored)
		req.Actor = urn.NewPrincipal(urn.PrincipalTypeUser, "user_2")
		decision, err := evaluator.Decide(humanSet(), req)
		require.NoError(t, err)
		require.False(t, decision.Allowed)
	})

	t.Run("undeclared and derived facts are rejected", func(t *testing.T) {
		t.Parallel()
		for _, facts := range []map[string]Fact{
			{"internal_user": StringFact("user_1")},
			{"author": StringFact("user_1")},
		} {
			_, err := evaluator.Decide(humanSet(), chatRequest("read", facts))
			require.Error(t, err)
		}
	})
}

func TestAlternativesNeedOneCompleteOptionInEveryPolicy(t *testing.T) {
	t.Parallel()

	reg := compileEvaluatorSlice(t)
	evaluator, err := NewRegistryEvaluator(reg)
	require.NoError(t, err)
	role := "role:organization:role_1"
	publish := pluginPublicationOptions("proj_1", fixtureOrgID)

	set := PolicySet{
		Actor: agentActor,
		Policies: []Policy{
			{Principal: "credential", Grants: []Grant{grantOf(role, string(ScopePluginWrite), ResourceKindProject, "proj_1")}},
			{Principal: "agent", Grants: []Grant{grantOf(role, string(ScopeOrgAdmin), ResourceKindOrg, fixtureOrgID)}},
		},
		Captured:        nil,
		RepresentedUser: "",
		Represented:     nil,
	}
	decision, err := evaluator.DecideAlternatives(set, pluginPublication, publish)
	require.NoError(t, err)
	require.False(t, decision.Allowed, "one policy allowing A and another allowing B is not one complete alternative")

	set.Policies[1].Grants = append(set.Policies[1].Grants, grantOf(role, string(ScopePluginWrite), ResourceKindProject, "*"))
	decision, err = evaluator.DecideAlternatives(set, pluginPublication, publish)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Equal(t, 0, decision.Option)

	_, err = evaluator.DecideAlternatives(set, pluginPublication, publish[:1])
	require.Error(t, err)
}

var pluginPublication = s.PermissionRef{Type: "project", Permission: "plugin_publication"}

func pluginPublicationOptions(projectID, organizationID string) []PermissionRequest {
	option := func(typeName, permission, id string) PermissionRequest {
		return PermissionRequest{
			Permission:      s.PermissionRef{Type: typeName, Permission: permission},
			ResourceID:      id,
			Dimensions:      nil,
			StrictMatch:     false,
			RepresentedUser: "",
			Actor:           urn.Principal{},
			Facts:           nil,
			RequiredFacts:   nil,
		}
	}
	return []PermissionRequest{
		option("project", "publish_plugin", projectID),
		option("organization", "administer", organizationID),
	}
}

// policyCorpusSkips lists every selected fixture the composed evaluator does
// not run, and why.
var policyCorpusSkips = map[string]string{}

// TestPolicyCompositionFoundationCorpus runs the hand-written frozen
// decisions for credential, API key and workload callers, and the
// RequireAnyUnblocked veto, through Decide and DecideAlternatives. The
// policies come from the same fixture fields admission reads today.
func TestPolicyCompositionFoundationCorpus(t *testing.T) {
	t.Parallel()

	reg := compileEvaluatorSlice(t)
	evaluator, err := NewRegistryEvaluator(reg)
	require.NoError(t, err)

	paths, err := filepath.Glob(filepath.Join(foundationCompatibilityDir, "*.json"))
	require.NoError(t, err)
	machine := map[string]bool{"agent_credential": true, "principal_api_key": true, "workload": true, "credential_unadmitted": true}
	ran := 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var file foundationFixtureFile
		require.NoError(t, json.Unmarshal(raw, &file), path)
		for _, fixture := range file.Cases {
			selected := (machine[fixture.Caller] && (fixture.Operation == "require" || fixture.Operation == "require_any")) ||
				(fixture.Caller == "human" && fixture.Operation == "require_any_unblocked")
			if !selected {
				continue
			}
			if _, skip := policyCorpusSkips[fixture.Name]; skip {
				continue
			}
			ran++
			t.Run(fixture.Name, func(t *testing.T) {
				t.Parallel()
				runPolicyCorpusCase(t, reg, evaluator, fixture)
			})
		}
	}
	require.Equal(t, 19, ran, "the selected frozen corpus changed; update the expected count after checking every new case runs")
}

func corpusPolicySet(f foundationFixture) PolicySet {
	if f.Caller == "human" {
		set := humanSet(foundationGrants(f.Grants)...)
		set.Actor = urn.NewPrincipal(urn.PrincipalTypeUser, fixtureUserID)
		set.RepresentedUser = fixtureUserID
		return set
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, fixtureAgentID)
	if f.Caller == "workload" {
		actor = urn.NewPrincipal(urn.PrincipalTypeWorkload, "workload_fixture")
	}
	set := PolicySet{Actor: actor, Policies: nil, Captured: nil, RepresentedUser: "", Represented: nil}
	if f.Policies == nil {
		return set
	}
	set.Policies = []Policy{
		{Principal: "credential", Grants: foundationGrants(f.Policies.Credential)},
		{Principal: "agent", Grants: foundationGrants(f.Policies.Agent)},
		{Principal: "owner", Grants: foundationGrants(f.Policies.Owner)},
	}
	if f.Caller != "workload" {
		set.RepresentedUser = cmp.Or(f.Policies.RepresentedUser, fixtureUserID)
		if f.Policies.Represented != nil {
			set.Represented = []Policy{{Principal: "represented", Grants: foundationGrants(f.Policies.Represented)}}
		}
	}
	return set
}

func runPolicyCorpusCase(t *testing.T, reg *s.Registry, evaluator *RegistryEvaluator, f foundationFixture) {
	t.Helper()

	set := corpusPolicySet(f)
	requests := make([]PermissionRequest, 0, len(f.Checks))
	for _, c := range f.Checks {
		ref, ok := reg.PermissionForCheck(string(c.Scope))
		require.True(t, ok, "no compiled permission checks %s", c.Scope)
		requests = append(requests, PermissionRequest{
			Permission:      ref,
			ResourceID:      c.ResourceID,
			Dimensions:      c.Dimensions,
			StrictMatch:     c.Strict,
			RepresentedUser: "",
			Actor:           urn.Principal{},
			Facts:           nil,
			RequiredFacts:   nil,
		})
	}

	var allowed bool
	var evalErr error
	switch f.Operation {
	case "require_any_unblocked":
		decision, err := evaluator.DecideAlternatives(set, pluginPublication, requests)
		allowed, evalErr = decision.Allowed, err
	case "require":
		allowed = true
		for _, req := range requests {
			decision, err := evaluator.Decide(set, req)
			if err != nil {
				evalErr = err
				break
			}
			allowed = allowed && decision.Allowed
		}
	case "require_any":
		for _, req := range requests {
			decision, err := evaluator.Decide(set, req)
			if err != nil {
				evalErr = err
				break
			}
			allowed = allowed || decision.Allowed
		}
	default:
		t.Fatalf("unexpected operation %q", f.Operation)
	}

	switch {
	case strings.HasPrefix(f.Want, "error"):
		require.Error(t, evalErr)
	case evalErr != nil:
		t.Fatalf("unexpected error: %v", evalErr)
	default:
		require.Equal(t, f.Want == "allow", allowed, "want %s", f.Want)
	}
}

func TestFactsMustMatchTheirDeclaredKind(t *testing.T) {
	t.Parallel()

	evaluator := ownershipEvaluator(t)
	role := "role:organization:role_1"
	set := humanSet(grantOf(role, "chat:read", "chat", "*"))

	cases := []struct {
		name  string
		facts map[string]Fact
	}{
		{"bare presence for a string", chatFacts(AbsentFact(), PresentFact(), liveAgent("user_1"))},
		{"bool for a string", chatFacts(BoolFact(false), AbsentFact(), AbsentFact())},
		{"string for a relationship", chatFacts(AbsentFact(), AbsentFact(), StringFact("agent_1"))},
		{"bare presence for a related bool", chatFacts(AbsentFact(), AbsentFact(), RelatedFact(RelatedResource{ID: "agent_1", Dimensions: nil, Facts: map[string]Fact{
			"owner":                          StringFact("user_1"),
			"deleted":                        PresentFact(),
			"owner_reassignment_required_at": AbsentFact(),
		}}))},
		{"string for a related timestamp", chatFacts(AbsentFact(), AbsentFact(), RelatedFact(RelatedResource{ID: "agent_1", Dimensions: nil, Facts: map[string]Fact{
			"owner":                          StringFact("user_1"),
			"deleted":                        BoolFact(false),
			"owner_reassignment_required_at": StringFact(""),
		}}))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := evaluator.Decide(set, chatRequest("read", tc.facts))
			require.Error(t, err)
		})
	}
}

func TestRelatedResourcesAreValidatedLikeRequests(t *testing.T) {
	t.Parallel()

	evaluator := ownershipEvaluator(t)
	agentOnly := func(related RelatedResource) map[string]Fact {
		return chatFacts(AbsentFact(), AbsentFact(), RelatedFact(related))
	}
	live := map[string]Fact{
		"owner":                          StringFact(humanActor.ID),
		"deleted":                        BoolFact(false),
		"owner_reassignment_required_at": AbsentFact(),
	}

	t.Run("an unaccepted dimension errors", func(t *testing.T) {
		t.Parallel()
		_, err := evaluator.Decide(humanSet(), chatRequest("read", agentOnly(RelatedResource{ID: "agent_1", Dimensions: map[string]string{"tool": "x"}, Facts: live})))
		require.Error(t, err)
	})

	t.Run("a wildcard target errors", func(t *testing.T) {
		t.Parallel()
		_, err := evaluator.Decide(humanSet(), chatRequest("read", agentOnly(RelatedResource{ID: WildcardResource, Dimensions: nil, Facts: live})))
		require.ErrorIs(t, err, ErrFactUnresolved)
	})

	t.Run("a further relationship errors", func(t *testing.T) {
		t.Parallel()
		reg, err := s.Compile(s.Config{Roles: nil, WriteSurfaces: []string{"access"}, Conditions: nil, RetiredRuntimeScopes: nil},
			s.NewType("folder", s.WrittenBy("access"),
				s.Attr("parent", "folder"),
				s.Attr("owner", s.FactUser),
				s.Grant("reader", s.StoredAs("folder:read"), s.Describe("Read")),
				s.Permission("owned", s.Is("owner")),
				s.Permission("read_grant", s.Rel("reader"), s.Checks("folder:read")),
				s.Permission("read", s.Union(s.Rel("reader"), s.Via("parent", "owned"))),
			))
		require.NoError(t, err)
		folders, err := NewRegistryEvaluator(reg)
		require.NoError(t, err)

		parent := func(facts map[string]Fact) map[string]Fact {
			return map[string]Fact{"parent": RelatedFact(RelatedResource{ID: "folder_0", Dimensions: nil, Facts: facts})}
		}
		req := PermissionRequest{
			Permission:      s.PermissionRef{Type: "folder", Permission: "read"},
			ResourceID:      "folder_1",
			Dimensions:      nil,
			StrictMatch:     false,
			RepresentedUser: "",
			Actor:           urn.Principal{},
			Facts:           parent(map[string]Fact{"owner": StringFact(humanActor.ID)}),
			RequiredFacts:   nil,
		}
		decision, err := folders.Decide(humanSet(), req)
		require.NoError(t, err)
		require.True(t, decision.Allowed, "one hop is followed")

		req.Facts = parent(map[string]Fact{"owner": StringFact("user_2"), "parent": RelatedFact(RelatedResource{ID: "folder_x", Dimensions: nil, Facts: nil})})
		_, err = folders.Decide(humanSet(), req)
		require.Error(t, err)
	})
}

func TestAuthClientPermissionsKeepTheRepresentedCeiling(t *testing.T) {
	t.Parallel()

	t.Run("a bound represented user without policies errors", func(t *testing.T) {
		t.Parallel()
		evaluator, err := NewRegistryEvaluator(compileEvaluatorSlice(t))
		require.NoError(t, err)
		role := "role:organization:role_1"
		set := PolicySet{
			Actor:           agentActor,
			Policies:        []Policy{{Principal: "agent", Grants: []Grant{grantOf(role, string(ScopeMCPAuth), ResourceKindMCPAuthClient, "client_a", SelectorKeyClientType, AuthClientTypeServiceAccount, SelectorKeyProjectID, "proj_1")}}},
			Captured:        nil,
			RepresentedUser: "user_1",
			Represented:     nil,
		}
		req := PermissionRequest{
			Permission:      s.PermissionRef{Type: "mcp_auth_client", Permission: "use"},
			ResourceID:      "client_a",
			Dimensions:      map[string]string{SelectorKeyClientType: AuthClientTypeServiceAccount, SelectorKeyProjectID: "proj_1"},
			StrictMatch:     false,
			RepresentedUser: "",
			Actor:           urn.Principal{},
			Facts:           nil,
			RequiredFacts:   nil,
		}
		_, err = evaluator.Decide(set, req)
		require.ErrorIs(t, err, ErrMissingRepresentedPolicy)

		set.Represented = []Policy{{Principal: "user_1", Grants: nil}}
		decision, err := evaluator.Decide(set, req)
		require.NoError(t, err)
		require.False(t, decision.Allowed)
		require.Equal(t, "user_1", decision.Policy)

		set.Represented[0].Grants = []Grant{grantOf(role, string(ScopeMCPAuth), ResourceKindMCPAuthClient, "client_a", SelectorKeyClientType, AuthClientTypeServiceAccount, SelectorKeyProjectID, "proj_1")}
		decision, err = evaluator.Decide(set, req)
		require.NoError(t, err)
		require.True(t, decision.Allowed)
	})

	t.Run("no permission may reach an auth client through a relationship", func(t *testing.T) {
		t.Parallel()
		reg, err := s.Compile(s.Config{Roles: nil, WriteSurfaces: []string{"access"}, Conditions: nil, RetiredRuntimeScopes: nil},
			s.NewType("client", s.StoredKind(ResourceKindMCPAuthClient), s.WrittenBy("access"),
				s.Grant("user", s.StoredAs("client:use"), s.StrictMatching(), s.Describe("Use")),
				s.Permission("use", s.Rel("user"), s.Checks("client:use")),
			),
			s.NewType("session", s.WrittenBy("access"),
				s.Attr("client", "client"),
				s.Grant("reader", s.StoredAs("session:read"), s.Describe("Read")),
				s.Permission("read_grant", s.Rel("reader"), s.Checks("session:read")),
				s.Permission("read", s.Union(s.Rel("reader"), s.Via("client", "use"))),
			))
		require.NoError(t, err)
		_, err = NewRegistryEvaluator(reg)
		require.Error(t, err)
	})
}

func TestVetoDeniesDespiteAnUndecidedOption(t *testing.T) {
	t.Parallel()

	reg, err := s.Compile(s.Config{Roles: nil, WriteSurfaces: []string{"access"}, Conditions: nil, RetiredRuntimeScopes: nil},
		s.NewType("doc", s.WrittenBy("access"),
			s.Attr("archived", s.FactBool),
			s.Guard("active", s.IsFalse("archived")),
			s.Grant("reader", s.StoredAs("doc:read"), s.Describe("Read")),
			s.Block("read_blocked", s.StoredAs("doc:blocked_read")),
			s.Permission("read_grant", s.Rel("reader"), s.Checks("doc:read")),
			s.Permission("read", s.When("active", s.Rel("reader")), s.ExcludedBy("read_blocked")),
			s.Alternatives("open", s.Alternative("doc", "read"), s.Alternative("folder", "read"), s.VetoBlocks()),
		),
		s.NewType("folder", s.WrittenBy("access"),
			s.Grant("reader", s.StoredAs("folder:read"), s.Describe("Read")),
			s.Block("read_blocked", s.StoredAs("folder:blocked_read")),
			s.Permission("read", s.Rel("reader"), s.Checks("folder:read"), s.ExcludedBy("read_blocked")),
		))
	require.NoError(t, err)
	evaluator, err := NewRegistryEvaluator(reg)
	require.NoError(t, err)

	option := func(typeName, id string) PermissionRequest {
		return PermissionRequest{
			Permission:      s.PermissionRef{Type: typeName, Permission: "read"},
			ResourceID:      id,
			Dimensions:      nil,
			StrictMatch:     false,
			RepresentedUser: "",
			Actor:           urn.Principal{},
			Facts:           nil,
			RequiredFacts:   nil,
		}
	}
	options := []PermissionRequest{option("doc", "doc_1"), option("folder", "folder_1")}
	// The doc block's exception can only be checked with the unloaded
	// archived fact; the folder block stands outright.
	undecided := []Grant{
		grantOf(testRole, "doc:blocked_read", "doc", "doc_1"),
		grantOf(testDirectUser, "doc:read", "doc", "doc_1"),
	}
	set := PolicySet{
		Actor:           urn.NewPrincipal(urn.PrincipalTypeUser, fixtureUserID),
		Policies:        []Policy{{Principal: "first", Grants: undecided}},
		Captured:        nil,
		RepresentedUser: "",
		Represented:     nil,
	}
	ref := s.PermissionRef{Type: "doc", Permission: "open"}

	_, err = evaluator.DecideAlternatives(set, ref, options)
	require.ErrorIs(t, err, ErrFactUnresolved, "an undecided veto with no standing block errors")

	set.Policies = append(set.Policies, Policy{Principal: "second", Grants: []Grant{grantOf(testRole, "folder:blocked_read", "folder", "folder_1")}})
	decision, err := evaluator.DecideAlternatives(set, ref, options)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, PermissionReasonExcluded, decision.Reason)
	require.Equal(t, "second", decision.Policy)
	require.Equal(t, 1, decision.Option)
}
