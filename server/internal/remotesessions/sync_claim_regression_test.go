package remotesessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestSyncClaim_StaleClaimAdvancesGenerationWithoutReplaying(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	key := repo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource}
	require.NoError(t, q.EnsureEMABinding(ctx, repo.EnsureEMABindingParams(key)))
	b, err := q.GetEMABinding(ctx, key)
	require.NoError(t, err)
	claim := conv.ToNullUUID(uuid.New())
	claimedAt := conv.ToPGTimestamptz(time.Now().Add(-2 * time.Minute))
	b, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: b.ID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID, Generation: b.Generation + 1, ExpectedGeneration: b.Generation, State: conv.ToPGText("in_progress"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: in.Scopes, ClaimID: claim, ClaimedAt: claimedAt})
	require.NoError(t, err)
	in.ClientID = uuid.Nil
	in.Mechanism = "dcr"
	in.ExpectedGeneration = b.Generation
	for range 2 {
		result, err := ti.service.PrepareIdentityChaining(ctx, in)
		require.NoError(t, err)
		require.Equal(t, "indeterminate", result.State)
		require.Equal(t, b.Generation+1, result.Generation)
		current, err := q.GetEMABinding(ctx, key)
		require.NoError(t, err)
		require.Equal(t, result.Generation, current.Generation)
		require.Equal(t, "indeterminate", current.State.String)
		require.Equal(t, b.ClaimID, current.ClaimID, "uncertain registration must remain claimed")
		require.Equal(t, b.ClaimedAt, current.ClaimedAt)
		require.Equal(t, b.RequestedScopes, current.RequestedScopes)
		read, err := ti.service.ReadIdentityChaining(ctx, in)
		require.NoError(t, err)
		require.Equal(t, result.Generation, read.Generation)
		require.Equal(t, result.State, read.State)
	}
	// The abandoned claim's generation can no longer persist a completion.
	_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: b.ID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID, Generation: b.Generation + 1, ExpectedGeneration: b.Generation, State: conv.ToPGText("provider_rejection"), GrantSource: b.GrantSource, RequestedScopes: b.RequestedScopes, ClaimID: b.ClaimID, ClaimedAt: b.ClaimedAt})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestSyncClaim_ReadinessRequiresRecordedProvenance(t *testing.T) {
	t.Parallel()
	for _, source := range []pgtype.Text{{}, conv.ToPGText("unknown"), conv.ToPGText("provider_returned"), conv.ToPGText("administrator_declared"), conv.ToPGText("cimd_published")} {
		name := source.String
		if !source.Valid {
			name = "null_grant_source"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			preparationRecordGrants(t, ctx, ti, in.ClientID, []string{oauthwire.GrantTypeJWTBearer})
			first, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			auth, _ := contextvalues.GetAuthContext(ctx)
			q := repo.New(ti.conn)
			b, err := q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: first.BindingID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, Generation: first.Generation + 1, ExpectedGeneration: first.Generation, State: conv.ToPGText("ready"), GrantSource: source, RemoteSessionClientID: conv.ToNullUUID(in.ClientID), RequestedScopes: in.Scopes})
			require.NoError(t, err)
			want := "ready"
			if !source.Valid || source.String == "unknown" {
				want = "unknown_grants"
			}
			read, err := ti.service.ReadIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, want, read.State)
			require.Equal(t, b.Generation, read.Generation)
			if want == "unknown_grants" {
				in.ExpectedGeneration = b.Generation
				prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
				require.NoError(t, err)
				require.Equal(t, want, prepared.State)
				require.Equal(t, b.Generation, prepared.Generation)
				in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
				confirmed, err := ti.service.PrepareIdentityChaining(ctx, in)
				require.NoError(t, err)
				require.Equal(t, "ready", confirmed.State)
				require.Equal(t, "administrator_declared", confirmed.GrantSource)
				require.Equal(t, b.Generation+1, confirmed.Generation)
			}
		})
	}
}

func TestSyncClaim_CIMDRequiresBindingEvidenceOrConfirmation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"existing_null_source", "existing_unknown_source", "new_selection", "replacement_selection"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			grants := []string{oauthwire.GrantTypeJWTBearer}
			preparationRecordGrants(t, ctx, ti, in.ClientID, grants)
			first, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, "ready", first.State, "explicit manual selection still records administrator evidence")
			auth, _ := contextvalues.GetAuthContext(ctx)
			q := repo.New(ti.conn)
			fixtures := testrepo.New(ti.conn)
			require.NoError(t, fixtures.EnablePreparationFixtureCIMD(ctx, testrepo.EnablePreparationFixtureCIMDParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)}))
			switch scenario {
			case "existing_null_source", "existing_unknown_source":
				source := pgtype.Text{}
				if scenario == "existing_unknown_source" {
					source = conv.ToPGText("unknown")
				}
				_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: first.BindingID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, Generation: first.Generation + 1, ExpectedGeneration: first.Generation, State: conv.ToPGText("published_acceptance_unverified"), GrantSource: source, RemoteSessionClientID: conv.ToNullUUID(in.ClientID), RequestedScopes: in.Scopes})
				require.NoError(t, err)
			case "new_selection":
				in.Resource = "https://resource.example.com/another"
			case "replacement_selection":
				in.ClientID = preparationManualClient(t, ctx, ti, *auth.ProjectID, in.RemoteSessionIssuerID, "cimd-replacement")
				preparationRecordGrants(t, ctx, ti, in.ClientID, grants)
			}
			require.NoError(t, fixtures.SetPreparationFixtureCIMDURI(ctx, testrepo.SetPreparationFixtureCIMDURIParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)}))
			key := repo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource}
			require.NoError(t, q.EnsureEMABinding(ctx, repo.EnsureEMABindingParams(key)))
			before, err := q.GetEMABinding(ctx, key)
			require.NoError(t, err)
			in.Mechanism = "cimd"
			in.ExpectedGeneration = before.Generation
			result, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, "unknown_grants", result.State)
			require.Equal(t, before.Generation, result.Generation)
			after, err := q.GetEMABinding(ctx, key)
			require.NoError(t, err)
			require.Equal(t, before, after, "CIMD selection must not promote provenance or persist a rebind")
			recorded, err := fixtures.GetPreparationFixtureClientGrants(ctx, testrepo.GetPreparationFixtureClientGrantsParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
			require.NoError(t, err)
			require.Equal(t, grants, recorded)
			in.ConfirmGrants = grants
			confirmed, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, "published_acceptance_unverified", confirmed.State)
			require.Equal(t, "cimd_published", confirmed.GrantSource)
			require.Equal(t, before.Generation+1, confirmed.Generation)
			require.Equal(t, in.ClientID, confirmed.ClientID)
			require.Equal(t, grants, confirmed.GrantTypes)
			in.ConfirmGrants = nil
			in.ExpectedGeneration = confirmed.Generation
			repeated, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, confirmed, repeated, "recorded evidence can be reused without redeclaring grants")
		})
	}
}

func TestSyncClaim_StaleSelectionMasksUnpersistedIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{oauthwire.GrantTypeJWTBearer})
	first, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	auth, _ := contextvalues.GetAuthContext(ctx)
	in.ClientID = preparationManualClient(t, ctx, ti, *auth.ProjectID, in.RemoteSessionIssuerID, "unpersisted-replacement")
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{oauthwire.GrantTypeJWTBearer, "refresh_token"})
	in.ExpectedGeneration = first.Generation - 1
	stale, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "configuration_required", stale.State)
	require.Equal(t, first.Generation, stale.Generation)
	require.Equal(t, uuid.Nil, stale.ClientID)
	require.Empty(t, stale.ExternalClientID)
	require.Empty(t, stale.GrantTypes)
	in.ClientID = uuid.Nil
	current, err := ti.service.ReadIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, first.ClientID, current.ClientID)
	require.Equal(t, first.ExternalClientID, current.ExternalClientID)
	require.Equal(t, first.GrantTypes, current.GrantTypes)
}
