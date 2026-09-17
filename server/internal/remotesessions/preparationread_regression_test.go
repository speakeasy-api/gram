package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestPreparationRead_DoesNotWaitForIssuerLock(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{preparationJWTGrant})
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", result.State)
	auth, _ := contextvalues.GetAuthContext(ctx)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err = repo.New(tx).LockEMAIssuer(ctx, repo.LockEMAIssuerParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID)})
	require.NoError(t, err)
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	current, err := ti.service.ReadIdentityChaining(readCtx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", current.State)
	require.Equal(t, result.Generation, current.Generation)
}

func TestPreparationRead_ProjectsStaleClaimWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	key := repo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource}
	require.NoError(t, q.EnsureEMABinding(ctx, repo.EnsureEMABindingParams(key)))
	b, err := q.GetEMABinding(ctx, key)
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: b.ID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID, Generation: b.Generation, ExpectedGeneration: b.Generation, State: conv.ToPGText("in_progress"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: []string{}, ClaimID: conv.ToNullUUID(uuid.New()), ClaimedAt: conv.ToPGTimestamptz(time.Now().Add(-2 * time.Minute))})
	require.NoError(t, err)
	before, err := q.GetEMABinding(ctx, key)
	require.NoError(t, err)
	in.ClientID = uuid.Nil
	current, err := ti.service.ReadIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "indeterminate", current.State)
	after, err := q.GetEMABinding(ctx, key)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestDetachUserSessionIssuer_OrganizationOwnedIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	user := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "detach-organization-user")
	_, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{ID: in.ClientID.String(), UserSessionIssuerID: user.String()})
	require.NoError(t, err)
	_, err = ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{ID: in.ClientID.String(), UserSessionIssuerID: user.String()})
	require.NoError(t, err)
	require.Equal(t, 0, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, in.ClientID, user))
}
