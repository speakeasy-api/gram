package authclients_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestSetAuthClientAudienceWritesUseAndBlocked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	public := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Public)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Shared")
	associate(t, ctx, ti.conn, ti.organizationID, public, clientID)
	member := callerPrincipal(t, ctx)

	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	current, err := ti.service.GetAuthClientAudience(ctx, &gen.GetAuthClientAudiencePayload{ID: clientID.String()})
	require.NoError(t, err)
	require.Equal(t, []string{public.id.String()}, current.PublicServers)

	before := auditCount(t, ti, audit.ActionAuthClientAudienceUpdate)
	saved, err := ti.service.SetAuthClientAudience(ctx, &gen.SetAuthClientAudiencePayload{
		ID:              clientID.String(),
		ExpectedVersion: current.Version,
		Entries: []*gen.AuthClientAudienceRule{
			{PrincipalUrn: urn.PrincipalWildcard, Level: authclients.LevelUse},
			{PrincipalUrn: member.String(), Level: authclients.LevelBlocked},
		},
	})
	require.NoError(t, err)
	require.Equal(t, before+1, auditCount(t, ti, audit.ActionAuthClientAudienceUpdate))
	require.ElementsMatch(t, []string{
		authz.AllUsersPrincipal().String() + "|" + authclients.LevelUse,
		member.String() + "|" + authclients.LevelBlocked,
	}, ownRules(saved))

	grants, err := authz.LoadGrants(ctx, ti.conn, ti.organizationID, []urn.Principal{member})
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, authz.ScopeMCPBlockedAuth, grants[0].Scope)
	require.Equal(t, clientID.String(), grants[0].Selector.ResourceID())

	_, err = ti.service.SetAuthClientAudience(ctx, &gen.SetAuthClientAudiencePayload{ID: clientID.String(), ExpectedVersion: current.Version, Entries: nil})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	cleared, err := ti.service.SetAuthClientAudience(ctx, &gen.SetAuthClientAudiencePayload{ID: clientID.String(), ExpectedVersion: saved.Version, Entries: nil})
	require.NoError(t, err)
	require.Empty(t, ownRules(cleared))
}

func TestSetAuthClientAudienceKeepsUntouchedProvisionedGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Shared")
	member := callerPrincipal(t, ctx)

	provisioned := authz.AuthClientSelector(clientID.String(), ti.projectID.String())
	require.NoError(t, authz.PatchPrincipalGrants(ctx, ti.conn, ti.organizationID, authz.AllUsersPrincipal(),
		[]*authz.RoleGrant{{Scope: string(authz.ScopeMCPAuth), Selectors: []authz.Selector{provisioned}}}, nil))

	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	current, err := ti.service.GetAuthClientAudience(ctx, &gen.GetAuthClientAudiencePayload{ID: clientID.String()})
	require.NoError(t, err)
	require.Len(t, current.Entries, 1)
	require.Equal(t, ti.projectID.String(), *current.Entries[0].ProjectID)

	_, err = ti.service.SetAuthClientAudience(ctx, &gen.SetAuthClientAudiencePayload{
		ID:              clientID.String(),
		ExpectedVersion: current.Version,
		Entries: []*gen.AuthClientAudienceRule{
			{PrincipalUrn: urn.PrincipalWildcard, Level: authclients.LevelUse},
			{PrincipalUrn: member.String(), Level: authclients.LevelBlocked},
		},
	})
	require.NoError(t, err)

	grants, err := authz.LoadGrants(ctx, ti.conn, ti.organizationID, []urn.Principal{authz.AllUsersPrincipal()})
	require.NoError(t, err)
	var kept bool
	for _, grant := range grants {
		if grant.Scope == authz.ScopeMCPAuth && grant.Selector.ResourceID() == clientID.String() {
			require.Equal(t, ti.projectID.String(), grant.Selector[authz.SelectorKeyProjectID], "the kept rule keeps its stored selector")
			kept = true
		}
	}
	require.True(t, kept)
}

func TestAuthClientAudienceOnPersonalClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	clientID, err := testrepo.New(ti.conn).CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
		OrganizationID: ti.organizationID,
		ProjectID:      uuid.NullUUID{UUID: ti.projectID, Valid: true},
		ClientType:     authz.AuthClientTypeRemoteSession,
		Name:           "Personal",
	})
	require.NoError(t, err)
	member := callerPrincipal(t, ctx)
	require.NoError(t, authz.SeedPersonalAuthClientDefaultTx(ctx, ti.conn, ti.organizationID))

	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	current, err := ti.service.GetAuthClientAudience(ctx, &gen.GetAuthClientAudiencePayload{ID: clientID.String()})
	require.NoError(t, err)
	var inherited *types.AuthClientAudienceEntry
	for _, entry := range current.Entries {
		if entry.AppliesTo == authclients.AppliesToClientType {
			inherited = entry
		}
	}
	require.NotNil(t, inherited, "the personal default reaches every remote session client")
	require.Equal(t, authclients.NoteActingForOwnerOnly, *inherited.ConnectionNote)

	saved, err := ti.service.SetAuthClientAudience(ctx, &gen.SetAuthClientAudiencePayload{
		ID:              clientID.String(),
		ExpectedVersion: current.Version,
		Entries:         []*gen.AuthClientAudienceRule{{PrincipalUrn: member.String(), Level: authclients.LevelUse}},
	})
	require.NoError(t, err)
	for _, entry := range saved.Entries {
		if entry.AppliesTo == authclients.AppliesToClient {
			require.Equal(t, authclients.NoteOwnConnectionOnly, *entry.ConnectionNote)
		}
	}

	grants, err := authz.LoadGrants(ctx, ti.conn, ti.organizationID, []urn.Principal{member})
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, authz.SelectorValueRepresentedUser, grants[0].Selector[authz.SelectorKeyConnectionOwner], "a grant never reaches another person's connection")
}

func TestSetAuthClientAudienceRejectsKeysAndNonAdmins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Shared")

	admin := withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	current, err := ti.service.GetAuthClientAudience(admin, &gen.GetAuthClientAudiencePayload{ID: clientID.String()})
	require.NoError(t, err)

	_, err = ti.service.SetAuthClientAudience(admin, &gen.SetAuthClientAudiencePayload{
		ID:              clientID.String(),
		ExpectedVersion: current.Version,
		Entries:         []*gen.AuthClientAudienceRule{{PrincipalUrn: urn.NewAPIKeyPrincipal(uuid.New()).String(), Level: authclients.LevelUse}},
	})
	requireOopsCode(t, err, oops.CodeInvalid)

	reader := withOrgGrants(t, ctx, authz.ScopeOrgRead)
	_, err = ti.service.SetAuthClientAudience(reader, &gen.SetAuthClientAudiencePayload{ID: clientID.String(), ExpectedVersion: current.Version, Entries: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func callerPrincipal(t *testing.T, ctx context.Context) urn.Principal {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
}

func ownRules(audience *types.AuthClientAudience) []string {
	rules := make([]string, 0, len(audience.Entries))
	for _, entry := range audience.Entries {
		if entry.AppliesTo == authclients.AppliesToClient {
			rules = append(rules, entry.PrincipalUrn+"|"+entry.Level)
		}
	}
	return rules
}
