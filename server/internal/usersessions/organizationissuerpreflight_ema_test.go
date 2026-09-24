package usersessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestOrganizationIssuerPreflightActiveEMABindings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, auth)
	require.NotNil(t, auth.ProjectID)
	user := seedOrganizationTierIssuer(t, ctx, ti.conn, "ema-preflight-human")
	remote := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "ema-preflight-remote", uuid.NullUUID{}, pgtype.Text{String: auth.ActiveOrganizationID, Valid: true})
	q := remoterepo.New(ti.conn)
	require.NoError(t, q.EnsureEMABinding(ctx, remoterepo.EnsureEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: user, RemoteSessionIssuerID: remote, Resource: "https://resource.example.com/"}))

	updated, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{ID: user.String(), Slug: conv.PtrEmpty("ema-renamed")})
	require.NoError(t, err)
	require.Equal(t, "ema-renamed", updated.Slug)
	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{ID: user.String(), AuthnChallengeMode: conv.PtrEmpty("chain"), SessionDurationHours: conv.PtrEmpty(24)})
	require.NoError(t, err, "unchanged binding-sensitive fields remain editable")
	for _, patch := range []*orggen.UpdateIssuerPayload{
		{ID: user.String(), AuthnChallengeMode: conv.PtrEmpty("none")},
		{ID: user.String(), SessionDurationHours: conv.PtrEmpty(12)},
		{ID: user.String(), ClientIDMetadataAdmissionMode: conv.PtrEmpty("open")},
		{ID: user.String(), TrustedRemoteSessionIssuerID: conv.PtrEmpty(remote.String())},
		{ID: user.String(), TrustedRemoteSessionClientID: conv.PtrEmpty(uuid.NewString())},
	} {
		_, err := ti.service.UpdateIssuer(ctx, patch)
		var shared *oops.ShareableError
		require.ErrorAs(t, err, &shared)
		require.Equal(t, oops.CodeConflict, shared.Code)
	}
	preflight, err := ti.service.GetIssuerDeletePreflight(ctx, &orggen.GetIssuerDeletePreflightPayload{ID: user.String()})
	require.NoError(t, err)
	require.Equal(t, int64(1), preflight.EmaBindingCount)
	require.Empty(t, preflight.McpServers)
	require.Empty(t, preflight.Toolsets)
	require.False(t, preflight.CanDelete)
	err = ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: user.String()})
	var shared *oops.ShareableError
	require.ErrorAs(t, err, &shared)
	require.Equal(t, oops.CodeConflict, shared.Code)
	binding, err := q.GetEMABinding(ctx, remoterepo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: user, RemoteSessionIssuerID: remote, Resource: "https://resource.example.com/"})
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, remoterepo.SetEMABindingParams{ID: binding.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ExpectedGeneration: binding.Generation, Generation: binding.Generation + 1, State: conv.ToPGText("unlinked"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: []string{}})
	require.NoError(t, err)
	preflight, err = ti.service.GetIssuerDeletePreflight(ctx, &orggen.GetIssuerDeletePreflightPayload{ID: user.String()})
	require.NoError(t, err)
	require.Zero(t, preflight.EmaBindingCount)
	require.True(t, preflight.CanDelete)
	require.NoError(t, ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: user.String()}))
	count, err := testrepo.New(ti.conn).CountPreparationFixtureBindingByID(ctx, testrepo.CountPreparationFixtureBindingByIDParams{ID: binding.ID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	require.Zero(t, count, "user issuer deletion explicitly removes unlinked claims")
}

// Preparation holds LockEMAUserIssuer through inserting its binding. A
// lifecycle writer must wait, then see the binding committed by preparation.
func TestOrganizationIssuerMutationWaitsForEMAPreparation(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"delete", "update"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			baseCtx, ti := newTestService(t)
			ctx, cancel := context.WithTimeout(baseCtx, 30*time.Second)
			defer cancel()
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, auth)
			require.NotNil(t, auth.ProjectID)
			user := seedOrganizationTierIssuer(t, ctx, ti.conn, "ema-serialize-human")
			remote := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "ema-serialize-remote", uuid.NullUUID{}, conv.ToPGText(auth.ActiveOrganizationID))
			tx := testenv.BeginTx(t, ctx, ti.conn)
			q := remoterepo.New(tx)
			_, err := q.LockEMAUserIssuer(ctx, remoterepo.LockEMAUserIssuerParams{ID: user, OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), ProjectID: conv.ToNullUUID(*auth.ProjectID)})
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				if mutation == "delete" {
					done <- ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: user.String()})
				} else {
					_, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{ID: user.String(), SessionDurationHours: conv.PtrEmpty(12)})
					done <- err
				}
			}()
			pattern := "%LockEMAUserIssuer :one%"
			if mutation == "update" {
				pattern = "%LockOrganizationUserSessionIssuer :one%"
			}
			require.Eventually(t, func() bool {
				blocked, err := testrepo.New(ti.conn).IsQueryBlockedOnLockFixture(ctx, pattern)
				return err == nil && blocked
			}, 10*time.Second, 10*time.Millisecond)
			require.Empty(t, done, "mutation must not pass the preparation row lock")
			require.NoError(t, q.EnsureEMABinding(ctx, remoterepo.EnsureEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: user, RemoteSessionIssuerID: remote, Resource: "https://resource.example.com/"}))
			require.NoError(t, tx.Commit(ctx))
			select {
			case err := <-done:
				var shared *oops.ShareableError
				require.ErrorAs(t, err, &shared)
				require.Equal(t, oops.CodeConflict, shared.Code)
			case <-ctx.Done():
				t.Fatal("issuer mutation did not finish after preparation committed")
			}
		})
	}
}

func TestProjectIssuerUpdateActiveEMABindings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, auth)
	require.NotNil(t, auth.ProjectID)
	user, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		Slug: "ema-project-human", AuthnChallengeMode: "chain", SessionDurationHours: 24,
	})
	require.NoError(t, err)
	remote := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "ema-project-remote", uuid.NullUUID{}, conv.ToPGText(auth.ActiveOrganizationID))
	q := remoterepo.New(ti.conn)
	require.NoError(t, q.EnsureEMABinding(ctx, remoterepo.EnsureEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: uuid.MustParse(user.ID), RemoteSessionIssuerID: remote, Resource: "https://resource.example.com/"}))
	for _, patch := range []*gen.UpdateUserSessionIssuerPayload{
		{ID: user.ID, Slug: conv.PtrEmpty("ema-project-renamed")},
		{ID: user.ID},
		{ID: user.ID, AuthnChallengeMode: conv.PtrEmpty("chain"), SessionDurationHours: conv.PtrEmpty(24)},
	} {
		updated, err := ti.service.UpdateUserSessionIssuer(ctx, patch)
		require.NoError(t, err)
		require.Equal(t, "ema-project-renamed", updated.Slug)
		require.Equal(t, "chain", updated.AuthnChallengeMode)
		require.Equal(t, 24, updated.SessionDurationHours)
	}
	for _, patch := range []*gen.UpdateUserSessionIssuerPayload{
		{ID: user.ID, AuthnChallengeMode: conv.PtrEmpty("interactive")},
		{ID: user.ID, SessionDurationHours: conv.PtrEmpty(12)},
		{ID: user.ID, ClientIDMetadataAdmissionMode: conv.PtrEmpty("disabled")},
	} {
		_, err := ti.service.UpdateUserSessionIssuer(ctx, patch)
		requireOopsCode(t, err, oops.CodeConflict)
	}
	binding, err := q.GetEMABinding(ctx, remoterepo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: uuid.MustParse(user.ID), RemoteSessionIssuerID: remote, Resource: "https://resource.example.com/"})
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, remoterepo.SetEMABindingParams{ID: binding.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ExpectedGeneration: binding.Generation, Generation: binding.Generation + 1, State: conv.ToPGText("unlinked"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: []string{}})
	require.NoError(t, err)
	updated, err := ti.service.UpdateUserSessionIssuer(ctx, &gen.UpdateUserSessionIssuerPayload{ID: user.ID, SessionDurationHours: conv.PtrEmpty(12)})
	require.NoError(t, err)
	require.Equal(t, 12, updated.SessionDurationHours)
}
