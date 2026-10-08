package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func preparationAuditCounts(t *testing.T, ctx context.Context, ti *testInstance) map[audit.Action]int64 {
	t.Helper()
	counts := map[audit.Action]int64{}
	for _, action := range []audit.Action{
		audit.ActionRemoteSessionClientEnableIdentityChaining,
		audit.ActionRemoteSessionClientUpdateIdentityChainingScopes,
		audit.ActionRemoteSessionClientDisableIdentityChaining,
	} {
		n, err := audittest.AuditLogCountByAction(ctx, ti.conn, action)
		require.NoError(t, err)
		counts[action] = n
	}
	return counts
}

func requirePreparationAuditDelta(t *testing.T, before, after map[audit.Action]int64, enable, scopes, disable int64) {
	t.Helper()
	require.Equal(t, before[audit.ActionRemoteSessionClientEnableIdentityChaining]+enable, after[audit.ActionRemoteSessionClientEnableIdentityChaining], "enable events")
	require.Equal(t, before[audit.ActionRemoteSessionClientUpdateIdentityChainingScopes]+scopes, after[audit.ActionRemoteSessionClientUpdateIdentityChainingScopes], "scope update events")
	require.Equal(t, before[audit.ActionRemoteSessionClientDisableIdentityChaining]+disable, after[audit.ActionRemoteSessionClientDisableIdentityChaining], "disable events")
}

func TestPreparationAudit_EnableScopesAndUnlink(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}

	start := preparationAuditCounts(t, ctx, ti)
	enabled, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", enabled.State)
	afterEnable := preparationAuditCounts(t, ctx, ti)
	requirePreparationAuditDelta(t, start, afterEnable, 1, 0, 0)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionClientEnableIdentityChaining)
	require.NoError(t, err)
	require.Equal(t, in.ClientID.String(), record.SubjectID)
	require.Equal(t, "remote_session_client", record.SubjectType)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, enabled.BindingID.String(), metadata["binding_id"])
	require.Equal(t, in.UserSessionIssuerID.String(), metadata["user_session_issuer_id"])
	require.Equal(t, in.Resource, metadata["resource"])
	require.Equal(t, []any{"openid"}, metadata["scopes"])

	replayed, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, enabled.Generation, replayed.Generation)
	afterReplay := preparationAuditCounts(t, ctx, ti)
	requirePreparationAuditDelta(t, afterEnable, afterReplay, 0, 0, 0)

	in.Scopes = []string{"read"}
	in.ExpectedGeneration = enabled.Generation
	rescoped, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, []string{"read"}, rescoped.Scopes)
	afterScopes := preparationAuditCounts(t, ctx, ti)
	requirePreparationAuditDelta(t, afterReplay, afterScopes, 0, 1, 0)

	record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdateIdentityChainingScopes)
	require.NoError(t, err)
	metadata, err = audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{"openid"}, metadata["previous_scopes"])
	require.Equal(t, []any{"read"}, metadata["scopes"])

	in.ExpectedGeneration = rescoped.Generation
	_, err = ti.service.UnlinkIdentityChaining(ctx, in)
	require.NoError(t, err)
	afterUnlink := preparationAuditCounts(t, ctx, ti)
	requirePreparationAuditDelta(t, afterScopes, afterUnlink, 0, 0, 1)

	record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDisableIdentityChaining)
	require.NoError(t, err)
	require.Equal(t, in.ClientID.String(), record.SubjectID)
	require.Equal(t, "downstream-client", record.SubjectDisplay)
}

func TestPreparationAudit_UnlinkWithoutBindingRecordsNothing(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)

	start := preparationAuditCounts(t, ctx, ti)
	_, err := ti.service.UnlinkIdentityChaining(ctx, in)
	require.NoError(t, err)
	requirePreparationAuditDelta(t, start, preparationAuditCounts(t, ctx, ti), 0, 0, 0)
}

func TestPreparationAudit_EnableOnlyWhenReady(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{})

	start := preparationAuditCounts(t, ctx, ti)
	manual, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "manual_setup_required", manual.State)
	afterManual := preparationAuditCounts(t, ctx, ti)
	requirePreparationAuditDelta(t, start, afterManual, 0, 0, 0)

	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	in.ExpectedGeneration = manual.Generation
	ready, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", ready.State)
	requirePreparationAuditDelta(t, afterManual, preparationAuditCounts(t, ctx, ti), 1, 0, 0)
}

func TestPreparationAudit_MoveClientRecordsBothClients(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	enabled, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", enabled.State)

	auth, _ := contextvalues.GetAuthContext(ctx)
	previous := in.ClientID
	in.ClientID = preparationManualClient(t, ctx, ti, *auth.ProjectID, in.RemoteSessionIssuerID, "replacement-client")
	in.ExpectedGeneration = enabled.Generation
	start := preparationAuditCounts(t, ctx, ti)
	moved, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", moved.State)
	requirePreparationAuditDelta(t, start, preparationAuditCounts(t, ctx, ti), 1, 0, 1)

	disabled, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDisableIdentityChaining)
	require.NoError(t, err)
	require.Equal(t, previous.String(), disabled.SubjectID)
	require.Equal(t, "downstream-client", disabled.SubjectDisplay)

	enabledRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionClientEnableIdentityChaining)
	require.NoError(t, err)
	require.Equal(t, in.ClientID.String(), enabledRecord.SubjectID)
	require.Equal(t, "replacement-client", enabledRecord.SubjectDisplay)
}

func TestPreparationAudit_AgentActorHasNoUserDisplayName(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	withEmail := *auth
	withEmail.Email = new("human@example.com")
	agentID := uuid.NewString()
	ctx = contextvalues.WithAuthenticatedActor(ctx, &withEmail, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID))

	enabled, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", enabled.State)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionClientEnableIdentityChaining)
	require.NoError(t, err)
	require.Equal(t, string(urn.PrincipalTypeAgent), record.ActorType)
	require.Equal(t, agentID, record.ActorID)
	require.Nil(t, record.ActorDisplayName)
}
