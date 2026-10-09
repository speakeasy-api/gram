package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func impactPayload(userIssuerID uuid.UUID, targetID *uuid.UUID, change string, providerID, clientID *uuid.UUID) *gen.GetServerIdentityImpactPayload {
	str := func(id *uuid.UUID) *string {
		if id == nil {
			return nil
		}
		return conv.PtrEmpty(id.String())
	}
	return &gen.GetServerIdentityImpactPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: userIssuerID.String(),
		McpServerID:         str(targetID),
		Change:              change,
		ProviderID:          str(providerID),
		ClientID:            str(clientID),
	}
}

func createImpactServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, slug string, userIssuerID uuid.UUID) uuid.UUID {
	t.Helper()
	remote, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		Name:          conv.ToPGText(slug),
		Slug:          conv.ToPGText(slug),
		TransportType: "streamable-http",
		Url:           "https://mcp.example.com/" + slug,
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		UserSessionIssuerID: conv.ToNullUUID(userIssuerID),
		RemoteMcpServerID:   conv.ToNullUUID(remote.ID),
		Visibility:          "private",
	})
	require.NoError(t, err)
	return server.ID
}

// createGatewayTarget makes a target on a project issuer stamped with the
// organization, which a gateway's issuer reference requires.
func createGatewayTarget(t *testing.T, ctx context.Context, ti *testInstance, slug string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectIDFromContext(t, ctx),
		OrganizationID:     conv.ToPGText(activeOrganizationID(t, ctx)),
		Slug:               slug + "-issuer",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	return createServerIdentityTargetOnIssuer(t, ctx, ti, slug, issuer.ID), issuer.ID
}

func createImpactGateway(t *testing.T, ctx context.Context, ti *testInstance, name string, userIssuerID uuid.UUID) uuid.UUID {
	t.Helper()
	gateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      activeOrganizationID(t, ctx),
		ProjectID:           projectIDFromContext(t, ctx),
		Name:                name,
		UserSessionIssuerID: conv.ToNullUUID(userIssuerID),
		Visibility:          "private",
		NetworkAccessMode:   pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return gateway.ID
}

func impactsByServer(result *gen.ServerIdentityImpactResult) map[string]string {
	out := map[string]string{}
	for _, server := range result.Servers {
		out[server.ID] = server.Impact
	}
	return out
}

func TestGetServerIdentityImpactProjectIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "impact-target")
	siblingID := createImpactServer(t, ctx, ti, projectID, "impact-sibling", userIssuerID)
	otherIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "impact-other-issuer")
	createImpactServer(t, ctx, ti, projectID, "impact-unrelated", otherIssuerID)

	currentProviderID := createServerIdentityProvider(t, ctx, ti, "impact-current", "", false, []string{"none"})
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "impact-next", "", false, []string{"none"})
	bindServerIdentityClient(t, ctx, ti, "impact-current-client", currentProviderID, userIssuerID, false)

	cases := []struct {
		name       string
		change     string
		providerID *uuid.UUID
		want       string
	}{
		{name: "another provider repoints", change: "replace", providerID: &nextProviderID, want: "repoint"},
		{name: "a new provider repoints", change: "replace", providerID: nil, want: "repoint"},
		{name: "same provider swaps the client", change: "replace", providerID: &currentProviderID, want: "resignin"},
		{name: "attaching a second provider clears", change: "attach", providerID: &nextProviderID, want: "clear"},
		{name: "detach clears", change: "detach", providerID: nil, want: "clear"},
	}
	for _, tc := range cases {
		result, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(userIssuerID, &targetID, tc.change, tc.providerID, nil))
		require.NoError(t, err, tc.name)
		require.Equal(t, map[string]string{siblingID.String(): tc.want}, impactsByServer(result), tc.name)
		require.Zero(t, result.HiddenServerCount, tc.name)
	}

	// Without a target nothing is excluded.
	result, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(userIssuerID, nil, "replace", &nextProviderID, nil))
	require.NoError(t, err)
	require.Equal(t, map[string]string{siblingID.String(): "repoint", targetID.String(): "repoint"}, impactsByServer(result))
}

func TestGetServerIdentityImpactUnchangedUpstreamIsEmpty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "impact-noop-target")
	createImpactServer(t, ctx, ti, projectID, "impact-noop-sibling", userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "impact-noop-provider", "", false, []string{"none"})
	clientID := bindServerIdentityClient(t, ctx, ti, "impact-noop-client", providerID, userIssuerID, false)

	result, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(userIssuerID, &targetID, "replace", &providerID, &clientID))
	require.NoError(t, err)
	require.Empty(t, result.Servers)
	require.Zero(t, result.HiddenServerCount)
}

func TestGetServerIdentityImpactOrganizationIssuerAcrossProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "impact-org-issuer")
	targetID := createServerIdentityTargetOnIssuer(t, ctx, ti, "impact-org-target", userIssuerID)
	siblingID := createImpactServer(t, ctx, ti, projectID, "impact-org-sibling", userIssuerID)
	otherProjectID := createProject(t, ctx, ti.conn, "impact-org-other")
	otherServerID := createImpactServer(t, ctx, ti, otherProjectID, "impact-org-other-server", userIssuerID)

	currentProviderID := createServerIdentityProvider(t, ctx, ti, "impact-org-current", "", false, []string{"none"})
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "impact-org-next", "", false, []string{"none"})
	bindServerIdentityClient(t, ctx, ti, "impact-org-project-client", currentProviderID, userIssuerID, false)

	// A project-owned client is invisible to other projects, so replacing it
	// leaves their upstream alone.
	result, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(userIssuerID, &targetID, "replace", &nextProviderID, nil))
	require.NoError(t, err)
	require.Equal(t, map[string]string{siblingID.String(): "repoint"}, impactsByServer(result))

	require.NotContains(t, impactsByServer(result), otherServerID.String())
	for _, server := range result.Servers {
		require.Equal(t, "mcp_server", server.Kind)
		require.Equal(t, projectID.String(), server.ProjectID)
	}

	// Binding an organization-level client would reach every project on the
	// issuer, which the commit refuses, so the preview refuses it too.
	orgClientID := createServerIdentityClient(t, ctx, ti, "impact-org-shared-client", nextProviderID, true)
	for _, change := range []string{"attach", "replace"} {
		_, err = ti.service.GetServerIdentityImpact(ctx, impactPayload(userIssuerID, &targetID, change, nil, &orgClientID))
		requireOopsCode(t, err, oops.CodeConflict)
		require.ErrorContains(t, err, remotesessions.OrgWideBindingMessage)
	}
	_, err = ti.service.CommitServerIdentityConfiguration(ctx, existingServerIdentityPayload(targetID, nextProviderID, orgClientID))
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, remotesessions.OrgWideBindingMessage)
}

func TestGetServerIdentityImpactRefusesUnbindingOrgClientOnOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "impact-org-unbind-issuer")
	targetID := createServerIdentityTargetOnIssuer(t, ctx, ti, "impact-org-unbind-target", userIssuerID)
	currentProviderID := createServerIdentityProvider(t, ctx, ti, "impact-org-unbind-current", "", false, []string{"none"})
	orgClientID := bindServerIdentityClient(t, ctx, ti, "impact-org-unbind-client", currentProviderID, userIssuerID, true)
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "impact-org-unbind-next", "", false, []string{"none"})

	for _, change := range []string{"detach", "replace"} {
		var providerID *uuid.UUID
		if change == "replace" {
			providerID = &nextProviderID
		}
		_, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(userIssuerID, &targetID, change, providerID, nil))
		requireOopsCode(t, err, oops.CodeConflict)
		require.ErrorContains(t, err, remotesessions.OrgWideBindingMessage)
	}

	// Keeping the already-bound client is allowed by both.
	result, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(userIssuerID, &targetID, "replace", &currentProviderID, &orgClientID))
	require.NoError(t, err)
	require.Empty(t, result.Servers)
	_, err = ti.service.CommitServerIdentityConfiguration(ctx, existingServerIdentityPayload(targetID, currentProviderID, orgClientID))
	require.NoError(t, err)
}

func TestGetServerIdentityImpactMatchesCommitOnDeletedProvider(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "impact-deleted-target")
	siblingID := createImpactServer(t, ctx, ti, projectID, "impact-deleted-sibling", userIssuerID)
	liveProviderID := createServerIdentityProvider(t, ctx, ti, "impact-deleted-live", "", false, []string{"none"})
	deletedProviderID := createServerIdentityProvider(t, ctx, ti, "impact-deleted-gone", "", false, []string{"none"})
	bindServerIdentityClient(t, ctx, ti, "impact-deleted-live-client", liveProviderID, userIssuerID, false)
	bindServerIdentityClient(t, ctx, ti, "impact-deleted-gone-client", deletedProviderID, userIssuerID, false)
	_, err := repo.New(ti.conn).DeleteRemoteSessionIssuer(ctx, repo.DeleteRemoteSessionIssuerParams{ID: deletedProviderID, ProjectID: conv.ToNullUUID(projectID)})
	require.NoError(t, err)

	// The commit still sees the deleted provider's bound client, and so does
	// the preview: two other providers make the replacement ambiguous.
	var requests atomic.Int64
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(registrationServer.Close)
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "impact-deleted-next", registrationServer.URL, false, []string{"client_secret_basic"})

	_, previewErr := ti.service.GetServerIdentityImpact(ctx, impactPayload(userIssuerID, &targetID, "replace", &nextProviderID, nil))
	_, commitErr := ti.service.CommitServerIdentityConfiguration(ctx, autoServerIdentityPayload(targetID, nextProviderID))
	requireOopsCode(t, previewErr, oops.CodeConflict)
	requireOopsCode(t, commitErr, oops.CodeConflict)
	var previewOops, commitOops *oops.ShareableError
	require.ErrorAs(t, previewErr, &previewOops)
	require.ErrorAs(t, commitErr, &commitOops)
	require.Equal(t, commitOops.Error(), previewOops.Error())
	require.Zero(t, requests.Load())

	// The deleted provider's client does not count toward the derivation, as
	// in the resync: the sibling derives the live provider until another joins.
	result, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(userIssuerID, &targetID, "attach", &nextProviderID, nil))
	require.NoError(t, err)
	require.Equal(t, map[string]string{siblingID.String(): "clear"}, impactsByServer(result))
}

func TestGetServerIdentityImpactListsGateways(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createGatewayTarget(t, ctx, ti, "impact-gateway-target")
	gatewayID := createImpactGateway(t, ctx, ti, "impact-gateway", userIssuerID)
	currentProviderID := createServerIdentityProvider(t, ctx, ti, "impact-gateway-current", "", false, []string{"none"})
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "impact-gateway-next", "", false, []string{"none"})
	bindServerIdentityClient(t, ctx, ti, "impact-gateway-client", currentProviderID, userIssuerID, false)

	for _, tc := range []struct {
		change     string
		providerID *uuid.UUID
		want       map[string]string
	}{
		{change: "replace", providerID: &nextProviderID, want: map[string]string{gatewayID.String(): "client_removed"}},
		{change: "detach", providerID: nil, want: map[string]string{gatewayID.String(): "client_removed"}},
		{change: "attach", providerID: &nextProviderID, want: map[string]string{}},
	} {
		result, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(userIssuerID, &targetID, tc.change, tc.providerID, nil))
		require.NoError(t, err, tc.change)
		require.Equal(t, tc.want, impactsByServer(result), tc.change)
		for _, server := range result.Servers {
			require.Equal(t, "gateway", server.Kind)
			require.Nil(t, server.Slug)
			require.Equal(t, "impact-gateway", conv.PtrValOr(server.Name, ""))
		}
	}
}

func TestGetServerIdentityImpactScopesToTenant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "impact-tenant-target")

	otherOrgID := createOrganization(t, ctx, ti.conn, "impact-tenant-other-org")
	foreignOrgIssuerID, err := testrepo.New(ti.conn).InsertOrganizationTierUserSessionIssuerFixture(ctx, testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{
		OrganizationID:     conv.ToPGText(otherOrgID),
		Slug:               "impact-tenant-foreign-org-issuer",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	_, err = ti.service.GetServerIdentityImpact(ctx, impactPayload(foreignOrgIssuerID, nil, "detach", nil, nil))
	requireOopsCode(t, err, oops.CodeNotFound)

	// A client or target of another project is not found from this one.
	otherProjectID := createProject(t, ctx, ti.conn, "impact-tenant-other-project")
	providerID := createServerIdentityProvider(t, ctx, ti, "impact-tenant-provider", "", false, []string{"none"})
	foreignClient, err := repo.New(ti.conn).CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(otherProjectID),
		OrganizationID:        conv.ToPGText(activeOrganizationID(t, ctx)),
		RemoteSessionIssuerID: providerID,
		ClientID:              "impact-tenant-foreign-client",
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)
	_, err = ti.service.GetServerIdentityImpact(ctx, impactPayload(userIssuerID, &targetID, "attach", nil, &foreignClient.ID))
	requireOopsCode(t, err, oops.CodeNotFound)

	foreignIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProjectID, "impact-tenant-foreign-usi")
	foreignTargetID := createImpactServer(t, ctx, ti, otherProjectID, "impact-tenant-foreign-target", foreignIssuerID)
	_, err = ti.service.GetServerIdentityImpact(ctx, impactPayload(foreignIssuerID, &foreignTargetID, "detach", nil, nil))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetServerIdentityImpactCountsUnreadableServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	targetID, userIssuerID := createGatewayTarget(t, ctx, ti, "impact-hidden-target")
	siblingID := createImpactServer(t, ctx, ti, projectID, "impact-hidden-sibling", userIssuerID)
	createImpactGateway(t, ctx, ti, "impact-hidden-gateway", userIssuerID)
	currentProviderID := createServerIdentityProvider(t, ctx, ti, "impact-hidden-current", "", false, []string{"none"})
	bindServerIdentityClient(t, ctx, ti, "impact-hidden-current-client", currentProviderID, userIssuerID, false)
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "impact-hidden-next", "", false, []string{"none"})
	orgClientID := createServerIdentityClient(t, ctx, ti, "impact-hidden-org-client", nextProviderID, true)

	readSiblingCtx := withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPWrite, targetID.String()),
		authz.NewGrant(authz.ScopeMCPRead, siblingID.String()),
	)
	// A project client: only affected servers count, and the gateway is not.
	result, err := ti.service.GetServerIdentityImpact(readSiblingCtx, impactPayload(userIssuerID, &targetID, "attach", &nextProviderID, nil))
	require.NoError(t, err)
	require.Equal(t, map[string]string{siblingID.String(): "clear"}, impactsByServer(result))
	require.Zero(t, result.HiddenServerCount)

	// An organization-level client: every unreadable server on the issuer
	// counts, including the unaffected gateway.
	result, err = ti.service.GetServerIdentityImpact(readSiblingCtx, impactPayload(userIssuerID, &targetID, "attach", nil, &orgClientID))
	require.NoError(t, err)
	require.Equal(t, map[string]string{siblingID.String(): "clear"}, impactsByServer(result))
	require.Equal(t, 1, result.HiddenServerCount)

	targetOnlyCtx := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, targetID.String()))
	result, err = ti.service.GetServerIdentityImpact(targetOnlyCtx, impactPayload(userIssuerID, &targetID, "attach", nil, &orgClientID))
	require.NoError(t, err)
	require.Empty(t, result.Servers)
	require.Equal(t, 2, result.HiddenServerCount)
}

func TestGetServerIdentityImpactRequiresWriteAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "impact-authz-target")

	readOnlyCtx := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, targetID.String()))
	_, err := ti.service.GetServerIdentityImpact(readOnlyCtx, impactPayload(userIssuerID, &targetID, "detach", nil, nil))
	requireOopsCode(t, err, oops.CodeForbidden)

	// Without a target the change needs project write.
	_, err = ti.service.GetServerIdentityImpact(readOnlyCtx, impactPayload(userIssuerID, nil, "detach", nil, nil))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetServerIdentityImpactScopesIssuerAndTarget(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "impact-scope-target")
	otherIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "impact-scope-other")

	_, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(otherIssuerID, &targetID, "detach", nil, nil))
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Another project's issuer is not visible from this one.
	otherProjectID := createProject(t, ctx, ti.conn, "impact-scope-project")
	foreignIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProjectID, "impact-scope-foreign")
	_, err = ti.service.GetServerIdentityImpact(ctx, impactPayload(foreignIssuerID, nil, "detach", nil, nil))
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.GetServerIdentityImpact(ctx, impactPayload(otherIssuerID, nil, "detach", &otherProjectID, nil))
	requireOopsCode(t, err, oops.CodeBadRequest)
}
