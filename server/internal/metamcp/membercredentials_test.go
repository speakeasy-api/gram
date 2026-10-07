package metamcp_test

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersessionbindings "github.com/speakeasy-api/gram/server/internal/usersessions/bindings"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// memberCredentialsFixture is a gateway whose two members front the same
// authorization server through distinct configured clients, the shape the
// gateway-member-credentials rollout targets.
type memberCredentialsFixture struct {
	ti              *testInstance
	flags           *feature.InMemory
	projectID       uuid.UUID
	orgID           string
	meta            *types.MetaMcpServer
	gatewayIssuerID uuid.UUID
	remoteIssuerID  uuid.UUID
	firstServer     uuid.UUID
	secondServer    uuid.UUID
	firstClient     uuid.UUID
	secondClient    uuid.UUID
}

func newMemberCredentialsFixture(t *testing.T, enabled bool) (context.Context, *memberCredentialsFixture) {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagGatewayMemberCredentials, orgID, enabled)
	ti.service.WithFeatureFlags(flags)

	meta := seedMetaMcpServer(t, ctx, ti, "member credentials host")
	require.NotNil(t, meta.UserSessionIssuerID, "create mints the gateway issuer")

	remoteIssuerID := seedRemoteSessionIssuer(t, ctx, ti.conn, projectID, orgID, "member-creds-rsi")
	firstServer := seedMcpServer(t, ctx, ti.conn, projectID)
	firstClient := createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, remoteIssuerID, "member-creds-client-1")
	stampAndWireMemberClient(t, ctx, ti.conn, projectID, firstServer, remoteIssuerID, firstClient)
	secondServer := seedMcpServer(t, ctx, ti.conn, projectID)
	secondClient := createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, remoteIssuerID, "member-creds-client-2")
	stampAndWireMemberClient(t, ctx, ti.conn, projectID, secondServer, remoteIssuerID, secondClient)

	return ctx, &memberCredentialsFixture{
		ti:              ti,
		flags:           flags,
		projectID:       projectID,
		orgID:           orgID,
		meta:            meta,
		gatewayIssuerID: uuid.MustParse(*meta.UserSessionIssuerID),
		remoteIssuerID:  remoteIssuerID,
		firstServer:     firstServer,
		secondServer:    secondServer,
		firstClient:     firstClient,
		secondClient:    secondClient,
	}
}

func (f *memberCredentialsFixture) setEnabled(enabled bool) {
	f.flags.SetFlag(feature.FlagGatewayMemberCredentials, f.orgID, enabled)
}

func addGatewayMember(ctx context.Context, service *metamcp.Service, metaID string, serverID uuid.UUID) (string, error) {
	member, err := service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  metaID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	if err != nil {
		return "", fmt.Errorf("add gateway member: %w", err)
	}
	return member.ID, nil
}

func (f *memberCredentialsFixture) addMember(t *testing.T, ctx context.Context, serverID uuid.UUID) string {
	t.Helper()
	memberID, err := addGatewayMember(ctx, f.ti.service, f.meta.ID, serverID)
	require.NoError(t, err)
	return memberID
}

func (f *memberCredentialsFixture) removeMember(t *testing.T, ctx context.Context, memberID string) {
	t.Helper()
	require.NoError(t, f.ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               memberID,
	}))
}

func (f *memberCredentialsFixture) save(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := f.ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  f.meta.ID,
		Name:                f.meta.Name,
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)
}

func (f *memberCredentialsFixture) createGatewayOnIssuer(ctx context.Context, name string) error {
	_, err := f.ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                name,
		UserSessionIssuerID: conv.PtrEmpty(f.gatewayIssuerID.String()),
	})
	if err != nil {
		return fmt.Errorf("create gateway on issuer: %w", err)
	}
	return nil
}

// boundClients lists the clients of the fixture's remote issuer bound to a
// user session issuer, sorted for stable comparison.
func (f *memberCredentialsFixture) boundClients(t *testing.T, ctx context.Context, userSessionIssuerID uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := remotesessionsrepo.New(f.ti.conn).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           conv.ToNullUUID(f.projectID),
		OrganizationID:      conv.ToPGText(f.orgID),
	})
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.RemoteSessionIssuerID == f.remoteIssuerID {
			ids = append(ids, row.ClientID)
		}
	}
	slices.SortFunc(ids, compareUUID)
	return ids
}

func (f *memberCredentialsFixture) clients(ids ...uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, compareUUID)
	return out
}

// memberIssuer returns the member server's own user session issuer.
func (f *memberCredentialsFixture) memberIssuer(t *testing.T, ctx context.Context, serverID uuid.UUID) uuid.UUID {
	t.Helper()
	server, err := mcpserversrepo.New(f.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: f.projectID,
	})
	require.NoError(t, err)
	require.True(t, server.UserSessionIssuerID.Valid)
	return server.UserSessionIssuerID.UUID
}

func compareUUID(a, b uuid.UUID) int {
	return bytes.Compare(a[:], b[:])
}

func TestMemberCredentials_AddBindsEachMembersClient(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, true)
	f.addMember(t, ctx, f.firstServer)
	f.addMember(t, ctx, f.secondServer)

	require.Equal(t, f.clients(f.firstClient, f.secondClient), f.boundClients(t, ctx, f.gatewayIssuerID),
		"each member's own client must be bound to the gateway issuer")
}

func TestMemberCredentials_FlagOffKeepsOneClientPerProvider(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, false)
	f.addMember(t, ctx, f.firstServer)
	f.addMember(t, ctx, f.secondServer)
	f.save(t, ctx)

	require.Equal(t, f.clients(f.firstClient), f.boundClients(t, ctx, f.gatewayIssuerID),
		"without the rollout a gateway issuer keeps one client per provider")
}

func TestMemberCredentials_SaveReconcilesSkippedClient(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, false)
	f.addMember(t, ctx, f.firstServer)
	f.addMember(t, ctx, f.secondServer)
	require.Equal(t, f.clients(f.firstClient), f.boundClients(t, ctx, f.gatewayIssuerID))

	f.setEnabled(true)
	f.save(t, ctx)
	require.Equal(t, f.clients(f.firstClient, f.secondClient), f.boundClients(t, ctx, f.gatewayIssuerID),
		"saving the gateway binds the member client the original rule skipped")

	f.save(t, ctx)
	require.Equal(t, f.clients(f.firstClient, f.secondClient), f.boundClients(t, ctx, f.gatewayIssuerID),
		"reconciliation is idempotent")
}

func TestMemberCredentials_RemoveDetachesOnlyThatMembersClient(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, true)
	firstMember := f.addMember(t, ctx, f.firstServer)
	secondMember := f.addMember(t, ctx, f.secondServer)

	f.removeMember(t, ctx, firstMember)
	require.Equal(t, f.clients(f.secondClient), f.boundClients(t, ctx, f.gatewayIssuerID),
		"removing a member must not disconnect its sibling")

	require.Equal(t, f.clients(f.firstClient), f.boundClients(t, ctx, f.memberIssuer(t, ctx, f.firstServer)),
		"the removed member keeps its own client binding")

	f.removeMember(t, ctx, secondMember)
	require.Empty(t, f.boundClients(t, ctx, f.gatewayIssuerID))
}

func TestMemberCredentials_RemoveRewiresSurvivingMember(t *testing.T) {
	t.Parallel()

	// Built under the original rule: only the first member's client is bound.
	ctx, f := newMemberCredentialsFixture(t, false)
	firstMember := f.addMember(t, ctx, f.firstServer)
	f.addMember(t, ctx, f.secondServer)
	require.Equal(t, f.clients(f.firstClient), f.boundClients(t, ctx, f.gatewayIssuerID))

	f.setEnabled(true)
	f.removeMember(t, ctx, firstMember)
	require.Equal(t, f.clients(f.secondClient), f.boundClients(t, ctx, f.gatewayIssuerID),
		"the surviving member gets its own client instead of the removed member's")
}

func TestMemberCredentials_SharedIssuerKeepsOneClientPerProvider(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, true)

	// A second gateway on the same issuer makes it shared, so the original
	// single-client rule still applies.
	require.NoError(t, f.createGatewayOnIssuer(ctx, "sharing gateway"))

	f.addMember(t, ctx, f.firstServer)
	f.addMember(t, ctx, f.secondServer)
	f.save(t, ctx)
	require.Equal(t, f.clients(f.firstClient), f.boundClients(t, ctx, f.gatewayIssuerID),
		"a shared issuer keeps one client per provider")
}

func TestMemberCredentials_RemoveOnSharedIssuerKeepsLegacyBinding(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, true)
	firstMember := f.addMember(t, ctx, f.firstServer)
	// While the issuer holds one client per provider another consumer may
	// join it, after which the original single-client rule applies.
	require.NoError(t, f.createGatewayOnIssuer(ctx, "sharing gateway"))
	f.addMember(t, ctx, f.secondServer)
	require.Equal(t, f.clients(f.firstClient), f.boundClients(t, ctx, f.gatewayIssuerID))

	f.removeMember(t, ctx, firstMember)
	require.Equal(t, f.clients(f.firstClient), f.boundClients(t, ctx, f.gatewayIssuerID),
		"a shared issuer keeps the client its other consumers rely on")
}

func TestMemberCredentials_RemoveAfterRolloutOffDetachesOwnClient(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, true)
	firstMember := f.addMember(t, ctx, f.firstServer)
	f.addMember(t, ctx, f.secondServer)
	require.Len(t, f.boundClients(t, ctx, f.gatewayIssuerID), 2)

	f.setEnabled(false)
	f.removeMember(t, ctx, firstMember)
	require.Equal(t, f.clients(f.secondClient), f.boundClients(t, ctx, f.gatewayIssuerID),
		"per-member clients do not outlive their members once the rollout is off")
}

func TestMemberCredentials_IssuerChangeRejectsInvisibleIssuer(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, true)
	_, err := f.ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  f.meta.ID,
		Name:                f.meta.Name,
		UserSessionIssuerID: conv.PtrEmpty(uuid.NewString()),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestMemberCredentials_IssuerCannotGainAnotherConsumer(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, true)
	f.addMember(t, ctx, f.firstServer)
	f.addMember(t, ctx, f.secondServer)
	require.Len(t, f.boundClients(t, ctx, f.gatewayIssuerID), 2)

	requireOopsCode(t, f.createGatewayOnIssuer(ctx, "second gateway"), oops.CodeConflict)

	// Servers and toolsets bind issuers through the same check.
	tx := testenv.BeginTx(t, ctx, f.ti.conn)
	_, err := usersessionbindings.ValidateAndLock(ctx, tx, f.gatewayIssuerID, f.projectID, f.orgID)
	require.ErrorIs(t, err, usersessionbindings.ErrGatewayMemberCredentials)
	require.NoError(t, tx.Rollback(ctx))

	// The owning gateway keeps saving normally.
	f.save(t, ctx)
}

// requireIssuerReusable asserts that another consumer may bind the issuer.
func (f *memberCredentialsFixture) requireIssuerReusable(t *testing.T, ctx context.Context, issuerID uuid.UUID) {
	t.Helper()
	tx := testenv.BeginTx(t, ctx, f.ti.conn)
	_, err := usersessionbindings.ValidateAndLock(ctx, tx, issuerID, f.projectID, f.orgID)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))
}

func TestMemberCredentials_IssuerChangeReleasesPreviousIssuer(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, true)
	f.addMember(t, ctx, f.firstServer)
	f.addMember(t, ctx, f.secondServer)
	require.Len(t, f.boundClients(t, ctx, f.gatewayIssuerID), 2)

	newIssuerID := seedUserSessionIssuer(t, ctx, f.ti.conn, f.projectID)
	_, err := f.ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  f.meta.ID,
		Name:                f.meta.Name,
		UserSessionIssuerID: conv.PtrEmpty(newIssuerID.String()),
	})
	require.NoError(t, err)

	require.Equal(t, f.clients(f.firstClient, f.secondClient), f.boundClients(t, ctx, newIssuerID),
		"the new issuer gets each member's client")
	require.Empty(t, f.boundClients(t, ctx, f.gatewayIssuerID),
		"the previous issuer drops the per-member credentials it no longer serves")
	f.requireIssuerReusable(t, ctx, f.gatewayIssuerID)
}

func TestMemberCredentials_DeleteReleasesIssuer(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, true)
	f.addMember(t, ctx, f.firstServer)
	f.addMember(t, ctx, f.secondServer)
	require.Len(t, f.boundClients(t, ctx, f.gatewayIssuerID), 2)

	require.NoError(t, f.ti.service.DeleteMetaMcpServer(ctx, &gen.DeleteMetaMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               f.meta.ID,
	}))

	require.Empty(t, f.boundClients(t, ctx, f.gatewayIssuerID))
	f.requireIssuerReusable(t, ctx, f.gatewayIssuerID)
}

// Member wiring takes the gateway issuer's owner-binding lock, the same lock
// manual client attachment and server/toolset issuer writers take, so none of
// them can change the issuer's consumers or clients mid-wiring.
func TestMemberCredentials_AddSerializesOnGatewayIssuerLock(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberCredentialsFixture(t, true)

	const probeTimeout = 100 * time.Millisecond
	pool := testenv.NewLockTimeoutPool(t, f.ti.conn, probeTimeout)
	logger := testenv.NewLogger(t)
	probeService := metamcp.NewService(logger, testenv.NewTracerProvider(t), pool, f.ti.sessionManager, authz.NewEngine(logger, pool, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), audit.NewLogger(), nil, networkaccess.DenyAllChecker{}).
		WithFeatureFlags(f.flags)

	holder := testenv.BeginTx(t, ctx, f.ti.conn)
	require.NoError(t, usersessionsrepo.New(holder).LockUserSessionIssuerForOwnerBinding(ctx, f.gatewayIssuerID))
	_, err := addGatewayMember(ctx, probeService, f.meta.ID, f.firstServer)
	testenv.RequireLockNotAvailable(t, err)
	require.NoError(t, holder.Rollback(ctx))

	_, err = addGatewayMember(ctx, probeService, f.meta.ID, f.firstServer)
	require.NoError(t, err)
	require.Equal(t, f.clients(f.firstClient), f.boundClients(t, ctx, f.gatewayIssuerID))
}
