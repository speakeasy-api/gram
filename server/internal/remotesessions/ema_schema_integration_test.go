package remotesessions_test

import (
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestEMASchema_ProjectTenantConstraint(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	err := repo.New(ti.conn).EnsureEMABinding(ctx, repo.EnsureEMABindingParams{
		ProjectID: *auth.ProjectID, OrganizationID: "other-organization", UserSessionIssuerID: in.UserSessionIssuerID,
		RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource,
	})
	require.NoError(t, err)
	count, err := testrepo.New(ti.conn).CountPreparationFixtureBindings(ctx, *auth.ProjectID)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestEMASchema_ClientIssuerConstraint(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	// A client from another issuer must not be accepted even by a direct repository write.
	otherIssuer := uuid.MustParse(createRemoteIssuer(t, ctx, ti, "other-preparation", ""))
	otherClient := preparationManualClient(t, ctx, ti, *auth.ProjectID, otherIssuer, "other-client")
	_, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	b, err := q.GetEMABinding(ctx, repo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID,
		UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource})
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: b.ID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID,
		ExpectedGeneration: b.Generation, Generation: b.Generation + 1, State: b.State, GrantSource: b.GrantSource,
		RequestedScopes: b.RequestedScopes, RemoteSessionClientID: conv.ToNullUUID(otherClient)})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// The query owns incarnation validation even when no database trigger exists.
func TestEMABindingSQLGenerationTransitions(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	p := repo.SetEMABindingParams{ID: prepared.BindingID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ExpectedGeneration: prepared.Generation, Generation: prepared.Generation, State: conv.ToPGText("unlinked"), GrantSource: conv.ToPGText("unknown"), RemoteSessionClientID: conv.ToNullUUID(in.ClientID), RequestedScopes: []string{}}
	_, err = q.SetEMABinding(ctx, p)
	require.ErrorIs(t, err, pgx.ErrNoRows, "unlink must increment the incarnation")
	p.Generation++
	b, err := q.SetEMABinding(ctx, p)
	require.NoError(t, err)
	require.False(t, b.RemoteSessionClientID.Valid, "unlink clears even an explicitly supplied reference")
	p.ExpectedGeneration = b.Generation
	p.State = conv.ToPGText("unknown_grants")
	_, err = q.SetEMABinding(ctx, p)
	require.ErrorIs(t, err, pgx.ErrNoRows, "revival must increment the incarnation")
	p.Generation++
	b, err = q.SetEMABinding(ctx, p)
	require.NoError(t, err)
	require.Equal(t, in.ClientID, b.RemoteSessionClientID.UUID)
	p.ExpectedGeneration = b.Generation
	p.Generation--
	_, err = q.SetEMABinding(ctx, p)
	require.ErrorIs(t, err, pgx.ErrNoRows, "generation cannot regress")
}

func TestPreparationArchitecture_NoLifecycleTriggers(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := preparationFixture(t)
	count, err := testrepo.New(ti.conn).CountPreparationFixtureLifecycleTriggers(ctx)
	require.NoError(t, err)
	require.Zero(t, count, "run preparation regressions against the trigger-free base; application guards must stand alone")
}

func TestEMABindingNullableLifecycleState(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	err := q.EnsureEMABinding(ctx, repo.EnsureEMABindingParams{
		ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID,
		UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource,
	})
	require.NoError(t, err)
	b, err := q.GetEMABinding(ctx, repo.GetEMABindingParams{
		ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID,
		UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource,
	})
	require.NoError(t, err)
	require.Equal(t, conv.ToPGText("configuration_required"), b.State)
	require.Equal(t, conv.ToPGText("unknown"), b.GrantSource)
	err = testrepo.New(ti.conn).ClearPreparationFixtureState(ctx, testrepo.ClearPreparationFixtureStateParams{ID: b.ID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID})
	require.NoError(t, err)
	count, err := q.CountActiveEMABindingsForIssuer(ctx, repo.CountActiveEMABindingsForIssuerParams{IssuerID: in.RemoteSessionIssuerID, OrganizationID: b.OrganizationID, ProjectID: b.ProjectID})
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "NULL state is not an explicit unlink")
	generation := b.Generation
	b, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{
		ID: b.ID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID,
		ExpectedGeneration: b.Generation, Generation: b.Generation,
		// Preserve nullable provenance: initializing it would require a new incarnation.
		State: conv.ToPGText("configuration_required"), GrantSource: pgtype.Text{String: "", Valid: false}, RequestedScopes: []string{},
	})
	require.NoError(t, err, "initializing only a NULL state preserves the binding incarnation")
	require.Equal(t, generation, b.Generation)
	require.False(t, b.GrantSource.Valid, "a status-only update preserves nullable provenance")
	require.Equal(t, conv.ToPGText("configuration_required"), b.State)
}
