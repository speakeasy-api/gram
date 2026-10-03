package remotesessions_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	remotesessionshttp "github.com/speakeasy-api/gram/server/gen/http/remote_sessions/server"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestCommitServerIdentityConfigurationInitialBindingReplay(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "initial-binding")
	providerID := createServerIdentityProvider(t, ctx, ti, "initial-binding-provider", "", false, []string{"client_secret_post"})
	payload := autoServerIdentityPayload(targetID, providerID)
	payload.ClientMode = "manual"
	initialOnly := true
	payload.InitialBindingOnly = &initialOnly
	payload.ClientConfiguration.ClientID = conv.PtrEmpty("initial-client")
	payload.ClientConfiguration.ClientSecret = conv.PtrEmpty("test-secret")
	payload.ClientConfiguration.TokenEndpointAuthMethod = conv.PtrEmpty("client_secret_post")
	first, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.NoError(t, err)
	encoded, err := json.Marshal(remotesessionshttp.NewCommitServerIdentityConfigurationResponseBody(first))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "test-secret")
	stored, err := repo.New(ti.conn).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{ID: uuid.MustParse(first.Client.ID), ProjectID: projectIDFromContext(t, ctx), OrganizationID: activeOrganizationID(t, ctx)})
	require.NoError(t, err)
	require.True(t, stored.RemoteSessionClient.ClientSecretEncrypted.Valid)
	require.NotEqual(t, "test-secret", stored.RemoteSessionClient.ClientSecretEncrypted.String)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	_, err = ti.service.CommitServerIdentityConfiguration(ctx, payload)
	requireOopsCode(t, err, oops.CodeConflict)
	count, err := repo.New(ti.conn).CountRemoteSessionClientsByIssuerID(ctx, providerID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, before, after)
	requireClientBound(t, ctx, ti, uuid.MustParse(first.Client.ID), userIssuerID, true)
	initialOnly = false
	replacement, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.NoError(t, err)
	require.NotEqual(t, first.Client.ID, replacement.Client.ID)
}

func TestCommitServerIdentityConfigurationInitialBindingRejectsAuto(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "initial-auto")
	providerID := createServerIdentityProvider(t, ctx, ti, "initial-auto-provider", "", true, []string{"none"})
	payload := autoServerIdentityPayload(targetID, providerID)
	initialOnly := true
	payload.InitialBindingOnly = &initialOnly
	_, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCommitServerIdentityConfigurationInitialBindingRefusesCrossProjectProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "initial-foreign")
	otherProject := createProject(t, ctx, ti.conn, "initial-other-project")
	providerID := createRemoteIssuerInProject(t, ctx, ti.conn, otherProject, "initial-foreign-provider")
	payload := autoServerIdentityPayload(targetID, providerID)
	payload.ClientMode = "manual"
	initialOnly := true
	payload.InitialBindingOnly = &initialOnly
	payload.ClientConfiguration.ClientID = conv.PtrEmpty("foreign-client")
	payload.ClientConfiguration.TokenEndpointAuthMethod = conv.PtrEmpty("none")
	_, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
	count, err := repo.New(ti.conn).CountRemoteSessionClientsByIssuerID(ctx, providerID)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestIdentityCommitInitialBindingRefusesAfterCompetingPreflight(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "initial-race")
	providerID := createServerIdentityProvider(t, ctx, ti, "initial-race-provider", "", false, []string{"none"})
	plan := linkPlan(t, ctx, userIssuerID, providerID, uuid.Nil)
	plan.Bound = remotesessions.RequireUnbound
	plan.Provider = remotesessions.CreateProvider(repo.CreateRemoteSessionIssuerParams{
		ProjectID: conv.ToNullUUID(projectIDFromContext(t, ctx)), OrganizationID: conv.ToPGText(activeOrganizationID(t, ctx)),
		Slug: "losing-provider", Issuer: "https://idp.example.com/losing",
	})
	plan.Client = remotesessions.ManualClient(remotesessions.ClientCredentials{ClientID: "losing-client", TokenEndpointAuthMethod: conv.PtrEmpty("none")})
	loser := identityCommitter(t, ti).Prepare(plan)
	require.NoError(t, loser.Preflight(ctx))
	reg, err := loser.Register(ctx)
	require.NoError(t, err)
	payload := autoServerIdentityPayload(targetID, providerID)
	payload.ClientMode = "manual"
	initialOnly := true
	payload.InitialBindingOnly = &initialOnly
	payload.ClientConfiguration.ClientID = conv.PtrEmpty("winning-client")
	payload.ClientConfiguration.TokenEndpointAuthMethod = conv.PtrEmpty("none")
	winner, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.NoError(t, err)
	providerAuditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	tx, err := loser.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, loser.Lock(ctx, tx))
	require.ErrorIs(t, loser.Bind(ctx, tx, reg), remotesessions.ErrIdentityConflict)
	require.NoError(t, tx.Rollback(ctx))
	providerAuditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, providerAuditBefore, providerAuditAfter)
	_, err = repo.New(ti.conn).GetRemoteSessionIssuerBySlug(ctx, repo.GetRemoteSessionIssuerBySlugParams{Slug: "losing-provider", ProjectID: conv.ToNullUUID(projectIDFromContext(t, ctx))})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	requireClientBound(t, ctx, ti, uuid.MustParse(winner.Client.ID), userIssuerID, true)
	count, err := repo.New(ti.conn).CountRemoteSessionClientsByIssuerID(ctx, providerID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
