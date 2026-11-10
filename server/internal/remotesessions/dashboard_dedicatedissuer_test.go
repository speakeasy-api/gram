package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// With upstream auth enforcement off, one user session issuer serves its MCP
// servers through one client, so a server that needs another client has to
// leave the issuer. A commit made with dedicated_issuer does that in the same
// transaction as the save, and only when the save would otherwise be refused.

const dedicatedIssuerCimdURL = "https://client.example.com/oauth/client-metadata.json"

func serverUserSessionIssuer(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID) uuid.UUID {
	t.Helper()
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: projectIDFromContext(t, ctx),
	})
	require.NoError(t, err)
	require.True(t, server.UserSessionIssuerID.Valid)
	return server.UserSessionIssuerID.UUID
}

func dedicatedIssuerOf(t *testing.T, result *gen.CommitServerIdentityConfigurationResult) uuid.UUID {
	t.Helper()
	require.NotNil(t, result.DedicatedIssuerID)
	id, err := uuid.Parse(*result.DedicatedIssuerID)
	require.NoError(t, err)
	return id
}

func TestCommitServerIdentityConfigurationMovesTheServerToADedicatedIssuer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// sameProvider configures the second server with the first server's
		// provider rather than another one.
		sameProvider bool
	}{
		{name: "another provider", sameProvider: false},
		{name: "the same provider", sameProvider: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			orgID := activeOrganizationID(t, ctx)
			projectID := projectIDFromContext(t, ctx)
			shared := seedSharedIssuer(t, ctx, ti, "dedicated-move", "")
			issuers := usersessionsrepo.New(ti.conn)
			_, err := issuers.UpdateUserSessionIssuer(ctx, usersessionsrepo.UpdateUserSessionIssuerParams{
				Slug:                          pgtype.Text{String: "", Valid: false},
				AuthnChallengeMode:            conv.ToPGText("chain"),
				SessionDuration:               pgtype.Interval{Microseconds: (3 * time.Hour).Microseconds(), Days: 0, Months: 0, Valid: true},
				ClientIDMetadataAdmissionMode: conv.ToPGText("presets"),
				ID:                            shared.userIssuerID,
				ProjectID:                     projectID,
			})
			require.NoError(t, err)
			_, err = issuers.CreateUserSessionIssuerCimdClient(ctx, usersessionsrepo.CreateUserSessionIssuerCimdClientParams{
				ClientIDMetadataUri: dedicatedIssuerCimdURL,
				UserSessionIssuerID: shared.userIssuerID,
				ProjectID:           projectID,
			})
			require.NoError(t, err)
			providerID := shared.firstProvider
			if !tc.sameProvider {
				providerID = createServerIdentityProvider(t, ctx, ti, "dedicated-move-second-provider", "", false, []string{"none"})
			}
			serverUpdatesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
			require.NoError(t, err)

			payload := manualExistingProviderIdentityPayload(shared.second, providerID, "dedicated-move-second-client")
			payload.DedicatedIssuer = new(true)
			result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
			require.NoError(t, err)
			require.Equal(t, "registered", *result.Status)
			secondClient, err := uuid.Parse(result.Client.ID)
			require.NoError(t, err)

			dedicated := dedicatedIssuerOf(t, result)
			require.NotEqual(t, shared.userIssuerID, dedicated)
			require.Equal(t, dedicated, serverUserSessionIssuer(t, ctx, ti, shared.second))
			require.Equal(t, shared.userIssuerID, serverUserSessionIssuer(t, ctx, ti, shared.first))

			// The new issuer signs people in the way the one it left does.
			issuer, err := issuers.GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{ID: dedicated, ProjectID: projectID, OrganizationID: orgID})
			require.NoError(t, err)
			require.Equal(t, conv.ToNullUUID(projectID), issuer.ProjectID)
			require.Equal(t, "chain", issuer.AuthnChallengeMode)
			require.Equal(t, (3 * time.Hour).Microseconds(), issuer.SessionDuration.Microseconds)
			require.Equal(t, conv.ToPGText("presets"), issuer.ClientIDMetadataAdmissionMode)
			require.Equal(t, "endpoint", issuer.AuthorizationServerMode)
			require.False(t, issuer.TrustedRemoteSessionIssuerID.Valid)
			admitted, err := issuers.ListUserSessionIssuerCimdClientsByIssuerID(ctx, usersessionsrepo.ListUserSessionIssuerCimdClientsByIssuerIDParams{
				ProjectID:           projectID,
				OrganizationID:      orgID,
				UserSessionIssuerID: dedicated,
				Cursor:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				LimitValue:          10,
			})
			require.NoError(t, err)
			require.Len(t, admitted, 1)
			require.Equal(t, dedicatedIssuerCimdURL, admitted[0].ClientIDMetadataUri)

			requireClientBound(t, ctx, ti, secondClient, dedicated, true)
			requireClientBound(t, ctx, ti, secondClient, shared.userIssuerID, false)
			require.Equal(t, []uuid.UUID{secondClient}, associatedClients(t, ctx, ti.conn, projectID, shared.second))
			require.Equal(t, conv.ToNullUUID(providerID), storedIssuer(t, ctx, ti.conn, projectID, shared.second))
			requireFirstServerKeepsItsClient(t, ctx, ti, shared)

			serverUpdatesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
			require.NoError(t, err)
			require.Equal(t, serverUpdatesBefore+1, serverUpdatesAfter)

			// Each server is now the only consumer of its issuer, which is what
			// the upstream client migration needs to prove a client is its own.
			report, err := associationbackfill.Run(ctx, ti.conn, associationbackfill.Options{OrganizationID: orgID, Apply: false, PageSize: 0})
			require.NoError(t, err)
			outcomes := map[uuid.UUID]associationbackfill.Outcome{}
			for _, decided := range report.Results {
				outcomes[decided.MCPServerID] = decided.Outcome
			}
			require.Equal(t, associationbackfill.OutcomeSoleConsumer, outcomes[shared.second])
			require.Equal(t, associationbackfill.OutcomeSoleConsumer, outcomes[shared.first])
		})
	}
}

// Moving is a repair for a save the issuer cannot take, so a save it can take
// leaves the server where it is.
func TestCommitServerIdentityConfigurationKeepsTheIssuerWhenTheSaveFits(t *testing.T) {
	t.Parallel()

	t.Run("the server's own client", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		shared := seedSharedIssuer(t, ctx, ti, "dedicated-fits", "")

		payload := manualExistingProviderIdentityPayload(shared.first, shared.firstProvider, "dedicated-fits-next-client")
		payload.DedicatedIssuer = new(true)
		result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
		require.NoError(t, err)
		require.Nil(t, result.DedicatedIssuerID)
		require.Equal(t, shared.userIssuerID, serverUserSessionIssuer(t, ctx, ti, shared.first))
	})

	t.Run("under enforcement", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		enforceUpstreamAuth(t, ctx, ti.conn, activeOrganizationID(t, ctx))
		shared := seedSharedIssuer(t, ctx, ti, "dedicated-enforced", "")
		providerID := createServerIdentityProvider(t, ctx, ti, "dedicated-enforced-second-provider", "", false, []string{"none"})

		for _, targetServerOnly := range []bool{false, true} {
			payload := manualExistingProviderIdentityPayload(shared.second, providerID, "dedicated-enforced-second-client")
			payload.DedicatedIssuer = new(true)
			payload.TargetServerOnly = new(targetServerOnly)
			result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
			require.NoError(t, err)
			require.Nil(t, result.DedicatedIssuerID)
			require.Equal(t, shared.userIssuerID, serverUserSessionIssuer(t, ctx, ti, shared.second))
		}
		requireFirstServerKeepsItsClient(t, ctx, ti, shared)
	})
}

// A setup save must not touch bindings other servers share, so with
// dedicated_issuer it leaves a shared issuer whatever is bound to it.
func TestSetupSaveOnSharedIssuerMovesTheServerToADedicatedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "dedicated-setup-issuer")
	first, _ := sourceBackedTarget(t, ctx, ti, "dedicated-setup-first", userIssuerID)
	second, _ := sourceBackedTarget(t, ctx, ti, "dedicated-setup-second", userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "dedicated-setup-provider", "", false, []string{"none"})

	payload := manualExistingProviderIdentityPayload(second, providerID, "dedicated-setup-client")
	payload.TargetServerOnly = new(true)
	_, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	requireOopsCode(t, err, oops.CodeConflict)

	payload.DedicatedIssuer = new(true)
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.NoError(t, err)
	clientID, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)

	dedicated := dedicatedIssuerOf(t, result)
	require.Equal(t, dedicated, serverUserSessionIssuer(t, ctx, ti, second))
	require.Equal(t, userIssuerID, serverUserSessionIssuer(t, ctx, ti, first))
	requireClientBound(t, ctx, ti, clientID, dedicated, true)
	requireClientBound(t, ctx, ti, clientID, userIssuerID, false)
	require.Equal(t, []uuid.UUID{clientID}, associatedClients(t, ctx, ti.conn, projectID, second))
	require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, first))
}

// A registration the provider refuses stores no client, and the server must
// not be left on an empty issuer because of it.
func TestCommitServerIdentityConfigurationKeepsTheIssuerWhenRegistrationFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(registrationServer.Close)
	shared := seedSharedIssuer(t, ctx, ti, "dedicated-failed", registrationServer.URL)
	providerID := createServerIdentityProvider(t, ctx, ti, "dedicated-failed-second-provider", registrationServer.URL, false, []string{"client_secret_basic"})

	payload := autoServerIdentityPayload(shared.second, providerID)
	payload.DedicatedIssuer = new(true)
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result.Failure)
	require.Nil(t, result.DedicatedIssuerID)

	require.Equal(t, shared.userIssuerID, serverUserSessionIssuer(t, ctx, ti, shared.second))
	requireFirstServerKeepsItsClient(t, ctx, ti, shared)
}

func TestCommitServerIdentityConfigurationDedicatedIssuerNeedsProjectWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	shared := seedSharedIssuer(t, ctx, ti, "dedicated-authz", "")
	providerID := createServerIdentityProvider(t, ctx, ti, "dedicated-authz-second-provider", "", false, []string{"none"})
	clientID := createServerIdentityClient(t, ctx, ti, "dedicated-authz-second-client", providerID, false)
	payload := &gen.CommitServerIdentityConfigurationPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		McpServerID:         shared.second.String(),
		ProviderID:          conv.PtrEmpty(providerID.String()),
		TargetServerOnly:    nil,
		DedicatedIssuer:     new(true),
		CreateProvider:      nil,
		ClientMode:          "existing",
		RegistrationMethod:  nil,
		ExistingClientID:    conv.PtrEmpty(clientID.String()),
		ClientConfiguration: nil,
	}

	serverWriteCtx := withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPWrite, shared.first.String()),
		authz.NewGrant(authz.ScopeMCPWrite, shared.second.String()),
	)
	_, err := ti.service.CommitServerIdentityConfiguration(serverWriteCtx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, shared.userIssuerID, serverUserSessionIssuer(t, ctx, ti, shared.second))

	projectWriteCtx := withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPWrite, shared.first.String()),
		authz.NewGrant(authz.ScopeMCPWrite, shared.second.String()),
		authz.NewGrant(authz.ScopeProjectWrite, projectID.String()),
	)
	result, err := ti.service.CommitServerIdentityConfiguration(projectWriteCtx, payload)
	require.NoError(t, err)
	require.Equal(t, "linked", *result.Status)
	require.Equal(t, dedicatedIssuerOf(t, result), serverUserSessionIssuer(t, ctx, ti, shared.second))
}

// Only an organization-level issuer can trust an external identity provider,
// so a server on one cannot keep that sign-in on an issuer created for it.
func TestCommitServerIdentityConfigurationRefusesADedicatedIssuerForATrustingIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	trustedProvider, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("dedicated-trusted-provider", nil))
	require.NoError(t, err)
	trustedProviderID, err := uuid.Parse(trustedProvider.ID)
	require.NoError(t, err)
	userIssuerID := createTrustedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "dedicated-trusting-issuer", trustedProviderID)
	shared := seedSharedIssuerOn(t, ctx, ti, "dedicated-trusting", "", userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "dedicated-trusting-second-provider", "", false, []string{"none"})

	payload := manualExistingProviderIdentityPayload(shared.second, providerID, "dedicated-trusting-second-client")
	payload.DedicatedIssuer = new(true)
	_, err = ti.service.CommitServerIdentityConfiguration(ctx, payload)
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "trusted identity provider")

	preview := impactPayload(shared.userIssuerID, &shared.second, "replace", &providerID, nil)
	preview.DedicatedIssuer = new(true)
	_, err = ti.service.GetServerIdentityImpact(ctx, preview)
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "trusted identity provider")

	require.Equal(t, shared.userIssuerID, serverUserSessionIssuer(t, ctx, ti, shared.second))
	requireFirstServerKeepsItsClient(t, ctx, ti, shared)
}

// The preview names what the move costs before anyone agrees to it.
func TestGetServerIdentityImpactForADedicatedIssuer(t *testing.T) {
	t.Parallel()

	t.Run("names the gateways that stop serving the server", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		projectID := projectIDFromContext(t, ctx)
		_, userIssuerID := createGatewayTarget(t, ctx, ti, "dedicated-impact-seed")
		shared := seedSharedIssuerOn(t, ctx, ti, "dedicated-impact", "", userIssuerID)
		providerID := createServerIdentityProvider(t, ctx, ti, "dedicated-impact-second-provider", "", false, []string{"none"})
		serving := createImpactGateway(t, ctx, ti, "Serving gateway", userIssuerID)
		_, err := metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{ProjectID: projectID, MetaMcpServerID: serving, McpServerID: shared.second, SortOrder: 0})
		require.NoError(t, err)
		// On the same issuer, without the server as a member.
		createImpactGateway(t, ctx, ti, "Other gateway", userIssuerID)

		preview := impactPayload(shared.userIssuerID, &shared.second, "replace", &providerID, nil)
		preview.DedicatedIssuer = new(true)
		result, err := ti.service.GetServerIdentityImpact(ctx, preview)
		require.NoError(t, err)
		require.Empty(t, result.Servers)
		require.NotNil(t, result.DedicatedIssuer)
		require.Len(t, result.DedicatedIssuer.Gateways, 1)
		require.Equal(t, serving.String(), result.DedicatedIssuer.Gateways[0].ID)
		require.Equal(t, "Serving gateway", *result.DedicatedIssuer.Gateways[0].Name)
		require.Zero(t, result.DedicatedIssuer.HiddenGatewayCount)
		require.False(t, result.DedicatedIssuer.OrganizationGateway)
	})

	t.Run("names the organization gateway", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "dedicated-impact-org-issuer")
		shared := seedSharedIssuerOn(t, ctx, ti, "dedicated-impact-org", "", userIssuerID)
		providerID := createServerIdentityProvider(t, ctx, ti, "dedicated-impact-org-second-provider", "", false, []string{"none"})

		preview := impactPayload(shared.userIssuerID, &shared.second, "replace", &providerID, nil)
		preview.DedicatedIssuer = new(true)
		result, err := ti.service.GetServerIdentityImpact(ctx, preview)
		require.NoError(t, err)
		require.NotNil(t, result.DedicatedIssuer)
		require.Empty(t, result.DedicatedIssuer.Gateways)
		require.True(t, result.DedicatedIssuer.OrganizationGateway)
	})

	t.Run("is absent when the issuer can take the change", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		shared := seedSharedIssuer(t, ctx, ti, "dedicated-impact-fits", "")

		preview := impactPayload(shared.userIssuerID, &shared.first, "replace", &shared.firstProvider, nil)
		preview.DedicatedIssuer = new(true)
		result, err := ti.service.GetServerIdentityImpact(ctx, preview)
		require.NoError(t, err)
		require.Nil(t, result.DedicatedIssuer)
	})
}
