package authz

import (
	"testing"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestOrganizationAdministratorEmailsMatchesInEveryMode(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newTestDB(t)
	organizationID := "org-admin-audience"
	seedOrganization(t, ctx, db, organizationID)
	require.NoError(t, SeedSystemRoleGrants(ctx, db, organizationID))
	seedAdminAudience(t, ctx, db, organizationID)
	want := []string{"Alpha@example.test", "zeta@example.test"}

	engine := NewEngine(testenv.NewLogger(t), db, staticChallengeLogging(false), workos.NewStubClient())
	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			modes := newSwitchableModes(map[string]MigrationMode{"organization": mode})
			recorder := &capturedComparisons{}
			registry, err := DefaultRegistry()
			require.NoError(t, err)
			router, err := NewMigrationRouter(registry, modes, DefaultMigrationUnits()...)
			require.NoError(t, err)
			client, err := NewClient(engine, registry, WithMigrationRouter(router), WithShadowRecorder(recorder))
			require.NoError(t, err)

			// No request admission: the audience runs on behalf of the
			// organization, as a background notification does.
			got, err := client.OrganizationAdministratorEmails(ctx, db, organizationID)
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, 1, modes.timesAsked("organization"), "the organization's mode is read once per resolution")

			comparisons := recorder.all()
			if mode != MigrationModeShadow {
				require.Empty(t, comparisons)
				return
			}
			require.Len(t, comparisons, 1)
			require.Equal(t, ShadowMatch, comparisons[0].Outcome, comparisons[0].Detail)
			require.Equal(t, "audience", comparisons[0].Entry)
			require.Equal(t, organizationID, comparisons[0].OrganizationID)
			require.Equal(t, []string{"organization.administer"}, comparisons[0].Permissions)
		})
	}
}

func TestOrganizationAdministratorEmailsShadowFindsDisagreement(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newTestDB(t)
	organizationID := "org-admin-audience-broken"
	seedOrganization(t, ctx, db, organizationID)
	require.NoError(t, SeedSystemRoleGrants(ctx, db, organizationID))
	seedAdminAudience(t, ctx, db, organizationID)

	engine := NewEngine(testenv.NewLogger(t), db, staticChallengeLogging(false), workos.NewStubClient())
	recorder := &capturedComparisons{}
	registry := brokenAdministerRegistry(t)
	router, err := NewMigrationRouter(registry, FixedModes(MigrationModeShadow), DefaultMigrationUnits()...)
	require.NoError(t, err)
	client, err := NewClient(engine, registry, WithMigrationRouter(router), WithShadowRecorder(recorder))
	require.NoError(t, err)

	got, err := client.OrganizationAdministratorEmails(ctx, db, organizationID)
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha@example.test", "zeta@example.test"}, got, "the engine still decides in shadow")
	comparisons := recorder.all()
	require.Len(t, comparisons, 1)
	require.Equal(t, ShadowNewAllow, comparisons[0].Outcome, "a registry that ignores the admin block lets the blocked admin in")
}

// brokenAdministerRegistry is the default registry with organization
// administration no longer excluded by the admin block.
func brokenAdministerRegistry(t *testing.T) *s.Registry {
	t.Helper()
	access := string(GrantSurfaceAccess)
	decls := []s.TypeDecl{s.NewType("organization", s.StoredKind(ResourceKindOrg), s.GrantSurface(access),
		s.Grant(ScopeOrgRead, s.GrantSpec{Description: "Read the organization"}),
		s.Grant(ScopeOrgAdmin, s.GrantSpec{Description: "Administer the organization"}),
		s.Grant(ScopeOrgDeviceAgentSync, s.GrantSpec{Description: "Sync device agents"}),
		s.Grant(ScopeOrgHooksIngest, s.GrantSpec{SelectorKeys: []string{SelectorKeyProjectID}, Description: "Ingest hook events"}),
		s.Block(ScopeOrgBlockedRead, s.BlockSpec{}),
		s.Block(ScopeOrgBlockedAdmin, s.BlockSpec{}),
		s.Permission("read", s.Union(s.Rel(ScopeOrgRead), s.Rel(ScopeOrgAdmin)), s.AnswersScope(string(ScopeOrgRead)), s.ExcludedBy(ScopeOrgBlockedRead)),
		s.Permission("administer", s.Rel(ScopeOrgAdmin), s.AnswersScope(string(ScopeOrgAdmin))),
		s.Permission("sync_device_agent", s.Union(s.Rel(ScopeOrgDeviceAgentSync), s.Rel(ScopeOrgAdmin)), s.AnswersScope(string(ScopeOrgDeviceAgentSync))),
		s.Permission("ingest_hooks", s.Union(s.Rel(ScopeOrgHooksIngest), s.Rel(ScopeOrgAdmin)), s.AnswersScope(string(ScopeOrgHooksIngest))),
	)}
	for _, decl := range DefaultDeclarations() {
		if decl.Name() != "organization" {
			decls = append(decls, decl)
		}
	}
	reg, err := s.Compile(DefaultRegistryConfig(), decls...)
	require.NoError(t, err)
	return reg
}
