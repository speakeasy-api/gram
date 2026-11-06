package mcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestResourceQualifiedTokens_DirectAndGatewayKeepBothResources(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := TestInfra.CloneTestDatabase(t, "resource_qualified_callers")
	require.NoError(t, err)
	// Exercise the post-migration model in this private clone; the application
	// compatibility PR must also run against the unchanged pre-migration schema.
	fixtures := testrepo.New(pool)
	require.NoError(t, fixtures.ExpandRemoteSessionResourceIndexesFixture(ctx))
	f := newEnforcedCallerFixture(t, pool)
	require.NoError(t, remotesessionsrepo.New(pool).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: f.registration, UserSessionIssuerID: f.signIn}))
	serverB, _ := f.server()
	subject := urn.NewUserSubject("shared-resource-user")
	request := remotesessions.AuthClientTokenRequest{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: f.signIn, Subject: subject, RegistrationID: f.registration}
	q := remotesessionsrepo.New(pool)
	type target struct {
		id              uuid.UUID
		resource, token string
	}
	targets := []target{{id: f.serverID, token: "token-a"}, {id: serverB, token: "token-b"}}
	for i := range targets {
		selected, err := f.service.resourceQualifiedRequest(ctx, targets[i].id, request)
		require.NoError(t, err)
		require.NotEmpty(t, selected.Resource)
		targets[i].resource = selected.Resource
		encrypted, err := f.service.enc.Encrypt([]byte(targets[i].token))
		require.NoError(t, err)
		_, err = q.UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
			SubjectUrn: subject, UserSessionIssuerID: f.consentedUnder, RemoteSessionClientID: f.registration,
			AccessTokenEncrypted: encrypted, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
			Resource: conv.ToPGText(selected.Resource), Scopes: []string{},
		})
		require.NoError(t, err)
	}
	require.NotEqual(t, targets[0].resource, targets[1].resource)
	// Repeat after both grants exist. Existing and newly built request contexts
	// select A and B independently; no server-level permission grant is created.
	for range 2 {
		for _, target := range targets {
			endpoint := &ResolvedMcpEndpoint{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: f.signIn, McpServerID: validUUID(target.id), UpstreamResource: target.resource}
			direct, err := f.service.resolveSelectedAccessTokens(ctx, &issuerGateAuthentication{endpoint: endpoint, subject: subject, chosenClient: validUUID(f.registration), checkedRegistration: true}, false)
			require.NoError(t, err)
			require.Equal(t, target.token, direct[f.issuerID].Token)
			require.Equal(t, target.resource, direct[f.issuerID].Resource)
			member := metaMember{serverID: target.id, projectID: f.projectID, chosenClientID: validUUID(f.registration), typedAuthClient: true}
			gateway, err := f.service.resolveChosenMemberTokens(ctx, remotesessions.RefreshAttribution{}, endpoint, subject, true, member)
			require.NoError(t, err)
			require.Equal(t, target.token, gateway[f.issuerID].Token)
			require.Equal(t, target.resource, gateway[f.issuerID].Resource)
			legacyDirect, err := f.service.resolveSelectedAccessTokens(ctx, &issuerGateAuthentication{endpoint: endpoint, subject: subject, chosenClient: validUUID(f.registration)}, false)
			require.NoError(t, err)
			require.Equal(t, target.token, legacyDirect[f.issuerID].Token)
			member.typedAuthClient = false
			legacyGateway, err := f.service.resolveChosenMemberTokens(ctx, remotesessions.RefreshAttribution{}, endpoint, subject, false, member)
			require.NoError(t, err)
			require.Equal(t, target.token, legacyGateway[f.issuerID].Token)
		}
	}
	// A selected legacy request cannot adopt B's singleton token after A is revoked.
	selectedA, err := q.GetActiveRemoteSession(ctx, remotesessionsrepo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: f.registration, Resource: targets[0].resource})
	require.NoError(t, err)
	_, err = q.RevokeRemoteSession(ctx, remotesessionsrepo.RevokeRemoteSessionParams{ID: selectedA.ID, ProjectID: f.projectID, OrganizationID: f.orgID})
	require.NoError(t, err)
	endpointA := &ResolvedMcpEndpoint{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: f.signIn, McpServerID: validUUID(targets[0].id), UpstreamResource: targets[0].resource}
	_, err = f.service.resolveSelectedAccessTokens(ctx, &issuerGateAuthentication{endpoint: endpointA, subject: subject, chosenClient: validUUID(f.registration)}, false)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	request.ProjectID = uuid.New()
	_, err = f.service.resourceQualifiedRequest(ctx, f.serverID, request)
	require.Error(t, err, "a foreign project cannot supply a target resource")
}

func TestResourceQualifiedTokens_ForwardKnownAudienceWithoutReload(t *testing.T) {
	t.Parallel()
	source := &fakeUpstreamTokens{issuerID: uuid.New(), bound: uuid.New()}
	service := &Service{upstreamTokens: source}
	endpoint := &ResolvedMcpEndpoint{ProjectID: uuid.New(), OrganizationID: "test-org", UserSessionIssuerID: uuid.New(), McpServerID: validUUID(uuid.New()), UpstreamResource: "https://known-audience.example/mcp/"}
	authentication := &issuerGateAuthentication{endpoint: endpoint, subject: urn.NewUserSubject("known-audience-user"), chosenClient: validUUID(uuid.New()), checkedRegistration: true}
	_, err := service.resolveSelectedAccessTokens(t.Context(), authentication, false)
	require.NoError(t, err)
	authentication.checkedRegistration = false
	_, err = service.resolveSelectedAccessTokens(t.Context(), authentication, false)
	require.NoError(t, err)
	require.Equal(t, []string{endpoint.UpstreamResource, endpoint.UpstreamResource}, source.resources)
}
