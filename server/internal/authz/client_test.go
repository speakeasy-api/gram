package authz

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newTestClient builds a default client whose every unit is routed to the
// registry evaluator, so tests exercise the new decisions directly.
func newTestClient(t *testing.T) (*Client, *Engine) {
	t.Helper()
	return newRoutedTestClient(t, nil, fixedModes(MigrationModeNew), nil)
}

// newRoutedTestClient builds a client over registry (the default registry
// when nil) with the default units routed by source, recording shadow
// comparisons into recorder when it is not nil.
func newRoutedTestClient(t *testing.T, registry *s.Registry, source ModeSource, recorder ShadowRecorder) (*Client, *Engine) {
	t.Helper()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	if registry == nil {
		var err error
		registry, err = DefaultRegistry()
		require.NoError(t, err)
	}
	router, err := NewMigrationRouter(registry, source, DefaultMigrationUnits()...)
	require.NoError(t, err)
	opts := []ClientOption{WithMigrationRouter(router), WithBuildVersion("test")}
	if recorder != nil {
		opts = append(opts, WithShadowRecorder(recorder))
	}
	client, err := NewClient(engine, registry, opts...)
	require.NoError(t, err)
	return client, engine
}

// clientCorpusOperations are the entry points whose frozen decisions the
// client must reproduce. require_any has no client equivalent: the client
// only answers named alternatives, which the registry declares.
var clientCorpusOperations = map[string]bool{
	"require":               true,
	"require_any_unblocked": true,
	"evaluate":              true,
	"find_matched":          true,
}

// TestClientFoundationCorpus runs every frozen engine decision whose scopes
// the default registry declares through the typed client, for every caller
// profile the corpus covers: humans, Platform MCP, assistants, embedded
// sessions, legacy and principal API keys, agent credentials, workloads,
// demo, support, override and org-less sessions. The expectations are the
// hand-written ones the engine is held to, not the engine's output.
func TestClientFoundationCorpus(t *testing.T) {
	t.Parallel()

	for _, fixture := range clientCorpusFixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()
			client, engine := newTestClient(t)
			runClientCorpusCase(t, client, engine, fixture)
		})
	}
}

// clientCorpusFixtures selects the frozen fixtures the client must
// reproduce: those on a corpus operation whose every scope the default
// registry declares, minus the listed skips.
func clientCorpusFixtures(t *testing.T) []foundationFixture {
	t.Helper()

	registry, err := DefaultRegistry()
	require.NoError(t, err)

	paths, err := filepath.Glob(filepath.Join(foundationCompatibilityDir, "*.json"))
	require.NoError(t, err)
	var selected []foundationFixture
	skipped := 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var file foundationFixtureFile
		require.NoError(t, json.Unmarshal(raw, &file))
		for _, fixture := range file.Cases {
			if !clientCorpusOperations[fixture.Operation] || !declaresEveryCheck(registry, fixture) {
				continue
			}
			if reason, skip := clientCorpusSkips[fixture.Name]; skip {
				require.NotEmpty(t, reason)
				skipped++
				continue
			}
			selected = append(selected, fixture)
		}
	}
	require.Equal(t, len(clientCorpusSkips), skipped, "a listed skip no longer applies")
	require.Len(t, selected, 104, "the selected frozen corpus changed; check every new case runs, then update the count")
	return selected
}

// clientCorpusSkips lists every selected fixture the client does not run,
// and why.
var clientCorpusSkips = map[string]string{
	"allow_explicit_kind_override_must_match_grant_kind": "typed operations take the resource kind from the declared type; a caller cannot override it",
}

func declaresEveryCheck(registry *s.Registry, f foundationFixture) bool {
	if len(f.Checks) == 0 {
		return false
	}
	for _, c := range f.Checks {
		if _, ok := registry.PermissionForCheck(string(c.Scope)); !ok {
			return false
		}
	}
	return true
}

func runClientCorpusCase(t *testing.T, client *Client, engine *Engine, f foundationFixture) {
	t.Helper()

	ctx := foundationContext(t, engine, f)
	ops := make([]Operation, 0, len(f.Checks))
	for _, c := range f.Checks {
		op, err := operationForCheck(client.registry, c.check())
		if err != nil {
			// An unbuildable operation is the client's form of an invalid
			// check, which the corpus expects to error.
			requireFoundationOutcome(t, f.Want, false, err)
			return
		}
		ops = append(ops, op)
	}

	switch f.Operation {
	case "require":
		err := client.Require(ctx, ops...)
		requireFoundationOutcome(t, f.Want, err == nil, ignoreForbidden(err))
	case "require_any_unblocked":
		require.Len(t, ops, 2, "the publication alternatives take a project and an organization option")
		publication := newAlternatives("project", "plugin_publication", ops...)
		err := client.Require(ctx, publication)
		requireFoundationOutcome(t, f.Want, err == nil, ignoreForbidden(err))
	case "evaluate":
		allowed, err := client.Evaluate(ctx, ops...)
		requireFoundationOutcome(t, f.Want, allowed, err)
	case "find_matched":
		matched, err := client.MatchMany(ctx, ops)
		requireFoundationOutcome(t, f.Want, err == nil, err)
		if err == nil {
			require.Equal(t, f.WantMatch, matched)
		}
	}
}

func TestNewClientRejectsMissingDeclarations(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	var organizationOnly []s.TypeDecl
	for _, decl := range DefaultDeclarations() {
		if decl.Name() == "organization" {
			organizationOnly = append(organizationOnly, decl)
		}
	}
	registry, err := s.Compile(DefaultRegistryConfig(), organizationOnly...)
	require.NoError(t, err)
	_, err = NewClient(engine, registry)
	require.ErrorIs(t, err, ErrUnknownPermission)

	_, err = NewClient(nil, registry)
	require.Error(t, err)
}

func humanClientContext(t *testing.T, grants ...Grant) context.Context {
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
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), authCtx, urn.NewPrincipal(urn.PrincipalTypeUser, fixtureUserID))
	if grants == nil {
		grants = []Grant{}
	}
	return GrantsToContext(ctx, grants)
}

func TestClientTypedOperations(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t)
	project := Project(fixtureOrgID, "proj_1")
	role := "role:organization:role_1"

	t.Run("project read and write", func(t *testing.T) {
		t.Parallel()
		ctx := humanClientContext(t, grantOf(role, string(ScopeProjectRead), ResourceKindProject, "proj_1"))
		require.NoError(t, client.Require(ctx, project.Read()))
		require.Error(t, client.Require(ctx, project.Write()))
		require.Error(t, client.Require(ctx, project.Read(), project.Write()), "every operation must be allowed")
		allowed, err := client.Evaluate(ctx, project.Write())
		require.NoError(t, err)
		require.False(t, allowed)
	})

	t.Run("organization admin reaches project write through the scope table", func(t *testing.T) {
		t.Parallel()
		ctx := humanClientContext(t, grantOf(role, string(ScopeOrgAdmin), ResourceKindOrg, fixtureOrgID))
		require.NoError(t, client.Require(ctx, Organization(fixtureOrgID).Administer()))
		allowed, err := client.Evaluate(ctx, project.Write())
		require.NoError(t, err)
		require.Equal(t, GrantsSatisfy([]Grant{grantOf(role, string(ScopeOrgAdmin), ResourceKindOrg, fixtureOrgID)}, Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "proj_1", Dimensions: nil}), allowed,
			"the client must agree with the engine's one-step expansion")
	})

	t.Run("plugin publication vetoes a block on either alternative", func(t *testing.T) {
		t.Parallel()
		ctx := humanClientContext(t,
			grantOf(role, string(ScopeOrgAdmin), ResourceKindOrg, fixtureOrgID),
			grantOf(role, string(ScopePluginBlockedWrite), ResourceKindProject, "proj_1"),
		)
		require.Error(t, client.Require(ctx, project.PublishPlugin()))
		ctx = humanClientContext(t, grantOf(role, string(ScopeOrgAdmin), ResourceKindOrg, fixtureOrgID))
		require.NoError(t, client.Require(ctx, project.PublishPlugin()))
	})

	t.Run("match many keeps input order and duplicate resources", func(t *testing.T) {
		t.Parallel()
		ctx := humanClientContext(t, grantOf(role, string(ScopeProjectRead), ResourceKindProject, "proj_1"))
		ops := []Operation{project.Write(), project.Read(), Project(fixtureOrgID, "proj_2").Read(), project.Read()}
		matched, err := client.MatchMany(ctx, ops)
		require.NoError(t, err)
		require.Equal(t, []bool{false, true, false, true}, matched)
	})

	t.Run("an unprepared context is an error, not a denial", func(t *testing.T) {
		t.Parallel()
		ctx := humanClientContext(t)
		ctx = context.WithValue(ctx, grantsContextKey, struct{}{})
		err := client.Require(ctx, project.Read())
		require.Error(t, err)
		require.Error(t, ignoreForbidden(err))
	})

	t.Run("the zero operation errors even where RBAC is not enforced", func(t *testing.T) {
		t.Parallel()
		ctx := humanClientContext(t)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		unenforced := *authCtx
		unenforced.SessionID = nil
		ctx = contextvalues.SetAuthContext(ctx, &unenforced)
		enforce, err := client.ShouldEnforce(ctx)
		require.NoError(t, err)
		require.False(t, enforce)

		require.NoError(t, client.Require(ctx, project.Read()), "valid operations pass where RBAC is not enforced")
		require.Error(t, ignoreForbidden(client.Require(ctx, Operation{})))
		_, err = client.Evaluate(ctx, Operation{})
		require.Error(t, err)
		_, err = client.MatchMany(ctx, []Operation{{}})
		require.Error(t, err)
	})

	t.Run("match many rejects alternatives", func(t *testing.T) {
		t.Parallel()
		ctx := humanClientContext(t, grantOf(role, string(ScopeOrgAdmin), ResourceKindOrg, fixtureOrgID))
		_, err := client.MatchMany(ctx, []Operation{project.PublishPlugin()})
		require.Error(t, ignoreForbidden(err))
	})

	t.Run("the zero operation and no operations error", func(t *testing.T) {
		t.Parallel()
		ctx := humanClientContext(t, grantOf(role, string(ScopeProjectRead), ResourceKindProject, "*"))
		require.Error(t, ignoreForbidden(client.Require(ctx, Operation{})))
		require.Error(t, ignoreForbidden(client.Require(ctx)))
	})
}

func TestClientCapturedBoundaryLimitsRefreshedAuthority(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t)
	role := "role:organization:role_1"
	project := Project(fixtureOrgID, "proj_1")

	captured := humanClientContext(t, grantOf(role, string(ScopeProjectRead), ResourceKindProject, "proj_1"))
	boundary, err := client.CaptureAdmissionBoundary(captured)
	require.NoError(t, err)

	// The job's later context is broader, but the captured policy still
	// limits it.
	later := boundary.Apply(humanClientContext(t, grantOf(role, string(ScopeProjectWrite), ResourceKindProject, "*")))
	require.NoError(t, client.Require(later, project.Read()))
	require.Error(t, client.Require(later, project.Write()))

	// A narrower refresh narrows the job.
	narrower := boundary.Apply(humanClientContext(t))
	require.Error(t, client.Require(narrower, project.Read()))
}

func TestLegacyFormMatchesTypedOperations(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t)
	for _, op := range typedOperations() {
		form, ok := client.legacyFormFor(op)
		require.True(t, ok, "%s has no engine form", op)
		require.Len(t, form.checks, max(1, len(op.options)), op.String())
		parts := []Operation{op}
		if op.alternatives {
			parts = op.options
		}
		for i, check := range form.checks {
			back, err := operationForCheck(client.registry, check)
			require.NoError(t, err)
			require.Equal(t, parts[i].permission, back.permission, "%s option %d maps back to another permission", op, i)
			require.Equal(t, parts[i].instance.resourceID, back.instance.resourceID, op.String())
			require.Equal(t, parts[i].instance.strict, back.instance.strict, op.String())
		}
	}
	form, ok := client.legacyFormFor(Project(fixtureOrgID, "proj_1").PublishPlugin())
	require.True(t, ok)
	require.True(t, form.anyUnblocked, "plugin publication vetoes blocks like RequireAnyUnblocked")
	require.Equal(t, []Scope{ScopePluginWrite, ScopeOrgAdmin}, []Scope{form.checks[0].Scope, form.checks[1].Scope})
}

func TestChallengeChecksRecordEveryAlternative(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t)
	project := Project(fixtureOrgID, "proj_1")
	checks := client.legacyChecksFor([]Operation{project.Read(), project.PublishPlugin()})

	scopes := make([]Scope, len(checks.requested))
	for i, check := range checks.requested {
		scopes[i] = check.Scope
	}
	require.Equal(t, []Scope{ScopeProjectRead, ScopePluginWrite, ScopeOrgAdmin}, scopes, "every option is a requested check")
	require.Equal(t, ScopeOrgAdmin, checks.focus(1, 1).Scope, "the deciding option is the focus")
	require.Equal(t, ScopePluginWrite, checks.focus(1, -1).Scope, "without a deciding option the first option is the focus")
	require.Equal(t, ScopeProjectRead, checks.focus(0, -1).Scope)
}

func TestClientDeviceAgentAndHookOperations(t *testing.T) {
	t.Parallel()

	role := "role:organization:role_1"
	org := Organization(fixtureOrgID)
	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeNew} {
		client, _ := newRoutedTestClient(t, nil, fixedModes(mode), nil)

		t.Run(mode.String()+" hook ingestion is bounded to the granted project", func(t *testing.T) {
			t.Parallel()
			ctx := humanClientContext(t, grantOf(role, string(ScopeOrgHooksIngest), ResourceKindOrg, fixtureOrgID, SelectorKeyProjectID, "proj_1"))
			require.NoError(t, client.Require(ctx, org.IngestHooks("proj_1")))
			require.Error(t, client.Require(ctx, org.IngestHooks("proj_2")))
			require.Error(t, client.Require(ctx, org.SyncDeviceAgent()))
		})

		t.Run(mode.String()+" device agent sync", func(t *testing.T) {
			t.Parallel()
			ctx := humanClientContext(t, grantOf(role, string(ScopeOrgDeviceAgentSync), ResourceKindOrg, fixtureOrgID))
			require.NoError(t, client.Require(ctx, org.SyncDeviceAgent()))
			require.Error(t, client.Require(ctx, org.IngestHooks("proj_1")))
			require.Error(t, client.Require(ctx, org.Administer()))
		})

		t.Run(mode.String()+" organization admin covers both", func(t *testing.T) {
			t.Parallel()
			ctx := humanClientContext(t, grantOf(role, string(ScopeOrgAdmin), ResourceKindOrg, fixtureOrgID))
			require.NoError(t, client.Require(ctx, org.SyncDeviceAgent(), org.IngestHooks("proj_1")))
		})
	}
}

func TestClientReadOrOrganizationRead(t *testing.T) {
	t.Parallel()

	role := "role:organization:role_1"
	project := Project(fixtureOrgID, "proj_1")
	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)

			projectReader := humanClientContext(t, grantOf(role, string(ScopeProjectRead), ResourceKindProject, "proj_1"))
			require.NoError(t, client.Require(projectReader, project.ReadOrOrganizationRead()))

			orgReader := humanClientContext(t, grantOf(role, string(ScopeOrgRead), ResourceKindOrg, fixtureOrgID))
			require.NoError(t, client.Require(orgReader, project.ReadOrOrganizationRead()))

			blockedProject := humanClientContext(t,
				grantOf(role, string(ScopeOrgRead), ResourceKindOrg, fixtureOrgID),
				grantOf(role, string(ScopeProjectBlockedRead), ResourceKindProject, "proj_1"),
			)
			require.NoError(t, client.Require(blockedProject, project.ReadOrOrganizationRead()), "a project block does not stop organization read")

			otherProject := humanClientContext(t, grantOf(role, string(ScopeProjectRead), ResourceKindProject, "proj_2"))
			require.Error(t, client.Require(otherProject, project.ReadOrOrganizationRead()))

			for _, outcome := range recorder.outcomes() {
				require.Equal(t, ShadowMatch, outcome)
			}
			if mode == MigrationModeShadow {
				require.Len(t, recorder.outcomes(), 4)
			}
		})
	}
}
