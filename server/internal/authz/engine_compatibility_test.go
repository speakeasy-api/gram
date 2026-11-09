package authz

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// engineCompatibilityDir holds the hand-written fixtures and golden files that
// pin today's authorization behaviour.
const engineCompatibilityDir = "testdata/engine_compatibility"

// The engine compatibility corpus pins today's authorization decisions as
// explicit expectations. Every expected outcome is written by hand in
// testdata/engine_compatibility/*.json and evaluated against the Engine and its
// engine-free helpers. The expectations are not derived from any scope table,
// so a refactor of those tables cannot make both sides agree by accident.
//
// Fixture fields:
//
//	caller     who is asking; see compatCaller
//	grants     the loaded grant set for human, assistant and embedded callers
//	policies   independent admitted policies for credential and workload callers
//	operation  which entry point decides; see runCompatCase
//	checks     the checks passed to it, in order
//	want       allow, deny, or error:<oops code>
const (
	fixtureUserID      = "user_fixture"
	fixtureOrgID       = "org_fixture"
	fixtureAgentID     = "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001"
	fixtureAssistantID = "018f8d7b-58d7-7cc4-bb16-9f8c6b99a002"
)

type compatFixtureFile struct {
	Description string          `json:"description"`
	Cases       []compatFixture `json:"cases"`
}

type compatFixture struct {
	Name      string            `json:"name"`
	Contract  string            `json:"contract"`
	Caller    string            `json:"caller"`
	Grants    []compatGrant     `json:"grants"`
	Policies  *compatPolicy     `json:"policies"`
	Override  string            `json:"override"`
	Operation string            `json:"operation"`
	Checks    []compatCheck     `json:"checks"`
	Selector  map[string]string `json:"selector"`
	Public    bool              `json:"public"`
	Want      string            `json:"want"`
	WantIDs   []string          `json:"want_ids"`
	WantMatch []bool            `json:"want_matched"`
	WantRows  []compatRow       `json:"want_rows"`
	Writes    []compatWrite     `json:"writes"`
	Raw       json.RawMessage   `json:"raw"`
}

type compatGrant struct {
	Principal string            `json:"principal"`
	Scope     Scope             `json:"scope"`
	Selector  map[string]string `json:"selector"`
	Inferred  bool              `json:"inferred"`
}

type compatPolicy struct {
	Credential      []compatGrant `json:"credential"`
	Agent           []compatGrant `json:"agent"`
	Owner           []compatGrant `json:"owner"`
	RepresentedUser string        `json:"represented_user"`
	Represented     []compatGrant `json:"represented"`
}

type compatCheck struct {
	Scope        Scope             `json:"scope"`
	ResourceKind string            `json:"resource_kind"`
	ResourceID   string            `json:"resource_id"`
	Dimensions   map[string]string `json:"dimensions"`
	Strict       bool              `json:"strict"`
}

type compatWrite struct {
	Scope     string              `json:"scope"`
	Selectors []map[string]string `json:"selectors"`
}

type compatRow struct {
	Scope    Scope             `json:"scope"`
	Selector map[string]string `json:"selector"`
}

func (g compatGrant) grant() Grant {
	principal := cmp.Or(g.Principal, "user:"+fixtureUserID)
	return Grant{PrincipalUrn: principal, Scope: g.Scope, Selector: Selector(g.Selector), Inferred: g.Inferred}
}

func compatGrants(in []compatGrant) []Grant {
	if in == nil {
		return nil
	}
	out := make([]Grant, 0, len(in))
	for _, g := range in {
		out = append(out, g.grant())
	}
	return out
}

func (c compatCheck) check() Check {
	check := Check{Scope: c.Scope, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: selectorMatchNormal, representedUser: ""}
	if c.Strict {
		check = check.WithStrictSelectorMatch()
	}
	return check
}

// TestEngineCompatibilityCorpus runs every fixture against the current
// evaluator. A failure here is a behaviour change: fix the code, or, for an
// approved access change, update the expectation in the same PR and say so.
func TestEngineCompatibilityCorpus(t *testing.T) {
	t.Parallel()

	for _, fixture := range loadCompatFixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()
			runCompatCase(t, fixture)
		})
	}
}

// loadCompatFixtures strictly decodes every fixture file and rejects
// unnamed, uncontracted and duplicate cases.
func loadCompatFixtures(t *testing.T) []compatFixture {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(engineCompatibilityDir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)

	var fixtures []compatFixture
	names := map[string]string{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		var file compatFixtureFile
		require.NoError(t, decoder.Decode(&file), path)
		require.ErrorIs(t, decoder.Decode(&struct{}{}), io.EOF, "%s: trailing data after the fixture document", path)
		require.NotEmpty(t, file.Cases, path)

		for _, fixture := range file.Cases {
			require.NotEmpty(t, fixture.Name, path)
			require.NotEmpty(t, fixture.Contract, "%s: %s has no contract", path, fixture.Name)
			if previous, ok := names[fixture.Name]; ok {
				t.Fatalf("duplicate fixture name %q in %s and %s", fixture.Name, previous, path)
			}
			names[fixture.Name] = path
			fixtures = append(fixtures, fixture)
		}
	}
	return fixtures
}

func runCompatCase(t *testing.T, f compatFixture) {
	t.Helper()

	checks := make([]Check, 0, len(f.Checks))
	for _, c := range f.Checks {
		checks = append(checks, c.check())
	}

	switch f.Operation {
	case "require", "require_any", "require_any_unblocked", "evaluate", "filter", "find_matched":
		engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
		ctx := compatContext(t, engine, f)
		runEngineOperation(t, engine, ctx, f, checks)
	case "mandatory", "mandatory_server_admission":
		engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
		ctx := compatContext(t, engine, f)
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
		requireCompatOutcome(t, f.Want, decision.Allowed, err)
	case "grants_authorize":
		require.Len(t, checks, 1)
		allowed, err := GrantsAuthorize(compatGrants(f.Grants), checks[0])
		requireCompatOutcome(t, f.Want, allowed, err)
	case "grants_satisfy":
		require.Len(t, checks, 1)
		requireCompatOutcome(t, f.Want, GrantsSatisfy(compatGrants(f.Grants), checks[0]), nil)
	case "grants_contain_selector":
		require.Len(t, checks, 1)
		requireCompatOutcome(t, f.Want, GrantsContainSelector(compatGrants(f.Grants), checks[0].Scope, Selector(f.Selector)), nil)
	case "risk_policy_applies":
		require.Len(t, checks, 1)
		dims := RiskPolicyDimensions{ServerURL: checks[0].Dimensions[SelectorKeyServerURL], ServerIdentity: checks[0].Dimensions[SelectorKeyServerIdentity]}
		result, err := RiskPolicyApplies(checks[0].ResourceID, dims).Evaluate(compatGrants(f.Grants))
		requireCompatOutcome(t, f.Want, result.Satisfied, err)
	case "validate_selector":
		require.Len(t, checks, 1)
		err := ValidateSelector(checks[0].Scope, Selector(f.Selector))
		requireCompatOutcome(t, f.Want, err == nil, nil)
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
			require.Error(t, err, "role grant flattening should reject these writes")
			requireCompatOutcome(t, f.Want, false, err)
			return
		}
		require.NoError(t, err)
		got := make([]compatRow, 0, len(rows))
		for _, row := range rows {
			got = append(got, compatRow{Scope: row.Scope, Selector: row.Selector})
		}
		want := f.WantRows
		if want == nil {
			want = []compatRow{}
		}
		require.Equal(t, want, got)
	case "selector_from_row":
		_, err := SelectorFromRow(f.Raw)
		requireCompatOutcome(t, f.Want, err == nil, nil)
	default:
		t.Fatalf("unknown operation %q", f.Operation)
	}
}

func runEngineOperation(t *testing.T, engine *Engine, ctx context.Context, f compatFixture, checks []Check) {
	t.Helper()

	switch f.Operation {
	case "require":
		err := engine.Require(ctx, checks...)
		requireCompatOutcome(t, f.Want, err == nil, ignoreForbidden(err))
	case "require_any":
		err := engine.RequireAny(ctx, checks...)
		requireCompatOutcome(t, f.Want, err == nil, ignoreForbidden(err))
	case "require_any_unblocked":
		err := engine.RequireAnyUnblocked(ctx, checks...)
		requireCompatOutcome(t, f.Want, err == nil, ignoreForbidden(err))
	case "evaluate":
		allowed, err := engine.Evaluate(ctx, checks...)
		requireCompatOutcome(t, f.Want, allowed, err)
	case "filter":
		ids, err := engine.Filter(ctx, checks)
		requireCompatOutcome(t, f.Want, err == nil, err)
		if err == nil {
			require.ElementsMatch(t, f.WantIDs, ids)
		}
	case "find_matched":
		matched, err := engine.FindMatched(ctx, checks)
		requireCompatOutcome(t, f.Want, err == nil, err)
		if err == nil {
			require.Equal(t, f.WantMatch, matched)
		}
	}
}

// ignoreForbidden turns the Engine's forbidden mapping into a plain deny so
// requireCompatOutcome can tell denial from other errors.
func ignoreForbidden(err error) error {
	var shareable *oops.ShareableError
	if errors.As(err, &shareable) && shareable.Code == oops.CodeForbidden {
		return nil
	}
	return err
}

func requireCompatOutcome(t *testing.T, want string, allowed bool, err error) {
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

// compatContext builds the request context for one caller profile using
// the same trusted setters and admission paths production authentication uses.
func compatContext(t *testing.T, engine *Engine, f compatFixture) context.Context {
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
	grants := compatGrants(f.Grants)
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
		// The engine honours the override header only for platform admins;
		// a non-admin's override is ignored and their own grants apply.
		_, honoured := engine.GetScopeOverrides(ctx)
		require.Equal(t, authCtx.IsAdmin, honoured, "scope override honoured for admin only")
		if !authCtx.IsAdmin {
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
			Credential:        compatGrants(f.Policies.Credential),
			Agent:             compatGrants(f.Policies.Agent),
			Owner:             compatGrants(f.Policies.Owner),
			Authorizer:        RepresentedUserPolicy{UserID: cmp.Or(f.Policies.RepresentedUser, fixtureUserID), Policy: compatGrants(f.Policies.Represented)},
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
			Ceiling:           compatGrants(f.Policies.Credential),
			Agent:             compatGrants(f.Policies.Agent),
			Owner:             compatGrants(f.Policies.Owner),
			LegacyAuthCeiling: false,
		})
		require.NoError(t, err)
		return admitted
	default:
		t.Fatalf("unknown caller %q", f.Caller)
		return nil
	}
}

// TestEngineCompatibilityCorpusCoversContract fails if a required
// compatibility contract has no fixture, so trimming the corpus cannot
// silently drop a compatibility obligation.
func TestEngineCompatibilityCorpusCoversContract(t *testing.T) {
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
		"mandatory_gates",
		"selector_validation",
		"lists",
		"engine_free_helpers",
	}
	// Every operation runCompatCase handles needs at least one fixture,
	// so no branch of it goes unexercised. Keep this list in sync with the
	// switch in runCompatCase when adding an operation.
	operations := []string{
		"require", "require_any", "require_any_unblocked", "evaluate", "filter", "find_matched",
		"mandatory", "mandatory_server_admission",
		"grants_authorize", "grants_satisfy", "grants_contain_selector",
		"risk_policy_applies", "validate_selector", "role_grant_rows", "selector_from_row",
	}
	seenContracts := map[string]bool{}
	seenOperations := map[string]bool{}
	for _, fixture := range loadCompatFixtures(t) {
		seenContracts[fixture.Contract] = true
		seenOperations[fixture.Operation] = true
	}
	var missing []string
	for _, contract := range required {
		if !seenContracts[contract] {
			missing = append(missing, "contract "+contract)
		}
	}
	for _, operation := range operations {
		if !seenOperations[operation] {
			missing = append(missing, "operation "+operation)
		}
	}
	slices.Sort(missing)
	require.Empty(t, missing)
}
