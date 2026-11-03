package authz

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The foundation compatibility corpus pins today's authorization decisions as
// explicit expectations (docs/rbac-foundation-migration.md). Every expected
// outcome is written by hand in testdata/foundation_compatibility/*.json and
// evaluated against the unchanged Engine and its engine-free helpers. Nothing
// here reads from a new registry, so a broken registry projection cannot make
// both sides agree.
//
// Fixture fields:
//
//	caller     who is asking; see foundationCaller
//	grants     the loaded grant set for human, assistant and embedded callers
//	policies   independent admitted policies for credential and workload callers
//	operation  which entry point decides; see runFoundationCase
//	checks     the checks passed to it, in order
//	want       allow, deny, or error:<oops code>
const (
	fixtureUserID      = "user_fixture"
	fixtureOrgID       = "org_fixture"
	fixtureAgentID     = "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001"
	fixtureAssistantID = "018f8d7b-58d7-7cc4-bb16-9f8c6b99a002"
)

type foundationFixtureFile struct {
	Description string              `json:"description"`
	Cases       []foundationFixture `json:"cases"`
}

type foundationFixture struct {
	Name      string             `json:"name"`
	Contract  string             `json:"contract"`
	Caller    string             `json:"caller"`
	Grants    []foundationGrant  `json:"grants"`
	Policies  *foundationPolicy  `json:"policies"`
	Override  string             `json:"override"`
	Operation string             `json:"operation"`
	Checks    []foundationCheck  `json:"checks"`
	Selector  map[string]string  `json:"selector"`
	Public    bool               `json:"public"`
	Want      string             `json:"want"`
	WantIDs   []string           `json:"want_ids"`
	WantMatch []bool             `json:"want_matched"`
	WantRows  []foundationRow    `json:"want_rows"`
	Writes    []foundationWrite  `json:"writes"`
	Raw       json.RawMessage    `json:"raw"`
	Defaults  map[string][]Scope `json:"defaults"`
}

type foundationGrant struct {
	Principal string            `json:"principal"`
	Scope     Scope             `json:"scope"`
	Selector  map[string]string `json:"selector"`
	Inferred  bool              `json:"inferred"`
}

type foundationPolicy struct {
	Credential      []foundationGrant `json:"credential"`
	Agent           []foundationGrant `json:"agent"`
	Owner           []foundationGrant `json:"owner"`
	RepresentedUser string            `json:"represented_user"`
	Represented     []foundationGrant `json:"represented"`
}

type foundationCheck struct {
	Scope        Scope             `json:"scope"`
	ResourceKind string            `json:"resource_kind"`
	ResourceID   string            `json:"resource_id"`
	Dimensions   map[string]string `json:"dimensions"`
	Strict       bool              `json:"strict"`
}

type foundationWrite struct {
	Scope     string              `json:"scope"`
	Selectors []map[string]string `json:"selectors"`
}

type foundationRow struct {
	Scope    Scope             `json:"scope"`
	Selector map[string]string `json:"selector"`
}

func (g foundationGrant) grant() Grant {
	principal := cmp.Or(g.Principal, "user:"+fixtureUserID)
	return Grant{PrincipalUrn: principal, Scope: g.Scope, Selector: Selector(g.Selector), Inferred: g.Inferred}
}

func foundationGrants(in []foundationGrant) []Grant {
	if in == nil {
		return nil
	}
	out := make([]Grant, 0, len(in))
	for _, g := range in {
		out = append(out, g.grant())
	}
	return out
}

func (c foundationCheck) check() Check {
	check := Check{Scope: c.Scope, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: selectorMatchNormal, representedUser: ""}
	if c.Strict {
		check = check.WithStrictSelectorMatch()
	}
	return check
}

// TestFoundationCompatibilityCorpus runs every fixture against the current
// evaluator. A failure here is a behaviour change: fix the code, or, for an
// approved access change, update the expectation in the same PR and say so.
func TestFoundationCompatibilityCorpus(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join(foundationCompatibilityDir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)

	names := map[string]string{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		var file foundationFixtureFile
		require.NoError(t, decoder.Decode(&file), path)
		require.NotEmpty(t, file.Cases, path)

		for _, fixture := range file.Cases {
			require.NotEmpty(t, fixture.Name, path)
			require.NotEmpty(t, fixture.Contract, "%s: %s has no contract", path, fixture.Name)
			if previous, ok := names[fixture.Name]; ok {
				t.Fatalf("duplicate fixture name %q in %s and %s", fixture.Name, previous, path)
			}
			names[fixture.Name] = path

			t.Run(fixture.Name, func(t *testing.T) {
				t.Parallel()
				runFoundationCase(t, fixture)
			})
		}
	}
}

func runFoundationCase(t *testing.T, f foundationFixture) {
	t.Helper()

	checks := make([]Check, 0, len(f.Checks))
	for _, c := range f.Checks {
		checks = append(checks, c.check())
	}

	switch f.Operation {
	case "require", "require_any", "require_any_unblocked", "evaluate", "filter", "find_matched":
		engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
		ctx := foundationContext(t, engine, f)
		runEngineOperation(t, engine, ctx, f, checks)
	case "mandatory", "mandatory_server_admission":
		engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
		ctx := foundationContext(t, engine, f)
		authorization, err := engine.PrepareMandatory(ctx)
		require.NoError(t, err)
		var decision MandatoryDecision
		if f.Operation == "mandatory" {
			decision, err = authorization.Decide(fixtureOrgID, checks...)
		} else {
			require.Len(t, checks, 1)
			visibility := MandatoryServerVisibility("private")
			if f.Public {
				visibility = MandatoryServerPublic
			}
			decision, err = authorization.DecideServerAdmission(fixtureOrgID, visibility, checks[0])
		}
		requireFoundationOutcome(t, f.Want, decision.Allowed, err)
	case "grants_authorize":
		require.Len(t, checks, 1)
		allowed, err := GrantsAuthorize(foundationGrants(f.Grants), checks[0])
		requireFoundationOutcome(t, f.Want, allowed, err)
	case "grants_satisfy":
		require.Len(t, checks, 1)
		requireFoundationOutcome(t, f.Want, GrantsSatisfy(foundationGrants(f.Grants), checks[0]), nil)
	case "grants_contain_selector":
		require.Len(t, checks, 1)
		requireFoundationOutcome(t, f.Want, GrantsContainSelector(foundationGrants(f.Grants), checks[0].Scope, Selector(f.Selector)), nil)
	case "risk_policy_applies":
		require.Len(t, checks, 1)
		dims := RiskPolicyDimensions{ServerURL: checks[0].Dimensions[SelectorKeyServerURL], ServerIdentity: checks[0].Dimensions[SelectorKeyServerIdentity]}
		result, err := RiskPolicyApplies(checks[0].ResourceID, dims).Evaluate(foundationGrants(f.Grants))
		requireFoundationOutcome(t, f.Want, result.Satisfied, err)
	case "validate_selector":
		require.Len(t, checks, 1)
		err := ValidateSelector(checks[0].Scope, Selector(f.Selector))
		requireFoundationOutcome(t, f.Want, err == nil, nil)
	case "role_grant_rows":
		writes := make([]*RoleGrant, 0, len(f.Writes))
		for _, w := range f.Writes {
			var selectors []Selector
			if w.Selectors != nil {
				selectors = make([]Selector, 0, len(w.Selectors))
				for _, s := range w.Selectors {
					selectors = append(selectors, Selector(s))
				}
			}
			writes = append(writes, &RoleGrant{Scope: w.Scope, Selectors: selectors})
		}
		rows, err := flattenRoleGrants(writes)
		if f.Want != "allow" {
			requireFoundationOutcome(t, f.Want, false, err)
			return
		}
		require.NoError(t, err)
		got := make([]foundationRow, 0, len(rows))
		for _, row := range rows {
			got = append(got, foundationRow{Scope: row.Scope, Selector: row.Selector})
		}
		want := f.WantRows
		if want == nil {
			want = []foundationRow{}
		}
		require.Equal(t, want, got)
	case "selector_from_row":
		_, err := SelectorFromRow(f.Raw)
		requireFoundationOutcome(t, f.Want, err == nil, nil)
	case "assistant_defaults":
		got := withAssistantSystemRoleDefaults(foundationGrants(f.Grants), f.Defaults)
		requireFoundationOutcome(t, f.Want, foundationAllows(t, got, checks), nil)
	default:
		t.Fatalf("unknown operation %q", f.Operation)
	}
}

func foundationAllows(t *testing.T, grants []Grant, checks []Check) bool {
	t.Helper()
	for _, check := range checks {
		allowed, err := GrantsAuthorize(grants, check)
		require.NoError(t, err)
		if !allowed {
			return false
		}
	}
	return true
}

func runEngineOperation(t *testing.T, engine *Engine, ctx context.Context, f foundationFixture, checks []Check) {
	t.Helper()

	switch f.Operation {
	case "require":
		err := engine.Require(ctx, checks...)
		requireFoundationOutcome(t, f.Want, err == nil, ignoreForbidden(err))
	case "require_any":
		err := engine.RequireAny(ctx, checks...)
		requireFoundationOutcome(t, f.Want, err == nil, ignoreForbidden(err))
	case "require_any_unblocked":
		err := engine.RequireAnyUnblocked(ctx, checks...)
		requireFoundationOutcome(t, f.Want, err == nil, ignoreForbidden(err))
	case "evaluate":
		allowed, err := engine.Evaluate(ctx, checks...)
		requireFoundationOutcome(t, f.Want, allowed, err)
	case "filter":
		ids, err := engine.Filter(ctx, checks)
		requireFoundationOutcome(t, f.Want, err == nil, err)
		if err == nil {
			require.ElementsMatch(t, f.WantIDs, ids)
		}
	case "find_matched":
		matched, err := engine.FindMatched(ctx, checks)
		requireFoundationOutcome(t, f.Want, err == nil, err)
		if err == nil {
			require.Equal(t, f.WantMatch, matched)
		}
	}
}

// ignoreForbidden turns the Engine's forbidden mapping into a plain deny so
// requireFoundationOutcome can tell denial from other errors.
func ignoreForbidden(err error) error {
	var shareable *oops.ShareableError
	if errors.As(err, &shareable) && shareable.Code == oops.CodeForbidden {
		return nil
	}
	return err
}

func requireFoundationOutcome(t *testing.T, want string, allowed bool, err error) {
	t.Helper()

	got := "deny"
	switch {
	case err != nil:
		got = "error"
		var shareable *oops.ShareableError
		if errors.As(err, &shareable) {
			got = fmt.Sprintf("error:%s", shareable.Code)
		}
	case allowed:
		got = "allow"
	}
	if want == "error" && strings.HasPrefix(got, "error") {
		return
	}
	require.Equal(t, want, got, "error: %v", err)
}

// foundationContext builds the request context for one caller profile using
// the same trusted setters and admission paths production authentication uses.
func foundationContext(t *testing.T, engine *Engine, f foundationFixture) context.Context {
	t.Helper()

	sessionID := "session_fixture"
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID:  fixtureOrgID,
		UserID:                fixtureUserID,
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	}
	ctx := t.Context()
	grants := foundationGrants(f.Grants)
	if grants == nil {
		grants = []Grant{}
	}

	switch f.Caller {
	case "anonymous":
		return ctx
	case "human":
		ctx = contextvalues.SetAuthContext(ctx, authCtx)
		return GrantsToContext(ctx, grants)
	case "human_unprepared":
		return contextvalues.SetAuthContext(ctx, authCtx)
	case "platform_mcp_human":
		authCtx.SessionID = nil
		ctx = contextvalues.SetActingSurface(contextvalues.SetAuthContext(ctx, authCtx), contextvalues.ActingSurfacePlatformMCP)
		return GrantsToContext(ctx, grants)
	case "managed_assistant":
		authCtx.SessionID = nil
		ctx = contextvalues.SetAssistantPrincipal(contextvalues.SetAuthContext(ctx, authCtx), contextvalues.AssistantPrincipal{AssistantID: uuid.MustParse(fixtureAssistantID), ThreadID: uuid.Nil})
		return GrantsToContext(ctx, grants)
	case "embedded_session":
		authCtx.ExternalUserID = "external_fixture"
		ctx = contextvalues.SetAuthContext(ctx, authCtx)
		return GrantsToContext(ctx, grants)
	case "sessionless_internal":
		authCtx.SessionID = nil
		return contextvalues.SetAuthContext(ctx, authCtx)
	case "legacy_api_key":
		authCtx.SessionID = nil
		authCtx.APIKeyID = "key_fixture"
		return contextvalues.WithLegacyAPIKeyAuthorization(ctx, authCtx)
	case "org_less_session":
		authCtx.ActiveOrganizationID = ""
		ctx = contextvalues.SetAuthContext(ctx, authCtx)
		prepared, err := engine.PrepareContext(ctx)
		require.NoError(t, err)
		return prepared
	case "demo_session":
		authCtx.ActiveOrganizationID = constants.DemoOrganizationID
		ctx = contextvalues.SetAuthContext(ctx, authCtx)
		prepared, err := engine.PrepareContext(ctx)
		require.NoError(t, err)
		return prepared
	case "support_session":
		authCtx.IsAdmin = true
		authCtx.SupportOrganizationID = fixtureOrgID
		ctx = contextvalues.WithValidatedSupportSession(ctx, authCtx)
		prepared, err := engine.PrepareContext(ctx)
		require.NoError(t, err)
		return prepared
	case "scope_override_admin", "scope_override_member":
		authCtx.IsAdmin = f.Caller == "scope_override_admin"
		ctx = contextvalues.SetRBACScopeOverride(contextvalues.SetAuthContext(ctx, authCtx), f.Override)
		if !authCtx.IsAdmin {
			// A non-admin's override is ignored; their own grants apply.
			return GrantsToContext(ctx, grants)
		}
		prepared, err := engine.PrepareContext(ctx)
		require.NoError(t, err)
		return prepared
	case "agent_credential", "principal_api_key", "credential_unadmitted":
		authCtx.SessionID = nil
		actor := urn.NewPrincipal(urn.PrincipalTypeAgent, fixtureAgentID)
		if f.Caller == "principal_api_key" {
			authCtx.APIKeyID = "key_fixture"
			ctx = contextvalues.WithPrincipalAPIKeyAuthorization(ctx, authCtx, actor, contextvalues.PrincipalCredential{})
		} else {
			ctx = contextvalues.WithPrincipalCredentialAuthorization(ctx, authCtx, actor, contextvalues.PrincipalCredential{})
		}
		if f.Caller == "credential_unadmitted" {
			return ctx
		}
		require.NotNil(t, f.Policies, "%s needs policies", f.Caller)
		admitted, err := applyPrincipalCredentialAdmission(ctx, PrincipalCredentialAdmission{
			OwnerUserID:       fixtureUserID,
			Credential:        foundationGrants(f.Policies.Credential),
			Agent:             foundationGrants(f.Policies.Agent),
			Owner:             foundationGrants(f.Policies.Owner),
			Authorizer:        RepresentedUserPolicy{UserID: cmp.Or(f.Policies.RepresentedUser, fixtureUserID), Policy: foundationGrants(f.Policies.Represented)},
			LegacyAuthCeiling: false,
		})
		require.NoError(t, err)
		return admitted
	case "workload":
		authCtx.SessionID = nil
		actor := urn.NewPrincipal(urn.PrincipalTypeWorkload, "workload_fixture")
		ctx = contextvalues.WithPrincipalCredentialAuthorization(ctx, authCtx, actor, contextvalues.PrincipalCredential{})
		require.NotNil(t, f.Policies, "workload needs policies")
		admitted, err := withWorkloadSessionAdmission(ctx, WorkloadSessionAdmission{
			AgentPrincipal:    urn.NewPrincipal(urn.PrincipalTypeAgent, fixtureAgentID).String(),
			OwnerUserID:       fixtureUserID,
			Ceiling:           foundationGrants(f.Policies.Credential),
			Agent:             foundationGrants(f.Policies.Agent),
			Owner:             foundationGrants(f.Policies.Owner),
			LegacyAuthCeiling: false,
		})
		require.NoError(t, err)
		return admitted
	default:
		t.Fatalf("unknown caller %q", f.Caller)
		return nil
	}
}

// TestFoundationCompatibilityCorpusCoversContract fails if a contract the F1
// ticket requires has no fixture, so trimming the corpus cannot silently drop
// a compatibility obligation.
func TestFoundationCompatibilityCorpusCoversContract(t *testing.T) {
	t.Parallel()

	required := []string{
		"nontransitive_expansion",
		"permissive_allow_matching",
		"strict_block_matching",
		"strict_auth_client_matching",
		"direct_exceptions",
		"own_blocks",
		"raw_subtraction",
		"root",
		"alternatives",
		"block_veto",
		"policy_intersection",
		"represented_user",
		"enforcement_profile",
		"input_errors",
		"nil_empty_writes",
		"malformed_grants",
		"assistant_defaults",
		"mandatory_gates",
		"selector_validation",
		"lists",
	}
	seen := map[string]bool{}
	paths, err := filepath.Glob(filepath.Join(foundationCompatibilityDir, "*.json"))
	require.NoError(t, err)
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var file foundationFixtureFile
		require.NoError(t, json.Unmarshal(raw, &file))
		for _, fixture := range file.Cases {
			seen[fixture.Contract] = true
		}
	}
	var missing []string
	for _, contract := range required {
		if !seen[contract] {
			missing = append(missing, contract)
		}
	}
	slices.Sort(missing)
	require.Empty(t, missing)
}
